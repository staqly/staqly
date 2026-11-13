package adminapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The operator's plan for a paying workspace, through the admin panel's API: checked, audited, kept while it
// runs (also over a sync of the same subscription), and at its end date the plan the customer pays for is back
// at once, not read-only until the next reconcile round. Sync and cancel reach Stripe for that workspace only.
func TestAdminPlanOverrideOfAPayingWorkspace(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.signIn()
	ws := e.workspace()
	ctx := context.Background()
	var mu sync.Mutex
	var cancelled []string
	period := time.Now().Add(20 * 24 * time.Hour).Unix()
	sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": 1, "current_period_end": period,
			"price": map[string]any{"id": "price_starter_month", "lookup_key": app.PriceLookupKey("starter", "month"), "recurring": map[string]any{"interval": "month"}}}}}}
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/subscriptions":
			// Like Stripe: a customer's subscriptions, the cancelled ones only when asked for all.
			data := []any{}
			if r.URL.Query().Get("customer") == "cus_1" && (sub["status"] != "canceled" || r.URL.Query().Get("status") == "all") {
				data = append(data, sub)
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "GET /v1/checkout/sessions", "GET /v1/entitlements/active_entitlements", "GET /v1/entitlements/features":
			fmt.Fprint(w, `{"data":[]}`)
		case "DELETE /v1/subscriptions/sub_1":
			cancelled = append(cancelled, "sub_1")
			sub["status"] = "canceled"
			json.NewEncoder(w).Encode(sub)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"code":"resource_missing","type":"invalid_request_error"}}`)
		}
	}))
	t.Cleanup(stripe.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_admin", WebhookSecret: "whsec", APIBase: stripe.URL}
	plan := func() (string, bool) {
		t.Helper()
		b, err := e.svc.GetBilling(ctx, app.Principal{WorkspaceID: ws, Scopes: []string{app.ScopeAdmin}})
		if err != nil {
			t.Fatal(err)
		}
		return b.Plan, b.Suspended
	}

	// No Stripe customer yet: nothing to sync (409), and Stripe isn't asked.
	if w := e.do("POST", "/v1/admin/workspaces/"+ws+"/billing/sync", map[string]string{}); w.Code != 409 {
		t.Fatalf("sync without a customer: %d %s", w.Code, w.Body)
	}
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET stripe_customer_id = 'cus_1' WHERE id = ?`, ws); err != nil {
		t.Fatal(err)
	}
	e.want(e.do("POST", "/v1/admin/workspaces/"+ws+"/billing/sync", map[string]string{}), 204)
	if p, suspended := plan(); p != "starter" || suspended {
		t.Fatalf("after the sync: %s, suspended %v", p, suspended)
	}

	// The plan is checked before anything changes.
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	for _, c := range []struct {
		ws   string
		body map[string]string
		want int
	}{
		{ws, map[string]string{"plan": "gold"}, 422},
		{ws, map[string]string{"plan": "unlocked"}, 422},
		{ws, map[string]string{"plan": "business", "until": "next week"}, 422},
		{"ws_nope", map[string]string{"plan": "business"}, 404},
	} {
		if w := e.do("POST", "/v1/admin/workspaces/"+c.ws+"/plan", c.body); w.Code != c.want {
			t.Fatalf("plan %v on %s: %d, want %d: %s", c.body, c.ws, w.Code, c.want, w.Body)
		}
	}
	if p, _ := plan(); p != "starter" {
		t.Fatalf("a refused plan changed the workspace: %s", p)
	}
	e.want(e.do("POST", "/v1/admin/workspaces/"+ws+"/plan", map[string]string{"plan": "business", "until": until.Format(time.RFC3339)}), 204)
	e.svc.RunDueJobsOnce(ctx)
	// Told again by a sync of the same paid subscription: the operator's plan stands.
	e.want(e.do("POST", "/v1/admin/workspaces/"+ws+"/billing/sync", map[string]string{}), 204)
	if p, suspended := plan(); p != "business" || suspended {
		t.Fatalf("while the operator's plan runs: %s, suspended %v", p, suspended)
	}
	// Its last second, then its end: the paid plan is back with the job queued for that moment.
	e.svc.Now = func() time.Time { return until.Add(-time.Second) }
	t.Cleanup(func() { e.svc.Now = time.Now })
	e.svc.RunDueJobsOnce(ctx)
	if p, suspended := plan(); p != "business" || suspended {
		t.Fatalf("a second before the end: %s, suspended %v", p, suspended)
	}
	e.svc.Now = func() time.Time { return until }
	e.svc.RunDueJobsOnce(ctx)
	if p, suspended := plan(); p != "starter" || suspended {
		t.Fatalf("at the end of the operator's plan: %s, suspended %v; want the paid starter, not suspended", p, suspended)
	}
	e.svc.Now = time.Now

	// Cancel: every subscription of this workspace's customer, at Stripe.
	e.want(e.do("POST", "/v1/admin/workspaces/"+ws+"/billing/cancel", map[string]string{}), 204)
	if w := e.do("POST", "/v1/admin/workspaces/ws_nope/billing/cancel", map[string]string{}); w.Code != 404 {
		t.Fatalf("cancel of an unknown workspace: %d", w.Code)
	}
	mu.Lock()
	got := strings.Join(cancelled, ",")
	mu.Unlock()
	if got != "sub_1" {
		t.Fatalf("cancelled at Stripe: %q, want sub_1 once", got)
	}
	// The cancelled subscription is found by the next sync (Stripe lists it only when asked for all).
	e.want(e.do("POST", "/v1/admin/workspaces/"+ws+"/billing/sync", map[string]string{}), 204)
	if p, suspended := plan(); p != "starter" || !suspended {
		t.Fatalf("after the cancellation: %s, suspended %v", p, suspended)
	}
	w := e.do("GET", "/v1/admin/audit?workspace_id="+ws, nil)
	e.want(w, 200)
	for _, action := range []string{"sync_billing", "set_plan", "cancel_subscription"} {
		if !strings.Contains(w.Body.String(), action) {
			t.Fatalf("the audit log has no %s: %s", action, w.Body)
		}
	}
	if n := strings.Count(w.Body.String(), `"set_plan"`); n != 1 {
		t.Fatalf("%d set_plan audit rows, want 1 (refused requests leave none)", n)
	}
}
