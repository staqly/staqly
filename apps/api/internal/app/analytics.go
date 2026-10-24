package app

import (
	"cmp"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Product analytics: a small, fixed set of events sent to the operator's own PostHog project (POSTHOG_KEY;
// staqly cloud sets it, an install sends nothing unless its operator does). Events say what happened and on
// which platform, never what was written: no post text, names, addresses, URLs or credentials. A person is
// their user id (an opaque usr_...), work the system does for a workspace (publishing, Stripe) is a personless
// event of that workspace, and every event carries workspace_id. Docs: self-hosting/analytics.

// PostHogConfig: POSTHOG_KEY (the project token, public by design: it can only send events), POSTHOG_HOST,
// POSTHOG_ENVIRONMENT (production by default; development and test events are filtered out of the dashboards).
type PostHogConfig struct {
	Key, Host, Environment string
}

// analyticsEvents maps the domain events (events.go) that answer product questions onto PostHog event
// names; every other event type stays out of PostHog.
var analyticsEvents = map[string]string{
	"session.signed_in":       "user_signed_in",
	"workspace.created":       "workspace_created",
	"invitation.created":      "member_invited",
	"invitation.accepted":     "member_joined",
	"channel.connected":       "channel_connected",
	"channel.disconnected":    "channel_disconnected",
	"channel.needs_reconnect": "channel_needs_reconnect",
	"post.created":            "post_created",
	"post.updated":            "post_updated",
	"post.scheduled":          "post_scheduled",
	"post.target_published":   "post_published",
	"post.target_failed":      "post_publish_failed",
	"schedule.created":        "schedule_created",
	"schedule.failed":         "schedule_failed",
	"api_key.created":         "api_key_created",
	"webhook.created":         "webhook_created",
	"alert.created":           "alert_created",
	"workspace.plan_changed":  "plan_changed",
	"workspace.credits_added": "credits_purchased",
}

// track queues c to be sent when q's transaction commits (dropped if it rolls back or is rerun); with q
// outside a transaction it is sent now. Sending only queues in memory: the client batches in the background.
func (s *Service) track(q *db.Queries, c posthog.Capture) {
	if s.Cfg.Analytics == nil {
		return
	}
	if _, inTx := s.txs.Load(q); !inTx {
		_ = s.Cfg.Analytics.Enqueue(c)
		return
	}
	v, _ := s.tracked.LoadOrStore(q, &[]posthog.Capture{})
	l := v.(*[]posthog.Capture)
	*l = append(*l, c)
}

func (s *Service) sendTracked(q *db.Queries) {
	v, ok := s.tracked.LoadAndDelete(q)
	if !ok {
		return
	}
	for _, c := range *v.(*[]posthog.Capture) {
		_ = s.Cfg.Analytics.Enqueue(c)
	}
}

// capture builds an event of p in p's workspace: a person's when p is one, else personless.
func (s *Service) capture(p Principal, event string, props posthog.Properties) posthog.Capture {
	if props == nil {
		props = posthog.NewProperties()
	}
	props.Set("workspace_id", p.WorkspaceID).Set("actor", p.Kind)
	distinct := p.UserID
	if distinct == "" {
		distinct = "workspace:" + p.WorkspaceID
		props.Set("$process_person_profile", false)
	}
	return posthog.Capture{DistinctId: distinct, Event: event, Timestamp: s.Now(), Properties: props}
}

// trackAs tracks event of p (see track) with properties given as key, value pairs.
func (s *Service) trackAs(q *db.Queries, p Principal, event string, kv ...any) {
	if s.Cfg.Analytics == nil {
		return
	}
	props := posthog.NewProperties()
	for i := 0; i+1 < len(kv); i += 2 {
		props.Set(kv[i].(string), kv[i+1])
	}
	s.track(q, s.capture(p, event, props))
}

// trackSignInFailed counts a sign-in that did not go through, by method and error code (no address, no
// person: personless).
func (s *Service) trackSignInFailed(method string, err error) {
	if err == nil || s.Cfg.Analytics == nil {
		return
	}
	code := "error"
	if e, ok := err.(*Error); ok {
		code = e.Code
	}
	_ = s.Cfg.Analytics.Enqueue(posthog.Capture{DistinctId: "sign-in", Event: "sign_in_failed", Timestamp: s.Now(),
		Properties: posthog.NewProperties().Set("method", method).Set("code", code).Set("$process_person_profile", false)})
}

// trackEvent sends the PostHog counterpart of a recorded domain event, if it has one (emitRef).
func (s *Service) trackEvent(q *db.Queries, p Principal, ev db.Event) {
	name, ok := analyticsEvents[ev.Type]
	if !ok || s.Cfg.Analytics == nil {
		return
	}
	var d struct {
		Post    *Post `json:"post"`
		Channel *struct {
			Provider string `json:"provider"`
		} `json:"channel"`
		ChannelID   string `json:"channel_id"`
		Error       string `json:"error"`
		Via         string `json:"via"`
		Role        string `json:"role"`
		Reconnected bool   `json:"reconnected"`
		From        string `json:"from"`
		To          string `json:"to"`
		Reason      string `json:"reason"`
		Posts       int64  `json:"posts"`
	}
	_ = json.Unmarshal([]byte(ev.Data), &d)
	props := posthog.NewProperties()
	if ev.Agent != "" {
		props.Set("agent", ev.Agent) // the MCP or REST client that acted (Claude Code, n8n...)
	}
	switch ev.Type {
	case "session.signed_in":
		props.Set("method", cmp.Or(d.Via, "api_key"))
	case "invitation.created", "invitation.accepted":
		props.Set("role", d.Role)
	case "channel.connected", "channel.disconnected", "channel.needs_reconnect":
		if d.Channel != nil {
			props.Set("platform", d.Channel.Provider)
		}
		if ev.Type == "channel.connected" {
			props.Set("reconnected", d.Reconnected)
		}
	case "post.created", "post.scheduled":
		if d.Post != nil {
			platforms := []string{}
			for _, t := range d.Post.Targets {
				platforms = append(platforms, t.Provider)
			}
			props.Set("status", d.Post.Status).Set("channel_count", len(d.Post.Targets)).Set("platforms", platforms).
				Set("media_count", len(d.Post.Media)).Set("thread_parts", len(d.Post.Thread)).Set("repeats", d.Post.RepeatEvery != "")
		}
	case "post.target_published", "post.target_failed":
		if d.Post != nil {
			for _, t := range d.Post.Targets {
				if t.ChannelID == d.ChannelID {
					props.Set("platform", t.Provider).Set("attempts", t.Attempts)
				}
			}
			// How late it went out after the time it was due (scheduled posts; publish now is due when asked).
			if at, err := time.Parse(timeFormat, deref(d.Post.PublishAt)); err == nil {
				props.Set("delay_seconds", int64(max(s.Now().Sub(at), 0)/time.Second))
			}
		}
		if ev.Type == "post.target_failed" {
			props.Set("error_class", errorClass(d.Error)).Set("error", truncateRunes(d.Error, 200))
		}
	case "workspace.plan_changed":
		props.Set("from", d.From).Set("to", d.To).Set("reason", d.Reason)
	case "workspace.credits_added":
		props.Set("posts", d.Posts)
	}
	s.track(q, s.capture(p, name, props))
}

// errorClass sorts a publishing error into a few kinds for the dashboards (the message, truncated, goes along).
func errorClass(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "needs reconnecting"):
		return "reauth"
	case strings.Contains(m, "refused the post"):
		return "rejected"
	case strings.Contains(m, "no result reported in time"):
		return "timeout"
	case strings.Contains(m, "could not start"):
		return "start_failed"
	case strings.Contains(m, "stored credentials"):
		return "credentials"
	case strings.Contains(m, "posts to x") || strings.Contains(m, "limit"):
		return "limit"
	case strings.Contains(m, "must") || strings.Contains(m, "at most") || strings.Contains(m, "required") || strings.Contains(m, "too "):
		return "validation"
	}
	return "platform_error"
}

// activeUsers remembers who was already counted active today, so user_active is one event per person,
// workspace and day. ponytail: per process; a second server instance may count a person once more a day
// (PostHog's unique-user counts absorb it).
type activeUsers struct {
	mu   sync.Mutex
	day  string
	seen map[string]bool
}

// trackActive sends user_active the first time today a person (or an API key acting for one) is authenticated.
func (s *Service) trackActive(p Principal) {
	if s.Cfg.Analytics == nil || p.UserID == "" {
		return
	}
	day, key := s.Now().UTC().Format(time.DateOnly), p.UserID+"/"+p.WorkspaceID+"/"+p.Kind
	s.active.mu.Lock()
	if s.active.day != day {
		s.active.day, s.active.seen = day, map[string]bool{}
	}
	seen := s.active.seen[key]
	s.active.seen[key] = true
	s.active.mu.Unlock()
	if !seen {
		_ = s.Cfg.Analytics.Enqueue(s.capture(p, "user_active", nil))
	}
}
