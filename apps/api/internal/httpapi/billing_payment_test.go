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

// fakeTrialStripe is a Stripe with one trialing Starter subscription (sub_t) whose plan switch stays pending:
// the subscription update answers with the pending update and its invoice (in the form the test picks), and
// the invoice's payment intent with the status and error the test picks.
type fakeTrialStripe struct {
	mu sync.Mutex
	// invoice: latest_invoice as the update and the subscription read return it.
	invoice map[string]any
	// payments: GET /v1/invoice_payments (Stripe API 2025-03-31 on); nil answers 404.
	payments []any
	// intent: GET /v1/payment_intents/pi_1.
	intent  map[string]any
	pending bool
	updated url.Values
	// reads: the subscription reads with their expand parameters.
	reads []string
}

func newFakeTrialStripe(t *testing.T, e *env, wsID string) *fakeTrialStripe {
	t.Helper()
	end := time.Now().Add(7 * 24 * time.Hour).Unix()
	sub := map[string]any{"id": "sub_t", "customer": "cus_t", "status": "trialing", "metadata": map[string]string{"workspace_id": wsID},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_t", "current_period_end": end, "price": stripePriceObject("starter", "month")}}}}
	f := &fakeTrialStripe{pending: true}
	pendingUpdate := func() any {
		if !f.pending {
			return nil
		}
		return map[string]any{"expires_at": 1893456000, "subscription_items": []any{map[string]any{"id": "si_t", "price": stripePriceObject("pro", "month")}}}
	}
	srv := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/subscriptions/sub_t":
			f.reads = append(f.reads, r.URL.Query().Get("expand[]"))
			out := map[string]any{}
			for k, v := range sub {
				out[k] = v
			}
			out["pending_update"] = pendingUpdate()
			if r.URL.Query().Get("expand[]") == "latest_invoice" {
				out["latest_invoice"] = f.invoice
			}
			json.NewEncoder(w).Encode(out)
		case "POST /v1/subscriptions/sub_t":
			f.updated = r.Form
			out := map[string]any{"id": "sub_t", "pending_update": pendingUpdate(), "latest_invoice": "in_1"}
			if r.Form.Get("expand[]") == "latest_invoice" {
				out["latest_invoice"] = f.invoice
			}
			json.NewEncoder(w).Encode(out)
		case "GET /v1/invoice_payments":
			if f.payments == nil || r.URL.Query().Get("invoice") != "in_1" {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": f.payments})
		case "GET /v1/payment_intents/pi_1":
			json.NewEncoder(w).Encode(f.intent)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_t", WebhookSecret: testWhsec, APIBase: srv.URL}
	if code := e.stripeEvent("evt_t1", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	return f
}

type paymentErr struct {
	Error struct {
		Code    string
		Message string
		Details map[string]string
	}
}

// The bank asks the customer to confirm the payment (3-D Secure): the switch answers 402 with Stripe's invoice
// page to confirm and pay there; the pending payment reads the same until Stripe has the payment.
func TestTrialUpgradeNeedsAuthentication(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	f := newFakeTrialStripe(t, e, ws.ID)
	// Before Stripe API 2025-03-31: the invoice carries its payment intent.
	f.invoice = map[string]any{"id": "in_1", "status": "open", "payment_intent": "pi_1",
		"hosted_invoice_url": "https://invoice.stripe.com/i/acct_1/test_in_1"}
	f.intent = map[string]any{"id": "pi_1", "status": "requires_action",
		"last_payment_error": map[string]any{"code": "authentication_required", "decline_code": "authentication_required"}}
	admin := e.keyWith(app.ScopeAdmin)

	var errBody paymentErr
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 402, &errBody)
	if f.updated.Get("expand[]") != "latest_invoice" || f.updated.Get("payment_behavior") != "pending_if_incomplete" {
		t.Fatalf("update: %v", f.updated)
	}
	d := errBody.Error.Details
	if errBody.Error.Code != "payment_failed" || d["reason"] != "authentication_required" ||
		d["pay_url"] != "https://invoice.stripe.com/i/acct_1/test_in_1" || d["plan"] != "pro" || !strings.Contains(errBody.Error.Message, "confirm") {
		t.Fatalf("402: %+v", errBody.Error)
	}

	var pp app.PendingPayment
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	if !pp.Pending || pp.Reason != app.PaymentAuthRequired || pp.PayURL != d["pay_url"] || pp.Plan != "pro" || pp.ExpiresAt != "2030-01-01T00:00:00Z" {
		t.Fatalf("pending payment: %+v", pp)
	}
	// Confirmed and paid on Stripe's page: Stripe applies the change, nothing waits.
	f.mu.Lock()
	f.pending = false
	f.mu.Unlock()
	pp = app.PendingPayment{}
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	if pp.Pending || pp.Reason != "" || pp.PayURL != "" {
		t.Fatalf("after paying: %+v", pp)
	}
	// A link that isn't Stripe's never reaches the dashboard.
	f.mu.Lock()
	f.pending = true
	f.invoice["hosted_invoice_url"] = "https://evil.example/pay"
	f.mu.Unlock()
	pp = app.PendingPayment{}
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	if pp.PayURL != "" || pp.Reason != app.PaymentAuthRequired {
		t.Fatalf("foreign link: %+v", pp)
	}

	// Only the account owner's keys (admin) read it.
	e.do(nil, "GET", "/v1/billing/pending-payment", e.keyWith(app.ScopePostsRead), nil, 403, nil)
}

// The card is declined: the switch answers 402 with the reason in plain words; the payment intent is found
// through the invoice's payments (Stripe API 2025-03-31 on, where the invoice has no payment_intent).
func TestTrialUpgradeDeclined(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	f := newFakeTrialStripe(t, e, ws.ID)
	f.invoice = map[string]any{"id": "in_1", "status": "open", "hosted_invoice_url": "https://invoice.stripe.com/i/acct_1/test_in_1"}
	f.payments = []any{map[string]any{"is_default": true, "payment": map[string]any{"type": "payment_intent", "payment_intent": "pi_1"}}}
	f.intent = map[string]any{"id": "pi_1", "status": "requires_payment_method",
		"last_payment_error": map[string]any{"code": "card_declined", "decline_code": "insufficient_funds"}}
	admin := e.keyWith(app.ScopeAdmin)

	var errBody paymentErr
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 402, &errBody)
	d := errBody.Error.Details
	if d["reason"] != "insufficient_funds" || d["decline_code"] != "insufficient_funds" || !strings.Contains(errBody.Error.Message, "declined") {
		t.Fatalf("402: %+v", errBody.Error)
	}
	// A plain decline, no precise code.
	f.mu.Lock()
	f.intent["last_payment_error"] = map[string]any{"code": "card_declined", "decline_code": "generic_decline"}
	f.mu.Unlock()
	var pp app.PendingPayment
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	if !pp.Pending || pp.Reason != app.PaymentDeclined || pp.DeclineCode != "generic_decline" {
		t.Fatalf("pending payment: %+v", pp)
	}
	// Stripe's payment can't be read: the change still waits, the reason is unknown.
	f.mu.Lock()
	f.payments = nil
	f.mu.Unlock()
	pp = app.PendingPayment{}
	e.do(nil, "GET", "/v1/billing/pending-payment", admin, nil, 200, &pp)
	if !pp.Pending || pp.Reason != app.PaymentUnknown || pp.PayURL == "" {
		t.Fatalf("unreadable payment: %+v", pp)
	}
}

// A paying workspace moving to a higher plan is charged the difference at once, and gets the plan only when that
// payment goes through; a lower plan, or the same plan billed monthly instead of yearly, is settled on the next
// invoice.
func TestUpgradeOfPayingSubscription(t *testing.T) {
	e := newEnv(t)
	ws := workspaceID(e)
	sub := subObject("sub_1", ws, "active", "starter", "year")
	var mu sync.Mutex
	var switched url.Values
	declined := true
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_1":
			switched = r.Form
			out := map[string]any{"id": "sub_1"}
			if declined {
				out["pending_update"] = map[string]any{"expires_at": 1893456000}
				out["latest_invoice"] = map[string]any{"id": "in_1", "status": "open", "payment_intent": "pi_1",
					"hosted_invoice_url": "https://invoice.stripe.com/i/acct_1/test_in_1"}
			}
			json.NewEncoder(w).Encode(out)
		case "GET /v1/payment_intents/pi_1":
			fmt.Fprint(w, `{"id":"pi_1","status":"requires_payment_method","last_payment_error":{"code":"card_declined","decline_code":"insufficient_funds"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer stripe.Close()
	admin := e.keyWith(app.ScopeAdmin)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_up", WebhookSecret: testWhsec, APIBase: stripe.URL}
	e.stripeEvent("evt_1", "customer.subscription.created", sub, testWhsec)
	if b := e.billing(admin); b.Plan != "starter" || *b.SubscriptionStatus != "active" {
		t.Fatalf("setup: %+v", b)
	}
	sent := func() url.Values {
		mu.Lock()
		defer mu.Unlock()
		return switched
	}

	// The card is declined: 402 says why, Stripe keeps the change pending and the workspace stays on Starter.
	var errBody paymentErr
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "interval": "year"}, 402, &errBody)
	if f := sent(); f.Get("proration_behavior") != "always_invoice" || f.Get("payment_behavior") != "pending_if_incomplete" ||
		f.Get("items[0][price]") != "price_business_year" || f.Get("trial_end") != "" {
		t.Fatalf("upgrade: %v", f)
	}
	if d := errBody.Error.Details; errBody.Error.Code != "payment_failed" || d["reason"] != "insufficient_funds" || d["plan"] != "business" ||
		d["pay_url"] == "" || !strings.Contains(errBody.Error.Message, "your current plan stays as it was") {
		t.Fatalf("402: %+v", errBody.Error)
	}
	e.stripeEvent("evt_2", "customer.subscription.updated", sub, testWhsec) // Stripe's event still has the old price
	if b := e.billing(admin); b.Plan != "starter" || b.Suspended {
		t.Fatalf("an unpaid upgrade must not change the plan: %+v", b)
	}

	// Paid: back to the plan page; the plan changes with Stripe's event.
	mu.Lock()
	declined = false
	mu.Unlock()
	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "interval": "year"}, 200, &out)
	if !strings.HasSuffix(out.URL, "?checkout=switched") || sent().Get("proration_behavior") != "always_invoice" {
		t.Fatalf("paid upgrade: %q %v", out.URL, sent())
	}
	mu.Lock()
	sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["price"] = stripePriceObject("business", "year")
	mu.Unlock()
	e.stripeEvent("evt_3", "customer.subscription.updated", sub, testWhsec)
	if b := e.billing(admin); b.Plan != "business" {
		t.Fatalf("after the upgrade: %+v", b)
	}

	// A lower plan, and the same plan billed monthly: prorated onto the next invoice, nothing waits.
	for _, to := range [][2]string{{"plus", "year"}, {"business", "month"}} {
		e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": to[0], "interval": to[1]}, 200, nil)
		if f := sent(); f.Get("proration_behavior") != "create_prorations" || f.Get("payment_behavior") != "" {
			t.Fatalf("switch to %v: %v", to, f)
		}
	}
}

// Was a bug: monthly to yearly kept create_prorations, and with billing_mode=flexible Stripe neither resets the
// billing cycle nor invoices an interval change by itself, so the year stayed unbilled as pending invoice items
// until the next invoice, a year away. It is charged at once and applied only once paid, like an upgrade,
// whatever the plan; the fake Stripe invoices only on always_invoice, like the sandbox.
func TestMonthlyToYearlyIsChargedAtOnce(t *testing.T) {
	e := newEnv(t)
	ws := workspaceID(e)
	sub := subObject("sub_1", ws, "active", "starter", "month")
	var mu sync.Mutex
	var switched url.Values
	invoices, declined := 0, true
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_1":
			switched = r.Form
			out := map[string]any{"id": "sub_1"}
			if r.Form.Get("proration_behavior") == "always_invoice" {
				invoices++
				if declined {
					out["pending_update"] = map[string]any{"expires_at": 1893456000}
					out["latest_invoice"] = map[string]any{"id": "in_1", "status": "open", "payment_intent": "pi_1",
						"hosted_invoice_url": "https://invoice.stripe.com/i/acct_1/test_in_1"}
				}
			}
			json.NewEncoder(w).Encode(out)
		case "GET /v1/payment_intents/pi_1":
			fmt.Fprint(w, `{"id":"pi_1","status":"requires_payment_method","last_payment_error":{"code":"card_declined","decline_code":"insufficient_funds"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer stripe.Close()
	admin := e.keyWith(app.ScopeAdmin)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_year", WebhookSecret: testWhsec, APIBase: stripe.URL}
	e.stripeEvent("evt_1", "customer.subscription.created", sub, testWhsec)

	// The card is declined: 402, an invoice was made, the change waits and the workspace stays as it was.
	var errBody paymentErr
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "starter", "interval": "year"}, 402, &errBody)
	mu.Lock()
	f, n := switched, invoices
	declined = false
	mu.Unlock()
	if f.Get("proration_behavior") != "always_invoice" || f.Get("payment_behavior") != "pending_if_incomplete" ||
		f.Get("items[0][price]") != "price_starter_year" || n != 1 {
		t.Fatalf("monthly to yearly: %d invoices, %v", n, f)
	}
	if errBody.Error.Code != "payment_failed" || errBody.Error.Details["pay_url"] == "" {
		t.Fatalf("402: %+v", errBody.Error)
	}

	// from (plan, interval, status) to (plan, interval): is it invoiced now?
	for _, c := range []struct {
		plan, interval, status, toPlan, toInterval string
		now                                        bool
	}{
		{"starter", "month", "active", "starter", "year", true},   // the same plan, yearly
		{"starter", "month", "active", "plus", "year", true},      // a higher plan, yearly
		{"plus", "month", "active", "starter", "year", true},      // a lower plan, yearly: the year is still due
		{"starter", "month", "past_due", "starter", "year", true}, // unpaid: the year never goes out unbilled
		{"starter", "year", "active", "plus", "month", true},      // a higher plan, monthly: an upgrade
		{"plus", "month", "active", "starter", "month", false},    // a lower plan: a credit
		{"plus", "year", "active", "plus", "month", false},        // yearly to monthly: a credit
		{"plus", "year", "active", "starter", "month", false},     // a lower plan, monthly: a credit
	} {
		mu.Lock()
		sub["status"] = c.status
		sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["price"] = stripePriceObject(c.plan, c.interval)
		before := invoices
		mu.Unlock()
		e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": c.toPlan, "interval": c.toInterval}, 200, nil)
		mu.Lock()
		f, made := switched, invoices-before
		mu.Unlock()
		want, pay := "create_prorations", ""
		if c.now {
			want, pay = "always_invoice", "pending_if_incomplete"
		}
		if f.Get("proration_behavior") != want || f.Get("payment_behavior") != pay || (made == 1) != c.now {
			t.Errorf("%+v: %d invoices, %v", c, made, f)
		}
	}
}
