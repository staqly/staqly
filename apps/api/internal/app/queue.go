package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivertype"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// The job queue runs on River (github.com/riverqueue/river, MPL-2.0): SQLite in local mode, PostgreSQL in
// cloud mode, in the application's own database. What this file adds on top:
//
//   - one live job per key: a job's arguments carry its key (`publish_target:<post>:<channel>`), unique
//     while the job is queued or running. Queueing the key again replaces a queued job (new run time and
//     payload); a running one keeps its run and the request waits in job_requeues until the run ends;
//   - every insert happens in the transaction of the state change it belongs to (InsertTx on s.tx's
//     *sql.Tx): a rolled back write leaves no job, a committed one always has its job;
//   - our backoff (1, 5, 15 minutes), run budgets per kind and the fair share per workspace.

// jobArgs are every job's River arguments: the kind (River's job kind, not serialized), the key and the
// workspace (for listing and fairness), and the payload the job acts on.
type jobArgs struct {
	kind        string
	Key         string     `json:"key" river:"unique"`
	WorkspaceID string     `json:"workspace_id"`
	Payload     jobPayload `json:"payload"`
}

func (a jobArgs) Kind() string { return a.kind }

// jobKinds are the kinds the workers run (execJob). testJobKindNames are registered too so the queue's
// own tests can run jobs of their making (testJobKinds); a missing one fails like an unknown kind.
var (
	jobKinds         = []string{"publish_post", "publish_target", "target_timeout", "run_schedule", "refresh_channel", "repeat_post", "publish_reply", "repost", "notify_people", "notify_person", "sync_seats", "sync_entitlements"}
	testJobKindNames = []string{"test_slow", "test_fast", "test_fail", "test_budget", "test_count"}
)

// River's job states, in the four groups the API shows.
var (
	queuedJobStates = []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending,
		rivertype.JobStateRetryable, rivertype.JobStateScheduled}
	liveJobStates = append(append([]rivertype.JobState{}, queuedJobStates...), rivertype.JobStateRunning)
)

func jobStatus(st rivertype.JobState) string {
	switch st {
	case rivertype.JobStateRunning:
		return "running"
	case rivertype.JobStateCompleted:
		return "done"
	case rivertype.JobStateDiscarded:
		return "failed"
	case rivertype.JobStateCancelled:
		return "cancelled"
	}
	return "queued"
}

func jobStates(status string) []rivertype.JobState {
	switch status {
	case "running":
		return []rivertype.JobState{rivertype.JobStateRunning}
	case "done":
		return []rivertype.JobState{rivertype.JobStateCompleted}
	case "failed":
		return []rivertype.JobState{rivertype.JobStateDiscarded}
	case "cancelled":
		return []rivertype.JobState{rivertype.JobStateCancelled}
	}
	return queuedJobStates
}

// jobRescueAfter: a job still running this long after it started belonged to a process that died; River
// puts it back (our longest run budget is 10 minutes).
var jobRescueAfter = 15 * time.Minute

// jobFairSnooze: how long a job of a workspace over its fair share steps aside while others wait.
var jobFairSnooze = time.Second

// riverClock gives River the service's clock (tests move it), so run times, retries and what is due agree
// with s.Now everywhere.
type riverClock struct{ s *Service }

func (c riverClock) Now() time.Time { return riverTime(c.s.Now()) }

func (c riverClock) NowOrNil() *time.Time {
	t := riverTime(c.s.Now())
	return &t
}

// riverTime cuts a time River stores or compares to microseconds, PostgreSQL's precision: PostgreSQL
// rounds some parameters and truncates others, so a job queued "now" could land 1µs after the "now" that
// fetches it and never be due under a clock that stands still (tests).
func riverTime(t time.Time) time.Time { return t.Truncate(time.Microsecond) }

type jobRetryPolicy struct{ s *Service }

func (r jobRetryPolicy) NextRetry(j *rivertype.JobRow) time.Time {
	return riverTime(r.s.Now().Add(backoffFor(int64(j.Attempt))))
}

type jobWorker struct {
	river.WorkerDefaults[jobArgs]
	s    *Service
	kind string
}

func (w *jobWorker) Timeout(*river.Job[jobArgs]) time.Duration { return runTimeout(w.kind) }

func (w *jobWorker) Work(ctx context.Context, j *river.Job[jobArgs]) error {
	return w.s.workJob(ctx, j)
}

func (s *Service) jobConcurrency() int {
	if n, err := strconv.Atoi(s.Env("JOB_CONCURRENCY", "")); err == nil && n > 0 {
		return min(n, 64)
	}
	return 4
}

func (s *Service) riverDriver() riverdriver.Driver[*sql.Tx] {
	if s.pg {
		return riverdatabasesql.New(s.DB)
	}
	return riversqlite.New(s.DB)
}

// newRiverClient builds a client with every worker. fast: poll often (RunDueJobsOnce, which waits for
// the queue to drain); the server's client polls every jobPoll.
func (s *Service) newRiverClient(fast bool) (*river.Client[*sql.Tx], error) {
	workers := river.NewWorkers()
	for _, k := range append(append([]string{}, jobKinds...), testJobKindNames...) {
		river.AddWorkerArgs(workers, jobArgs{kind: k}, &jobWorker{s: s, kind: k})
	}
	cfg := &river.Config{
		Queues:                      map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: s.jobConcurrency()}},
		Workers:                     workers,
		RetryPolicy:                 jobRetryPolicy{s},
		MaxAttempts:                 jobMaxAttempts,
		JobTimeout:                  jobDefaultRunTimeout,
		RescueStuckJobsAfter:        jobRescueAfter,
		CompletedJobRetentionPeriod: jobRetention,
		CancelledJobRetentionPeriod: jobRetention,
		DiscardedJobRetentionPeriod: jobRetention,
		FetchPollInterval:           jobPoll,
		Logger:                      s.Log,
		Test:                        river.TestConfig{Time: riverClock{s}},
	}
	if fast {
		cfg.FetchPollInterval, cfg.FetchCooldown = 10*time.Millisecond, 2*time.Millisecond
	}
	return river.NewClient(s.riverDriver(), cfg)
}

// initQueue sets up the client every service inserts with (and the server runs: RunJobs).
func (s *Service) initQueue() {
	c, err := s.newRiverClient(false)
	if err != nil {
		panic("job queue: " + err.Error()) // a configuration mistake in this file, not a runtime condition
	}
	s.river = c
	if s.pg {
		s.completeTx = func(ctx context.Context, tx *sql.Tx, j *river.Job[jobArgs]) error {
			_, err := river.JobCompleteTx[*riverdatabasesql.Driver](ctx, tx, j)
			return err
		}
	} else {
		s.completeTx = func(ctx context.Context, tx *sql.Tx, j *river.Job[jobArgs]) error {
			_, err := river.JobCompleteTx[*riversqlite.Driver](ctx, tx, j)
			return err
		}
	}
	s.fair.perWS = map[string]int{}
}

// sqlTx is the database transaction behind q (s.tx registers it), nil outside one.
func (s *Service) sqlTx(q *db.Queries) *sql.Tx {
	if t, ok := s.txs.Load(q); ok {
		return t.(*sql.Tx)
	}
	return nil
}

// jobArgsWhere: River's job listing filtered by a field of the job's arguments, per dialect.
func (s *Service) jobArgsWhere(field string) string {
	if s.pg {
		return "args->>'" + field + "' = @" + field
	}
	return "json_extract(args, '$." + field + "') = @" + field
}

// enqueue queues the one job for a key, in q's transaction (or its own). A queued job of the key is
// replaced (the new run time and payload win, attempts start over); a running one keeps its run and the
// key runs again at runAt once it ends (job_requeues).
func (s *Service) enqueue(ctx context.Context, q *db.Queries, workspaceID, kind, key string, payload any, runAt time.Time, maxAttempts int64) error {
	tx := s.sqlTx(q)
	if tx == nil {
		return s.tx(ctx, func(q *db.Queries) error {
			return s.enqueue(ctx, q, workspaceID, kind, key, payload, runAt, maxAttempts)
		})
	}
	if maxAttempts <= 0 {
		maxAttempts = jobMaxAttempts
	}
	var pl jobPayload
	_ = json.Unmarshal([]byte(mustJSON(payload)), &pl)
	if err := s.dropQueuedJobs(ctx, tx, kind, key); err != nil {
		return err
	}
	opts := &river.InsertOpts{MaxAttempts: int(maxAttempts), UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveJobStates}}
	if runAt.After(s.Now()) {
		opts.ScheduledAt = riverTime(runAt)
	}
	res, err := s.river.InsertTx(ctx, tx, jobArgs{kind: kind, Key: key, WorkspaceID: workspaceID, Payload: pl}, opts)
	if err != nil {
		return fmt.Errorf("queue %s: %w", kind, err)
	}
	if res.UniqueSkippedAsDuplicate { // the key's job is running right now
		return q.UpsertJobRequeue(ctx, db.UpsertJobRequeueParams{Key: key, WorkspaceID: workspaceID, Kind: kind,
			Payload: mustJSON(pl), RunAt: formatTime(runAt), MaxAttempts: maxAttempts, CreatedAt: s.now()})
	}
	// The latest request wins: an older requeue of the key must not come back (the sweep) over this job.
	_, err = q.DeleteJobRequeue(ctx, key)
	return err
}

// QueueJob queues a job outside any other write (tools and tests).
func (s *Service) QueueJob(ctx context.Context, workspaceID, kind, key string, payload any, runAt time.Time) error {
	return s.enqueue(ctx, s.Q, workspaceID, kind, key, payload, runAt, 0)
}

// cancelJobs takes a key's queued job out of the queue; a running one finishes on its own (it checks the
// state it acts on first) and only loses a pending requeue.
func (s *Service) cancelJobs(ctx context.Context, q *db.Queries, key string) error {
	tx := s.sqlTx(q)
	if tx == nil {
		return s.tx(ctx, func(q *db.Queries) error { return s.cancelJobs(ctx, q, key) })
	}
	kind, _, _ := strings.Cut(key, ":")
	if err := s.dropQueuedJobs(ctx, tx, kind, key); err != nil {
		return err
	}
	_, err := q.DeleteJobRequeue(ctx, key)
	return err
}

func (s *Service) dropQueuedJobs(ctx context.Context, tx *sql.Tx, kind, key string) error {
	res, err := s.river.JobListTx(ctx, tx, river.NewJobListParams().Kinds(kind).States(queuedJobStates...).
		Where(s.jobArgsWhere("key"), river.NamedArgs{"key": key}).First(10))
	if err != nil {
		return fmt.Errorf("find queued %s: %w", key, err)
	}
	for _, j := range res.Jobs {
		if _, err := s.river.JobDeleteTx(ctx, tx, j.ID); err != nil && !errors.Is(err, river.ErrNotFound) {
			return fmt.Errorf("replace queued %s: %w", key, err)
		}
	}
	return nil
}

// fairShare counts the jobs this process runs per workspace.
type fairShare struct {
	mu    sync.Mutex
	perWS map[string]int
}

// fairStart: a workspace already running half the slots steps aside (snoozes) while another workspace's
// job waits; with nobody else waiting it may use them all. ponytail: per process, like before; River Pro
// has global concurrency limits if several servers ever need one fair share.
func (s *Service) fairStart(ctx context.Context, workspaceID string) bool {
	share := max(1, s.jobConcurrency()/2)
	s.fair.mu.Lock()
	n := s.fair.perWS[workspaceID]
	s.fair.mu.Unlock()
	if n >= share && s.othersWaiting(ctx, workspaceID) {
		return false
	}
	s.fair.mu.Lock()
	s.fair.perWS[workspaceID]++
	s.fair.mu.Unlock()
	return true
}

func (s *Service) fairEnd(workspaceID string) {
	s.fair.mu.Lock()
	if s.fair.perWS[workspaceID]--; s.fair.perWS[workspaceID] <= 0 {
		delete(s.fair.perWS, workspaceID)
	}
	s.fair.mu.Unlock()
}

func (s *Service) othersWaiting(ctx context.Context, workspaceID string) bool {
	res, err := s.river.JobList(ctx, river.NewJobListParams().States(rivertype.JobStateAvailable).First(50))
	if err != nil {
		return false
	}
	for _, j := range res.Jobs {
		var a jobArgs
		if json.Unmarshal(j.EncodedArgs, &a) == nil && a.WorkspaceID != workspaceID {
			return true
		}
	}
	return false
}

// workJob runs one job: its kind's code within its budget (River's Timeout), then a requeue that came in
// meanwhile, then what a last failed attempt settles.
func (s *Service) workJob(ctx context.Context, j *river.Job[jobArgs]) error {
	a := j.Args
	if !s.fairStart(ctx, a.WorkspaceID) {
		return river.JobSnooze(jobFairSnooze)
	}
	defer s.fairEnd(a.WorkspaceID)
	ctx = logctx.With(ctx, "workspace_id", a.WorkspaceID, "job_id", jobPublicID(j.ID), "job_kind", j.Kind)
	err := s.execJob(ctx, j.Kind, a)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err() // the run's budget ran out
	}

	// What follows is written with its own context: the run's budget running out must not lose it.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
	defer cancel()
	requeued, rerr := s.requeueAfterRun(wctx, j, err == nil)
	if rerr != nil {
		s.logger(ctx).Error("job requeue failed", "err", rerr)
	}
	switch {
	case requeued:
		metricJobs.Inc(j.Kind, "requeued")
		s.logger(ctx).Info("job ran; queued again for its new run time", "attempt", j.Attempt)
		return nil
	case err == nil:
		metricJobs.Inc(j.Kind, "done")
		s.logger(ctx).Info("job done", "attempt", j.Attempt)
		return nil
	case s.quotaWait(err) > 0:
		wait := s.quotaWait(err)
		metricJobs.Inc(j.Kind, "snoozed")
		s.logger(ctx).Warn("job waits for the platform's limit", "attempt", j.Attempt, "until", s.Now().Add(wait), "err", err)
		return river.JobSnooze(wait) // a snooze spends no attempt
	case j.Attempt >= j.MaxAttempts:
		metricJobs.Inc(j.Kind, "failed")
		s.logger(ctx).Warn("job failed", "attempt", j.Attempt, "err", err)
		s.jobFailed(wctx, j.Kind, a, err)
	default:
		metricJobs.Inc(j.Kind, "retry")
		s.logger(ctx).Warn("job attempt failed", "attempt", j.Attempt, "err", err)
	}
	return err
}

// quotaWait is how long a job waits for a platform whose quota is spent until a known time (0: retry as usual).
// ponytail: at most 25 hours; a platform asking for longer is simply asked again then.
func (s *Service) quotaWait(err error) time.Duration {
	at := providers.RetryAt(err)
	if at.IsZero() || !at.After(s.Now()) {
		return 0
	}
	return min(at.Sub(s.Now()), 25*time.Hour)
}

// requeueAfterRun queues the key again when it was asked for while this run went on: the run completes
// and the new job goes in one transaction (attempts start over, as for any fresh queueing).
//
// A run that succeeded is completed here too, in the same transaction as the check: River completes jobs
// in batches a moment after they return, and a key queued in that gap would see the job still running,
// leave a requeue and then nothing would read it. Completed here, a later enqueue finds no live job and
// queues a new one; an earlier one left its requeue for this check.
func (s *Service) requeueAfterRun(ctx context.Context, j *river.Job[jobArgs], succeeded bool) (bool, error) {
	if !succeeded {
		if _, err := s.Q.GetJobRequeue(ctx, j.Args.Key); isNoRows(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	}
	done := false
	err := s.tx(ctx, func(q *db.Queries) error {
		done = false // fn may run again on PostgreSQL: only the run that commits counts
		r, err := q.GetJobRequeue(ctx, j.Args.Key)
		if isNoRows(err) {
			if succeeded {
				return s.completeTx(ctx, s.sqlTx(q), j)
			}
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.completeTx(ctx, s.sqlTx(q), j); err != nil {
			return err
		}
		if _, err := q.DeleteJobRequeue(ctx, r.Key); err != nil {
			return err
		}
		runAt, err := time.Parse(timeFormat, r.RunAt)
		if err != nil {
			runAt = s.Now()
		}
		var pl jobPayload
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		done = true
		return s.enqueue(ctx, q, r.WorkspaceID, r.Kind, r.Key, pl, runAt, r.MaxAttempts)
	})
	return done, err
}

// sweepJobRequeues queues the keys of requeues no run picked up (a key queued again just as a last failed
// attempt ended, or a process that stopped mid-run): delete, then enqueue, so a key whose job is running
// right now simply gets its requeue back.
func (s *Service) sweepJobRequeues(ctx context.Context) int {
	rows, err := s.Q.ListStaleJobRequeues(ctx, formatTime(s.Now().Add(-time.Minute)))
	if err != nil {
		if ctx.Err() == nil {
			s.logger(ctx).Error("job requeue sweep failed", "err", err)
		}
		return 0
	}
	swept := 0
	for _, r := range rows {
		err := s.tx(ctx, func(q *db.Queries) error {
			n, err := q.DeleteJobRequeue(ctx, r.Key)
			if err != nil || n == 0 {
				return err // taken by a run meanwhile
			}
			runAt, perr := time.Parse(timeFormat, r.RunAt)
			if perr != nil {
				runAt = s.Now()
			}
			var pl jobPayload
			_ = json.Unmarshal([]byte(r.Payload), &pl)
			return s.enqueue(ctx, q, r.WorkspaceID, r.Kind, r.Key, pl, runAt, r.MaxAttempts)
		})
		if err != nil {
			s.logger(ctx).Warn("job requeue sweep: not queued", "key", r.Key, "err", err)
			continue
		}
		swept++
	}
	return swept
}

// riverRescuedError is the error River's rescuer records on a job it found still running long after it
// started (the process running it died).
const riverRescuedError = "Stuck job rescued by JobRescuer"

// errJobInterrupted is what a job discarded by the rescuer settles with.
var errJobInterrupted = errors.New("the server stopped during the last attempt (it may have gone out; check before retrying)")

// settleRescuedJobs settles what rescued jobs were about. A process that died during a job's last attempt
// leaves it running; River's rescuer then discards it without running our worker, so jobFailed never ran (a
// target publishing for good, a recurring schedule with no next run). Each is settled once (marked in the
// job's output), and only when it was discarded lately, not before this check existed.
func (s *Service) settleRescuedJobs(ctx context.Context) int {
	where := "json_extract(errors, '$[#-1].error') = @rescued"
	if s.pg {
		where = "errors[array_length(errors, 1)]->>'error' = @rescued"
	}
	res, err := s.river.JobList(ctx, river.NewJobListParams().States(rivertype.JobStateDiscarded).
		Where(where, river.NamedArgs{"rescued": riverRescuedError}).OrderBy(river.JobListOrderByID, river.SortOrderDesc).First(100))
	if err != nil {
		if ctx.Err() == nil {
			s.logger(ctx).Error("rescued jobs not read", "err", err)
		}
		return 0
	}
	since := s.Now().Add(-time.Hour)
	settled := 0
	for _, r := range res.Jobs {
		var a jobArgs
		if len(r.Output()) > 0 || r.FinalizedAt == nil || r.FinalizedAt.Before(since) || json.Unmarshal(r.EncodedArgs, &a) != nil {
			continue
		}
		jctx := logctx.With(ctx, "workspace_id", a.WorkspaceID, "job_id", jobPublicID(r.ID), "job_kind", r.Kind)
		if _, err := s.river.JobUpdate(ctx, r.ID, &river.JobUpdateParams{Output: "settled"}); err != nil {
			s.logger(jctx).Error("rescued job not marked", "err", err)
			continue
		}
		s.logger(jctx).Warn("job failed: its process stopped during the last attempt", "attempt", r.Attempt)
		s.jobFailed(jctx, r.Kind, a, errJobInterrupted)
		settled++
	}
	return settled
}

// RunJobs runs the queue until ctx ends: it first carries jobs of the queue before River over, then lets
// River work. On shutdown running jobs get jobShutdownGrace to finish, then are cancelled (River puts them
// back for the next start).
func (s *Service) RunJobs(ctx context.Context) {
	if err := s.importLegacyJobs(ctx); err != nil && ctx.Err() == nil {
		s.logger(ctx).Error("carrying queued jobs into the job queue failed", "err", err)
	}
	if err := s.river.Start(context.WithoutCancel(ctx)); err != nil {
		s.logger(ctx).Error("job queue did not start", "err", err)
		return
	}
	sweep := time.NewTicker(time.Minute)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			s.stopRiver(s.river)
			return
		case <-sweep.C:
			if n := s.sweepJobRequeues(ctx); n > 0 {
				s.logger(ctx).Info("job requeues swept", "keys", n)
			}
			s.settleRescuedJobs(ctx)
		}
	}
}

func (s *Service) stopRiver(c *river.Client[*sql.Tx]) {
	sctx, cancel := context.WithTimeout(context.Background(), jobShutdownGrace)
	defer cancel()
	if err := c.Stop(sctx); err != nil {
		hctx, hcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer hcancel()
		_ = c.StopAndCancel(hctx)
	}
}

// RunDueJobsOnce runs everything that is due by s.Now, including jobs those runs queue for now, and
// returns when nothing is due or running (tests and tools). It uses a client of its own that polls often.
func (s *Service) RunDueJobsOnce(ctx context.Context) {
	c, err := s.newRiverClient(true)
	if err != nil {
		s.logger(ctx).Error("job queue client", "err", err)
		return
	}
	if err := c.Start(context.WithoutCancel(ctx)); err != nil {
		s.logger(ctx).Error("job queue did not start", "err", err)
		return
	}
	defer s.stopRiver(c)
	for ctx.Err() == nil {
		promoted := s.promoteDueJobs(ctx, c)
		live, err := c.JobList(ctx, river.NewJobListParams().States(rivertype.JobStateAvailable, rivertype.JobStateRunning).First(1))
		if err != nil {
			s.logger(ctx).Error("job queue poll failed", "err", err)
			return
		}
		if promoted == 0 && len(live.Jobs) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// promoteDueJobs makes scheduled and retrying jobs that are due by s.Now available at once (the server's
// client leaves that to River's scheduler, which runs every few seconds).
func (s *Service) promoteDueJobs(ctx context.Context, c *river.Client[*sql.Tx]) int {
	res, err := c.JobList(ctx, river.NewJobListParams().States(rivertype.JobStateScheduled, rivertype.JobStateRetryable).
		OrderBy(river.JobListOrderByScheduledAt, river.SortOrderAsc).First(1000))
	if err != nil {
		return 0
	}
	n := 0
	for _, j := range res.Jobs {
		if j.ScheduledAt.After(s.Now()) {
			break
		}
		if _, err := c.JobRetry(ctx, j.ID); err == nil {
			n++
		}
	}
	return n
}

// importLegacyJobs carries the queued and running jobs of the queue before River (an upgrade, or rows
// `server import-sqlite` brought along) into River, once, then empties that table.
func (s *Service) importLegacyJobs(ctx context.Context) error {
	rows, err := s.Q.ListLegacyJobs(ctx)
	if err != nil || len(rows) == 0 {
		return err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		for _, r := range rows {
			runAt, err := time.Parse(timeFormat, r.RunAt)
			if err != nil || runAt.Before(s.Now()) {
				runAt = s.Now()
			}
			if err := s.enqueue(ctx, q, r.WorkspaceID, r.Kind, r.Key, parsePayload(r.Payload), runAt, r.MaxAttempts); err != nil {
				return err
			}
		}
		return q.DeleteLegacyJobs(ctx)
	})
	if err == nil {
		s.logger(ctx).Info("carried queued jobs into the job queue", "jobs", len(rows))
	}
	return err
}

// deleteWorkspaceJobs drops a deleted workspace's jobs (River's table has no foreign key to it).
func (s *Service) deleteWorkspaceJobs(ctx context.Context, q *db.Queries, workspaceID string) error {
	if err := q.DeleteWorkspaceJobRequeues(ctx, workspaceID); err != nil {
		return err
	}
	stmt := "DELETE FROM river_job WHERE json_extract(args, '$.workspace_id') = ?"
	if s.pg {
		stmt = "DELETE FROM river_job WHERE args->>'workspace_id' = $1"
	}
	_, err := s.sqlTx(q).ExecContext(ctx, stmt, workspaceID)
	return err
}

func jobPublicID(id int64) string { return "job_" + strconv.FormatInt(id, 10) }

func parseJobID(id string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "job_"), 10, 64)
	return n, err == nil && strings.HasPrefix(id, "job_")
}
