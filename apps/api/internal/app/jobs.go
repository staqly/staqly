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
//   - a job still running after jobRescueAfter belonged to a process that died and is put back.
//
// These are variables so tests can shrink them.
var (
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
	return time.Duration(float64(d) * (1 + 0.2*rand.Float64()))
}

type jobPayload struct {
	PostID     string `json:"post_id,omitempty"`
	ChannelID  string `json:"channel_id,omitempty"`
	ScheduleID string `json:"schedule_id,omitempty"`
	Index      int    `json:"index,omitempty"`     // publish_reply: the thread part
	ParentID   string `json:"parent_id,omitempty"` // publish_reply: the platform id it replies to
	RepostBy   string `json:"repost_by,omitempty"` // repost: the channel that reposts
	EventID    string `json:"event_id,omitempty"`  // notify_people, notify_person: the event to tell about
	UserID     string `json:"user_id,omitempty"`   // notify_person: who gets the email
	At         string `json:"at,omitempty"`        // sync_seats: when the membership changed (the proration date)
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
	case "target_timeout":
		return s.targetTimedOut(ctx, p.PostID, p.ChannelID)
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
	}
	if run, ok := testJobKinds[kind]; ok {
		return run(ctx, a)
	}
	return fmt.Errorf("unknown job kind %q", kind)
}

// jobFailed settles what a job that ran out of attempts was about.
func (s *Service) jobFailed(ctx context.Context, kind string, a jobArgs, cause error) {
	p := a.Payload
	switch kind {
	case "publish_target":
		if err := s.finishTarget(ctx, p.PostID, p.ChannelID, "failed", providers.Result{}, cause.Error()); err != nil {
			s.logger(ctx).Error("target not settled after failed job", "err", err)
		}
	case "publish_post":
		s.scheduledPublishFailed(ctx, a.WorkspaceID, p.PostID, cause)
	case "run_schedule":
		s.scheduleFailed(ctx, p.ScheduleID, cause)
	case "publish_reply":
		// The post is out; its thread stops at this part.
		if err := s.replyEnded(ctx, p.PostID, p.ChannelID, p.Index, providers.Result{}, cause.Error()); err != nil {
			s.logger(ctx).Error("reply not settled after failed job", "err", err)
		}
	}
}

// scheduledPublishFailed: a scheduled post whose publishing could not start after every attempt is failed
// (post.failed, which alerts), instead of staying "scheduled" with nothing left to publish it. It can be
// edited and scheduled again.
func (s *Service) scheduledPublishFailed(ctx context.Context, workspaceID, postID string, cause error) {
	sys := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "scheduler", Name: "Scheduler"}
	err := s.tx(ctx, func(q *db.Queries) error {
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
	if err != nil {
		s.logger(ctx).Error("scheduled post not failed after its job failed", "post_id", postID, "err", err)
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
		if _, err := s.river.JobRetryTx(ctx, tx, r.ID); err != nil {
			return err
		}
		// A retried publish puts its target back to publishing so the outcome lands where it belongs.
		if r.Kind != "publish_target" {
			return nil
		}
		pl := a.Payload
		t, err := q.GetTarget(ctx, db.GetTargetParams{PostID: pl.PostID, ChannelID: pl.ChannelID})
		if isNoRows(err) || (err == nil && t.Status != "failed") {
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.SetTargetStatus(ctx, db.SetTargetStatusParams{Status: "publishing", ExternalID: "", ExternalUrl: "", Error: "",
			Attempts: t.Attempts, PublishedAt: nullStr(""), UpdatedAt: s.now(), PostID: pl.PostID, ChannelID: pl.ChannelID}); err != nil {
			return err
		}
		return q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "publishing", PublishedAt: nullStr(""), UpdatedAt: s.now(), ID: pl.PostID})
	})
	if err != nil {
		return Job{}, err
	}
	return s.GetJob(ctx, p, id)
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
	if jobStatus(r.State) != "queued" {
		return Job{}, conflict("only a queued job can be cancelled")
	}
	if _, test := testJobKinds[r.Kind]; !cancellableJobKinds[r.Kind] && !test {
		return Job{}, conflict("this is staqly's own work (billing, alerts, timeouts) and can't be cancelled")
	}
	if _, err := s.river.JobCancel(ctx, r.ID); err != nil {
		return Job{}, err
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
