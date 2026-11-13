package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Verification of the billing audit of 2026-11-13: each test states what should hold and fails while the defect
// it describes is in the code. None changes product code.

// Audit 1: a plan checkout left open is paid after its workspace was deleted. Nothing closes the checkout at
// deletion, and the subscription it starts is ignored (the workspace is gone), so it bills with nobody to cancel it.
func TestAuditCheckoutPaidAfterWorkspaceDeleted(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	ok := func() (int, any) { return 200, map[string]string{} }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 200, nil) // cus_1, a checkout left open
	f.set("GET /v1/checkout/sessions", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]string{"id": "cs_open", "mode": "subscription"}}}
	})
	f.set("POST /v1/checkout/sessions/cs_open/expire", ok)
	f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": []any{}} }) // none yet
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, "X-CSRF-Token", e.csrf)

	// The open checkout is paid in its tab.
	late := subObject("sub_late", ws, "active", "pro", "month")
	f.set("GET /v1/subscriptions/sub_late", func() (int, any) { return 200, late })
	f.set("DELETE /v1/subscriptions/sub_late", ok)
	if code := e.stripeEvent("evt_cs", "checkout.session.completed", map[string]any{"id": "cs_open", "subscription": "sub_late", "customer": "cus_1"}, testWhsec); code != 200 {
		t.Fatalf("checkout.session.completed: %d", code)
	}
	if code := e.stripeEvent("evt_sub", "customer.subscription.created", late, testWhsec); code != 200 {
		t.Fatalf("customer.subscription.created: %d", code)
	}
	if expired, cancelled := f.called("POST /v1/checkout/sessions/cs_open/expire"), f.called("DELETE /v1/subscriptions/sub_late"); expired == 0 && cancelled == 0 {
		t.Fatalf("a checkout paid after its workspace was deleted keeps billing: the open checkout was expired %d times at deletion, the new subscription cancelled %d times (calls: %v)",
			expired, cancelled, f.calls)
	}
}

// Audit 2: the customer's newest subscription is a cancelled duplicate (B); the kept one (A) renewed at Stripe
// but its webhook was lost. The sync reads only the newest, so A is never refreshed and the paying workspace
// stays read-only.
func TestAuditSyncRefreshesKeptSubscriptionBehindCancelledDuplicate(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	ok := func() (int, any) { return 200, map[string]string{} }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 200, nil) // cus_1

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

	// At Stripe A renewed (the webhook never came). The list is newest first: with limit=1 it is B, cancelled.
	renewedA := withEnd(subObject("sub_A", ws, "active", "pro", "month"), time.Now().Add(26*day))
	f.set("GET /v1/subscriptions/sub_A", func() (int, any) { return 200, renewedA })
	f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": []any{cancelledB}} })
	readsBefore := f.called("GET /v1/subscriptions/sub_A")
	e.do(nil, "POST", "/v1/billing/sync", admin, nil, 200, nil)
	if n := e.svc.ReconcileBilling(context.Background()); n != 1 {
		t.Fatalf("the reconcile round synced %d workspaces, want 1", n)
	}
	if b := e.billing(admin); b.Suspended {
		t.Fatalf("a paying workspace stays read-only after a sync and a reconcile round: the kept subscription sub_A (renewed at Stripe) was read %d times after the renewal, plan_expires_at %s",
			f.called("GET /v1/subscriptions/sub_A")-readsBefore, deref(b.PlanExpiresAt))
	}
}

// Audit 3: a yearly plan with a monthly add-on (what monthly to yearly leaves behind: switchSubscription moves the
// plan's item only). The add-on's monthly renewal fails; the year is paid, so the plan must not end.
func TestAuditFailedAddonRenewalKeepsThePaidYear(t *testing.T) {
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

// Audit 4: a seat is removed while Stripe is down for all three attempts of the seat sync. Nothing queues it
// again while the plan's paid period is still ahead: Stripe keeps billing the seat, and the credit is not given.
func TestAuditSeatSyncIsRepairedAfterItsJobFailed(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	ws := e.session(owner).Workspace.ID
	ctx := context.Background()
	var mu sync.Mutex
	quantity, down, failed := int64(1), false, 0
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		r.ParseForm()
		sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": quantity,
				"current_period_start": time.Now().Add(-10 * 24 * time.Hour).Unix(), "current_period_end": time.Now().Add(355 * 24 * time.Hour).Unix(),
				"price": stripePriceObject("plus", "year")}}}}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_plus_year"}]}`)
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "POST /v1/checkout/sessions":
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "GET /v1/subscriptions":
			json.NewEncoder(w).Encode(map[string]any{"data": []any{sub}})
		case "POST /v1/subscriptions/sub_1":
			if down {
				failed++
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"message":"down"}}`)
				return
			}
			fmt.Sscan(r.Form.Get("items[0][quantity]"), &quantity)
			fmt.Fprint(w, `{"id":"sub_1"}`)
		case "GET /v1/entitlements/active_entitlements", "GET /v1/entitlements/features":
			fmt.Fprint(w, `{"data":[]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	read := func() (q int64, f int) { mu.Lock(); defer mu.Unlock(); return quantity, failed }

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
	if q, _ := read(); q != 2 {
		t.Fatalf("setup: Bob joined, Stripe bills %d seats", q)
	}

	// Stripe goes down; Bob is removed; the seat sync uses up its three attempts (1 and 5 minutes apart).
	mu.Lock()
	down = true
	mu.Unlock()
	var members struct{ Data []app.Member }
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	for _, m := range members.Data {
		if m.Email == bob.Email {
			e.as(owner, "DELETE", "/v1/members/"+m.UserID, nil, 204, nil)
		}
	}
	clock := time.Now()
	e.svc.Now = func() time.Time { return clock }
	defer func() { e.svc.Now = time.Now }()
	for range 5 {
		e.svc.RunDueJobsOnce(ctx)
		clock = clock.Add(20 * time.Minute)
	}
	if _, f := read(); f != 3 {
		t.Fatalf("setup: the seat sync was tried %d times, want all 3 attempts", f)
	}

	// Stripe is back. Reconcile rounds and the job queue run for a day; the plan's year is still ahead.
	mu.Lock()
	down = false
	mu.Unlock()
	for range 3 {
		clock = clock.Add(8 * time.Hour)
		e.svc.ReconcileBilling(ctx)
		e.svc.RunDueJobsOnce(ctx)
	}
	if q, _ := read(); q != 1 {
		t.Fatalf("a day after Stripe came back the removed seat is still billed: Stripe has %d seats for 1 member (nothing queued the seat sync again)", q)
	}
}

// Audit 6: a feature made at Stripe while the feature catalog is cached (10 minutes) and granted to a customer
// right away is stored without its metadata, so the limit it raises never applies, even after the cache expired.
func TestAuditFeatureNewerThanTheCatalogCacheGetsItsAllowance(t *testing.T) {
	e := newEnv(t)
	ws := workspaceID(e)
	ctx := context.Background()
	admin := e.keyWith(app.ScopeAdmin)
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
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "starter", "interval": "month"}, 200, nil)
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
	got := webhooks()
	// The catalog cache has long expired; jobs and reconcile rounds ran.
	clock := time.Now().Add(time.Hour)
	e.svc.Now = func() time.Time { return clock }
	defer func() { e.svc.Now = time.Now }()
	e.svc.ReconcileBilling(ctx)
	e.svc.RunDueJobsOnce(ctx)
	later := webhooks()
	if got != base+2 || later != base+2 {
		t.Fatalf("a paid feature granted within the catalog cache's 10 minutes has no allowance: webhooks limit %d after the sync, %d an hour later, want %d (plan %d + 2)",
			got, later, base+2, base)
	}
}

// Audit 7: StartPaidPlan applies the subscription Stripe answered with, outside the customer's billing lock. An
// answer that stalls is applied over a newer state a webhook brought meanwhile.
func TestAuditStartPaidPlanDoesNotApplyAStaleSnapshot(t *testing.T) {
	e := newEnv(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var subs map[string]any // sub_c as Stripe has it
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
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

	done := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/start-paid-plan", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		resp.Body.Close()
		done <- resp.StatusCode
	}()
	<-arrived
	// Meanwhile the subscription moves to Business (another owner's upgrade, paid) and its webhook is applied.
	mu.Lock()
	subs = object("active", "business")
	mu.Unlock()
	if code := e.stripeEvent("evt_business", "customer.subscription.updated", object("active", "business"), testWhsec); code != 200 {
		t.Fatalf("upgrade event: %d", code)
	}
	if b := e.billingOf(key); b.Plan != "business" {
		t.Fatalf("setup: the upgrade is applied: %+v", b)
	}
	close(release)
	if code := <-done; code != 200 {
		t.Fatalf("start-paid-plan: %d", code)
	}
	if b := e.billingOf(key); b.Plan != "business" {
		t.Fatalf("a stalled StartPaidPlan answer overwrote the newer plan: the workspace is on %q, Stripe bills business", b.Plan)
	}
}

// Audit 8: the operator comps Business until a date to a workspace with an active Starter subscription. Stripe
// telling that same subscription again (a seat sync, a renewal) puts Starter and the subscription's date back.
func TestAuditOperatorPlanSurvivesActiveSubscriptionToldAgain(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	e.stripeEvent("evt_1", "customer.subscription.created", subObject("sub_1", ws, "active", "starter", "month"), testWhsec)
	if b := e.billing(admin); b.Plan != "starter" {
		t.Fatalf("setup: %+v", b)
	}
	until := time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := e.svc.SetPlan(context.Background(), ws, "business", until.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if b := e.billing(admin); b.Plan != "business" {
		t.Fatalf("setup: the operator's plan: %+v", b)
	}
	if code := e.stripeEvent("evt_2", "customer.subscription.updated", subObject("sub_1", ws, "active", "starter", "month"), testWhsec); code != 200 {
		t.Fatalf("event: %d", code)
	}
	b := e.billing(admin)
	end, _ := time.Parse(time.RFC3339, deref(b.PlanExpiresAt))
	if b.Plan != "business" || !end.Equal(until) {
		t.Fatalf("the operator's plan (business until %s) was undone by the same active subscription told again: plan %q until %s", until, b.Plan, end)
	}
}
