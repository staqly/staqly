package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// A schedule is a collection's rule for publishing posts: once at a time or
// recurring (times of day on weekdays in a time zone, or every N minutes), to
// chosen channels, taking the next waiting post, selected posts or all of
// them; when nothing waits it can ask an agent or an endpoint for new posts.

type ScheduleRule struct {
	Times        []string `json:"times,omitempty"`    // "09:00", "17:30"
	Weekdays     []int    `json:"weekdays,omitempty"` // 0 = Sunday ... 6 = Saturday; empty = every day
	TZ           string   `json:"tz,omitempty"`       // IANA name; empty = UTC
	EveryMinutes int      `json:"every_minutes,omitempty"`
}

type Schedule struct {
	ID             string       `json:"id"`
	CollectionID   string       `json:"collection_id"`
	CollectionName string       `json:"collection_name"`
	Name           string       `json:"name"`
	Channels       []string     `json:"channels"`
	ChannelNames   []string     `json:"channel_names"`
	Kind           string       `json:"kind"`
	RunAt          *string      `json:"run_at"`
	Rule           ScheduleRule `json:"rule"`
	Pick           string       `json:"pick"`
	PostIDs        []string     `json:"post_ids"`
	Source         string       `json:"source"`
	EndpointURL    string       `json:"endpoint_url"`
	// EndpointSecret signs the endpoint request (source endpoint only). When the server
	// generated it (none given), it is shown in full once, in the response to that create
	// or update; a secret the caller chose is never echoed back. Masked everywhere else.
	EndpointSecret string  `json:"endpoint_secret,omitempty"`
	Enabled        bool    `json:"enabled"`
	NextRunAt      *string `json:"next_run_at"`
	LastRunAt      *string `json:"last_run_at"`
	LastResult     string  `json:"last_result"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type ScheduleInput struct {
	Collection     *string       `json:"collection"`
	Name           *string       `json:"name"`
	Channels       *[]string     `json:"channels"`
	Kind           *string       `json:"kind"`
	RunAt          *string       `json:"run_at"`
	Rule           *ScheduleRule `json:"rule"`
	Pick           *string       `json:"pick"`
	PostIDs        *[]string     `json:"post_ids"`
	Source         *string       `json:"source"`
	EndpointURL    *string       `json:"endpoint_url"`
	EndpointSecret *string       `json:"endpoint_secret"`
	Enabled        *bool         `json:"enabled"`
}

func (s *Service) toSchedule(ctx context.Context, r db.Schedule) Schedule {
	out := Schedule{ID: r.ID, CollectionID: r.CollectionID, Name: r.Name, Channels: []string{}, ChannelNames: []string{}, Kind: r.Kind,
		RunAt: ptr(r.RunAt), Pick: r.Pick, PostIDs: []string{}, Source: r.Source, EndpointURL: r.EndpointUrl, Enabled: r.Enabled == 1,
		NextRunAt: ptr(r.NextRunAt), LastRunAt: ptr(r.LastRunAt), LastResult: r.LastResult, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if r.Source == "endpoint" {
		out.EndpointSecret = maskEndpointSecret(r.EndpointSecret)
	}
	_ = json.Unmarshal([]byte(r.Channels), &out.Channels)
	_ = json.Unmarshal([]byte(r.Rule), &out.Rule)
	_ = json.Unmarshal([]byte(r.PostIds), &out.PostIDs)
	if c, err := s.Q.GetCollectionByID(ctx, r.CollectionID); err == nil {
		out.CollectionName = c.Name
	}
	if len(out.Channels) > 0 {
		if rows, err := s.Q.ChannelsByIDs(ctx, db.ChannelsByIDsParams{WorkspaceID: r.WorkspaceID, Ids: r.Channels}); err == nil {
			names := map[string]string{}
			for _, c := range rows {
				names[c.ID] = c.Name
			}
			for _, id := range out.Channels {
				if n, ok := names[id]; ok {
					out.ChannelNames = append(out.ChannelNames, n)
				}
			}
		}
	}
	return out
}

// maskEndpointSecret hides a schedule's endpoint secret: "****" and its last 4 characters.
func maskEndpointSecret(v string) string {
	if len(v) <= 10 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

// revealedSchedule is toSchedule with the endpoint secret in full: only for the
// response to the write that generated it (secretGenerated).
func (s *Service) revealedSchedule(ctx context.Context, r db.Schedule) Schedule {
	out := s.toSchedule(ctx, r)
	if r.Source == "endpoint" {
		out.EndpointSecret = r.EndpointSecret
	}
	return out
}

// ---- the rule ----

var errBadRule = invalid("rule", "set times (HH:MM) with optional weekdays and tz, or every_minutes (5 to 10080)")

func parseClock(v string) (int, int, bool) {
	h, m, ok := strings.Cut(v, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// validateRule normalizes a recurring rule: sorted unique times, sorted weekdays, a known time zone.
func validateRule(r *ScheduleRule) error {
	if r.EveryMinutes != 0 {
		if r.EveryMinutes < 5 || r.EveryMinutes > 10080 || len(r.Times) > 0 || len(r.Weekdays) > 0 {
			return errBadRule
		}
		r.TZ = ""
		return nil
	}
	if len(r.Times) == 0 || len(r.Times) > 24 {
		return errBadRule
	}
	times := []string{}
	for _, t := range r.Times {
		if _, _, ok := parseClock(t); !ok {
			return invalid("rule.times", t+" is not a time of day (HH:MM)")
		}
		if !slices.Contains(times, t) {
			times = append(times, t)
		}
	}
	sort.Strings(times)
	r.Times = times
	days := []int{}
	for _, d := range r.Weekdays {
		if d < 0 || d > 6 {
			return invalid("rule.weekdays", "weekdays are 0 (Sunday) to 6 (Saturday)")
		}
		if !slices.Contains(days, d) {
			days = append(days, d)
		}
	}
	sort.Ints(days)
	r.Weekdays = days
	if r.TZ == "" {
		r.TZ = "UTC"
	}
	if _, err := time.LoadLocation(r.TZ); err != nil {
		return invalid("rule.tz", "unknown time zone "+r.TZ)
	}
	return nil
}

// nextRun is the first run strictly after `after` (DST-safe: wall-clock times are resolved per day in the
// rule's zone, so 09:00 stays 09:00 across a change; every_minutes counts real minutes).
func nextRun(r ScheduleRule, after time.Time) (time.Time, bool) {
	if r.EveryMinutes > 0 {
		return after.Add(time.Duration(r.EveryMinutes) * time.Minute), true
	}
	loc, err := time.LoadLocation(firstNonEmpty(r.TZ, "UTC"))
	if err != nil || len(r.Times) == 0 {
		return time.Time{}, false
	}
	local := after.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	for d := 0; d < 8; d++ { // at most a week ahead, plus one for the day in progress
		date := day.AddDate(0, 0, d)
		if len(r.Weekdays) > 0 && !slices.Contains(r.Weekdays, int(date.Weekday())) {
			continue
		}
		for _, t := range r.Times {
			hh, mm, _ := parseClock(t)
			at := time.Date(date.Year(), date.Month(), date.Day(), hh, mm, 0, 0, loc)
			if at.After(after) {
				return at, true
			}
		}
	}
	return time.Time{}, false
}

// ---- CRUD ----

func (s *Service) applyScheduleInput(ctx context.Context, p Principal, r *db.Schedule, in ScheduleInput, creating bool) error {
	var err error
	if in.Collection != nil {
		col, err := s.findCollection(ctx, p, *in.Collection)
		if err != nil {
			return err
		}
		if col.Archived {
			return conflict("collection is archived")
		}
		r.CollectionID = col.ID
	} else if creating {
		return invalid("collection", "is required")
	}
	if in.Name != nil {
		if r.Name, err = trimmed(*in.Name, "name", 100, true); err != nil {
			return err
		}
	} else if creating {
		return invalid("name", "is required")
	}
	if in.Channels != nil {
		ids := []string{}
		for _, id := range *in.Channels {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) > maxTargets {
			return invalid("channels", fmt.Sprintf("at most %d channels", maxTargets))
		}
		if len(ids) > 0 {
			rows, err := s.Q.ChannelsByIDs(ctx, db.ChannelsByIDsParams{WorkspaceID: p.WorkspaceID, Ids: mustJSON(ids)})
			if err != nil {
				return err
			}
			if len(rows) != len(ids) {
				return invalid("channels", "unknown channel")
			}
		}
		r.Channels = mustJSON(ids)
	}
	if in.Kind != nil {
		if *in.Kind != "once" && *in.Kind != "recurring" {
			return invalid("kind", "must be once or recurring")
		}
		r.Kind = *in.Kind
	} else if creating {
		return invalid("kind", "is required")
	}
	if in.RunAt != nil {
		at, err := s.parsePublishAt(*in.RunAt)
		if err != nil {
			return invalid("run_at", strings.TrimPrefix(err.Error(), "publish_at: "))
		}
		r.RunAt = nullStr(at)
	}
	// An empty rule (a one-time schedule sent as {}) is no rule.
	if in.Rule != nil && (len(in.Rule.Times) > 0 || len(in.Rule.Weekdays) > 0 || in.Rule.EveryMinutes > 0 || in.Rule.TZ != "") {
		rule := *in.Rule
		if rule.TZ == "" && rule.EveryMinutes == 0 {
			rule.TZ = s.workspaceLocation(ctx, s.Q, p.WorkspaceID).String() // no zone of its own: the workspace's
		}
		if err := validateRule(&rule); err != nil {
			return err
		}
		r.Rule = mustJSON(rule)
	}
	if in.Pick != nil {
		if !slices.Contains([]string{"next", "selected", "all"}, *in.Pick) {
			return invalid("pick", "must be next, selected or all")
		}
		r.Pick = *in.Pick
	}
	if in.PostIDs != nil {
		if len(*in.PostIDs) > 200 {
			return invalid("post_ids", "at most 200 posts")
		}
		r.PostIds = mustJSON(*in.PostIDs)
	}
	if in.Source != nil {
		if !slices.Contains([]string{"none", "agent", "endpoint"}, *in.Source) {
			return invalid("source", "must be none, agent or endpoint")
		}
		r.Source = *in.Source
	}
	if in.EndpointURL != nil {
		if *in.EndpointURL != "" {
			if err := validateWebhookURL(*in.EndpointURL); err != nil {
				return invalid("endpoint_url", err.Error())
			}
		}
		r.EndpointUrl = *in.EndpointURL
	}
	if in.EndpointSecret != nil {
		if r.EndpointSecret, err = trimmed(*in.EndpointSecret, "endpoint_secret", 200, false); err != nil {
			return err
		}
	}
	if in.Enabled != nil {
		r.Enabled = 0
		if *in.Enabled {
			r.Enabled = 1
		}
	}
	// Whole-schedule rules.
	switch r.Kind {
	case "once":
		if !r.RunAt.Valid {
			return invalid("run_at", "a one-time schedule needs its time")
		}
	case "recurring":
		var rule ScheduleRule
		_ = json.Unmarshal([]byte(r.Rule), &rule)
		if validateRule(&rule) != nil {
			return invalid("rule", "a recurring schedule needs a rule")
		}
	}
	if r.Pick == "selected" && r.PostIds == "[]" {
		return invalid("post_ids", "choose the posts this schedule publishes")
	}
	if r.Source == "endpoint" {
		if r.EndpointUrl == "" {
			return invalid("endpoint_url", "is required when the source is an endpoint")
		}
		if r.EndpointSecret == "" {
			r.EndpointSecret = randomString(24)
		}
	}
	return nil
}

// computeNext sets next_run_at from the kind and rule (nil when disabled or spent).
func (s *Service) computeNext(r *db.Schedule) {
	if r.Enabled != 1 {
		r.NextRunAt = nullStr("")
		return
	}
	if r.Kind == "once" {
		r.NextRunAt = r.RunAt
		return
	}
	var rule ScheduleRule
	_ = json.Unmarshal([]byte(r.Rule), &rule)
	if at, ok := nextRun(rule, s.Now()); ok {
		r.NextRunAt = nullStr(formatTime(at))
	} else {
		r.NextRunAt = nullStr("")
	}
}

func (s *Service) queueScheduleJob(ctx context.Context, q *db.Queries, r db.Schedule) error {
	if !r.NextRunAt.Valid {
		err := s.cancelJobs(ctx, q, "run_schedule:"+r.ID)
		return err
	}
	at, _ := time.Parse(timeFormat, r.NextRunAt.String)
	return s.enqueue(ctx, q, r.WorkspaceID, "run_schedule", "run_schedule:"+r.ID, map[string]any{"schedule_id": r.ID}, at, 1)
}

func (s *Service) CreateSchedule(ctx context.Context, p Principal, in ScheduleInput) (Schedule, error) {
	if err := p.require(ScopeSchedulesManage); err != nil {
		return Schedule{}, err
	}
	now := s.now()
	r := db.Schedule{ID: newID("sch"), WorkspaceID: p.WorkspaceID, Channels: "[]", Rule: "{}", Pick: "next", PostIds: "[]", Source: "none",
		Enabled: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.applyScheduleInput(ctx, p, &r, in, true); err != nil {
		return Schedule{}, err
	}
	s.computeNext(&r)
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := s.useScheduleQuota(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		if err := q.CreateSchedule(ctx, db.CreateScheduleParams{ID: r.ID, WorkspaceID: r.WorkspaceID, CollectionID: r.CollectionID, Name: r.Name,
			Channels: r.Channels, Kind: r.Kind, RunAt: r.RunAt, Rule: r.Rule, Pick: r.Pick, PostIds: r.PostIds, Source: r.Source,
			EndpointUrl: r.EndpointUrl, EndpointSecret: r.EndpointSecret, Enabled: r.Enabled, NextRunAt: r.NextRunAt, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := s.queueScheduleJob(ctx, q, r); err != nil {
			return err
		}
		_, err := s.emitRef(ctx, q, p, "schedule.created", Ref{Collection: r.CollectionID, Schedule: r.ID}, map[string]any{"schedule": s.toSchedule(ctx, r)})
		return err
	})
	if err != nil {
		return Schedule{}, err
	}
	if secretGenerated(in) {
		return s.revealedSchedule(ctx, r), nil
	}
	return s.toSchedule(ctx, r), nil
}

// secretGenerated: the write left the endpoint secret to the server (none or an empty one given).
func secretGenerated(in ScheduleInput) bool {
	return in.EndpointSecret == nil || strings.TrimSpace(*in.EndpointSecret) == ""
}

func (s *Service) GetSchedule(ctx context.Context, p Principal, id string) (Schedule, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return Schedule{}, err
	}
	r, err := s.Q.GetSchedule(ctx, db.GetScheduleParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return Schedule{}, notFound("schedule")
	}
	if err != nil {
		return Schedule{}, err
	}
	return s.toSchedule(ctx, r), nil
}

func (s *Service) ListSchedules(ctx context.Context, p Principal, collectionID string) ([]Schedule, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListSchedules(ctx, db.ListSchedulesParams{WorkspaceID: p.WorkspaceID, CollectionID: optAny(collectionID)})
	if err != nil {
		return nil, err
	}
	out := make([]Schedule, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toSchedule(ctx, r))
	}
	return out, nil
}

func (s *Service) UpdateSchedule(ctx context.Context, p Principal, id string, in ScheduleInput) (Schedule, error) {
	if err := p.require(ScopeSchedulesManage); err != nil {
		return Schedule{}, err
	}
	var out db.Schedule
	secretChanged := false
	err := s.tx(ctx, func(q *db.Queries) error {
		r, err := q.GetSchedule(ctx, db.GetScheduleParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("schedule")
		}
		if err != nil {
			return err
		}
		before := r.EndpointSecret
		if err := s.applyScheduleInput(ctx, p, &r, in, false); err != nil {
			return err
		}
		r.UpdatedAt = s.now()
		s.computeNext(&r)
		if err := q.UpdateSchedule(ctx, db.UpdateScheduleParams{Name: r.Name, Channels: r.Channels, Kind: r.Kind, RunAt: r.RunAt, Rule: r.Rule,
			Pick: r.Pick, PostIds: r.PostIds, Source: r.Source, EndpointUrl: r.EndpointUrl, EndpointSecret: r.EndpointSecret, Enabled: r.Enabled,
			NextRunAt: r.NextRunAt, UpdatedAt: r.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if err := s.queueScheduleJob(ctx, q, r); err != nil {
			return err
		}
		out = r
		secretChanged = r.EndpointSecret != before
		_, err = s.emitRef(ctx, q, p, "schedule.updated", Ref{Collection: r.CollectionID, Schedule: r.ID}, map[string]any{"schedule": s.toSchedule(ctx, r)})
		return err
	})
	if err != nil {
		return Schedule{}, err
	}
	if secretChanged && secretGenerated(in) {
		return s.revealedSchedule(ctx, out), nil
	}
	return s.toSchedule(ctx, out), nil
}

func (s *Service) DeleteSchedule(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeSchedulesManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		r, err := q.GetSchedule(ctx, db.GetScheduleParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("schedule")
		}
		if err != nil {
			return err
		}
		if _, err := q.DeleteSchedule(ctx, db.DeleteScheduleParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if err := s.cancelJobs(ctx, q, "run_schedule:"+id); err != nil {
			return err
		}
		_, err = s.emitRef(ctx, q, p, "schedule.deleted", Ref{Collection: r.CollectionID, Schedule: id}, map[string]any{"schedule": s.toSchedule(ctx, r)})
		return err
	})
}

// RunScheduleNow runs a schedule by hand; its next regular run is unchanged.
func (s *Service) RunScheduleNow(ctx context.Context, p Principal, id string) (Schedule, error) {
	if err := p.require(ScopeSchedulesManage); err != nil {
		return Schedule{}, err
	}
	if _, err := s.Q.GetSchedule(ctx, db.GetScheduleParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
		if isNoRows(err) {
			return Schedule{}, notFound("schedule")
		}
		return Schedule{}, err
	}
	if err := s.runScheduleWith(ctx, id, false); err != nil {
		return Schedule{}, err
	}
	return s.GetSchedule(ctx, p, id)
}

// ---- the run ----

type incomingPost struct {
	Title    string            `json:"title"` // optional here: the first line stands in
	Content  string            `json:"content"`
	Media    []string          `json:"media"`
	Targets  []PostTargetInput `json:"targets"`
	Tags     []string          `json:"tags"`
	Metadata json.RawMessage   `json:"metadata"`
}

// runSchedule is the run_schedule job.
func (s *Service) runSchedule(ctx context.Context, id string) error {
	return s.runScheduleWith(ctx, id, true)
}

func (s *Service) runScheduleWith(ctx context.Context, id string, advance bool) error {
	r, err := s.Q.GetScheduleByID(ctx, id)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Enabled != 1 && advance {
		return nil
	}
	sys := Principal{WorkspaceID: r.WorkspaceID, Kind: PrincipalSystem, ID: "scheduler", Name: "Scheduler", Scopes: AdminKeyScopes}
	var channels, postIDs []string
	_ = json.Unmarshal([]byte(r.Channels), &channels)
	_ = json.Unmarshal([]byte(r.PostIds), &postIDs)
	// A platform turned off on this server takes no new posts: its channels drop out of this run.
	if channels, err = s.enabledChannels(ctx, r.WorkspaceID, channels); err != nil {
		return err
	}

	// 1. What waits (nothing in a read-only workspace: the run is recorded and the next one still queued, so
	// the schedule picks up again once the plan is paid).
	var waiting []db.Post
	onePerRun := false
	paused := s.suspended(ctx, r.WorkspaceID)
	switch {
	case paused:
	case r.Pick == "selected":
		var rows []db.Post
		if len(postIDs) > 0 {
			if rows, err = s.Q.PostsByIDs(ctx, db.PostsByIDsParams{WorkspaceID: r.WorkspaceID, Ids: mustJSON(uniqueIDs(postIDs))}); err != nil {
				return err
			}
		}
		for _, p := range rows {
			if p.Status == "draft" || p.Status == "scheduled" || p.Status == "failed" {
				waiting = append(waiting, p)
			}
		}
	case r.Pick == "all":
		if waiting, err = s.Q.WaitingPosts(ctx, db.WaitingPostsParams{WorkspaceID: r.WorkspaceID, CollectionID: r.CollectionID, Limit: 200}); err != nil {
			return err
		}
	default:
		// The next one that can go out: an older draft this run can't publish (no channels, refused) is passed
		// over instead of holding every later one back.
		if waiting, err = s.Q.WaitingPosts(ctx, db.WaitingPostsParams{WorkspaceID: r.WorkspaceID, CollectionID: r.CollectionID, Limit: 200}); err != nil {
			return err
		}
		onePerRun = true
	}

	// 2. Nothing waiting: ask, or fetch.
	result := ""
	var sourceErr error
	if paused {
		result = "skipped: the workspace's plan has ended"
	} else if len(waiting) == 0 {
		switch r.Source {
		case "agent":
			result = "asked for posts"
			err := s.tx(ctx, func(q *db.Queries) error {
				_, err := s.emitRef(ctx, q, sys, "schedule.posts_requested", Ref{Collection: r.CollectionID, Schedule: r.ID},
					map[string]any{"schedule": s.toSchedule(ctx, r), "channels": channels})
				return err
			})
			if err != nil {
				return err
			}
		case "endpoint":
			created, err := s.fetchPostsFromEndpoint(ctx, sys, r, channels)
			if err != nil {
				sourceErr = err
				result = "endpoint failed: " + truncateRunes(err.Error(), 200)
			} else {
				waiting, onePerRun = created, false // what the endpoint answers all goes out
			}
		default:
			result = "nothing to publish"
		}
	}

	// 3. Publish what we have.
	published := []string{}
	for _, p := range waiting {
		if onePerRun && len(published) > 0 {
			break // pick next: one waiting post per run (what an endpoint answers all goes out, as before)
		}
		nowhere := false
		started := false // set by the run that commits (on PostgreSQL fn may run again after a serialization failure)
		err := s.tx(ctx, func(q *db.Queries) error {
			started, nowhere = false, false
			cur, err := q.GetPostByID(ctx, p.ID)
			if err != nil || cur.Status == "publishing" || cur.Status == "published" {
				return err
			}
			if err := checkNoDeletedMedia(ctx, q, r.WorkspaceID, cur); err != nil {
				return err
			}
			targets, err := q.ListTargets(ctx, p.ID)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				if len(channels) == 0 {
					nowhere = true
					return nil
				}
				in := make([]PostTargetInput, 0, len(channels))
				for _, ch := range channels {
					in = append(in, PostTargetInput{ChannelID: ch})
				}
				var media []string
				_ = json.Unmarshal([]byte(cur.Media), &media)
				var tags []string
				_ = json.Unmarshal([]byte(cur.Tags), &tags)
				if _, err := s.resolveTargets(ctx, q, r.WorkspaceID, cur.Title, tags, cur.Content, media, in, nil); err != nil {
					return err
				}
				if err := s.writeTargets(ctx, q, p.ID, in, s.now()); err != nil {
					return err
				}
			}
			if err := s.cancelJobs(ctx, q, "publish_post:"+p.ID); err != nil {
				return err
			}
			if err := s.startPublish(ctx, q, sys, r.WorkspaceID, p.ID); err != nil {
				return err
			}
			started = true
			return nil
		})
		if err == nil && started {
			published = append(published, p.ID)
		}
		if nowhere && result == "" {
			result = "skipped " + p.ID + ": it has no channels and the schedule names none"
		}
		if err != nil {
			s.logger(ctx).Warn("schedule could not publish a post", "schedule_id", r.ID, "post_id", p.ID, "err", err)
			if result == "" {
				result = "could not publish " + p.ID + ": " + truncateRunes(err.Error(), 200)
			}
		}
	}
	if len(published) > 0 {
		result = fmt.Sprintf("published %d", len(published))
	}

	// 4. Record the run and the next one.
	now := s.now()
	if advance {
		if r.Kind == "once" {
			r.Enabled, r.NextRunAt = 0, nullStr("")
		} else {
			r.LastRunAt = nullStr(now)
			s.computeNext(&r)
		}
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.FinishScheduleRun(ctx, db.FinishScheduleRunParams{Enabled: r.Enabled, NextRunAt: r.NextRunAt, LastRunAt: nullStr(now),
			LastResult: result, UpdatedAt: now, ID: r.ID}); err != nil {
			return err
		}
		if advance {
			if err := s.queueScheduleJob(ctx, q, r); err != nil {
				return err
			}
		}
		typ := "schedule.ran"
		data := map[string]any{"schedule": s.toSchedule(ctx, r), "result": result, "published_post_ids": published}
		if sourceErr != nil {
			typ, data["error"] = "schedule.failed", sourceErr.Error()
		}
		_, err := s.emitRef(ctx, q, sys, typ, Ref{Collection: r.CollectionID, Schedule: r.ID}, data)
		return err
	})
	return err
}

// scheduleFailed records a run that errored out after its attempts.
func (s *Service) scheduleFailed(ctx context.Context, id string, cause error) {
	r, err := s.Q.GetScheduleByID(ctx, id)
	if err != nil {
		return
	}
	sys := Principal{WorkspaceID: r.WorkspaceID, Kind: PrincipalSystem, ID: "scheduler", Name: "Scheduler"}
	now := s.now()
	r.LastRunAt = nullStr(now)
	s.computeNext(&r)
	_ = s.tx(ctx, func(q *db.Queries) error {
		if err := q.FinishScheduleRun(ctx, db.FinishScheduleRunParams{Enabled: r.Enabled, NextRunAt: r.NextRunAt, LastRunAt: r.LastRunAt,
			LastResult: "failed: " + truncateRunes(cause.Error(), 200), UpdatedAt: now, ID: r.ID}); err != nil {
			return err
		}
		if err := s.queueScheduleJob(ctx, q, r); err != nil {
			return err
		}
		_, err := s.emitRef(ctx, q, sys, "schedule.failed", Ref{Collection: r.CollectionID, Schedule: r.ID}, map[string]any{"schedule": s.toSchedule(ctx, r), "error": cause.Error()})
		return err
	})
}

// fetchPostsFromEndpoint POSTs the slot to the schedule's endpoint (signed like a webhook) and creates the
// posts it answers with.
func (s *Service) fetchPostsFromEndpoint(ctx context.Context, sys Principal, r db.Schedule, channels []string) ([]db.Post, error) {
	body, _ := json.Marshal(map[string]any{"schedule": s.toSchedule(ctx, r), "collection_id": r.CollectionID, "channels": channels,
		"slot": s.Now().UTC().Format(time.RFC3339)})
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, r.EndpointUrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	ts := s.Now().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Staqly-Schedules/1.0")
	req.Header.Set("X-Staqly-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Staqly-Signature", Sign(r.EndpointSecret, ts, body))
	resp, err := s.safeClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("endpoint responded %d", resp.StatusCode)
	}
	var reply struct {
		Posts []incomingPost `json:"posts"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, errors.New("endpoint reply is not {posts: [...]} JSON")
	}
	if len(reply.Posts) > 50 {
		reply.Posts = reply.Posts[:50]
	}
	var out []db.Post
	for _, ip := range reply.Posts {
		in := PostInput{Collection: r.CollectionID, Title: titleOr(ip.Title, ip.Content), Content: ip.Content, Media: ip.Media, Targets: ip.Targets, Tags: ip.Tags, Metadata: ip.Metadata,
			Source: "schedule:" + r.Name}
		if len(in.Targets) == 0 {
			for _, ch := range channels {
				in.Targets = append(in.Targets, PostTargetInput{ChannelID: ch})
			}
		}
		post, err := s.CreatePost(ctx, sys, in)
		if err != nil {
			return nil, fmt.Errorf("a post from the endpoint was refused: %w", err)
		}
		row, err := s.Q.GetPostByID(ctx, post.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// enabledChannels keeps the channels that still exist and whose platform is not turned off on this server (a
// schedule keeps naming a channel that was removed; publishing to it would fail every run).
func (s *Service) enabledChannels(ctx context.Context, workspaceID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	rows, err := s.Q.ChannelsByIDs(ctx, db.ChannelsByIDsParams{WorkspaceID: workspaceID, Ids: mustJSON(ids)})
	if err != nil {
		return nil, err
	}
	on := map[string]bool{}
	for _, r := range rows {
		on[r.ID] = s.providerEnabled(r.Provider)
	}
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return !on[id] }), nil
}
