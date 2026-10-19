package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The cloud plans as sold (Free / Pro $30); a change here is a pricing change.
func TestPlanLimitValues(t *testing.T) {
	const mb, gb = 1 << 20, 1 << 30
	want := map[string]app.PlanLimits{
		"free": {PostsPerMonth: 50, Channels: 3, Alerts: 2, Webhooks: 3, APIKeys: 1, StorageBytes: 1 * gb, MaxUploadBytes: 5 * mb},
		"pro": {PostsPerMonth: app.Unlimited, Channels: app.Unlimited, Alerts: app.Unlimited, Webhooks: app.Unlimited, APIKeys: app.Unlimited,
			StorageBytes: 100 * gb, MaxUploadBytes: 100 * mb, DataExport: true},
	}
	for id, limits := range want {
		p, ok := app.PlanByID(id)
		if !ok || p.Limits != limits {
			t.Errorf("plan %s: %+v, want %+v", id, p.Limits, limits)
		}
	}
	// Local installs: every count is unlimited (a zero would block everything), and exports are on.
	u := app.UnlockedPlan.Limits
	if !u.DataExport {
		t.Error("local installs must be able to export")
	}
	for name, v := range map[string]int64{"posts": u.PostsPerMonth, "channels": u.Channels, "alerts": u.Alerts, "webhooks": u.Webhooks,
		"api_keys": u.APIKeys, "storage": u.StorageBytes, "upload": u.MaxUploadBytes} {
		if v != app.Unlimited {
			t.Errorf("unlocked %s = %d, want unlimited", name, v)
		}
	}
	if p, _ := app.PlanByID("pro"); p.PriceUSDMonth != 30 {
		t.Errorf("pro price %d", p.PriceUSDMonth)
	}
	for _, old := range []string{"indie", "team", "business"} {
		if _, ok := app.PlanByID(old); ok {
			t.Errorf("old plan id %s still accepted", old)
		}
	}
}

// The per-plan file size limit is enforced by the API (402), below the
// operator's MAX_UPLOAD_MB.
func TestPlanUploadLimit(t *testing.T) {
	e := newEnv(t)
	e.svc.Cfg.MaxUploadBytes = 200 << 20
	file := func(mb int) []byte { return append(append([]byte{}, pngBytes...), make([]byte, mb<<20)...) }

	e.free()
	e.upload("/v1/media", e.key, nil, "big.png", file(6), 402, nil) // Free: 5 MB
	e.upload("/v1/media", e.key, nil, "ok.png", file(4), 201, nil)
	e.paid()
	e.upload("/v1/media", e.key, nil, "big.png", file(6), 201, nil) // Pro: 100 MB
	e.upload("/v1/media", e.key, nil, "huge.png", file(101), 402, nil)
}

// A new cloud workspace gets 7 days of Pro; unpaid afterwards it is read-only
// (reads and exports work) until it is paid or switched to Free.
func TestProTrialAndSuspension(t *testing.T) {
	e := newEnv(t)
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if u.Plan.ID != "pro" || u.PlanExpiresAt == nil || u.Suspended {
		t.Fatalf("trial: %+v", u)
	}
	ends, _ := time.Parse(time.RFC3339Nano, *u.PlanExpiresAt)
	if d := time.Until(ends); d < 6*24*time.Hour || d > 7*24*time.Hour {
		t.Fatalf("trial ends in %v", d)
	}
	c := e.collection("Trial")
	it := e.post(c.ID, map[string]any{"content": "during trial"})

	// The trial ends unpaid.
	e.svc.Now = func() time.Time { return ends.Add(time.Minute) }
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if !u.Suspended {
		t.Fatalf("not suspended after the trial: %+v", u)
	}
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "x"}, 402, &errBody)
	if errBody.Error.Code != "plan_expired" {
		t.Fatalf("error: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/posts/"+it.ID+"/publish", e.key, nil, 402, nil)
	e.do(nil, "GET", "/v1/posts/"+it.ID, e.key, nil, 200, nil) // reading still works
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, nil, "X-CSRF-Token", e.csrf)
	// MCP: read-only tools work, writing tools report plan_expired.
	if _, out := mcpCall(e, e.key, "list_collections", nil); isToolError(out) {
		t.Fatalf("read-only MCP tool refused while suspended: %v", out)
	}
	if _, out := mcpCall(e, e.key, "create_post", map[string]any{"collection": "Trial", "content": "x"}); !isToolError(out) ||
		!strings.Contains(fmt.Sprint(out), "read-only") {
		t.Fatalf("MCP write allowed while suspended: %v", out)
	}

	// Upgrading is not self-service yet; switching to Free lifts the suspension.
	e.do(e.browser, "POST", "/v1/workspace/plan", "", map[string]any{"plan": "pro"}, 402, nil, "X-CSRF-Token", e.csrf)
	e.do(e.browser, "POST", "/v1/workspace/plan", "", map[string]any{"plan": "free"}, 200, &u, "X-CSRF-Token", e.csrf)
	if u.Plan.ID != "free" || u.Suspended || u.PlanExpiresAt != nil {
		t.Fatalf("after switching to free: %+v", u)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "on free"}, 201, nil)
}

// The activity log export is a paid feature (402 on Free); the workspace export works on every plan, data only
// (no media files) on Free and full on Pro (and the trial).
func TestDataExportNeedsPaidPlan(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	csrf := []string{"X-CSRF-Token", e.csrf}
	e.free()
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, csrf...)
	if job.IncludesMedia {
		t.Fatalf("Free export includes media: %+v", job)
	}
	e.do(nil, "GET", "/v1/events/export", e.key, nil, 402, nil)
	e.svc.ProcessExports(context.Background())
	e.paid()
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, csrf...)
	if !job.IncludesMedia {
		t.Fatalf("Pro export without media: %+v", job)
	}
	e.do(nil, "GET", "/v1/events/export", e.key, nil, 200, nil)
}
