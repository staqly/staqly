package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// rawStripeEvent delivers an event whose object is NOT handed to the fake Stripe as the subscription's state
// (stripeEvent does that): the payload and what Stripe has now can differ, as with a late or retried event.
func (e *env) rawStripeEvent(id, typ string, object any) int {
	e.t.Helper()
	return e.signedStripe(stripeBody(id, typ, object), e.svc.Now())
}

func planChanges(e *env, admin string) int {
	e.t.Helper()
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?type=workspace.plan_changed&limit=100", admin, nil, 200, &page)
	return len(page.Data)
}

// Every subscription event is applied from what Stripe has now, never from its payload (Stripe sends events out
// of order and retries them for days); an event id is applied once; an event about a subscription of no workspace
// here is acknowledged without asking Stripe anything.
func TestSubscriptionEventsDuplicateStaleAndUnknown(t *testing.T) {
	kinds := []string{"customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted",
		"customer.subscription.paused", "customer.subscription.resumed"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			f := withStripe(t, e)
			ws := workspaceID(e)
			admin := e.keyWith(app.ScopeAdmin)
			var mu sync.Mutex
			current := subObject("sub_1", ws, "active", "starter", "month") // what Stripe has
			f.set("GET /v1/subscriptions/sub_1", func() (int, any) {
				mu.Lock()
				defer mu.Unlock()
				return 200, current
			})
			f.set("GET /v1/invoices", func() (int, any) { return 200, map[string]any{"data": []any{}} })

			// The payload is older than Stripe's state (Business, past_due): Stripe's state wins.
			stale := subObject("sub_1", ws, "past_due", "business", "year")
			if code := e.rawStripeEvent("evt_1", kind, stale); code != 200 {
				t.Fatalf("event: %d", code)
			}
			b := e.billing(admin)
			if b.Plan != "starter" || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "active" || b.Suspended {
				t.Fatalf("the payload was applied instead of Stripe's state: %+v", b)
			}
			changes := planChanges(e, admin)

			// The same event id again, after the subscription moved on at Stripe: applied once, nothing changes.
			mu.Lock()
			current = subObject("sub_1", ws, "active", "business", "month")
			mu.Unlock()
			if code := e.rawStripeEvent("evt_1", kind, stale); code != 200 {
				t.Fatalf("duplicate: %d", code)
			}
			if b := e.billing(admin); b.Plan != "starter" || planChanges(e, admin) != changes {
				t.Fatalf("a duplicate delivery was applied again: %+v, %d plan changes (were %d)", b, planChanges(e, admin), changes)
			}
			// A new event id applies what Stripe has now.
			if code := e.rawStripeEvent("evt_2", kind, stale); code != 200 {
				t.Fatalf("next event: %d", code)
			}
			if b := e.billing(admin); b.Plan != "business" {
				t.Fatalf("the next event: %+v", b)
			}

			// An event sent before the cancellation, arriving after it: the subscription stays ended.
			mu.Lock()
			current = subObject("sub_1", ws, "canceled", "business", "month")
			mu.Unlock()
			if code := e.rawStripeEvent("evt_3", "customer.subscription.deleted", current); code != 200 {
				t.Fatalf("cancellation: %d", code)
			}
			if code := e.rawStripeEvent("evt_4", kind, subObject("sub_1", ws, "active", "business", "month")); code != 200 {
				t.Fatalf("late event: %d", code)
			}
			if b := e.billing(admin); !b.Suspended || *b.SubscriptionStatus != "canceled" {
				t.Fatalf("a late %s revived an ended subscription: %+v", kind, b)
			}

			// No workspace here: by the metadata (another instance on the same Stripe account, a deleted workspace)
			// and by the customer. Acknowledged, Stripe isn't asked, nothing changes.
			before, calls := e.billing(admin), len(f.calls)
			for i, stranger := range []map[string]any{
				subObject("sub_else", "ws_someone_else", "active", "business", "month"),
				{"id": "sub_else", "customer": "cus_nobody", "status": "active"},
			} {
				if code := e.rawStripeEvent(fmt.Sprint("evt_stranger_", i), kind, stranger); code != 200 {
					t.Fatalf("unknown workspace %d: %d", i, code)
				}
			}
			if after := e.billing(admin); len(f.calls) != calls || after.Plan != before.Plan || *after.SubscriptionStatus != *before.SubscriptionStatus {
				t.Fatalf("an event for no workspace here: Stripe asked %v, billing %+v", f.calls[calls:], after)
			}
		})
	}
}

// A subscription Checkout that finished: the session carries only the subscription's id, which is read from
// Stripe. Told twice it is applied once; for a workspace that isn't here it changes nothing.
func TestCheckoutCompletedDuplicateAndUnknown(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, subObject("sub_1", ws, "active", "plus", "month") })
	f.set("GET /v1/subscriptions/sub_else", func() (int, any) {
		return 200, subObject("sub_else", "ws_someone_else", "active", "business", "month")
	})
	session := map[string]any{"id": "cs_1", "mode": "subscription", "subscription": "sub_1", "customer": "cus_1"}
	for range 2 {
		if code := e.rawStripeEvent("evt_cs_1", "checkout.session.completed", session); code != 200 {
			t.Fatalf("checkout: %d", code)
		}
	}
	if b := e.billing(admin); b.Plan != "plus" || *b.SubscriptionStatus != "active" || planChanges(e, admin) != 2 { // keyWith's Pro, then Plus
		t.Fatalf("after the checkout told twice: %+v, %d plan changes", b, planChanges(e, admin))
	}
	stranger := map[string]any{"id": "cs_else", "mode": "subscription", "subscription": "sub_else", "customer": "cus_else"}
	if code := e.rawStripeEvent("evt_cs_else", "checkout.session.completed", stranger); code != 200 {
		t.Fatalf("someone else's checkout: %d", code)
	}
	// The delayed-payment event of a subscription checkout carries nothing staqly waits for: ignored, unread.
	calls := len(f.calls)
	if code := e.rawStripeEvent("evt_cs_async", "checkout.session.async_payment_succeeded", session); code != 200 || len(f.calls) != calls {
		t.Fatalf("async payment of a subscription checkout: %d, Stripe asked %v", code, f.calls[calls:])
	}
	if b := e.billing(admin); b.Plan != "plus" || recordedSubscription(e, ws) != "sub_1" {
		t.Fatalf("someone else's checkout changed this workspace: %+v", b)
	}
}

// creditStripe is a Stripe that knows the credit Checkout sessions and charges of a test and, like Stripe, lists
// only the sessions of the payment asked for.
type creditStripe struct {
	mu       sync.Mutex
	sessions []map[string]any
	charges  map[string]map[string]any
	lookups  int
}

func newCreditStripe(t *testing.T, e *env) *creditStripe {
	t.Helper()
	f := &creditStripe{charges: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/checkout/sessions":
			f.lookups++
			var data []any
			for _, s := range f.sessions {
				if pi := r.URL.Query().Get("payment_intent"); pi != "" && s["payment_intent"] == pi {
					data = append(data, s)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/charges/"):
			ch, ok := f.charges[strings.TrimPrefix(r.URL.Path, "/v1/charges/")]
			if !ok {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"code":"resource_missing","type":"invalid_request_error"}}`)
				return
			}
			json.NewEncoder(w).Encode(ch)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"fake: unhandled","type":"invalid_request_error"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_credit_events", WebhookSecret: testWhsec, APIBase: srv.URL}
	return f
}

// session makes (and remembers) a credits Checkout session of a workspace.
func (f *creditStripe) session(id, pi, ws, posts, paid string) map[string]any {
	s := map[string]any{"id": id, "mode": "payment", "payment_status": paid, "payment_intent": pi, "client_reference_id": ws,
		"metadata": map[string]string{"workspace_id": ws, "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": posts}}
	f.mu.Lock()
	f.sessions = append(f.sessions, s)
	f.mu.Unlock()
	return s
}

func paidSession(s map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range s {
		out[k] = v
	}
	out["payment_status"] = "paid"
	return out
}

// Bought X posts are granted by the paid Checkout session, once, whichever event brings it and in whichever
// order: a payment that is still on its way (a bank debit) grants nothing until Stripe says it succeeded; a
// session of another workspace, or of something else than credits, grants nothing here.
func TestCreditEventsPaidOnceInAnyOrder(t *testing.T) {
	e := newEnv(t)
	f := newCreditStripe(t, e)
	ws := workspaceID(e)
	left := func() int64 {
		t.Helper()
		var x app.XPosts
		e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
		return x.CreditsLeft
	}
	send := func(id, typ string, object any) {
		t.Helper()
		if code := e.rawStripeEvent(id, typ, object); code != 200 {
			t.Fatalf("%s %s: %d", typ, id, code)
		}
	}
	// A delayed payment: completed while unpaid, then the payment succeeds; each told twice.
	slow := f.session("cs_slow", "pi_slow", ws, "200", "unpaid")
	send("evt_slow_1", "checkout.session.completed", slow)
	send("evt_slow_1", "checkout.session.completed", slow)
	if n := left(); n != 0 {
		t.Fatalf("an unpaid checkout granted %d posts", n)
	}
	send("evt_slow_2", "checkout.session.async_payment_succeeded", paidSession(slow))
	send("evt_slow_2", "checkout.session.async_payment_succeeded", paidSession(slow))
	send("evt_slow_3", "checkout.session.async_payment_succeeded", paidSession(slow)) // another event, the same session
	if n := left(); n != 200 {
		t.Fatalf("after the payment succeeded: %d posts, want 200 once", n)
	}
	// The other order: the payment's success first, the (unpaid) completion late.
	late := f.session("cs_late", "pi_late", ws, "1000", "unpaid")
	send("evt_late_2", "checkout.session.async_payment_succeeded", paidSession(late))
	send("evt_late_1", "checkout.session.completed", late)
	if n := left(); n != 1200 {
		t.Fatalf("events out of order: %d posts, want 1200", n)
	}
	// Not for this instance, not credits, not paid, no posts: nothing.
	other := f.session("cs_other", "pi_other", "ws_someone_else", "4000", "paid")
	send("evt_other", "checkout.session.completed", other)
	send("evt_other_async", "checkout.session.async_payment_succeeded", other)
	send("evt_plain", "checkout.session.completed", map[string]any{"id": "cs_plain", "mode": "payment", "payment_status": "paid", "client_reference_id": ws})
	failed := f.session("cs_failed", "pi_failed", ws, "200", "unpaid")
	send("evt_failed", "checkout.session.async_payment_failed", failed)
	zero := f.session("cs_zero", "pi_zero", ws, "0", "paid")
	send("evt_zero", "checkout.session.completed", zero)
	if n := left(); n != 1200 {
		t.Fatalf("after events that grant nothing: %d posts, want 1200", n)
	}
	var grants, events int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM credit_grants`).Scan(&grants)
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type = 'workspace.credits_added'`).Scan(&events)
	if grants != 2 || events != 2 {
		t.Fatalf("%d grants and %d workspace.credits_added events, want 2 and 2", grants, events)
	}
}

// Refunds and disputes of bought posts: a dispute counts on top of what was refunded before; the same event
// again, an older total arriving late, a refund before the purchase's own event, and a charge of another
// workspace or of something that isn't credits all leave the right number of posts.
func TestCreditRefundEventsDuplicateStaleAndUnknown(t *testing.T) {
	e := newEnv(t)
	f := newCreditStripe(t, e)
	ws := workspaceID(e)
	left := func() int64 {
		t.Helper()
		var x app.XPosts
		e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
		return x.CreditsLeft
	}
	send := func(id, typ string, object any) {
		t.Helper()
		if code := e.rawStripeEvent(id, typ, object); code != 200 {
			t.Fatalf("%s %s: %d", typ, id, code)
		}
	}
	charge := func(id, pi string, amount, refunded int) map[string]any {
		ch := map[string]any{"id": id, "object": "charge", "payment_intent": pi, "amount": amount, "amount_refunded": refunded}
		f.mu.Lock()
		f.charges[id] = ch
		f.mu.Unlock()
		return ch
	}
	bought := f.session("cs_1", "pi_1", ws, "1000", "paid")
	send("evt_buy", "checkout.session.completed", bought)

	// A quarter refunded, then a dispute of half the charge: three quarters are gone, 250 stay.
	send("evt_ref_1", "charge.refunded", charge("ch_1", "pi_1", 4000, 1000))
	if n := left(); n != 750 {
		t.Fatalf("after a quarter's refund: %d posts, want 750", n)
	}
	dispute := map[string]any{"id": "dp_1", "object": "dispute", "charge": "ch_1", "amount": 2000}
	send("evt_dp_1", "charge.dispute.created", dispute)
	if n := left(); n != 250 {
		t.Fatalf("a dispute of half after a quarter's refund: %d posts, want 250", n)
	}
	// Told again, and the older refund arriving late under a new id: nothing comes back.
	send("evt_dp_1", "charge.dispute.created", dispute)
	send("evt_ref_1", "charge.refunded", charge("ch_1", "pi_1", 4000, 1000))
	send("evt_ref_late", "charge.refunded", map[string]any{"id": "ch_1", "object": "charge", "payment_intent": "pi_1", "amount": 4000, "amount_refunded": 500})
	if n := left(); n != 250 {
		t.Fatalf("a repeated or older event changed the posts: %d, want 250", n)
	}

	// A refund that arrives before the purchase's own event: the grant is made, lowered, and the purchase's
	// event later adds nothing.
	early := f.session("cs_2", "pi_2", ws, "200", "paid")
	send("evt_ref_2", "charge.refunded", charge("ch_2", "pi_2", 1000, 1000))
	send("evt_buy_2", "checkout.session.completed", early)
	if n := left(); n != 250 {
		t.Fatalf("a purchase refunded before its event arrived: %d posts, want 250", n)
	}

	// Another workspace's purchase, a payment that isn't credits (a subscription invoice), a dispute whose charge
	// isn't named: acknowledged, nothing changes here.
	f.session("cs_else", "pi_else", "ws_someone_else", "4000", "paid")
	send("evt_ref_else", "charge.refunded", charge("ch_else", "pi_else", 15000, 15000))
	send("evt_ref_invoice", "charge.refunded", charge("ch_inv", "pi_invoice", 1900, 1900))
	send("evt_dp_bare", "charge.dispute.created", map[string]any{"id": "dp_2", "object": "dispute", "amount": 4000})
	if n := left(); n != 250 {
		t.Fatalf("someone else's refund changed this workspace: %d posts, want 250", n)
	}
	var grants int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM credit_grants`).Scan(&grants)
	if grants != 2 {
		t.Fatalf("%d grants, want this workspace's 2", grants)
	}
	// Stripe can't be asked about a disputed charge: the event fails, so Stripe sends it again.
	if code := e.rawStripeEvent("evt_dp_gone", "charge.dispute.created", map[string]any{"id": "dp_3", "charge": "ch_missing", "amount": 100}); code != 502 {
		t.Fatalf("a dispute whose charge can't be read: %d, want 502", code)
	}
}

// Stripe's entitlement summary names a customer; the job reads what the customer has now, so the event's own
// content and order don't matter. Events told twice or in a burst run one sync; a customer of no workspace here
// queues nothing.
func TestEntitlementEventsDuplicateStaleAndUnknown(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ctx := context.Background()
	e.pay("POST", "/v1/billing/checkout", nil, 200, nil) // cus_1
	var mu sync.Mutex
	active := []any{}
	f.set("GET /v1/entitlements/active_entitlements", func() (int, any) {
		mu.Lock()
		defer mu.Unlock()
		return 200, map[string]any{"data": active}
	})
	f.set("GET /v1/entitlements/features", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]any{"id": "feat_wh_events", "name": "More webhooks", "lookup_key": "webhooks_events_test",
			"metadata": map[string]string{"staqly_limit_webhooks": "+2"}}}}
	})
	webhooks := func() int64 {
		t.Helper()
		var u app.Usage
		e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
		return u.Plan.Limits.Webhooks
	}
	base := webhooks()
	summary := func(customer string, features ...string) map[string]any {
		data := []any{}
		for _, f := range features {
			data = append(data, map[string]any{"feature": f})
		}
		return map[string]any{"object": "entitlements.active_entitlement_summary", "customer": customer, "entitlements": map[string]any{"data": data}}
	}
	const kind = "entitlements.active_entitlement_summary.updated"
	// The feature was granted and taken away again at Stripe; the grant's event arrives last, twice.
	if code := e.rawStripeEvent("evt_ent_removed", kind, summary("cus_1")); code != 200 {
		t.Fatalf("summary: %d", code)
	}
	for range 2 {
		if code := e.rawStripeEvent("evt_ent_granted", kind, summary("cus_1", "feat_wh_events")); code != 200 {
			t.Fatalf("summary: %d", code)
		}
	}
	e.svc.RunDueJobsOnce(ctx)
	if got, n := webhooks(), f.called("GET /v1/entitlements/active_entitlements"); got != base || n != 1 {
		t.Fatalf("a late event granted a feature Stripe no longer has: webhooks limit %d (plan %d), %d syncs for 3 deliveries", got, base, n)
	}
	// Granted at Stripe: the next event brings it, whatever the event says.
	mu.Lock()
	active = []any{map[string]any{"id": "ent_1", "feature": "feat_wh_events", "lookup_key": "webhooks_events_test"}}
	mu.Unlock()
	if code := e.rawStripeEvent("evt_ent_3", kind, summary("cus_1")); code != 200 {
		t.Fatalf("summary: %d", code)
	}
	e.svc.RunDueJobsOnce(ctx)
	if got := webhooks(); got != base+2 {
		t.Fatalf("after the grant: webhooks limit %d, want %d", got, base+2)
	}
	// A customer of no workspace here: acknowledged, no sync.
	calls := len(f.calls)
	if code := e.rawStripeEvent("evt_ent_else", kind, summary("cus_nobody", "feat_wh_events")); code != 200 {
		t.Fatalf("unknown customer: %d", code)
	}
	e.svc.RunDueJobsOnce(ctx)
	if len(f.calls) != calls || webhooks() != base+2 {
		t.Fatalf("a summary for an unknown customer: Stripe asked %v, webhooks limit %d", f.calls[calls:], webhooks())
	}
	if code := e.stripeDirect(stripeBody("evt_ent_bad", kind, map[string]any{"object": "entitlements.active_entitlement_summary"})); code != 422 {
		t.Fatalf("a summary without a customer: %d, want 422", code)
	}
}

// A card attached to a customer of no workspace here, or told twice, is acknowledged and leaves one mark at most.
func TestCardAttachedDuplicateAndUnknown(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	e.pay("POST", "/v1/billing/checkout", nil, 200, nil) // cus_1
	card := func(customer string) map[string]any {
		return map[string]any{"id": "pm_1", "customer": customer, "type": "card", "card": map[string]any{"fingerprint": "fp_events_card"}}
	}
	marks := func() (n int) {
		t.Helper()
		if err := e.svc.DB.QueryRow(`SELECT COUNT(*) FROM trial_fingerprints WHERE kind = 'card'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, id := range []string{"evt_pm_else", "evt_pm_else"} {
		if code := e.rawStripeEvent(id, "payment_method.attached", card("cus_nobody")); code != 200 {
			t.Fatalf("unknown customer: %d", code)
		}
	}
	if n := marks(); n != 0 {
		t.Fatalf("a card of an unknown customer left %d marks", n)
	}
	for _, id := range []string{"evt_pm_1", "evt_pm_1", "evt_pm_2"} {
		if code := e.rawStripeEvent(id, "payment_method.attached", card("cus_1")); code != 200 {
			t.Fatalf("card attached: %d", code)
		}
	}
	if n := marks(); n != 1 {
		t.Fatalf("one card told three times left %d marks, want 1", n)
	}
	// Events staqly doesn't handle are acknowledged, whatever they carry.
	for _, typ := range []string{"invoice.paid", "invoice.payment_failed", "customer.updated", "charge.succeeded", "customer.subscription.trial_will_end"} {
		if code := e.rawStripeEvent("evt_"+typ, typ, map[string]any{"id": "x", "customer": "cus_1"}); code != 200 {
			t.Fatalf("%s: %d", typ, code)
		}
	}
}
