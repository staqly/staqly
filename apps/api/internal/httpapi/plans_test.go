package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The cloud plans as sold, per user (Starter $15, Plus $29, Pro $59, Business $99; yearly $12, $23, $47, $79 a month);
// a change here is a pricing change. No plan caps the seats: every member is billed as one.
func TestPlanLimitValues(t *testing.T) {
	const mb, gb = 1 << 20, 1 << 30
	u := int64(app.Unlimited)
	want := map[string]app.PlanLimits{
		"starter":  {PostsPerMonth: u, Channels: 5, Alerts: 3, Webhooks: 3, APIKeys: 2, StorageBytes: 10 * gb, MaxUploadBytes: 25 * mb, XPostsPerPeriod: 250},
		"plus":     {PostsPerMonth: u, Channels: 15, Alerts: 10, Webhooks: 10, APIKeys: 10, StorageBytes: 50 * gb, MaxUploadBytes: 100 * mb, DataExport: true, XPostsPerPeriod: 450},
		"pro":      {PostsPerMonth: u, Channels: 40, Alerts: u, Webhooks: u, APIKeys: u, StorageBytes: 200 * gb, MaxUploadBytes: 250 * mb, DataExport: true, XPostsPerPeriod: 950},
		"business": {PostsPerMonth: u, Channels: u, Alerts: u, Webhooks: u, APIKeys: u, StorageBytes: 1024 * gb, MaxUploadBytes: 1024 * mb, DataExport: true, XPostsPerPeriod: 1600},
	}
	prices := map[string][2]int{"starter": {15, 144}, "plus": {29, 276}, "pro": {59, 564}, "business": {99, 948}}
	for id, pr := range prices {
		p, _ := app.PlanByID(id)
		if p.PriceMonth != pr[0] || p.PriceYear != pr[1] {
			t.Errorf("%s prices %d / %v, want %v", id, p.PriceMonth, p.PriceYear, pr)
		}
	}
	for id, limits := range want {
		p, ok := app.PlanByID(id)
		if !ok || p.Limits != limits {
			t.Errorf("plan %s: %+v, want %+v", id, p.Limits, limits)
		}
	}
	// Local installs: every count is unlimited (a zero would block everything), and exports are on.
	ul := app.UnlockedPlan.Limits
	if !ul.DataExport {
		t.Error("local installs must be able to export")
	}
	for name, v := range map[string]int64{"posts": ul.PostsPerMonth, "channels": ul.Channels, "alerts": ul.Alerts, "webhooks": ul.Webhooks,
		"api_keys": ul.APIKeys, "storage": ul.StorageBytes, "upload": ul.MaxUploadBytes, "x_posts": ul.XPostsPerPeriod} {
		if v != app.Unlimited {
			t.Errorf("unlocked %s = %d, want unlimited", name, v)
		}
	}
	for _, old := range []string{"indie", "team", "free"} {
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
	e.upload("/v1/media", e.key, nil, "big.png", file(6), 402, nil) // the test plan: 5 MB
	e.upload("/v1/media", e.key, nil, "ok.png", file(4), 201, nil)
	e.plan("plus")
	e.upload("/v1/media", e.key, nil, "big.png", file(6), 201, nil) // Plus: 100 MB
	e.upload("/v1/media", e.key, nil, "huge.png", file(101), 402, nil)
}

// A new cloud workspace gets a 7-day Starter trial; unpaid afterwards it is read-only
// (reads and exports work) until a plan is paid for.
func TestProTrialAndSuspension(t *testing.T) {
	e := newEnv(t)
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if u.Plan.ID != e.svc.Cfg.TrialPlan || u.PlanExpiresAt == nil || u.Suspended { // the test server's TRIAL_PLAN
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

	// There is no free plan to fall back to: paying (or the operator) lifts the suspension.
	e.do(e.browser, "POST", "/v1/workspace/plan", "", map[string]any{"plan": "free"}, 404, nil, "X-CSRF-Token", e.csrf)
	e.do(e.browser, "POST", "/v1/billing/checkout", "", map[string]any{"plan": "starter"}, 409, nil, "X-CSRF-Token", e.csrf) // billing is off here
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if err := e.svc.SetPlan(context.Background(), ws.ID, "starter", ""); err != nil {
		t.Fatal(err)
	}
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if u.Plan.ID != "starter" || u.Suspended || u.PlanExpiresAt != nil {
		t.Fatalf("after the operator set Starter: %+v", u)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "on starter"}, 201, nil)
}

// The activity log export needs Plus or above (402 below); the workspace export works on every plan, data only
// (no media files) without it and full with it.
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

// With billing on, the plan list shows what Stripe charges: each plan's price (per seat) as found by its lookup
// key, cached; a price Stripe doesn't have, or a Stripe that fails, falls back to the built-in amount.
func TestPlansShowStripeAmounts(t *testing.T) {
	e := newEnv(t)
	var plans struct{ Data []app.Plan }
	price := func(id string) app.Plan {
		for _, p := range plans.Data {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("no plan %s", id)
		return app.Plan{}
	}
	e.do(nil, "GET", "/v1/plans", e.key, nil, 200, &plans)
	if p := price("plus"); p.PriceMonth != 29 || p.PriceYear != 276 {
		t.Fatalf("billing off: the built-in amounts, got %+v", p)
	}

	lookups := 0
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("lookup_keys[]")
		lookups++
		switch key {
		case "staqly_plus_month":
			fmt.Fprint(w, `{"data":[{"id":"price_pm","unit_amount":3100}]}`)
		case "staqly_plus_year":
			fmt.Fprint(w, `{"data":[{"id":"price_py","unit_amount":29700}]}`)
		case "staqly_pro_month":
			w.WriteHeader(500) // Stripe failing: the built-in price
		case "staqly_business_month":
			fmt.Fprint(w, `{"data":[{"id":"price_usd","unit_amount":12000,"currency":"usd"}]}`) // another currency: built-in
		case "staqly_pro_year":
			fmt.Fprint(w, `{"data":[{"id":"price_bad","unit_amount":4750}]}`) // not whole dollars: the built-in price
		default:
			fmt.Fprint(w, `{"data":[]}`) // not set up: the built-in price
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_plans", WebhookSecret: testWhsec, APIBase: stripe.URL}
	e.do(nil, "GET", "/v1/plans", e.key, nil, 200, &plans)
	if p := price("plus"); p.PriceMonth != 31 || p.PriceYear != 297 {
		t.Fatalf("plus from Stripe: %+v", p)
	}
	if p := price("pro"); p.PriceMonth != 59 || p.PriceYear != 564 {
		t.Fatalf("pro falls back: %+v", p)
	}
	if p := price("business"); p.PriceMonth != 99 || p.Currency != "eur" {
		t.Fatalf("a price in another currency is not shown: %+v", p)
	}
	if p := price("starter"); p.PriceMonth != 15 {
		t.Fatalf("starter falls back: %+v", p)
	}
	n := lookups
	e.do(nil, "GET", "/v1/plans", e.key, nil, 200, &plans)
	if lookups != n {
		t.Fatalf("found prices are cached: %d lookups, then %d", n, lookups)
	}

	// A canceled request (a page left while it loaded) leaves no failure behind for the next caller.
	e.svc.Cfg.Billing.SecretKey = "sk_test_canceled" // a fresh cache key
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	e.svc.LivePlans(canceled)
	e.do(nil, "GET", "/v1/plans", e.key, nil, 200, &plans)
	if p := price("plus"); p.PriceMonth != 31 {
		t.Fatalf("after a canceled lookup the next one asks Stripe again: %+v", p)
	}
}

// The landing page lists the same prices as the server (apps/web/lib/plans.ts mirrors Plans).
func TestWebsitePricesMatchPlans(t *testing.T) {
	src, err := os.ReadFile("../../../web/lib/plans.ts")
	if err != nil {
		t.Skip("apps/web is not in this checkout:", err)
	}
	re := regexp.MustCompile(`id: '([a-z]+)',\s*name: '[^']*',\s*month: (\d+),\s*year: (\d+),`)
	found := re.FindAllStringSubmatch(string(src), -1)
	if len(found) != len(app.OfferedPlans()) {
		t.Fatalf("apps/web/lib/plans.ts lists %d plans, the server offers %d", len(found), len(app.OfferedPlans()))
	}
	for _, m := range found {
		p, ok := app.PlanByID(m[1])
		if !ok || fmt.Sprint(p.PriceMonth) != m[2] || fmt.Sprint(p.PriceYear) != m[3] {
			t.Errorf("apps/web/lib/plans.ts %s: $%s / $%s, plans.go: $%d / $%d", m[1], m[2], m[3], p.PriceMonth, p.PriceYear)
		}
	}
	// The X post bundles and the included posts per plan.
	bundles := regexp.MustCompile(`\{ posts: (\d+), price: (\d+) \}`).FindAllStringSubmatch(string(src), -1)
	if len(bundles) != len(app.XBundles) {
		t.Fatalf("apps/web/lib/plans.ts lists %d X bundles, credits.go %d", len(bundles), len(app.XBundles))
	}
	for i, b := range app.XBundles {
		if bundles[i][1] != fmt.Sprint(b.Posts) || bundles[i][2] != fmt.Sprint(b.Price) {
			t.Errorf("X bundle %d: web %v, credits.go %+v", i, bundles[i][1:], b)
		}
	}
	for _, p := range app.OfferedPlans() {
		n := strings.ReplaceAll(fmt.Sprintf("%d", p.Limits.XPostsPerPeriod), "1600", "1,600")
		if !strings.Contains(string(src), "'"+n+" posts to X a month included'") {
			t.Errorf("apps/web/lib/plans.ts: %s should include %s posts to X a month", p.ID, n)
		}
	}
}
