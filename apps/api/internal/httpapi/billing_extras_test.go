package httpapi_test

import (
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

// addonStripe is a Stripe with one Starter subscription (sub_1 of cus_1) and one add-on on sale (webhooks_pack),
// answering as the sandbox did (Stripe billing_mode=flexible): an item added while the card declines waits as a
// pending update with payment_behavior=pending_if_incomplete, and without it joins the subscription at once,
// leaving its invoice open and the subscription past_due.
type addonStripe struct {
	mu       sync.Mutex
	status   string
	addon    bool // the add-on's item is on the subscription
	declines bool // the card declines
	pending  bool // an unpaid change waits (the subscription's pending update)
	// invoices: the subscription's open invoices; voidFails: Stripe refuses to void them.
	invoices  []map[string]any
	voidFails bool
	forms     []url.Values // what added an item
	removed   []string     // proration_behavior of each removed item
	voided    []string
	calls     []string
}

func newAddonStripe(t *testing.T, e *env) *addonStripe {
	t.Helper()
	ws := workspaceID(e)
	f := &addonStripe{status: "active"}
	addonPrice := map[string]any{"id": "price_addon_webhooks_pack_month", "lookup_key": "staqly_addon_webhooks_pack_month",
		"recurring": map[string]any{"interval": "month"}}
	planItem := map[string]any{"id": "si_plan", "quantity": 1, "current_period_end": time.Now().Add(20 * 24 * time.Hour).Unix(),
		"price": stripePriceObject("starter", "month")}
	invoice := map[string]any{"id": "in_addon", "status": "open", "payment_intent": "pi_1",
		"hosted_invoice_url": "https://invoice.stripe.com/i/acct_1/test_in_addon",
		"lines":              map[string]any{"data": []any{map[string]any{"subscription_item": "si_addon"}}}}
	sub := func() map[string]any {
		items := []any{planItem}
		if f.addon {
			items = append(items, map[string]any{"id": "si_addon", "quantity": 1, "price": addonPrice})
		}
		out := map[string]any{"id": "sub_1", "customer": "cus_1", "status": f.status, "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": items}}
		if f.pending {
			out["pending_update"] = map[string]any{"expires_at": 1893456000, "subscription_items": []any{
				map[string]any{"id": "si_plan", "price": planItem["price"]}, map[string]any{"price": addonPrice}}}
			out["latest_invoice"] = invoice
		}
		return out
	}
	srv := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		route := r.Method + " " + r.URL.Path
		f.calls = append(f.calls, route)
		enc := json.NewEncoder(w)
		switch route {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":900,"currency":"eur"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/products":
			enc.Encode(map[string]any{"data": []any{map[string]any{"name": "Webhooks pack",
				"metadata": map[string]string{"staqly_kind": "addon", "staqly_addon": "webhooks_pack"}}}})
		case "GET /v1/subscriptions/sub_1":
			enc.Encode(sub())
		case "POST /v1/subscriptions/sub_1", "POST /v1/subscription_items":
			f.forms = append(f.forms, r.Form)
			switch {
			case !f.declines:
				f.addon, f.pending = true, false
			case r.Form.Get("payment_behavior") == "pending_if_incomplete":
				f.pending = true
			default:
				f.addon, f.status, f.invoices = true, "past_due", append(f.invoices, invoice)
			}
			enc.Encode(sub())
		case "GET /v1/payment_intents/pi_1":
			fmt.Fprint(w, `{"id":"pi_1","status":"requires_payment_method","last_payment_error":{"code":"card_declined","decline_code":"insufficient_funds"}}`)
		case "GET /v1/invoices":
			if q := r.URL.Query(); q.Get("subscription") != "sub_1" || q.Get("status") != "open" {
				t.Errorf("invoices read: %v", q)
			}
			enc.Encode(map[string]any{"data": f.invoices})
		case "DELETE /v1/subscription_items/si_addon":
			f.removed = append(f.removed, r.URL.Query().Get("proration_behavior"))
			f.addon = false
			fmt.Fprint(w, `{"id":"si_addon","deleted":true}`)
		default:
			id, ok := strings.CutPrefix(r.URL.Path, "/v1/invoices/")
			if id, ok = strings.CutSuffix(id, "/void"); !ok || r.Method != "POST" || f.voidFails {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"message":"fake: unhandled"}}`)
				return
			}
			f.voided = append(f.voided, id)
			for i, inv := range f.invoices {
				if inv["id"] == id {
					f.invoices = append(f.invoices[:i:i], f.invoices[i+1:]...)
					break
				}
			}
			fmt.Fprint(w, `{"status":"void"}`)
		}
	}))
	t.Cleanup(srv.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_addon", WebhookSecret: testWhsec, APIBase: srv.URL}
	f.mu.Lock()
	first := sub()
	f.mu.Unlock()
	if code := e.stripeEvent("evt_addon_sub", "customer.subscription.created", first, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	return f
}

// Was a bug: an add-on bought while the card declines answered 204, joined the subscription all the same and
// left it past_due with an open invoice (Stripe cancels the whole subscription when that stays unpaid), and the
// pending payment said nothing waited. It is charged like an upgrade: added only once paid, 402 until then.
func TestAddonWithADecliningCard(t *testing.T) {
	e := newEnv(t)
	f := newAddonStripe(t, e)
	admin := e.keyWith(app.ScopeAdmin)
	f.mu.Lock()
	f.declines = true
	f.mu.Unlock()

	var errBody paymentErr
	e.do(nil, "POST", "/v1/billing/addons/webhooks_pack", admin, nil, 402, &errBody)
	d := errBody.Error.Details
	if errBody.Error.Code != "payment_failed" || d["reason"] != "insufficient_funds" || d["addon"] != "webhooks_pack" || d["plan"] != "" ||
		d["pay_url"] != "https://invoice.stripe.com/i/acct_1/test_in_addon" || !strings.Contains(errBody.Error.Message, "your subscription stays as it was") {
		t.Fatalf("402: %+v", errBody.Error)
	}
	f.mu.Lock()
	form, status, has := f.forms[len(f.forms)-1], f.status, f.addon
	f.mu.Unlock()
	if form.Get("payment_behavior") != "pending_if_incomplete" || form.Get("proration_behavior") != "always_invoice" ||
		form.Get("items[0][price]") != "price_addon_webhooks_pack_month" || form.Get("items[0][id]") != "" {
		t.Fatalf("add-on request: %v", form)
	}
	if status != "active" || has {
		t.Fatalf("an unpaid add-on changed the subscription: status %s, add-on %v", status, has)
	}
	var list struct{ Data []app.Addon }
	e.do(nil, "GET", "/v1/billing/addons", admin, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].Active {
		t.Fatalf("an unpaid add-on is not active: %+v", list.Data)
	}
	var pp app.PendingPayment
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	if !pp.Pending || pp.Addon != "webhooks_pack" || pp.Plan != "" || pp.Reason != app.PaymentInsufficientFunds || pp.PayURL != d["pay_url"] {
		t.Fatalf("pending payment: %+v", pp)
	}
	if b := e.billing(admin); *b.SubscriptionStatus != "active" || b.Suspended {
		t.Fatalf("billing: %+v", b)
	}

	// The card works again: the add-on joins.
	f.mu.Lock()
	f.declines, f.pending = false, false
	f.mu.Unlock()
	e.do(nil, "POST", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	pp = app.PendingPayment{}
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	e.do(nil, "GET", "/v1/billing/addons", admin, nil, 200, &list)
	if pp.Pending || !list.Data[0].Active {
		t.Fatalf("after paying: %+v %+v", pp, list.Data)
	}
}

// Was a bug: removing an add-on that was never paid (from before add-ons waited for their payment: the
// subscription is past_due) left its invoice open and credited the unused time, so the customer owed for an
// add-on they no longer had. Its own invoices are voided and nothing is credited; a paid add-on is credited at
// once; an invoice Stripe won't void leaves the credit as before.
func TestRemovingAnUnpaidAddon(t *testing.T) {
	e := newEnv(t)
	f := newAddonStripe(t, e)
	admin := e.keyWith(app.ScopeAdmin)
	line := func(item string) map[string]any { // a line as Stripe API 2025-03-31 and later carries its item
		return map[string]any{"parent": map[string]any{"subscription_item_details": map[string]any{"subscription_item": item}}}
	}
	unpaid := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.addon, f.status = true, "past_due"
		f.invoices = []map[string]any{
			{"id": "in_renewal", "lines": map[string]any{"data": []any{line("si_plan"), line("si_addon")}}},
			{"id": "in_addon", "lines": map[string]any{"data": []any{line("si_addon")}}},
			{"id": "in_addon_old", "lines": map[string]any{"data": []any{map[string]any{"subscription_item": "si_addon"}}}},
		}
	}
	state := func() (removed, voided []string, reads int) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, c := range f.calls {
			if c == "GET /v1/invoices" {
				reads++
			}
		}
		return f.removed, f.voided, reads
	}

	unpaid()
	e.do(nil, "DELETE", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	removed, voided, _ := state()
	if fmt.Sprint(removed) != "[none]" || fmt.Sprint(voided) != "[in_addon in_addon_old]" {
		t.Fatalf("unpaid add-on: removed %v, voided %v", removed, voided)
	}

	// A paid one: credited at once, no invoice is looked at.
	f.mu.Lock()
	f.addon, f.status, f.invoices = true, "active", nil
	f.mu.Unlock()
	_, _, before := state()
	e.do(nil, "DELETE", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	removed, voided, reads := state()
	if fmt.Sprint(removed) != "[none always_invoice]" || len(voided) != 2 || reads != before {
		t.Fatalf("paid add-on: removed %v, voided %v, %d invoice reads", removed, voided, reads-before)
	}

	// Stripe refuses to void: the removal goes through with its credit.
	unpaid()
	f.mu.Lock()
	f.voidFails = true
	f.mu.Unlock()
	e.do(nil, "DELETE", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	if removed, voided, _ = state(); fmt.Sprint(removed) != "[none always_invoice always_invoice]" || len(voided) != 2 {
		t.Fatalf("invoice not voided: removed %v, voided %v", removed, voided)
	}
}

// Was a bug: the credit of a downgrade or a removed add-on stayed as pending invoice items (create_prorations),
// and with billing_mode=flexible the invoice of a pending update (an upgrade) leaves those out, so a later upgrade
// was charged in full while the credit waited for the renewal, up to a year away. Each credit is invoiced at once:
// its negative total is the customer's balance, which Stripe takes off the next invoice. The fake keeps Stripe's
// books that way (a removed seat goes the same way: TestSeatsAreBilledPerMember).
func TestCreditsBecomeBalanceAtOnce(t *testing.T) {
	e := newEnv(t)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	var mu sync.Mutex
	plan, addon := "business", true
	var pendingItems, credit, charged int64 // cents: pending invoice items, the customer's balance, what the card paid
	total := func() int64 {
		n := map[string]int64{"starter": 1000, "plus": 3000, "business": 10000}[plan]
		if addon {
			n += 900
		}
		return n
	}
	// settle books a change of the period's price by delta, as Stripe does for the proration behaviour.
	settle := func(delta int64, behavior string, waits bool) {
		switch behavior {
		case "always_invoice":
			if !waits { // the invoice of a pending update leaves pending invoice items out
				delta, pendingItems = delta+pendingItems, 0
			}
			if delta < 0 {
				credit -= delta
				return
			}
			used := min(credit, delta)
			credit, charged = credit-used, charged+delta-used
		case "none":
		default:
			pendingItems += delta
		}
	}
	sub := func() map[string]any {
		s := subObject("sub_1", ws, "active", plan, "month")
		if addon {
			items := s["items"].(map[string]any)
			items["data"] = append(items["data"].([]any), map[string]any{"id": "si_addon", "quantity": 1,
				"price": map[string]any{"id": "price_addon", "lookup_key": "staqly_addon_webhooks_pack_month", "recurring": map[string]any{"interval": "month"}}})
		}
		return s
	}
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		before := total()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub())
		case "POST /v1/subscriptions/sub_1":
			plan = strings.TrimSuffix(strings.TrimPrefix(r.Form.Get("items[0][price]"), "price_"), "_month")
			settle(total()-before, r.Form.Get("proration_behavior"), r.Form.Get("payment_behavior") == "pending_if_incomplete")
			fmt.Fprint(w, `{"id":"sub_1"}`)
		case "DELETE /v1/subscription_items/si_addon":
			addon = false
			settle(total()-before, r.URL.Query().Get("proration_behavior"), false)
			fmt.Fprint(w, `{"id":"si_addon","deleted":true}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_credit", WebhookSecret: testWhsec, APIBase: stripe.URL}
	mu.Lock()
	first := sub()
	mu.Unlock()
	e.stripeEvent("evt_credit_1", "customer.subscription.created", first, testWhsec)

	// Business to Starter, then the add-on removed: 90.00 and 9.00 of credit.
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "starter", "interval": "month"}, 200, nil)
	e.do(nil, "DELETE", "/v1/billing/addons/webhooks_pack", admin, nil, 204, nil)
	mu.Lock()
	if pendingItems != 0 || credit != 9900 || charged != 0 {
		t.Errorf("after the credits: %d pending, balance %d, charged %d (want 0, 9900, 0)", pendingItems, credit, charged)
	}
	mu.Unlock()
	// The upgrade to Plus (20.00 more) is paid from the balance, not by the card.
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "plus", "interval": "month"}, 200, nil)
	mu.Lock()
	defer mu.Unlock()
	if pendingItems != 0 || credit != 7900 || charged != 0 {
		t.Fatalf("after the upgrade: %d pending, balance %d, charged %d (want 0, 7900, 0)", pendingItems, credit, charged)
	}
}

// Was a bug: Stripe starts the next period (as active) about an hour before it charges the renewal, which moved
// plan_expires_at a period ahead, and the past_due that followed kept it: an unpaid workspace worked a period and
// the grace for free where Stripe is set to leave subscriptions past_due. An unpaid renewal puts the end back to
// where the paid period stopped (plus the grace); an unpaid invoice of another kind doesn't touch a paid period.
func TestFailedRenewalDoesNotExtendThePlan(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	const day = 24 * time.Hour
	renewed := time.Now().Truncate(time.Second)
	n := 0
	// apply: the subscription in a status, in the period that started at start, with its latest invoice.
	apply := func(status string, start time.Time, reason, invoice string) time.Time {
		t.Helper()
		n++
		sub := subObject("sub_1", ws, status, "starter", "month")
		item := sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)
		item["current_period_start"], item["current_period_end"] = start.Unix(), start.Add(30*day).Unix()
		sub["latest_invoice"] = map[string]any{"id": fmt.Sprint("in_", n), "billing_reason": reason, "status": invoice}
		if code := e.stripeEvent(fmt.Sprint("evt_renewal_", n), "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("%s event: %d", status, code)
		}
		b := e.billing(admin)
		if b.Plan != "starter" || b.PlanExpiresAt == nil {
			t.Fatalf("%s: %+v", status, b)
		}
		end, err := time.Parse(time.RFC3339, *b.PlanExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		return end
	}
	grace := 3 * day
	if end := apply("active", renewed.Add(-30*day), "subscription_cycle", "paid"); !end.Equal(renewed.Add(grace)) {
		t.Fatalf("the paid period: ends %s, want %s", end, renewed.Add(grace))
	}
	if end := apply("active", renewed, "subscription_cycle", "draft"); !end.Equal(renewed.Add(30*day + grace)) {
		t.Fatalf("the new period: ends %s", end)
	}
	if end := apply("past_due", renewed, "subscription_cycle", "open"); !end.Equal(renewed.Add(grace)) {
		t.Fatalf("an unpaid renewal: the plan ends %s, want the paid period's end plus the grace, %s", end, renewed.Add(grace))
	}
	if b := e.billing(admin); b.Suspended || *b.SubscriptionStatus != "past_due" {
		t.Fatalf("within the grace: %+v", b)
	}
	if end := apply("past_due", renewed, "subscription_cycle", "open"); !end.Equal(renewed.Add(grace)) { // told again
		t.Fatalf("an unpaid renewal told again: %s", end)
	}
	if end := apply("active", renewed, "subscription_cycle", "paid"); !end.Equal(renewed.Add(30*day + grace)) {
		t.Fatalf("paid after all: ends %s", end)
	}
	// A seat added mid-period whose invoice the card declines: the period itself is paid.
	if end := apply("past_due", renewed, "subscription_update", "open"); !end.Equal(renewed.Add(30*day + grace)) {
		t.Fatalf("an unpaid seat took the paid period away: ends %s", end)
	}
}

// The owners are told once per failure that a payment failed, and once that the subscription ended for it and the
// workspace is read-only; a payment put right, and a subscription the customer cancelled, send nothing. GET
// /v1/billing says why the subscription ended (ended_for).
func TestPaymentFailureEmails(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	owner := w.mustSignIn(ana, "")
	ws := w.session(owner).Workspace.ID
	sub := w.f.sub("sub_a", ws, "active", "")
	w.f.set(w.env, sub, "active")
	failed, ended := "Subject: The payment for staqly Starter failed", "Subject: Your staqly workspace is read-only"
	expect := func(what string, n int, want ...string) {
		t.Helper()
		got := w.received(failed, ended)
		if len(got) != n {
			t.Fatalf("%s: %d emails, want %d:\n%s", what, len(got), n, strings.Join(got, "\n---\n"))
		}
		for _, m := range got {
			for _, s := range want {
				if !strings.Contains(m, s) {
					t.Fatalf("%s: email lacks %q:\n%s", what, s, m)
				}
			}
		}
	}
	billing := func() app.Billing {
		t.Helper()
		var b app.Billing
		w.as(owner, "GET", "/v1/billing", nil, 200, &b)
		return b
	}
	expect("paying", 0)

	// The payment fails; Stripe tells it twice. One email, with the day the workspace becomes read-only.
	w.f.set(w.env, sub, "past_due")
	w.f.set(w.env, sub, "past_due")
	b := billing()
	until, err := time.Parse(time.RFC3339, *b.PlanExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	expect("payment failed", 1, "<ana@example.com>", failed, "didn't go through", "Stripe tries the card again",
		until.Format("2 January 2006"), "becomes read-only", "Update the card: https://staqly.example.com/settings/plan")
	if b.EndedFor != nil {
		t.Fatalf("past_due has not ended: %+v", b)
	}
	w.f.set(w.env, sub, "past_due") // Stripe's retries tell the same state for days
	expect("still past_due", 0)

	// Paid: nothing. A failure put right before its email went out: nothing either.
	w.f.set(w.env, sub, "active")
	expect("recovered", 0)
	w.f.set(w.env, sub, "past_due")
	w.f.set(w.env, sub, "active")
	expect("failed and paid at once", 0)

	// It fails again and Stripe gives up: one email for the failure, one for the end.
	w.f.set(w.env, sub, "past_due")
	expect("a second failure", 1, failed)
	w.f.mu.Lock()
	sub["cancellation_details"] = map[string]any{"reason": "payment_failed"}
	w.f.mu.Unlock()
	w.f.set(w.env, sub, "canceled")
	w.f.set(w.env, sub, "canceled")
	expect("ended for non-payment", 1, "<ana@example.com>", ended, "kept failing", "The workspace is read-only",
		"Choose a plan: https://staqly.example.com/settings/plan")
	if b := billing(); b.EndedFor == nil || *b.EndedFor != "payment_failed" || !b.Suspended {
		t.Fatalf("ended for a failed payment: %+v", b)
	}
	w.f.set(w.env, sub, "canceled") // a sync or a replayed event, after the email went out
	expect("ended, told again", 0)

	// A new subscription clears it; one the customer cancels ends without a reason and without an email.
	again := w.f.sub("sub_b", ws, "active", "")
	w.f.set(w.env, again, "active")
	if b := billing(); b.EndedFor != nil || b.Suspended {
		t.Fatalf("subscribed again: %+v", b)
	}
	w.f.mu.Lock()
	again["cancellation_details"] = map[string]any{"reason": "cancellation_requested"}
	w.f.mu.Unlock()
	w.f.set(w.env, again, "canceled")
	expect("cancelled by the customer", 0)
	if b := billing(); b.EndedFor != nil || !b.Suspended {
		t.Fatalf("cancelled by the customer: %+v", b)
	}
}
