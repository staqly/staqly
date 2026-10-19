package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := cors([]string{"https://docs.staqly.ai/"})(ok)
	do := func(method, origin string, preflight bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/v1/collections", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if preflight {
			r.Header.Set("Access-Control-Request-Method", "POST")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := do("OPTIONS", "https://docs.staqly.ai", true)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://docs.staqly.ai" ||
		w.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must never be allowed cross-origin")
	}
	if w := do("GET", "https://docs.staqly.ai", false); w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("simple request: %d %v", w.Code, w.Header())
	}
	for _, o := range []string{"https://evil.example", "", "null"} {
		if w := do("OPTIONS", o, true); w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("origin %q got CORS: %d %v", o, w.Code, w.Header())
		}
	}
	if w := httptest.NewRecorder(); true {
		cors(nil)(ok).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != http.StatusTeapot {
			t.Fatal("no origins: handler not called")
		}
	}
}
