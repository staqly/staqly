package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
	readOnly := strings.Fields(`get_item_versions get_media list_events export_events list_webhooks get_webhook
		list_webhook_deliveries get_watcher list_notification_channels get_notification_channel
		list_notification_deliveries list_api_keys list_sessions get_workspace get_workspace_export list_plans get_usage get_billing`)
	writes := strings.Fields(`update_collection delete_collection delete_item purge_item execute_bulk_action upload_media
		delete_media create_webhook update_webhook delete_webhook rotate_webhook_secret test_webhook retry_webhook_delivery
		create_watcher update_watcher delete_watcher run_watcher create_notification_channel
		update_notification_channel delete_notification_channel test_notification_channel create_api_key revoke_api_key
		rotate_api_key revoke_session rename_workspace delete_workspace start_workspace_export choose_free_plan create_device_link start_billing_checkout open_billing_portal`)
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
	if len(tools) != 71 {
		t.Errorf("tool count %d, want 71", len(tools))
	}
	if _, ok := tools["rotate_recovery_key"]; ok {
		t.Error("recovery key rotation is dashboard-session only")
	}
	// Destructive ones say so; required inputs are required (also through
	// embedded field groups).
	if ann := tools["delete_webhook"]["annotations"].(map[string]any); ann["destructiveHint"] != true {
		t.Errorf("delete_webhook annotations %v", ann)
	}
	// await_decision's default wait stays under the 60 s tool timeout of Claude Code and Codex.
	wait := tools["await_decision"]["inputSchema"].(map[string]any)["properties"].(map[string]any)["timeout_seconds"].(map[string]any)
	if !strings.Contains(fmt.Sprint(wait["description"]), "default 45") {
		t.Errorf("await_decision timeout_seconds: %v", wait)
	}
	schema := tools["create_webhook"]["inputSchema"].(map[string]any)
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
	wh := mcpResult(e, e.key, "create_webhook", map[string]any{"url": "http://127.0.0.1:9/hook", "events": []string{"item.approved"}})
	id, secret := wh["id"].(string), wh["secret"]
	if id == "" || secret == "" || fmt.Sprint(wh["events"]) != "[item.approved]" {
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

	// Bulk approve two pending items; the third id fails on its own.
	a := e.item(c.ID, map[string]any{"title": "migrate users"})
	b := e.item(c.ID, map[string]any{"title": "migrate orders"})
	res := mcpResult(e, e.key, "execute_bulk_action", map[string]any{"item_ids": []string{a.ID, b.ID, "itm_missing"}, "action": "approve"})
	results := res["results"].([]any)
	if len(results) != 3 || results[0].(map[string]any)["ok"] != true || results[1].(map[string]any)["ok"] != true ||
		results[2].(map[string]any)["ok"] != false {
		t.Fatalf("bulk approve: %v", res)
	}
	if got := mcpResult(e, e.key, "get_item", map[string]any{"item_id": a.ID}); got["status"] != "approved" {
		t.Fatalf("after bulk approve: %v", got)
	}

	// Events: list and export.
	ev := mcpResult(e, e.key, "list_events", map[string]any{"type": "item.approved"})
	if len(ev["data"].([]any)) != 2 {
		t.Fatalf("list_events: %v", ev)
	}
	x := mcpResult(e, e.key, "export_events", map[string]any{"collection": "Deploys", "type_prefix": "item."})
	if x["format"] != "ndjson" || x["truncated"] != false || x["events"].(float64) != 4 ||
		strings.Count(x["content"].(string), "\n") != 4 {
		t.Fatalf("export_events: %v", x)
	}
	csv := mcpResult(e, e.key, "export_events", map[string]any{"format": "csv", "item_id": a.ID})
	if !strings.HasPrefix(csv["content"].(string), "created_at,id,type") {
		t.Fatalf("csv export: %v", csv)
	}

	// Items, media, collections.
	if v := mcpResult(e, e.key, "get_item_versions", map[string]any{"item_id": a.ID}); len(v["versions"].([]any)) == 0 {
		t.Fatalf("versions: %v", v)
	}
	m := mcpResult(e, e.key, "upload_media", map[string]any{"media_base64": "aGVsbG8=", "filename": "hello.txt"})
	got := mcpResult(e, e.key, "get_media", map[string]any{"media_id": m["id"], "include_content": true})
	if got["content_base64"] != "aGVsbG8=" || got["original_name"] != "hello.txt" {
		t.Fatalf("get_media: %v", got)
	}
	mcpResult(e, e.key, "delete_media", map[string]any{"media_id": m["id"]})
	mcpResult(e, e.key, "delete_item", map[string]any{"item_id": b.ID})
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
	if _, out := mcpCall(e, e.key, "purge_item", map[string]any{"item_id": a.ID}); !isToolError(out) {
		t.Fatal("non-admin key purged an item")
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
	if got := mcpResult(e, admin, "purge_item", map[string]any{"item_id": a.ID}); got["item_id"] != a.ID {
		t.Fatalf("purge_item: %v", got)
	}
}

// A suspended workspace: reads work, writes are refused, and the REST
// allowlist (revoke a key, switch to Free) still works.
func TestMCPWriteToolsBlockedWhenSuspended(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.AllScopes...)
	spare := e.keyWith(app.ScopeItemsRead)
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
	if _, out := mcpCall(e, e.key, "execute_bulk_action", map[string]any{"item_ids": []string{"itm_x"}, "action": "approve"}); !isToolError(out) ||
		!strings.Contains(fmt.Sprint(out), "read-only") {
		t.Fatalf("bulk action while suspended: %v", out)
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
	if u := mcpResult(e, admin, "choose_free_plan", nil); u["suspended"] != false {
		t.Fatalf("choose_free_plan: %v", u)
	}
	mcpResult(e, e.key, "create_webhook", map[string]any{"url": "http://127.0.0.1:9/hook"})
}
