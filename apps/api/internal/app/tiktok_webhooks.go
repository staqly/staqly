package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// TikTok's webhooks: TikTok calls the server when a published video becomes public (post.publish.publicly_available)
// or stops being public, and when someone removes the app from their account (authorization.removed). A video
// published while TikTok was still reviewing it keeps its publish id; the event brings its public id, so the post
// links to the video and its numbers can be read. A removed app queues a token check, which finds the sign-in gone
// and asks to reconnect. As with Meta's, the event is only a hint: the check and the numbers ask TikTok.
//
// Set up in the TikTok developer portal (Webhooks): the callback URL is the dashboard's /api/v1/tiktok/webhook.
// TikTok signs every call with the app's client secret (TikTok-Signature).

// TikTokWebhookEnabled: the server has TikTok's app; without it the endpoint answers 404.
func (s *Service) TikTokWebhookEnabled() bool {
	return s.Env("PROVIDER_TIKTOK_CLIENT_SECRET", "") != ""
}

// tiktokSigned: TikTok-Signature is "t=<unix time>,s=<hex HMAC-SHA256 of "<t>.<body>">" with the client secret.
// shortcut: the server's app only, not a workspace's own TikTok app (Developer apps); check those too when one needs
// webhooks.
func (s *Service) tiktokSigned(body []byte, header string) bool {
	var ts, sig string
	for part := range strings.SplitSeq(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "s":
			sig = v
		}
	}
	got, err := hex.DecodeString(sig)
	secret := s.Env("PROVIDER_TIKTOK_CLIENT_SECRET", "")
	if err != nil || ts == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// tiktokEvent is one webhook call; content is a JSON object sent as a string.
type tiktokEvent struct {
	Event   string `json:"event"`
	OpenID  string `json:"user_openid"`
	Content string `json:"content"`
}

// HandleTikTokWebhook checks the signature and acts on the event. It returns how many posts or channels it touched.
// TikTok retries a call that fails, so only a bad signature is an error.
func (s *Service) HandleTikTokWebhook(ctx context.Context, body []byte, signature string) (int, error) {
	if !s.tiktokSigned(body, signature) {
		return 0, forbidden("the signature doesn't match TikTok's client secret")
	}
	var ev tiktokEvent
	if json.Unmarshal(body, &ev) != nil {
		return 0, nil
	}
	var c struct {
		PublishID string `json:"publish_id"`
		PostID    string `json:"post_id"`
	}
	_ = json.Unmarshal([]byte(ev.Content), &c)
	switch ev.Event {
	case "authorization.removed":
		return s.tiktokAppRemoved(ctx, ev.OpenID)
	case "post.publish.publicly_available":
		return s.tiktokPublic(ctx, c.PublishID, c.PostID)
	case "post.publish.no_longer_publicaly_available": // TikTok's spelling
		return s.tiktokCheck(ctx, c.PostID)
	}
	return 0, nil
}

// tiktokPublic gives the targets still known by their publish id the video's public id and link, then reads its
// numbers.
func (s *Service) tiktokPublic(ctx context.Context, publishID, postID string) (int, error) {
	if publishID == "" || postID == "" {
		return 0, nil
	}
	rows, err := s.Q.TikTokTargetsByExternalID(ctx, publishID)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		// The target links to the profile (https://www.tiktok.com/@user) until the video has an id.
		url := r.ExternalUrl
		if strings.HasPrefix(url, "https://www.tiktok.com/@") && !strings.Contains(url, "/video/") {
			url = strings.TrimRight(url, "/") + "/video/" + postID
		}
		if err := s.W.SetTargetExternalID(ctx, db.SetTargetExternalIDParams{ExternalID: postID, ExternalUrl: url,
			UpdatedAt: s.now(), PostID: r.PostID, ChannelID: r.ChannelID}); err != nil {
			return 0, err
		}
	}
	n, err := s.tiktokCheck(ctx, postID)
	return max(n, len(rows)), err
}

// tiktokCheck reads the numbers of the targets with this video (check_target), which also notices a video gone.
func (s *Service) tiktokCheck(ctx context.Context, postID string) (int, error) {
	if postID == "" {
		return 0, nil
	}
	rows, err := s.Q.TikTokTargetsByExternalID(ctx, postID)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := s.enqueue(ctx, s.W, r.WorkspaceID, "check_target", "check_target:"+r.PostID+":"+r.ChannelID,
			map[string]any{"post_id": r.PostID, "channel_id": r.ChannelID}, s.Now(), 3); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// tiktokAppRemoved queues a token check (refresh_channel) of every connected channel of that TikTok account.
func (s *Service) tiktokAppRemoved(ctx context.Context, openID string) (int, error) {
	if openID == "" {
		return 0, nil
	}
	// shortcut: unseals every connected TikTok channel, fine while removals are rare; keep open_id in a column when
	// channels number in the many thousands.
	rows, err := s.Q.ConnectedTikTokChannels(ctx)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, r := range rows {
		var c providers.Credentials
		if s.unseal(r.Credentials, &c) != nil || c["open_id"] != openID {
			continue
		}
		if err := s.enqueue(ctx, s.W, r.WorkspaceID, "refresh_channel", "refresh_channel:"+r.ID, map[string]any{"channel_id": r.ID}, s.Now(), 3); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}
