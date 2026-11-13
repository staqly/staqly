package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Meta's webhooks (Instagram, Facebook Pages, Threads): Meta calls the server when someone comments under a post, and
// (Instagram, story_insights) with a story's final numbers when it expires, which are stored as they come.
// The server answers by reading that post's comments now (the read_responses job, as the 30-minute sweep does),
// and its numbers a minute later (check_target, as the 6-hour sweep does), instead of waiting for the sweeps. The payload itself is only a hint: the read asks the platform, so a forged or
// odd event can at most queue a read of a post that is ours.
//
// Set up per app in Meta for Developers (Webhooks): the callback URL is the dashboard's /api/v1/meta/webhook and the
// verify token PROVIDER_META_WEBHOOK_VERIFY_TOKEN. Meta signs every call with the app's secret (X-Hub-Signature-256).

// metaWebhookDelay: Meta calls before its API shows the new comment, and a burst of comments makes one read.
const metaWebhookDelay = 10 * time.Second

// metaWebhookNumbersDelay: the same event (a comment, or on Facebook a reaction) also changes the post's numbers,
// read by a check_target this long after the first event of a burst (debounce): later events join the queued check,
// so a burst of reactions costs one read and a busy post still gets its numbers every minute or so.
const metaWebhookNumbersDelay = time.Minute

// MetaWebhookEnabled: the operator set a verify token; without one the endpoint answers 404.
func (s *Service) MetaWebhookEnabled() bool { return s.metaVerifyToken() != "" }

// metaVerifyToken: PROVIDER_META_WEBHOOK_VERIFY_TOKEN, or META_WEBHOOK_VERIFY_TOKEN as servers set before it was renamed.
func (s *Service) metaVerifyToken() string {
	return s.Env("PROVIDER_META_WEBHOOK_VERIFY_TOKEN", s.Env("META_WEBHOOK_VERIFY_TOKEN", ""))
}

// VerifyMetaWebhook answers Meta's subscription check (GET ?hub.mode=subscribe&hub.verify_token=...&hub.challenge=...):
// the challenge when the token is ours.
func (s *Service) VerifyMetaWebhook(mode, token, challenge string) (string, error) {
	want := s.metaVerifyToken()
	if want == "" || mode != "subscribe" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		return "", forbidden("the verify token doesn't match PROVIDER_META_WEBHOOK_VERIFY_TOKEN")
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

// HandleMetaWebhook checks the signature and queues a read of the comments and of the numbers (likes, reactions,
// comments count) of every published post the event names. It returns how many comment reads it queued. Meta retries
// a call that fails, so only a bad signature is an error.
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
	if m, _ := payload.(map[string]any); m["object"] == "permissions" {
		return s.metaPermissionsChanged(ctx, body)
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
			if err := s.debounce(ctx, s.W, r.WorkspaceID, "check_target", "check_target:"+r.PostID+":"+r.ChannelID,
				map[string]any{"post_id": r.PostID, "channel_id": r.ChannelID}, s.Now().Add(metaWebhookNumbersDelay), 3); err != nil {
				return queued, err
			}
			queued++
		}
	}
	return queued, nil
}

func (s *Service) queueReadAt(ctx context.Context, r db.PublishedTargetsByExternalIDRow, at time.Time) error {
	return s.debounce(ctx, s.W, r.WorkspaceID, "read_responses", "read_responses:"+r.PostID+":"+r.ChannelID,
		map[string]any{"post_id": r.PostID, "channel_id": r.ChannelID}, at, 3)
}

// storyLifetime: an Instagram story is up for a day; afterwards the API no longer gives its numbers.
const storyLifetime = 24 * time.Hour

// storyExpired: a published Instagram story past its day, whose numbers are final (story_insights) and must not be
// asked for again.
func storyExpired(provider, settings string, publishedAt sql.NullString, now time.Time) bool {
	if (provider != "instagram" && provider != "instagram_standalone") || !publishedAt.Valid {
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
			if r.Provider != "instagram" && r.Provider != "instagram_standalone" {
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

// metaPermissionEvent is a Permissions webhook: entry.id is the Facebook user, a change names the permission
// (field), granted or revoked (verb), and the Pages or Instagram accounts it was taken from when only some were.
type metaPermissionEvent struct {
	Entry []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Verb      string   `json:"verb"`
				TargetIDs []string `json:"target_ids"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// metaPermissionProviders: the permissions a channel of a provider needs to publish or to comment.
var metaPermissionProviders = map[string]string{
	"pages_manage_posts": "facebook", "pages_manage_engagement": "facebook",
	"instagram_content_publish": "instagram", "instagram_manage_comments": "instagram",
}

// metaPermissionsChanged queues a token check (refresh_channel) of every channel a permission taken away (or given
// back) may touch: a Facebook Page connected by that person (and, when only some Pages are named, one of them), an
// Instagram account named. The check asks Meta, so the event is only a hint, like a comment's. It returns how many
// checks it queued.
func (s *Service) metaPermissionsChanged(ctx context.Context, body []byte) (int, error) {
	var ev metaPermissionEvent
	if json.Unmarshal(body, &ev) != nil {
		return 0, nil
	}
	type change struct {
		user    string
		targets []string
	}
	byProvider := map[string][]change{}
	for _, e := range ev.Entry {
		for _, c := range e.Changes {
			if p := metaPermissionProviders[c.Field]; p != "" && (c.Value.Verb == "revoked" || c.Value.Verb == "granted") {
				byProvider[p] = append(byProvider[p], change{e.ID, c.Value.TargetIDs})
			}
		}
	}
	if len(byProvider) == 0 {
		return 0, nil
	}
	// shortcut: unseals every connected Facebook and Instagram channel, fine while permission changes are rare and channels
	// number in the thousands; keep the account ids in a column when either grows.
	rows, err := s.Q.ConnectedMetaChannels(ctx)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, r := range rows {
		var c providers.Credentials
		if s.unseal(r.Credentials, &c) != nil {
			continue
		}
		hit := false
		provider := r.Provider
		if provider == "instagram_standalone" { // the same Instagram permissions
			provider = "instagram"
		}
		for _, rv := range byProvider[provider] {
			switch provider {
			case "facebook": // the Page's token is the person's who connected it
				hit = hit || (rv.user != "" && rv.user == c["authorized_by"] && (len(rv.targets) == 0 || slices.Contains(rv.targets, c["page_id"])))
			case "instagram": // the sign-in is a business integration's, not the person's: only the account names it
				hit = hit || slices.Contains(rv.targets, c["user_id"])
			}
		}
		if !hit {
			continue
		}
		if err := s.enqueue(ctx, s.W, r.WorkspaceID, "refresh_channel", "refresh_channel:"+r.ID, map[string]any{"channel_id": r.ID}, s.Now(), 3); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}
