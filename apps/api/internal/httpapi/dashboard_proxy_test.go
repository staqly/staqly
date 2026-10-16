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
	dash := httptest.NewServer(web.Handler(fstest.MapFS{}, u, log, true))
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
