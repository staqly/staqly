package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The webhook endpoint's API version doesn't matter: a subscription event's object is only used to find the
// subscription, which is read back from Stripe (in the account's current shape), and a bundle's Checkout session
// is read through fields every version since 2020 carries. The same events delivered in an older shape (API
// 2024-06-20: periods on the subscription, one `discount`, no periods on the items) apply exactly as current ones.
func TestWebhookPayloadVersionDoesNotMatter(t *testing.T) {
	start := time.Now().Add(-5 * 24 * time.Hour).Truncate(time.Second).Unix()
	end := time.Now().Add(25 * 24 * time.Hour).Truncate(time.Second).Unix()
	current := func(ws string) map[string]any {
		return map[string]any{"id": "sub_1", "object": "subscription", "customer": "cus_1", "status": "active",
			"metadata": map[string]string{"workspace_id": ws}, "discounts": []any{},
			"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": 1, "current_period_start": start,
				"current_period_end": end, "price": stripePriceObject("pro", "month")}}}}
	}
	older := func(ws string) map[string]any {
		price := stripePriceObject("pro", "month")
		legacyPlan := map[string]any{"id": price["id"], "object": "plan", "interval": "month"}
		return map[string]any{"id": "sub_1", "object": "subscription", "customer": "cus_1", "status": "active", "plan": legacyPlan,
			"quantity": 1, "metadata": map[string]string{"workspace_id": ws}, "current_period_start": start, "current_period_end": end,
			"discount": map[string]any{"object": "discount", "coupon": map[string]any{"id": "co_1", "percent_off": 10}},
			"items":    map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": 1, "plan": legacyPlan, "price": price}}}}
	}
	session := func(ws string, old bool) map[string]any {
		s := map[string]any{"id": "cs_bundle", "object": "checkout.session", "mode": "payment", "payment_status": "paid",
			"status": "complete", "customer": "cus_1", "client_reference_id": ws, "amount_total": 900, "currency": "chf",
			"metadata": map[string]string{"workspace_id": ws, "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": "200"}}
		if old {
			s["shipping"] = nil // shipping_details and collected_information came later
			s["total_details"] = map[string]any{"amount_discount": 0, "amount_tax": 0}
		} else {
			s["collected_information"] = map[string]any{"shipping_details": nil}
			s["discounts"] = []any{}
		}
		return s
	}
	type result struct {
		billing app.Billing
		units   int64
		expires string
	}
	apply := func(old bool) result {
		e := newEnv(t)
		f := withStripe(t, e)
		ws := workspaceID(e)
		admin := e.keyWith(app.ScopeAdmin)
		e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // makes cus_1
		f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, current(ws) })
		sub := current(ws)
		if old {
			sub = older(ws)
		}
		// Posted without handing the object to the fake Stripe (stripeEvent would): its read answers the current shape.
		for i, ev := range []struct {
			typ    string
			object map[string]any
		}{{"customer.subscription.updated", sub}, {"checkout.session.completed", session(ws, old)}} {
			if code := e.signedStripe(stripeBody(fmt.Sprint("evt_", i), ev.typ, ev.object), time.Now()); code != 200 {
				t.Fatalf("%s: %d", ev.typ, code)
			}
		}
		if f.called("GET /v1/subscriptions/sub_1") == 0 {
			t.Fatal("the subscription was not read back from Stripe")
		}
		r := result{billing: e.billing(admin)}
		if err := e.svc.DB.QueryRowContext(context.Background(), `SELECT units, expires_at FROM credit_grants WHERE workspace_id = ?`, ws).
			Scan(&r.units, &r.expires); err != nil {
			t.Fatalf("bundle not granted: %v", err)
		}
		return r
	}
	now, then := apply(false), apply(true)
	b := now.billing
	if b.Plan != "pro" || b.Suspended || deref(b.SubscriptionStatus) != "active" || b.Seats != 1 || b.PlanExpiresAt == nil ||
		now.units != 200 || now.expires[:19] != time.Unix(end, 0).UTC().Format("2006-01-02T15:04:05") {
		t.Fatalf("current shape: %+v, bundle %d until %s", b, now.units, now.expires)
	}
	o := then.billing
	if o.Plan != b.Plan || deref(o.PlanExpiresAt) != deref(b.PlanExpiresAt) || o.Seats != b.Seats || o.Suspended != b.Suspended ||
		deref(o.SubscriptionStatus) != deref(b.SubscriptionStatus) || then.units != now.units || then.expires != now.expires {
		t.Fatalf("older shape applied differently:\n%+v, bundle %d until %s\nwant\n%+v, bundle %d until %s", o, then.units, then.expires, b, now.units, now.expires)
	}
}
