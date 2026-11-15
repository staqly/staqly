package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// CF-IPCountry and CF-Connecting-IP are believed only when the address that reached us (or, with trustProxy, our
// own proxy) is Cloudflare's: anyone else can send those headers.
func TestClientCountryAndIP(t *testing.T) {
	for _, tc := range []struct {
		name         string
		remote       string
		trustProxy   bool
		xff, cc, cip string
		country, ip  string
	}{
		{name: "Cloudflare connects directly", remote: "104.16.0.1:443", cc: "CH", cip: "203.0.113.7", country: "CH", ip: "203.0.113.7"},
		{name: "Cloudflare over IPv6", remote: "[2606:4700::1]:443", cc: "de", cip: "2001:db8::1", country: "DE", ip: "2001:db8::1"},
		{name: "anyone else sends the headers", remote: "198.51.100.1:443", cc: "CH", cip: "203.0.113.7", ip: "198.51.100.1"},
		{name: "a Cloudflare address claimed in X-Forwarded-For, no trusted proxy", remote: "198.51.100.1:443", xff: "104.16.0.1",
			cc: "CH", cip: "203.0.113.7", ip: "198.51.100.1"},
		{name: "behind our proxy, Cloudflare reached it", remote: "127.0.0.1:1", trustProxy: true, xff: "104.16.0.1",
			cc: "LI", cip: "203.0.113.7", country: "LI", ip: "203.0.113.7"},
		{name: "behind our proxy, a client-sent Cloudflare address left of the real peer", remote: "127.0.0.1:1", trustProxy: true,
			xff: "104.16.0.1, 198.51.100.1", cc: "CH", cip: "203.0.113.7", ip: "198.51.100.1"},
		{name: "Cloudflare doesn't know the country", remote: "104.16.0.1:443", cc: "XX", cip: "203.0.113.7", ip: "203.0.113.7"},
		{name: "Tor", remote: "104.16.0.1:443", cc: "T1", cip: "203.0.113.7", ip: "203.0.113.7"},
		{name: "garbage country", remote: "104.16.0.1:443", cc: "Switzerland", ip: "104.16.0.1"},
		{name: "Cloudflare without CF-Connecting-IP", remote: "104.16.0.1:443", cc: "FR", country: "FR", ip: "104.16.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			for k, v := range map[string]string{"X-Forwarded-For": tc.xff, "CF-IPCountry": tc.cc, "CF-Connecting-IP": tc.cip} {
				if v != "" {
					r.Header.Set(k, v)
				}
			}
			if got := ClientCountry(r, tc.trustProxy); got != tc.country {
				t.Errorf("ClientCountry = %q, want %q", got, tc.country)
			}
			if got := ClientIP(r, tc.trustProxy); got != tc.ip {
				t.Errorf("ClientIP = %q, want %q", got, tc.ip)
			}
		})
	}
}

// The dashboard relays Cloudflare's country to the API in its own header, and never a browser's copy of it.
func TestAPIProxyRelaysCountry(t *testing.T) {
	var got *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	h := Handler(site, u, slog.New(slog.DiscardHandler), true, "s3cret", false)

	do(h, "GET", "/api/v1/plans", nil, map[string]string{"X-Forwarded-For": "104.16.0.1", "CF-IPCountry": "CH", "CF-Connecting-IP": "203.0.113.7"})
	if got.Header.Get(CountryHeader) != "CH" || got.Header.Get("X-Forwarded-For") != "203.0.113.7" {
		t.Fatalf("through Cloudflare: country %q, X-Forwarded-For %q", got.Header.Get(CountryHeader), got.Header.Get("X-Forwarded-For"))
	}
	do(h, "GET", "/api/v1/plans", nil, map[string]string{"X-Forwarded-For": "198.51.100.1", "CF-IPCountry": "CH", CountryHeader: "LI"})
	if v := got.Header.Values(CountryHeader); len(v) != 0 || got.Header.Get("X-Forwarded-For") != "198.51.100.1" {
		t.Fatalf("not through Cloudflare: country %v, X-Forwarded-For %q", v, got.Header.Get("X-Forwarded-For"))
	}
}
