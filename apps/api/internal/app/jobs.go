package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// The durable queue (docs/posts-pivot.md, "Jobs"): every piece of background
// work is a row in jobs, claimed with a lease, retried with backoff, one live
// job per subject. It survives restarts and, with the lease, a crashed worker.

// Timing rules (one place):
//   - a claim takes a short lease (jobLease) that the worker renews every jobLease/3 while the run goes
//     on, so a crashed worker's job is taken over within about jobLease, whatever the run's own budget;
//   - each kind has a run budget (jobRunTimeout): a publish has 10 minutes for uploads and the platform's
//     processing, other kinds a minute or two;
//   - the result is written with its own short context (jobWriteTimeout), never cut by the run budget;
//   - renewing and finishing need the claim's lease_token: a worker that lost its lease (another worker
//     took the job after it expired) stops the run and writes nothing.
//
// These are variables so tests can shrink them.
var (
	jobLease         = 60 * time.Second
	jobWriteTimeout  = 30 * time.Second
	jobShutdownGrace = 30 * time.Second
	jobRunTimeout    = map[string]time.Duration{
		"publish_target":  10 * time.Minute,
		"run_schedule":    3 * time.Minute, // the schedule's endpoint call has its own 60 s limit
		"refresh_channel": 2 * time.Minute,
		"publish_reply":   10 * time.Minute, // a reply uploads its media like a post
	}
	jobDefaultRunTimeout = time.Minute
)

const (
	jobPoll        = 2 * time.Second
	jobRetention   = 7 * 24 * time.Hour
	jobMaxAttempts = 3
)

func runTimeout(kind string) time.Duration {
	if d, ok := jobRunTimeout[kind]; ok {
		return d
	}
	return jobDefaultRunTimeout
}

// testJobKinds lets the queue's own tests run jobs of their making (slow, failing, re-queuing ones).
var testJobKinds = map[string]func(context.Context, db.Job) error{}

// errLeaseLost cancels a run whose lease another worker took over.
var errLeaseLost = errors.New("job lease lost to another worker")

// jobBackoff: 1, 5 and 15 minutes after the first, second and third failure, with up to 20% jitter.
var jobBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

func backoffFor(attempt int64) time.Duration {
	d := jobBackoff[min(max(attempt-1, 0), int64(len(jobBackoff)-1))]
	return time.Duration(float64(d) * (1 + 0.2*rand.Float64()))
}

// enqueue queues (or re-times) the one job for a key.
func (s *Service) enqueue(ctx context.Context, q *db.Queries, workspaceID, kind, key string, payload any, runAt time.Time, maxAttempts int64) error {
	now := s.now()
	if maxAttempts <= 0 {
		maxAttempts = jobMaxAttempts
	}
	return q.UpsertJob(ctx, db.UpsertJobParams{ID: newID("job"), WorkspaceID: workspaceID, Kind: kind, Key: key, Payload: mustJSON(payload),
		RunAt: formatTime(runAt), MaxAttempts: maxAttempts, CreatedAt: now, UpdatedAt: now})
}

// wakeJobs nudges the job worker after a commit.
func (s *Service) wakeJobs() {
	select {
	case s.jobsWake <- struct{}{}:
	default:
	}
}

type jobPayload struct {
	PostID     string `json:"post_id,omitempty"`
	ChannelID  string `json:"channel_id,omitempty"`
	ScheduleID string `json:"schedule_id,omitempty"`
	Index      int    `json:"index,omitempty"`     // publish_reply: the thread part
	ParentID   string `json:"parent_id,omitempty"` // publish_reply: the platform id it replies to
}

func parsePayload(raw string) jobPayload {
	var p jobPayload
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// jobPool runs claimed jobs on at most `size` goroutines. A job is claimed only when a slot is free, so
// nothing is reserved long before it runs; a slow platform holds one slot, not the queue.
type jobPool struct {
	size    int
	wg      sync.WaitGroup
	mu      sync.Mutex
	running int
	perWS   map[string]int  // running jobs per workspace (fair share)
	keys    map[string]bool // keys running in this process (never claim a second run of one key)
	done    chan struct{}   // a slot freed up
	stop    context.CancelFunc
	runCtx  context.Context // parent of every run: cancelled only when shutdown's grace runs out
}

func (s *Service) newJobPool() *jobPool {
	size := 4
	if n, err := strconv.Atoi(s.Env("JOB_CONCURRENCY", "")); err == nil && n > 0 {
		size = min(n, 64)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &jobPool{size: size, perWS: map[string]int{}, keys: map[string]bool{}, done: make(chan struct{}, 1), stop: cancel, runCtx: ctx}
}

func (p *jobPool) free() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size - p.running
}

func (p *jobPool) idle() bool { return p.free() == p.size }

// RunJobs is the worker loop: claim due jobs while slots are free, run them, retry or fail them. When ctx
// ends it claims nothing more, lets running jobs finish for jobShutdownGrace, then cancels them (their
// leases expire and another start of the server retries them).
func (s *Service) RunJobs(ctx context.Context) {
	pool := s.newJobPool()
	tick := time.NewTicker(jobPoll)
	defer tick.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		s.dispatchJobs(ctx, pool)
		select {
		case <-ctx.Done():
			s.drainJobs(pool)
			return
		case <-s.jobsWake:
		case <-pool.done:
		case <-tick.C:
		case <-prune.C:
			if _, err := s.W.PruneJobs(ctx, nullStr(formatTime(s.Now().Add(-jobRetention)))); err != nil && ctx.Err() == nil {
				s.logger(ctx).Warn("job prune failed", "err", err)
			}
		}
	}
}

func (s *Service) drainJobs(pool *jobPool) {
	finished := make(chan struct{})
	go func() { pool.wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(jobShutdownGrace):
		pool.stop()
		<-finished
	}
}

// RunDueJobsOnce runs everything that is due now, including jobs those runs queue for now, and returns
// when the queue has nothing due and nothing running (tests and the CLI).
func (s *Service) RunDueJobsOnce(ctx context.Context) {
	pool := s.newJobPool()
	for {
		n := s.dispatchJobs(ctx, pool)
		if n == 0 && pool.idle() {
			return
		}
		select {
		case <-pool.done:
		case <-ctx.Done():
			pool.wg.Wait()
			return
		}
	}
}

// dispatchJobs claims due jobs into free slots and starts them; it returns how many it started.
// Fairness: a first pass gives each workspace at most half the slots, so one busy workspace cannot starve
// the others; a second pass fills what is left. ponytail: fairness is per process; with several
// processes each applies it to its own slots.
func (s *Service) dispatchJobs(ctx context.Context, pool *jobPool) int {
	if ctx.Err() != nil {
		return 0
	}
	started := 0
	for pool.free() > 0 {
		due, err := s.Q.DueJobs(ctx, db.DueJobsParams{Now: s.now(), Lim: int64(pool.size * 4)})
		if err != nil {
			if ctx.Err() == nil {
				s.logger(ctx).Error("job poll failed", "err", err)
			}
			return started
		}
		share := max(1, pool.size/2)
		n := 0
		for _, fair := range []bool{true, false} {
			for _, j := range due {
				if pool.free() == 0 {
					break
				}
				pool.mu.Lock()
				skip := pool.keys[j.Key] || (fair && pool.perWS[j.WorkspaceID] >= share)
				pool.mu.Unlock()
				if skip {
					continue
				}
				if s.startJob(ctx, pool, j) {
					n++
				}
			}
		}
		started += n
		if n == 0 {
			return started
		}
	}
	return started
}

// startJob claims one job (atomically: only one worker's lease matches) and runs it in a slot.
func (s *Service) startJob(ctx context.Context, pool *jobPool, j db.Job) bool {
	token := newID("lease")
	n, err := s.W.LeaseJob(ctx, db.LeaseJobParams{LeaseUntil: nullStr(formatTime(s.Now().Add(jobLease))), LeasedBy: s.Instance,
		LeaseToken: token, Now: s.now(), ID: j.ID})
	if err != nil || n == 0 {
		return false // another worker took it, or it was cancelled or re-timed meanwhile
	}
	j.Attempts++
	pool.mu.Lock()
	pool.running++
	pool.perWS[j.WorkspaceID]++
	pool.keys[j.Key] = true
	pool.mu.Unlock()
	pool.wg.Add(1)
	go func() {
		defer func() {
			pool.mu.Lock()
			pool.running--
			if pool.perWS[j.WorkspaceID]--; pool.perWS[j.WorkspaceID] <= 0 {
				delete(pool.perWS, j.WorkspaceID)
			}
			delete(pool.keys, j.Key)
			pool.mu.Unlock()
			pool.wg.Done()
			select {
			case pool.done <- struct{}{}:
			default:
			}
		}()
		s.runJob(pool.runCtx, j, token)
	}()
	return true
}

// runJob runs one claimed job within its kind's budget, renewing the lease meanwhile, and records the
// outcome with the claim's token.
func (s *Service) runJob(parent context.Context, j db.Job, token string) {
	ctx := logctx.With(parent, "workspace_id", j.WorkspaceID, "job_id", j.ID, "job_kind", j.Kind)
	jctx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	jctx, cancelBudget := context.WithTimeout(jctx, runTimeout(j.Kind))
	defer cancelBudget()

	// Heartbeat: extend the lease while the run goes on; losing it stops the run.
	stopBeat := make(chan struct{})
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		t := time.NewTicker(jobLease / 3)
		defer t.Stop()
		for {
			select {
			case <-stopBeat:
				return
			case <-t.C:
				wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
				n, err := s.W.RenewJobLease(wctx, db.RenewJobLeaseParams{LeaseUntil: nullStr(formatTime(s.Now().Add(jobLease))),
					Now: s.now(), ID: j.ID, LeaseToken: token})
				cancel()
				if err == nil && n == 0 {
					cancelRun(errLeaseLost)
					return
				}
				if err != nil {
					s.logger(ctx).Warn("job lease renewal failed", "err", err)
				}
			}
		}
	}()

	err := s.execJob(jctx, j)
	close(stopBeat)
	<-beatDone
	if errors.Is(context.Cause(jctx), errLeaseLost) {
		s.logger(ctx).Warn("job lease lost; another worker owns the job now", "attempt", j.Attempts)
		return
	}
	if err == nil && jctx.Err() == context.DeadlineExceeded {
		err = jctx.Err()
	}

	// The outcome is written with its own context: the run's budget running out must not lose it.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
	defer cancel()
	now := s.now()
	fin := db.FinishJobParams{ID: j.ID, Now: now, RunAt: j.RunAt, LeaseToken: token}
	switch {
	case err == nil:
		fin.Status, fin.DoneAt = "done", nullStr(now)
	case j.Attempts >= j.MaxAttempts:
		fin.Status, fin.DoneAt, fin.LastError = "failed", nullStr(now), truncateRunes(err.Error(), 500)
	default:
		fin.Status, fin.LastError = "queued", truncateRunes(err.Error(), 500)
		fin.RunAt = formatTime(s.Now().Add(backoffFor(j.Attempts)))
	}
	ended, ferr := s.W.FinishJob(wctx, fin)
	switch {
	case isNoRows(ferr):
		s.logger(ctx).Warn("job finished after its lease was lost; the new holder records the outcome", "attempt", j.Attempts)
		return
	case ferr != nil:
		s.logger(ctx).Error("job state update failed", "err", ferr)
		return
	}
	if ended == "failed" {
		s.jobFailed(wctx, j, err)
	}
	result := map[string]string{"done": "done", "queued": "retry", "failed": "failed"}[ended]
	metricJobs.Inc(j.Kind, result)
	if err != nil {
		s.logger(ctx).Warn("job attempt failed", "attempt", j.Attempts, "status", ended, "err", err)
	} else {
		s.logger(ctx).Info("job done", "attempt", j.Attempts, "status", ended)
	}
}

// execJob dispatches one job to the code it stands for.
func (s *Service) execJob(ctx context.Context, j db.Job) error {
	p := parsePayload(j.Payload)
	switch j.Kind {
	case "publish_post":
		return s.runPublishPost(ctx, j.WorkspaceID, p.PostID)
	case "publish_target":
		return s.publishTarget(ctx, p.PostID, p.ChannelID)
	case "target_timeout":
		return s.targetTimedOut(ctx, p.PostID, p.ChannelID)
	case "run_schedule":
		return s.runSchedule(ctx, p.ScheduleID)
	case "refresh_channel":
		return s.refreshChannel(ctx, p.ChannelID)
	case "repeat_post":
		return s.runRepeatPost(ctx, j.WorkspaceID, p.PostID)
	case "publish_reply":
		return s.publishReply(ctx, p.PostID, p.ChannelID, p.Index, p.ParentID)
	}
	if run, ok := testJobKinds[j.Kind]; ok {
		return run(ctx, j)
	}
	return fmt.Errorf("unknown job kind %q", j.Kind)
}

// jobFailed settles what a job that ran out of attempts was about.
func (s *Service) jobFailed(ctx context.Context, j db.Job, cause error) {
	p := parsePayload(j.Payload)
	switch j.Kind {
	case "publish_target":
		if err := s.finishTarget(ctx, p.PostID, p.ChannelID, "failed", providers.Result{}, cause.Error()); err != nil {
			s.logger(ctx).Error("target not settled after failed job", "err", err)
		}
	case "run_schedule":
		s.scheduleFailed(ctx, p.ScheduleID, cause)
	case "publish_reply":
		// The post is out; its thread stops at this part.
		if err := s.replyEnded(ctx, p.PostID, p.ChannelID, p.Index, providers.Result{}, cause.Error()); err != nil {
			s.logger(ctx).Error("reply not settled after failed job", "err", err)
		}
	}
}

// runPublishPost is the publish_post job: the scheduled time came.
func (s *Service) runPublishPost(ctx context.Context, workspaceID, postID string) error {
	row, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != "scheduled" {
		return nil // unscheduled or published by hand meanwhile
	}
	sys := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "scheduler", Name: "Scheduler"}
	return s.tx(ctx, func(q *db.Queries) error { return s.startPublish(ctx, q, sys, workspaceID, postID) })
}

// ---- the queue as clients see it ----

type Job struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Key         string     `json:"key"`
	Status      string     `json:"status"`
	RunAt       string     `json:"run_at"`
	Attempts    int64      `json:"attempts"`
	MaxAttempts int64      `json:"max_attempts"`
	LeaseUntil  *string    `json:"lease_until"`
	LeasedBy    string     `json:"leased_by"`
	LastError   string     `json:"last_error"`
	Payload     jobPayload `json:"payload"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
	DoneAt      *string    `json:"done_at"`
}

func toJob(j db.Job) Job {
	return Job{ID: j.ID, Kind: j.Kind, Key: j.Key, Status: j.Status, RunAt: j.RunAt, Attempts: j.Attempts, MaxAttempts: j.MaxAttempts,
		LeaseUntil: ptr(j.LeaseUntil), LeasedBy: j.LeasedBy, LastError: j.LastError, Payload: parsePayload(j.Payload),
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, DoneAt: ptr(j.DoneAt)}
}

type JobFilter struct {
	Status, Kind, Cursor string
	Limit                int
}

type JobPage struct {
	Data       []Job  `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

var jobStatuses = []string{"queued", "running", "done", "failed", "cancelled"}

func (s *Service) ListJobs(ctx context.Context, p Principal, f JobFilter) (JobPage, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return JobPage{}, err
	}
	if f.Status != "" && !contains(jobStatuses, f.Status) {
		return JobPage{}, invalid("status", "must be one of "+strings.Join(jobStatuses, ", "))
	}
	lim := clampLimit(f.Limit, 50, 200)
	params := db.ListJobsParams{WorkspaceID: p.WorkspaceID, Status: optAny(f.Status), Kind: optAny(f.Kind), Lim: int64(lim + 1)}
	if f.Cursor != "" {
		created, id, ok := strings.Cut(f.Cursor, "|")
		if !ok {
			return JobPage{}, invalid("cursor", "is invalid")
		}
		params.BeforeCreated, params.BeforeID = nullStr(created), nullStr(id)
	}
	rows, err := s.Q.ListJobs(ctx, params)
	if err != nil {
		return JobPage{}, err
	}
	page := JobPage{Data: []Job{}}
	for i, r := range rows {
		if i == lim {
			page.NextCursor = rows[i-1].CreatedAt + "|" + rows[i-1].ID
			break
		}
		page.Data = append(page.Data, toJob(r))
	}
	return page, nil
}

func (s *Service) GetJob(ctx context.Context, p Principal, id string) (Job, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return Job{}, err
	}
	j, err := s.Q.GetJob(ctx, db.GetJobParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return Job{}, notFound("job")
	}
	return toJob(j), err
}

type JobKindStats struct {
	Queued  int64 `json:"queued"`
	Running int64 `json:"running"`
	Failed  int64 `json:"failed"`
}

type JobStats struct {
	Queued  int64                   `json:"queued"`
	Running int64                   `json:"running"`
	Failed  int64                   `json:"failed"`
	Done24h int64                   `json:"done_24h"`
	ByKind  map[string]JobKindStats `json:"by_kind"`
}

func (s *Service) JobStats(ctx context.Context, p Principal) (JobStats, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return JobStats{}, err
	}
	rows, err := s.Q.JobCountsByKind(ctx, p.WorkspaceID)
	if err != nil {
		return JobStats{}, err
	}
	st := JobStats{ByKind: map[string]JobKindStats{}}
	for _, r := range rows {
		k := st.ByKind[r.Kind]
		switch r.Status {
		case "queued":
			st.Queued += r.N
			k.Queued += r.N
		case "running":
			st.Running += r.N
			k.Running += r.N
		case "failed":
			st.Failed += r.N
			k.Failed += r.N
		}
		st.ByKind[r.Kind] = k
	}
	st.Done24h, err = s.Q.JobsDoneSince(ctx, db.JobsDoneSinceParams{WorkspaceID: p.WorkspaceID, DoneAt: nullStr(formatTime(s.Now().Add(-24 * time.Hour)))})
	return st, err
}

// RetryJob queues a failed job again, now (its attempts count stays).
func (s *Service) RetryJob(ctx context.Context, p Principal, id string) (Job, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Job{}, err
	}
	j, err := s.Q.GetJob(ctx, db.GetJobParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return Job{}, notFound("job")
	}
	if err != nil {
		return Job{}, err
	}
	if j.Status != "failed" {
		return Job{}, conflict("only a failed job can be retried")
	}
	if _, err := s.W.RetryJob(ctx, db.RetryJobParams{Now: s.now(), WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
		return Job{}, err
	}
	// A retried publish puts its target back to publishing so the outcome lands where it belongs.
	if j.Kind == "publish_target" {
		pl := parsePayload(j.Payload)
		if t, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: pl.PostID, ChannelID: pl.ChannelID}); err == nil && t.Status == "failed" {
			_ = s.W.SetTargetStatus(ctx, db.SetTargetStatusParams{Status: "publishing", ExternalID: "", ExternalUrl: "", Error: "",
				Attempts: t.Attempts, PublishedAt: nullStr(""), UpdatedAt: s.now(), PostID: pl.PostID, ChannelID: pl.ChannelID})
			_ = s.W.SetPostStatus(ctx, db.SetPostStatusParams{Status: "publishing", PublishedAt: nullStr(""), UpdatedAt: s.now(), ID: pl.PostID})
		}
	}
	s.wakeJobs()
	return s.GetJob(ctx, p, id)
}

// CancelJob takes a queued job out of the queue.
func (s *Service) CancelJob(ctx context.Context, p Principal, id string) (Job, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Job{}, err
	}
	n, err := s.W.CancelJobByID(ctx, db.CancelJobByIDParams{Now: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return Job{}, err
	}
	if n == 0 {
		if _, err := s.Q.GetJob(ctx, db.GetJobParams{WorkspaceID: p.WorkspaceID, ID: id}); isNoRows(err) {
			return Job{}, notFound("job")
		}
		return Job{}, conflict("only a queued job can be cancelled")
	}
	return s.GetJob(ctx, p, id)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
