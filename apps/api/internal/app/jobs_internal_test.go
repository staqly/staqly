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

	"github.com/staqly/staqly/apps/api/internal/providers"

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
	// Nanoseconds past the microsecond, as Linux clocks give: PostgreSQL rounds them, and the job must still
	// be due at the time it was queued for.
	base := time.Now().Truncate(time.Microsecond).Add(900 * time.Nanosecond)
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

// A key queued again just as its run returns (River completes jobs in batches a moment later) still runs
// again: the run completes itself together with the requeue check, so the new request is never left unread.
func TestJobsQueuedAsTheRunEndsStillRuns(t *testing.T) {
	s, _ := queueTest(t)
	var runs atomic.Int32
	returned := make(chan struct{}, 4)
	testJobKinds["test_count"] = func(context.Context, jobArgs) error {
		runs.Add(1)
		returned <- struct{}{}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { s.RunJobs(ctx); close(stopped) }()
	defer func() { cancel(); <-stopped }()

	enqueueAt(t, s, "test_count", "end:1", s.Now(), nil)
	<-returned
	// Right after the run returned, before River's completer flushes (every 50 ms).
	enqueueAt(t, s, "test_count", "end:1", s.Now(), map[string]any{"post_id": "again"})
	waitFor(t, "the second run", func() bool { return runs.Load() == 2 })
	if _, err := s.Q.GetJobRequeue(context.Background(), "end:1"); !isNoRows(err) {
		t.Fatalf("no requeue may be left behind: %v", err)
	}
}

// A requeue no run picked up (a last failed attempt, a stopped process) is queued by the sweep.
func TestJobsSweepQueuesLeftoverRequeues(t *testing.T) {
	s, _ := queueTest(t)
	ctx := context.Background()
	var runs atomic.Int32
	testJobKinds["test_count"] = func(context.Context, jobArgs) error { runs.Add(1); return nil }
	if err := s.Q.UpsertJobRequeue(ctx, db.UpsertJobRequeueParams{Key: "left:1", WorkspaceID: "ws_test", Kind: "test_count",
		Payload: "{}", RunAt: formatTime(s.Now()), MaxAttempts: 3, CreatedAt: formatTime(s.Now().Add(-2 * time.Minute))}); err != nil {
		t.Fatal(err)
	}
	if n := s.sweepJobRequeues(ctx); n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	s.RunDueJobsOnce(ctx)
	if runs.Load() != 1 {
		t.Fatalf("the leftover key ran %d times, want 1", runs.Load())
	}
	if _, err := s.Q.GetJobRequeue(ctx, "left:1"); !isNoRows(err) {
		t.Fatalf("the requeue is gone once queued: %v", err)
	}
}

// A new job for a key replaces an older requeue of it: the sweep must not bring the older request back over it.
func TestJobsNewJobReplacesAnOlderRequeue(t *testing.T) {
	s, _ := queueTest(t)
	ctx := context.Background()
	if err := s.Q.UpsertJobRequeue(ctx, db.UpsertJobRequeueParams{Key: "newer:1", WorkspaceID: "ws_test", Kind: "test_fast",
		Payload: `{"post_id":"older"}`, RunAt: formatTime(s.Now()), MaxAttempts: 3, CreatedAt: formatTime(s.Now().Add(-2 * time.Minute))}); err != nil {
		t.Fatal(err)
	}
	enqueueAt(t, s, "test_fast", "newer:1", s.Now().Add(time.Hour), map[string]any{"post_id": "newer"})
	if _, err := s.Q.GetJobRequeue(ctx, "newer:1"); !isNoRows(err) {
		t.Fatalf("the older requeue is gone: %v", err)
	}
	if n := s.sweepJobRequeues(ctx); n != 0 {
		t.Fatalf("swept %d", n)
	}
}

// rescueDiscard does to a key's job what River's rescuer does when the process running its last attempt died:
// it is discarded without running again (and without our worker's last-failure handling).
func rescueDiscard(t *testing.T, s *Service, key string) {
	t.Helper()
	jobs := jobsByKey(t, s, key)
	if len(jobs) == 0 {
		t.Fatalf("no job for %s", key)
	}
	rescued := `{"at":"2026-10-24T00:00:00Z","attempt":1,"error":"` + riverRescuedError + `","trace":""}`
	errs := "[" + rescued + "]" // SQLite: one JSON array
	stmt := "UPDATE river_job SET state = 'discarded', attempt = max_attempts, attempted_at = ?, finalized_at = ?, errors = jsonb(?) WHERE id = ?"
	if s.pg { // PostgreSQL: an array of jsonb
		errs = rescued
		stmt = "UPDATE river_job SET state = 'discarded', attempt = max_attempts, attempted_at = $1, finalized_at = $2, errors = ARRAY[$3::jsonb] WHERE id = $4"
	}
	now := s.Now().UTC()
	if _, err := s.DB.ExecContext(context.Background(), stmt, now, now, errs, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
}

// The process died during a job's last attempt: River's rescuer discards the job without running our
// worker, so what the job was about is settled by the sweep (once): the target fails instead of staying
// publishing, and a recurring schedule keeps its next run.
func TestRescuedLastAttemptIsSettled(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	if err := s.tx(ctx, func(q *db.Queries) error {
		return s.startPublish(ctx, q, p, p.WorkspaceID, post.ID)
	}); err != nil {
		t.Fatal(err)
	}
	channelID := post.Targets[0].ChannelID
	rescueDiscard(t, s, "publish_target:"+post.ID+":"+channelID)

	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Hourly"), Kind: ptrTo("recurring"),
		Rule: &ScheduleRule{EveryMinutes: 60}})
	if err != nil {
		t.Fatal(err)
	}
	rescueDiscard(t, s, "run_schedule:"+sc.ID)

	if n := s.settleRescuedJobs(ctx); n != 2 {
		t.Fatalf("settled %d rescued jobs, want 2", n)
	}
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: post.ID, ChannelID: channelID})
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.Q.GetPostByID(ctx, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Status != "failed" || row.Status != "failed" {
		t.Fatalf("target %s, post %s after the rescue, want both failed", target.Status, row.Status)
	}
	live := 0
	for _, j := range jobsByKey(t, s, "run_schedule:"+sc.ID) {
		if jobStatus(j.State) == "queued" {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("%d queued runs of the recurring schedule after the rescue, want 1", live)
	}
	if n := s.settleRescuedJobs(ctx); n != 0 {
		t.Fatalf("settled %d again, want 0", n)
	}
}

// A platform's spent daily quota makes the job wait until it starts over, not burn its attempts and fail.
func TestQuotaWait(t *testing.T) {
	s := &Service{}
	now := time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	for _, c := range []struct {
		err  error
		want time.Duration
	}{
		{&providers.Failure{Msg: "daily limit", RetryAt: now.Add(90 * time.Minute)}, 90 * time.Minute},
		{&providers.Failure{Msg: "daily limit", RetryAt: now.Add(72 * time.Hour)}, 25 * time.Hour},
		{&providers.Failure{Msg: "passed", RetryAt: now.Add(-time.Minute)}, 0},
		{&providers.Failure{Kind: providers.ErrRejected, Msg: "refused", RetryAt: now.Add(time.Hour)}, 0},
		{errors.New("plain"), 0},
	} {
		if got := s.quotaWait(c.err); got != c.want {
			t.Errorf("quotaWait(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
