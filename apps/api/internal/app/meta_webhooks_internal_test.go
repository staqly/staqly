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
