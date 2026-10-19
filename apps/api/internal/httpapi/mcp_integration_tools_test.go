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
	mcpErr(e, e.key, "create_api_key", map[string]any{"name": "x", "scopes": []string{"posts:read"}}, "admin")
	mcpErr(e, e.key, "rename_workspace", map[string]any{"name": "x"}, "admin")
	mcpErr(e, e.key, "list_sessions", nil, "admin")
	mcpErr(e, e.key, "start_workspace_export", nil, "admin")
	mcpErr(e, e.key, "get_billing", nil, "admin")

	k := mcpResult(e, admin, "create_api_key", map[string]any{"name": "agent", "scopes": []string{"posts:read"}})
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
