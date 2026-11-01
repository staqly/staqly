package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAdminHandler(t *testing.T) {
	site := fstest.MapFS{
		"index.html":     {Data: []byte("<html>home</html>")},
		"workspace.html": {Data: []byte("<html>ws</html>")},
		"404.html":       {Data: []byte("<html>nope</html>")},
	}
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "api:"+r.URL.Path) })
	h := AdminHandler(site, stub, slog.New(slog.DiscardHandler))
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}
	if r := get("/api/v1/session"); r.Code != 404 || !strings.Contains(r.Body.String(), "not_found") {
		t.Fatalf("user API: %d %s", r.Code, r.Body)
	}
	if r := get("/api/v1/admin/me"); r.Code != 200 || r.Body.String() != "api:/v1/admin/me" {
		t.Fatalf("admin API: %d %s", r.Code, r.Body)
	}
	r := get("/workspace?id=w1")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "ws") {
		t.Fatalf("page: %d %s", r.Code, r.Body)
	}
	for k, v := range map[string]string{"X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff"} {
		if r.Header().Get(k) != v {
			t.Errorf("%s = %q", k, r.Header().Get(k))
		}
	}
	if !strings.Contains(r.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("no CSP")
	}
	if r := get("/nothing"); r.Code != 404 || !strings.Contains(r.Body.String(), "nope") {
		t.Fatalf("unknown: %d %s", r.Code, r.Body)
	}
}
