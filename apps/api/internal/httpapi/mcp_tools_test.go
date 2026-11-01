package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// mcpResult calls a tool over Streamable HTTP and returns its structured
// content, failing the test on a tool error.
func mcpResult(e *env, key, tool string, args map[string]any) map[string]any {
	e.t.Helper()
	_, out := mcpCall(e, key, tool, args)
	if isToolError(out) {
		e.t.Fatalf("%s: %v", tool, out)
	}
	sc, _ := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	return sc
}

// Every REST capability (except browser-only flows and the session-only
// recovery key rotation) is reachable as an MCP tool.
func TestMCPToolsCoverREST(t *testing.T) {
	e := newEnv(t)
	var list map[string]any
	e.do(nil, "POST", "/mcp", e.key, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, 200, &list,
		"Accept", "application/json, text/event-stream")
	tools := map[string]map[string]any{}
	for _, raw := range list["result"].(map[string]any)["tools"].([]any) {
		tl := raw.(map[string]any)
		tools[tl["name"].(string)] = tl
	}
	readOnly := strings.Fields(`list_collections get_collection get_media list_media get_media_usage list_media_folders list_events export_events list_webhooks get_webhook
		list_webhook_deliveries list_alerts get_alert list_alert_deliveries list_api_keys list_sessions get_workspace
		get_workspace_export list_plans get_usage get_billing list_providers list_channels get_channel list_posts get_post
		search_posts get_post_history preview_post list_requested_work list_tags list_schedules list_jobs
		get_post_metrics list_preview_links list_post_comments list_channel_groups list_channel_links list_channel_options
		get_generated_media get_analytics`)
	writes := strings.Fields(`create_collection update_collection delete_collection upload_media update_media delete_media create_media_folder update_media_folder delete_media_folder organize_media restore_media keep_media generate_media keep_generated_media create_webhook
		update_webhook delete_webhook rotate_webhook_secret test_webhook retry_webhook_delivery create_alert update_alert
		delete_alert test_alert create_api_key revoke_api_key rotate_api_key revoke_session rename_workspace delete_workspace
		start_workspace_export create_device_link start_billing_checkout open_billing_portal connect_channel
		update_channel delete_channel test_channel create_post update_post schedule_post publish_post unpublish_post cancel_post duplicate_post
		delete_post delete_posts purge_post report_publish_result retry_target create_schedule update_schedule delete_schedule run_schedule retry_job
		report_post_metrics create_preview_link revoke_preview_link reply_to_post_comment resolve_post_comment create_channel_group update_channel_group
		delete_channel_group create_channel_link revoke_channel_link`)
	for _, n := range readOnly {
		tl, ok := tools[n]
		if !ok {
			t.Fatalf("missing tool %s", n)
		}
		if ann, _ := tl["annotations"].(map[string]any); ann["readOnlyHint"] != true {
			t.Errorf("%s should be read-only: %v", n, tl["annotations"])
		}
	}
	for _, n := range writes {
		tl, ok := tools[n]
		if !ok {
			t.Fatalf("missing tool %s", n)
		}
		if ann, _ := tl["annotations"].(map[string]any); ann["readOnlyHint"] == true {
			t.Errorf("%s must not be read-only", n)
		}
	}
	if want := len(readOnly) + len(writes); len(tools) != want || want != 106 {
		t.Errorf("tool count %d, want %d (every tool listed here)", len(tools), want)
	}
	if _, ok := tools["rotate_recovery_key"]; ok {
		t.Error("recovery key rotation is dashboard-session only")
	}
	// Destructive ones say so; required inputs are required (also through
	// embedded field groups).
	if ann := tools["delete_webhook"]["annotations"].(map[string]any); ann["destructiveHint"] != true {
		t.Errorf("delete_webhook annotations %v", ann)
	}
	// A custom channel's worker closes targets through report_publish_result: the schema says what is required.
	schema := tools["report_publish_result"]["inputSchema"].(map[string]any)
	if fmt.Sprint(schema["required"]) != "[post_id channel_id status]" {
		t.Errorf("report_publish_result required %v", schema["required"])
	}
	schema = tools["create_webhook"]["inputSchema"].(map[string]any)
	if fmt.Sprint(schema["required"]) != "[url]" || schema["properties"].(map[string]any)["events"] == nil {
		t.Errorf("create_webhook schema %v", schema)
	}
	schema = tools["delete_collection"]["inputSchema"].(map[string]any)
	if fmt.Sprint(schema["required"]) != "[collection confirm]" {
		t.Errorf("delete_collection required %v", schema["required"])
	}
}

func TestMCPRESTParityEndToEnd(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.AllScopes...)
	c := e.collection("Deploys")

	// Webhooks: create, read, rotate, test, delete.
	wh := mcpResult(e, e.key, "create_webhook", map[string]any{"url": "http://127.0.0.1:9/hook", "events": []string{"post.published"}})
	id, secret := wh["id"].(string), wh["secret"]
	if id == "" || secret == "" || fmt.Sprint(wh["events"]) != "[post.published]" {
		t.Fatalf("create_webhook: %v", wh)
	}
	if got := mcpResult(e, e.key, "get_webhook", map[string]any{"webhook_id": id}); got["id"] != id {
		t.Fatalf("get_webhook: %v", got)
	}
	if got := mcpResult(e, e.key, "update_webhook", map[string]any{"webhook_id": id, "description": "deploy bot"}); got["description"] != "deploy bot" {
		t.Fatalf("update_webhook: %v", got)
	}
	if got := mcpResult(e, e.key, "rotate_webhook_secret", map[string]any{"webhook_id": id}); got["secret"] == secret || got["secret"] == "" {
		t.Fatalf("rotate_webhook_secret: %v", got)
	}
	if got := mcpResult(e, e.key, "test_webhook", map[string]any{"webhook_id": id}); got["event_type"] != "webhook.test" {
		t.Fatalf("test_webhook: %v", got)
	}
	if got := mcpResult(e, e.key, "list_webhooks", nil); len(got["webhooks"].([]any)) != 1 {
		t.Fatalf("list_webhooks: %v", got)
	}
	mcpResult(e, e.key, "delete_webhook", map[string]any{"webhook_id": id})
	if got := mcpResult(e, e.key, "list_webhooks", nil); len(got["webhooks"].([]any)) != 0 {
		t.Fatalf("webhook not deleted: %v", got)
	}

	// Posts: publish on your own automation, report the result.
	ch := e.customChannel("Bot")
	a := e.post(c.ID, map[string]any{"content": "migrate users", "targets": []map[string]any{{"channel_id": ch.ID}}})
	b := e.post(c.ID, map[string]any{"content": "migrate orders"})
	if got := mcpResult(e, e.key, "publish_post", map[string]any{"post_id": a.ID}); got["status"] != "publishing" {
		t.Fatalf("publish_post: %v", got)
	}
	e.svc.RunDueJobsOnce(context.Background())
	if got := mcpResult(e, e.key, "report_publish_result", map[string]any{"post_id": a.ID, "channel_id": ch.ID, "status": "published", "url": "https://example.com/p/1"}); got["status"] != "published" {
		t.Fatalf("report_publish_result: %v", got)
	}
	if got := mcpResult(e, e.key, "get_post", map[string]any{"post_id": a.ID}); got["status"] != "published" {
		t.Fatalf("after publishing: %v", got)
	}

	// Events: list and export.
	ev := mcpResult(e, e.key, "list_events", map[string]any{"type": "post.published"})
	if len(ev["data"].([]any)) != 1 {
		t.Fatalf("list_events: %v", ev)
	}
	x := mcpResult(e, e.key, "export_events", map[string]any{"collection": "Deploys", "type_prefix": "post."})
	if n := x["events"].(float64); x["format"] != "ndjson" || x["truncated"] != false || n < 4 ||
		strings.Count(x["content"].(string), "\n") != int(n) {
		t.Fatalf("export_events: %v", x)
	}
	csv := mcpResult(e, e.key, "export_events", map[string]any{"format": "csv", "post_id": a.ID})
	if !strings.HasPrefix(csv["content"].(string), "created_at,id,type") {
		t.Fatalf("csv export: %v", csv)
	}

	// Media, collections.
	m := mcpResult(e, e.key, "upload_media", map[string]any{"media_base64": "T2dnUwACAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "filename": "voice.ogg"})
	got := mcpResult(e, e.key, "get_media", map[string]any{"media_id": m["id"], "include_content": true})
	if got["content_base64"] != "T2dnUwACAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" || got["original_name"] != "voice.ogg" {
		t.Fatalf("get_media: %v", got)
	}
	// Deleting needs its own scope: posts:write (in the default key) is not enough, posts:delete is.
	writer := e.keyWith(app.ScopeCollectionsRead, app.ScopeCollectionsWrite, app.ScopePostsRead, app.ScopePostsWrite, app.ScopeMediaWrite)
	for _, k := range []string{e.key, writer} {
		if _, out := mcpCall(e, k, "delete_media", map[string]any{"media_id": m["id"]}); !isToolError(out) || !strings.Contains(fmt.Sprint(out), "posts:delete") {
			t.Fatalf("delete_media without posts:delete: %v", out)
		}
		if _, out := mcpCall(e, k, "delete_post", map[string]any{"post_id": b.ID}); !isToolError(out) || !strings.Contains(fmt.Sprint(out), "posts:delete") {
			t.Fatalf("delete_post without posts:delete: %v", out)
		}
		if _, out := mcpCall(e, k, "delete_collection", map[string]any{"collection": c.ID, "confirm": "Deploys"}); !isToolError(out) || !strings.Contains(fmt.Sprint(out), "collections:delete") {
			t.Fatalf("delete_collection without collections:delete: %v", out)
		}
	}
	deleter := e.keyWith(app.ScopePostsDelete)
	mcpResult(e, deleter, "delete_media", map[string]any{"media_id": m["id"]})
	mcpResult(e, deleter, "delete_post", map[string]any{"post_id": b.ID})
	// repeat_every over MCP: set on create, cleared with an empty string on update, refused when unknown.
	rp := mcpResult(e, e.key, "create_post", map[string]any{"title": "Post", "collection": "Deploys", "content": "weekly", "repeat_every": "1w"})
	if rp["repeat_every"] != "1w" {
		t.Fatalf("create_post repeat_every: %v", rp)
	}
	if got := mcpResult(e, e.key, "update_post", map[string]any{"post_id": rp["id"], "repeat_every": ""}); got["repeat_every"] != "" {
		t.Fatalf("update_post clears repeat_every: %v", got)
	}
	if _, out := mcpCall(e, e.key, "create_post", map[string]any{"title": "Post", "collection": "Deploys", "content": "x", "repeat_every": "1y"}); !isToolError(out) {
		t.Fatal("create_post with an unknown repeat_every")
	}
	if got := mcpResult(e, e.key, "update_collection", map[string]any{"collection": "Deploys", "description": "prod"}); got["description"] != "prod" {
		t.Fatalf("update_collection: %v", got)
	}
	if _, out := mcpCall(e, e.key, "delete_collection", map[string]any{"collection": c.ID, "confirm": "wrong"}); !isToolError(out) {
		t.Fatal("delete_collection without the right confirm")
	}

	// Scopes are the service's: the default key is not admin.
	if _, out := mcpCall(e, e.key, "list_api_keys", nil); !isToolError(out) {
		t.Fatal("non-admin key listed API keys")
	}
	if _, out := mcpCall(e, e.key, "purge_post", map[string]any{"post_id": a.ID}); !isToolError(out) {
		t.Fatal("non-admin key purged a post")
	}
	keys := mcpResult(e, admin, "list_api_keys", nil)
	if len(keys["keys"].([]any)) < 2 {
		t.Fatalf("list_api_keys: %v", keys)
	}
	// Session-only operations stay session-only.
	if _, out := mcpCall(e, admin, "delete_workspace", map[string]any{"confirm": "Test"}); !isToolError(out) ||
		!strings.Contains(fmt.Sprint(out), "dashboard session") {
		t.Fatalf("API key deleted the workspace: %v", out)
	}
	if _, out := mcpCall(e, admin, "create_device_link", nil); !isToolError(out) {
		t.Fatal("API key created a device link")
	}
	if got := mcpResult(e, admin, "purge_post", map[string]any{"post_id": a.ID}); got["post_id"] != a.ID {
		t.Fatalf("purge_post: %v", got)
	}
}

// A suspended workspace: reads work, writes are refused, and the REST
// allowlist (revoke a key, pay) still works.
func TestMCPWriteToolsBlockedWhenSuspended(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.AllScopes...)
	spare := e.keyWith(app.ScopePostsRead)
	var keys struct{ Data []app.APIKey }
	e.do(nil, "GET", "/v1/api-keys", admin, nil, 200, &keys)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if err := e.svc.SetPlan(context.Background(), ws.ID, "pro", "2020-01-01"); err != nil {
		t.Fatal(err)
	}

	if _, out := mcpCall(e, e.key, "create_webhook", map[string]any{"url": "http://127.0.0.1:9/hook"}); !isToolError(out) ||
		!strings.Contains(fmt.Sprint(out), "read-only") {
		t.Fatalf("create_webhook while suspended: %v", out)
	}
	// Claude Code speaks protocol 2026-07-28 and rejects a tool result without resultType as malformed.
	var newer map[string]any
	e.do(nil, "POST", "/mcp", e.key, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "create_webhook", "arguments": map[string]any{"url": "http://127.0.0.1:9/hook"},
			"_meta": map[string]any{mcp.MetaKeyProtocolVersion: "2026-07-28", mcp.MetaKeyClientInfo: map[string]any{"name": "claude-code", "version": "2"},
				mcp.MetaKeyClientCapabilities: map[string]any{}}}},
		0, &newer, "Accept", "application/json, text/event-stream", "Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "create_webhook")
	if res, _ := newer["result"].(map[string]any); !isToolError(newer) || res["resultType"] != "complete" {
		t.Fatalf("suspended refusal under protocol 2026-07-28: %v", newer)
	}
	if _, out := mcpCall(e, e.key, "publish_post", map[string]any{"post_id": "post_x"}); !isToolError(out) ||
		!strings.Contains(fmt.Sprint(out), "read-only") {
		t.Fatalf("publish while suspended: %v", out)
	}
	mcpResult(e, e.key, "list_webhooks", nil)
	mcpResult(e, e.key, "list_events", nil)
	if u := mcpResult(e, e.key, "get_usage", nil); u["suspended"] != true {
		t.Fatalf("usage: %v", u)
	}

	var spareID string
	for _, k := range keys.Data {
		if strings.HasPrefix(spare, k.Prefix) && k.RevokedAt == nil {
			spareID = k.ID
		}
	}
	mcpResult(e, admin, "revoke_api_key", map[string]any{"key_id": spareID})
	// Paying (here the operator) lifts the suspension.
	if err := e.svc.SetPlan(context.Background(), ws.ID, "starter", ""); err != nil {
		t.Fatal(err)
	}
	mcpResult(e, e.key, "create_webhook", map[string]any{"url": "http://127.0.0.1:9/hook"})
}
