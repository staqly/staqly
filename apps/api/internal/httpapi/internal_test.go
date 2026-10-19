package httpapi

// White-box tests for pieces that can't be reached through a real TCP
// connection from a test (non-loopback peers) or are pure helpers.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

func testServer(t *testing.T, cfg app.Config) *Server {
	t.Helper()
	database, err := storetest.Open(t, t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	svc := app.New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
	return &Server{svc: svc, log: svc.Log, tests: newTokenBucket(10, 5, 100)}
}

// Local mode has no keyless access: a request from this machine without the access token is refused like
// one from the LAN, and the token works from any address (the API's 127.0.0.1 listener is what keeps the
// LAN out by default).
func TestLocalModeNeedsTheTokenFromEveryPeer(t *testing.T) {
	const tok = "stk_local_whiteboxwhiteboxwhiteboxwhitebox"
	s := testServer(t, app.Config{UploadDir: t.TempDir(), LocalToken: tok})
	if err := s.svc.EnsureLocalWorkspace(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := New(s.svc, s.log, Config{})
	call := func(peer, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/collections", nil)
		r.RemoteAddr, r.Host = peer, "localhost:3333"
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, p := range []string{"127.0.0.1:5000", "[::1]:5000", "192.168.1.50:5000", "172.17.0.2:1"} {
		if w := call(p, ""); w.Code != 401 || !strings.Contains(w.Body.String(), "access token") {
			t.Fatalf("no token from %s: %d %s", p, w.Code, w.Body)
		}
		if w := call(p, tok+"x"); w.Code != 401 {
			t.Fatalf("wrong token from %s: %d", p, w.Code)
		}
		if w := call(p, tok); w.Code != 200 {
			t.Fatalf("token from %s: %d %s", p, w.Code, w.Body)
		}
	}
}

// Audit M5 + L10: with TRUST_PROXY the right-most X-Forwarded-For entry is
// the client (the left part is client-controlled); IPv6 is keyed per /64.
func TestClientIPRightMostAndIPv6Prefix(t *testing.T) {
	s := &Server{cfg: Config{TrustProxy: true}}
	req := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	cases := []struct {
		r    *http.Request
		want string
	}{
		{req("10.0.0.1:1", "1.2.3.4, 198.51.100.7"), "198.51.100.7"},
		{req("10.0.0.1:1", "spoofed", "1.2.3.4,198.51.100.8"), "198.51.100.8"},
		{req("10.0.0.1:1", "1.2.3.4, not-an-ip"), "10.0.0.1"},
		{req("10.0.0.1:1"), "10.0.0.1"},
		{req("10.0.0.1:1", "2001:db8:1:2:aaaa::1"), "2001:db8:1:2::/64"},
		{req("[2001:db8:5:6:1::9]:1"), "2001:db8:5:6::/64"},
	}
	for i, c := range cases {
		if got := s.clientIP(c.r); got != c.want {
			t.Errorf("case %d: %s, want %s", i, got, c.want)
		}
	}
	s.cfg.TrustProxy = false
	if got := s.clientIP(req("192.0.2.1:1", "198.51.100.7")); got != "192.0.2.1" {
		t.Fatalf("without TRUST_PROXY the header must be ignored: %s", got)
	}
}

// Audit L10: limiters evict instead of resetting, so an attacker spraying
// keys can't clear their own counter.
func TestRateLimitersEvictInsteadOfReset(t *testing.T) {
	l := newRateLimiter(2, time.Hour)
	l.maxKeys = 100
	first, second, third := l.allow("attacker"), l.allow("attacker"), l.allow("attacker")
	if !first || !second || third {
		t.Fatal("attacker should be limited after 2")
	}
	for i := range 500 {
		l.allow(fmt.Sprintf("spray-%d", i))
	}
	if len(l.hits) > 100 {
		t.Fatalf("limiter grew to %d keys", len(l.hits))
	}
	if l.allow("attacker") {
		t.Fatal("attacker counter was reset by spraying keys")
	}

	b := newTokenBucket(1, 5, 100)
	now := time.Now()
	b.now = func() time.Time { return now }
	for range 5 {
		if ok, _ := b.take("attacker"); !ok {
			t.Fatal("burst take")
		}
	}
	for i := range 500 {
		now = now.Add(time.Millisecond)
		b.take(fmt.Sprintf("spray-%d", i))
	}
	if len(b.buckets) > 100 {
		t.Fatalf("bucket map grew to %d", len(b.buckets))
	}
	if ok, _ := b.take("attacker"); ok {
		t.Fatal("drained bucket was reset by spraying keys")
	}
}

// Audit M8: the reservation is released when the handler panics or the
// response is too large to replay, and the query string is part of the hash.
func TestIdempotencyReleasesOnPanicAndOverflow(t *testing.T) {
	s := testServer(t, app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	res, err := s.svc.Setup(context.Background(), "w", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.svc.AuthenticateAPIKey(context.Background(), res.APIKey.Key)
	call := func(h http.HandlerFunc, key, target string) (rec *httptest.ResponseRecorder, panicked bool) {
		defer func() { panicked = recover() != nil }()
		r := httptest.NewRequest("POST", target, strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(context.WithValue(r.Context(), principalKey, p))
		rec = httptest.NewRecorder()
		s.idempotency(h).ServeHTTP(rec, r)
		return rec, false
	}
	reserved := func(key string) bool {
		_, err := s.svc.Q.GetIdempotencyKey(context.Background(), db.GetIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: key})
		return err == nil
	}
	if _, panicked := call(func(http.ResponseWriter, *http.Request) { panic("boom") }, "k-panic", "/v1/x"); !panicked {
		t.Fatal("panic should propagate to the recoverer")
	}
	if reserved("k-panic") {
		t.Fatal("a panicking handler left the key reserved (retries would get 409 for 24h)")
	}
	big := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(strings.Repeat("x", maxStoredResponse+10)))
	}
	if rec, _ := call(big, "k-big", "/v1/x"); rec.Body.Len() != maxStoredResponse+10 {
		t.Fatalf("client must still get the full body: %d", rec.Body.Len())
	}
	if reserved("k-big") {
		t.Fatal("an oversized response was stored (truncated) instead of released")
	}
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); w.Write([]byte(`{"ok":true}`)) }
	if rec, _ := call(ok, "k-q", "/v1/x?a=1"); rec.Code != 201 || !reserved("k-q") {
		t.Fatalf("normal response must be stored: %d", rec.Code)
	}
	if rec, _ := call(ok, "k-q", "/v1/x?a=2"); rec.Code != 422 || !strings.Contains(rec.Body.String(), "idempotency_mismatch") {
		t.Fatalf("different query string must not replay: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := call(ok, "k-q", "/v1/x?a=1"); rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("same request must replay: %v", rec.Header())
	}
}

// A database locked past busy_timeout (e.g. by another process) is
// transient: 503 + Retry-After, never 500.
func TestBusyDatabaseIs503(t *testing.T) {
	path := t.TempDir() + "/w.db"
	database, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := database.BeginTx(context.Background(), nil) // BEGIN IMMEDIATE holds the write lock
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, busy := other.Exec("CREATE TABLE x (a)")
	if !app.IsBusy(busy) {
		t.Fatalf("want SQLITE_BUSY, got %v", busy)
	}
	if app.IsBusy(fmt.Errorf("database is locked")) {
		t.Fatal("plain errors are not busy")
	}
	rec := httptest.NewRecorder()
	writeErr(rec, httptest.NewRequest("POST", "/v1/posts", nil), fmt.Errorf("create: %w", busy))
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), `"busy"`) {
		t.Fatalf("got %d %q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
	}
}
