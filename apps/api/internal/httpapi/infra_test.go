package httpapi

// White-box tests for the infrastructure audit (2026-10): idempotency, limits, middleware.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// A client that hangs up mid-request (the usual reason to retry with the same Idempotency-Key) gets
// 499 logged, and the key must be released: storing the 499 would replay an empty 499 for 24 h.
func TestIdempotencyReleasesWhenClientHangsUp(t *testing.T) {
	s := testServer(t, app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	res, err := s.svc.Setup(context.Background(), "w", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.svc.AuthenticateAPIKey(context.Background(), res.APIKey.Key)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), principalKey, p))
	r := httptest.NewRequest("POST", "/v1/x", strings.NewReader(`{}`)).WithContext(ctx)
	r.Header.Set("Idempotency-Key", "k-gone")
	h := func(w http.ResponseWriter, r *http.Request) {
		cancel() // the client went away while the handler waited on the database
		writeErr(w, r, r.Context().Err())
	}
	s.idempotency(http.HandlerFunc(h)).ServeHTTP(httptest.NewRecorder(), r)
	if _, err := s.svc.Q.GetIdempotencyKey(context.Background(), db.GetIdempotencyKeyParams{WorkspaceID: p.WorkspaceID,
		PrincipalID: p.ID, Key: "k-gone"}); err == nil {
		t.Fatal("a request the client abandoned was stored; the retry would replay an empty 499")
	}
}

// Two requests with one Idempotency-Key at once: the second is refused as in progress (it never runs the
// handler), a retry after the first finished replays it, and another principal's same key is its own.
func TestIdempotencyConcurrentSameKey(t *testing.T) {
	s := testServer(t, app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	ctx := context.Background()
	res, err := s.svc.Setup(ctx, "w", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.svc.AuthenticateAPIKey(ctx, res.APIKey.Key)
	other := p
	other.ID = "key_other"
	var runs atomic.Int32
	release := make(chan struct{})
	h := s.idempotency(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if runs.Add(1) == 1 {
			<-release
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"ok":true}`))
	}))
	call := func(p app.Principal) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/x", strings.NewReader(`{"a":1}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "same")
		r = r.WithContext(context.WithValue(r.Context(), principalKey, p))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	first := make(chan *httptest.ResponseRecorder)
	go func() { first <- call(p) }()
	for runs.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if rec := call(p); rec.Code != 409 || !strings.Contains(rec.Body.String(), "idempotency_in_progress") {
		t.Fatalf("concurrent duplicate: %d %s", rec.Code, rec.Body)
	}
	close(release)
	if rec := <-first; rec.Code != 201 {
		t.Fatalf("first: %d", rec.Code)
	}
	if rec := call(p); rec.Code != 201 || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("retry after completion: %d %v", rec.Code, rec.Header())
	}
	if rec := call(other); rec.Code != 201 || rec.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("another principal's key replayed someone else's response: %d %v", rec.Code, rec.Header())
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("handler ran %d times, want 2 (first + other principal)", n)
	}
}

// A panicking handler answers with the usual JSON error envelope (chi's Recoverer sent an empty 500 and
// printed a coloured stack to stderr, next to the JSON log line obs already writes).
func TestPanicAnswersJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })).ServeHTTP(rec,
		httptest.NewRequest("GET", "/v1/x", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"code":"internal"`) ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("panic answer: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Fatal("http.ErrAbortHandler must keep aborting the response")
		}
	}()
	recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/x", nil))
}

// A reservation left by a request that never finished (the process died with it in flight) blocks its key
// only for staleReservation; after that a retry of the same request takes it over and runs. A fresh one
// still answers in progress, and a different request is still a mismatch.
func TestIdempotencyTakesOverStaleReservation(t *testing.T) {
	s := testServer(t, app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	ctx := context.Background()
	res, err := s.svc.Setup(ctx, "w", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.svc.AuthenticateAPIKey(ctx, res.APIKey.Key)
	req := func(key, body string) *http.Request {
		r := httptest.NewRequest("POST", "/v1/x", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		return r.WithContext(context.WithValue(r.Context(), principalKey, p))
	}
	reserve := func(key string, age time.Duration) {
		if _, err := s.svc.W.ReserveIdempotencyKey(ctx, db.ReserveIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, PrincipalID: p.ID,
			Key: key, RequestHash: requestHash(req(key, `{"a":1}`), []byte(`{"a":1}`)),
			CreatedAt: time.Now().Add(-age).UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
	}
	var runs atomic.Int32
	h := s.idempotency(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runs.Add(1)
		w.WriteHeader(201)
		w.Write([]byte(`{"ok":true}`))
	}))
	call := func(key, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req(key, body))
		return rec
	}
	reserve("fresh", time.Minute)
	if rec := call("fresh", `{"a":1}`); rec.Code != 409 || !strings.Contains(rec.Body.String(), "idempotency_in_progress") {
		t.Fatalf("fresh reservation: %d %s", rec.Code, rec.Body)
	}
	reserve("stale", staleReservation+time.Minute)
	if rec := call("stale", `{"a":2}`); rec.Code != 422 {
		t.Fatalf("stale reservation, other request: %d %s", rec.Code, rec.Body)
	}
	if rec := call("stale", `{"a":1}`); rec.Code != 201 || runs.Load() != 1 {
		t.Fatalf("stale reservation not taken over: %d %s (runs %d)", rec.Code, rec.Body, runs.Load())
	}
	if rec := call("stale", `{"a":1}`); rec.Header().Get("Idempotent-Replayed") != "true" || runs.Load() != 1 {
		t.Fatalf("taken-over request not stored for replay: %d %v", rec.Code, rec.Header())
	}
}
