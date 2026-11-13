package httpapi_test

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// toolRoutes: the REST route each MCP tool stands for. Both call the same service, so whoever REST refuses the
// tool refuses, in the same words. A new tool without an entry fails TestMCPToolsAnswerLikeREST.
var toolRoutes = map[string]string{
	"get_workspace": "GET /v1/workspace", "get_usage": "GET /v1/workspace/usage", "rename_workspace": "PATCH /v1/workspace",
	"delete_workspace": "DELETE /v1/workspace", "list_plans": "GET /v1/plans",
	"start_workspace_export": "POST /v1/workspace/export", "get_workspace_export": "GET /v1/workspace/exports/{id}",
	"get_billing": "GET /v1/billing", "start_billing_checkout": "POST /v1/billing/checkout", "open_billing_portal": "POST /v1/billing/portal",
	"list_sessions": "GET /v1/sessions", "revoke_session": "DELETE /v1/sessions/{id}", "create_device_link": "POST /v1/device-links",
	"list_api_keys": "GET /v1/api-keys", "create_api_key": "POST /v1/api-keys", "revoke_api_key": "DELETE /v1/api-keys/{id}",
	"rotate_api_key": "POST /v1/api-keys/{id}/rotate",

	"list_collections": "GET /v1/collections", "get_collection": "GET /v1/collections/{id}", "create_collection": "POST /v1/collections",
	"update_collection": "PATCH /v1/collections/{id}", "delete_collection": "DELETE /v1/collections/{id}",

	"list_posts": "GET /v1/posts", "search_posts": "GET /v1/posts", "get_post": "GET /v1/posts/{id}", "create_post": "POST /v1/posts",
	"update_post": "PATCH /v1/posts/{id}", "delete_post": "DELETE /v1/posts/{id}", "delete_posts": "POST /v1/posts/delete",
	"purge_post": "POST /v1/posts/{id}/purge", "duplicate_post": "POST /v1/posts/{id}/duplicate",
	"archive_post": "POST /v1/posts/{id}/archive", "unarchive_post": "POST /v1/posts/{id}/unarchive",
	"schedule_post": "POST /v1/posts/{id}/schedule", "publish_post": "POST /v1/posts/{id}/publish",
	"cancel_post": "POST /v1/posts/{id}/cancel", "unpublish_post": "POST /v1/posts/{id}/unpublish",
	"retry_target": "POST /v1/posts/{id}/targets/{channel}/retry", "report_publish_result": "POST /v1/posts/{id}/targets/{channel}/result",
	"report_post_metrics": "PUT /v1/posts/{id}/targets/{channel}/metrics", "get_post_metrics": "GET /v1/posts/{id}/metrics",
	"get_post_history": "GET /v1/posts/{id}/events", "preview_post": "GET /v1/posts/{id}/preview",
	"get_post_responses": "GET /v1/posts/{id}/responses", "refresh_post_responses": "POST /v1/posts/{id}/responses/refresh",
	"list_preview_links": "GET /v1/posts/{id}/previews", "create_preview_link": "POST /v1/posts/{id}/previews",
	"revoke_preview_link": "DELETE /v1/posts/{id}/previews/{preview}", "list_post_comments": "GET /v1/posts/{id}/comments",
	"reply_to_post_comment": "POST /v1/posts/{id}/comments", "resolve_post_comment": "POST /v1/posts/{id}/comments/{comment}/resolve",
	"list_tags": "GET /v1/tags", "list_requested_work": "GET /v1/work", "get_analytics": "GET /v1/analytics",
	"list_jobs": "GET /v1/jobs", "retry_job": "POST /v1/jobs/{id}/retry",

	"list_schedules": "GET /v1/schedules", "create_schedule": "POST /v1/schedules", "update_schedule": "PATCH /v1/schedules/{id}",
	"delete_schedule": "DELETE /v1/schedules/{id}", "run_schedule": "POST /v1/schedules/{id}/run",

	"list_media": "GET /v1/media", "get_media": "GET /v1/media/{id}/info", "get_media_usage": "GET /v1/media/{id}/usage",
	"upload_media": "POST /v1/media", "update_media": "PATCH /v1/media/{id}", "organize_media": "POST /v1/media/bulk",
	"delete_media": "DELETE /v1/media/{id}", "restore_media": "POST /v1/media/{id}/restore", "keep_media": "POST /v1/media/{id}/keep",
	"list_media_folders": "GET /v1/media-folders", "create_media_folder": "POST /v1/media-folders",
	"update_media_folder": "PATCH /v1/media-folders/{id}", "delete_media_folder": "DELETE /v1/media-folders/{id}",
	"generate_media": "POST /v1/ai/jobs", "get_generated_media": "GET /v1/ai/jobs/{id}", "keep_generated_media": "POST /v1/ai/jobs/{id}/accept",

	"list_providers": "GET /v1/providers", "list_channels": "GET /v1/channels", "get_channel": "GET /v1/channels/{id}",
	"connect_channel": "POST /v1/channels", "update_channel": "PATCH /v1/channels/{id}", "delete_channel": "DELETE /v1/channels/{id}",
	"test_channel": "POST /v1/channels/{id}/test", "list_channel_options": "GET /v1/channels/{id}/options/{key}",
	"list_channel_groups": "GET /v1/channel-groups", "create_channel_group": "POST /v1/channel-groups",
	"update_channel_group": "PATCH /v1/channel-groups/{id}", "delete_channel_group": "DELETE /v1/channel-groups/{id}",
	"list_channel_links": "GET /v1/channel-links", "create_channel_link": "POST /v1/channel-links",
	"revoke_channel_link": "DELETE /v1/channel-links/{id}",

	"list_events": "GET /v1/events", "export_events": "GET /v1/events/export",
	"list_webhooks": "GET /v1/webhooks", "get_webhook": "GET /v1/webhooks/{id}", "create_webhook": "POST /v1/webhooks",
	"update_webhook": "PATCH /v1/webhooks/{id}", "rotate_webhook_secret": "PATCH /v1/webhooks/{id}",
	"delete_webhook": "DELETE /v1/webhooks/{id}", "test_webhook": "POST /v1/webhooks/{id}/test",
	"list_webhook_deliveries": "GET /v1/webhook-deliveries", "retry_webhook_delivery": "POST /v1/webhook-deliveries/{id}/retry",
	"list_alerts": "GET /v1/alerts", "get_alert": "GET /v1/alerts/{id}", "create_alert": "POST /v1/alerts",
	"update_alert": "PATCH /v1/alerts/{id}", "delete_alert": "DELETE /v1/alerts/{id}", "test_alert": "POST /v1/alerts/{id}/test",
	"list_alert_deliveries": "GET /v1/alerts/{id}/deliveries",
}

// toolArgs: arguments a tool checks itself before it calls the service; the rest come from its schema's required
// fields ("nobody" for a string, nothing for a list).
var toolArgs = map[string]map[string]any{
	"upload_media":          {"media_base64": "iVBORw0KGgo=", "filename": "dot.png"},
	"generate_media":        {"kind": "image", "prompt": "a dot"},
	"create_api_key":        {"name": "nobody", "scopes": []string{postR}},
	"report_publish_result": {"post_id": "nobody", "channel_id": "nobody", "status": "published"},
	"update_collection":     {"collection": "Seed"},
	"delete_collection":     {"collection": "Seed", "confirm": "not its name"},
}

// toolNeedsToo: where a tool asks for more than its route. These two take a collection by name or id and look it
// up first, so a key that may write or delete collections but not read them is refused by the tool only.
var toolNeedsToo = map[string][]string{"update_collection": {colR}, "delete_collection": {colR}}

// mcpTool: a tool as the pinned tool list (mcpserver/testdata/tools.golden.json) has it.
type mcpTool struct {
	Name        string `json:"name"`
	InputSchema struct {
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	} `json:"inputSchema"`
}

// refusal: an answer that says who may ask, not what was asked for.
func refusal(text string) bool {
	for _, phrase := range []string{"missing required scope", "signed-in person", "only an owner", "only admins and owners", "dashboard"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// Every MCP tool against the rule of its REST route (accessRules): the keys REST refuses get the same refusal,
// word for word, from the tool; the keys REST lets in are let in.
func TestMCPToolsAnswerLikeREST(t *testing.T) {
	raw, err := os.ReadFile("../mcpserver/testdata/tools.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var tools []mcpTool
	if err := json.Unmarshal(raw, &tools); err != nil || len(tools) < 100 {
		t.Fatalf("tool list: %v (%d tools)", err, len(tools))
	}
	e := newMatrixEnv(t)
	e.callers()
	withStripe(t, e.env)
	e.as(e.owner, "POST", "/v1/collections", map[string]any{"name": "Seed"}, 201, nil)
	for name := range toolRoutes {
		if !slices.ContainsFunc(tools, func(tool mcpTool) bool { return tool.Name == name }) {
			t.Errorf("toolRoutes names %q, which is not a tool", name)
		}
	}
	checks := 0
	for _, tool := range tools {
		route, ok := toolRoutes[tool.Name]
		r, known := accessRules[route]
		if !ok || !known {
			t.Errorf("tool %s: no REST route in toolRoutes (or no rule for %q)", tool.Name, route)
			continue
		}
		if r.public {
			r = anyone() // /mcp itself needs a key; any key will do
		}
		args := toolArgs[tool.Name]
		if args == nil {
			args = map[string]any{}
			for _, req := range tool.InputSchema.Required {
				if fmt.Sprint(tool.InputSchema.Properties[req]["type"]) == "string" {
					args[req] = "nobody"
				} else {
					args[req] = []string{}
				}
			}
		}
		for _, c := range e.keysFor(r) {
			status, _, body := e.send(c, route, r)
			var rest struct{ Error struct{ Message string } }
			_ = json.Unmarshal([]byte(body), &rest)
			code, out := mcpCall(e.env, c.key, tool.Name, args)
			text := fmt.Sprint(out["result"])
			checks++
			tr := r
			tr.scopes = append(slices.Clone(r.scopes), toolNeedsToo[tool.Name]...)
			switch want, wantTool := c.allowed(r), c.allowed(tr); {
			case code != 200:
				t.Errorf("%s as %s: /mcp answered %d", tool.Name, c.name, code)
			case want != (status != 403):
				t.Errorf("%s as %s: REST %s let in = %v, want %v (%d)", tool.Name, c.name, route, !want, want, status)
			case wantTool != !(isToolError(out) && refusal(text)):
				t.Errorf("%s as %s: the tool let in = %v, want %v: %s", tool.Name, c.name, !wantTool, wantTool, text)
			case !want && !strings.Contains(text, rest.Error.Message):
				t.Errorf("%s as %s: REST %s refuses with %q, the tool with %s", tool.Name, c.name, route, rest.Error.Message, text)
			}
		}
	}
	t.Logf("%d tools, %d checks", len(tools), checks)
}
