package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// workUntil runs the webhook worker, as a server process does, until ok.
func workUntil(t *testing.T, s *Service, what string, ok func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, what, ok)
}

// Webhooks are at least once: the event and its delivery are committed together, so a process that stops at
// any point afterwards leaves a pending delivery the next process sends. Three places to stop: before any
// worker saw it, after a worker claimed it, and after the receiver answered but before that was recorded
// (the receiver then gets it twice, with the same delivery id to tell).
func TestWebhookIsDeliveredAfterTheProcessStops(t *testing.T) {
	for _, stop := range []string{"after the commit", "after the claim", "after the receiver answered"} {
		t.Run(stop, func(t *testing.T) {
			s, p, post := publishTest(t)
			ctx := t.Context()
			s.Cfg.Webhook.AllowLoopback, s.Cfg.Webhook.PollInterval = true, 5*time.Millisecond
			type request struct {
				delivery string
				attempt  int64
			}
			var mu sync.Mutex
			var got []request
			recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					DeliveryID string `json:"delivery_id"`
					Attempt    int64  `json:"attempt"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				got = append(got, request{r.Header.Get("X-Staqly-Delivery"), body.Attempt})
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(recv.Close)
			received := func() []request {
				mu.Lock()
				defer mu.Unlock()
				return append([]request{}, got...)
			}
			if _, err := s.CreateWebhook(ctx, p, WebhookInput{URL: &recv.URL, Events: &[]string{"post.unscheduled"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CancelPost(ctx, p, post.ID); err != nil { // the event and its delivery, in one commit
				t.Fatal(err)
			}
			delivery := func() (id, status string, attempts int64) {
				t.Helper()
				if err := s.DB.QueryRow(`SELECT id, status, attempts FROM webhook_deliveries WHERE workspace_id = ?`, p.WorkspaceID).Scan(&id, &status, &attempts); err != nil {
					t.Fatalf("the delivery: %v", err)
				}
				return id, status, attempts
			}
			id, status, attempts := delivery()
			if status != "pending" || attempts != 0 {
				t.Fatalf("setup: the delivery is %s after %d attempts", status, attempts)
			}

			now := time.Now()
			wantSends := 1
			switch stop {
			case "after the claim":
				due, err := s.Q.DueDeliveries(ctx, s.now(), 2, 50)
				if err != nil || len(due) != 1 {
					t.Fatalf("setup: due deliveries %d %v", len(due), err)
				}
				lease := formatTime(now.Add(s.Cfg.Webhook.Lease))
				if n, err := s.W.LeaseDelivery(ctx, db.LeaseDeliveryParams{LeaseUntil: lease, Now: nullStr(s.now()), ID: due[0].ID, ExpectedNext: due[0].NextAttemptAt}); err != nil || n != 1 {
					t.Fatalf("setup: claim %d %v", n, err)
				}
			case "after the receiver answered":
				lift := refuseWrites(t, s, `UPDATE ON webhook_deliveries WHEN NEW.status = 'succeeded'`)
				workUntil(t, s, "the first send", func() bool { return len(received()) == 1 })
				lift()
				wantSends = 2
			}
			if _, status, _ = delivery(); status != "pending" {
				t.Fatalf("after the process stopped %s the delivery is %s, want pending", stop, status)
			}

			// The next process, on the same database.
			next := New(s.DB, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: t.TempDir()})
			next.Cfg.Webhook.AllowLoopback, next.Cfg.Webhook.PollInterval = true, 5*time.Millisecond
			if stop != "after the commit" {
				// A claimed delivery is the claimer's until its lease runs out: nobody sends it meanwhile.
				next.Now = func() time.Time { return now.Add(next.Cfg.Webhook.Lease - 10*time.Second) }
				if due, err := next.Q.DueDeliveries(ctx, next.now(), 2, 50); err != nil || len(due) != 0 {
					t.Fatalf("%d deliveries due before the lease ran out (%v), want 0", len(due), err)
				}
				next.Now = func() time.Time { return now.Add(next.Cfg.Webhook.Lease + time.Second) }
			}
			workUntil(t, next, "the delivery to succeed", func() bool { _, st, _ := delivery(); return st == "succeeded" })

			reqs := received()
			if len(reqs) != wantSends {
				t.Fatalf("the receiver got %d requests, want %d: %+v", len(reqs), wantSends, reqs)
			}
			for i, r := range reqs {
				if r.delivery != id {
					t.Errorf("request %d carries delivery %s, want %s (the id a receiver deduplicates on)", i+1, r.delivery, id)
				}
			}
			_, _, attempts = delivery()
			if last := reqs[len(reqs)-1].attempt; last != attempts || (stop != "after the commit") != (attempts == 2) {
				t.Fatalf("the last request says attempt %d, the delivery %d", last, attempts)
			}
		})
	}
}

// The signature a receiver checks is the documented one: hex HMAC-SHA256 with the webhook's secret over
// "<timestamp>.<body>", computed here without Sign. A request replayed with another timestamp or body does not
// verify.
func TestWebhookSignatureIsHMACOverTimestampAndBody(t *testing.T) {
	expected := func(secret, ts, body string) string {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(ts + "." + body))
		return "sha256=" + hex.EncodeToString(m.Sum(nil))
	}
	if got, want := Sign("whsec_test", 1700000000, []byte(`{"id":"evt_1"}`)), expected("whsec_test", "1700000000", `{"id":"evt_1"}`); got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}

	// What a delivery carries verifies with the secret shown when the webhook was made.
	s, p, post := publishTest(t)
	ctx := t.Context()
	s.Cfg.Webhook.AllowLoopback, s.Cfg.Webhook.PollInterval = true, 5*time.Millisecond
	type request struct{ ts, sig, body string }
	got := make(chan request, 1)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- request{r.Header.Get("X-Staqly-Timestamp"), r.Header.Get("X-Staqly-Signature"), string(b)}
	}))
	t.Cleanup(recv.Close)
	wh, err := s.CreateWebhook(ctx, p, WebhookInput{URL: &recv.URL, Events: &[]string{"post.unscheduled"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	var r request
	workUntil(t, s, "the delivery", func() bool {
		select {
		case r = <-got:
			return true
		default:
			return false
		}
	})
	if wh.Secret == "" || r.sig != expected(wh.Secret, r.ts, r.body) {
		t.Fatalf("the delivery's signature %s does not verify over %q with the webhook's secret", r.sig, r.ts+"."+r.body)
	}
	if r.sig == expected(wh.Secret, "1", r.body) || r.sig == expected(wh.Secret, r.ts, r.body+" ") || r.sig == expected("other", r.ts, r.body) {
		t.Fatal("the signature also verifies with another timestamp, body or secret")
	}
}
