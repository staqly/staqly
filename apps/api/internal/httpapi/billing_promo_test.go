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
			if strings.EqualFold(r.URL.Query().Get("code"), "BIZ-FREE") && r.URL.Query().Get("active") == "true" {
				fmt.Fprint(w, `{"data":[{"id":"promo_biz","code":"BIZ-FREE","promotion":{"type":"coupon","coupon":"biz_free"}}]}`)
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

	// One discount at a time: while it lasts, no other code and not the same one again, checked or applied.
	sub["discounts"] = []string{"di_1"}
	updated = nil
	for path, body := range map[string]map[string]string{
		"/v1/billing/checkout":       {"plan": "business", "promotion_code": "BIZ-FREE"},
		"/v1/billing/promotion-code": {"promotion_code": "BIZ-FREE"},
	} {
		e.do(nil, "POST", path, admin, body, 422, &errBody)
		if !strings.Contains(errBody.Error.Fields["promotion_code"], "already has a discount") || updated != nil {
			t.Fatalf("%s with a discount on: %+v %v", path, errBody.Error, updated)
		}
	}
	// A switch without a code still works.
	e.do(nil, "POST", "/v1/billing/checkout", admin, map[string]string{"plan": "pro"}, 200, nil)
}

// Checking a promotion code before paying: its discount and the plans it applies to, so the plan picker can show
// the new prices; a code that is unknown, used up or for no plan on sale is refused under the field.
func TestCheckPromotionCode(t *testing.T) {
	e := newEnv(t)
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.Method + " " + r.URL.Path
		switch {
		case path == "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case strings.HasPrefix(path, "GET /v1/prices/price_"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/prices/")
			plan, _, _ := strings.Cut(strings.TrimPrefix(id, "price_"), "_")
			fmt.Fprintf(w, `{"id":%q,"product":"prod_%s"}`, id, plan)
		case path == "GET /v1/promotion_codes":
			// Like Stripe: the lookup ignores case and answers with the code as it was made.
			code := strings.ToUpper(r.URL.Query().Get("code"))
			coupon := map[string]string{"BIZ-FREE": "biz_free", "SPRING20": "spring", "DEAD": "dead", "GONE": "gone"}[code]
			if coupon == "" {
				fmt.Fprint(w, `{"data":[]}`)
				return
			}
			fmt.Fprintf(w, `{"data":[{"id":"promo_%s","code":%q,"promotion":{"type":"coupon","coupon":%q}}]}`, coupon, code, coupon)
		case path == "GET /v1/coupons/biz_free":
			fmt.Fprint(w, `{"id":"biz_free","valid":true,"percent_off":100,"duration":"once","applies_to":{"products":["prod_business"]}}`)
		case path == "GET /v1/coupons/spring":
			fmt.Fprint(w, `{"id":"spring","valid":true,"percent_off":20,"duration":"repeating","duration_in_months":3}`)
		case path == "GET /v1/coupons/dead":
			fmt.Fprint(w, `{"id":"dead","valid":false,"percent_off":10}`)
		case path == "GET /v1/coupons/gone":
			fmt.Fprint(w, `{"id":"gone","valid":true,"amount_off":500,"currency":"eur","applies_to":{"products":["prod_old"]}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_c", WebhookSecret: testWhsec, APIBase: stripe.URL}
	admin := e.keyWith(app.ScopeAdmin)

	var c app.PromotionCode
	e.do(nil, "POST", "/v1/billing/promotion-code", admin, map[string]string{"promotion_code": " SPRING20 "}, 200, &c)
	if c.Code != "SPRING20" || c.PercentOff != 20 || c.Duration != "repeating" || c.DurationInMonths != 3 ||
		strings.Join(c.Plans, ",") != "starter,plus,pro,business" {
		t.Fatalf("a code for every plan: %+v", c)
	}
	e.do(nil, "POST", "/v1/billing/promotion-code", admin, map[string]string{"promotion_code": "BIZ-FREE"}, 200, &c)
	if c.PercentOff != 100 || c.Duration != "once" || strings.Join(c.Plans, ",") != "business" {
		t.Fatalf("a code for Business: %+v", c)
	}
	// Codes are case sensitive: spring20 is not SPRING20, though Stripe's lookup finds it.
	for _, code := range []string{"NOPE", "DEAD", "GONE", " ", "spring20", "Biz-Free"} {
		var errBody struct{ Error app.Error }
		e.do(nil, "POST", "/v1/billing/promotion-code", admin, map[string]string{"promotion_code": code}, 422, &errBody)
		if errBody.Error.Fields["promotion_code"] == "" {
			t.Fatalf("%q: %+v", code, errBody.Error)
		}
	}
	e.do(nil, "POST", "/v1/billing/promotion-code", e.keyWith(app.ScopePostsRead), map[string]string{"promotion_code": "SPRING20"}, 403, nil)
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

// An admin manages admins' and editors' keys, not an owner's: revoking or rotating one is refused (403), as
// with an owner's sessions and membership. The owner still manages their own keys.
func TestAdminCannotRevokeOrRotateAnOwnersKey(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, _, _ := e.team()
	var key app.APIKey
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "billing", "scopes": []string{"admin"}}, 201, &key)
	e.as(admin, "DELETE", "/v1/api-keys/"+key.ID, nil, 403, nil)
	e.as(admin, "POST", "/v1/api-keys/"+key.ID+"/rotate", nil, 403, nil)
	var own app.APIKey
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "mine", "scopes": []string{"posts:read"}}, 201, &own)
	e.as(admin, "DELETE", "/v1/api-keys/"+own.ID, nil, 204, nil)
	e.as(owner, "DELETE", "/v1/api-keys/"+key.ID, nil, 204, nil)
}
