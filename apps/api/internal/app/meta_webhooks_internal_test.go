package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
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

// fakeMetaPermissions is a Meta platform whose token check finds the comments permission gone.
type fakeMetaPermissions struct{ providers.Base }

func (f *fakeMetaPermissions) Refresh(_ context.Context, _ providers.App, c providers.Credentials) (providers.Credentials, error) {
	out := maps.Clone(c)
	out["no_comments"] = "true"
	return out, nil
}

// A permission taken away or given back queues a token check of the channels it may touch, and only those: a Page connected by
// that person, an Instagram account named in it. The check turning comments off turns the channel's comments off.
func TestMetaWebhookPermissionRevokedChecksTheChannel(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	env := map[string]string{"META_WEBHOOK_VERIFY_TOKEN": "tok", "PROVIDER_META_CLIENT_SECRET": "meta-secret", "PROVIDER_META_CLIENT_ID": "cid"}
	s.Getenv = func(k string) string { return env[k] }
	for _, id := range []string{"facebook", "instagram"} {
		orig, _ := providers.Get(id)
		providers.Register(&fakeMetaPermissions{providers.NewBase(id, id, providers.AuthOAuth2, providers.Limits{MaxChars: 280})}, providers.StatusAvailable)
		t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
	}
	for id, c := range map[string]providers.Credentials{
		"chn_fb1": {"authorized_by": "u1", "page_id": "p1"}, "chn_fb2": {"authorized_by": "u2", "page_id": "p2"}, "chn_ig1": {"user_id": "ig1"},
	} {
		sealed, err := s.seal(c)
		if err != nil {
			t.Fatal(err)
		}
		provider := map[bool]string{true: "facebook", false: "instagram"}[strings.HasPrefix(id, "chn_fb")]
		if err := s.W.CreateChannel(ctx, db.CreateChannelParams{ID: id, WorkspaceID: p.WorkspaceID, Provider: provider, Name: id,
			Status: "connected", Credentials: sealed, Settings: "{}", CreatedAt: s.now(), UpdatedAt: s.now()}); err != nil {
			t.Fatal(err)
		}
	}
	send := func(body string) int {
		t.Helper()
		n, err := s.HandleMetaWebhook(ctx, []byte(body), metaSign("meta-secret", body))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := send(`{"object":"permissions","entry":[{"id":"u1","changes":[{"field":"pages_manage_posts","value":{"verb":"granted"}}]},
		{"id":"u1","changes":[{"field":"user_posts","value":{"verb":"revoked"}}]}]}`); n != 1 {
		t.Fatalf("granted: %d checks queued, want chn_fb1", n)
	}
	if n := send(`{"object":"permissions","entry":[{"id":"u9","changes":[{"field":"instagram_manage_comments","value":{"verb":"revoked","target_ids":["ig1"]}},
		{"field":"pages_manage_engagement","value":{"verb":"revoked"}}]},
		{"id":"u1","changes":[{"field":"pages_manage_engagement","value":{"verb":"revoked","target_ids":["p1"]}}]}]}`); n != 2 {
		t.Fatalf("revoked: %d checks queued, want chn_fb1 and chn_ig1", n)
	}
	s.RunDueJobsOnce(ctx)
	for id, want := range map[string]int64{"chn_fb1": 0, "chn_fb2": 1, "chn_ig1": 0} {
		row, err := s.Q.GetChannelByID(ctx, id)
		if err != nil || row.Comments != want || row.Status != "connected" {
			t.Fatalf("%s: comments %d status %q, want comments %d: %v", id, row.Comments, row.Status, want, err)
		}
	}
}
