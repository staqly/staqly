package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/web"
)

// Cloud behind Caddy (TRUST_PROXY): browsers reach the API through the dashboard in the same process. Each
// browser keeps its own per-IP limits (sign-up here), instead of all of them sharing the loopback peer of the
// dashboard's relay, where one visitor could use up sign-up, recovery and connect-link limits for everyone.
func TestDashboardRelayKeepsPerBrowserLimits(t *testing.T) {
	dir := t.TempDir()
	database, err := store.Open(context.Background(), dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.DiscardHandler)
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, UploadDir: dir + "/uploads", PublicURL: "http://test"})
	api := httptest.NewServer(httpapi.New(svc, log, httpapi.Config{TrustProxy: true, OpenAPISpec: []byte("openapi: 3.1.0")}))
	t.Cleanup(api.Close)
	u, _ := url.Parse(api.URL)
	dash := httptest.NewServer(web.Handler(fstest.MapFS{}, u, log, true, ""))
	t.Cleanup(dash.Close)

	signup := func(browser string) int {
		req, _ := http.NewRequest("POST", dash.URL+"/api/v1/workspaces", bytes.NewReader([]byte(`{"name":"W"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", browser) // what Caddy appends
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := range 5 {
		if code := signup("203.0.113.1"); code != 201 {
			t.Fatalf("signup %d: %d", i, code)
		}
	}
	if code := signup("203.0.113.1"); code != 429 {
		t.Fatalf("6th signup from one browser: %d, want 429", code)
	}
	for i := range 3 {
		if code := signup(fmt.Sprintf("198.51.100.%d", i+1)); code != 201 {
			t.Fatalf("another browser: %d, want 201", code)
		}
	}
}

// Without TRUST_PROXY (staqly serve --expose, the image) the API still keys per-IP limits by the browser the
// embedded dashboard names, but only on requests that carry the dashboard's relay secret: anyone else's
// X-Forwarded-For is ignored.
func TestRelaySecretKeepsPerBrowserLimits(t *testing.T) {
	dir := t.TempDir()
	database, err := store.Open(context.Background(), dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.DiscardHandler)
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, UploadDir: dir + "/uploads", PublicURL: "http://test"})
	api := httptest.NewServer(httpapi.New(svc, log, httpapi.Config{RelayToken: "s3cret", OpenAPISpec: []byte("openapi: 3.1.0")}))
	t.Cleanup(api.Close)

	signup := func(browser, relay string) int {
		req, _ := http.NewRequest("POST", api.URL+"/v1/workspaces", bytes.NewReader([]byte(`{"name":"W"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", browser)
		if relay != "" {
			req.Header.Set(web.RelayHeader, relay)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := range 5 {
		if code := signup("203.0.113.1", "s3cret"); code != 201 {
			t.Fatalf("signup %d: %d", i, code)
		}
	}
	if code := signup("203.0.113.1", "s3cret"); code != 429 {
		t.Fatalf("6th signup from one browser: %d, want 429", code)
	}
	if code := signup("198.51.100.1", "s3cret"); code != 201 {
		t.Fatalf("another browser: %d, want 201", code)
	}
	// Without the secret (or with a wrong one) X-Forwarded-For is the caller's claim: all count as the peer.
	for i := range 5 {
		if code := signup(fmt.Sprintf("192.0.2.%d", i+1), "forged"); code != 201 {
			t.Fatalf("unrelayed signup %d: %d", i, code)
		}
	}
	if code := signup("192.0.2.99", ""); code != 429 {
		t.Fatalf("spoofed X-Forwarded-For escaped the limit: %d", code)
	}
}
