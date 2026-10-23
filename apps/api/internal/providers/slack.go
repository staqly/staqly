package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Slack over the Web API: the person installs the server's Slack app into a workspace (OAuth v2) and staqly keeps
// the bot token; every channel the bot may post to can become a staqly channel (AccountLister). Messages go out with
// chat.postMessage, files through files.getUploadURLExternal + files.completeUploadExternal, a post's thread as
// replies in the message's Slack thread. Bot tokens last until the app is removed, unless the app turned token
// rotation on: then a refresh token renews them every 12 hours.
type Slack struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	slackAPI       = "https://slack.com/api"
	slackAuthorize = "https://slack.com/oauth/v2/authorize"
	// slackPoll is how long to wait between checks for the message a file upload made; slackPolls how many.
	slackPoll  = time.Second
	slackPolls = 10
)

// slackScopes are bot scopes: post (chat:write), join public channels to post files there (channels:join), upload
// files and find the message they made (files:write, files:read), list the channels to pick from (channels:read,
// groups:read).
const slackScopes = "chat:write,channels:join,channels:read,groups:read,files:write,files:read"

func init() {
	Register(Slack{NewBase("slack", "Slack", AuthOAuth2,
		Limits{MaxChars: 40000, MaxMedia: 10, MediaKinds: []string{"image", "video"}}).WithSettings(slackSettings...)}, StatusAvailable)
}

// slackSettings: per-post settings.
var slackSettings = []Field{
	{Key: "link_preview", Label: "Show link previews", Kind: "boolean", Default: "true",
		Help: "Slack unfurls links in the message into previews."},
}

// Slack calls back over https only.
func (Slack) Needs() Needs { return Needs{HTTPS: true} }

func (Slack) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":    {app.ClientID},
		"scope":        {slackScopes},
		"redirect_uri": {redirect},
		"state":        {state},
	}
	return slackAuthorize + "?" + q.Encode(), nil
}

type slackToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	BotUserID    string `json:"bot_user_id"`
	Team         struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
}

func (t slackToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" {
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if t.BotUserID != "" {
		out["bot_user_id"] = t.BotUserID
	}
	if t.Team.ID != "" {
		out["team_id"], out["team_name"] = t.Team.ID, t.Team.Name
	}
	return out
}

func (Slack) token(ctx context.Context, app App, form url.Values) (slackToken, error) {
	var t slackToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := checkForm(ctx, slackAPI+"/oauth.v2.access", form)
	if err != nil {
		return t, err
	}
	if err := slackDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Slack returned no bot token")
	}
	return t, nil
}

func (s Slack) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := s.token(ctx, app, url.Values{"code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	who, err := s.authTest(ctx, creds["access_token"])
	if err != nil {
		return nil, Profile{}, err
	}
	creds["team_url"] = who.URL
	if who.Team != "" {
		creds["team_name"] = who.Team
	}
	return creds, Profile{Name: creds["team_name"], Handle: creds["team_name"], ProfileURL: who.URL}, nil
}

type slackWho struct {
	URL    string `json:"url"` // https://acme.slack.com/
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	UserID string `json:"user_id"`
}

func (Slack) authTest(ctx context.Context, token string) (slackWho, error) {
	var who slackWho
	req, err := slackRequest(ctx, token, "auth.test", nil)
	if err != nil {
		return who, err
	}
	return who, slackDo(ctx, req, &who)
}

// Refresh renews a rotating token; a token without rotation does not expire, so it is only checked.
func (s Slack) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		if _, err := s.authTest(ctx, c["access_token"]); err != nil {
			return nil, err
		}
		return c, nil
	}
	t, err := s.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// Slack signs in a workspace; each of its channels the bot may post to becomes a staqly channel.
var _ AccountLister = Slack{}

type slackChannel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsPrivate bool   `json:"is_private"`
	IsMember  bool   `json:"is_member"`
	IsArchive bool   `json:"is_archived"`
}

// Accounts lists the public channels (the bot joins one when it is picked) and the private ones the app was
// invited to.
func (s Slack) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	var out []Account
	cursor := ""
	for page := 0; page < 20; page++ {
		q := url.Values{"types": {"public_channel,private_channel"}, "exclude_archived": {"true"}, "limit": {"200"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var res struct {
			Channels []slackChannel `json:"channels"`
			Meta     struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		req, err := slackRequest(ctx, c["access_token"], "conversations.list?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if err := slackDo(ctx, req, &res); err != nil {
			return nil, err
		}
		for _, ch := range res.Channels {
			if ch.IsArchive || (ch.IsPrivate && !ch.IsMember) {
				continue
			}
			out = append(out, Account{ID: ch.ID, Name: "#" + ch.Name, Handle: "#" + ch.Name,
				ProfileURL: slackChannelURL(c["team_url"], ch.ID)})
		}
		if cursor = res.Meta.NextCursor; cursor == "" {
			break
		}
	}
	if len(out) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "Slack: the app sees no channel to post to in " + c["team_name"] + "; create a channel (or invite the app to a private one with /invite), then connect again"}
	}
	return out, nil
}

// UseAccount narrows the workspace sign-in to one channel; the bot joins a public channel it is not in yet, so
// files can be shared there too.
func (s Slack) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	var info struct {
		Channel slackChannel `json:"channel"`
	}
	req, err := slackRequest(ctx, c["access_token"], "conversations.info?"+url.Values{"channel": {accountID}}.Encode(), nil)
	if err != nil {
		return nil, Profile{}, err
	}
	if err := slackDo(ctx, req, &info); err != nil {
		return nil, Profile{}, err
	}
	ch := info.Channel
	switch {
	case ch.ID == "":
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Slack: that channel is not there any more"}
	case ch.IsArchive:
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Slack: #" + ch.Name + " is archived; unarchive it or pick another channel"}
	case ch.IsPrivate && !ch.IsMember:
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Slack: invite the app to #" + ch.Name + " (/invite in the channel), then connect again"}
	case !ch.IsMember:
		req, err := slackRequest(ctx, c["access_token"], "conversations.join", map[string]any{"channel": ch.ID})
		if err != nil {
			return nil, Profile{}, err
		}
		if err := slackDo(ctx, req, nil); err != nil {
			return nil, Profile{}, err
		}
	}
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["channel_id"], out["channel_name"] = ch.ID, ch.Name
	// The channel id is what stays unique: two workspaces may both have a #general.
	return out, Profile{Name: "#" + ch.Name, Handle: "#" + ch.Name + " (" + ch.ID + ")",
		ProfileURL: slackChannelURL(c["team_url"], ch.ID)}, nil
}

func slackChannelURL(teamURL, channel string) string {
	if teamURL == "" {
		return ""
	}
	return strings.TrimSuffix(teamURL, "/") + "/archives/" + channel
}

// Slack publishes a post's thread as replies in the message's thread.
var _ Replier = Slack{}

func (s Slack) Publish(ctx context.Context, c Credentials, p PostContent, set Settings) (Result, error) {
	return s.send(ctx, c, p, set, "")
}

// Reply posts a thread part into the thread of the post. Slack threads hang off the first message, so a reply's
// ExternalID is "<thread ts>/<reply ts>" and every part names the thread's ts.
func (s Slack) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, set Settings) (Result, error) {
	root, _, _ := strings.Cut(parentExternalID, "/")
	if root == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Slack: nothing to reply to (the post has no id)"}
	}
	return s.send(ctx, c, p, set, root)
}

// slackEscape escapes what Slack's message format reserves.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func (s Slack) send(ctx context.Context, c Credentials, p PostContent, set Settings, threadTS string) (Result, error) {
	token, channel := c["access_token"], c["channel_id"]
	if token == "" || channel == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Slack: no channel stored; reconnect it"}
	}
	text := slackEscape.Replace(p.Text)
	var ts string
	if len(p.Media) == 0 {
		body := map[string]any{"channel": channel, "text": text}
		if threadTS != "" {
			body["thread_ts"] = threadTS
		}
		if set["link_preview"] == "false" {
			body["unfurl_links"], body["unfurl_media"] = false, false
		}
		req, err := slackRequest(ctx, token, "chat.postMessage", body)
		if err != nil {
			return Result{}, err
		}
		var out struct {
			TS string `json:"ts"`
		}
		if err := slackDo(ctx, req, &out); err != nil {
			return Result{}, slackLost(err)
		}
		ts = out.TS
	} else {
		// Upload every file first (safe to retry: nothing is shared yet), then share them in one message.
		files := make([]map[string]string, 0, len(p.Media))
		for _, m := range p.Media {
			id, err := s.upload(ctx, token, m)
			if err != nil {
				return Result{}, err
			}
			files = append(files, map[string]string{"id": id, "title": m.Name})
		}
		body := map[string]any{"files": files, "channel_id": channel}
		if text != "" {
			body["initial_comment"] = text
		}
		if threadTS != "" {
			body["thread_ts"] = threadTS
		}
		req, err := slackRequest(ctx, token, "files.completeUploadExternal", body)
		if err != nil {
			return Result{}, err
		}
		if err := slackDo(ctx, req, nil); err != nil {
			return Result{}, slackLost(err)
		}
		// The answer names the files, not the message: find the message the share made.
		ts = s.sharedTS(ctx, token, files[0]["id"], channel)
	}
	if ts == "" {
		// Published, but Slack has not said where yet: the post links to the channel and takes no replies.
		return Result{URL: slackChannelURL(c["team_url"], channel)}, nil
	}
	res := Result{ExternalID: ts, URL: slackMessageURL(c["team_url"], channel, ts, threadTS)}
	if threadTS != "" {
		res.ExternalID = threadTS + "/" + ts
	}
	return res, nil
}

// slackLost: Slack has no idempotency key, so posting again after an answer that never came could post twice.
func slackLost(err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return err
	}
	return &Failure{Kind: ErrRejected, Msg: "Slack may have published the post, but its answer was lost (" + err.Error() + "); check the channel before posting it again"}
}

func slackMessageURL(teamURL, channel, ts, threadTS string) string {
	base := slackChannelURL(teamURL, channel)
	if base == "" {
		return ""
	}
	u := base + "/p" + strings.Replace(ts, ".", "", 1)
	if threadTS != "" {
		u += "?thread_ts=" + threadTS + "&cid=" + channel
	}
	return u
}

// upload sends one file's bytes to the address Slack gives for it and returns the file's id.
func (Slack) upload(ctx context.Context, token string, m MediaFile) (string, error) {
	f, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("Slack: cannot read %s: %w", m.Name, err)
	}
	defer f.Close()
	var data io.Reader = f
	size := m.Size
	if size <= 0 { // Slack needs the exact length up front
		b, err := io.ReadAll(io.LimitReader(f, maxMediaRead+1))
		if err == nil && int64(len(b)) > maxMediaRead {
			err = errors.New("too large to send")
		}
		if err != nil {
			return "", fmt.Errorf("Slack: cannot read %s: %w", m.Name, err)
		}
		data, size = bytes.NewReader(b), int64(len(b))
	}
	var slot struct {
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	form := url.Values{"filename": {m.Name}, "length": {strconv.FormatInt(size, 10)}}
	req, err := checkForm(ctx, slackAPI+"/files.getUploadURLExternal", form)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if err := slackDo(ctx, req, &slot); err != nil {
		return "", err
	}
	if slot.UploadURL == "" || slot.FileID == "" {
		return "", errors.New("Slack gave no address to upload " + m.Name + " to")
	}
	up, err := http.NewRequestWithContext(ctx, http.MethodPost, slot.UploadURL, data)
	if err != nil {
		return "", err
	}
	up.ContentLength = size
	up.Header.Set("Content-Type", "application/octet-stream")
	resp, err := httpClient(ctx).Do(up)
	if err != nil {
		return "", fmt.Errorf("Slack could not be reached (%s)", up.URL.Host)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return "", &Failure{Msg: fmt.Sprintf("Slack could not take %s (%d); staqly tries again in a few minutes", m.Name, resp.StatusCode)}
		}
		return "", &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("Slack refused %s (%d)", m.Name, resp.StatusCode)}
	}
	return slot.FileID, nil
}

// sharedTS waits until Slack reports the message that shared the file in the channel ("" when it does not say).
func (Slack) sharedTS(ctx context.Context, token, fileID, channel string) string {
	for i := 0; i < slackPolls; i++ {
		var info struct {
			File struct {
				Shares map[string]map[string][]struct {
					TS string `json:"ts"`
				} `json:"shares"` // public|private -> channel -> shares
			} `json:"file"`
		}
		req, err := slackRequest(ctx, token, "files.info?"+url.Values{"file": {fileID}}.Encode(), nil)
		if err != nil {
			return ""
		}
		if slackDo(ctx, req, &info) == nil {
			for _, kind := range []string{"public", "private"} {
				if s := info.File.Shares[kind][channel]; len(s) > 0 && s[0].TS != "" {
					return s[0].TS
				}
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(slackPoll):
		}
	}
	return ""
}

// slackRequest builds a Web API call: a GET for a method with its query, a JSON POST when body is set, a bare
// POST otherwise.
func slackRequest(ctx context.Context, token, method string, body map[string]any) (*http.Request, error) {
	verb, rd := http.MethodGet, io.Reader(nil)
	if body != nil {
		b, _ := json.Marshal(body)
		verb, rd = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, verb, slackAPI+"/"+method, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	return req, nil
}

// slackDo sends a request and decodes the reply into out. Slack answers 200 with {"ok":false,"error":"..."} for
// most failures, 429 with Retry-After for rate limits.
func slackDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Slack could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		return &Failure{Msg: "Slack rate limit reached; staqly tries again in a few minutes"}
	}
	if resp.StatusCode >= 500 {
		return &Failure{Msg: fmt.Sprintf("Slack is unavailable (%d); staqly tries again in a few minutes", resp.StatusCode)}
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return fmt.Errorf("Slack answered %d without a readable reply", resp.StatusCode)
	}
	if !env.OK {
		return slackError(env.Error)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// slackError maps Slack's error codes to what staqly does next: a sign-in problem needs reconnecting, a refused
// message is final, the rest is retried.
func slackError(code string) error {
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "Slack: " + msg} }
	switch code {
	case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "invalid_refresh_token",
		"app_uninstalled", "team_access_not_granted":
		return &Failure{Kind: ErrReauth, Msg: "Slack no longer accepts the app's sign-in for this workspace (" + code + "); reconnect the channel"}
	case "missing_scope", "no_permission", "not_allowed_token_type":
		return &Failure{Kind: ErrReauth, Msg: "Slack: the app is missing a permission (" + code + "); reconnect the channel to grant it"}
	case "channel_not_found", "not_in_channel", "is_archived":
		return &Failure{Kind: ErrReauth, Msg: "Slack: the app can no longer post in this channel (" + code + "); invite it back or unarchive the channel, then reconnect"}
	case "ratelimited", "rate_limited":
		return &Failure{Msg: "Slack rate limit reached; staqly tries again in a few minutes"}
	case "internal_error", "fatal_error", "service_unavailable", "request_timeout":
		return &Failure{Msg: "Slack is unavailable (" + code + "); staqly tries again in a few minutes"}
	case "invalid_code", "code_already_used":
		return rejected("the sign-in expired or was used already; connect again")
	case "bad_redirect_uri":
		// For whoever runs the server: the app's OAuth & Permissions page needs <dashboard>/channels/callback/slack.
		return rejected("the app does not list this server's return address; connect again once it does")
	case "invalid_client_id", "bad_client_secret":
		// For whoever runs the server: PROVIDER_SLACK_CLIENT_ID and _SECRET are on the app's Basic Information page.
		return rejected("this server's Slack app keys are not accepted (" + code + ")")
	case "msg_too_long":
		return rejected("the message is too long")
	case "no_text":
		return rejected("the message is empty")
	case "restricted_action", "restricted_action_read_only_channel", "restricted_action_thread_only_channel",
		"restricted_action_non_threadable_channel", "ekm_access_denied", "posting_to_general_channel_denied":
		return rejected("the workspace does not let the app post here (" + code + ")")
	case "file_uploads_disabled", "file_upload_size_restricted", "file_uploads_except_images_disabled":
		return rejected("the workspace does not take this file (" + code + ")")
	case "thread_reply_not_allowed", "cannot_reply_to_message":
		return rejected("Slack does not take a reply under this message (" + code + ")")
	}
	return rejected("Slack refused the request (" + code + ")")
}

// Slack checks the app's keys before the sign-in code: a made-up code with good keys is "invalid_code".
func (Slack) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, slackAPI+"/oauth.v2.access", url.Values{"client_id": {app.ClientID},
		"client_secret": {app.ClientSecret}, "code": {checkCode}, "redirect_uri": {redirect}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Slack")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client_id"), strings.Contains(body, "bad_client_secret"):
		return false, errors.New("Slack did not accept the app keys: check the Client ID and Client Secret on the app's Basic Information page")
	case strings.Contains(body, "bad_redirect_uri"):
		return false, errors.New("Slack took the app keys but not the return address: add " + redirect + " under OAuth & Permissions > Redirect URLs")
	case strings.Contains(body, "invalid_code"), strings.Contains(body, "code_already_used"):
		return true, nil
	}
	return false, unexpected("Slack", status, body)
}
