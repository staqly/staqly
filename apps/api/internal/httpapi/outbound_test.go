package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Stripe delivers events out of order (and retries a failed one for days): an update of a subscription that
// was sent before its cancellation, arriving after it, must not bring the paid plan back. A canceled
// subscription stays canceled; paying again is a new subscription.
func TestStripeLateUpdateAfterCancellationIgnored(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	withStripe(t, e)
	ws := workspaceID(e)
	item := map[string]any{"id": "si_1", "quantity": 1, "current_period_end": time.Now().Add(30 * 24 * time.Hour).Unix(),
		"price": stripePriceObject("plus", "month")}
	sub := func(id, status string) map[string]any {
		return map[string]any{"id": id, "customer": "cus_1", "status": status, "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": []any{item}}}
	}
	if code := e.stripeEvent("evt_created", "customer.subscription.created", sub("sub_1", "active"), testWhsec); code != 200 {
		t.Fatalf("created: %d", code)
	}
	if code := e.stripeEvent("evt_deleted", "customer.subscription.deleted", sub("sub_1", "canceled"), testWhsec); code != 200 {
		t.Fatalf("deleted: %d", code)
	}
	if b := e.billing(admin); !b.Suspended || *b.SubscriptionStatus != "canceled" {
		t.Fatalf("after the cancellation: %+v", b)
	}
	// The update Stripe sent before the cancellation, retried late.
	if code := e.stripeEvent("evt_late_update", "customer.subscription.updated", sub("sub_1", "active"), testWhsec); code != 200 {
		t.Fatalf("late update: %d", code)
	}
	if b := e.billing(admin); !b.Suspended || *b.SubscriptionStatus != "canceled" {
		t.Fatalf("a late update revived a canceled subscription: %+v", b)
	}
	// Paying again (a new subscription) works.
	if code := e.stripeEvent("evt_new", "customer.subscription.created", sub("sub_2", "active"), testWhsec); code != 200 {
		t.Fatalf("new subscription: %d", code)
	}
	if b := e.billing(admin); b.Suspended || b.Plan != "plus" || *b.SubscriptionStatus != "active" {
		t.Fatalf("a new subscription after the cancellation: %+v", b)
	}
}

// The inviter's name is theirs to choose (the welcome steps take any text): a name with line breaks and a
// link must not turn into the button of an invitation email that the server sends in its own name.
func TestInvitationEmailInviterNameCannotPlantALink(t *testing.T) {
	e := newAccountsEnv(t, "")
	host, port, inbox := fakeSMTP(t)
	e.svc.Getenv = func(k string) string {
		return map[string]string{"MAIL_SMTP_HOST": host, "MAIL_SMTP_PORT": itoa(port), "MAIL_SMTP_TLS": "none", "MAIL_FROM": "staqly <noreply@example.com>"}[k]
	}
	owner := e.mustSignIn(ana, "")
	e.as(owner, "POST", "/v1/session/onboarded", map[string]any{"name": "Ana\n\nhttps://evil.example/login\n\nPlease sign in"}, 204, nil)
	_, tok := e.invite(owner, bob.Email, app.RoleEditor)
	select {
	case m := <-inbox:
		data := strings.NewReplacer("=\r\n", "", "=\n", "", "=3D", "=").Replace(m.Data)
		if strings.Contains(data, `href="https://evil.example`) {
			t.Fatalf("the inviter's name became a link in the email:\n%s", data)
		}
		if !strings.Contains(data, `href="https://staqly.example.com/invite?token=`+tok) {
			t.Fatalf("the invitation link is not the button:\n%s", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no invitation email")
	}
}

// The webhook worker's poll: due pending deliveries, oldest first, at most perWorkspace from each workspace
// (a long backlog in one can't crowd out the others), never ones not due yet or finished.
func TestDueDeliveriesFairPerWorkspace(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.svc.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().UTC().Add(-time.Hour)
	ts := func(sec int) string {
		return base.Add(time.Duration(sec) * time.Second).Format("2006-01-02T15:04:05.000000Z")
	}
	n := 0
	add := func(ws, hook, status string, sec int) string {
		n++
		ev, id := fmt.Sprintf("evt_f%03d", n), fmt.Sprintf("dlv_f%03d", n)
		exec(`INSERT INTO events (id, workspace_id, type, actor_type, actor_id, actor_name, data, created_at)
			VALUES (?, ?, 'post.created', 'system', 't', 't', '{}', ?)`, ev, ws, ts(sec))
		exec(`INSERT INTO webhook_deliveries (id, workspace_id, webhook_id, event_id, event_type, status, next_attempt_at, created_at)
			VALUES (?, ?, ?, ?, 'post.created', ?, ?, ?)`, id, ws, hook, ev, status, ts(sec), ts(sec))
		return id
	}
	for _, ws := range []string{"ws_fa", "ws_fb", "ws_fc"} {
		exec(`INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at) VALUES (?, ?, '', '', '', '')`, ws, ws)
		exec(`INSERT INTO webhooks (id, workspace_id, url, description, secret, events, collection_ids, active, timeout_ms, max_attempts, created_at, updated_at)
			VALUES (?, ?, 'http://127.0.0.1:1/', '', 's', '["*"]', '[]', 1, 1000, 3, '', '')`, "wh_"+ws, ws)
	}
	a1, a2 := add("ws_fa", "wh_ws_fa", "pending", 1), add("ws_fa", "wh_ws_fa", "pending", 2)
	for i := range 10 { // A's backlog
		add("ws_fa", "wh_ws_fa", "pending", 10+i)
	}
	b1 := add("ws_fb", "wh_ws_fb", "pending", 30)
	add("ws_fb", "wh_ws_fb", "succeeded", 0)  // finished
	add("ws_fc", "wh_ws_fc", "pending", 7200) // not due yet
	add("ws_fc", "wh_ws_fc", "failed", 3)

	due, err := e.svc.Q.DueDeliveries(ctx, ts(3600), 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range due {
		got = append(got, d.ID)
	}
	if want := []string{a1, a2, b1}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("due = %v, want %v", got, want)
	}
	if due, _ := e.svc.Q.DueDeliveries(ctx, ts(3600), 2, 1); len(due) != 1 || due[0].ID != a1 {
		t.Fatalf("limit 1: %+v", due)
	}
	if due, _ := e.svc.Q.DueDeliveries(ctx, ts(3600), 5, 50); len(due) != 6 || due[5].ID != b1 {
		t.Fatalf("5 per workspace: %d", len(due))
	}
}

// Two checkouts started at once (a double click, two tabs) before the workspace has a Stripe customer must
// end up with one customer: the one both sessions bill and the workspace keeps (else the portal, add-ons and
// entitlements look at a customer the subscription isn't on). The fake honours Idempotency-Key like Stripe.
func TestConcurrentFirstCheckoutsMakeOneCustomer(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	var mu sync.Mutex
	byKey, sessions, created := map[string]string{}, map[string]bool{}, 0
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/customers":
			k := r.Header.Get("Idempotency-Key")
			id, ok := byKey[k]
			if !ok || k == "" {
				created++
				id = fmt.Sprintf("cus_%d", created)
				byKey[k] = id
			}
			json.NewEncoder(w).Encode(map[string]string{"id": id})
		case "POST /v1/checkout/sessions":
			sessions[r.Form.Get("customer")] = true
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/s"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL,
		Prices: map[string]string{"pro_month": "price_pro"}}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			e.do(nil, "POST", "/v1/billing/checkout", admin, nil, 200, nil)
		}()
	}
	close(start)
	wg.Wait()
	if len(sessions) != 1 {
		t.Fatalf("checkouts billed %d customers: %v", len(sessions), sessions)
	}
	// The workspace keeps the customer the sessions bill: a later checkout uses it without making another.
	created = 0
	e.do(nil, "POST", "/v1/billing/checkout", admin, nil, 200, nil)
	if created != 0 || len(sessions) != 1 {
		t.Fatalf("stored customer differs from the billed one: %v", sessions)
	}
}

// A STRIPE_PRICE_* pin at a price archived in Stripe (active: false): Checkout refuses inactive prices, so the
// checkout and the shown amount use the plan's live price by its lookup key instead.
func TestPinnedInactivePriceUsesLookupKey(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var used []string
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices/price_old":
			fmt.Fprint(w, `{"id":"price_old","active":false,"unit_amount":100,"currency":"usd"}`)
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_live_plus","active":true,"unit_amount":2900,"currency":"eur"}]}`)
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_p"}`)
		case "POST /v1/checkout/sessions":
			used = append(used, r.Form.Get("line_items[0][price]"))
			if r.Form.Get("line_items[0][price]") == "price_old" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"param":"line_items[0][price]","message":"The price specified is inactive. This field only accepts active prices."}}`)
				return
			}
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/ok"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_inactive", WebhookSecret: testWhsec, APIBase: stripe.URL,
		Prices: map[string]string{"plus_month": "price_old"}}
	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/checkout", e.admin, map[string]string{"plan": "plus", "interval": "month"}, 200, &out)
	if out.URL != "https://checkout.stripe.test/ok" || strings.Join(used, ",") != "price_live_plus" {
		t.Fatalf("checkout with an inactive pin: %q %v", out.URL, used)
	}
	var plans struct{ Data []app.Plan }
	e.do(nil, "GET", "/v1/plans", e.key, nil, 200, &plans)
	if len(plans.Data) == 0 {
		t.Fatal("no plans")
	}
	for _, p := range plans.Data {
		if p.ID == "plus" && p.PriceMonth != 29 {
			t.Fatalf("plus shows %d a month, Stripe charges 29", p.PriceMonth)
		}
	}
}

// A customer.subscription.* event is applied as Stripe has the subscription now, not as its (maybe older)
// payload says; when Stripe can't be read the event fails, is not marked applied, and Stripe's retry works.
func TestStripeSubscriptionEventReadsCurrentState(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	f := withStripe(t, e)
	ws := workspaceID(e)
	sub := func(plan string) map[string]any {
		return map[string]any{"id": "sub_cur", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": 1,
				"current_period_end": time.Now().Add(30 * 24 * time.Hour).Unix(), "price": stripePriceObject(plan, "month")}}}}
	}
	f.set("GET /v1/subscriptions/sub_cur", func() (int, any) { return 500, map[string]any{"error": map[string]string{"message": "down"}} })
	// signedStripe: the payload alone, Stripe's answer comes from the routes above.
	stale := stripeBody("evt_stale", "customer.subscription.updated", sub("business"))
	if code := e.signedStripe(stale, time.Now()); code != 502 {
		t.Fatalf("Stripe unreachable: %d, want 502 (Stripe retries)", code)
	}
	if b := e.billing(admin); b.SubscriptionStatus != nil {
		t.Fatalf("applied without reading Stripe: %+v", b)
	}
	f.set("GET /v1/subscriptions/sub_cur", func() (int, any) { return 200, sub("plus") })
	if code := e.signedStripe(stale, time.Now()); code != 200 {
		t.Fatalf("retry: %d", code)
	}
	if b := e.billing(admin); b.Plan != "plus" || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "active" {
		t.Fatalf("the payload's plan won over Stripe's current one: %+v", b)
	}
}

// A refunded or disputed purchase of extra X posts takes back the refunded share of the posts not used yet:
// partial refunds count up (Stripe's amount_refunded is the total so far), what was already used stays, the
// allowance never goes below it, and a repeated or late event changes nothing more.
func TestCreditRefundsAndDisputesTakeBackUnusedPosts(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	ctx := context.Background()
	creditsLeft := func() int64 {
		t.Helper()
		var x app.XPosts
		e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
		return x.CreditsLeft
	}
	buy := func(cs, pi, posts string) {
		t.Helper()
		session := map[string]any{"id": cs, "mode": "payment", "payment_status": "paid", "payment_intent": pi, "client_reference_id": ws,
			"metadata": map[string]string{"workspace_id": ws, "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": posts}}
		if code := e.stripeEvent("evt_buy_"+cs, "checkout.session.completed", session, testWhsec); code != 200 {
			t.Fatalf("buy: %d", code)
		}
		f.set("GET /v1/checkout/sessions", func() (int, any) { return 200, map[string]any{"data": []any{session}} })
	}
	buy("cs_r1", "pi_r1", "200")
	if _, err := e.svc.DB.ExecContext(ctx, `UPDATE credit_grants SET used = 30 WHERE source = 'stripe:cs_r1'`); err != nil {
		t.Fatal(err)
	}
	if n := creditsLeft(); n != 170 {
		t.Fatalf("before the refund: %d", n)
	}
	charge := func(refunded int) map[string]any {
		return map[string]any{"id": "ch_r1", "object": "charge", "payment_intent": "pi_r1", "amount": 1000, "amount_refunded": refunded, "currency": "eur"}
	}
	// Half refunded: 100 of the 200 posts go back; 30 were used, 70 stay.
	if code := e.stripeEvent("evt_ref_1", "charge.refunded", charge(500), testWhsec); code != 200 {
		t.Fatalf("refund: %d", code)
	}
	if n := creditsLeft(); n != 70 {
		t.Fatalf("after half a refund: %d left, want 70", n)
	}
	for _, id := range []string{"evt_ref_1", "evt_ref_late"} { // the same event again; an older one arriving late
		e.stripeEvent(id, "charge.refunded", charge(250), testWhsec)
	}
	if n := creditsLeft(); n != 70 {
		t.Fatalf("a repeated or older refund changed the credits: %d", n)
	}
	// The rest refunded: only what was used stays (never below it).
	e.stripeEvent("evt_ref_2", "charge.refunded", charge(1000), testWhsec)
	if n := creditsLeft(); n != 0 {
		t.Fatalf("after a full refund: %d left", n)
	}
	var units, used int64
	if err := e.svc.DB.QueryRowContext(ctx, `SELECT units, used FROM credit_grants WHERE source = 'stripe:cs_r1'`).Scan(&units, &used); err != nil || units != 30 || used != 30 {
		t.Fatalf("grant after a full refund: units %d used %d (%v)", units, used, err)
	}

	// A dispute of a whole purchase takes back every unused post of it.
	buy("cs_d1", "pi_d1", "1000")
	f.set("GET /v1/charges/ch_d1", func() (int, any) {
		return 200, map[string]any{"id": "ch_d1", "payment_intent": "pi_d1", "amount": 4000, "amount_refunded": 0}
	})
	dispute := map[string]any{"id": "dp_1", "object": "dispute", "charge": "ch_d1", "payment_intent": "pi_d1", "amount": 4000}
	if code := e.stripeEvent("evt_dp_1", "charge.dispute.created", dispute, testWhsec); code != 200 {
		t.Fatalf("dispute: %d", code)
	}
	if n := creditsLeft(); n != 0 {
		t.Fatalf("after a dispute: %d left", n)
	}
	// A refund of something else (no credits session behind it) is acknowledged and ignored.
	f.set("GET /v1/checkout/sessions", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	if code := e.stripeEvent("evt_ref_other", "charge.refunded", map[string]any{"id": "ch_x", "payment_intent": "pi_x", "amount": 100, "amount_refunded": 100}, testWhsec); code != 200 {
		t.Fatalf("unrelated refund: %d", code)
	}
}

// A webhook whose receiver keeps failing stops taking delivery slots: after a few failures in a row its
// deliveries are postponed (still pending, attempts unchanged: at least once) while the workspace's healthy
// webhook gets everything at once.
func TestWebhookCircuitBreaker(t *testing.T) {
	e := newEnv(t)
	e.svc.Cfg.Webhook.RetryBase, e.svc.Cfg.Webhook.RetryMax = time.Second, time.Hour // retries out of the test's window
	var bad, good atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bad.Add(1)
		w.WriteHeader(503)
	}))
	defer down.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		good.Add(1)
		w.WriteHeader(204)
	}))
	defer up.Close()
	var broken, healthy app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": down.URL, "events": []string{"collection.created"}}, 201, &broken)
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": up.URL, "events": []string{"collection.created"}}, 201, &healthy)
	const n = 20
	for i := range n {
		e.collection(fmt.Sprintf("C%d", i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, func() bool { return good.Load() == n })
	time.Sleep(200 * time.Millisecond) // the poll sees the open breaker by now
	if got := bad.Load(); got > 8 {
		t.Fatalf("the failing receiver was called %d times for %d events; the breaker should stop it after a few", got, n)
	}
	var res struct{ Data []app.Delivery }
	e.do(nil, "GET", "/v1/webhook-deliveries?webhook_id="+broken.ID+"&limit=50", e.key, nil, 200, &res)
	if len(res.Data) != n {
		t.Fatalf("deliveries: %d", len(res.Data))
	}
	tried := 0
	for _, d := range res.Data {
		if d.Status != "pending" {
			t.Fatalf("a postponed delivery must stay pending: %+v", d)
		}
		tried += int(d.Attempts)
	}
	if tried != int(bad.Load()) {
		t.Fatalf("attempts %d, receiver calls %d: postponing must not count as an attempt", tried, bad.Load())
	}
}

// One alert endpoint that hangs must not hold up the others: the worker keeps sending to the healthy
// channel (a new event, a later pass) while the hanging send is still waiting for its timeout.
func TestAlertsHangingChannelDoesNotStallOthers(t *testing.T) {
	fake := newFakeChat(t)
	e, vars := newNotifyEnv(t, fake)
	vars.mu.Lock()
	vars.m["NOTIFY_POLL_INTERVAL"] = "10ms"
	vars.mu.Unlock()
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer hang.Close()
	defer close(release)
	var got atomic.Int32
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got.Add(1) }))
	defer ok.Close()
	for name, u := range map[string]string{"Hangs": hang.URL + "/hook", "Works": ok.URL + "/hook"} {
		e.channel(map[string]any{"name": name, "type": "slack", "digest_window_seconds": 0, "events": []string{"collection.created"},
			"config": map[string]any{"webhook_url": u}}, 201)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunNotifyWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	e.collection("First")
	waitFor(t, func() bool { return got.Load() == 1 })
	e.collection("Second") // the hanging send of the first is still open
	deadline := time.Now().Add(3 * time.Second)
	for got.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() < 2 {
		t.Fatal("the healthy channel waited for the hanging one")
	}
}

// In cloud mode an alert's error never names the address a host resolved to (it would map the operator's
// network for a tenant), for refused destinations and failed connections alike; webhooks likewise.
func TestOutboundErrorsHideResolvedAddressesInCloud(t *testing.T) {
	fake := newFakeChat(t)
	e, vars := newNotifyEnv(t, fake)
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	port := closed.Listener.Addr().(*net.TCPAddr).Port
	closed.Close() // nothing listens there now
	alertErr := func(name string) string {
		t.Helper()
		ch := e.channel(map[string]any{"name": name, "type": "slack", "digest_window_seconds": 0, "events": []string{"collection.created"},
			"config": map[string]any{"webhook_url": fmt.Sprintf("http://localhost:%d/hook", port)}}, 201)
		e.collection("For " + name)
		e.process()
		var a app.Alert
		e.do(e.browser, "GET", "/v1/alerts/"+ch.ID, "", nil, 200, &a)
		e.do(e.browser, "DELETE", "/v1/alerts/"+ch.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
		return a.LastError
	}
	check := func(what, msg string) {
		t.Helper()
		if msg == "" || strings.Contains(msg, "127.0.0.1") || strings.Contains(msg, "::1") {
			t.Fatalf("%s: %q", what, msg)
		}
	}
	check("connection refused", alertErr("Refused")) // loopback allowed by the env, nothing listening
	vars.mu.Lock()
	vars.m["NOTIFY_ALLOW_LOOPBACK"] = ""
	vars.mu.Unlock()
	msg := alertErr("Denied")
	check("refused destination", msg)
	if !strings.Contains(msg, "loopback") {
		t.Fatalf("the reason is still named: %q", msg)
	}
	mail := e.channel(map[string]any{"name": "Mail", "type": "email", "digest_window_seconds": 0, "events": []string{"collection.created"},
		"config": map[string]any{"host": "localhost", "port": port, "tls": "none", "from": "a@example.com", "to": []string{"b@example.com"}}}, 201)
	e.collection("For mail")
	e.process()
	var a app.Alert
	e.do(e.browser, "GET", "/v1/alerts/"+mail.ID, "", nil, 200, &a)
	check("smtp refused destination", a.LastError)

	e.svc.Cfg.Webhook.AllowLoopback = false
	one := int64(1)
	u := fmt.Sprintf("http://localhost:%d/hook", port)
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.admin, map[string]any{"url": u, "max_attempts": one}, 201, &wh)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	e.do(nil, "POST", "/v1/webhooks/"+wh.ID+"/test", e.admin, nil, 202, nil)
	var d []app.Delivery
	waitFor(t, func() bool {
		var res struct{ Data []app.Delivery }
		e.do(nil, "GET", "/v1/webhook-deliveries?webhook_id="+wh.ID+"&status=failed", e.admin, nil, 200, &res)
		d = res.Data
		return len(d) == 1
	})
	check("webhook refused destination", d[0].LastError)
}
