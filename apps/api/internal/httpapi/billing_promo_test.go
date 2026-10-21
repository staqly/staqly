package httpapi_test

import (
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

// A promotion code typed in staqly: checked against Stripe (active, its coupon valid, for the picked plan), then
// put on the new Checkout session or on the live subscription, also without a plan change.
func TestPromotionCodes(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	end := time.Now().Add(30 * 24 * time.Hour).Unix()
	sub := map[string]any{"id": "sub_p", "customer": "cus_p", "status": "active", "metadata": map[string]string{"workspace_id": ws.ID},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_p", "current_period_end": end, "price": stripePriceObject("plus", "month")}}}}
	var checkout, updated url.Values
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/prices/price_business_month":
			fmt.Fprint(w, `{"id":"price_business_month","product":"prod_business"}`)
		case "GET /v1/prices/price_pro_month":
			fmt.Fprint(w, `{"id":"price_pro_month","product":"prod_pro"}`)
		case "GET /v1/promotion_codes":
			if r.URL.Query().Get("code") == "BIZ-FREE" && r.URL.Query().Get("active") == "true" {
				fmt.Fprint(w, `{"data":[{"id":"promo_biz","promotion":{"type":"coupon","coupon":"biz_free"}}]}`)
				return
			}
			fmt.Fprint(w, `{"data":[]}`)
		case "GET /v1/coupons/biz_free":
			fmt.Fprint(w, `{"id":"biz_free","valid":true,"applies_to":{"products":["prod_business"]}}`)
		case "POST /v1/customers", "POST /v1/customers/cus_p":
			fmt.Fprint(w, `{"id":"cus_p"}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/p"}`)
		case "GET /v1/subscriptions/sub_p":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_p":
			updated = r.Form
			fmt.Fprint(w, `{"id":"sub_p"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_p", WebhookSecret: testWhsec, APIBase: stripe.URL}
	admin := e.keyWith(app.ScopeAdmin)
	var errBody struct{ Error app.Error }

	// No plan yet: the code goes on the Checkout session instead of Checkout's own code box.
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "promotion_code": " BIZ-FREE "}, 200, nil)
	if checkout.Get("discounts[0][promotion_code]") != "promo_biz" || checkout.Get("allow_promotion_codes") != "" {
		t.Fatalf("checkout with a code: %v", checkout)
	}
	// An unknown code and one for another plan are refused under the field.
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "promotion_code": "NOPE"}, 422, &errBody)
	if errBody.Error.Fields["promotion_code"] == "" {
		t.Fatalf("unknown code: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro", "promotion_code": "BIZ-FREE"}, 422, &errBody)
	if !strings.Contains(errBody.Error.Fields["promotion_code"], "Pro") {
		t.Fatalf("a code for another plan: %+v", errBody.Error)
	}

	// Paying for Plus: switching to Business with the code puts the code on the subscription.
	if code := e.stripeEvent("evt_p1", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "promotion_code": "BIZ-FREE"}, 200, nil)
	if updated.Get("items[0][price]") != "price_business_month" || updated.Get("discounts[0][promotion_code]") != "promo_biz" {
		t.Fatalf("switch with a code: %v", updated)
	}
	// The current plan with a code: only the code is applied; without one it is still "already on this plan".
	sub["items"] = map[string]any{"data": []any{map[string]any{"id": "si_p", "current_period_end": end, "price": stripePriceObject("business", "month")}}}
	if code := e.stripeEvent("evt_p2", "customer.subscription.updated", sub, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	updated = nil
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business", "promotion_code": "BIZ-FREE"}, 200, nil)
	if updated.Get("discounts[0][promotion_code]") != "promo_biz" || updated.Get("items[0][price]") != "" {
		t.Fatalf("code on the current plan: %v", updated)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business"}, 409, nil)
}

// Leaving Starter's trial for another plan ends the trial now and charges the new plan at once; the change waits
// for that payment, so a card that fails keeps the trial and the dashboard is told.
func TestUpgradeFromTrialChargesNow(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	end := time.Now().Add(7 * 24 * time.Hour).Unix()
	sub := map[string]any{"id": "sub_t", "customer": "cus_t", "status": "trialing", "metadata": map[string]string{"workspace_id": ws.ID},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_t", "current_period_end": end, "price": stripePriceObject("starter", "month")}}}}
	var updated url.Values
	pending := false
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "GET /v1/subscriptions/sub_t":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/subscriptions/sub_t":
			updated = r.Form
			if pending {
				fmt.Fprint(w, `{"id":"sub_t","pending_update":{"expires_at":1}}`)
				return
			}
			fmt.Fprint(w, `{"id":"sub_t","pending_update":null}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_t", WebhookSecret: testWhsec, APIBase: stripe.URL}
	admin := e.keyWith(app.ScopeAdmin)
	if code := e.stripeEvent("evt_t1", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}

	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "business"}, 200, nil)
	if updated.Get("items[0][price]") != "price_business_month" || updated.Get("trial_end") != "now" ||
		updated.Get("payment_behavior") != "pending_if_incomplete" || updated.Get("proration_behavior") != "always_invoice" {
		t.Fatalf("upgrade from the trial: %v", updated)
	}
	// The card fails: Stripe keeps the change pending, the trial stays, the dashboard hears why.
	pending = true
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 402, &errBody)
	if errBody.Error.Code != "payment_failed" {
		t.Fatalf("failed payment: %+v", errBody.Error)
	}
}
