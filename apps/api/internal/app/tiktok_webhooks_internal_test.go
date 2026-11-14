package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

func tiktokSign(secret, ts, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + body))
	return "t=" + ts + ",s=" + hex.EncodeToString(mac.Sum(nil))
}

// A video TikTok makes public gets its id and link in place of the publish id, and its numbers are read; one that
// stops being public is checked; an unsigned call is refused.
func TestTikTokWebhookPublicVideo(t *testing.T) {
	s, p, post, _, chans := publishedOn(t, "faketiktokpub")
	ctx := context.Background()
	s.Getenv = func(k string) string { return map[string]string{"PROVIDER_TIKTOK_CLIENT_SECRET": "tt-secret"}[k] }
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'tiktok' WHERE id = ?`, chans[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE post_targets SET external_id = 'v_pub_file~v2.1', external_url = 'https://www.tiktok.com/@jane' WHERE post_id = ?`, post.ID); err != nil {
		t.Fatal(err)
	}
	body := `{"client_key":"ck","event":"post.publish.publicly_available","create_time":1,"user_openid":"o1",` +
		`"content":"{\"publish_id\":\"v_pub_file~v2.1\",\"post_id\":\"736\",\"publish_type\":\"DIRECT_PUBLISH\"}"}`
	if _, err := s.HandleTikTokWebhook(ctx, []byte(body), tiktokSign("other", "1", body)); err == nil {
		t.Fatal("a body signed with another secret must be refused")
	}
	if n, err := s.HandleTikTokWebhook(ctx, []byte(body), tiktokSign("tt-secret", "1", body)); err != nil || n != 1 {
		t.Fatalf("handled %d, %v", n, err)
	}
	tg, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: post.ID, ChannelID: chans[0]})
	if err != nil || tg.ExternalID != "736" || tg.ExternalUrl != "https://www.tiktok.com/@jane/video/736" {
		t.Fatalf("target %q %q %v", tg.ExternalID, tg.ExternalUrl, err)
	}
	if page, err := s.ListJobs(ctx, p, JobFilter{Kind: "check_target"}); err != nil || len(page.Data) != 1 {
		t.Fatalf("jobs %+v %v", page.Data, err)
	}
	gone := `{"event":"post.publish.no_longer_publicaly_available","user_openid":"o1","content":"{\"post_id\":\"736\"}"}`
	if n, err := s.HandleTikTokWebhook(ctx, []byte(gone), tiktokSign("tt-secret", "2", gone)); err != nil || n != 1 {
		t.Fatalf("no longer public: %d %v", n, err)
	}
	other := `{"event":"post.publish.publicly_available","content":"{\"publish_id\":\"v_pub_other\",\"post_id\":\"9\"}"}`
	if n, err := s.HandleTikTokWebhook(ctx, []byte(other), tiktokSign("tt-secret", "3", other)); err != nil || n != 0 {
		t.Fatalf("someone else's video: %d %v", n, err)
	}
}

// authorization.removed queues a token check of that account's channels only.
func TestTikTokWebhookAppRemoved(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	s.Getenv = func(k string) string { return map[string]string{"PROVIDER_TIKTOK_CLIENT_SECRET": "tt-secret"}[k] }
	for id, open := range map[string]string{"chn_tt1": "o1", "chn_tt2": "o2"} {
		sealed, err := s.seal(providers.Credentials{"open_id": open})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.W.CreateChannel(ctx, db.CreateChannelParams{ID: id, WorkspaceID: p.WorkspaceID, Provider: "tiktok", Name: id,
			Status: "connected", Credentials: sealed, Settings: "{}", CreatedAt: s.now(), UpdatedAt: s.now()}); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"event":"authorization.removed","user_openid":"o1","content":"{\"reason\":1}"}`
	if n, err := s.HandleTikTokWebhook(ctx, []byte(body), tiktokSign("tt-secret", "1", body)); err != nil || n != 1 {
		t.Fatalf("removed: %d %v", n, err)
	}
	page, err := s.ListJobs(ctx, p, JobFilter{Kind: "refresh_channel"})
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("jobs %+v %v", page.Data, err)
	}
}
