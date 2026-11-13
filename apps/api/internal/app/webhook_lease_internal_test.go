package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// leaseMeeting holds each worker's claim of a delivery until the other worker claims the same delivery too, so
// both have read it as due and both try to lease it: the worst interleaving two processes can reach.
type leaseMeeting struct {
	mu      sync.Mutex
	waiting map[string]chan struct{}
}

func (m *leaseMeeting) meet(ctx context.Context, id string) {
	m.mu.Lock()
	if ch, ok := m.waiting[id]; ok {
		delete(m.waiting, id)
		close(ch)
		m.mu.Unlock()

		return
	}

	ch := make(chan struct{})
	m.waiting[id] = ch
	m.mu.Unlock()
	select {
	case <-ch:
	case <-ctx.Done():
	}
}

// meetingDB is a worker's write connection with its LeaseDelivery calls held at the meeting.
type meetingDB struct {
	db.DBTX
	m *leaseMeeting
}

func (d meetingDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "name: LeaseDelivery") {
		d.m.meet(ctx, args[2].(string)) // LeaseDeliveryParams.ID
	}

	return d.DBTX.ExecContext(ctx, query, args...)
}

// webhookWorkers: two services on one database, as two server processes are (cloud mode on PostgreSQL can run
// several; local mode runs one worker, so this is the strictest case), sharing a clock the test moves, and a
// receiver that counts requests per delivery id.
type webhookWorkers struct {
	a, b    *Service
	p       Principal
	clock   atomic.Pointer[time.Time]
	mu      sync.Mutex
	sends   map[string][]int64 // delivery id → attempt numbers received
	handler atomic.Pointer[http.HandlerFunc]
}

func newWebhookWorkers(t *testing.T) *webhookWorkers {
	t.Helper()
	a, p, _ := publishTest(t)
	w := &webhookWorkers{a: a, p: p, sends: map[string][]int64{}}
	start := time.Now().UTC().Truncate(time.Second)
	w.clock.Store(&start)
	w.b = New(a.DB, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: t.TempDir()})
	for _, s := range []*Service{w.a, w.b} {
		s.Now = func() time.Time { return *w.clock.Load() }
		s.Cfg.Webhook.AllowLoopback, s.Cfg.Webhook.PollInterval = true, 5*time.Millisecond
		// Room for every delivery of a round in one poll, so both workers see the same due rows.
		s.Cfg.Webhook.Concurrency, s.Cfg.Webhook.PerWorkspace = 100, 100
	}

	recv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Attempt int64 `json:"attempt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.mu.Lock()
		id := r.Header.Get("X-Staqly-Delivery")
		w.sends[id] = append(w.sends[id], body.Attempt)
		w.mu.Unlock()
		if h := w.handler.Load(); h != nil {
			(*h)(rw, r)

			return
		}

		rw.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(recv.Close)
	if _, err := a.CreateWebhook(t.Context(), p, WebhookInput{URL: &recv.URL, Events: &[]string{"post.unscheduled"}}); err != nil {
		t.Fatal(err)
	}

	return w
}

func (w *webhookWorkers) advance(d time.Duration) {
	next := w.clock.Load().Add(d)
	w.clock.Store(&next)
}

// emit commits n events, each with its delivery, in one transaction; each a millisecond after the previous, so
// every worker lists the due deliveries in the same order.
func (w *webhookWorkers) emit(t *testing.T, n int) {
	t.Helper()
	err := w.a.tx(t.Context(), func(q *db.Queries) error {
		for i := range n {
			w.advance(time.Millisecond)
			if _, err := w.a.emit(t.Context(), q, w.p, "post.unscheduled", "", "", map[string]int{"i": i}); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (w *webhookWorkers) received() map[string][]int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string][]int64{}
	for id, a := range w.sends {
		out[id] = append([]int64{}, a...)
	}

	return out
}

func (w *webhookWorkers) statuses(t *testing.T) map[string]string {
	t.Helper()
	rows, err := w.a.DB.Query(`SELECT id, status FROM webhook_deliveries WHERE workspace_id = ?`, w.p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			t.Fatal(err)
		}
		out[id] = st
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return out
}

func (w *webhookWorkers) allSucceeded(t *testing.T, want int) func() bool {
	return func() bool {
		st := w.statuses(t)
		ok := len(st) == want
		for _, s := range st {
			ok = ok && s == "succeeded"
		}

		return ok
	}
}

// run starts s's worker; the returned stop cancels it and waits for its requests in flight.
func run(s *Service) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunWorker(ctx); close(done) }()

	return func() { cancel(); <-done }
}

// Two workers on one database read the same due deliveries and both try to claim every one of them at once:
// LeaseDelivery lets exactly one claim through (it moves next_attempt_at only from the value both read), and a
// worker whose claim changed no row does not send. Every delivery reaches the receiver once, as attempt 1.
func TestWebhookWorkersNeverSendADeliveryTwice(t *testing.T) {
	w := newWebhookWorkers(t)
	meeting := &leaseMeeting{waiting: map[string]chan struct{}{}}
	for _, s := range []*Service{w.a, w.b} {
		var write db.DBTX = gatedDB{s.DB, s.writes}
		if s.pg {
			write = s.DB
		}
		s.W = db.New(meetingDB{write, meeting})
	}
	stopA, stopB := run(w.a), run(w.b)
	defer func() { stopA(); stopB() }()

	const rounds, perRound = 5, 40
	for round := 1; round <= rounds; round++ {
		w.emit(t, perRound)
		waitFor(t, fmt.Sprintf("round %d delivered", round), w.allSucceeded(t, round*perRound))
		w.advance(time.Minute) // later rounds come later; the leases taken so far (2 minutes) still hold
	}

	got := w.received()
	if len(got) != rounds*perRound {
		t.Fatalf("the receiver got %d deliveries, want %d", len(got), rounds*perRound)
	}
	for id, attempts := range got {
		if len(attempts) != 1 || attempts[0] != 1 {
			t.Errorf("delivery %s reached the receiver as attempts %v, want once as attempt 1", id, attempts)
		}
	}
	meeting.mu.Lock()
	defer meeting.mu.Unlock()
	if len(meeting.waiting) != 0 {
		t.Errorf("%d claims were made by one worker only: the workers did not race over every delivery", len(meeting.waiting))
	}
}

// A worker whose request outlasts its lease (stalled past it: a paused process, a clock running behind) loses
// the delivery to the next worker. Webhooks are at least once, so the receiver then gets it twice, with the same
// delivery id and attempts 1 and 2. What the design promises: no more than that, and once the second send
// succeeded the stalled worker's late answer, even a failure, neither undoes it nor queues a third send.
func TestWebhookWorkerTakesOverAnExpiredLease(t *testing.T) {
	w := newWebhookWorkers(t)
	arrived, answer := make(chan struct{}), make(chan int)
	var first atomic.Bool
	stall := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if first.CompareAndSwap(false, true) {
			close(arrived)
			rw.WriteHeader(<-answer)

			return
		}

		rw.WriteHeader(http.StatusNoContent)
	})
	w.handler.Store(&stall)
	w.emit(t, 1)

	stopA := run(w.a)
	<-arrived // A claimed it and its request hangs
	if due, err := w.b.Q.DueDeliveries(t.Context(), w.b.now(), 100, 50); err != nil || len(due) != 0 {
		t.Fatalf("%d deliveries due while A's lease holds (%v), want 0", len(due), err)
	}

	w.advance(w.a.Cfg.Webhook.Lease + time.Second)
	stopB := run(w.b)
	waitFor(t, "B to deliver it", w.allSucceeded(t, 1))
	stopB()

	answer <- http.StatusInternalServerError // A's stalled request finally fails
	stopA()                                  // and A records that
	if st := w.statuses(t); len(st) != 1 || !w.allSucceeded(t, 1)() {
		t.Errorf("after the stalled worker answered, the delivery is %v, want succeeded", st)
	}
	w.advance(time.Hour) // past any retry A could have queued
	if due, err := w.b.Q.DueDeliveries(t.Context(), w.b.now(), 100, 50); err != nil || len(due) != 0 {
		t.Errorf("%d deliveries due again after the stalled worker answered (%v), want 0", len(due), err)
	}

	got := w.received()
	if len(got) != 1 {
		t.Fatalf("the receiver got %d deliveries, want 1", len(got))
	}
	for id, attempts := range got {
		if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
			t.Errorf("delivery %s reached the receiver as attempts %v, want [1 2]", id, attempts)
		}
	}
}
