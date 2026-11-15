package httpapi_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/web"
)

// Regional pricing: CHF for Switzerland and Liechtenstein, EUR for the EU, USD for everyone else, by time zone
// (the page's signed out, the workspace's signed in); a Stripe customer with a currency keeps it.
func TestRegionalCurrency(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	customerCurrency := ""
	var checkout url.Values
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			if !slices.Contains(r.URL.Query()["expand[]"], "data.currency_options") {
				t.Errorf("prices read without their currency options: %s", r.URL.RawQuery)
			}
			key := r.URL.Query().Get("lookup_keys[]")
			cents := 2900
			if strings.HasPrefix(key, "staqly_credits_") {
				cents = 1000
			}
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":%d,"currency":"eur","currency_options":{"eur":{"unit_amount":%[2]d},"chf":{"unit_amount":%[2]d},"usd":{"unit_amount":%[2]d}}}]}`, key, cents)
		case "GET /v1/customers/cus_1":
			fmt.Fprintf(w, `{"id":"cus_1","currency":%q}`, customerCurrency)
		case "POST /v1/customers", "POST /v1/customers/cus_1":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "GET /v1/checkout/sessions":
			fmt.Fprint(w, `{"data":[]}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_regional", WebhookSecret: testWhsec, APIBase: stripe.URL}

	plus := func(auth, query string) (app.Plan, string) {
		t.Helper()
		var plans struct{ Data []app.Plan }
		resp := e.do(nil, "GET", "/v1/plans"+query, auth, nil, 200, &plans)
		for _, p := range plans.Data {
			if p.ID == "plus" {
				return p, resp.Header.Get("Cache-Control")
			}
		}
		t.Fatal("no plus plan")
		return app.Plan{}, ""
	}

	// Signed out (the landing page): the page's time zone.
	for _, tc := range []struct{ query, want string }{
		{"?timezone=Europe/Zurich", "chf"},
		{"?timezone=Europe/Vaduz", "chf"},
		{"?timezone=Europe/Berlin", "eur"},
		{"?timezone=Europe/Lisbon", "eur"},
		{"?timezone=America/New_York", "usd"},
		{"?timezone=Europe/London", "usd"},
		{"?timezone=nonsense", "usd"},
		{"", "usd"},
	} {
		p, cache := plus("", tc.query)
		if p.Currency != tc.want || p.PriceMonth != 29 || p.PriceYear != 29 { // the fake's yearly price is 2900 too
			t.Errorf("signed out %q: %s %d/%d, want %s 29", tc.query, p.Currency, p.PriceMonth, p.PriceYear, tc.want)
		}
		if !strings.HasPrefix(cache, "public") {
			t.Errorf("signed out %q: Cache-Control %q", tc.query, cache)
		}
	}

	// Signed in: the workspace's zone (UTC says nothing, then the page's, then USD); not for shared caches.
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if p, cache := plus(e.key, ""); p.Currency != "usd" || !strings.HasPrefix(cache, "private") {
		t.Errorf("workspace in UTC: %s, Cache-Control %q", p.Currency, cache)
	}
	if p, _ := plus(e.key, "?timezone=Europe/Rome"); p.Currency != "eur" {
		t.Errorf("workspace in UTC, page in Rome: %s", p.Currency)
	}
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET timezone = 'Europe/Zurich' WHERE id = '` + ws.ID + `'`); err != nil {
		t.Fatal(err)
	}
	if p, _ := plus(e.key, "?timezone=America/New_York"); p.Currency != "chf" {
		t.Errorf("workspace in Zurich: %s", p.Currency)
	}

	// A new customer (no currency yet) checks out in the workspace's currency: plans and X post bundles alike.
	var out struct{ URL string }
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "plus", "interval": "month"}, 200, &out)
	if checkout.Get("currency") != "chf" || checkout.Get("line_items[0][price]") != "price_staqly_plus_month" {
		t.Errorf("checkout for a new customer: %v", checkout)
	}
	e.pay("POST", "/v1/billing/credits/checkout", map[string]any{"provider": "x", "posts": 200}, 200, &out)
	if checkout.Get("currency") != "chf" {
		t.Errorf("credits checkout for a new customer: %v", checkout)
	}
	var x app.XPosts
	e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
	if x.Currency != "chf" {
		t.Errorf("x posts currency = %s", x.Currency)
	}

	// Stripe locked the customer to EUR (an earlier payment): it keeps EUR wherever the workspace is.
	mu.Lock()
	customerCurrency = "eur"
	mu.Unlock()
	if p, _ := plus(e.key, ""); p.Currency != "eur" {
		t.Errorf("customer in EUR, workspace in Zurich: %s", p.Currency)
	}
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "plus", "interval": "year"}, 200, &out)
	if checkout.Get("currency") != "eur" {
		t.Errorf("checkout for a customer in EUR: %v", checkout)
	}
	e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
	if x.Currency != "eur" {
		t.Errorf("x posts currency for a customer in EUR = %s", x.Currency)
	}
}

// Cloudflare's country (CF-IPCountry) picks the currency when the request really came through Cloudflare: from one
// of its addresses as our trusted proxy saw it, or relayed by the dashboard (web.CountryHeader with the relay
// secret). Anything else falls back to the time zone; a Stripe customer's currency wins over both.
func TestRegionalCurrencyFromCloudflare(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(httpapi.New(e.svc, slog.New(slog.DiscardHandler), httpapi.Config{TrustProxy: true, RelayToken: "s3cret",
		OpenAPISpec: []byte("openapi: 3.1.0")}))
	defer srv.Close()
	cacheControl := ""
	plusCurrency := func(auth string, hdr map[string]string) string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/v1/plans?timezone=America/New_York", nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var plans struct{ Data []app.Plan }
		if err := json.NewDecoder(resp.Body).Decode(&plans); err != nil || resp.StatusCode != 200 {
			t.Fatalf("GET /v1/plans: %d %v", resp.StatusCode, err)
		}
		cacheControl = resp.Header.Get("Cache-Control")
		for _, p := range plans.Data {
			if p.ID == "plus" {
				return p.Currency
			}
		}
		t.Fatal("no plus plan")
		return ""
	}
	const cf = "104.16.0.1" // one of Cloudflare's addresses
	for _, tc := range []struct {
		name string
		hdr  map[string]string
		want string
	}{
		{"no country: the page's time zone", nil, "usd"},
		{"through Cloudflare, Switzerland", map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "CH"}, "chf"},
		{"through Cloudflare, Liechtenstein", map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "LI"}, "chf"},
		{"through Cloudflare, Germany", map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "DE"}, "eur"},
		{"through Cloudflare, Japan", map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "JP"}, "usd"},
		{"through Cloudflare, country unknown", map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "XX"}, "usd"},
		{"not from Cloudflare: the header is ignored", map[string]string{"X-Forwarded-For": "198.51.100.1", "CF-IPCountry": "CH"}, "usd"},
		{"relayed by the dashboard", map[string]string{web.RelayHeader: "s3cret", web.CountryHeader: "CH"}, "chf"},
		{"a forged relay", map[string]string{web.RelayHeader: "forged", web.CountryHeader: "CH"}, "usd"},
		{"the dashboard's header without its secret", map[string]string{web.CountryHeader: "DE"}, "usd"},
	} {
		if got := plusCurrency("", tc.hdr); got != tc.want {
			t.Errorf("signed out, %s: %s, want %s", tc.name, got, tc.want)
		}
		// An answer that depends on Cloudflare's country must not be kept by a cache shared by other countries.
		if byCountry := tc.hdr["CF-IPCountry"] != "" && tc.hdr["CF-IPCountry"] != "XX" && tc.hdr["X-Forwarded-For"] == cf ||
			tc.hdr[web.RelayHeader] == "s3cret"; byCountry != strings.HasPrefix(cacheControl, "private") {
			t.Errorf("signed out, %s: Cache-Control %q", tc.name, cacheControl)
		}
	}

	// Signed in: Cloudflare's country wins over the workspace's zone.
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET timezone = 'Europe/Zurich' WHERE id = '` + ws.ID + `'`); err != nil {
		t.Fatal(err)
	}
	if got := plusCurrency(e.key, nil); got != "chf" {
		t.Errorf("workspace in Zurich, no country: %s", got)
	}
	if got := plusCurrency(e.key, map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "FR"}); got != "eur" {
		t.Errorf("workspace in Zurich, Cloudflare says France: %s", got)
	}

	// A Stripe customer with a currency keeps it, whatever Cloudflare says.
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/customers/cus_usd":
			fmt.Fprint(w, `{"id":"cus_usd","currency":"usd"}`)
		default:
			fmt.Fprint(w, `{"data":[]}`)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_cf", WebhookSecret: testWhsec, APIBase: stripe.URL}
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET stripe_customer_id = 'cus_usd' WHERE id = '` + ws.ID + `'`); err != nil {
		t.Fatal(err)
	}
	if got := plusCurrency(e.key, map[string]string{"X-Forwarded-For": cf, "CF-IPCountry": "CH"}); got != "usd" {
		t.Errorf("customer in USD, Cloudflare says Switzerland: %s", got)
	}
}

// An amount-off code is in one currency (plus the ones in its currency options): a workspace paying in another is
// told so when it checks or uses the code, instead of Stripe refusing the Checkout (a 502). A percent-off code goes
// everywhere, and an amount is told in the workspace's currency.
func TestPromotionCodeCurrency(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var checkout url.Values
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			key := r.URL.Query().Get("lookup_keys[]")
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":2900,"currency":"eur","currency_options":{"eur":{"unit_amount":2900},"chf":{"unit_amount":2900},"usd":{"unit_amount":2900}}}]}`, key)
		case "GET /v1/promotion_codes":
			code := r.URL.Query().Get("code")
			fmt.Fprintf(w, `{"data":[{"id":"promo_%s","code":%q,"promotion":{"type":"coupon","coupon":"c_%[1]s"}}]}`, code, code)
		case "GET /v1/coupons/c_EUR5", "GET /v1/coupons/c_MULTI5":
			if !slices.Contains(r.URL.Query()["expand[]"], "currency_options") {
				t.Errorf("coupon read without its currency options: %s", r.URL.RawQuery)
			}
			if strings.HasSuffix(r.URL.Path, "EUR5") {
				fmt.Fprint(w, `{"id":"c_EUR5","valid":true,"amount_off":500,"currency":"eur","duration":"once"}`)
			} else {
				fmt.Fprint(w, `{"id":"c_MULTI5","valid":true,"amount_off":500,"currency":"eur","duration":"once","currency_options":{"chf":{"amount_off":450}}}`)
			}
		case "GET /v1/coupons/c_PCT20":
			fmt.Fprint(w, `{"id":"c_PCT20","valid":true,"percent_off":20,"duration":"once"}`)
		case "POST /v1/customers", "POST /v1/customers/cus_1":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "GET /v1/customers/cus_1":
			fmt.Fprint(w, `{"id":"cus_1","currency":null}`)
		case "GET /v1/checkout/sessions":
			fmt.Fprint(w, `{"data":[]}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		default:
			t.Errorf("unexpected Stripe call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_promo_cur", WebhookSecret: testWhsec, APIBase: stripe.URL}
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET timezone = 'Europe/Zurich' WHERE id = '` + ws.ID + `'`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		code     string
		status   int
		amount   int64
		currency string
	}{
		{"EUR5", 422, 0, ""},        // EUR only: not for a workspace paying in CHF
		{"MULTI5", 200, 450, "chf"}, // its CHF amount
		{"PCT20", 200, 0, ""},
	} {
		var c app.PromotionCode
		var errBody struct{ Error app.Error }
		out := any(&c)
		if tc.status != 200 {
			out = &errBody
		}
		e.pay("POST", "/v1/billing/promotion-code", map[string]string{"promotion_code": tc.code}, tc.status, out)
		if tc.status != 200 {
			if !strings.Contains(errBody.Error.Fields["promotion_code"], "EUR") {
				t.Errorf("%s: %+v", tc.code, errBody.Error)
			}
		} else if c.AmountOff != tc.amount || c.Currency != tc.currency {
			t.Errorf("%s: %+v", tc.code, c)
		}

		mu.Lock()
		checkout = nil
		mu.Unlock()
		e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "plus", "promotion_code": tc.code}, tc.status, nil)
		mu.Lock()
		switch {
		case tc.status != 200 && checkout != nil:
			t.Errorf("%s: a Checkout was started: %v", tc.code, checkout)
		case tc.status == 200 && (checkout.Get("discounts[0][promotion_code]") != "promo_"+tc.code || checkout.Get("currency") != "chf"):
			t.Errorf("%s: checkout %v", tc.code, checkout)
		}
		mu.Unlock()
	}
}

// An add-on is offered and sold only in the currencies its prices have: a subscription billed in CHF can't take an
// EUR-only price (Stripe refuses it), so the dashboard must not show it at a CHF amount.
func TestAddonCurrency(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var calls []string
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/products":
			fmt.Fprint(w, `{"data":[{"name":"Euro pack","metadata":{"staqly_kind":"addon","staqly_addon":"euro_pack"}},`+
				`{"name":"World pack","metadata":{"staqly_kind":"addon","staqly_addon":"world_pack"}}]}`)
		case "GET /v1/prices":
			key := r.URL.Query().Get("lookup_keys[]")
			opts := `"eur":{"unit_amount":900}`
			if strings.Contains(key, "world_pack") {
				opts += `,"chf":{"unit_amount":800},"usd":{"unit_amount":1000}`
			}
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":900,"currency":"eur","currency_options":{%s}}]}`, key, opts)
		case "GET /v1/customers/cus_a":
			fmt.Fprint(w, `{"id":"cus_a","currency":"chf"}`)
		case "GET /v1/subscriptions/sub_a":
			fmt.Fprint(w, `{"id":"sub_a","customer":"cus_a","status":"active","currency":"chf","items":{"data":[{"id":"si_plan","quantity":1,`+
				`"price":{"id":"price_plus_month","lookup_key":"staqly_plus_month","recurring":{"interval":"month"}}}]}}`)
		case "POST /v1/subscriptions/sub_a":
			fmt.Fprint(w, `{"id":"sub_a"}`)
		default:
			fmt.Fprint(w, `{"data":[]}`)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_addon_cur", WebhookSecret: testWhsec, APIBase: stripe.URL}
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET stripe_customer_id = 'cus_a', stripe_subscription_id = 'sub_a', subscription_status = 'active' WHERE id = '` + ws.ID + `'`); err != nil {
		t.Fatal(err)
	}

	var list struct{ Data []app.Addon }
	e.do(nil, "GET", "/v1/billing/addons", e.keyWith(app.ScopeAdmin), nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != "world_pack" || list.Data[0].Currency != "chf" || list.Data[0].PriceMonth != 8 {
		t.Fatalf("add-ons for a workspace paying in CHF: %+v", list.Data)
	}
	var errBody struct{ Error app.Error }
	e.pay("POST", "/v1/billing/addons/euro_pack", nil, 409, &errBody)
	if errBody.Error.Code != "not_available" || !strings.Contains(errBody.Error.Message, "CHF") {
		t.Errorf("an EUR-only add-on bought in CHF: %+v", errBody.Error)
	}
	mu.Lock()
	if slices.Contains(calls, "POST /v1/subscriptions/sub_a") {
		t.Errorf("Stripe was asked to add an EUR-only price to a CHF subscription: %v", calls)
	}
	mu.Unlock()
	e.pay("POST", "/v1/billing/addons/world_pack", nil, 204, nil)
}
