package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeAnalytics records what would go to PostHog; nothing leaves the test.
type fakeAnalytics struct {
	mu   sync.Mutex
	sent []posthog.Capture
}

func (f *fakeAnalytics) Enqueue(m posthog.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m.(posthog.Capture))
	return nil
}

func (f *fakeAnalytics) named(event string) []posthog.Capture {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []posthog.Capture
	for _, c := range f.sent {
		if c.Event == event {
			out = append(out, c)
		}
	}
	return out
}

func analyticsTest(t *testing.T) (*Service, Principal, Post, *fakeAnalytics) {
	t.Helper()
	s, p, post := publishTest(t)
	f := &fakeAnalytics{}
	s.Cfg.Analytics = f
	return s, p, post, f
}

// A post's life sends its product events with the platform, never its text, collection name or the error's
// account details; local mode has no person, so the events are the workspace's (personless).
func TestAnalyticsPostEvents(t *testing.T) {
	s, p, post, f := analyticsTest(t)
	ctx := context.Background()
	ch, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: "Second worker"})
	if err != nil {
		t.Fatal(err)
	}
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	created, err := s.CreatePost(ctx, p, PostInput{Collection: post.CollectionID, Content: "secret launch text", Status: "scheduled",
		PublishAt: &at, Targets: []PostTargetInput{{ChannelID: ch.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.W.SetTargetStatus(ctx, db.SetTargetStatusParams{Status: "publishing", Attempts: 1, PublishedAt: nullStr(""),
		UpdatedAt: s.now(), PostID: created.ID, ChannelID: ch.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.finishTarget(ctx, created.ID, ch.ID, "failed", providers.Result{}, "the platform refused the post: text too long"); err != nil {
		t.Fatal(err)
	}

	if c := f.named("channel_connected"); len(c) != 1 || c[0].Properties["platform"] != "custom" {
		t.Fatalf("channel_connected = %+v", c)
	}
	pc := f.named("post_created")
	if len(pc) != 1 || pc[0].Properties["status"] != "scheduled" || pc[0].Properties["channel_count"] != 1 ||
		fmt.Sprint(pc[0].Properties["platforms"]) != "[custom]" {
		t.Fatalf("post_created = %+v", pc)
	}
	failed := f.named("post_publish_failed")
	if len(failed) != 1 || failed[0].Properties["platform"] != "custom" || failed[0].Properties["error_class"] != "rejected" {
		t.Fatalf("post_publish_failed = %+v", failed)
	}
	for _, c := range f.sent {
		if c.DistinctId != "workspace:"+p.WorkspaceID || c.Properties["$process_person_profile"] != false ||
			c.Properties["workspace_id"] != p.WorkspaceID {
			t.Fatalf("%s: distinct %q, props %v", c.Event, c.DistinctId, c.Properties)
		}
		if s := fmt.Sprint(c.Properties); strings.Contains(s, "secret launch") || strings.Contains(s, "Races") || strings.Contains(s, "worker") {
			t.Fatalf("%s carries content: %s", c.Event, s)
		}
	}
}

// Events of a transaction that rolls back are never sent; a committed one's are sent once, after the commit.
func TestAnalyticsOnlyAfterCommit(t *testing.T) {
	s, p, post, f := analyticsTest(t)
	ctx := context.Background()
	boom := errors.New("boom")
	err := s.tx(ctx, func(q *db.Queries) error {
		if _, err := s.emit(ctx, q, p, "post.updated", post.CollectionID, post.ID, map[string]any{}); err != nil {
			return err
		}
		if len(f.named("post_updated")) != 0 {
			t.Error("sent before the commit")
		}
		return boom
	})
	if !errors.Is(err, boom) || len(f.named("post_updated")) != 0 {
		t.Fatalf("err %v, sent %d", err, len(f.named("post_updated")))
	}
	if err := s.tx(ctx, func(q *db.Queries) error {
		_, err := s.emit(ctx, q, p, "post.updated", post.CollectionID, post.ID, map[string]any{})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.named("post_updated")); n != 1 {
		t.Fatalf("post_updated sent %d times", n)
	}
	// Event types without a counterpart stay out of PostHog.
	if err := s.tx(ctx, func(q *db.Queries) error {
		_, err := s.emit(ctx, q, p, "post.feedback_added", post.CollectionID, post.ID, map[string]any{"comment": "private"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.sent {
		if strings.Contains(c.Event, "feedback") {
			t.Fatalf("sent %s", c.Event)
		}
	}
}

// A person counts active once a day per workspace, as a person (not personless).
func TestAnalyticsActiveOncePerDay(t *testing.T) {
	s, _, _, f := analyticsTest(t)
	day := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return day }
	p := Principal{WorkspaceID: "ws_1", Kind: PrincipalSession, UserID: "usr_1"}
	s.trackActive(p)
	s.trackActive(p)
	s.trackActive(Principal{WorkspaceID: "ws_1", Kind: PrincipalSession}) // no person: not counted
	if got := f.named("user_active"); len(got) != 1 || got[0].DistinctId != "usr_1" || got[0].Properties["$process_person_profile"] != nil {
		t.Fatalf("user_active = %+v", got)
	}
	day = day.Add(24 * time.Hour)
	s.trackActive(p)
	if n := len(f.named("user_active")); n != 2 {
		t.Fatalf("next day: %d events", n)
	}
}

// Without POSTHOG_KEY nothing is tracked and nothing breaks.
func TestAnalyticsOff(t *testing.T) {
	s, p, post := publishTest(t)
	s.trackActive(Principal{WorkspaceID: p.WorkspaceID, Kind: PrincipalSession, UserID: "usr_1"})
	s.trackSignInFailed(ProviderEmail, ErrUnauthorized)
	if _, err := s.UpdatePost(context.Background(), p, post.ID, PostPatch{Tags: &[]string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.tracked.Load(s.Q); ok {
		t.Fatal("events kept without a client")
	}
}

func TestErrorClass(t *testing.T) {
	for msg, want := range map[string]string{
		"the account needs reconnecting":                      "reauth",
		"the platform refused the post: duplicate":            "rejected",
		"No result reported in time":                          "timeout",
		"Publishing could not start at the scheduled time: x": "start_failed",
		"text: must be at most 280 characters":                "validation",
		"bluesky: 503 Service Unavailable":                    "platform_error",
	} {
		if got := errorClass(msg); got != want {
			t.Errorf("errorClass(%q) = %q, want %q", msg, got, want)
		}
	}
}
