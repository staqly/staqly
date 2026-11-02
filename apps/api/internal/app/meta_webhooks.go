package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Meta's webhooks (Instagram, Facebook Pages, Threads): Meta calls the server when someone comments under a post, and
// (Instagram, story_insights) with a story's final numbers when it expires, which are stored as they come.
// The server answers by reading that post's comments now (the read_responses job, as the 30-minute sweep does),
// instead of waiting for the sweep. The payload itself is only a hint: the read asks the platform, so a forged or
// odd event can at most queue a read of a post that is ours.
//
// Set up per app in Meta for Developers (Webhooks): the callback URL is the dashboard's /api/v1/meta/webhook and the
// verify token META_WEBHOOK_VERIFY_TOKEN. Meta signs every call with the app's secret (X-Hub-Signature-256).

// metaWebhookDelay: Meta calls before its API shows the new comment, and a burst of comments makes one read.
const metaWebhookDelay = 10 * time.Second

// MetaWebhookEnabled: the operator set a verify token; without one the endpoint answers 404.
func (s *Service) MetaWebhookEnabled() bool { return s.Env("META_WEBHOOK_VERIFY_TOKEN", "") != "" }

// VerifyMetaWebhook answers Meta's subscription check (GET ?hub.mode=subscribe&hub.verify_token=...&hub.challenge=...):
// the challenge when the token is ours.
func (s *Service) VerifyMetaWebhook(mode, token, challenge string) (string, error) {
	want := s.Env("META_WEBHOOK_VERIFY_TOKEN", "")
	if want == "" || mode != "subscribe" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		return "", forbidden("the verify token doesn't match META_WEBHOOK_VERIFY_TOKEN")
	}
	return challenge, nil
}

// metaAppSecrets: the secrets Meta may sign with: the Meta app's (Facebook, Instagram with Facebook login), the
// Instagram app's (Instagram login) and the Threads app's.
func (s *Service) metaAppSecrets() []string {
	var out []string
	for _, k := range []string{"PROVIDER_META_CLIENT_SECRET", "PROVIDER_INSTAGRAM_CLIENT_SECRET", "PROVIDER_THREADS_CLIENT_SECRET"} {
		if v := s.Env(k, ""); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// metaSigned: X-Hub-Signature-256 is "sha256=<hex HMAC of the body>" with one of the apps' secrets.
func (s *Service) metaSigned(body []byte, header string) bool {
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	for _, secret := range s.metaAppSecrets() {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		if hmac.Equal(got, mac.Sum(nil)) {
			return true
		}
	}
	return false
}

// metaPostIDKeys: where an event names the post a comment is under. Instagram: value.media.id; Facebook: value.post_id
// ("<page>_<post>"); Threads: value.root_post.id and value.replied_to.id.
var metaPostIDKeys = map[string]bool{"media": true, "post_id": true, "root_post": true, "replied_to": true, "parent_id": true}

// metaPostIDs collects the ids an event names under metaPostIDKeys, anywhere in it (the three platforms nest their
// events differently: entry[].changes[].value, values[].value).
func metaPostIDs(v any, out map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if metaPostIDKeys[k] {
				switch id := x.(type) {
				case string:
					out[id] = true
				case map[string]any:
					if s, ok := id["id"].(string); ok {
						out[s] = true
					}
				}
			}
			metaPostIDs(x, out)
		}
	case []any:
		for _, x := range t {
			metaPostIDs(x, out)
		}
	}
}

// maxMetaIDs: an event names a handful of posts; more is not Meta's.
const maxMetaIDs = 100

// HandleMetaWebhook checks the signature and queues a read of the comments of every published post the event names.
// It returns how many reads it queued. Meta retries a call that fails, so only a bad signature is an error.
func (s *Service) HandleMetaWebhook(ctx context.Context, body []byte, signature string) (int, error) {
	if !s.metaSigned(body, signature) {
		return 0, forbidden("the signature doesn't match any Meta app secret")
	}
	var payload any
	if json.Unmarshal(body, &payload) != nil {
		return 0, nil
	}
	if err := s.storeStoryInsights(ctx, payload); err != nil {
		return 0, err
	}
	ids := map[string]bool{}
	metaPostIDs(payload, ids)
	queued := 0
	for id := range ids {
		if queued >= maxMetaIDs || id == "" {
			break
		}
		rows, err := s.Q.PublishedTargetsByExternalID(ctx, id)
		if err != nil {
			return queued, err
		}
		for _, r := range rows {
			if err := s.queueReadAt(ctx, r, s.Now().Add(metaWebhookDelay)); err != nil {
				return queued, err
			}
			queued++
		}
	}
	return queued, nil
}

func (s *Service) queueReadAt(ctx context.Context, r db.PublishedTargetsByExternalIDRow, at time.Time) error {
	return s.enqueue(ctx, s.W, r.WorkspaceID, "read_responses", "read_responses:"+r.PostID+":"+r.ChannelID,
		map[string]any{"post_id": r.PostID, "channel_id": r.ChannelID}, at, 3)
}

// storyLifetime: an Instagram story is up for a day; afterwards the API no longer gives its numbers.
const storyLifetime = 24 * time.Hour

// storyExpired: a published Instagram story past its day, whose numbers are final (story_insights) and must not be
// asked for again.
func storyExpired(provider, settings string, publishedAt sql.NullString, now time.Time) bool {
	if provider != "instagram" || !publishedAt.Valid {
		return false
	}
	var s map[string]string
	_ = json.Unmarshal([]byte(settings), &s)
	t, err := time.Parse(time.RFC3339Nano, publishedAt.String)
	return s["post_type"] == "story" && err == nil && now.Sub(t) > storyLifetime
}

// storeStoryInsights keeps an expired Instagram story's final numbers (the story_insights field): Meta sends them once,
// and the API has none afterwards. Every change {"field":"story_insights","value":{"media_id":...}} names the story.
func (s *Service) storeStoryInsights(ctx context.Context, payload any) error {
	var changes []map[string]any
	collectFields(payload, "story_insights", &changes)
	for _, v := range changes {
		id, _ := v["media_id"].(string)
		if id == "" {
			continue
		}
		n := func(keys ...string) *int64 {
			for _, k := range keys {
				if f, ok := v[k].(float64); ok && f >= 0 {
					i := int64(f)
					return &i
				}
			}
			return nil
		}
		m := providers.Metrics{Impressions: n("views", "impressions"), Reach: n("reach"), Replies: n("replies"),
			TapsForward: n("taps_forward"), TapsBack: n("taps_back"), Exits: n("exits"), Shares: n("shares")}
		rows, err := s.Q.PublishedTargetsByExternalID(ctx, id)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.Provider != "instagram" {
				continue
			}
			if err := s.W.SetTargetMetrics(ctx, db.SetTargetMetricsParams{Metrics: mustJSON(m), MetricsAt: nullStr(s.now()),
				PostID: r.PostID, ChannelID: r.ChannelID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectFields gathers the values of every {"field": name, "value": {...}} in a webhook body.
func collectFields(v any, name string, out *[]map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		if f, _ := t["field"].(string); f == name {
			if val, ok := t["value"].(map[string]any); ok {
				*out = append(*out, val)
			}
		}
		for _, x := range t {
			collectFields(x, name, out)
		}
	case []any:
		for _, x := range t {
			collectFields(x, name, out)
		}
	}
}
