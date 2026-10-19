package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Stripe Entitlements: what a customer's subscription grants reaches the workspace through the sync job
// (queued by subscription events and the entitlement summary event), and every limit check uses the result.
// Add-ons join and leave the subscription as items of their own.
func TestEntitlementsAndAddons(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	ctx := context.Background()
	admin := e.keyWith(app.AllScopes...) // (keys make the workspace paid: Starter comes after)
	reader := e.keyWith(app.ScopePostsRead)
	if err := e.svc.SetPlan(ctx, ws.ID, "starter", ""); err != nil {
		t.Fatal(err)
	}

	// Without billing, no features apply: Starter's 3 webhooks.
	hook := func(code int) {
		t.Helper()
		e.do(nil, "POST", "/v1/webhooks", admin, map[string]any{"url": "http://127.0.0.1:9/h", "events": []string{"post.*"}}, code, nil)
	}
	for range 3 {
		hook(201)
	}
	hook(402)

	var mu sync.Mutex
	active := []map[string]any{} // what Stripe grants cus_1 now
	items := []any{map[string]any{"id": "si_plan", "quantity": 1, "price": stripePriceObject("starter", "month")}}
	var calls []string
	var added url.Values
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		r.ParseForm()
		calls = append(calls, r.Method+" "+r.URL.Path)
		enc := json.NewEncoder(w)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "GET /v1/prices":
			key := r.URL.Query().Get("lookup_keys[]")
			if strings.HasPrefix(key, "staqly_addon_draft") {
				fmt.Fprint(w, `{"data":[]}`)
				return
			}
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":900,"currency":"eur"}]}`, strings.TrimPrefix(key, "staqly_"))
		case "POST /v1/checkout/sessions":
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/entitlements/active_entitlements":
			if r.URL.Query().Get("customer") != "cus_1" {
				t.Errorf("entitlements of %q", r.URL.Query().Get("customer"))
			}
			enc.Encode(map[string]any{"data": active})
		case "GET /v1/entitlements/features":
			enc.Encode(map[string]any{"data": []any{
				map[string]any{"id": "feat_ch", "name": "Channels pack", "lookup_key": "webhooks_extra_2", "metadata": map[string]string{"staqly_limit_webhooks": "+2"}},
				map[string]any{"id": "feat_ai", "name": "AI pack", "lookup_key": "ai_pack"},
			}})
		case "GET /v1/products":
			enc.Encode(map[string]any{"data": []any{
				map[string]any{"name": "Starter", "metadata": map[string]string{"staqly_kind": "plan"}},
				map[string]any{"name": "Webhooks pack", "description": "Two more webhooks", "metadata": map[string]string{"staqly_kind": "addon", "staqly_addon": "webhooks_pack"},
					"marketing_features": []any{map[string]any{"name": "+2 webhooks"}}},
				map[string]any{"name": "Draft pack", "metadata": map[string]string{"staqly_kind": "addon", "staqly_addon": "draft"}},
			}})
		case "GET /v1/subscriptions/sub_1":
			enc.Encode(map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "items": map[string]any{"data": items}})
		case "POST /v1/subscription_items":
			added = r.Form
			items = append(items, map[string]any{"id": "si_addon", "quantity": 1, "price": map[string]any{"id": r.Form.Get("price"),
				"lookup_key": "staqly_addon_webhooks_pack_month", "metadata": map[string]string{"staqly_addon": "webhooks_pack"}, "recurring": map[string]any{"interval": "month"}}})
			fmt.Fprint(w, `{"id":"si_addon"}`)
		case "DELETE /v1/subscription_items/si_addon":
			if r.URL.Query().Get("proration_behavior") != "create_prorations" {
				t.Errorf("removal without a credit: %v", r.URL.Query())
			}
			items = items[:1]
			fmt.Fprint(w, `{"id":"si_addon","deleted":true}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"fake: unhandled"}}`)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}

	// Add-ons come with a plan: none to add before a subscription.
	e.do(nil, "POST", "/v1/billing/addons/webhooks_pack", admin, nil, 409, nil)
	// A checkout makes the customer; the subscription event ties it to the workspace and queues the sync.
	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "starter", "interval": "month"}, 200, &out)
	sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws.ID},
		"items": map[string]any{"data": items}}
	sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["current_period_end"] = time.Now().Add(30 * 24 * time.Hour).Unix()
	if code := e.stripeEvent("evt_sub", "customer.subscription.updated", sub, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	e.svc.RunDueJobsOnce(ctx)
	var u app.Usage
	usage := func() { u = app.Usage{}; e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u) }
	usage()
	if u.Plan.ID != "starter" || u.Plan.Limits.Webhooks != 3 || len(u.Plan.Entitlements) != 0 || u.Plan.BaseLimits != nil {
		t.Fatalf("no features yet: %+v", u.Plan)
	}

	// The add-ons on sale: drafts and products without a price aren't offered.
	var list struct{ Data []app.Addon }
	e.do(nil, "GET", "/v1/billing/addons", admin, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != "webhooks_pack" || list.Data[0].PriceMonth != 9 || list.Data[0].Active {
		t.Fatalf("add-ons: %+v", list.Data)
	}
	e.do(nil, "GET", "/v1/billing/addons", reader, nil, 403, nil)

	// Add it: one more item on the subscription in its interval, charged at once.
	e.do(nil, "POST", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	if added.Get("subscription") != "sub_1" || added.Get("price") != "price_addon_webhooks_pack_month" ||
		added.Get("proration_behavior") != "always_invoice" || added.Get("quantity") != "1" {
		t.Fatalf("add-on item: %v", added)
	}
	e.do(nil, "POST", "/v1/billing/addons/webhooks_pack", admin, nil, 409, nil) // already there
	// Stripe reports the new features; the event is applied once and queues the sync.
	mu.Lock()
	active = []map[string]any{{"id": "ent_1", "feature": "feat_ch", "lookup_key": "webhooks_extra_2"}, {"id": "ent_2", "feature": "feat_ai", "lookup_key": "ai_pack"}}
	mu.Unlock()
	summary := map[string]any{"object": "entitlements.active_entitlement_summary", "customer": "cus_1"}
	for range 2 {
		if code := e.stripeEvent("evt_ent_1", "entitlements.active_entitlement_summary.updated", summary, testWhsec); code != 200 {
			t.Fatalf("summary event: %d", code)
		}
	}
	e.svc.RunDueJobsOnce(ctx)
	usage()
	if u.Plan.Limits.Webhooks != 5 || u.Plan.BaseLimits == nil || u.Plan.BaseLimits.Webhooks != 3 ||
		fmt.Sprint(u.Plan.Entitlements) != "[ai_pack webhooks_extra_2]" || len(u.Plan.LimitBonuses) != 1 ||
		u.Plan.LimitBonuses[0].From != "Channels pack" || u.Plan.LimitBonuses[0].Add != 2 {
		t.Fatalf("effective plan: %+v", u.Plan)
	}
	// The limit check itself uses it: two more webhooks, then 402.
	hook(201)
	hook(201)
	hook(402)
	e.do(nil, "GET", "/v1/billing/addons", admin, nil, 200, &list)
	if !list.Data[0].Active {
		t.Fatalf("the add-on is active: %+v", list.Data)
	}

	// Remove it: the item leaves (the rest credited); Stripe drops the features; back to Starter's limits.
	e.do(nil, "DELETE", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	e.do(nil, "DELETE", "/v1/billing/addons/webhooks_pack", admin, nil, 404, nil)
	mu.Lock()
	active = []map[string]any{}
	mu.Unlock()
	e.stripeEvent("evt_ent_2", "entitlements.active_entitlement_summary.updated", summary, testWhsec)
	e.svc.RunDueJobsOnce(ctx)
	usage()
	if u.Plan.Limits.Webhooks != 3 || len(u.Plan.Entitlements) != 0 {
		t.Fatalf("after removal: %+v", u.Plan)
	}

	// Features are read once per 10 minutes (one list for both syncs that needed them).
	mu.Lock()
	if n := strings.Count(strings.Join(calls, ","), "GET /v1/entitlements/features"); n != 1 {
		t.Errorf("feature lists = %d: %v", n, calls)
	}
	mu.Unlock()

	// Billing turned off: stored features no longer count.
	mu.Lock()
	active = []map[string]any{{"id": "ent_1", "feature": "feat_ch", "lookup_key": "webhooks_extra_2"}}
	mu.Unlock()
	e.stripeEvent("evt_ent_3", "entitlements.active_entitlement_summary.updated", summary, testWhsec)
	e.svc.RunDueJobsOnce(ctx)
	e.svc.Cfg.Billing = app.BillingConfig{}
	usage()
	if u.Plan.Limits.Webhooks != 3 || len(u.Plan.Entitlements) != 0 {
		t.Fatalf("billing off: %+v", u.Plan)
	}
	e.do(nil, "GET", "/v1/billing/addons", admin, nil, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("no add-ons without billing: %+v", list.Data)
	}
}
