package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// obsEnv is a server whose feature env vars come from vars (svc.Getenv).
type obsEnv struct {
	t   *testing.T
	svc *app.Service
	srv *httptest.Server
	key string
}

func newObsEnv(t *testing.T, cloud, trustProxy bool, vars map[string]string) *obsEnv {
	t.Helper()
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := app.Config{UploadDir: dir + "/uploads", LocalToken: testLocalToken}
	if cloud {
		cfg.Mode, cfg.SetupToken, cfg.PublicURL = app.ModeCloud, "tok", "http://test"
	}
	svc := app.New(database, log, cfg)
	svc.Getenv = func(k string) string { return vars[k] }
	if !cloud {
		if err := svc.EnsureLocalWorkspace(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var h http.Handler = httpapi.New(svc, log, httpapi.Config{MCP: mcpserver.HTTPHandler(svc, "test"),
		TrustProxy: trustProxy, Version: "test"})
	if !cloud {
		h = withLocalToken(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e := &obsEnv{t: t, svc: svc, srv: srv}
	if cloud {
		var res struct {
			APIKey app.APIKey `json:"api_key"`
		}
		code, body, _ := e.req("POST", "/v1/workspaces", `{"name":"T","setup_token":"tok"}`)
		if code != 201 {
			t.Fatalf("setup: %d %s", code, body)
		}
		json.Unmarshal([]byte(body), &res)
		e.key = res.APIKey.Key
	}
	return e
}

// req sends a request; hdr are header name/value pairs ("Host" sets req.Host).
func (e *obsEnv) req(method, path, body string, hdr ...string) (int, string, http.Header) {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			r.Host = hdr[i+1]
		} else {
			r.Header.Set(hdr[i], hdr[i+1])
		}
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), resp.Header
}

func TestObsRequestIDHeader(t *testing.T) {
	e := newEnv(t)
	resp := e.do(nil, "GET", "/v1/collections", e.key, nil, 200, nil)
	if resp.Header.Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
	resp = e.do(nil, "GET", "/v1/posts/nope", e.key, nil, 404, nil, "X-Request-Id", "trace-abc-123")
	if got := resp.Header.Get("X-Request-Id"); got != "trace-abc-123" {
		t.Fatalf("X-Request-Id %q, want the caller's id echoed", got)
	}
	resp = e.do(nil, "GET", "/no-such-path", "", nil, 404, nil)
	if resp.Header.Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id on unmatched route")
	}
}

func TestObsLivezReadyz(t *testing.T) {
	e := newObsEnv(t, true, false, nil)
	if code, body, _ := e.req("GET", "/livez", ""); code != 200 || !strings.Contains(body, `"ok"`) {
		t.Fatalf("livez %d %s", code, body)
	}
	var rd struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	readyz := func() int {
		code, body, _ := e.req("GET", "/readyz", "")
		rd.Checks = nil
		json.Unmarshal([]byte(body), &rd)
		return code
	}
	if code := readyz(); code != 200 || rd.Checks["database"] != "ok" || rd.Checks["migrations"] != "ok" || rd.Checks["workers"] != "not_started" {
		t.Fatalf("readyz %d %+v", code, rd)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunBackground(ctx); close(done) }()
	waitFor(t, func() bool { return readyz() == 200 && rd.Checks["workers"] == "ok" })
	cancel()
	<-done
	if code := readyz(); code != 503 || rd.Checks["workers"] != "stopped" || rd.Status != "unavailable" {
		t.Fatalf("readyz after stop %d %+v", code, rd)
	}

	e.svc.DB.Close()
	if code := readyz(); code != 503 || rd.Checks["database"] != "unavailable" {
		t.Fatalf("readyz with closed db %d %+v", code, rd)
	}
	if code, _, _ := e.req("GET", "/livez", ""); code != 200 {
		t.Fatalf("livez must not depend on the database: %d", code)
	}
}

func TestObsMetricsContentAndProtection(t *testing.T) {
	e := newObsEnv(t, true, false, map[string]string{"METRICS_TOKEN": "scrape-secret"})
	if code, body, _ := e.req("POST", "/v1/collections", `{"name":"C"}`, "Authorization", "Bearer "+e.key); code != 201 {
		t.Fatalf("create collection %d %s", code, body)
	}
	e.req("GET", "/v1/collections", "", "Authorization", "Bearer "+e.key)
	e.req("GET", "/v1/posts/post_secret_id", "", "Authorization", "Bearer "+e.key)

	code, body, hdr := e.req("GET", "/metrics", "")
	if code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("local scrape %d %s", code, hdr.Get("Content-Type"))
	}
	for _, want := range []string{
		`http_requests_total{method="GET",route="/v1/collections",status="200"} 1`,
		`http_requests_total{method="POST",route="/v1/collections",status="201"} 1`,
		`http_requests_total{method="GET",route="/v1/posts/{id}",status="404"} 1`,
		`http_request_duration_seconds_bucket{method="GET",route="/v1/collections",le="+Inf"} 1`,
		`# TYPE http_request_duration_seconds histogram`,
		`staqly_webhook_deliveries{status="pending"} 0`,
		`staqly_posts{status="draft"} 0`,
		`staqly_channels{status="connected"} 0`,
		`staqly_jobs{status="queued"} 0`,
		`staqly_webhook_outbox_oldest_pending_seconds 0`,
		`staqly_webhook_outbox_due 0`,
		`staqly_metrics_db_up 1`,
		`staqly_build_info{version="test",mode="cloud"} 1`,
		`process_start_time_seconds `,
		`go_goroutines `,
		`go_memstats_alloc_bytes `,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	if strings.Contains(body, "post_secret_id") || strings.Contains(body, e.key) {
		t.Fatal("raw path or key leaked into metrics")
	}

	// Not local: proxied, foreign Host, or a wrong token → 404.
	for _, hdrs := range [][]string{
		{"X-Forwarded-For", "203.0.113.9"},
		{"Host", "metrics.example.com"},
		{"X-Forwarded-For", "203.0.113.9", "Authorization", "Bearer wrong"},
		{"Origin", "http://evil.example"},
	} {
		if code, body, _ := e.req("GET", "/metrics", "", hdrs...); code != 404 || strings.Contains(body, "http_requests_total") {
			t.Fatalf("%v: status %d", hdrs, code)
		}
	}
	if code, body, _ := e.req("GET", "/metrics", "", "X-Forwarded-For", "203.0.113.9", "Authorization", "Bearer scrape-secret"); code != 200 || !strings.Contains(body, "go_goroutines") {
		t.Fatalf("token scrape %d", code)
	}
}

func TestObsMetricsWithoutTokenIsLoopbackOnly(t *testing.T) {
	e := newObsEnv(t, false, false, nil)
	if code, _, _ := e.req("GET", "/metrics", "", "Authorization", "Bearer anything", "X-Forwarded-For", "10.0.0.1"); code != 404 {
		t.Fatalf("remote scrape without METRICS_TOKEN configured: %d", code)
	}
	if code, body, _ := e.req("GET", "/metrics", ""); code != 200 || !strings.Contains(body, `mode="local"`) {
		t.Fatalf("local scrape %d", code)
	}
}

func TestObsRateLimitPerPrincipal(t *testing.T) {
	e := newObsEnv(t, true, false, map[string]string{"RATE_LIMIT_AUTH_PER_MINUTE": "600", "RATE_LIMIT_AUTH_BURST": "3"})
	auth := []string{"Authorization", "Bearer " + e.key}
	for i := range 3 {
		if code, body, _ := e.req("GET", "/v1/collections", "", auth...); code != 200 {
			t.Fatalf("request %d: %d %s", i, code, body)
		}
	}
	code, body, hdr := e.req("GET", "/v1/collections", "", auth...)
	if code != 429 || hdr.Get("Retry-After") != "1" || !strings.Contains(body, `"rate_limited"`) {
		t.Fatalf("over limit: %d Retry-After=%q %s", code, hdr.Get("Retry-After"), body)
	}
	if hdr.Get("X-Request-Id") == "" {
		t.Fatal("429 without X-Request-Id")
	}
	// Another principal (anonymous caller, keyed by IP) has its own bucket.
	if code, _, _ := e.req("GET", "/v1/collections", ""); code != 401 {
		t.Fatalf("other principal: %d", code)
	}
	// Probes and metrics are never limited.
	for range 5 {
		if code, _, _ := e.req("GET", "/livez", "", auth...); code != 200 {
			t.Fatal("livez limited")
		}
	}
	// 10 tokens/s: the bucket refills after ~100ms.
	time.Sleep(150 * time.Millisecond)
	if code, _, _ := e.req("GET", "/v1/collections", "", auth...); code != 200 {
		t.Fatalf("after refill: %d", code)
	}
	if _, body, _ := e.req("GET", "/metrics", ""); !strings.Contains(body, `staqly_rate_limited_total{scope="principal"} 1`) {
		t.Fatal("rate-limited counter missing")
	}
}

func TestObsSensitiveEndpointsLimitedPerIP(t *testing.T) {
	e := newObsEnv(t, true, true, nil) // TRUST_PROXY on: X-Forwarded-For identifies the client
	ip := []string{"X-Forwarded-For", "198.51.100.7"}
	for _, path := range []string{"/v1/sessions/recover", "/v1/sessions/api-key", "/v1/device-links/redeem"} {
		for i := range 10 {
			if code, body, _ := e.req("POST", path, `{}`, ip...); code == 429 {
				t.Fatalf("%s request %d limited early: %s", path, i, body)
			}
		}
		code, body, hdr := e.req("POST", path, `{}`, ip...)
		if code != 429 || hdr.Get("Retry-After") == "" || !strings.Contains(body, `"rate_limited"`) {
			t.Fatalf("%s: 11th request %d %s", path, code, body)
		}
		// A different client IP is unaffected.
		if code, _, _ := e.req("POST", path, `{}`, "X-Forwarded-For", "198.51.100.8"); code == 429 {
			t.Fatalf("%s: other IP limited", path)
		}
	}
	// The limit is per IP, not per credential: a valid API key does not bypass it.
	if code, _, _ := e.req("POST", "/v1/sessions/recover", `{}`, append(ip, "Authorization", "Bearer "+e.key)...); code != 429 {
		t.Fatalf("authenticated caller bypassed IP limit: %d", code)
	}
}

func TestObsRateLimitDisabled(t *testing.T) {
	e := newObsEnv(t, true, false, map[string]string{"RATE_LIMIT_DISABLED": "true", "RATE_LIMIT_AUTH_BURST": "1"})
	for i := range 15 {
		if code, _, _ := e.req("POST", "/v1/sessions/recover", `{}`); code == 429 {
			t.Fatalf("request %d limited although disabled", i)
		}
		if code, _, _ := e.req("GET", "/v1/collections", "", "Authorization", "Bearer "+e.key); code != 200 {
			t.Fatalf("request %d: %d", i, code)
		}
	}
}

func TestObsLocalModeNotLimitedPerPrincipal(t *testing.T) {
	e := newObsEnv(t, false, false, map[string]string{"RATE_LIMIT_AUTH_PER_MINUTE": "1", "RATE_LIMIT_AUTH_BURST": "1"})
	for i := range 10 {
		if code, body, _ := e.req("GET", "/v1/collections", ""); code != 200 {
			t.Fatalf("request %d: %d %s", i, code, body)
		}
	}
}

// Local installs record the person (LOCAL_USER_NAME, else the OS user) as the actor.
func TestLocalActorName(t *testing.T) {
	e := newObsEnv(t, false, false, map[string]string{"LOCAL_USER_NAME": "Jane"})
	if code, body, _ := e.req("POST", "/v1/collections", `{"name":"Mine"}`); code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	_, body, _ := e.req("GET", "/v1/events?type=collection.created", "")
	var page app.EventPage
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Data) != 1 {
		t.Fatalf("events: %s", body)
	}
	if a := page.Data[0].Actor; a.Type != "local" || a.Name != "Jane" {
		t.Fatalf("actor: %+v", a)
	}
}

// A fresh random bearer token (or session cookie) per request must not get a fresh per-credential bucket
// each time: that made the per-principal limit (and the per-IP one for anonymous callers) free to
// bypass, every request costing a database lookup. First sightings are limited per client IP.
func TestObsRandomCredentialsDoNotBypassLimit(t *testing.T) {
	e := newObsEnv(t, true, true, nil)
	ip := []string{"X-Forwarded-For", "198.51.100.9"}
	if code, body, _ := e.req("GET", "/v1/collections", "", append(ip, "Authorization", "Bearer "+e.key)...); code != 200 {
		t.Fatalf("valid key: %d %s", code, body)
	}
	limited := 0
	for i := range 200 {
		code, _, _ := e.req("GET", "/v1/collections", "", append(ip, "Authorization", fmt.Sprintf("Bearer stk_guess%d", i))...)
		if code == 429 {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("200 requests with random bearer tokens from one IP were never limited")
	}
	// A key in use keeps its own bucket, from the same IP too; other clients are unaffected.
	if code, body, _ := e.req("GET", "/v1/collections", "", append(ip, "Authorization", "Bearer "+e.key)...); code != 200 {
		t.Fatalf("valid key after the spray: %d %s", code, body)
	}
	if code, _, _ := e.req("GET", "/v1/collections", "", "X-Forwarded-For", "198.51.100.10", "Authorization", "Bearer stk_other"); code != 401 {
		t.Fatalf("other IP: %d", code)
	}
}
