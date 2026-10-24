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
