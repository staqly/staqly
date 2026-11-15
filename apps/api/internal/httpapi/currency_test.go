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
