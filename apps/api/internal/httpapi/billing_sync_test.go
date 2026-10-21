package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// No webhook ever arrives: the workspace still opens, once the dashboard asks for a sync after Checkout and,
// without the dashboard, on the background reconcile.
func TestBillingSyncWithoutWebhook(t *testing.T) {
	e := newAccountsEnv(t, "")
	var subs []map[string]any
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":1500,"currency":"eur"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_s"}`)
		case "POST /v1/checkout/sessions":
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/s"}`)
		case "GET /v1/subscriptions":
			if r.URL.Query().Get("customer") != "cus_s" || r.URL.Query().Get("status") != "all" {
				t.Errorf("subscriptions query = %v", r.URL.Query())
			}
			json.NewEncoder(w).Encode(map[string]any{"data": subs})
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_sync", WebhookSecret: testWhsec, APIBase: stripe.URL}
	owner := e.mustSignIn(ana, "")
	ws := e.session(owner).Workspace.ID
	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "month"}, 200, nil)

	// Back from Checkout before Stripe has a subscription: still locked, nothing breaks.
	var b app.Billing
	e.as(owner, "POST", "/v1/billing/sync", nil, 200, &b)
	if !b.Suspended {
		t.Fatalf("no subscription yet: still locked, got %+v", b)
	}

	// The checkout went through, but its webhook never comes: the background reconcile opens the workspace.
	end := time.Now().Add(7 * 24 * time.Hour).Unix()
	subs = []map[string]any{{"id": "sub_s", "customer": "cus_s", "status": "trialing", "metadata": map[string]string{"workspace_id": ws},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_s", "quantity": 1, "current_period_end": end,
			"price": stripePriceObject("starter", "month")}}}}}
	if n := e.svc.ReconcileBilling(t.Context()); n != 1 {
		t.Fatalf("reconcile synced %d workspaces, want 1", n)
	}
	e.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if b.Suspended || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "trialing" {
		t.Fatalf("after the reconcile: %+v", b)
	}

	// The dashboard's own sync answers with the same state; nothing is left to reconcile.
	e.as(owner, "POST", "/v1/billing/sync", nil, 200, &b)
	if b.Suspended || b.Plan != "starter" {
		t.Fatalf("sync: %+v", b)
	}
	if n := e.svc.ReconcileBilling(t.Context()); n != 0 {
		t.Fatalf("a workspace with a live subscription is not reconciled again, synced %d", n)
	}
}
