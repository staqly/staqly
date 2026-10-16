package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func mcpErr(e *env, key, tool string, args map[string]any, substr string) {
	e.t.Helper()
	_, out := mcpCall(e, key, tool, args)
	if !isToolError(out) || !strings.Contains(strings.ToLower(fmt.Sprint(out)), substr) {
		e.t.Fatalf("%s: want a tool error containing %q, got %v", tool, substr, out)
	}
}

// Rules, watchers and notification channels through MCP: same results and
// the same authorization as REST.
func TestMCPRulesWatchersChannels(t *testing.T) {
	e := newEnv(t)
	e.svc.Fetcher = http.DefaultClient
	admin := e.keyWith(app.AllScopes...)
	reader := e.keyWith(app.ScopeItemsRead)
	feed := &feedServer{items: []string{rssItem("1", "first", "x")}}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	c := e.collection("MCP")

	// Watchers.
	w := mcpResult(e, admin, "create_watcher", map[string]any{"name": "feed", "kind": "rss", "url": srv.URL + "/feed.xml",
		"collection": c.Name, "keywords": []string{"news"}, "every": "2h"})
	wid := w["id"].(string)
	if w["collection_id"] != c.ID || w["every_seconds"] != float64(7200) {
		t.Fatalf("create_watcher: %v", w)
	}
	if run := mcpResult(e, admin, "run_watcher", map[string]any{"watcher_id": wid}); run["baseline"] != true || run["entries_seen"] != float64(1) {
		t.Fatalf("run_watcher: %v", run)
	}
	if got := mcpResult(e, admin, "get_watcher", map[string]any{"watcher_id": wid}); got["last_run_at"] == nil {
		t.Fatalf("get_watcher after run: %v", got)
	}
	if got := mcpResult(e, admin, "update_watcher", map[string]any{"watcher_id": wid, "status": "paused", "exclude_keywords": []string{"ads"}}); got["status"] != "paused" {
		t.Fatalf("update_watcher: %v", got)
	}
	if got := mcpResult(e, admin, "list_watchers", nil); len(got["watchers"].([]any)) != 1 {
		t.Fatalf("list_watchers: %v", got)
	}
	mcpErr(e, reader, "list_watchers", nil, "watchers:manage")
	mcpErr(e, admin, "create_watcher", map[string]any{"name": "bad", "kind": "rss", "url": "ftp://x", "collection": c.ID}, "url")
	mcpResult(e, admin, "delete_watcher", map[string]any{"watcher_id": wid})
	mcpErr(e, admin, "run_watcher", map[string]any{"watcher_id": wid}, "not found")

	// Notification channels: secrets masked, scope enforced.
	ch := mcpResult(e, admin, "create_notification_channel", map[string]any{"name": "phone", "type": "ntfy",
		"config": map[string]any{"topic": "alerts", "access_token": "tk_secret-123"}, "events": []string{"item.created"}})
	cid := ch["id"].(string)
	if strings.Contains(fmt.Sprint(ch), "secret-123") {
		t.Fatalf("channel secret not masked: %v", ch)
	}
	if got := mcpResult(e, admin, "get_notification_channel", map[string]any{"channel_id": cid}); got["name"] != "phone" {
		t.Fatalf("get channel: %v", got)
	}
	if got := mcpResult(e, admin, "update_notification_channel", map[string]any{"channel_id": cid, "name": "tablet", "enabled": false}); got["name"] != "tablet" || got["enabled"] != false {
		t.Fatalf("update channel: %v", got)
	}
	if got := mcpResult(e, admin, "list_notification_channels", nil); len(got["channels"].([]any)) != 1 {
		t.Fatalf("list channels: %v", got)
	}
	if got := mcpResult(e, admin, "list_notification_deliveries", map[string]any{"channel_id": cid, "limit": 5}); len(got["deliveries"].([]any)) != 0 {
		t.Fatalf("channel deliveries: %v", got)
	}
	mcpErr(e, reader, "list_notification_channels", nil, "channels:manage")
	mcpErr(e, admin, "create_notification_channel", map[string]any{"name": "x", "type": "pager"}, "type")
	mcpResult(e, admin, "delete_notification_channel", map[string]any{"channel_id": cid})
	mcpErr(e, admin, "get_notification_channel", map[string]any{"channel_id": cid}, "not found")
}

// Webhook deliveries through MCP: list, filter and retry a failed one.
func TestMCPWebhookDeliveriesRetry(t *testing.T) {
	e := newEnv(t)
	var fail atomic.Bool
	fail.Store(true)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(500)
		}
	}))
	defer recv.Close()
	wh := mcpResult(e, e.key, "create_webhook", map[string]any{"url": recv.URL + "/hook", "events": []string{"collection.created"}, "max_attempts": 1})
	whID := wh["id"].(string)
	stop := runWorker(e)
	defer stop()
	e.collection("Triggers")
	var dlv map[string]any
	waitFor(t, func() bool {
		got := mcpResult(e, e.key, "list_webhook_deliveries", map[string]any{"webhook_id": whID, "status": "failed"})
		ds, _ := got["deliveries"].([]any)
		if len(ds) == 1 {
			dlv = ds[0].(map[string]any)
		}
		return dlv != nil
	})
	if dlv["event_type"] != "collection.created" || dlv["last_status_code"] != float64(500) {
		t.Fatalf("failed delivery: %v", dlv)
	}
	if got := mcpResult(e, e.key, "list_webhook_deliveries", map[string]any{"status": "succeeded"}); len(got["deliveries"].([]any)) != 0 {
		t.Fatalf("status filter: %v", got)
	}
	fail.Store(false)
	mcpResult(e, e.key, "retry_webhook_delivery", map[string]any{"delivery_id": dlv["id"]})
	waitFor(t, func() bool {
		got := mcpResult(e, e.key, "list_webhook_deliveries", map[string]any{"webhook_id": whID, "status": "succeeded"})
		return len(got["deliveries"].([]any)) == 1
	})
	mcpErr(e, e.key, "retry_webhook_delivery", map[string]any{"delivery_id": "dlv_nope"}, "not found")
}

// Workspace administration through MCP needs admin; API keys can't touch
// sessions they don't own the scope for, and can't delete the workspace.
func TestMCPWorkspaceAdminTools(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	mcpErr(e, e.key, "create_api_key", map[string]any{"name": "x", "scopes": []string{"items:read"}}, "admin")
	mcpErr(e, e.key, "rename_workspace", map[string]any{"name": "x"}, "admin")
	mcpErr(e, e.key, "list_sessions", nil, "admin")
	mcpErr(e, e.key, "start_workspace_export", nil, "admin")
	mcpErr(e, e.key, "get_billing", nil, "admin")

	k := mcpResult(e, admin, "create_api_key", map[string]any{"name": "agent", "scopes": []string{"items:read"}})
	raw, kid := k["key"].(string), k["id"].(string)
	if raw == "" {
		t.Fatalf("create_api_key: %v", k)
	}
	mcpErr(e, admin, "create_api_key", map[string]any{"name": "x", "scopes": []string{"god:mode"}}, "scope")
	rot := mcpResult(e, admin, "rotate_api_key", map[string]any{"key_id": kid})
	if rot["key"] == raw || rot["key"] == "" {
		t.Fatalf("rotate_api_key: %v", rot)
	}
	e.do(nil, "GET", "/v1/collections", raw, nil, 401, nil)

	ss := mcpResult(e, admin, "list_sessions", nil)
	sessions, _ := ss["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("list_sessions: %v", ss)
	}
	sid := sessions[0].(map[string]any)["id"].(string)
	mcpResult(e, admin, "revoke_session", map[string]any{"session_id": sid})
	e.do(e.browser, "GET", "/v1/collections", "", nil, 401, nil)

	if got := mcpResult(e, admin, "rename_workspace", map[string]any{"name": "Renamed"}); got["name"] != "Renamed" {
		t.Fatalf("rename_workspace: %v", got)
	}
	mcpErr(e, admin, "rename_workspace", map[string]any{"name": "  "}, "name")
	if got := mcpResult(e, e.key, "get_workspace", nil); got["name"] != "Renamed" {
		t.Fatalf("get_workspace: %v", got)
	}
	mcpErr(e, admin, "delete_workspace", map[string]any{"confirm": "Renamed"}, "session")

	withDataDir(e)
	x := mcpResult(e, admin, "start_workspace_export", nil)
	xid := x["id"].(string)
	e.svc.ProcessExports(t.Context())
	got := mcpResult(e, admin, "get_workspace_export", map[string]any{"export_id": xid})
	if got["status"] != "ready" || !strings.HasPrefix(fmt.Sprint(got["download_url"]), "http://test/") {
		t.Fatalf("get_workspace_export: %v", got)
	}
	mcpErr(e, admin, "get_workspace_export", map[string]any{"export_id": "exp_nope"}, "not found")

	if plans := mcpResult(e, e.key, "list_plans", nil)["plans"].([]any); len(plans) < 2 {
		t.Fatalf("list_plans: %v", plans)
	}
	if b := mcpResult(e, admin, "get_billing", nil); b["enabled"] != false || b["plan"] != "pro" {
		t.Fatalf("get_billing: %v", b)
	}
	mcpErr(e, admin, "start_billing_checkout", nil, "not enabled")
	mcpErr(e, admin, "open_billing_portal", nil, "not enabled")
}
