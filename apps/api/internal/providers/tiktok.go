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
	"strings"
	"time"
)

// TikTok over the Content Posting API (v2): Login Kit OAuth 2.0 (access tokens for a day, refresh tokens for a
// year), then a direct post of one video: TikTok is asked what this creator may post (creator_info), the post is
// initialized with its settings, the bytes are uploaded in chunks (FILE_UPLOAD, no public link needed) and staqly
// waits until TikTok reports it published. Until TikTok audits the app, it publishes only as "Only you".
type TikTok struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	tiktokAPI       = "https://open.tiktokapis.com"
	tiktokAuthorize = "https://www.tiktok.com/v2/auth/authorize/"
	// tiktokPoll is how long to wait between checks of a post TikTok is still processing.
	tiktokPoll = 5 * time.Second
)

const (
	// user.info.profile gives the username (the channel's handle); video.publish posts directly. No video.upload:
	// staqly publishes, it does not leave drafts in the TikTok inbox.
	tiktokScopes = "user.info.basic,user.info.profile,video.publish"
	// TikTok's chunks: 5 to 64 MB, a file up to 64 MB in one; larger files in 10 MB chunks, the last one taking
	// the rest (up to 128 MB).
	tiktokSingleChunk = 64 << 20
	tiktokChunk       = 10 << 20
)

func init() {
	l := Limits{MaxChars: 2200, MaxMedia: 1, MediaKinds: []string{"video"}, NeedsMedia: true}
	Register(TikTok{NewBase("tiktok", "TikTok", AuthOAuth2, l).WithSettings(soonSettings("tiktok", l)...)}, StatusAvailable)
}

var tiktokPrivacyLabels = map[string]string{
	"PUBLIC_TO_EVERYONE":    "Everyone",
	"MUTUAL_FOLLOW_FRIENDS": "Friends",
	"FOLLOWER_OF_CREATOR":   "Followers",
	"SELF_ONLY":             "Only you",
}

func (t TikTok) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(t.Limits(), p, s); err != nil {
		return err
	}
	for _, m := range p.Media {
		if !strings.HasPrefix(m.MimeType, "video/") {
			return invalid("media", "TikTok takes one video (MP4, WebM or MOV)")
		}
	}
	if s["branded_content"] == "true" && s["privacy_level"] == "SELF_ONLY" {
		return invalid("privacy_level", "TikTok does not allow a paid partnership to be seen by only you; choose who can watch")
	}
	return CheckFields(t.SettingsFields(), s)
}

// AuthURL: TikTok's web sign-in (no PKCE: TikTok asks for it from desktop and mobile apps only).
func (TikTok) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_key":    {app.ClientID},
		"response_type": {"code"},
		"scope":         {tiktokScopes},
		"redirect_uri":  {redirect},
		"state":         {state},
	}
	return tiktokAuthorize + "?" + q.Encode(), nil
}

// token calls TikTok's token endpoint, which names the app in the form and answers refusals as {"error": ...}.
func (TikTok) token(ctx context.Context, app App, form url.Values) (refreshableToken, error) {
	var t refreshableToken
	form.Set("client_key", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tiktokAPI+"/v2/oauth/token/", strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := tiktokDo(ctx, req, &t); err != nil && t.Error == "" {
		return t, err
	}
	switch {
	case t.Error == "invalid_client":
		// For whoever runs the server: PROVIDER_TIKTOK_CLIENT_ID and _SECRET are the app's Client key and secret.
		return t, &Failure{Kind: ErrRejected, Msg: "TikTok did not accept this server's app keys"}
	case t.Error == "invalid_grant", t.Error == "access_denied":
		return t, &Failure{Kind: ErrReauth, Msg: "TikTok no longer accepts this sign-in (" + t.ErrorDescription + "); reconnect the account"}
	case t.Error != "":
		return t, &Failure{Kind: ErrRejected, Msg: "TikTok refused the sign-in (" + t.Error + ": " + t.ErrorDescription + ")"}
	case t.AccessToken == "":
		return t, errors.New("TikTok returned no access token")
	}
	return t, nil
}

func (t TikTok) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	tok, err := t.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	if tok.Scope != "" && !hasScope(tok.Scope, "video.publish") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "TikTok did not allow posting videos: allow it when you connect, then try again"}
	}
	var me struct {
		Data struct {
			User struct {
				OpenID      string `json:"open_id"`
				DisplayName string `json:"display_name"`
				Username    string `json:"username"`
				AvatarURL   string `json:"avatar_url"`
				DeepLink    string `json:"profile_deep_link"`
			} `json:"user"`
		} `json:"data"`
	}
	fields := "open_id,display_name,avatar_url"
	if hasScope(tok.Scope, "user.info.profile") || tok.Scope == "" {
		fields += ",username,profile_deep_link"
	}
	req, err := tiktokRequest(ctx, http.MethodGet, "/v2/user/info/?fields="+fields, tok.AccessToken, nil)
	if err != nil {
		return nil, Profile{}, err
	}
	if err := tiktokDo(ctx, req, &me); err != nil {
		return nil, Profile{}, err
	}
	u := me.Data.User
	id := u.OpenID
	if id == "" {
		id = tok.OpenID
	}
	if id == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "TikTok did not say which account signed in"}
	}
	creds := tok.into(Credentials{"open_id": id, "username": u.Username}, time.Now())
	profile := Profile{Name: u.DisplayName, AvatarURL: u.AvatarURL, Handle: "@" + u.Username, ProfileURL: "https://www.tiktok.com/@" + u.Username}
	if u.Username == "" {
		// Without the username (user.info.profile not allowed), the account's id is what stays the same.
		profile.Handle, profile.ProfileURL = id, u.DeepLink
	}
	if profile.Name == "" {
		profile.Name = profile.Handle
	}
	return creds, profile, nil
}

// Refresh renews the day-long access token with the year-long refresh token (TikTok may hand out a new one).
func (t TikTok) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "TikTok: no lasting sign-in stored; reconnect the account"}
	}
	tok, err := t.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return tok.into(c, time.Now()), nil
}

// TikTok hands an app token for its keys alone (client_credentials): issued means the keys are right.
func (TikTok) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	req, err := checkForm(ctx, tiktokAPI+"/v2/oauth/token/", url.Values{"grant_type": {"client_credentials"},
		"client_key": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "TikTok")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client"):
		return false, errors.New("TikTok did not accept the app keys: check the Client key and Client secret on the app's page")
	case strings.Contains(body, `"access_token"`):
		return true, nil
	}
	return false, unexpected("TikTok", status, body)
}

// TikTok calls back over https only.
func (TikTok) Needs() Needs { return Needs{HTTPS: true} }

type tiktokCreator struct {
	Username        string   `json:"creator_username"`
	PrivacyOptions  []string `json:"privacy_level_options"`
	CommentDisabled bool     `json:"comment_disabled"`
	DuetDisabled    bool     `json:"duet_disabled"`
	StitchDisabled  bool     `json:"stitch_disabled"`
	MaxDurationSec  int      `json:"max_video_post_duration_sec"`
}

func (t TikTok) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "TikTok: no account stored; reconnect the account"}
	}
	if len(p.Media) != 1 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "TikTok: a post is one video"}
	}
	m := p.Media[0]

	// TikTok asks apps to check what the creator may post right before posting.
	var info struct {
		Data tiktokCreator `json:"data"`
	}
	req, err := tiktokRequest(ctx, http.MethodPost, "/v2/post/publish/creator_info/query/", token, nil)
	if err != nil {
		return Result{}, err
	}
	if err := tiktokDo(ctx, req, &info); err != nil {
		return Result{}, err
	}
	cr := info.Data
	privacy := s["privacy_level"]
	if len(cr.PrivacyOptions) > 0 && !containsString(cr.PrivacyOptions, privacy) {
		var allowed []string
		for _, o := range cr.PrivacyOptions {
			if l := tiktokPrivacyLabels[o]; l != "" {
				allowed = append(allowed, l)
			} else {
				allowed = append(allowed, o)
			}
		}
		msg := "TikTok lets this account post only as: " + strings.Join(allowed, ", ") + "; change Who can watch"
		if len(cr.PrivacyOptions) == 1 && cr.PrivacyOptions[0] == "SELF_ONLY" {
			msg += " (an app TikTok has not audited yet, or a private account, posts only as Only you)"
		}
		return Result{}, &Failure{Kind: ErrRejected, Msg: msg}
	}

	size := m.Size
	var whole []byte
	if size <= 0 {
		if whole, err = readMedia(ctx, m, 0); err != nil {
			return Result{}, fmt.Errorf("TikTok: %w", err)
		}
		size = int64(len(whole))
	}
	chunk, count := size, int64(1)
	if size > tiktokSingleChunk {
		chunk, count = tiktokChunk, size/tiktokChunk
	}
	postInfo := map[string]any{
		"title":           p.Text,
		"privacy_level":   privacy,
		"disable_comment": s["allow_comments"] == "false" || cr.CommentDisabled,
		"disable_duet":    s["allow_duet"] == "false" || cr.DuetDisabled,
		"disable_stitch":  s["allow_stitch"] == "false" || cr.StitchDisabled,
		"is_aigc":         s["ai_generated"] == "true",
		// Commercial content disclosure: a third party's paid partnership, or the creator's own business.
		"brand_content_toggle": s["branded_content"] == "true",
		"brand_organic_toggle": s["your_brand"] == "true",
	}
	b, _ := json.Marshal(map[string]any{
		"post_info":   postInfo,
		"source_info": map[string]any{"source": "FILE_UPLOAD", "video_size": size, "chunk_size": chunk, "total_chunk_count": count},
	})
	req, err = tiktokRequest(ctx, http.MethodPost, "/v2/post/publish/video/init/", token, b)
	if err != nil {
		return Result{}, err
	}
	var started struct {
		Data struct {
			PublishID string `json:"publish_id"`
			UploadURL string `json:"upload_url"`
		} `json:"data"`
	}
	// Nothing is published before the bytes arrive: an init whose answer was lost is safe to try again.
	if err := tiktokDo(ctx, req, &started); err != nil {
		return Result{}, err
	}
	if started.Data.PublishID == "" || started.Data.UploadURL == "" {
		return Result{}, &Failure{Msg: "TikTok gave no place to upload the video; staqly tries again in a few minutes"}
	}

	sent, err := t.upload(ctx, started.Data.UploadURL, m, whole, size, chunk, count)
	if err != nil && !sent {
		return Result{}, err
	}
	// Once the last chunk went out TikTok publishes on its own: from here a retry could post twice.
	user := c["username"]
	if user == "" {
		user = cr.Username
	}
	return t.wait(ctx, token, started.Data.PublishID, user)
}

// upload sends the video in its chunks. sent is true once the last chunk left (even if its answer was lost), after
// which TikTok may publish whatever staqly does next.
func (TikTok) upload(ctx context.Context, uploadURL string, m MediaFile, whole []byte, size, chunk, count int64) (sent bool, err error) {
	var src io.Reader
	if whole != nil {
		src = bytes.NewReader(whole)
	} else {
		f, err := m.Reader(ctx)
		if err != nil {
			return false, fmt.Errorf("TikTok: cannot read %s: %w", m.Name, err)
		}
		defer f.Close()
		src = f
	}
	ct := m.MimeType
	if ct == "" {
		ct = "video/mp4"
	}
	for i := int64(0); i < count; i++ {
		start := i * chunk
		end := start + chunk - 1
		if i == count-1 {
			end = size - 1
		}
		n := end - start + 1
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, io.LimitReader(src, n))
		if err != nil {
			return false, err
		}
		req.ContentLength = n
		req.Header.Set("Content-Type", ct)
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		resp, err := httpClient(ctx).Do(req)
		last := i == count-1
		if err != nil {
			if last {
				return true, err
			}
			return false, &Failure{Msg: "TikTok's upload could not be reached; staqly tries again in a few minutes"}
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return false, tiktokError(resp.StatusCode, raw)
		}
	}
	return true, nil
}

// wait polls the post's status until TikTok has published it or says why not. The job's deadline ends a wait that
// never finishes; since the video is uploaded by then, that is reported as "may have published", not retried.
func (TikTok) wait(ctx context.Context, token, publishID, user string) (Result, error) {
	profile := "https://www.tiktok.com/"
	if user != "" {
		profile = "https://www.tiktok.com/@" + user
	}
	mayHave := func(why string) error {
		return &Failure{Kind: ErrRejected, Msg: "TikTok may have published the post, but " + why + "; check the profile before posting it again"}
	}
	for {
		var st struct {
			Data struct {
				Status     string        `json:"status"`
				FailReason string        `json:"fail_reason"`
				PostIDs    []json.Number `json:"publicaly_available_post_id"` // TikTok's spelling
			} `json:"data"`
		}
		b, _ := json.Marshal(map[string]string{"publish_id": publishID})
		req, err := tiktokRequest(ctx, http.MethodPost, "/v2/post/publish/status/fetch/", token, b)
		if err != nil {
			return Result{}, err
		}
		err = tiktokDo(ctx, req, &st)
		var f *Failure
		switch {
		case err != nil && errors.As(err, &f) && f.Kind == ErrReauth:
			return Result{}, mayHave("its sign-in ran out while staqly waited for it (" + f.Msg + ")")
		case err != nil:
			// Asking failed this time; the post is on its way regardless: ask again.
		case st.Data.Status == "PUBLISH_COMPLETE":
			if len(st.Data.PostIDs) > 0 && user != "" {
				id := st.Data.PostIDs[0].String()
				return Result{ExternalID: id, URL: profile + "/video/" + id}, nil
			}
			// A post only you can see, or one TikTok is still reviewing, has no public id yet.
			return Result{ExternalID: publishID, URL: profile}, nil
		case st.Data.Status == "SEND_TO_USER_INBOX":
			return Result{ExternalID: publishID, URL: profile}, nil
		case st.Data.Status == "FAILED":
			return Result{}, tiktokFailReason(st.Data.FailReason)
		}
		select {
		case <-ctx.Done():
			return Result{}, mayHave("it was still processing the video when staqly stopped waiting")
		case <-time.After(tiktokPoll):
		}
	}
}

// tiktokFailReason explains why TikTok did not publish an uploaded video.
func tiktokFailReason(reason string) error {
	msg := map[string]string{
		"file_format_check_failed":           "TikTok could not read the video's format; use MP4 (H.264), WebM or MOV",
		"duration_check_failed":              "the video is longer (or shorter) than this account may post",
		"frame_rate_check_failed":            "TikTok takes videos at 23 to 60 frames per second",
		"picture_size_check_failed":          "TikTok takes videos at least 360 pixels on each side, at most 4096",
		"internal":                           "TikTok had a problem processing the video",
		"video_pull_failed":                  "TikTok could not fetch the video",
		"publish_cancelled":                  "the post was cancelled",
		"auth_removed":                       "the account removed staqly's access",
		"spam_risk_too_many_posts":           "this account reached TikTok's daily post limit; schedule it for tomorrow",
		"spam_risk_user_banned_from_posting": "TikTok does not let this account post right now",
		"spam_risk_text":                     "TikTok flagged the caption as possible spam",
		"spam_risk":                          "TikTok flagged the post as possible spam",
	}[reason]
	if msg == "" {
		msg = "TikTok did not publish it (" + reason + ")"
	} else if !strings.HasPrefix(msg, "TikTok") {
		msg = "TikTok did not publish it: " + msg
	}
	if reason == "auth_removed" {
		return &Failure{Kind: ErrReauth, Msg: msg + "; reconnect it"}
	}
	return &Failure{Kind: ErrRejected, Msg: msg}
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// tiktokRequest builds an API call with the account's token and a JSON body (nil for none).
func tiktokRequest(ctx context.Context, method, path, token string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, tiktokAPI+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	return req, nil
}

// tiktokDo sends a request and decodes the reply into out (also on errors, for the token endpoint's "error");
// TikTok's errors become messages a person can act on. Its API answers {"data":{...},"error":{"code":"ok"}}.
func tiktokDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("TikTok could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && (e.Error.Code == "" || e.Error.Code == "ok") {
		return nil
	}
	return tiktokError(resp.StatusCode, raw)
}

// tiktokError maps TikTok's error codes to what staqly does next.
func tiktokError(status int, raw []byte) error {
	var e struct {
		Error any `json:"error"` // {"code","message","log_id"} from the API; a string from the token endpoint
	}
	_ = json.Unmarshal(raw, &e)
	code, detail := "", ""
	switch v := e.Error.(type) {
	case map[string]any:
		code, _ = v["code"].(string)
		detail, _ = v["message"].(string)
	case string:
		code = v
	}
	if detail == "" {
		detail = code
	}
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "TikTok: " + msg} }
	switch {
	case code == "access_token_invalid", status == http.StatusUnauthorized && code != "scope_not_authorized":
		return &Failure{Kind: ErrReauth, Msg: "TikTok no longer accepts this account's sign-in; reconnect it"}
	case code == "scope_not_authorized", code == "scope_permission_missed":
		return &Failure{Kind: ErrReauth, Msg: "TikTok: the account did not allow posting videos; reconnect it and allow it"}
	case code == "rate_limit_exceeded", status == http.StatusTooManyRequests:
		return &Failure{Msg: "TikTok rate limit reached; staqly tries again in a few minutes"}
	case code == "internal_error", status >= 500:
		return &Failure{Msg: fmt.Sprintf("TikTok is unavailable (%s); staqly tries again in a few minutes", detail)}
	case code == "unaudited_client_can_only_post_to_private_accounts":
		return rejected("until TikTok audits this app, it posts only as Only you; change Who can watch")
	case code == "privacy_level_option_mismatch":
		return rejected("this account may not post with that choice of Who can watch; choose another")
	case code == "spam_risk_too_many_posts":
		return rejected("this account reached TikTok's daily post limit; schedule it for tomorrow")
	case code == "spam_risk_user_banned_from_posting":
		return rejected("TikTok does not let this account post right now")
	case code == "reached_active_user_cap":
		return rejected("the app reached TikTok's daily limit of accounts posting through it; try again tomorrow")
	case strings.HasPrefix(code, "spam_risk"):
		return rejected("TikTok flagged the post as possible spam (" + detail + ")")
	case code == "url_ownership_unverified", code == "invalid_file_upload", code == "invalid_params":
		return rejected(detail)
	}
	return rejected(detail)
}
