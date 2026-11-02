package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func metaSign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// A signed comment event about a published Instagram post queues a read of its comments; an unsigned one, or one
// about a post that isn't ours, queues nothing.
func TestMetaWebhookQueuesACommentsRead(t *testing.T) {
	s, p, _, _, chans := publishedOn(t, "fakemetaig")
	ctx := context.Background()
	env := map[string]string{"META_WEBHOOK_VERIFY_TOKEN": "tok", "PROVIDER_META_CLIENT_SECRET": "meta-secret"}
	s.Getenv = func(k string) string { return env[k] }
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'instagram' WHERE id = ?`, chans[0]); err != nil {
		t.Fatal(err)
	}
	if got, err := s.VerifyMetaWebhook("subscribe", "tok", "123"); err != nil || got != "123" {
		t.Fatalf("verify: %q %v", got, err)
	}
	if _, err := s.VerifyMetaWebhook("subscribe", "wrong", "123"); err == nil {
		t.Fatal("a wrong verify token must be refused")
	}
	// The fake published the post as "post" (fakeRemovable): Instagram names it as value.media.id.
	body := `{"object":"instagram","entry":[{"id":"1784","changes":[{"field":"comments","value":{"id":"c1","text":"nice","media":{"id":"post"}}}]}]}`
	if _, err := s.HandleMetaWebhook(ctx, []byte(body), metaSign("other", body)); err == nil {
		t.Fatal("a body signed with another secret must be refused")
	}
	n, err := s.HandleMetaWebhook(ctx, []byte(body), metaSign("meta-secret", body))
	if err != nil || n != 1 {
		t.Fatalf("queued %d, %v", n, err)
	}
	page, err := s.ListJobs(ctx, p, JobFilter{Kind: "read_responses"})
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("jobs %+v %v", page.Data, err)
	}
	other := `{"object":"page","entry":[{"changes":[{"field":"feed","value":{"item":"comment","post_id":"999_1"}}]}]}`
	if n, err := s.HandleMetaWebhook(ctx, []byte(other), metaSign("meta-secret", other)); err != nil || n != 0 {
		t.Fatalf("someone else's post: %d %v", n, err)
	}
}

// story_insights keeps an expired Instagram story's final numbers, and staqly asks the API for them no more.
func TestMetaWebhookKeepsStoryInsights(t *testing.T) {
	s, p, post, _, chans := publishedOn(t, "fakemetastory")
	ctx := context.Background()
	env := map[string]string{"META_WEBHOOK_VERIFY_TOKEN": "tok", "PROVIDER_META_CLIENT_SECRET": "meta-secret"}
	s.Getenv = func(k string) string { return env[k] }
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'instagram' WHERE id = ?`, chans[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE post_targets SET settings = '{"post_type":"story"}', published_at = '2020-01-01T00:00:00Z' WHERE post_id = ?`, post.ID); err != nil {
		t.Fatal(err)
	}
	body := `{"object":"instagram","entry":[{"id":"1784","changes":[{"field":"story_insights","value":{"media_id":"post","views":120,"reach":90,"replies":2,"taps_forward":30,"taps_back":4,"exits":7}}]}]}`
	if _, err := s.HandleMetaWebhook(ctx, []byte(body), metaSign("meta-secret", body)); err != nil {
		t.Fatal(err)
	}
	got, err := s.PostMetrics(ctx, p, post.ID, true)
	if err != nil || len(got) != 1 || got[0].Metrics == nil {
		t.Fatalf("metrics %+v %v", got, err)
	}
	m := got[0].Metrics
	if *m.Impressions != 120 || *m.Reach != 90 || *m.Replies != 2 || *m.TapsForward != 30 || *m.TapsBack != 4 || *m.Exits != 7 || got[0].Error != "" {
		t.Fatalf("story numbers %+v (error %q)", m, got[0].Error)
	}
}
