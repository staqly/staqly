package httpapi_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestStripeBillingLifecycle(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	periodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
	sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active",
		"metadata": map[string]string{"workspace_id": ws.ID},
		"items":    map[string]any{"data": []any{map[string]any{"current_period_end": periodEnd}}}}
	var calls []string
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, _ := r.BasicAuth(); u != "sk_test_1" {
			t.Errorf("stripe auth = %q", u)
		}
		r.ParseForm()
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "POST /v1/checkout/sessions":
			if r.Form.Get("customer") != "cus_1" || r.Form.Get("line_items[0][price]") != "price_pro" ||
				!strings.HasSuffix(r.Form.Get("success_url"), "/settings/plan?checkout=success") {
				t.Errorf("checkout form = %v", r.Form)
			}
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/subscriptions/sub_1":
			json.NewEncoder(w).Encode(sub)
		case "POST /v1/billing_portal/sessions":
			fmt.Fprint(w, `{"url":"https://billing.stripe.test/p_1"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()

	admin := e.keyWith(app.ScopeAdmin)
	var b app.Billing
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Enabled {
		t.Fatal("billing must be off without Stripe settings")
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, nil, 409, nil)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, PricePro: "price_pro", APIBase: stripe.URL}

	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/checkout", admin, nil, 200, &out)
	if out.URL != "https://checkout.stripe.test/cs_1" {
		t.Fatalf("checkout url = %q", out.URL)
	}
	e.do(nil, "POST", "/v1/billing/checkout", e.key, nil, 403, nil) // needs admin

	if code := e.stripeEvent("evt_1", "checkout.session.completed", map[string]any{"subscription": "sub_1"}, "wrong"); code != 400 {
		t.Fatalf("bad signature: %d", code)
	}
	for range 2 { // a retried event is applied once
		if code := e.stripeEvent("evt_1", "checkout.session.completed", map[string]any{"subscription": "sub_1"}, testWhsec); code != 200 {
			t.Fatalf("checkout.session.completed: %d", code)
		}
	}
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Plan != "pro" || b.Suspended || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "active" || b.PlanExpiresAt == nil {
		t.Fatalf("after checkout: %+v", b)
	}
	if end, _ := time.Parse(time.RFC3339, *b.PlanExpiresAt); end.Before(time.Unix(periodEnd, 0)) {
		t.Fatalf("pro should run past the paid period, until %s", *b.PlanExpiresAt)
	}
	e.do(nil, "POST", "/v1/billing/portal", admin, nil, 200, &out)
	if out.URL != "https://billing.stripe.test/p_1" {
		t.Fatalf("portal url = %q", out.URL)
	}
	e.do(nil, "POST", "/v1/workspace/plan", admin, map[string]string{"plan": "free"}, 409, nil)

	// past_due keeps the paid end; canceled ends Pro now → suspended.
	sub["status"] = "past_due"
	e.stripeEvent("evt_2", "customer.subscription.updated", sub, testWhsec)
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Suspended || b.Plan != "pro" {
		t.Fatalf("past_due must not suspend yet: %+v", b)
	}
	sub["status"] = "canceled"
	e.stripeEvent("evt_3", "customer.subscription.deleted", sub, testWhsec)
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if !b.Suspended {
		t.Fatalf("canceled subscription should suspend: %+v", b)
	}
	e.do(nil, "POST", "/v1/billing/checkout", admin, nil, 200, &out) // paying again works while suspended
	e.do(nil, "POST", "/v1/workspace/plan", admin, map[string]string{"plan": "free"}, 200, nil)
	e.do(nil, "GET", "/v1/billing", admin, nil, 200, &b)
	if b.Plan != "free" || b.Suspended {
		t.Fatalf("after switching to free: %+v", b)
	}
	if strings.Count(strings.Join(calls, ","), "POST /v1/customers") != 1 {
		t.Fatalf("customer must be created once: %v", calls)
	}
}
