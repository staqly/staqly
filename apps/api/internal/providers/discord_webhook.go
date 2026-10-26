package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Discord through a channel webhook: the person makes one in the channel's settings (Integrations > Webhooks) and
// pastes its URL; no bot, no app, no OAuth. The way in on a server without its own Discord bot (discord.go), and
// what every Discord channel connected before the bot is (migration 00063 moved them from "discord" here).
// Messages go to POST <webhook>?wait=true (so the message comes back with its id), files as multipart attachments.
// A webhook can't reply to a message in a text channel, but it can post inside a thread: a post made in a forum
// channel (with a forum post title) starts a thread, and its thread parts go there. The webhook URL is the secret
// (its token is in the path): errors never quote it.
type DiscordWebhook struct{ Base }

// discordAPI is Discord's API (webhook URLs point there too), a variable so tests point it at httptest.
var discordAPI = "https://discord.com/api"

func init() {
	b := NewBase("discord_webhook", "Discord webhook", AuthToken,
		Limits{MaxChars: 2000, MaxMedia: 10, MediaKinds: []string{"image", "video"}}).WithSettings(discordWebhookSettings...)
	b.docs = "https://docs.staqly.ai/channels/discord#through-a-webhook"
	Register(DiscordWebhook{b}, StatusAvailable)
}

func (DiscordWebhook) CredentialFields() []Field {
	return []Field{
		{Key: "webhook_url", Label: "Webhook URL", Kind: "secret", Required: true,
			Placeholder: "https://discord.com/api/webhooks/123456789012345678/abc...",
			Help:        "In Discord: the channel's settings (or the server's) > Integrations > Webhooks > New Webhook > Copy Webhook URL."},
	}
}

// discordWebhookSettings: per-post settings; a forum channel needs a title for the forum post the message starts.
var discordWebhookSettings = []Field{
	{Key: "thread_name", Label: "Forum post title", Kind: "text", MaxLength: 100,
		Help: "Needed when the webhook posts into a forum channel: the post starts a forum thread with this title, and its comments go into the thread."},
}

// discordWebhook is what GET <webhook> says about it.
type discordWebhook struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	Type      int    `json:"type"` // 1 incoming
}

// discordWebhookPath turns a pasted webhook URL into "<id>/<token>" (discord.com, discordapp.com, canary and ptb).
func discordWebhookPath(v string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || u.Scheme != "https" {
		return "", false
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(u.Host, "canary."), "ptb."), "www.")
	if host != "discord.com" && host != "discordapp.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// /api/webhooks/<id>/<token> or /api/v10/webhooks/<id>/<token>
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "webhooks" {
			id, token := parts[i+1], parts[i+2]
			if _, err := strconv.ParseUint(id, 10, 64); err != nil || token == "" {
				return "", false
			}
			return id + "/" + token, true
		}
	}
	return "", false
}

func (d DiscordWebhook) Connect(ctx context.Context, fields map[string]string) (Credentials, Profile, error) {
	path, ok := discordWebhookPath(fields["webhook_url"])
	if !ok {
		return nil, Profile{}, invalid("webhook_url", "paste the whole webhook URL, like https://discord.com/api/webhooks/123456789012345678/abc...")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discordAPI+"/webhooks/"+path, nil)
	if err != nil {
		return nil, Profile{}, err
	}
	var w discordWebhook
	if err := discordDo(ctx, req, &w); err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, invalid("webhook_url", "Discord does not know this webhook (deleted, or the URL is incomplete); copy it again")
		}
		return nil, Profile{}, err
	}
	if w.ID == "" || w.ChannelID == "" {
		return nil, Profile{}, invalid("webhook_url", "this is not a channel webhook; make one under the channel's Integrations > Webhooks")
	}
	creds := Credentials{"webhook_url": "https://discord.com/api/webhooks/" + path, "webhook_id": w.ID,
		"channel_id": w.ChannelID, "guild_id": w.GuildID}
	name := w.Name
	if name == "" {
		name = "Discord webhook"
	}
	// The webhook's id is what stays the same: two webhooks may share a name, and a channel may have several.
	profile := Profile{Name: name, Handle: name + " (" + w.ID + ")"}
	if w.GuildID != "" {
		profile.ProfileURL = "https://discord.com/channels/" + w.GuildID + "/" + w.ChannelID
	}
	if w.Avatar != "" {
		profile.AvatarURL = "https://cdn.discordapp.com/avatars/" + w.ID + "/" + w.Avatar + ".png"
	}
	return creds, profile, nil
}

// Discord publishes a post's thread inside a forum thread the post started.
var _ Replier = DiscordWebhook{}

func (d DiscordWebhook) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	return d.send(ctx, c, p, strings.TrimSpace(s["thread_name"]), "")
}

// Reply posts a thread part into the forum thread the post started. ExternalIDs of messages in a thread are
// "<thread id>/<message id>"; a post in a text channel has no thread a webhook could post into.
func (d DiscordWebhook) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	thread, _, ok := strings.Cut(parentExternalID, "/")
	if !ok || thread == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Discord: a webhook cannot reply to a message in a text channel; comments go out only under a forum post (give the post a forum post title)"}
	}
	return d.send(ctx, c, p, "", thread)
}

type discordMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
}

// send executes the webhook: the text, the files as attachments, a new forum thread (threadName) or an existing
// thread (threadID).
func (DiscordWebhook) send(ctx context.Context, c Credentials, p PostContent, threadName, threadID string) (Result, error) {
	path, ok := discordWebhookPath(c["webhook_url"])
	if !ok {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Discord: no webhook stored; reconnect the channel"}
	}
	payload := map[string]any{"content": p.Text}
	if threadName != "" {
		payload["thread_name"] = threadName
	}
	body, ctype, err := discordMultipart(ctx, payload, p.Media)
	if err != nil {
		return Result{}, err
	}
	q := url.Values{"wait": {"true"}}
	if threadID != "" {
		q.Set("thread_id", threadID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discordAPI+"/webhooks/"+path+"?"+q.Encode(), body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", ctype)
	var out discordMessage
	if err := discordDo(ctx, req, &out); err != nil {
		return Result{}, discordLost(err)
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Discord published the post but did not say its id; check the channel"}
	}
	res := Result{ExternalID: out.ID}
	// A message in a thread (a new forum post, or a reply in one) lives in the thread's channel.
	if out.ChannelID != "" && (out.ChannelID != c["channel_id"] || threadID != "") {
		res.ExternalID = out.ChannelID + "/" + out.ID
	}
	if g, ch := c["guild_id"], out.ChannelID; g != "" && ch != "" {
		res.URL = "https://discord.com/channels/" + g + "/" + ch + "/" + out.ID
	}
	return res, nil
}

// discordMultipart is a message as Discord takes it with files: payload_json (with an attachment per file) and
// the files as files[n].
func discordMultipart(ctx context.Context, payload map[string]any, media []MediaFile) (io.Reader, string, error) {
	attachments := make([]map[string]any, len(media))
	for i, m := range media {
		attachments[i] = map[string]any{"id": i, "filename": m.Name}
	}
	payload["attachments"] = attachments

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	pj, _ := json.Marshal(payload)
	_ = w.WriteField("payload_json", string(pj))
	for i, m := range media {
		f, err := m.Reader(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("Discord: cannot read %s: %w", m.Name, err)
		}
		part, err := w.CreateFormFile("files["+strconv.Itoa(i)+"]", m.Name)
		if err == nil {
			_, err = io.Copy(part, f)
		}
		_ = f.Close()
		if err != nil {
			return nil, "", fmt.Errorf("Discord: cannot read %s: %w", m.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &body, w.FormDataContentType(), nil
}

// discordLost: Discord takes no idempotency key for webhooks (and a bot's nonce dedupes only for a few minutes),
// so sending again after an answer that never came could post twice.
func discordLost(err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return err
	}
	return &Failure{Kind: ErrRejected, Msg: "Discord may have published the post, but its answer was lost (" + err.Error() + "); check the channel before posting it again"}
}

// discordDo sends a request and decodes a 2xx reply into out; Discord's errors become messages a person can act
// on. The URL holds the webhook's token, so a transport error names the host only.
func discordDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Discord could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return discordError(resp.StatusCode, raw)
}

// discordError maps Discord's replies ({"message","code"}, or {"retry_after"} on a rate limit) to what staqly does
// next: a webhook that is gone needs reconnecting, a refused message is final, rate limits and outages are retried.
func discordError(status int, raw []byte) error {
	var e struct {
		Message    string          `json:"message"`
		Code       int             `json:"code"`
		RetryAfter float64         `json:"retry_after"`
		Errors     json.RawMessage `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = string(raw)
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "Discord: " + msg} }
	switch {
	case status == http.StatusTooManyRequests:
		f := &Failure{Msg: "Discord rate limit reached; staqly tries again in a few minutes"}
		if e.RetryAfter > 0 {
			f.RetryAt = time.Now().Add(time.Duration(e.RetryAfter * float64(time.Second)))
		}
		return f
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Discord is unavailable (%d); staqly tries again in a few minutes", status)}
	case e.Code == 10015, status == http.StatusUnauthorized, status == http.StatusNotFound && e.Code != 10003:
		// 10015 Unknown Webhook: deleted in Discord, or its token was reset.
		return &Failure{Kind: ErrReauth, Msg: "Discord: the webhook no longer exists (deleted or reset in the channel's settings); reconnect the channel with a new webhook URL"}
	case e.Code == 10003:
		return rejected("the thread is gone (deleted or archived), so the comment has nowhere to go")
	case e.Code == 220001:
		return rejected("this webhook posts into a forum channel: give the post a forum post title in its Discord settings")
	case e.Code == 220003:
		return rejected("a forum post title only works with a webhook in a forum channel; clear it for this channel")
	case status == http.StatusRequestEntityTooLarge, e.Code == 40005:
		return rejected("a file is larger than this server lets webhooks upload (10 MB unless the server is boosted)")
	case status == http.StatusForbidden:
		return rejected("Discord refused the message (" + detail + ")")
	case status == http.StatusBadRequest:
		if len(e.Errors) > 0 {
			d := string(e.Errors)
			if len(d) > 200 {
				d = d[:200] + "..."
			}
			return rejected(detail + ": " + d)
		}
		return rejected(detail)
	}
	return &Failure{Msg: fmt.Sprintf("Discord answered %d: %s", status, detail)}
}
