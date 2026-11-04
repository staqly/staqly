package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// liveTest: a workspace with a draft on a custom channel, and a Kick channel (jane-doe) whose stream it can wait for.
func liveTest(t *testing.T) (*Service, Principal, Post, string, *rsa.PrivateKey) {
	t.Helper()
	s, p, post := publishTest(t)
	ctx := context.Background()
	post, err := s.CancelPost(ctx, p, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	kick, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: "Kick"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'kick', profile_url = 'https://kick.com/jane-doe' WHERE id = ?`, kick.ID); err != nil {
		t.Fatal(err)
	}
	s.Getenv = func(k string) string { return map[string]string{"PROVIDER_KICK_CLIENT_ID": "kick-app"}[k] }
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	t.Cleanup(providers.UseKickKey(&key.PublicKey))
	return s, p, post, kick.ID, key
}

func goLive(t *testing.T, s *Service, key *rsa.PrivateKey, id string, live bool) int {
	t.Helper()
	body := `{"broadcaster":{"user_id":4242,"channel_slug":"jane-doe"},"is_live":false,"title":"Stream"}`
	if live {
		body = `{"broadcaster":{"user_id":4242,"channel_slug":"jane-doe"},"is_live":true,"title":"Stream"}`
	}
	n, err := s.HandleKickWebhook(context.Background(), kickCall(t, key, id, s.Now().UTC().Format(time.RFC3339), "livestream.status.updated", body), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A post scheduled for the Kick stream waits without a time and goes out once when the stream starts; the stream
// ending, a repeated event and a second stream publish nothing more.
func TestPublishOnLive(t *testing.T) {
	s, p, post, kick, key := liveTest(t)
	ctx := context.Background()
	got, err := s.SchedulePostOn(ctx, p, post.ID, PublishOn{Event: "live", ChannelID: kick})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "scheduled" || got.PublishAt != nil || got.PublishOn == nil || got.PublishOn.ChannelID != kick {
		t.Fatalf("scheduled %+v %v %+v", got.Status, got.PublishAt, got.PublishOn)
	}
	s.RunDueJobsOnce(ctx)
	if got, _ := s.GetPost(ctx, p, post.ID); got.Status != "scheduled" {
		t.Fatalf("published before the stream: %s", got.Status)
	}
	if n := goLive(t, s, key, "01OFF", false); n != 0 {
		t.Fatalf("a stream ending published %d", n)
	}
	if n := goLive(t, s, key, "01LIVE", true); n != 1 {
		t.Fatalf("live: %d", n)
	}
	if n := goLive(t, s, key, "01LIVE", true); n != 0 {
		t.Fatalf("the same event again: %d", n)
	}
	s.RunDueJobsOnce(ctx)
	if got, _ := s.GetPost(ctx, p, post.ID); got.Status == "scheduled" || got.Status == "draft" {
		t.Fatalf("not published at the stream start: %s", got.Status)
	}
	if n := goLive(t, s, key, "01LIVE2", true); n != 0 {
		t.Fatalf("a second stream: %d", n)
	}
}

// What a post may wait for, and how a time and an event replace each other.
func TestPublishOnRules(t *testing.T) {
	s, p, post, kick, _ := liveTest(t)
	ctx := context.Background()
	var e *Error
	if _, err := s.SchedulePostOn(ctx, p, post.ID, PublishOn{Event: "live", ChannelID: post.Targets[0].ChannelID}); !errors.As(err, &e) || e.Fields["publish_on"] == "" {
		t.Fatalf("a platform without live_trigger: %v", err)
	}
	if _, err := s.SchedulePostOn(ctx, p, post.ID, PublishOn{Event: "offline", ChannelID: kick}); !errors.As(err, &e) {
		t.Fatalf("an unknown event: %v", err)
	}
	if _, err := s.SchedulePostOn(ctx, p, post.ID, PublishOn{Event: "live", ChannelID: "ch_nope"}); !errors.As(err, &e) {
		t.Fatalf("an unknown channel: %v", err)
	}
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err := s.CreatePost(ctx, p, PostInput{Title: "Both", Collection: post.CollectionID, Status: "scheduled", PublishAt: &at,
		PublishOn: &PublishOn{Event: "live", ChannelID: kick}, Targets: []PostTargetInput{{ChannelID: kick}}}); !errors.As(err, &e) {
		t.Fatalf("publish_at and publish_on: %v", err)
	}
	if _, err := s.CreatePost(ctx, p, PostInput{Title: "Repeat", Collection: post.CollectionID, Status: "scheduled", RepeatEvery: "1w",
		PublishOn: &PublishOn{Event: "live", ChannelID: kick}, Targets: []PostTargetInput{{ChannelID: kick}}}); !errors.As(err, &e) {
		t.Fatalf("repeat with publish_on: %v", err)
	}
	created, err := s.CreatePost(ctx, p, PostInput{Title: "Going live", Content: "Live now", Collection: post.CollectionID, Status: "scheduled",
		PublishOn: &PublishOn{Event: "live", ChannelID: kick}, Targets: []PostTargetInput{{ChannelID: kick}}})
	if err != nil || created.Status != "scheduled" || created.PublishOn == nil {
		t.Fatalf("create: %+v %v", created, err)
	}
	// A time replaces the event, and the event the time.
	got, err := s.UpdatePost(ctx, p, created.ID, PostPatch{PublishAt: []byte(`"` + at + `"`)})
	if err != nil || got.PublishOn != nil || got.PublishAt == nil || got.Status != "scheduled" {
		t.Fatalf("to a time: %+v %v", got, err)
	}
	got, err = s.UpdatePost(ctx, p, created.ID, PostPatch{PublishOn: []byte(`{"event":"live","channel_id":"` + kick + `"}`)})
	if err != nil || got.PublishOn == nil || got.PublishAt != nil || got.Status != "scheduled" {
		t.Fatalf("back to the stream: %+v %v", got, err)
	}
	if page, err := s.ListJobs(ctx, p, JobFilter{Kind: "publish_post"}); err != nil || len(page.Data) != 0 {
		t.Fatalf("a post waiting for a stream has no publish job: %+v %v", page.Data, err)
	}
	got, err = s.UpdatePost(ctx, p, created.ID, PostPatch{PublishOn: []byte(`null`)})
	if err != nil || got.Status != "draft" {
		t.Fatalf("cleared: %+v %v", got, err)
	}
	// A removed channel takes what waited for it back to draft.
	if _, err := s.SchedulePostOn(ctx, p, created.ID, PublishOn{Event: "live", ChannelID: kick}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChannel(ctx, p, kick); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPost(ctx, p, created.ID); got.Status != "draft" {
		t.Fatalf("after the channel went: %s", got.Status)
	}
}
