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

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// queueTest opens a migrated database (SQLite, or PostgreSQL with STAQLY_TEST_DATABASE_URL) and a service
// on it, with the shutdown grace shrunk.
func queueTest(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	database, err := storetest.Open(t, t.TempDir()+"/q.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	oldGrace := jobShutdownGrace
	jobShutdownGrace = 200 * time.Millisecond
	t.Cleanup(func() {
		jobShutdownGrace = oldGrace
		for k := range testJobKinds {
			delete(testJobKinds, k)
		}
		delete(jobRunTimeout, "test_budget")
	})
	return s, database
}

func enqueueAt(t *testing.T, s *Service, kind, key string, at time.Time, payload map[string]any) {
	t.Helper()
	if err := s.tx(context.Background(), func(q *db.Queries) error {
		return s.enqueue(context.Background(), q, "ws_test", kind, key, payload, at, 3)
	}); err != nil {
		t.Fatal(err)
	}
}

// jobsByKey lists a key's jobs in River, newest first.
func jobsByKey(t *testing.T, s *Service, key string) []*rivertype.JobRow {
	t.Helper()
	res, err := s.river.JobList(context.Background(), river.NewJobListParams().
		Where(s.jobArgsWhere("key"), river.NamedArgs{"key": key}).OrderBy(river.JobListOrderByID, river.SortOrderDesc).First(20))
	if err != nil {
		t.Fatal(err)
	}
	return res.Jobs
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The job goes in the transaction of the change it belongs to: rolled back, there is no job.
func TestJobsInsertWithTheTransaction(t *testing.T) {
	s, _ := queueTest(t)
	ctx := context.Background()
	boom := errors.New("boom")
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := s.enqueue(ctx, q, "ws_test", "test_fast", "tx:1", map[string]any{}, s.Now(), 3); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || len(jobsByKey(t, s, "tx:1")) != 0 {
		t.Fatalf("rolled back: %v, jobs %d", err, len(jobsByKey(t, s, "tx:1")))
	}
	enqueueAt(t, s, "test_fast", "tx:1", s.Now(), nil)
	if js := jobsByKey(t, s, "tx:1"); len(js) != 1 || js[0].State != rivertype.JobStateAvailable {
		t.Fatalf("committed: %+v", js)
	}
}

// One live job per key: queueing a queued key again re-times it (the latest payload wins).
func TestJobsOneQueuedJobPerKey(t *testing.T) {
	s, _ := queueTest(t)
	later := s.Now().Add(time.Hour)
	enqueueAt(t, s, "test_fast", "one:1", later, map[string]any{"post_id": "pst_a"})
	enqueueAt(t, s, "test_fast", "one:1", later.Add(time.Hour), map[string]any{"post_id": "pst_b"})
	js := jobsByKey(t, s, "one:1")
	if len(js) != 1 || js[0].State != rivertype.JobStateScheduled || !js[0].ScheduledAt.Equal(later.Add(time.Hour).Truncate(time.Millisecond)) &&
		js[0].ScheduledAt.Sub(later.Add(time.Hour)).Abs() > time.Millisecond {
		t.Fatalf("re-timed: %+v", js)
	}
	if j := toJob(js[0]); j.Payload.PostID != "pst_b" || j.Status != "queued" || j.Key != "one:1" {
		t.Fatalf("latest payload: %+v", j)
	}
	// Due now, it runs once.
	var runs atomic.Int32
	testJobKinds["test_fast"] = func(context.Context, jobArgs) error { runs.Add(1); return nil }
	enqueueAt(t, s, "test_fast", "one:1", s.Now(), map[string]any{"post_id": "pst_c"})
	s.RunDueJobsOnce(context.Background())
	if runs.Load() != 1 || jobsByKey(t, s, "one:1")[0].State != rivertype.JobStateCompleted {
		t.Fatalf("ran %d times: %+v", runs.Load(), jobsByKey(t, s, "one:1"))
	}
}

// A key queued again while its job runs: no second run at once; it runs again after the first ends.
func TestJobsRequeueWhileRunning(t *testing.T) {
	s, _ := queueTest(t)
	ctx := context.Background()
	release := make(chan struct{})
	var runs, concurrent, maxConcurrent atomic.Int32
	testJobKinds["test_slow"] = func(context.Context, jobArgs) error {
		n := concurrent.Add(1)
		for m := maxConcurrent.Load(); n > m && !maxConcurrent.CompareAndSwap(m, n); m = maxConcurrent.Load() {
		}
		if runs.Add(1) == 1 {
			<-release
		}
		concurrent.Add(-1)
		return nil
	}
	enqueueAt(t, s, "test_slow", "rq:1", s.Now(), nil)
	done := make(chan struct{})
	go func() { s.RunDueJobsOnce(ctx); close(done) }()
	waitFor(t, "the first run", func() bool { return runs.Load() == 1 })
	enqueueAt(t, s, "test_slow", "rq:1", s.Now(), map[string]any{"post_id": "again"})
	if _, err := s.Q.GetJobRequeue(ctx, "rq:1"); err != nil {
		t.Fatalf("requeue waits for the run: %v", err)
	}
	close(release)
	<-done
	if runs.Load() != 2 || maxConcurrent.Load() != 1 {
		t.Fatalf("runs %d, at once %d", runs.Load(), maxConcurrent.Load())
	}
	if _, err := s.Q.GetJobRequeue(ctx, "rq:1"); !isNoRows(err) {
		t.Fatalf("requeue taken: %v", err)
	}

	// Cancelling while it runs drops the pending requeue only.
	release = make(chan struct{})
	runs.Store(0)
	enqueueAt(t, s, "test_slow", "rq:2", s.Now(), nil)
	done = make(chan struct{})
	go func() { s.RunDueJobsOnce(ctx); close(done) }()
	waitFor(t, "the run", func() bool { return runs.Load() == 1 })
	enqueueAt(t, s, "test_slow", "rq:2", s.Now(), nil)
	if err := s.tx(ctx, func(q *db.Queries) error { return s.cancelJobs(ctx, q, "rq:2") }); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if runs.Load() != 1 {
		t.Fatalf("cancelled requeue ran: %d", runs.Load())
	}
}

// Failures retry after 1 and 5 minutes; the last one fails the job, which the API can retry.
func TestJobsRetryBackoffThenFail(t *testing.T) {
	s, _ := queueTest(t)
	ctx := context.Background()
	base := time.Now()
	s.Now = func() time.Time { return base }
	var runs atomic.Int32
	testJobKinds["test_fail"] = func(context.Context, jobArgs) error { runs.Add(1); return errors.New("platform down") }
	enqueueAt(t, s, "test_fail", "fail:1", base, nil)
	s.RunDueJobsOnce(ctx)
	j := jobsByKey(t, s, "fail:1")[0]
	if runs.Load() != 1 || j.State != rivertype.JobStateRetryable || j.ScheduledAt.Before(base.Add(time.Minute-time.Second)) ||
		j.ScheduledAt.After(base.Add(73*time.Second)) {
		t.Fatalf("first retry about a minute later: %+v", j)
	}
	s.RunDueJobsOnce(ctx) // not due yet
	if runs.Load() != 1 {
		t.Fatalf("ran before its retry time: %d", runs.Load())
	}
	for i, ahead := range []time.Duration{2 * time.Minute, 10 * time.Minute} {
		s.Now = func() time.Time { return base.Add(ahead) }
		s.RunDueJobsOnce(ctx)
		if int(runs.Load()) != i+2 {
			t.Fatalf("attempt %d: runs %d", i+2, runs.Load())
		}
	}
	j = jobsByKey(t, s, "fail:1")[0]
	pub := toJob(j)
	if j.State != rivertype.JobStateDiscarded || pub.Status != "failed" || pub.Attempts != 3 || pub.LastError != "platform down" {
		t.Fatalf("failed after three attempts: %+v", pub)
	}
	p := Principal{WorkspaceID: "ws_test", Kind: PrincipalSystem, Scopes: AdminKeyScopes}
	page, err := s.ListJobs(ctx, p, JobFilter{Status: "failed"})
	if err != nil || len(page.Data) != 1 || page.Data[0].ID != pub.ID {
		t.Fatalf("listed failed: %+v %v", page, err)
	}
	other := Principal{WorkspaceID: "ws_other", Kind: PrincipalSystem, Scopes: AdminKeyScopes}
	if _, err := s.GetJob(ctx, other, pub.ID); err == nil {
		t.Fatal("another workspace sees the job")
	}
	again, err := s.RetryJob(ctx, p, pub.ID)
	if err != nil || again.Status != "queued" {
		t.Fatalf("retried: %+v %v", again, err)
	}
	if _, err := s.RetryJob(ctx, p, pub.ID); err == nil {
		t.Fatal("only a failed job can be retried")
	}
	if c, err := s.CancelJob(ctx, p, pub.ID); err != nil || c.Status != "cancelled" {
		t.Fatalf("cancelled: %+v %v", c, err)
	}
	st, err := s.JobStats(ctx, p)
	if err != nil || st.Failed != 0 || st.Queued != 0 {
		t.Fatalf("stats: %+v %v", st, err)
	}
}

// A run that outlives its kind's budget is cut and retried.
func TestJobsRunBudget(t *testing.T) {
	s, _ := queueTest(t)
	jobRunTimeout["test_budget"] = 50 * time.Millisecond
	testJobKinds["test_budget"] = func(ctx context.Context, _ jobArgs) error { <-ctx.Done(); return ctx.Err() }
	enqueueAt(t, s, "test_budget", "budget:1", s.Now(), nil)
	start := time.Now()
	s.RunDueJobsOnce(context.Background())
	j := jobsByKey(t, s, "budget:1")[0]
	if time.Since(start) > 5*time.Second || j.State != rivertype.JobStateRetryable || len(j.Errors) != 1 {
		t.Fatalf("cut by its budget: %+v", j)
	}
}

// The server's runner: a slow job holds one slot while another runs; shutdown waits, then cancels.
func TestJobsRunnerSlowJobDoesNotBlockOthers(t *testing.T) {
	s, _ := queueTest(t)
	release := make(chan struct{})
	var fastDone atomic.Bool
	testJobKinds["test_slow"] = func(ctx context.Context, _ jobArgs) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}
	testJobKinds["test_fast"] = func(context.Context, jobArgs) error { fastDone.Store(true); return nil }
	enqueueAt(t, s, "test_slow", "slow:1", s.Now(), nil)
	enqueueAt(t, s, "test_fast", "fast:1", s.Now(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { s.RunJobs(ctx); close(stopped) }()
	waitFor(t, "the fast job", fastDone.Load)
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("RunJobs did not stop after the grace")
	}
	close(release)
}

// The fair share: a workspace at half the slots steps aside while another workspace waits.
func TestJobsFairShare(t *testing.T) {
	s, _ := queueTest(t)
	s.Getenv = func(k string) string {
		if k == "JOB_CONCURRENCY" {
			return "2"
		}
		return ""
	}
	ctx := context.Background()
	if !s.fairStart(ctx, "ws_busy") {
		t.Fatal("first slot")
	}
	if !s.fairStart(ctx, "ws_busy") {
		t.Fatal("nobody else waits: it may take the other slot")
	}
	s.fairEnd("ws_busy")
	if err := s.tx(ctx, func(q *db.Queries) error {
		return s.enqueue(ctx, q, "ws_quiet", "test_fast", "quiet:1", map[string]any{}, s.Now(), 3)
	}); err != nil {
		t.Fatal(err)
	}
	if s.fairStart(ctx, "ws_busy") {
		t.Fatal("over its share while another workspace waits")
	}
	if !s.fairStart(ctx, "ws_quiet") {
		t.Fatal("the waiting workspace runs")
	}
}

// Jobs of the queue before River are carried over once.
func TestJobsLegacyRowsCarriedOver(t *testing.T) {
	s, database := queueTest(t)
	ctx := context.Background()
	now := s.now()
	if _, err := database.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, kind, key, payload, status, run_at, attempts, max_attempts, created_at, updated_at)
		VALUES ('job_old', 'ws_test', 'test_fast', 'old:1', '{"post_id":"pst_old"}', 'queued', ?, 0, 3, ?, ?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := s.importLegacyJobs(ctx); err != nil {
		t.Fatal(err)
	}
	js := jobsByKey(t, s, "old:1")
	if len(js) != 1 || toJob(js[0]).Payload.PostID != "pst_old" {
		t.Fatalf("carried: %+v", js)
	}
	if rows, _ := s.Q.ListLegacyJobs(ctx); len(rows) != 0 {
		t.Fatalf("old rows left: %d", len(rows))
	}
}
