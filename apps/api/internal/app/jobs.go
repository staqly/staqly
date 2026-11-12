package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// The durable queue runs on River (queue.go); this file holds what the jobs do and the queue as clients see
// it. Timing rules (one place):
//   - each kind has a run budget (jobRunTimeout): a publish has 10 minutes for uploads and the platform's
//     processing, other kinds a minute or two; River cancels the run's context when it runs out;
//   - what a run writes afterwards (a requeue, a failed target) has its own short context (jobWriteTimeout);
//   - a job still running after jobRescueAfter belonged to a process that died and is put back (on its last
//     attempt River discards it, and settleRescuedJobs settles what it was about).
//
// These are variables so tests can shrink them.
var (
	jobWriteTimeout  = 30 * time.Second
	jobShutdownGrace = 30 * time.Second
	jobRunTimeout    = map[string]time.Duration{
		"publish_target":    10 * time.Minute,
		"remove_target":     2 * time.Minute,
		"run_schedule":      3 * time.Minute, // the schedule's endpoint call has its own 60 s limit
		"refresh_channel":   2 * time.Minute,
		"analyze_responses": 5 * time.Minute,  // one model call over all the responses
		"publish_reply":     10 * time.Minute, // a reply uploads its media like a post
		"ai_run":            10 * time.Minute, // a video takes a few minutes at the AI provider
	}
	jobDefaultRunTimeout = time.Minute
)

const (
	jobPoll        = time.Second
	jobRetention   = 7 * 24 * time.Hour
	jobMaxAttempts = 3
)

func runTimeout(kind string) time.Duration {
	if d, ok := jobRunTimeout[kind]; ok {
		return d
	}
	return jobDefaultRunTimeout
}

// testJobKinds lets the queue's own tests run jobs of their making (slow, failing, re-queuing ones), under
// the kind names in testJobKindNames.
var testJobKinds = map[string]func(context.Context, jobArgs) error{}

// jobBackoff: 1, 5 and 15 minutes after the first, second and third failure, with up to 20% jitter.
var jobBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

func backoffFor(attempt int64) time.Duration {
	d := jobBackoff[min(max(attempt-1, 0), int64(len(jobBackoff)-1))]
	return time.Duration(float64(d) * (1 + 0.2*rand.Float64())) // #nosec G404 -- retry jitter, not a secret
}

type jobPayload struct {
	PostID     string `json:"post_id,omitempty"`
	ChannelID  string `json:"channel_id,omitempty"`
	ScheduleID string `json:"schedule_id,omitempty"`
	Index      int    `json:"index,omitempty"`      // publish_reply: the thread part
	ParentID   string `json:"parent_id,omitempty"`  // publish_reply: the platform id it replies to
	RepostBy   string `json:"repost_by,omitempty"`  // repost: the channel that reposts
	EventID    string `json:"event_id,omitempty"`   // notify_people, notify_person: the event to tell about
	UserID     string `json:"user_id,omitempty"`    // notify_person: who gets the email
	At         string `json:"at,omitempty"`         // sync_seats: when the membership changed (the proration date)
	AIJobID    string `json:"ai_job_id,omitempty"`  // ai_run: the AI request
	AttemptID  string `json:"attempt_id,omitempty"` // target_timeout: the attempt it times out
	Welcome    string `json:"welcome,omitempty"`    // welcome_email: which one (welcomeTrial..., emailPaymentFailed...)
	Interval   string `json:"interval,omitempty"`   // welcome_email: the billing interval (paid)
}

func parsePayload(raw string) jobPayload {
	var p jobPayload
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// execJob dispatches one job to the code it stands for.
func (s *Service) execJob(ctx context.Context, kind string, a jobArgs) error {
	p := a.Payload
	switch kind {
	case "publish_post":
		return s.runPublishPost(ctx, a.WorkspaceID, p.PostID)
	case "publish_target":
		return s.publishTarget(ctx, p.PostID, p.ChannelID)
	case "remove_target":
		return s.removeTarget(ctx, p.PostID, p.ChannelID)
	case "target_timeout":
		return s.targetTimedOut(ctx, p.PostID, p.ChannelID, p.AttemptID)
	case "run_schedule":
		return s.runSchedule(ctx, p.ScheduleID)
	case "refresh_channel":
		return s.refreshChannel(ctx, p.ChannelID)
	case "repeat_post":
		return s.runRepeatPost(ctx, a.WorkspaceID, p.PostID)
	case "publish_reply":
		return s.publishReply(ctx, p.PostID, p.ChannelID, p.Index, p.ParentID)
	case "repost":
		return s.runRepost(ctx, p.PostID, p.ChannelID, p.RepostBy)
	case "notify_people":
		return s.runNotifyPeople(ctx, p.EventID)
	case "notify_person":
		return s.runNotifyPerson(ctx, p.EventID, p.UserID)
	case "sync_seats":
		return s.runSyncSeats(ctx, a.WorkspaceID, p.At)
	case "sync_entitlements":
		return s.runSyncEntitlements(ctx, a.WorkspaceID)
	case "ai_run":
		return s.runAIJob(ctx, a.WorkspaceID, p.AIJobID)
	case "read_responses":
		return s.readResponses(ctx, p.PostID, p.ChannelID)
	case "check_target":
		return s.checkTarget(ctx, p.PostID, p.ChannelID)
	case "analyze_responses":
		return s.analyzeResponses(ctx, a.WorkspaceID, p.PostID)
	case "sync_events":
		return s.syncEvents(ctx, p.ChannelID)
	case "welcome_email":
		return s.runWelcomeEmail(ctx, a.WorkspaceID, p)
	}
	if run, ok := testJobKinds[kind]; ok {
		return run(ctx, a)
	}
	return fmt.Errorf("unknown job kind %q", kind)
}

// jobFailed settles what a job that ran out of attempts was about. Its error says the settling did not happen
// (the rescue sweep tries again; a worker logs it).
func (s *Service) jobFailed(ctx context.Context, kind string, a jobArgs, cause error) error {
	p := a.Payload
	switch kind {
	case "publish_target":
		// A send whose result could not be recorded keeps its link: the target fails, to be checked by a person.
		result := providers.Result{}
		var sent *unrecordedSend
		if errors.As(cause, &sent) {
			result = sent.result
		}
		return s.finishTarget(ctx, p.PostID, p.ChannelID, "failed", result, cause.Error())
	case "remove_target":
		return s.settleRemoval(ctx, p.PostID, p.ChannelID, "failed", cause.Error())
	case "publish_post":
		return s.scheduledPublishFailed(ctx, a.WorkspaceID, p.PostID, cause)
	case "ai_run":
		s.aiRunFailed(ctx, a.WorkspaceID, p.AIJobID)
	case "run_schedule":
		return s.scheduleFailed(ctx, p.ScheduleID, cause)
	case "repeat_post":
		return s.repeatStopped(ctx, a.WorkspaceID, p.PostID, cause.Error())
	case "refresh_channel":
		// A refused sign-in already marked the channel needs_reconnect (refreshChannel); what is left is the
		// platform or the network being down (a laptop waking up, DNS failing). Reconnecting would not help:
		// the channel stays connected and the refresh is tried again later.
		s.logger(ctx).Warn("token refresh failed; trying again later", "channel", p.ChannelID, "err", cause)
		return s.enqueue(ctx, s.W, a.WorkspaceID, "refresh_channel", "refresh_channel:"+p.ChannelID,
			map[string]any{"channel_id": p.ChannelID}, s.Now().Add(refreshRetryDelay), 3)
	case "publish_reply":
		// The post is out; its thread stops at this part.
		return s.replyEnded(ctx, p.PostID, p.ChannelID, p.Index, providers.Result{}, cause.Error())
	}
	return nil
}

// scheduledPublishFailed: a scheduled post whose publishing could not start after every attempt is failed
// (post.failed, which alerts), instead of staying "scheduled" with nothing left to publish it. It can be
// edited and scheduled again.
func (s *Service) scheduledPublishFailed(ctx context.Context, workspaceID, postID string, cause error) error {
	sys := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "scheduler", Name: "Scheduler"}
	return s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPostByID(ctx, postID)
		if isNoRows(err) || (err == nil && (row.Status != "scheduled" || (row.PublishAt.Valid && row.PublishAt.String > s.now()))) {
			return nil // published by hand, unscheduled, rescheduled (its new run is queued) or deleted meanwhile
		}
		if err != nil {
			return err
		}
		if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "failed", PublishedAt: nullStr(""), UpdatedAt: s.now(), ID: postID}); err != nil {
			return err
		}
		data := s.postEventData(ctx, q, workspaceID, postID, nil)
		data["error"] = "Publishing could not start at the scheduled time: " + cause.Error()
		_, err = s.emit(ctx, q, sys, "post.failed", row.CollectionID, postID, data)
		return err
	})
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
	blocked := s.publishBlocked(ctx, workspaceID)
	if row.UnpublishedAt.Valid {
		blocked = errUnpublished // taken down since it was scheduled: it does not go out again
	}
	if blocked != nil {
		// A read-only workspace, or one whose trial is paused, publishes nothing: the post fails with why
		// (post.failed alerts).
		return s.scheduledPublishFailed(ctx, workspaceID, postID, blocked)
	}
	sys := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "scheduler", Name: "Scheduler"}
	return s.tx(ctx, func(q *db.Queries) error {
		// Again inside the write: a cancel, a Publish now or a new time may have committed since the read above.
		// A later time has its own run queued (the requeue of this key).
		row, err := q.GetPostByID(ctx, postID)
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.Status != "scheduled" || (row.PublishAt.Valid && row.PublishAt.String > s.now()) {
			return nil
		}
		return s.startPublish(ctx, q, sys, workspaceID, postID)
	})
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
	LeaseUntil  *string    `json:"lease_until"` // ponytail: always null since River (kept for clients)
	LeasedBy    string     `json:"leased_by"`   // the worker of the last attempt
	LastError   string     `json:"last_error"`
	Payload     jobPayload `json:"payload"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
	DoneAt      *string    `json:"done_at"`
}

func toJob(r *rivertype.JobRow) Job {
	var a jobArgs
	_ = json.Unmarshal(r.EncodedArgs, &a)
	j := Job{ID: jobPublicID(r.ID), Kind: r.Kind, Key: a.Key, Status: jobStatus(r.State), RunAt: formatTime(r.ScheduledAt),
		Attempts: int64(r.Attempt), MaxAttempts: int64(r.MaxAttempts), Payload: a.Payload, CreatedAt: formatTime(r.CreatedAt)}
	updated := r.CreatedAt
	if r.AttemptedAt != nil && r.AttemptedAt.After(updated) {
		updated = *r.AttemptedAt
	}
	if r.FinalizedAt != nil {
		done := formatTime(*r.FinalizedAt)
		j.DoneAt = &done
		if r.FinalizedAt.After(updated) {
			updated = *r.FinalizedAt
		}
	}
	j.UpdatedAt = formatTime(updated)
	if len(r.AttemptedBy) > 0 {
		j.LeasedBy = r.AttemptedBy[len(r.AttemptedBy)-1]
	}
	if len(r.Errors) > 0 {
		j.LastError = truncateRunes(r.Errors[len(r.Errors)-1].Error, 500)
	}
	return j
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
	params := river.NewJobListParams().Where(s.jobArgsWhere("workspace_id"), river.NamedArgs{"workspace_id": p.WorkspaceID}).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc).First(lim + 1)
	if f.Status != "" {
		params = params.States(jobStates(f.Status)...)
	}
	if f.Kind != "" {
		params = params.Kinds(f.Kind)
	}
	if f.Cursor != "" {
		var c river.JobListCursor
		if err := c.UnmarshalText([]byte(f.Cursor)); err != nil {
			return JobPage{}, invalid("cursor", "is invalid")
		}
		params = params.After(&c)
	}
	res, err := s.river.JobList(ctx, params)
	if err != nil {
		return JobPage{}, err
	}
	page := JobPage{Data: []Job{}}
	for i, r := range res.Jobs {
		if i == lim {
			if c, err := river.JobListCursorFromJob(res.Jobs[i-1]).MarshalText(); err == nil {
				page.NextCursor = string(c)
			}
			break
		}
		page.Data = append(page.Data, toJob(r))
	}
	return page, nil
}

// jobRow is one of the workspace's jobs, by its public id.
func (s *Service) jobRow(ctx context.Context, p Principal, id string) (*rivertype.JobRow, error) {
	n, ok := parseJobID(id)
	if !ok {
		return nil, notFound("job")
	}
	r, err := s.river.JobGet(ctx, n)
	if errors.Is(err, river.ErrNotFound) {
		return nil, notFound("job")
	}
	if err != nil {
		return nil, err
	}
	var a jobArgs
	if json.Unmarshal(r.EncodedArgs, &a) != nil || a.WorkspaceID != p.WorkspaceID {
		return nil, notFound("job")
	}
	return r, nil
}

func (s *Service) GetJob(ctx context.Context, p Principal, id string) (Job, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return Job{}, err
	}
	return s.getJob(ctx, p, id)
}

func (s *Service) getJob(ctx context.Context, p Principal, id string) (Job, error) {
	r, err := s.jobRow(ctx, p, id)
	if err != nil {
		return Job{}, err
	}
	return toJob(r), nil
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
	// River's table, per dialect (river_* statements are not rewritten for PostgreSQL).
	since := s.Now().Add(-24 * time.Hour).UTC()
	countQ := "SELECT kind, state, COUNT(*) FROM river_job WHERE json_extract(args, '$.workspace_id') = ? GROUP BY kind, state"
	// River's SQLite timestamps are text (RFC 3339 with our clock, "YYYY-MM-DD HH:MM:SS" from SQL defaults):
	// julianday reads both.
	doneQ := "SELECT COUNT(*) FROM river_job WHERE json_extract(args, '$.workspace_id') = ? AND state = 'completed' AND julianday(finalized_at) >= julianday(?)"
	var sinceArg any = since.Format(time.RFC3339Nano)
	if s.pg {
		countQ = "SELECT kind, state, COUNT(*) FROM river_job WHERE args->>'workspace_id' = $1 GROUP BY kind, state"
		doneQ = "SELECT COUNT(*) FROM river_job WHERE args->>'workspace_id' = $1 AND state = 'completed' AND finalized_at >= $2"
		sinceArg = since
	}
	rows, err := s.DB.QueryContext(ctx, countQ, p.WorkspaceID)
	if err != nil {
		return JobStats{}, err
	}
	defer rows.Close()
	st := JobStats{ByKind: map[string]JobKindStats{}}
	for rows.Next() {
		var kind, state string
		var n int64
		if err := rows.Scan(&kind, &state, &n); err != nil {
			return JobStats{}, err
		}
		k := st.ByKind[kind]
		switch jobStatus(rivertype.JobState(state)) {
		case "queued":
			st.Queued += n
			k.Queued += n
		case "running":
			st.Running += n
			k.Running += n
		case "failed":
			st.Failed += n
			k.Failed += n
		}
		st.ByKind[kind] = k
	}
	if err := rows.Err(); err != nil {
		return JobStats{}, err
	}
	err = s.DB.QueryRowContext(ctx, doneQ, p.WorkspaceID, sinceArg).Scan(&st.Done24h)
	return st, err
}

// JobStatusCounts: every workspace's jobs by status (metrics).
func (s *Service) JobStatusCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT state, COUNT(*) FROM river_job GROUP BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[jobStatus(rivertype.JobState(state))] += n
	}
	return out, rows.Err()
}

// RetryJob queues a failed job again, now.
func (s *Service) RetryJob(ctx context.Context, p Principal, id string) (Job, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Job{}, err
	}
	r, err := s.jobRow(ctx, p, id)
	if err != nil {
		return Job{}, err
	}
	if r.State != rivertype.JobStateDiscarded {
		return Job{}, conflict("only a failed job can be retried")
	}
	var a jobArgs
	_ = json.Unmarshal(r.EncodedArgs, &a)
	pl := a.Payload
	if r.Kind == "publish_target" {
		if err := s.checkTrialReview(ctx, p.WorkspaceID); err != nil {
			return Job{}, err
		}
	}
	// One transaction: the job comes back together with its target's state, so a worker picking it up at once
	// finds the target publishing (not failed, which it would skip and leave stuck).
	err = s.tx(ctx, func(q *db.Queries) error {
		tx := s.sqlTx(q)
		live, err := s.river.JobListTx(ctx, tx, river.NewJobListParams().Kinds(r.Kind).States(liveJobStates...).
			Where(s.jobArgsWhere("key"), river.NamedArgs{"key": a.Key}).First(1))
		if err != nil {
			return err
		}
		if len(live.Jobs) > 0 {
			return conflict("this work is queued again already (job " + jobPublicID(live.Jobs[0].ID) + ")")
		}
		switch r.Kind {
		case "publish_target":
			// The same checks and the same way back to publishing as Retry on the post's channel.
			if _, err := s.retryTargetTx(ctx, q, p.WorkspaceID, pl.PostID, pl.ChannelID); err != nil {
				return err
			}
		case "publish_post":
			// Its last failure failed the post (or someone changed it since): the run would find nothing to do.
			row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: pl.PostID})
			if isNoRows(err) {
				return notFound("post")
			}
			if err != nil {
				return err
			}
			if row.UnpublishedAt.Valid {
				return errUnpublished
			}
			if row.Status != "scheduled" {
				return conflict("this post is not scheduled any more (it is " + row.Status + "); publish it or schedule it again from the post")
			}
		}
		_, err = s.river.JobRetryTx(ctx, tx, r.ID)
		return err
	})
	if err != nil {
		return Job{}, err
	}
	return s.getJob(ctx, p, id)
}

// cancellableJobKinds: the work people start (publishing, schedules, repeats, replies, reposts). The rest is
// staqly's own bookkeeping that must run: seat and entitlement sync with Stripe (cancelling it would leave the
// subscription wrong), alerts, a custom target's timeout, a channel's token refresh.
var cancellableJobKinds = map[string]bool{"publish_post": true, "publish_target": true, "run_schedule": true,
	"repeat_post": true, "publish_reply": true, "repost": true}

// CancelJob takes a queued job out of the queue.
func (s *Service) CancelJob(ctx context.Context, p Principal, id string) (Job, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Job{}, err
	}
	r, err := s.jobRow(ctx, p, id)
	if err != nil {
		return Job{}, err
	}
	if _, test := testJobKinds[r.Kind]; !cancellableJobKinds[r.Kind] && !test {
		return Job{}, conflict("this is staqly's own work (billing, alerts, timeouts) and can't be cancelled")
	}
	if err := s.cancelQueuedJob(ctx, p, r); err != nil {
		return Job{}, err
	}
	return s.getJob(ctx, p, id)
}

// cancelQueuedJob cancels r if it is still queued and settles what it would have done. r may be a moment old: a
// worker that claimed the job since keeps it (409), because cancelling a run in progress would fail its target
// while the post goes out.
func (s *Service) cancelQueuedJob(ctx context.Context, p Principal, r *rivertype.JobRow) error {
	// The state is read again in the transaction that cancels: SQLite holds the write lock, PostgreSQL reruns
	// the transaction when a worker claimed the row meanwhile.
	err := s.tx(ctx, func(q *db.Queries) error {
		tx := s.sqlTx(q)
		cur, err := s.river.JobGetTx(ctx, tx, r.ID)
		if errors.Is(err, river.ErrNotFound) {
			return notFound("job")
		}
		if err != nil {
			return err
		}
		if jobStatus(cur.State) != "queued" {
			return conflict("only a queued job can be cancelled (this one is " + jobStatus(cur.State) + ")")
		}
		_, err = s.river.JobCancelTx(ctx, tx, r.ID)
		return err
	})
	if err != nil {
		return err
	}
	// Settle what the job would have done, or the post waits for it for good: a target fails (Retry brings it
	// back), a reply ends its thread, a scheduled post goes back to draft as if taken off the schedule, a
	// schedule skips this run and queues its next one.
	var a jobArgs
	_ = json.Unmarshal(r.EncodedArgs, &a)
	switch r.Kind {
	case "publish_target", "publish_reply":
		return s.jobFailed(ctx, r.Kind, a, errors.New("the job was cancelled"))
	case "run_schedule":
		return s.scheduleRunSkipped(ctx, a.Payload.ScheduleID)
	case "publish_post":
		var e *Error
		if _, err := s.CancelPost(ctx, p, a.Payload.PostID); err != nil && !errors.As(err, &e) {
			return err // a conflict (not scheduled any more) or a deleted post needs nothing
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
