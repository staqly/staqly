package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// fakeStripe records calls and answers from a per-route table; a missing
// route is a 404 and status >= 300 makes Stripe "reject" the request.
type fakeStripe struct {
	mu     sync.Mutex
	calls  []string
	routes map[string]func() (int, any)
}

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, key)
	if servePendingSub(w, r) {
		return
	}
	h, ok := f.routes[key]
	if !ok {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"message":"no such route"}}`)
		return
	}
	code, body := h()
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(body)
}

func (f *fakeStripe) set(route string, h func() (int, any)) {
	f.mu.Lock()
	f.routes[route] = h
	f.mu.Unlock()
}

func (f *fakeStripe) called(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == route {
			n++
		}
	}
	return n
}

func withStripe(t *testing.T, e *env) *fakeStripe {
	t.Helper()
	f := &fakeStripe{routes: map[string]func() (int, any){
		"POST /v1/customers":         func() (int, any) { return 200, map[string]string{"id": "cus_1"} },
		"POST /v1/checkout/sessions": func() (int, any) { return 200, map[string]string{"url": "https://checkout.stripe.test/cs"} },
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: srv.URL,
		Prices: map[string]string{"pro_month": "price_pro"}}
	return f
}

// signedStripe posts a raw body with a signature made at ts.
func (e *env) signedStripe(body []byte, ts time.Time) int {
	e.t.Helper()
	t := fmt.Sprint(ts.Unix())
	mac := hmac.New(sha256.New, []byte(testWhsec))
	mac.Write([]byte(t + "." + string(body)))
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t="+t+",v1="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// stripeDirect applies a signed payload through the service (bypassing the
// HTTP contract checks) and returns the error status, 0 on success.
func (e *env) stripeDirect(body []byte) int {
	e.t.Helper()
	ts := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(testWhsec))
	mac.Write([]byte(ts + "." + string(body)))
	e.rememberSubscription(body)
	err := e.svc.HandleStripeWebhook(context.Background(), body, "t="+ts+",v1="+hex.EncodeToString(mac.Sum(nil)))
	var ae *app.Error
	if errors.As(err, &ae) {
		return ae.Status
	}
	if err != nil {
		e.t.Fatal(err)
	}
	return 0
}

func stripeBody(id, typ string, object any) []byte {
	b, _ := json.Marshal(map[string]any{"id": id, "type": typ, "data": map[string]any{"object": object}})
	return b
}

func (e *env) billing(admin string) app.Billing {
	e.t.Helper()
	var b app.Billing
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	return b
}

func TestStripeWebhookRejectsAndIgnores(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	if code := e.stripeEvent("evt_off", "customer.subscription.updated", map[string]any{}, testWhsec); code != 409 {
		t.Fatalf("billing off: %d", code)
	}
	withStripe(t, e)
	before := e.billing(admin)

	if code := e.signedStripe([]byte(`{"id":"evt_1"}`), time.Now().Add(-10*time.Minute)); code != 400 {
		t.Fatalf("stale signature: %d", code)
	}
	if code := e.signedStripe([]byte(`{"id":"evt_1"}`), time.Now().Add(10*time.Minute)); code != 400 {
		t.Fatalf("future signature: %d", code)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/webhook", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=abc")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 400 {
		t.Fatalf("garbage signature header: %v %v", resp, err)
	}
	// 422s go through the service: the OpenAPI document does not list them.
	if code := e.stripeDirect([]byte(`not json`)); code != 422 {
		t.Fatalf("not an event: %d", code)
	}
	if code := e.stripeDirect([]byte(`{"type":"customer.subscription.updated"}`)); code != 422 {
		t.Fatalf("event without id: %d", code)
	}
	if code := e.stripeDirect(stripeBody("evt_bad", "customer.subscription.updated", "a string")); code != 422 {
		t.Fatalf("bad subscription object: %d", code)
	}
	// Acknowledged and ignored: other event types, non-subscription checkouts,
	// subscriptions of customers this instance does not know.
	if code := e.stripeEvent("evt_inv", "invoice.paid", map[string]any{"id": "in_1"}, testWhsec); code != 200 {
		t.Fatalf("unknown type: %d", code)
	}
	if code := e.stripeEvent("evt_pay", "checkout.session.completed", map[string]any{"mode": "payment"}, testWhsec); code != 200 {
		t.Fatalf("payment checkout: %d", code)
	}
	if code := e.stripeEvent("evt_unk", "customer.subscription.created", map[string]any{"id": "sub_x", "customer": "cus_nobody",
		"status": "active"}, testWhsec); code != 200 {
		t.Fatalf("unknown customer: %d", code)
	}
	if code := e.stripeEvent("evt_gone", "customer.subscription.created", map[string]any{"id": "sub_x", "customer": "cus_x",
		"status": "active", "metadata": map[string]string{"workspace_id": "ws_deleted"}}, testWhsec); code != 200 {
		t.Fatalf("deleted workspace: %d", code)
	}
	if after := e.billing(admin); after.Plan != before.Plan || after.SubscriptionStatus != nil || fmt.Sprint(after.PlanExpiresAt) != fmt.Sprint(before.PlanExpiresAt) {
		t.Fatalf("ignored events changed billing: %+v -> %+v", before, after)
	}
}

// A checkout event whose subscription can't be read fails (Stripe retries it)
// and is not marked as applied, so the retry still takes effect.
func TestStripeCheckoutEventRetriedAfterProviderError(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	f := withStripe(t, e)
	ws := workspaceID(e)
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 500, map[string]any{"error": map[string]string{"message": "down"}} })
	if code := e.stripeDirect(stripeBody("evt_cs", "checkout.session.completed", map[string]any{"subscription": "sub_1"})); code != 502 {
		t.Fatalf("provider error: %d", code)
	}
	if b := e.billing(admin); b.SubscriptionStatus != nil {
		t.Fatalf("nothing should be applied: %+v", b)
	}
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) {
		return 200, map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws}}
	})
	if code := e.stripeEvent("evt_cs", "checkout.session.completed", map[string]any{"subscription": "sub_1"}, testWhsec); code != 200 {
		t.Fatalf("retry: %d", code)
	}
	b := e.billing(admin)
	if b.Plan != "pro" || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "active" || b.PlanExpiresAt == nil {
		t.Fatalf("after retry: %+v", b)
	}
	// No period end in the object: Pro runs about a month.
	end, _ := time.Parse(time.RFC3339, *b.PlanExpiresAt)
	if d := time.Until(end); d < 30*24*time.Hour || d > 32*24*time.Hour {
		t.Fatalf("fallback period end %s", *b.PlanExpiresAt)
	}
	if f.called("GET /v1/subscriptions/sub_1") != 2 {
		t.Fatalf("calls: %v", f.calls)
	}
}

func TestStripeSubscriptionMatchedByCustomerAndStaleSubscriptions(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	f := withStripe(t, e)
	e.pay("POST", "/v1/billing/checkout", nil, 200, nil) // creates customer cus_1
	if b := e.billing(admin); !b.HasCustomer {
		t.Fatalf("customer not stored: %+v", b)
	}
	periodEnd := time.Now().Add(20 * 24 * time.Hour).Unix()
	// No workspace_id metadata: matched through the stored customer.
	sub := map[string]any{"id": "sub_2", "customer": "cus_1", "status": "trialing", "current_period_end": periodEnd}
	if code := e.stripeEvent("evt_a", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("created: %d", code)
	}
	b := e.billing(admin)
	if b.Plan != "pro" || *b.SubscriptionStatus != "trialing" {
		t.Fatalf("by customer: %+v", b)
	}
	if end, _ := time.Parse(time.RFC3339, *b.PlanExpiresAt); !end.After(time.Unix(periodEnd, 0)) {
		t.Fatalf("grace after the period end: %s", *b.PlanExpiresAt)
	}
	// Already subscribed: a checkout for the plan it has is refused (another one would switch the subscription).
	f.routes["GET /v1/subscriptions/sub_2"] = func() (int, any) {
		return 200, map[string]any{"id": "sub_2", "items": map[string]any{"data": []any{map[string]any{"id": "si_2", "price": map[string]any{"id": "price_pro"}}}}}
	}
	e.pay("POST", "/v1/billing/checkout", nil, 409, nil)

	// An older subscription of the same customer ending does not touch the live one.
	old := map[string]any{"id": "sub_old", "customer": "cus_1", "status": "canceled"}
	if code := e.stripeEvent("evt_b", "customer.subscription.deleted", old, testWhsec); code != 200 {
		t.Fatalf("old deleted: %d", code)
	}
	if b := e.billing(admin); b.Suspended || *b.SubscriptionStatus != "trialing" {
		t.Fatalf("stale subscription applied: %+v", b)
	}

	// unpaid ends Pro now: suspended, and the end date never moves later.
	sub["status"] = "unpaid"
	e.stripeEvent("evt_c", "customer.subscription.updated", sub, testWhsec)
	b = e.billing(admin)
	if !b.Suspended || *b.SubscriptionStatus != "unpaid" {
		t.Fatalf("unpaid: %+v", b)
	}
	if end, _ := time.Parse(time.RFC3339, *b.PlanExpiresAt); end.After(time.Now()) {
		t.Fatalf("unpaid end date in the future: %s", *b.PlanExpiresAt)
	}
	// Writes are refused while suspended.
	if err := e.svc.CheckWritable(context.Background(), workspaceID(e)); err != app.ErrSuspended {
		t.Fatalf("CheckWritable while unpaid: %v", err)
	}

	// A paused subscription stays read-only.
	sub["status"] = "paused"
	e.stripeEvent("evt_d", "customer.subscription.paused", sub, testWhsec)
	if b := e.billing(admin); b.Plan != "pro" || !b.Suspended {
		t.Fatalf("paused: %+v", b)
	}

	// Paying again resumes Pro; the plan change is audited with Stripe as actor.
	sub["status"] = "active"
	e.stripeEvent("evt_e", "customer.subscription.resumed", sub, testWhsec)
	if b := e.billing(admin); b.Plan != "pro" || b.Suspended {
		t.Fatalf("resumed: %+v", b)
	}
	// The same state told again (another event about it) changes nothing and records nothing.
	e.stripeEvent("evt_e2", "customer.subscription.updated", sub, testWhsec)
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?type=workspace.plan_changed&limit=50", admin, nil, 200, &page)
	var reasons []string
	for _, ev := range page.Data {
		var d struct{ Reason string }
		json.Unmarshal(ev.Data, &d)
		if ev.Actor.Name == "Stripe" {
			reasons = append(reasons, d.Reason)
		}
	}
	if got := strings.Join(reasons, ","); got != "stripe:active,stripe:paused,stripe:unpaid,stripe:trialing" {
		t.Fatalf("plan_changed reasons (newest first): %q", got)
	}
}

// Deleting a workspace cancels its live subscription first; if Stripe can't
// be reached, nothing is deleted.
func TestDeleteWorkspaceCancelsSubscription(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	e.stripeEvent("evt_1", "customer.subscription.created", map[string]any{"id": "sub_9", "customer": "cus_9", "status": "active",
		"metadata": map[string]string{"workspace_id": ws}}, testWhsec)
	csrf := []string{"X-CSRF-Token", e.csrf}

	f.set("DELETE /v1/subscriptions/sub_9", func() (int, any) { return 500, map[string]any{"error": map[string]string{"message": "down"}} })
	session := app.Principal{WorkspaceID: ws, Kind: app.PrincipalSession, ID: "ses_test", Name: "Owner", Scopes: app.AllScopes}
	err := e.svc.DeleteWorkspace(context.Background(), session, "Test")
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Status != 502 {
		t.Fatalf("delete with Stripe down: %v", err)
	}
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, nil)

	f.set("DELETE /v1/subscriptions/sub_9", func() (int, any) { return 200, map[string]string{"id": "sub_9", "status": "canceled"} })
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, csrf...)
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 401, nil)
	if n := f.called("DELETE /v1/subscriptions/sub_9"); n != 2 {
		t.Fatalf("cancel calls: %d (%v)", n, f.calls)
	}
}

// A workspace without a live subscription is deleted without calling Stripe.
func TestDeleteWorkspaceWithoutSubscriptionSkipsStripe(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	e.stripeEvent("evt_1", "customer.subscription.deleted", map[string]any{"id": "sub_9", "customer": "cus_9", "status": "canceled",
		"metadata": map[string]string{"workspace_id": ws}}, testWhsec)
	f.mu.Lock()
	f.calls = nil // the event read the subscription back; the delete must not call Stripe at all
	f.mu.Unlock()
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, "X-CSRF-Token", e.csrf)
	if len(f.calls) != 0 {
		t.Fatalf("stripe called: %v", f.calls)
	}
}

func TestBillingPortalNeedsCustomerAndAdmin(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	e.do(nil, "GET", "/v1/billing", e.key, nil, 403, nil)
	e.pay("POST", "/v1/billing/portal", nil, 409, nil) // billing off
	f := withStripe(t, e)
	e.do(nil, "POST", "/v1/billing/portal", e.key, nil, 403, nil)
	e.pay("POST", "/v1/billing/portal", nil, 409, nil) // no customer yet
	if f.called("POST /v1/billing_portal/sessions") != 0 {
		t.Fatalf("stripe called without a customer: %v", f.calls)
	}
	// Stripe rejecting the customer creation surfaces as 502 and stores nothing.
	f.set("POST /v1/customers", func() (int, any) { return 400, map[string]any{"error": map[string]string{"message": "bad"}} })
	e.pay("POST", "/v1/billing/checkout", nil, 502, nil)
	if b := e.billing(admin); b.HasCustomer || !b.Enabled {
		t.Fatalf("after failed checkout: %+v", b)
	}
}

// subObject is a Stripe subscription of a workspace's customer (cus_1) on a plan, as events and reads carry it.
func subObject(id, ws, status, plan, interval string) map[string]any {
	return map[string]any{"id": id, "customer": "cus_1", "status": status, "metadata": map[string]string{"workspace_id": ws},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_" + id, "quantity": 1,
			"current_period_end": time.Now().Add(300 * 24 * time.Hour).Unix(), "price": stripePriceObject(plan, interval)}}}}
}

func recordedSubscription(e *env, ws string) string {
	e.t.Helper()
	w, err := e.svc.Q.GetWorkspaceBilling(context.Background(), ws)
	if err != nil {
		e.t.Fatal(err)
	}
	return w.StripeSubscriptionID.String
}

// Was a bug: two checkouts left open (an old tab) and both paid gave two live subscriptions, of which only the
// last one was known; deleting the workspace cancelled only that one.
func TestOneSubscriptionPerWorkspace(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	ok := func() (int, any) { return 200, map[string]string{} }
	f.set("POST /v1/customers/cus_1", ok)
	f.set("GET /v1/checkout/sessions", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]string{"id": "cs_old", "mode": "subscription"}, map[string]string{"id": "cs_posts", "mode": "payment"}}}
	})
	f.set("POST /v1/checkout/sessions/cs_old/expire", ok)

	// A second plan checkout closes the first; a checkout for extra posts is left alone.
	for range 2 {
		e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil)
	}
	if n, m := f.called("POST /v1/checkout/sessions/cs_old/expire"), f.called("POST /v1/checkout/sessions/cs_posts/expire"); n != 1 || m != 0 {
		t.Fatalf("expired the older plan checkout %d times (want 1), the posts checkout %d times (want 0)", n, m)
	}

	// Both were paid all the same: the first stays, the second is cancelled with its unused time credited.
	subA, subB := subObject("sub_A", ws, "active", "pro", "month"), subObject("sub_B", ws, "active", "pro", "month")
	f.set("GET /v1/subscriptions/sub_A", func() (int, any) { return 200, subA })
	f.set("DELETE /v1/subscriptions/sub_A", ok)
	f.set("DELETE /v1/subscriptions/sub_B", ok)
	for i, sub := range []map[string]any{subA, subB, subObject("sub_B", ws, "canceled", "pro", "month")} {
		if code := e.stripeEvent(fmt.Sprint("evt_", i), "customer.subscription.created", sub, testWhsec); code != 200 {
			t.Fatalf("event %d: %d", i, code)
		}
	}
	if got, n := recordedSubscription(e, ws), f.called("DELETE /v1/subscriptions/sub_B"); got != "sub_A" || n != 1 || f.called("DELETE /v1/subscriptions/sub_A") != 0 {
		t.Fatalf("kept %q, cancelled the duplicate %d times", got, n)
	}
	if b := e.billing(admin); b.Plan != "pro" || b.Suspended {
		t.Fatalf("after the duplicate: %+v", b)
	}

	// Deleting the workspace cancels every subscription its customer still has, known here or not.
	f.set("GET /v1/subscriptions", func() (int, any) {
		return 200, map[string]any{"data": []any{subA, subObject("sub_C", ws, "past_due", "pro", "month")}}
	})
	f.set("DELETE /v1/subscriptions/sub_C", ok)
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, "X-CSRF-Token", e.csrf)
	if a, c := f.called("DELETE /v1/subscriptions/sub_A"), f.called("DELETE /v1/subscriptions/sub_C"); a != 1 || c != 1 {
		t.Fatalf("workspace deleted; cancel calls sub_A=%d sub_C=%d (a live subscription keeps charging)", a, c)
	}
}

// The subscription on record ended at Stripe and its event never came: a new one takes its place, and is not
// mistaken for a duplicate.
func TestNewSubscriptionReplacesOneThatEndedUnseen(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	// One whose first payment never came gives way to a paid one without asking Stripe.
	e.stripeEvent("evt_0", "customer.subscription.created", subObject("sub_0", ws, "incomplete", "starter", "month"), testWhsec)
	e.stripeEvent("evt_0b", "customer.subscription.created", subObject("sub_A", ws, "active", "starter", "month"), testWhsec)
	if got := recordedSubscription(e, ws); got != "sub_A" || f.called("DELETE /v1/subscriptions/sub_A") != 0 {
		t.Fatalf("after an unpaid first one: %q, calls %v", got, f.calls)
	}
	e.stripeEvent("evt_1", "customer.subscription.created", subObject("sub_A", ws, "active", "starter", "month"), testWhsec)
	f.set("GET /v1/subscriptions/sub_A", func() (int, any) { return 200, subObject("sub_A", ws, "canceled", "starter", "month") })
	if code := e.stripeEvent("evt_2", "customer.subscription.created", subObject("sub_B", ws, "active", "pro", "month"), testWhsec); code != 200 {
		t.Fatalf("event: %d", code)
	}
	if got := recordedSubscription(e, ws); got != "sub_B" || f.called("DELETE /v1/subscriptions/sub_B") != 0 {
		t.Fatalf("on record: %q, calls %v", got, f.calls)
	}
	if b := e.billing(admin); b.Plan != "pro" || b.Suspended || *b.SubscriptionStatus != "active" {
		t.Fatalf("after the new subscription: %+v", b)
	}
}

// Was a bug: a plan the operator set (server set-plan, the admin panel) after the subscription ended was undone by
// POST /v1/billing/sync, or by Stripe telling the ended subscription again, and the workspace went read-only.
func TestOperatorPlanSurvivesEndedSubscription(t *testing.T) {
	for _, how := range []string{"sync", "replayed event"} {
		t.Run(how, func(t *testing.T) {
			e := newEnv(t)
			f := withStripe(t, e)
			ws := workspaceID(e)
			admin := e.keyWith(app.ScopeAdmin)
			e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // makes cus_1
			e.stripeEvent("evt_1", "customer.subscription.created", subObject("sub_1", ws, "active", "starter", "month"), testWhsec)
			ended := subObject("sub_1", ws, "canceled", "starter", "month")
			e.stripeEvent("evt_2", "customer.subscription.deleted", ended, testWhsec)
			if b := e.billing(admin); !b.Suspended {
				t.Fatalf("setup: canceled must suspend: %+v", b)
			}
			if err := e.svc.SetPlan(context.Background(), ws, "pro", ""); err != nil { // server set-plan <ws> pro
				t.Fatal(err)
			}
			if how == "sync" {
				f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": []any{ended}} })
				e.do(nil, "POST", "/v1/billing/sync", admin, nil, 200, nil)
			} else if code := e.stripeEvent("evt_3", "customer.subscription.updated", ended, testWhsec); code != 200 {
				t.Fatalf("event: %d", code)
			}
			if b := e.billing(admin); b.Suspended || b.Plan != "pro" || b.PlanExpiresAt != nil {
				t.Fatalf("operator plan undone: %+v", b)
			}
			// Paying again puts Stripe back in charge: that subscription ending ends the plan.
			e.stripeEvent("evt_4", "customer.subscription.created", subObject("sub_2", ws, "active", "plus", "month"), testWhsec)
			e.stripeEvent("evt_5", "customer.subscription.deleted", subObject("sub_2", ws, "canceled", "plus", "month"), testWhsec)
			if b := e.billing(admin); !b.Suspended || b.Plan != "plus" {
				t.Fatalf("a paid plan that ended must suspend: %+v", b)
			}
		})
	}
}
