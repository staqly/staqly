package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// queueTest opens a migrated database and two services on it (two workers, as two processes would be),
// with the queue's timings shrunk to milliseconds.
func queueTest(t *testing.T) (*Service, *Service, *sql.DB) {
	t.Helper()
	database, err := storetest.Open(t, t.TempDir()+"/q.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(database, log, Config{})
	a.Instance = "worker-a"
	b := New(database, log, Config{})
	b.Instance = "worker-b"
	oldLease, oldGrace := jobLease, jobShutdownGrace
	jobLease, jobShutdownGrace = 150*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() {
		jobLease, jobShutdownGrace = oldLease, oldGrace
		for k := range testJobKinds {
			delete(testJobKinds, k)
		}
		delete(jobRunTimeout, "test_budget")
	})
	return a, b, database
}

func enqueueTest(t *testing.T, s *Service, kind, key string) {
	t.Helper()
	if err := s.tx(context.Background(), func(q *db.Queries) error {
		return s.enqueue(context.Background(), q, "ws_test", kind, key, map[string]string{}, s.Now(), 3)
	}); err != nil {
		t.Fatal(err)
	}
}

func jobByKey(t *testing.T, s *Service, key string) db.Job {
	t.Helper()
	j, err := s.Q.GetJobByKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A slow platform holds one slot; an independent job runs meanwhile.
func TestJobsSlowJobDoesNotBlockOthers(t *testing.T) {
	a, _, _ := queueTest(t)
	release := make(chan struct{})
	var fastDone atomic.Bool
	testJobKinds["test_slow"] = func(ctx context.Context, _ db.Job) error { <-release; return nil }
	testJobKinds["test_fast"] = func(context.Context, db.Job) error { fastDone.Store(true); return nil }
	enqueueTest(t, a, "test_slow", "slow:1")
	enqueueTest(t, a, "test_fast", "fast:1")

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { a.RunJobs(ctx); close(stopped) }()
	waitFor(t, "the fast job while the slow one runs", fastDone.Load)
	if j := jobByKey(t, a, "slow:1"); j.Status != "running" {
		t.Fatalf("the slow job should still run, it is %s", j.Status)
	}
	close(release)
	waitFor(t, "the slow job to finish", func() bool { return jobByKey(t, a, "slow:1").Status == "done" })
	cancel()
	<-stopped
}

// While the run goes on the lease is renewed: another worker cannot take the job, even long after the
// first lease would have run out; the run is not cut at the lease length either.
func TestJobsLeaseRenewedWhileRunning(t *testing.T) {
	a, b, _ := queueTest(t)
	release := make(chan struct{})
	var runs atomic.Int32
	testJobKinds["test_slow"] = func(context.Context, db.Job) error { runs.Add(1); <-release; return nil }
	enqueueTest(t, a, "test_slow", "slow:1")
	pool := a.newJobPool()
	if n := a.dispatchJobs(context.Background(), pool); n != 1 {
		t.Fatalf("started %d jobs", n)
	}
	time.Sleep(4 * jobLease) // four lease lengths
	if n := b.dispatchJobs(context.Background(), b.newJobPool()); n != 0 {
		t.Fatal("a second worker took a job whose lease is being renewed")
	}
	close(release)
	pool.wg.Wait()
	if j := jobByKey(t, a, "slow:1"); j.Status != "done" || runs.Load() != 1 {
		t.Fatalf("status %s after %d runs", j.Status, runs.Load())
	}
}

// A worker that lost its lease (it stalled, the lease ran out, another worker took the job) stops and
// cannot finish the job: the new holder's outcome is the one recorded.
func TestJobsStaleWorkerCannotFinish(t *testing.T) {
	a, b, database := queueTest(t)
	stalled := make(chan struct{})
	var aCtxCause atomic.Value
	var runs atomic.Int32
	testJobKinds["test_stall"] = func(ctx context.Context, j db.Job) error {
		if runs.Add(1) == 1 {
			close(stalled)
			<-ctx.Done() // worker A: stays until its run is stopped
			aCtxCause.Store(context.Cause(ctx))
			return errors.New("worker A's late failure")
		}
		return nil // worker B: done at once
	}
	enqueueTest(t, a, "test_stall", "stall:1")
	poolA := a.newJobPool()
	a.dispatchJobs(context.Background(), poolA)
	<-stalled
	// A's lease runs out (as if its process froze past renewal), B takes the job.
	if _, err := database.Exec(`UPDATE jobs SET lease_until = '2000-01-01T00:00:00.000000Z' WHERE key = 'stall:1'`); err != nil {
		t.Fatal(err)
	}
	poolB := b.newJobPool()
	if n := b.dispatchJobs(context.Background(), poolB); n != 1 {
		t.Fatal("worker B should take over the expired job")
	}
	poolB.wg.Wait()
	poolA.wg.Wait() // A's next renewal fails, its run is cancelled, its outcome is dropped
	if c, _ := aCtxCause.Load().(error); !errors.Is(c, errLeaseLost) {
		t.Fatalf("A's run should end with the lease lost, got %v", c)
	}
	j := jobByKey(t, a, "stall:1")
	if j.Status != "done" || j.LastError != "" || j.Attempts != 2 {
		t.Fatalf("B's outcome should stand: status=%s attempts=%d err=%q", j.Status, j.Attempts, j.LastError)
	}
}

// The run's budget running out does not lose the outcome: the failure is recorded and retried.
func TestJobsBudgetExpiryIsRecorded(t *testing.T) {
	a, _, _ := queueTest(t)
	jobRunTimeout["test_budget"] = 30 * time.Millisecond
	testJobKinds["test_budget"] = func(ctx context.Context, _ db.Job) error { <-ctx.Done(); return ctx.Err() }
	enqueueTest(t, a, "test_budget", "budget:1")
	a.RunDueJobsOnce(context.Background())
	j := jobByKey(t, a, "budget:1")
	if j.Status != "queued" || j.Attempts != 1 || j.LastError == "" || j.LeaseToken != "" {
		t.Fatalf("a timed-out run should be queued for a retry: %+v", j)
	}
}

// Queueing the same key while it runs never starts a second run; the job comes back at the new time.
func TestJobsRequeueWhileRunning(t *testing.T) {
	a, b, _ := queueTest(t)
	inRun := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	testJobKinds["test_requeue"] = func(context.Context, db.Job) error {
		runs.Add(1)
		close(inRun)
		<-release
		return nil
	}
	enqueueTest(t, a, "test_requeue", "rq:1")
	pool := a.newJobPool()
	a.dispatchJobs(context.Background(), pool)
	<-inRun
	later := a.Now().Add(time.Hour)
	if err := a.tx(context.Background(), func(q *db.Queries) error {
		return a.enqueue(context.Background(), q, "ws_test", "test_requeue", "rq:1", map[string]string{}, later, 3)
	}); err != nil {
		t.Fatal(err)
	}
	if n := b.dispatchJobs(context.Background(), b.newJobPool()); n != 0 {
		t.Fatal("a second run of a running key started")
	}
	close(release)
	pool.wg.Wait()
	j := jobByKey(t, a, "rq:1")
	if j.Status != "queued" || j.RunAt != formatTime(later) || j.Attempts != 0 || runs.Load() != 1 {
		t.Fatalf("the requeued job should wait for its new time: %+v runs=%d", j, runs.Load())
	}
	// Cancelling it now drops the requeue, as for any queued job.
	if n, err := a.W.CancelJob(context.Background(), db.CancelJobParams{Now: nullStr(a.now()), Key: "rq:1"}); err != nil || n != 1 {
		t.Fatalf("cancel: %d %v", n, err)
	}
}

// Shutdown claims nothing new, waits for running jobs within the grace, and returns.
func TestJobsShutdownDrains(t *testing.T) {
	a, _, _ := queueTest(t)
	started := make(chan struct{})
	testJobKinds["test_slow"] = func(ctx context.Context, _ db.Job) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	}
	enqueueTest(t, a, "test_slow", "slow:1")
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { a.RunJobs(ctx); close(stopped) }()
	<-started
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("RunJobs did not stop")
	}
	if j := jobByKey(t, a, "slow:1"); j.Status != "done" {
		t.Fatalf("the running job should finish within the grace, it is %s", j.Status)
	}
}

// One busy workspace cannot take every slot while another one waits.
func TestJobsFairShare(t *testing.T) {
	a, _, _ := queueTest(t)
	release := make(chan struct{})
	testJobKinds["test_slow"] = func(context.Context, db.Job) error { <-release; return nil }
	for i := range 6 {
		enqueueTest(t, a, "test_slow", "busy:"+string(rune('a'+i)))
	}
	if err := a.tx(context.Background(), func(q *db.Queries) error {
		return a.enqueue(context.Background(), q, "ws_other", "test_slow", "other:1", map[string]string{}, a.Now().Add(time.Millisecond), 3)
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	pool := a.newJobPool() // 4 slots: at most 2 for one workspace in the first pass
	a.dispatchJobs(context.Background(), pool)
	if j := jobByKey(t, a, "other:1"); j.Status != "running" {
		t.Fatalf("the other workspace's job should run alongside the busy one, it is %s", j.Status)
	}
	close(release)
	pool.wg.Wait()
}
