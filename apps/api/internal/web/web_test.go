package web

import (
	"bytes"
	"compress/gzip"
	"fmt"
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
	"schedule.html":            {Data: []byte("schedule")},
	"schedule.txt":             {Data: []byte("schedule-rsc")},
	"404.html":                 {Data: []byte("missing")},
	"posts/_.html":             {Data: []byte("post-shell")},
	"posts/_.txt":              {Data: []byte("post-rsc")},
	"posts/_/__next._tree.txt": {Data: []byte("post-tree")},
	"channels/callback/_.html": {Data: []byte("callback")},
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
	h := Handler(site, nowhere, slog.New(slog.DiscardHandler), false, "")
	for path, want := range map[string]string{
		"/": "home", "/schedule": "schedule", "/schedule.txt": "schedule-rsc",
		"/posts/post_1": "post-shell", "/posts/post_1.txt": "post-rsc",
		"/posts/post_1/__next._tree.txt": "post-tree", "/channels/callback/telegram": "callback", "/_next/static/app.js": "js",
	} {
		w := do(h, "GET", path, nil, nil)
		if w.Code != 200 || w.Body.String() != want {
			t.Errorf("%s: %d %q, want %q", path, w.Code, w.Body.String(), want)
		}
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s: security headers missing", path)
		}
	}
	// Old bookmarks land on the new pages.
	for from, to := range map[string]string{"/inbox": "/schedule", "/notifications": "/alerts", "/collections": "/posts",
		"/collections/col_1": "/posts?collection=col_1"} {
		if w := do(h, "GET", from, nil, nil); w.Code != 301 || w.Header().Get("Location") != to {
			t.Errorf("%s: %d %q, want a redirect to %s", from, w.Code, w.Header().Get("Location"), to)
		}
	}
	for _, path := range []string{"/nope", "/schedule/x", "/items/itm_1", "/posts/post_1/x"} {
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
	if w := do(h, "POST", "/schedule", nil, nil); w.Code != 405 {
		t.Errorf("POST page: %d", w.Code)
	}
	if w := do(Handler(fstest.MapFS{}, nowhere, slog.New(slog.DiscardHandler), false, ""), "GET", "/", nil, nil); w.Code != 503 {
		t.Errorf("unbuilt dashboard: %d", w.Code)
	}
}

// Text assets go out gzipped to browsers that accept it, every file carries an ETag, and a revalidation
// with that ETag is a 304 (pages and RSC payloads are no-cache, so this is every navigation).
func TestFilesCompressionAndETag(t *testing.T) {
	big := strings.Repeat("console.log('staqly');\n", 200)
	h := Handler(fstest.MapFS{
		"index.html":           {Data: []byte("home")},
		"schedule.html":        {Data: []byte("<html>" + strings.Repeat("<p>schedule</p>", 200) + "</html>")},
		"_next/static/app.js":  {Data: []byte(big)},
		"brand/icon-192.png":   {Data: bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 400)},
		"_next/static/tiny.js": {Data: []byte("x")},
	}, nowhere, slog.New(slog.DiscardHandler), false, "")

	w := do(h, "GET", "/_next/static/app.js", nil, map[string]string{"Accept-Encoding": "gzip, deflate, br"})
	if w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("js not gzipped: %v", w.Header())
	}
	if w.Header().Get("Content-Length") != fmt.Sprint(w.Body.Len()) {
		t.Errorf("Content-Length %q for %d gzipped bytes", w.Header().Get("Content-Length"), w.Body.Len())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); string(got) != big {
		t.Fatalf("gzip body differs")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("content type %q", ct)
	}
	gzTag := w.Header().Get("ETag")

	w = do(h, "GET", "/_next/static/app.js", nil, nil)
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != big {
		t.Errorf("plain request got an encoded body")
	}
	plainTag := w.Header().Get("ETag")
	if gzTag == "" || plainTag == "" || gzTag == plainTag {
		t.Errorf("ETags: gzip %q, plain %q", gzTag, plainTag)
	}
	if w := do(h, "GET", "/_next/static/app.js", nil, map[string]string{"Accept-Encoding": "gzip;q=0"}); w.Header().Get("Content-Encoding") != "" {
		t.Errorf("gzip;q=0 still gzipped")
	}

	w = do(h, "GET", "/schedule", nil, map[string]string{"Accept-Encoding": "gzip"})
	tag := w.Header().Get("ETag")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("page: %d %v", w.Code, w.Header())
	}
	if w := do(h, "GET", "/schedule", nil, map[string]string{"Accept-Encoding": "gzip", "If-None-Match": tag}); w.Code != 304 || w.Body.Len() != 0 {
		t.Errorf("revalidation: %d (%d bytes), want 304", w.Code, w.Body.Len())
	}
	if w := do(h, "HEAD", "/schedule", nil, nil); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %d", w.Code, w.Body.Len())
	}
	// Images and tiny files stay as they are.
	for _, p := range []string{"/brand/icon-192.png", "/_next/static/tiny.js"} {
		if w := do(h, "GET", p, nil, map[string]string{"Accept-Encoding": "gzip"}); w.Header().Get("Content-Encoding") != "" || w.Header().Get("ETag") == "" {
			t.Errorf("%s: encoding %q etag %q", p, w.Header().Get("Content-Encoding"), w.Header().Get("ETag"))
		}
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
	h := Handler(site, u, slog.New(slog.DiscardHandler), false, "")

	w := do(h, "POST", "http://phone.lan:8080/api/v1/posts?x=1", strings.NewReader("payload"), map[string]string{
		"X-Forwarded-For": "1.2.3.4", "X-Forwarded-Host": "localhost", "Forwarded": "for=1.2.3.4", "X-Real-Ip": "1.2.3.4",
	})
	if w.Code != 201 || w.Body.String() != "ok" || len(w.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("response: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if got.URL.Path != "/v1/posts" || got.URL.RawQuery != "x=1" || gotBody != "payload" {
		t.Fatalf("upstream got %s?%s %q", got.URL.Path, got.URL.RawQuery, gotBody)
	}
	// Client forwarding headers are replaced, never trusted: X-Forwarded-For names the TCP peer.
	if got.Header.Get("X-Forwarded-Host") != "phone.lan:8080" || got.Header.Get("X-Forwarded-Proto") != "http" ||
		got.Header.Get("X-Forwarded-For") != "192.0.2.1" || got.Header.Get("Forwarded") != "" || got.Header.Get("X-Real-Ip") != "" {
		t.Fatalf("forwarding headers: %v", got.Header)
	}
	// Behind your own proxy (TRUST_PROXY) the browser is its right-most X-Forwarded-For entry.
	behind := Handler(site, u, slog.New(slog.DiscardHandler), true, "")
	do(behind, "GET", "/api/v1/posts", nil, map[string]string{"X-Forwarded-For": "6.6.6.6, 1.2.3.4"})
	if xff := got.Header.Values("X-Forwarded-For"); len(xff) != 1 || xff[0] != "1.2.3.4" {
		t.Fatalf("trusted proxy: X-Forwarded-For %v", xff)
	}
	// The relay secret is the dashboard's own: a browser's copy is dropped, and only a configured token is sent.
	do(h, "GET", "/api/v1/posts", nil, map[string]string{RelayHeader: "forged"})
	if v := got.Header.Values(RelayHeader); len(v) != 0 {
		t.Fatalf("browser relay header passed on: %v", v)
	}
	relayed := Handler(site, u, slog.New(slog.DiscardHandler), false, "s3cret")
	do(relayed, "GET", "/api/v1/posts", nil, map[string]string{RelayHeader: "forged"})
	if v := got.Header.Values(RelayHeader); len(v) != 1 || v[0] != "s3cret" {
		t.Fatalf("relay header: %v", v)
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

	down := Handler(site, nowhere, slog.New(slog.DiscardHandler), false, "")
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

// The editor's jsDelivr sources are per package version: a source path without a trailing "/" matches only that
// exact path, so "onnxruntime-web@" blocked every ONNX Runtime file, and a bare host would allow any package.
func TestEditorCSPPinsCDN(t *testing.T) {
	h := Handler(site, nowhere, slog.New(slog.DiscardHandler), false, "")
	csp := do(h, "GET", "/media/edit", nil, nil).Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"https://cdn.jsdelivr.net/npm/onnxruntime-web@1.27.0/",
		"https://cdn.jsdelivr.net/npm/@huggingface/transformers@4.2.0",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("editor CSP lacks %s: %s", want, csp)
		}
	}
	for _, src := range strings.Fields(csp) {
		src = strings.TrimSuffix(src, ";")
		if strings.HasPrefix(src, "https://cdn.jsdelivr.net") &&
			!(strings.HasSuffix(src, "/") || src == "https://cdn.jsdelivr.net/npm/@huggingface/transformers@4.2.0") {
			t.Errorf("editor CSP source %q is not a pinned package path", src)
		}
		if src == "https://cdn.jsdelivr.net" || src == "https://cdn.jsdelivr.net/" || src == "https://cdn.jsdelivr.net/npm/" {
			t.Errorf("editor CSP allows all of jsDelivr: %s", csp)
		}
	}
	if dash := do(h, "GET", "/schedule", nil, nil).Header().Get("Content-Security-Policy"); strings.Contains(dash, "jsdelivr") {
		t.Errorf("dashboard CSP allows jsDelivr: %s", dash)
	}
}

// Was a bug: a failed connection lands on the callback page with ?error=... (the API's answer); forwarding
// that to the API again made it wrap the error and redirect back, round and round. The platform's own answer
// carries the state; the landing doesn't, so it is the page.
func TestOAuthCallbackLandingIsThePage(t *testing.T) {
	var forwarded []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = append(forwarded, r.URL.Path+"?"+r.URL.RawQuery)
		w.WriteHeader(302)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	h := Handler(site, u, slog.New(slog.DiscardHandler), false, "")
	if w := do(h, "GET", "/channels/callback/linkedin?error=The+connection+failed", nil, nil); w.Code != 200 || len(forwarded) != 0 {
		t.Fatalf("landing: %d, forwarded %v", w.Code, forwarded)
	}
	do(h, "GET", "/channels/callback/linkedin?code=c&state=s", nil, nil)
	do(h, "GET", "/channels/callback/linkedin?error=access_denied&state=s", nil, nil)
	if len(forwarded) != 2 {
		t.Fatalf("the platform's answers forwarded: %v", forwarded)
	}
}
