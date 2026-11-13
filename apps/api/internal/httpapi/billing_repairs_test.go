package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Was a bug: a plan checkout left open (another tab) could be paid after its workspace was deleted; the
// subscription it started was ignored (the workspace is gone) and billed with nobody to cancel it. Deleting the
// workspace closes its open plan checkouts, and fails when Stripe can't do that.
func TestDeleteWorkspaceExpiresOpenCheckouts(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ok := func() (int, any) { return 200, map[string]string{} }
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // cus_1, a checkout left open
	f.set("GET /v1/checkout/sessions", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]string{"id": "cs_open", "mode": "subscription"}}}
	})
	f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": []any{}} }) // none yet
	csrf := []string{"X-CSRF-Token", e.csrf}
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 502, nil, csrf...) // can't expire it
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, nil)
	f.set("POST /v1/checkout/sessions/cs_open/expire", ok)
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, csrf...)
	if n := f.called("POST /v1/checkout/sessions/cs_open/expire"); n != 2 {
		t.Fatalf("the open checkout was expired %d times, want 2 (one refused): %v", n, f.calls)
	}
}

// Was a bug: the sync read only the customer's newest subscription. With a cancelled duplicate (B) newer than the
// kept one (A), A was never read again: its renewal webhook lost, a paying workspace stayed read-only.
func TestSyncReadsTheLiveSubscriptionBehindACancelledDuplicate(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	ok := func() (int, any) { return 200, map[string]string{} }
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // cus_1

	const day = 24 * time.Hour
	withEnd := func(sub map[string]any, end time.Time) map[string]any {
		sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["current_period_end"] = end.Unix()
		return sub
	}
	// A's paid period ended four days ago (past the grace), as staqly knows it.
	subA := withEnd(subObject("sub_A", ws, "active", "pro", "month"), time.Now().Add(-4*day))
	f.set("GET /v1/subscriptions/sub_A", func() (int, any) { return 200, subA })
	f.set("DELETE /v1/subscriptions/sub_B", ok)
	cancelledB := subObject("sub_B", ws, "canceled", "pro", "month")
	for i, sub := range []map[string]any{subA, subObject("sub_B", ws, "active", "pro", "month"), cancelledB} {
		if code := e.stripeEvent(fmt.Sprint("evt_dup_", i), "customer.subscription.created", sub, testWhsec); code != 200 {
			t.Fatalf("event %d: %d", i, code)
		}
	}
	if got, n := recordedSubscription(e, ws), f.called("DELETE /v1/subscriptions/sub_B"); got != "sub_A" || n != 1 {
		t.Fatalf("setup: kept %q, the duplicate cancelled %d times", got, n)
	}
	if b := e.billing(admin); !b.Suspended {
		t.Fatalf("setup: the period known here has ended: %+v", b)
	}

	// At Stripe A renewed (the webhook never came). The list is newest first: B, cancelled, then A.
	renewedA := withEnd(subObject("sub_A", ws, "active", "pro", "month"), time.Now().Add(26*day))
	f.set("GET /v1/subscriptions/sub_A", func() (int, any) { return 200, renewedA })
	f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": []any{cancelledB, renewedA}} })
	if n := e.svc.ReconcileBilling(context.Background()); n != 1 { // the worker's round finds it: its period ended
		t.Fatalf("the reconcile round synced %d workspaces, want 1", n)
	}
	if b := e.billing(admin); b.Suspended {
		t.Fatalf("a paying workspace stays read-only after the sync: %+v", b)
	}
	if got, n := recordedSubscription(e, ws), f.called("DELETE /v1/subscriptions/sub_A"); got != "sub_A" || n != 0 {
		t.Fatalf("after the sync: on record %q, sub_A cancelled %d times", got, n)
	}
}

// Was a bug: a yearly plan with a monthly add-on (monthly to yearly used to move the plan's item only) lost its
// paid year when the add-on's monthly renewal failed: the plan ended three days after the add-on's month began.
func TestFailedAddonRenewalKeepsThePaidYear(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	const day = 24 * time.Hour
	now := time.Now().Truncate(time.Second)
	yearEnd := now.Add(265 * day)
	addonRenewed := now.Add(-4 * day) // the add-on's month began (and its charge failed) four days ago
	n := 0
	apply := func(status, invoice string) app.Billing {
		t.Helper()
		n++
		sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": status, "metadata": map[string]string{"workspace_id": ws},
			"latest_invoice": map[string]any{"id": fmt.Sprint("in_", n), "billing_reason": "subscription_cycle", "status": invoice},
			"items": map[string]any{"data": []any{
				map[string]any{"id": "si_plan", "quantity": 1, "current_period_start": now.Add(-100 * day).Unix(),
					"current_period_end": yearEnd.Unix(), "price": stripePriceObject("starter", "year")},
				map[string]any{"id": "si_addon", "quantity": 1, "current_period_start": addonRenewed.Unix(),
					"current_period_end": addonRenewed.Add(30 * day).Unix(),
					"price": map[string]any{"id": "price_addon", "lookup_key": app.AddonLookupKey("webhooks_pack", "month"),
						"recurring": map[string]any{"interval": "month"}}},
			}}}
		if code := e.stripeEvent(fmt.Sprint("evt_mixed_", n), "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("%s event: %d", status, code)
		}
		return e.billing(admin)
	}
	want := yearEnd.Add(3 * day)
	if b := apply("active", "paid"); b.Suspended || b.Plan != "starter" || b.PlanExpiresAt == nil {
		t.Fatalf("setup: %+v", b)
	} else if end, _ := time.Parse(time.RFC3339, *b.PlanExpiresAt); !end.Equal(want) {
		t.Fatalf("setup: the year ends %s, want %s", end, want)
	}
	b := apply("past_due", "open")
	end, _ := time.Parse(time.RFC3339, deref(b.PlanExpiresAt))
	if b.Suspended || !end.Equal(want) {
		t.Fatalf("an unpaid monthly add-on took the paid year away: suspended=%v, the plan ends %s, want %s (the add-on's month began %s)",
			b.Suspended, end, want, addonRenewed)
	}
}

// Was a bug: a seat or entitlement sync that failed three times (Stripe down for a few minutes) was dropped, and
// nothing ran it again before the next subscription event, up to a year away: a removed seat stayed billed, a paid
// feature stayed off. The job keeps trying every hour, with the time of the change it prorates from.
func TestBillingSyncJobsOutlastAStripeOutage(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	ws := e.session(owner).Workspace.ID
	ctx := context.Background()
	var mu sync.Mutex
	quantity, down, failed, featuresFailed := int64(1), false, 0, 0
	var prorated string
	active := []any{}
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		r.ParseForm()
		sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": quantity,
				"current_period_start": time.Now().Add(-10 * 24 * time.Hour).Unix(), "current_period_end": time.Now().Add(355 * 24 * time.Hour).Unix(),
				"price": stripePriceObject("plus", "year")}}}}
		unavailable := func() {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"message":"down"}}`)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_plus_year"}]}`)
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "POST /v1/checkout/sessions":
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_1":
			if down {
				failed++
				unavailable()
				return
			}
			fmt.Sscan(r.Form.Get("items[0][quantity]"), &quantity)
			prorated = r.Form.Get("proration_date")
			fmt.Fprint(w, `{"id":"sub_1"}`)
		case "GET /v1/entitlements/active_entitlements":
			if down {
				featuresFailed++
				unavailable()
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": active})
		case "GET /v1/entitlements/features":
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "feat_wh", "name": "Webhooks pack",
				"lookup_key": "webhooks_extra_2", "metadata": map[string]string{"staqly_limit_webhooks": "+2"}}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	read := func() (q int64, f, ff int) { mu.Lock(); defer mu.Unlock(); return quantity, failed, featuresFailed }
	webhooks := func() int64 {
		t.Helper()
		var u app.Usage
		e.as(owner, "GET", "/v1/workspace/usage", nil, 200, &u)
		return u.Plan.Limits.Webhooks
	}

	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "plus", "interval": "year"}, 200, nil)
	first := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": 1,
			"current_period_end": time.Now().Add(355 * 24 * time.Hour).Unix(), "price": stripePriceObject("plus", "year")}}}}
	if code := e.stripeEvent("evt_seat_1", "customer.subscription.created", first, testWhsec); code != 200 {
		t.Fatalf("webhook: %d", code)
	}
	_, tok := e.invite(owner, bob.Email, app.RoleEditor)
	e.mustSignIn(bob, "invite="+url.QueryEscape(tok))
	e.svc.RunDueJobsOnce(ctx)
	if q, _, _ := read(); q != 2 {
		t.Fatalf("setup: Bob joined, Stripe bills %d seats", q)
	}
	base := webhooks()

	// Stripe goes down. Bob is removed and a feature is granted; both syncs use up their three attempts.
	mu.Lock()
	down = true
	active = []any{map[string]any{"id": "ent_1", "feature": "feat_wh", "lookup_key": "webhooks_extra_2"}}
	mu.Unlock()
	removed := time.Now()
	var members struct{ Data []app.Member }
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	for _, m := range members.Data {
		if m.Email == bob.Email {
			e.as(owner, "DELETE", "/v1/members/"+m.UserID, nil, 204, nil)
		}
	}
	summary := map[string]any{"object": "entitlements.active_entitlement_summary", "customer": "cus_1"}
	if code := e.stripeEvent("evt_ent", "entitlements.active_entitlement_summary.updated", summary, testWhsec); code != 200 {
		t.Fatalf("summary event: %d", code)
	}
	clock := time.Now()
	e.svc.Now = func() time.Time { return clock }
	defer func() { e.svc.Now = time.Now }()
	for range 4 { // retries come 1 and 5 minutes apart
		e.svc.RunDueJobsOnce(ctx)
		clock = clock.Add(10 * time.Minute)
	}
	if q, f, ff := read(); f != 3 || ff != 3 || q != 2 {
		t.Fatalf("setup: the seat sync was tried %d times and the entitlement sync %d times, want 3 each; Stripe has %d seats", f, ff, q)
	}

	// Stripe is back: the next hourly try puts both right, the seat credited from when Bob left.
	mu.Lock()
	down = false
	mu.Unlock()
	clock = clock.Add(2 * time.Hour)
	e.svc.RunDueJobsOnce(ctx)
	e.svc.Now = time.Now
	if q, _, _ := read(); q != 1 {
		t.Fatalf("after Stripe came back the removed seat is still billed: Stripe has %d seats for 1 member", q)
	}
	var at int64
	fmt.Sscan(prorated, &at)
	if at < removed.Unix()-1 || at > removed.Unix()+5 {
		t.Fatalf("the seat's credit starts at %d, want when Bob left, %d", at, removed.Unix())
	}
	if got := webhooks(); got != base+2 {
		t.Fatalf("after Stripe came back the paid feature is still off: webhooks limit %d, want %d", got, base+2)
	}
}

// Was a bug: a feature made at Stripe while the feature catalog was cached (10 minutes) and granted to a customer
// right away was stored without its metadata, so the limit it raises didn't apply until the next subscription
// event. A feature the cached catalog doesn't know makes the sync read the catalog again.
func TestFeatureNewerThanTheCatalogCacheGetsItsAllowance(t *testing.T) {
	e := newEnv(t)
	ws := workspaceID(e)
	ctx := context.Background()
	if err := e.svc.SetPlan(ctx, ws, "starter", ""); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	features := []any{map[string]any{"id": "feat_old", "name": "Old pack", "lookup_key": "old_pack", "metadata": map[string]string{"staqly_limit_alerts": "+1"}}}
	active := []any{}
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_starter_month"}]}`)
		case "POST /v1/checkout/sessions":
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/entitlements/active_entitlements":
			json.NewEncoder(w).Encode(map[string]any{"data": active})
		case "GET /v1/entitlements/features":
			json.NewEncoder(w).Encode(map[string]any{"data": features})
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(subObject("sub_1", ws, "active", "starter", "month"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "month"}, 200, nil)
	if code := e.stripeEvent("evt_sub", "customer.subscription.created", subObject("sub_1", ws, "active", "starter", "month"), testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	e.svc.RunDueJobsOnce(ctx) // the first sync reads the catalog: cached now
	webhooks := func() int64 {
		t.Helper()
		var u app.Usage
		e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
		return u.Plan.Limits.Webhooks
	}
	base := webhooks()

	// A new feature (two more webhooks), on the customer's subscription at once.
	mu.Lock()
	features = append(features, map[string]any{"id": "feat_new", "name": "Webhooks pack", "lookup_key": "webhooks_extra_2",
		"metadata": map[string]string{"staqly_limit_webhooks": "+2"}})
	active = []any{map[string]any{"id": "ent_1", "feature": "feat_new", "lookup_key": "webhooks_extra_2"}}
	mu.Unlock()
	summary := map[string]any{"object": "entitlements.active_entitlement_summary", "customer": "cus_1"}
	if code := e.stripeEvent("evt_ent", "entitlements.active_entitlement_summary.updated", summary, testWhsec); code != 200 {
		t.Fatalf("summary event: %d", code)
	}
	e.svc.RunDueJobsOnce(ctx)
	if got := webhooks(); got != base+2 {
		t.Fatalf("a paid feature granted within the catalog cache's 10 minutes has no allowance: webhooks limit %d, want %d (plan %d + 2)", got, base+2, base)
	}
}

// Was a bug: StartPaidPlan applied the subscription Stripe answered with outside the customer's billing lock, so
// an answer that stalled was applied over a newer state a webhook brought meanwhile. It holds the lock now: the
// webhook waits, then applies the subscription as it is.
func TestStartPaidPlanHoldsTheBillingLock(t *testing.T) {
	e := newEnv(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var subs map[string]any // sub_c as Stripe has it
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_starter_month"}]}`)
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_c"}`)
		case "POST /v1/checkout/sessions":
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs"}`)
		case "GET /v1/subscriptions/sub_c":
			mu.Lock()
			defer mu.Unlock()
			json.NewEncoder(w).Encode(subs)
		case "POST /v1/subscriptions/sub_c": // trial_end=now: Starter, active; the answer stalls on its way back
			mu.Lock()
			subs["status"] = "active"
			answer, _ := json.Marshal(subs)
			mu.Unlock()
			close(arrived)
			<-release
			w.Write(answer)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"no such route"}}`)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	key, ws := e.trialWorkspace("C")
	e.payAs(key, "POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "month"}, 200, nil) // cus_c
	end := time.Now().Add(app.TrialDays * 24 * time.Hour).Unix()
	object := func(status, plan string) map[string]any {
		return map[string]any{"id": "sub_c", "customer": "cus_c", "status": status, "trial_end": end, "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": []any{map[string]any{"id": "si_c", "quantity": 1,
				"current_period_end": time.Now().Add(30 * 24 * time.Hour).Unix(), "price": stripePriceObject(plan, "month")}}}}
	}
	mu.Lock()
	subs = object("trialing", "starter")
	mu.Unlock()
	if code := e.stripeEvent("evt_trial", "customer.subscription.created", object("trialing", "starter"), testWhsec); code != 200 {
		t.Fatalf("trial event: %d", code)
	}
	if b := e.billingOf(key); deref(b.SubscriptionStatus) != "trialing" || b.Plan != "starter" {
		t.Fatalf("setup: %+v", b)
	}

	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar} // the owner, signed in: an API key can't start the paid plan
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(owner, "POST", "/v1/sessions/api-key", "", map[string]string{"api_key": key}, 201, &session)
	started := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/start-paid-plan", nil)
		req.Header.Set("X-CSRF-Token", session.CSRF)
		resp, err := owner.Do(req)
		if err != nil {
			started <- 0
			return
		}
		resp.Body.Close()
		started <- resp.StatusCode
	}()
	<-arrived
	// Meanwhile the subscription moves to Business (another owner's upgrade, paid) and its webhook comes in.
	mu.Lock()
	subs = object("active", "business")
	mu.Unlock()
	upgraded := make(chan int, 1)
	go func() {
		upgraded <- e.stripeEvent("evt_business", "customer.subscription.updated", object("active", "business"), testWhsec)
	}()
	select {
	case code := <-upgraded: // applied without waiting for the lock
		upgraded <- code
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if code := <-started; code != 200 {
		t.Fatalf("start-paid-plan: %d", code)
	}
	if code := <-upgraded; code != 200 {
		t.Fatalf("upgrade event: %d", code)
	}
	if b := e.billingOf(key); b.Plan != "business" {
		t.Fatalf("a stalled StartPaidPlan answer overwrote the newer plan: the workspace is on %q, Stripe bills business", b.Plan)
	}
}

// Was a bug: a plan the operator set for a workspace with an active subscription (a comp until a date) was undone
// by Stripe telling that same subscription again (a seat sync, a renewal). It stands while it runs, unless the
// customer pays for a plan above it; when its date has passed, the subscription's plan is back.
func TestOperatorPlanSurvivesActiveSubscriptionToldAgain(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	n := 0
	tell := func(plan string) app.Billing {
		t.Helper()
		n++
		if code := e.stripeEvent(fmt.Sprint("evt_told_", n), "customer.subscription.updated", subObject("sub_1", ws, "active", plan, "month"), testWhsec); code != 200 {
			t.Fatalf("event: %d", code)
		}
		return e.billing(admin)
	}
	if b := tell("starter"); b.Plan != "starter" {
		t.Fatalf("setup: %+v", b)
	}
	until := time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := e.svc.SetPlan(context.Background(), ws, "pro", until.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []string{"starter", "plus"} { // told again; a switch to a plan below the operator's
		b := tell(plan)
		if end, _ := time.Parse(time.RFC3339, deref(b.PlanExpiresAt)); b.Plan != "pro" || !end.Equal(until) {
			t.Fatalf("the operator's plan (pro until %s) was undone by the active %s subscription told again: plan %q until %s", until, plan, b.Plan, end)
		}
	}
	// The operator's date has passed: the paying workspace gets its subscription's plan back, not a lock.
	if err := e.svc.SetPlan(context.Background(), ws, "pro", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if b := tell("plus"); b.Plan != "plus" || b.Suspended {
		t.Fatalf("after the operator's plan ran out: %+v", b)
	}
	// A plan above the operator's, paid for: Stripe is in charge.
	if err := e.svc.SetPlan(context.Background(), ws, "pro", until.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if b := tell("business"); b.Plan != "business" {
		t.Fatalf("a paid plan above the operator's: %+v", b)
	}
}

// An interval change takes the add-ons along (every item of a subscription bills alike, as AddAddon assumes).
// Was a bug: monthly to yearly moved the plan's item only and left a monthly add-on on a yearly plan.
func TestIntervalChangeMovesAddons(t *testing.T) {
	e := newEnv(t)
	ws := workspaceID(e)
	var mu sync.Mutex
	var switched url.Values
	sub := subObject("sub_1", ws, "active", "starter", "month")
	items := sub["items"].(map[string]any)
	items["data"] = append(items["data"].([]any), map[string]any{"id": "si_addon", "quantity": 1,
		"price": map[string]any{"id": "price_addon_webhooks_pack_month", "lookup_key": app.AddonLookupKey("webhooks_pack", "month"),
			"recurring": map[string]any{"interval": "month"}}})
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			key := r.URL.Query().Get("lookup_keys[]")
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":900,"currency":"eur"}]}`, strings.TrimPrefix(key, "staqly_"))
		case "POST /v1/customers", "POST /v1/customers/cus_1":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "GET /v1/checkout/sessions":
			fmt.Fprint(w, `{"data":[]}`)
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_1":
			switched = r.Form
			fmt.Fprint(w, `{"id":"sub_1"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	if code := e.stripeEvent("evt_1", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("event: %d", code)
	}
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "year"}, 200, nil)
	mu.Lock()
	defer mu.Unlock()
	if switched.Get("items[0][id]") != "si_sub_1" || switched.Get("items[0][price]") != "price_starter_year" ||
		switched.Get("items[1][id]") != "si_addon" || switched.Get("items[1][price]") != "price_addon_webhooks_pack_year" ||
		switched.Get("items[1][quantity]") != "" || switched.Get("payment_behavior") != "pending_if_incomplete" {
		t.Fatalf("monthly to yearly with an add-on: %v", switched)
	}
}

// Was a bug (found on the Stripe sandbox): an add-on bought during the trial was free until its end, as Stripe
// charges nothing in a trial. Add-ons come with a paid plan: refused in the trial before Stripe is asked about
// the subscription. Only what the list shows is sold (was a bug: a draft product's id was accepted), and an add-on
// the subscription holds several times (a quantity set at Stripe) adds that many times its bonus.
func TestAddonsNeedAPaidPlanAndCountTheirQuantity(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	ctx := context.Background()
	if err := e.svc.SetPlan(ctx, ws, "starter", ""); err != nil {
		t.Fatal(err)
	}
	f.set("GET /v1/products", func() (int, any) {
		return 200, map[string]any{"data": []any{
			map[string]any{"name": "Webhooks pack", "metadata": map[string]string{"staqly_kind": "addon", "staqly_addon": "webhooks_pack"}},
			map[string]any{"name": "Draft pack", "metadata": map[string]string{"staqly_kind": "draft", "staqly_addon": "draft_pack"}},
			map[string]any{"name": "Starter", "metadata": map[string]string{"staqly_kind": "plan"}},
		}}
	})
	f.set("GET /v1/prices", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]any{"id": "price_addon", "unit_amount": 900, "currency": "eur"}}}
	})
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // cus_1
	f.set("POST /v1/subscriptions/sub_1", func() (int, any) { return 200, map[string]string{"id": "sub_1"} })
	sub := subObject("sub_1", ws, "trialing", "starter", "month")
	sub["trial_end"] = time.Now().Add(app.TrialDays * 24 * time.Hour).Unix()
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, sub })
	tell := func(id string) {
		t.Helper()
		if code := e.stripeEvent(id, "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("event %s: %d", id, code)
		}
	}
	tell("evt_trial")
	reads := f.called("GET /v1/subscriptions/sub_1")

	var errBody struct{ Error app.Error }
	e.pay("POST", "/v1/billing/addons/webhooks_pack", nil, 409, &errBody)
	if errBody.Error.Details["reason"] != "trialing" || !strings.Contains(errBody.Error.Message, "choose a plan first") {
		t.Fatalf("an add-on in the trial: %+v", errBody.Error)
	}
	if f.called("POST /v1/subscriptions/sub_1") != 0 || f.called("GET /v1/subscriptions/sub_1") != reads {
		t.Fatalf("Stripe was asked about the subscription for an add-on in the trial: %v", f.calls)
	}

	// Paying: the listed add-on is added; a draft, a plan's product and an unknown id are not found.
	sub["status"] = "active"
	tell("evt_paid")
	for _, id := range []string{"draft_pack", "starter", "nope"} {
		e.pay("POST", "/v1/billing/addons/"+id, nil, 404, nil)
	}
	if n := f.called("POST /v1/subscriptions/sub_1"); n != 0 {
		t.Fatalf("an add-on that isn't on sale reached Stripe %d times", n)
	}
	e.pay("POST", "/v1/billing/addons/webhooks_pack", nil, 204, nil)
	if n := f.called("POST /v1/subscriptions/sub_1"); n != 1 {
		t.Fatalf("the add-on was sent to Stripe %d times, want 1", n)
	}

	// Three of it at Stripe: three times its two webhooks.
	webhooks := func() int64 {
		t.Helper()
		var u app.Usage
		e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
		return u.Plan.Limits.Webhooks
	}
	base := webhooks()
	items := sub["items"].(map[string]any)
	items["data"] = append(items["data"].([]any), map[string]any{"id": "si_addon", "quantity": 3,
		"price": map[string]any{"id": "price_addon", "product": "prod_wh", "lookup_key": app.AddonLookupKey("webhooks_pack", "month"),
			"recurring": map[string]any{"interval": "month"}}})
	f.set("GET /v1/entitlements/features", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]any{"id": "feat_wh", "name": "Webhooks pack", "lookup_key": "webhooks_extra_2",
			"metadata": map[string]string{"staqly_limit_webhooks": "+2"}}}}
	})
	f.set("GET /v1/entitlements/active_entitlements", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]any{"id": "ent_1", "feature": "feat_wh", "lookup_key": "webhooks_extra_2"}}}
	})
	f.set("GET /v1/products/prod_wh/features", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]any{"id": "pf_1", "entitlement_feature": map[string]any{"id": "feat_wh", "lookup_key": "webhooks_extra_2"}}}}
	})
	tell("evt_three")
	e.svc.RunDueJobsOnce(ctx)
	if got := webhooks(); got != base+6 {
		t.Fatalf("three of an add-on that adds 2 webhooks: limit %d, want %d", got, base+6)
	}
}

// Paying and managing the subscription are a signed-in person's, like members and invitations: an API key, with
// every scope, gets 403 (was: any admin key could spend the card on file). Reading billing stays open to keys.
func TestBillingIsManagedByPeople(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	f.set("GET /v1/products", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	key := e.keyWith(app.AllScopes...)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}},
		{"POST", "/v1/billing/portal", nil},
		{"POST", "/v1/billing/start-paid-plan", nil},
		{"POST", "/v1/billing/addons/webhooks_pack", nil},
		{"DELETE", "/v1/billing/addons/webhooks_pack", nil},
		{"POST", "/v1/billing/credits/checkout", map[string]any{"posts": 200}},
	} {
		var errBody struct{ Error app.Error }
		e.do(nil, c.method, c.path, key, c.body, 403, &errBody)
		if !strings.Contains(errBody.Error.Message, "signed-in person") {
			t.Fatalf("%s %s with a key: %+v", c.method, c.path, errBody.Error)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a key's billing request reached Stripe: %v", f.calls)
	}
	for _, path := range []string{"/v1/billing", "/v1/billing/addons", "/v1/billing/x-posts"} {
		e.do(nil, "GET", path, key, nil, 200, nil)
	}
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // the signed-in owner
}

// Was a bug (found on the Stripe sandbox): extra X posts bought during the trial expired with it, a week later at
// most. They last a month from the purchase, as long as on a monthly plan.
func TestCreditsBoughtInTheTrialLastAMonth(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	sub := subObject("sub_1", ws, "trialing", "starter", "month")
	trialEnd := time.Now().Add(app.TrialDays * 24 * time.Hour)
	sub["trial_end"] = trialEnd.Unix()
	sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["current_period_end"] = trialEnd.Unix()
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, sub })
	if code := e.stripeEvent("evt_trial", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("trial event: %d", code)
	}
	session := map[string]any{"id": "cs_x", "mode": "payment", "payment_status": "paid", "client_reference_id": ws,
		"metadata": map[string]string{"workspace_id": ws, "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": "200"}}
	if code := e.stripeEvent("evt_buy", "checkout.session.completed", session, testWhsec); code != 200 {
		t.Fatalf("buy: %d", code)
	}
	var x app.XPosts
	e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
	if x.CreditsLeft != 200 || x.CreditsExpireAt == nil {
		t.Fatalf("bought posts: %+v", x)
	}
	if end, _ := time.Parse(time.RFC3339, *x.CreditsExpireAt); end.Before(time.Now().AddDate(0, 1, 0).Add(-time.Minute)) {
		t.Fatalf("posts bought in the trial expire %s, with the trial (%s); want a month from now", end, trialEnd)
	}
}

// The plan's paid period is its own item's: an add-on item on another period (left yearly by an older interval
// change) doesn't hold plan_expires_at at the year's end after the plan went back to monthly.
func TestPlanExpiresWithThePlansItem(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	const day = 24 * time.Hour
	monthEnd := time.Now().Add(20 * day).Truncate(time.Second)
	sub := subObject("sub_1", ws, "active", "starter", "month")
	items := sub["items"].(map[string]any)
	items["data"].([]any)[0].(map[string]any)["current_period_end"] = monthEnd.Unix()
	items["data"] = append(items["data"].([]any), map[string]any{"id": "si_addon", "quantity": 1, "current_period_end": time.Now().Add(300 * day).Unix(),
		"price": map[string]any{"id": "price_addon", "lookup_key": app.AddonLookupKey("webhooks_pack", "year"), "recurring": map[string]any{"interval": "year"}}})
	if code := e.stripeEvent("evt_1", "customer.subscription.updated", sub, testWhsec); code != 200 {
		t.Fatalf("event: %d", code)
	}
	b := e.billing(admin)
	if end, _ := time.Parse(time.RFC3339, deref(b.PlanExpiresAt)); !end.Equal(monthEnd.Add(3 * day)) {
		t.Fatalf("a monthly plan with a yearly add-on is paid until %s, want the plan's month plus the grace, %s", end, monthEnd.Add(3*day))
	}
}

// Was a bug (found on the Stripe sandbox): a subscription Stripe cancelled because a payment kept failing left its
// other open invoices (a renewal that failed meanwhile) to be collected: a card added later was charged for a plan
// that had ended, and choosing a plan charged again. They are voided when the subscription ends for non-payment;
// one the customer cancelled, and one held unpaid (paying opens it), keep theirs.
func TestSubscriptionEndedForNonPaymentVoidsItsOpenInvoices(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	f.set("GET /v1/invoices", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]string{"id": "in_renewal"}, map[string]string{"id": "in_paid_meanwhile"}}}
	})
	f.set("POST /v1/invoices/in_renewal/void", func() (int, any) { return 200, map[string]string{"id": "in_renewal", "status": "void"} })
	f.set("POST /v1/invoices/in_paid_meanwhile/void", func() (int, any) { // Stripe refuses: tolerated
		return 400, map[string]any{"error": map[string]string{"code": "invoice_not_open"}}
	})
	tell := func(id, status, reason string) {
		t.Helper()
		sub := subObject("sub_1", ws, status, "plus", "month")
		if reason != "" {
			sub["cancellation_details"] = map[string]any{"reason": reason}
		}
		if code := e.stripeEvent(id, "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("%s: %d", id, code)
		}
	}
	tell("evt_1", "active", "")
	tell("evt_2", "past_due", "")
	tell("evt_3", "unpaid", "")
	if n := f.called("GET /v1/invoices"); n != 0 {
		t.Fatalf("invoices of a live or unpaid subscription were read %d times: paying them is the way out", n)
	}
	tell("evt_4", "canceled", "payment_failed")
	if a, b := f.called("POST /v1/invoices/in_renewal/void"), f.called("POST /v1/invoices/in_paid_meanwhile/void"); a != 1 || b != 1 {
		t.Fatalf("a subscription ended for non-payment: its open invoices were voided %d and %d times, want 1 each (a later card would be charged): %v", a, b, f.calls)
	}

	// Cancelled by the customer: nothing is voided.
	e2 := newEnv(t)
	f2 := withStripe(t, e2)
	sub := subObject("sub_2", workspaceID(e2), "canceled", "plus", "month")
	sub["cancellation_details"] = map[string]any{"reason": "cancellation_requested"}
	if code := e2.stripeEvent("evt_5", "customer.subscription.deleted", sub, testWhsec); code != 200 {
		t.Fatalf("cancelled: %d", code)
	}
	if n := f2.called("GET /v1/invoices"); n != 0 {
		t.Fatalf("a subscription the customer cancelled had its invoices read %d times", n)
	}
}

// Was a bug (found on the Stripe sandbox): an add-on waiting for its payment (a pending update) couldn't be
// removed: 404, and its invoice stayed due until Stripe dropped the update, about a day later. Removing it voids
// the update's invoice, which is how Stripe cancels a pending update.
func TestRemoveAddonCancelsOneWaitingForItsPayment(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	live := subObject("sub_1", ws, "active", "starter", "month")
	if code := e.stripeEvent("evt_1", "customer.subscription.created", live, testWhsec); code != 200 {
		t.Fatalf("event: %d", code)
	}
	waiting := subObject("sub_1", ws, "active", "starter", "month")
	waiting["latest_invoice"] = "in_addon"
	waiting["pending_update"] = map[string]any{"expires_at": time.Now().Add(23 * time.Hour).Unix(), "subscription_items": []any{
		map[string]any{"id": "si_sub_1", "price": stripePriceObject("starter", "month")},
		map[string]any{"price": map[string]any{"id": "price_addon", "lookup_key": app.AddonLookupKey("webhooks_pack", "month")}},
	}}
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, waiting })
	f.set("POST /v1/invoices/in_addon/void", func() (int, any) { return 200, map[string]string{"id": "in_addon", "status": "void"} })

	e.pay("DELETE", "/v1/billing/addons/other_pack", nil, 404, nil) // not the one that waits
	if n := f.called("POST /v1/invoices/in_addon/void"); n != 0 {
		t.Fatalf("another add-on's removal voided the waiting invoice %d times", n)
	}
	e.pay("DELETE", "/v1/billing/addons/webhooks_pack", nil, 204, nil)
	if n := f.called("POST /v1/invoices/in_addon/void"); n != 1 {
		t.Fatalf("the waiting add-on's invoice was voided %d times, want 1: %v", n, f.calls)
	}
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, live }) // the update is gone
	e.pay("DELETE", "/v1/billing/addons/webhooks_pack", nil, 404, nil)
}

// Was a bug (found on the Stripe sandbox): a failed payment for an extra seat or an add-on was told like a failed
// renewal: "the payment for the Plus plan failed" and a day the workspace becomes read-only (the paid period's end
// plus the grace) that staqly doesn't control: Stripe ends the subscription when its retries run out, earlier. The
// email says what failed and promises no day; GET /v1/billing/details tells the Plan page which it is.
func TestFailedSeatPaymentPromisesNoDate(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	owner := w.mustSignIn(ana, "")
	ws := w.session(owner).Workspace.ID
	sub := w.f.sub("sub_a", ws, "active", "")
	w.f.set(w.env, sub, "active")
	w.f.mu.Lock()
	sub["latest_invoice"] = map[string]any{"id": "in_seat", "billing_reason": "subscription_update", "status": "open"}
	w.f.mu.Unlock()
	w.f.set(w.env, sub, "past_due")
	var b app.Billing
	w.as(owner, "GET", "/v1/billing", nil, 200, &b)
	until, _ := time.Parse(time.RFC3339, deref(b.PlanExpiresAt))
	got := w.received("Subject: A payment for your staqly workspace failed", "Subject: The payment for staqly Starter failed")
	if len(got) != 1 {
		t.Fatalf("emails for a failed seat payment: %d\n%s", len(got), strings.Join(got, "\n---\n"))
	}
	for _, want := range []string{"Subject: A payment for your staqly workspace failed", "an extra seat or an add-on", "plan itself is paid", "If it stays unpaid", "becomes read-only"} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("the email lacks %q:\n%s", want, got[0])
		}
	}
	if day := until.Format("2 January 2006"); strings.Contains(got[0], day) || strings.Contains(got[0], "The payment for the") {
		t.Fatalf("the email promises %s or blames the plan:\n%s", day, got[0])
	}
}

// past_due_for in GET /v1/billing/details: what the unpaid invoice of a past_due subscription is for.
func TestBillingDetailsSayWhatIsPastDue(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // cus_1
	sub := subObject("sub_1", ws, "active", "plus", "month")
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, sub })
	empty := func() (int, any) { return 200, map[string]any{"data": []any{}} }
	f.set("GET /v1/customers/cus_1", func() (int, any) { return 200, map[string]string{"id": "cus_1"} })
	f.set("GET /v1/customers/cus_1/payment_methods", empty)
	f.set("GET /v1/invoices", empty)
	for i, c := range []struct{ status, reason, invoice, want string }{
		{"active", "subscription_cycle", "paid", ""},
		{"past_due", "subscription_update", "open", "extra"},
		{"past_due", "subscription_cycle", "open", "renewal"},
		{"past_due", "subscription_cycle", "paid", ""},
	} {
		sub["status"] = c.status
		sub["latest_invoice"] = map[string]any{"id": "in_1", "billing_reason": c.reason, "status": c.invoice}
		if code := e.stripeEvent(fmt.Sprint("evt_", i), "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("event %d: %d", i, code)
		}
		var d app.BillingDetails
		e.do(nil, "GET", "/v1/billing/details", admin, nil, 200, &d)
		if d.Subscription == nil || d.Subscription.PastDueFor != c.want {
			t.Fatalf("%s with a %s %s invoice: %+v, want past_due_for %q", c.status, c.invoice, c.reason, d.Subscription, c.want)
		}
	}
}
