package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
)

var site = fstest.MapFS{
	"index.html":               {Data: []byte("home")},
	"inbox.html":               {Data: []byte("inbox")},
	"inbox.txt":                {Data: []byte("inbox-rsc")},
	"404.html":                 {Data: []byte("missing")},
	"items/_.html":             {Data: []byte("item-shell")},
	"items/_.txt":              {Data: []byte("item-rsc")},
	"items/_/__next._tree.txt": {Data: []byte("item-tree")},
	"_next/static/app.js":      {Data: []byte("js")},
}

var nowhere = DialURL("127.0.0.1:1")

func do(h http.Handler, method, target string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFiles(t *testing.T) {
	h := Handler(site, nowhere, slog.New(slog.DiscardHandler))
	for path, want := range map[string]string{
		"/": "home", "/inbox": "inbox", "/inbox.txt": "inbox-rsc",
		"/items/itm_1": "item-shell", "/items/itm_1.txt": "item-rsc",
		"/items/itm_1/__next._tree.txt": "item-tree", "/_next/static/app.js": "js",
	} {
		w := do(h, "GET", path, nil, nil)
		if w.Code != 200 || w.Body.String() != want {
			t.Errorf("%s: %d %q, want %q", path, w.Code, w.Body.String(), want)
		}
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s: security headers missing", path)
		}
	}
	for _, path := range []string{"/nope", "/inbox/x", "/collections/c_1"} {
		if w := do(h, "GET", path, nil, nil); w.Code != 404 || w.Body.String() != "missing" {
			t.Errorf("%s: %d %q, want the 404 page", path, w.Code, w.Body.String())
		}
	}
	if w := do(h, "GET", "/../../etc/passwd", nil, nil); w.Code == 200 {
		t.Errorf("path traversal served: %q", w.Body.String())
	}
	if w := do(h, "GET", "/_next/static/app.js", nil, nil); !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("static assets not cached: %q", w.Header().Get("Cache-Control"))
	}
	if w := do(h, "POST", "/inbox", nil, nil); w.Code != 405 {
		t.Errorf("POST page: %d", w.Code)
	}
	if w := do(Handler(fstest.MapFS{}, nowhere, slog.New(slog.DiscardHandler)), "GET", "/", nil, nil); w.Code != 503 {
		t.Errorf("unbuilt dashboard: %d", w.Code)
	}
}

func TestAPIProxy(t *testing.T) {
	var got *http.Request
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(201)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	h := Handler(site, u, slog.New(slog.DiscardHandler))

	w := do(h, "POST", "http://phone.lan:8080/api/v1/items?x=1", strings.NewReader("payload"), map[string]string{
		"X-Forwarded-For": "1.2.3.4", "X-Forwarded-Host": "localhost", "Forwarded": "for=1.2.3.4", "X-Real-Ip": "1.2.3.4",
	})
	if w.Code != 201 || w.Body.String() != "ok" || len(w.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("response: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if got.URL.Path != "/v1/items" || got.URL.RawQuery != "x=1" || gotBody != "payload" {
		t.Fatalf("upstream got %s?%s %q", got.URL.Path, got.URL.RawQuery, gotBody)
	}
	// Client forwarding headers are replaced, never trusted.
	if got.Header.Get("X-Forwarded-Host") != "phone.lan:8080" || got.Header.Get("X-Forwarded-Proto") != "http" ||
		got.Header.Get("X-Forwarded-For") != "" || got.Header.Get("Forwarded") != "" || got.Header.Get("X-Real-Ip") != "" {
		t.Fatalf("forwarding headers: %v", got.Header)
	}
	if w := do(h, "GET", "/api", nil, nil); w.Code != 201 || got.URL.Path != "/" {
		t.Fatalf("/api → %q (%d)", got.URL.Path, w.Code)
	}

	big := httptest.NewRequest("POST", "/api/v1/media", strings.NewReader("x"))
	big.ContentLength = MaxProxyBodyBytes + 1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, big)
	if rec.Code != 413 {
		t.Fatalf("declared oversize body: %d", rec.Code)
	}

	down := Handler(site, nowhere, slog.New(slog.DiscardHandler))
	if w := do(down, "GET", "/api/health", nil, nil); w.Code != 502 || !strings.Contains(w.Body.String(), "bad_gateway") {
		t.Fatalf("API down: %d %s", w.Code, w.Body.String())
	}
}

func TestDialURL(t *testing.T) {
	for in, want := range map[string]string{":3333": "127.0.0.1:3333", "0.0.0.0:3333": "127.0.0.1:3333",
		"[::]:3333": "127.0.0.1:3333", "127.0.0.1:3333": "127.0.0.1:3333", "api:3333": "api:3333"} {
		if got := DialURL(in).Host; got != want {
			t.Errorf("DialURL(%q) = %q, want %q", in, got, want)
		}
	}
}
