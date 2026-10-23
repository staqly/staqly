package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Mastodon (and other servers speaking its API: GoToSocial, Akkoma...) with an access token the person makes on
// their own server (Preferences > Development > New application). Mastodon apps are registered per server, and one
// OAuth app per staqly server can't sign in to every instance, so this is a token platform: the person names the
// instance and pastes the token. Statuses carry an Idempotency-Key, so a retry after a lost answer never posts twice.
type Mastodon struct{ Base }

var (
	// mastodonPoll is how long to wait between checks of an attachment the server is still processing.
	mastodonPoll = 2 * time.Second
	// mastodonDefault is the instance a person who leaves the field empty is on.
	mastodonDefault = "https://mastodon.social"
)

func init() {
	Register(Mastodon{NewBase("mastodon", "Mastodon", AuthToken,
		Limits{MaxChars: 500, MaxMedia: 4, MediaKinds: []string{"image", "video"}}).WithSettings(
		Field{Key: "visibility", Label: "Who can see it", Kind: "select", Default: "public", Options: []Option{
			{Value: "public", Label: "Public"},
			{Value: "unlisted", Label: "Quiet public (not in public timelines)"},
			{Value: "private", Label: "Followers only"},
		}},
		Field{Key: "spoiler_text", Label: "Content warning", Kind: "text", MaxLength: 500, Placeholder: "Spoilers for episode 3",
			Help: "Hides the post behind this warning until a reader opens it."},
	)}, StatusAvailable)
}

func (Mastodon) CredentialFields() []Field {
	return []Field{
		{Key: "instance", Label: "Server", Kind: "url", Default: mastodonDefault, Placeholder: mastodonDefault,
			Help: "The address of your Mastodon server."},
		{Key: "access_token", Label: "Access token", Kind: "secret", Required: true,
			Help: "Preferences > Development > New application, with the scopes write:statuses, write:media and read:accounts; copy \"Your access token\"."},
	}
}

func (m Mastodon) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(m.Limits(), p, s); err != nil {
		return err
	}
	for _, f := range p.Media {
		if strings.HasPrefix(f.MimeType, "video/") && len(p.Media) > 1 {
			return invalid("media", "Mastodon takes one video alone, or up to 4 pictures")
		}
	}
	return CheckFields(m.SettingsFields(), s)
}

// fediInstance turns what a person typed ("mastodon.social", "https://lemmy.world/") into a base URL.
func fediInstance(raw, def, field string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = def
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", invalid(field, "enter the server's address, for example "+def)
	}
	return u.Scheme + "://" + u.Host, nil
}

// fediKey is a stable key for one publish of one post (or one reply) to one channel: the same on every retry.
func fediKey(parts ...string) string {
	sum := sha256.Sum256([]byte("staqly\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

type mastodonAccount struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	URL         string `json:"url"`
}

func (Mastodon) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	instance, err := fediInstance(f["instance"], mastodonDefault, "instance")
	if err != nil {
		return nil, Profile{}, err
	}
	token := strings.TrimSpace(f["access_token"])
	if token == "" {
		return nil, Profile{}, invalid("access_token", "paste the access token")
	}
	var me mastodonAccount
	if err := mastodonCall(ctx, http.MethodGet, instance, token, "/api/v1/accounts/verify_credentials", nil, "", "", &me); err != nil {
		return nil, Profile{}, err
	}
	if me.Username == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "this server did not answer like Mastodon; check the server's address"}
	}
	host := strings.TrimPrefix(strings.TrimPrefix(instance, "https://"), "http://")
	name := me.DisplayName
	if name == "" {
		name = me.Username
	}
	return Credentials{"instance": instance, "access_token": token}, Profile{
		Handle: "@" + me.Username + "@" + host, Name: name, AvatarURL: me.Avatar, ProfileURL: me.URL,
	}, nil
}

// Mastodon publishes a post's thread as replies under it.
var _ Replier = Mastodon{}

func (m Mastodon) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	return m.post(ctx, c, "", p, s)
}

// Reply answers the post (or the reply before) with a status of the same visibility.
func (m Mastodon) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, s Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Mastodon: nothing to reply to (the post has no id)"}
	}
	return m.post(ctx, c, parentExternalID, p, s)
}

func (Mastodon) post(ctx context.Context, c Credentials, parent string, p PostContent, s Settings) (Result, error) {
	instance, token := c["instance"], c["access_token"]
	if instance == "" || token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Mastodon: no account stored; reconnect the account"}
	}
	var ids []string
	for _, f := range p.Media {
		id, err := mastodonUpload(ctx, instance, token, f)
		if err != nil {
			return Result{}, err
		}
		ids = append(ids, id)
	}
	body := map[string]any{"status": p.Text}
	if len(ids) > 0 {
		body["media_ids"] = ids
	}
	if v := s["visibility"]; v != "" {
		body["visibility"] = v
	}
	if cw := strings.TrimSpace(s["spoiler_text"]); cw != "" {
		body["spoiler_text"] = cw
		body["sensitive"] = true
	}
	if parent != "" {
		body["in_reply_to_id"] = parent
	}
	raw, _ := json.Marshal(body)
	// Mastodon keeps an Idempotency-Key for an hour and answers a repeat with the status it already made, so the job
	// queue's retries (1, 5 and 15 minutes) after an answer that never came can't post twice.
	key := fediKey("mastodon", p.PostID, p.ChannelID, parent)
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
		URI string `json:"uri"`
	}
	if err := mastodonCall(ctx, http.MethodPost, instance, token, "/api/v1/statuses", bytes.NewReader(raw), "application/json", key, &out); err != nil {
		return Result{}, err
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Mastodon published the post but did not say its id; check the profile"}
	}
	link := out.URL
	if link == "" {
		link = out.URI
	}
	return Result{ExternalID: out.ID, URL: link}, nil
}

// mastodonUpload sends one file and waits until the server has processed it (videos take a while).
func mastodonUpload(ctx context.Context, instance, token string, f MediaFile) (string, error) {
	r, err := f.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", f.Name, err)
	}
	defer r.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename=%q`, f.Name)}
	h["Content-Type"] = []string{f.MimeType}
	part, err := w.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, r); err != nil {
		return "", fmt.Errorf("reading %s: %w", f.Name, err)
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	var media struct {
		ID  string  `json:"id"`
		URL *string `json:"url"`
	}
	if err := mastodonCall(ctx, http.MethodPost, instance, token, "/api/v2/media", &buf, w.FormDataContentType(), "", &media); err != nil {
		return "", err
	}
	if media.ID == "" {
		return "", &Failure{Kind: ErrRejected, Msg: "Mastodon took " + f.Name + " but did not say its id"}
	}
	// 200 with a url: ready. 202 (url null): still processing; GET /api/v1/media/:id answers 206 until it is done.
	for media.URL == nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(mastodonPoll):
		}
		if err := mastodonCall(ctx, http.MethodGet, instance, token, "/api/v1/media/"+url.PathEscape(media.ID), nil, "", "", &media); err != nil {
			return "", err
		}
	}
	return media.ID, nil
}

// mastodonCall sends one request and decodes a 2xx reply into out.
func mastodonCall(ctx context.Context, method, instance, token, path string, body io.Reader, contentType, idemKey string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, instance+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	return mastodonDo(ctx, req, out)
}

func mastodonDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Failure{Msg: "Mastodon server " + req.URL.Host + " could not be reached; staqly tries again in a few minutes"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: "Mastodon server " + req.URL.Host + " did not answer like Mastodon; check the server's address"}
			}
		}
		return nil
	}
	return mastodonError(resp.StatusCode, raw)
}

// mastodonError maps Mastodon's answers ({"error": "..."}) to what staqly does next.
func mastodonError(status int, raw []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Error
	if detail == "" {
		detail = fediSnippet(raw)
	}
	lower := strings.ToLower(detail)
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Mastodon no longer accepts this access token (" + detail + "); make a new one and reconnect"}
	case status == http.StatusForbidden && strings.Contains(lower, "scope"):
		return &Failure{Kind: ErrReauth, Msg: "the access token lacks a scope (" + detail + "); make one with write:statuses, write:media and read:accounts and reconnect"}
	case status == http.StatusForbidden && (strings.Contains(lower, "suspended") || strings.Contains(lower, "disabled") || strings.Contains(lower, "login")):
		return &Failure{Kind: ErrReauth, Msg: "Mastodon refused the account (" + detail + ")"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Mastodon rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("the Mastodon server is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "Mastodon could not find it (" + detail + "); the server's address may be wrong, or the post replied to was deleted"}
	case status == http.StatusRequestEntityTooLarge:
		return &Failure{Kind: ErrRejected, Msg: "the file is larger than this Mastodon server takes"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Mastodon refused it (" + detail + ")"}
}

// fediSnippet is a short readable piece of a reply body that had no error field.
func fediSnippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		s = "no details"
	}
	return s
}
