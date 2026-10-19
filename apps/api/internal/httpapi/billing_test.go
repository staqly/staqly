package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

const testWhsec = "whsec_test"

// stripeEvent posts a signed Stripe event to the webhook endpoint.
func (e *env) stripeEvent(id, typ string, object any, secret string) int {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "type": typ, "data": map[string]any{"object": object}})
	ts := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(body)))
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t="+ts+",v1="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// stripePriceObject is a Stripe price as the subscription item carries it, with the lookup key the server
// uses to place it.
func stripePriceObject(plan, interval string) map[string]any {
	return map[string]any{"id": "price_" + plan + "_" + interval, "lookup_key": app.PriceLookupKey(plan, interval),
		"recurring": map[string]any{"interval": interval}}
}

func TestStripeBillingLifecycle(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	periodEnd := time.Now().Add(365 * 24 * time.Hour).Unix()
	item := map[string]any{"id": "si_1", "current_period_end": periodEnd, "price": stripePriceObject("plus", "year")}
	sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active",
		"metadata": map[string]string{"workspace_id": ws.ID}, "items": map[string]any{"data": []any{item}}}
	var calls []string
	var switched, checkout url.Values
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, _ := r.BasicAuth(); u != "sk_test_1" {
			t.Errorf("stripe auth = %q", u)
		}
		r.ParseForm()
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			key := r.URL.Query().Get("lookup_keys[]")
			if key == app.PriceLookupKey("starter", "year") { // not created in this Stripe account
				fmt.Fprint(w, `{"data":[]}`)
				return
			}
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(key, "staqly_"))
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_1":
			switched = r.Form
			fmt.Fprint(w, `{"id":"sub_1"}`)
		case "POST /v1/billing_portal/sessions":
			fmt.Fprint(w, `{"url":"https://billing.stripe.test/p_1"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()

	admin := e.keyWith(app.ScopeAdmin)
	// In the Starter trial with five days left.
	if err := e.svc.SetPlan(context.Background(), ws.ID, app.TrialPlan, time.Now().Add(5*24*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	var b app.Billing
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Enabled || b.Plan != app.TrialPlan || b.PlanExpiresAt == nil {
		t.Fatalf("billing must be off without Stripe settings, the trial on: %+v", b)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, nil, 409, nil)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}

	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "enterprise"}, 422, nil)
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "plus", "interval": "week"}, 422, nil)
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "starter", "interval": "year"}, 409, &errBody)
	if errBody.Error.Code != "not_available" {
		t.Fatalf("a price that doesn't exist: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "plus", "interval": "year"}, 200, &out)
	if out.URL != "https://checkout.stripe.test/cs_1" {
		t.Fatalf("checkout url = %q", out.URL)
	}
	if checkout.Get("customer") != "cus_1" || checkout.Get("line_items[0][price]") != "price_plus_year" ||
		checkout.Get("subscription_data[metadata][staqly_plan]") != "plus" ||
		checkout.Get("subscription_data[trial_period_days]") != "" || // Plus is paid from the first day
		checkout.Get("tax_id_collection[enabled]") != "true" || checkout.Get("billing_address_collection") != "required" ||
		checkout.Get("automatic_tax[enabled]") != "true" || checkout.Get("payment_method_collection") != "always" ||
		!strings.HasSuffix(checkout.Get("success_url"), "/settings/billing?checkout=success") {
		t.Errorf("checkout form = %v", checkout)
	}
	// Starter: Stripe's 7-day trial with a card required (cancelled if no card is on file at its end).
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "starter", "interval": "month"}, 200, &out)
	if checkout.Get("line_items[0][price]") != "price_starter_month" || checkout.Get("subscription_data[trial_period_days]") != "7" ||
		checkout.Get("subscription_data[trial_settings][end_behavior][missing_payment_method]") != "cancel" ||
		checkout.Get("payment_method_collection") != "always" {
		t.Errorf("starter checkout form = %v", checkout)
	}
	e.do(nil, "POST", "/v1/billing/checkout", e.key, map[string]string{"plan": "plus"}, 403, nil) // needs admin

	if code := e.stripeEvent("evt_1", "checkout.session.completed", map[string]any{"subscription": "sub_1"}, "wrong"); code != 400 {
		t.Fatalf("bad signature: %d", code)
	}
	for range 2 { // a retried event is applied once
		if code := e.stripeEvent("evt_1", "checkout.session.completed", map[string]any{"subscription": "sub_1"}, testWhsec); code != 200 {
			t.Fatalf("checkout.session.completed: %d", code)
		}
	}
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Plan != "plus" || b.Suspended || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "active" || b.PlanExpiresAt == nil {
		t.Fatalf("after checkout: %+v", b)
	}
	if end, _ := time.Parse(time.RFC3339, *b.PlanExpiresAt); end.Before(time.Unix(periodEnd, 0)) {
		t.Fatalf("the plan should run past the paid period, until %s", *b.PlanExpiresAt)
	}
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if u.Plan.ID != "plus" || u.Plan.Limits.Channels != 15 {
		t.Fatalf("usage after checkout: %+v", u.Plan)
	}

	// Paying already: a checkout for another plan switches the subscription (prorated), back to the billing page.
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "plus", "interval": "year"}, 409, nil) // same price
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "interval": "month"}, 200, &out)
	if !strings.HasSuffix(out.URL, "/settings/billing?checkout=switched") || switched.Get("items[0][id]") != "si_1" ||
		switched.Get("items[0][price]") != "price_business_month" || switched.Get("proration_behavior") != "create_prorations" {
		t.Fatalf("switch: %q %v", out.URL, switched)
	}
	item["price"] = stripePriceObject("business", "month")
	e.stripeEvent("evt_switch", "customer.subscription.updated", sub, testWhsec)
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Plan != "business" {
		t.Fatalf("after the switch: %+v", b)
	}
	e.do(nil, "POST", "/v1/billing/portal", admin, nil, 200, &out)
	if out.URL != "https://billing.stripe.test/p_1" {
		t.Fatalf("portal url = %q", out.URL)
	}

	// past_due keeps the paid end; canceled ends the plan now → suspended.
	sub["status"] = "past_due"
	e.stripeEvent("evt_2", "customer.subscription.updated", sub, testWhsec)
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Suspended || b.Plan != "business" {
		t.Fatalf("past_due must not suspend yet: %+v", b)
	}
	sub["status"] = "canceled"
	e.stripeEvent("evt_3", "customer.subscription.deleted", sub, testWhsec)
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if !b.Suspended {
		t.Fatalf("canceled subscription should suspend: %+v", b)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 200, &out) // paying again works while suspended
	if strings.Count(strings.Join(calls, ","), "POST /v1/customers") != 1 {
		t.Fatalf("customer must be created once: %v", calls)
	}
	// A price looked up once is cached (the usage read adds Plus monthly, for its live amount); STRIPE_PRICE_*
	// overrides the lookup.
	if n := strings.Count(strings.Join(calls, ","), "GET /v1/prices"); n != 6 {
		t.Fatalf("price lookups = %d: %v", n, calls)
	}
	e.svc.Cfg.Billing.Prices = map[string]string{"pro_year": "price_custom"}
	switched = nil
	sub["status"] = "active"
	e.stripeEvent("evt_4", "customer.subscription.updated", sub, testWhsec)
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro", "interval": "year"}, 200, &out)
	if switched.Get("items[0][price]") != "price_custom" {
		t.Fatalf("override: %v", switched)
	}
}

// Billing details come from Stripe on request: the subscription period, the cards (brand and last four only)
// and the issued invoices, paged; empty without billing or before the first checkout.
func TestBillingDetails(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	start, end := time.Now().Add(-5*24*time.Hour).Unix(), time.Now().Add(25*24*time.Hour).Unix()
	var invoiceQueries []string
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(map[string]any{"id": "sub_1", "status": "active", "cancel_at_period_end": true,
				"items": map[string]any{"data": []any{map[string]any{"current_period_start": start, "current_period_end": end,
					"price": map[string]any{"unit_amount": 3000, "currency": "usd", "recurring": map[string]any{"interval": "month"}}}}}})
		case "GET /v1/customers/cus_1":
			fmt.Fprint(w, `{"id":"cus_1","invoice_settings":{"default_payment_method":"pm_2"}}`)
		case "GET /v1/customers/cus_1/payment_methods":
			fmt.Fprint(w, `{"data":[{"id":"pm_1","card":{"brand":"visa","last4":"4242","exp_month":1,"exp_year":2030}},
				{"id":"pm_2","card":{"brand":"mastercard","last4":"4444","exp_month":12,"exp_year":2031}}]}`)
		case "GET /v1/invoices":
			invoiceQueries = append(invoiceQueries, r.URL.RawQuery)
			fmt.Fprintf(w, `{"has_more":%t,"data":[
				{"id":"in_2","number":"A-0002","created":%d,"total":3000,"amount_paid":3000,"currency":"usd","status":"paid","hosted_invoice_url":"https://invoice.stripe.test/2","invoice_pdf":"https://invoice.stripe.test/2.pdf"},
				{"id":"in_draft","status":"draft","created":%d}]}`, r.URL.Query().Get("starting_after") == "", start, start)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()

	admin := e.keyWith(app.ScopeAdmin)
	var d app.BillingDetails
	e.do(nil, "GET", "/v1/billing/details", admin, nil, 200, &d)
	if d.Enabled || d.Subscription != nil || len(d.Invoices) != 0 || d.PaymentMethods == nil {
		t.Fatalf("without billing: %+v", d)
	}
	e.do(nil, "GET", "/v1/billing/details", e.key, nil, 403, nil) // admin only

	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	e.do(nil, "GET", "/v1/billing/details", admin, nil, 200, &d)
	if !d.Enabled || d.Subscription != nil || len(d.Invoices) != 0 {
		t.Fatalf("before the first checkout: %+v", d)
	}
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET stripe_customer_id = 'cus_1', stripe_subscription_id = 'sub_1',
		subscription_status = 'active' WHERE id = ?`, ws.ID); err != nil {
		t.Fatal(err)
	}
	e.do(nil, "GET", "/v1/billing/details", admin, nil, 200, &d)
	sub := d.Subscription
	if sub == nil || sub.Status != "active" || !sub.CancelAtPeriodEnd || sub.Amount != 3000 || sub.Interval != "month" ||
		sub.CurrentPeriodStart == nil || sub.CurrentPeriodEnd == nil {
		t.Fatalf("subscription: %+v", sub)
	}
	if len(d.PaymentMethods) != 2 || d.PaymentMethods[1].Last4 != "4444" || !d.PaymentMethods[1].Default || d.PaymentMethods[0].Default {
		t.Fatalf("payment methods: %+v", d.PaymentMethods)
	}
	if len(d.Invoices) != 1 || d.Invoices[0].Number != "A-0002" || d.Invoices[0].InvoicePDF == "" || !d.HasMoreInvoices {
		t.Fatalf("invoices (drafts left out): %+v", d.Invoices)
	}
	e.do(nil, "GET", "/v1/billing/details?starting_after=in_2", admin, nil, 200, &d)
	if d.Subscription != nil || d.HasMoreInvoices || len(invoiceQueries) != 2 || !strings.Contains(invoiceQueries[1], "starting_after=in_2") {
		t.Fatalf("next page: %+v %v", d, invoiceQueries)
	}
}

// A price pinned with STRIPE_PRICE_* that Stripe doesn't have (archived, or from another account) is retried
// once with the plan's price found by its lookup key, so checkout keeps working.
func TestPinnedPriceFallsBackToLookupKey(t *testing.T) {
	e := newEnv(t)
	var used []string
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_live_plus","unit_amount":2900,"currency":"eur"}]}`)
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_p"}`)
		case "POST /v1/checkout/sessions":
			used = append(used, r.Form.Get("line_items[0][price]"))
			if r.Form.Get("line_items[0][price]") == "price_archived" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"code":"resource_missing","param":"line_items[0][price]","message":"No such price: 'price_archived'"}}`)
				return
			}
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/ok"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_pinned", WebhookSecret: testWhsec, APIBase: stripe.URL,
		Prices: map[string]string{"plus_month": "price_archived"}}
	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/checkout", e.admin, map[string]string{"plan": "plus", "interval": "month"}, 200, &out)
	if out.URL != "https://checkout.stripe.test/ok" || strings.Join(used, ",") != "price_archived,price_live_plus" {
		t.Fatalf("fallback: %q %v", out.URL, used)
	}
}
