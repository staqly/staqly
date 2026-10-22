package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// X (x.com) over API v2: OAuth 2.0 with PKCE (the app layer passes the verifier as extra["code_verifier"]),
// media through the v2 media upload endpoints, posts through POST /2/tweets. Access tokens last two hours;
// the refresh token renews them (offline.access).
type X struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	xAPI       = "https://api.x.com"
	xAuthorize = "https://x.com/i/oauth2/authorize"
	// xPoll is how long to wait between video processing checks.
	xPoll = 2 * time.Second
)

const (
	xScopes    = "tweet.read tweet.write users.read offline.access media.write"
	xChunkSize = 1 << 20 // 1 MiB per APPEND, as X documents
)

func init() {
	Register(X{NewBase("x", "X", AuthOAuth2, Limits{MaxChars: 280, MaxMedia: 4, MediaKinds: []string{"image", "gif", "video"},
		// The X API bills a post with a link at a higher rate and X shows it to fewer people: links never reach
		// X as links. They are removed, or kept as readable text (example[.]com) if the channel says so.
		Links: LinksRemove, LinkModes: []string{LinksRemove, LinksDefang}, XWeighted: true})}, StatusAvailable)
}

func (X) SettingsFields() []Field {
	return []Field{
		{Key: "reply_settings", Label: "Who can reply", Kind: "select", Default: "everyone",
			Options: []Option{
				{Value: "everyone", Label: "Everyone"},
				{Value: "following", Label: "Accounts you follow"},
				{Value: "mentionedUsers", Label: "Only accounts you mention"},
				{Value: "subscribers", Label: "Your subscribers"},
				{Value: "verified", Label: "Verified accounts"},
			}},
		{Key: "community", Label: "Community", Kind: "url", Placeholder: "https://x.com/i/communities/1493446837214187523",
			Help: "Post into an X Community you belong to instead of your timeline."},
		{Key: "made_with_ai", Label: "Made with AI", Kind: "boolean", Default: "false",
			Help: "X labels the post as containing AI-generated media."},
		{Key: "paid_partnership", Label: "Paid partnership", Kind: "boolean", Default: "false",
			Help: "X labels the post as a paid partnership."},
		{Key: "thread_finisher", Label: "Last comment", Kind: "textarea", MaxLength: 280,
			Placeholder: "Thanks for reading. Follow for more.",
			Help:        "Added as the final comment under the post, after its other comments."},
		{Key: "reposters", Label: "Repost from", Kind: "channels", MaxItems: 10,
			Help: "Your other X accounts repost it right after it goes out."},
	}
}

// Reposter is implemented by platforms where another connected account can repost a published post.
var _ Reposter = X{}

// Repost reposts the post externalID as the account of c (POST /2/users/{id}/retweets).
func (x X) Repost(ctx context.Context, c Credentials, externalID string) error {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" {
		return &Failure{Kind: ErrReauth, Msg: "X: no account stored; reconnect the account"}
	}
	b, _ := json.Marshal(map[string]string{"tweet_id": externalID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xAPI+"/2/users/"+user+"/retweets", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return xDo(ctx, req, nil)
}

func (x X) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(x.Limits(), p, s); err != nil {
		return err
	}
	videos := 0
	for _, m := range p.Media {
		if strings.HasPrefix(m.MimeType, "video/") {
			videos++
		}
	}
	if videos > 0 && len(p.Media) > 1 {
		return invalid("media", "X takes up to 4 pictures or one video")
	}
	if c := s["community"]; c != "" && xCommunityID(c) == "" {
		return invalid("community", "use a community link like https://x.com/i/communities/1493446837214187523")
	}
	switch s["reply_settings"] {
	case "", "everyone", "following", "mentionedUsers", "subscribers", "verified":
	default:
		return invalid("reply_settings", "choose who can reply")
	}
	return CheckFields(x.SettingsFields(), s)
}

func xCommunityID(v string) string {
	v = strings.TrimRight(strings.TrimSpace(v), "/")
	id := v[strings.LastIndex(v, "/")+1:]
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return ""
	}
	return id
}

func (X) AuthURL(app App, redirect, state string, extra map[string]string) (string, error) {
	verifier := extra["code_verifier"]
	if verifier == "" {
		return "", errors.New("X needs a PKCE code verifier")
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {app.ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {xScopes},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	return xAuthorize + "?" + q.Encode(), nil
}

type xToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (X) token(ctx context.Context, app App, form url.Values) (xToken, error) {
	var t xToken
	if app.ClientSecret == "" {
		form.Set("client_id", app.ClientID) // a public client names itself; a confidential one authenticates below
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xAPI+"/2/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if app.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret))
	}
	if err := xDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("X returned no access token")
	}
	return t, nil
}

func (t xToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" { // X rotates refresh tokens; keep the old one only when no new one came
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

func (x X) Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	t, err := x.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {extra["code_verifier"]}})
	if err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	profile, err := x.me(ctx, creds["access_token"])
	if err != nil {
		return nil, Profile{}, err
	}
	creds["user_id"], creds["username"] = profile.userID, profile.username
	return creds, profile.Profile, nil
}

func (x X) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "X gave no refresh token; reconnect the account"}
	}
	t, err := x.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

type xProfile struct {
	Profile
	userID, username string
}

func (X) me(ctx context.Context, token string) (xProfile, error) {
	var out struct {
		Data struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Username        string `json:"username"`
			ProfileImageURL string `json:"profile_image_url"`
		} `json:"data"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xAPI+"/2/users/me?user.fields=profile_image_url", nil)
	if err != nil {
		return xProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if err := xDo(ctx, req, &out); err != nil {
		return xProfile{}, err
	}
	d := out.Data
	return xProfile{Profile: Profile{Name: d.Name, Handle: "@" + d.Username,
		AvatarURL: strings.Replace(d.ProfileImageURL, "_normal.", "_400x400.", 1), ProfileURL: "https://x.com/" + d.Username},
		userID: d.ID, username: d.Username}, nil
}

// X publishes a post's thread as replies.
var _ Replier = X{}

// X reports a post's public numbers (tweet.read is enough; impressions included for the account's own posts).
var _ MetricsReader = X{}

func (X) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	var out struct {
		Data struct {
			ID            string `json:"id"`
			PublicMetrics struct {
				Impressions int64 `json:"impression_count"`
				Likes       int64 `json:"like_count"`
				Reposts     int64 `json:"retweet_count"`
				Replies     int64 `json:"reply_count"`
				Quotes      int64 `json:"quote_count"`
				Bookmarks   int64 `json:"bookmark_count"`
			} `json:"public_metrics"`
		} `json:"data"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		xAPI+"/2/tweets/"+url.PathEscape(externalID)+"?tweet.fields=public_metrics", nil)
	if err != nil {
		return Metrics{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c["access_token"])
	if err := xDo(ctx, req, &out); err != nil {
		return Metrics{}, err
	}
	if out.Data.ID == "" { // X answers 200 with only an errors list for a post that is gone
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "X: the post is not there any more"}
	}
	m := out.Data.PublicMetrics
	return Metrics{Impressions: &m.Impressions, Likes: &m.Likes, Reposts: &m.Reposts, Replies: &m.Replies,
		Quotes: &m.Quotes, Bookmarks: &m.Bookmarks}, nil
}

func (x X) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	return x.tweet(ctx, c, p, s, "")
}

// Reply posts a thread part under the tweet parentExternalID (the post, or the part before).
func (x X) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, s Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "X: nothing to reply to (the post has no id)"}
	}
	return x.tweet(ctx, c, p, s, parentExternalID)
}

// tweet posts one tweet: a new one, or a reply when replyTo is set. A reply takes the conversation's
// reply settings and community, so only the post sends them.
func (x X) tweet(ctx context.Context, c Credentials, p PostContent, s Settings, replyTo string) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "X: no access token stored; reconnect the account"}
	}
	var ids []string
	for _, m := range p.Media {
		id, err := x.upload(ctx, token, m)
		if err != nil {
			return Result{}, err
		}
		ids = append(ids, id)
	}
	body := map[string]any{"text": p.Text}
	if len(ids) > 0 {
		body["media"] = map[string]any{"media_ids": ids}
	}
	if replyTo != "" {
		body["reply"] = map[string]any{"in_reply_to_tweet_id": replyTo}
	} else {
		if r := s["reply_settings"]; r != "" && r != "everyone" {
			body["reply_settings"] = r
		}
		if id := xCommunityID(s["community"]); id != "" {
			body["community_id"] = id
		}
		// Disclosures X shows on the post (not on its replies).
		if s["made_with_ai"] == "true" {
			body["made_with_ai"] = true
		}
		if s["paid_partnership"] == "true" {
			body["paid_partnership"] = true
		}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xAPI+"/2/tweets", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := xDo(ctx, req, &out); err != nil {
		// A retry after a lost answer (a restart, the job's time ran out) is refused as a duplicate: when the
		// account's latest posts hold this text, that is the post, published.
		if !strings.Contains(err.Error(), xDuplicateMsg) {
			return Result{}, err
		}
		id := x.recentWithText(ctx, token, c["user_id"], p.Text)
		if id == "" {
			return Result{}, err
		}
		out.Data.ID = id
	}
	user := c["username"]
	if user == "" {
		user = "i/web"
	}
	return Result{ExternalID: out.Data.ID, URL: "https://x.com/" + user + "/status/" + out.Data.ID}, nil
}

const xDuplicateMsg = "this text was already posted"

var xTrailingLink = regexp.MustCompile(`(\s*https://t\.co/\S+)+$`)

// recentWithText finds the account's latest post with this text ("" when none or X can't be asked). X writes
// &, < and > as entities and appends a t.co link for attached media.
func (X) recentWithText(ctx context.Context, token, userID, text string) string {
	if userID == "" {
		return ""
	}
	var out struct {
		Data []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xAPI+"/2/users/"+url.PathEscape(userID)+"/tweets?max_results=5", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if xDo(ctx, req, &out) != nil {
		return ""
	}
	want := strings.TrimSpace(text)
	for _, t := range out.Data {
		if strings.TrimSpace(xTrailingLink.ReplaceAllString(html.UnescapeString(t.Text), "")) == want {
			return t.ID
		}
	}
	return ""
}

// upload sends one file: pictures in a single request, videos (and GIFs) in 1 MiB chunks, then waits
// until X has processed them.
func (x X) upload(ctx context.Context, token string, m MediaFile) (string, error) {
	f, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("X: cannot read %s: %w", m.Name, err)
	}
	defer f.Close()
	video := strings.HasPrefix(m.MimeType, "video/")
	if !video && m.MimeType != "image/gif" {
		var out struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		req, err := xMultipart(ctx, token, xAPI+"/2/media/upload", map[string]string{"media_category": "tweet_image", "media_type": m.MimeType}, m.Name, f)
		if err != nil {
			return "", err
		}
		if err := xDo(ctx, req, &out); err != nil {
			return "", err
		}
		return out.Data.ID, nil
	}
	category := "tweet_gif"
	if video {
		category = "tweet_video"
	}
	var initOut struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	b, _ := json.Marshal(map[string]any{"media_type": m.MimeType, "total_bytes": m.Size, "media_category": category})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xAPI+"/2/media/upload/initialize", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if err := xDo(ctx, req, &initOut); err != nil {
		return "", err
	}
	id := initOut.Data.ID
	buf := make([]byte, xChunkSize)
	for seg := 0; ; seg++ {
		n, rerr := io.ReadFull(f, buf)
		if n > 0 {
			req, err := xMultipart(ctx, token, xAPI+"/2/media/upload/"+id+"/append", map[string]string{"segment_index": strconv.Itoa(seg)}, m.Name, bytes.NewReader(buf[:n]))
			if err != nil {
				return "", err
			}
			if err := xDo(ctx, req, nil); err != nil {
				return "", err
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	type processing struct {
		State          string `json:"state"`
		CheckAfterSecs int    `json:"check_after_secs"`
		Error          struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	var fin struct {
		Data struct {
			ProcessingInfo *processing `json:"processing_info"`
		} `json:"data"`
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, xAPI+"/2/media/upload/"+id+"/finalize", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if err := xDo(ctx, req, &fin); err != nil {
		return "", err
	}
	info := fin.Data.ProcessingInfo
	for info != nil && (info.State == "pending" || info.State == "in_progress") {
		wait := time.Duration(info.CheckAfterSecs) * time.Second
		if wait <= 0 || wait > 30*time.Second {
			wait = xPoll
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
		var st struct {
			Data struct {
				ProcessingInfo *processing `json:"processing_info"`
			} `json:"data"`
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, xAPI+"/2/media/upload?command=STATUS&media_id="+url.QueryEscape(id), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if err := xDo(ctx, req, &st); err != nil {
			return "", err
		}
		info = st.Data.ProcessingInfo
	}
	if info != nil && info.State == "failed" {
		return "", &Failure{Kind: ErrRejected, Msg: "X could not process " + m.Name + ": " + info.Error.Message}
	}
	return id, nil
}

func xMultipart(ctx context.Context, token, endpoint string, fields map[string]string, name string, r io.Reader) (*http.Request, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	part, err := w.CreateFormFile("media", name)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, r); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req, nil
}

// xDo sends a request and decodes a 2xx reply into out; X's errors become messages a person can act on.
func xDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("X could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	return xError(resp.StatusCode, string(raw))
}

// xError maps X's replies (status and the problem text) to what staqly does next: a sign-in problem needs
// reconnecting, a refused post is final, the rest is retried.
func xError(status int, body string) error {
	has := func(s string) bool { return strings.Contains(body, s) }
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "X: " + msg} }
	switch {
	case status == http.StatusUnauthorized, has("Unsupported Authentication"), has(`"title":"Unauthorized"`), has("invalid_grant"):
		return &Failure{Kind: ErrReauth, Msg: "X no longer accepts this account's sign-in; reconnect it"}
	case status == http.StatusTooManyRequests, has("Too Many Requests"):
		return &Failure{Msg: "X rate limit reached; staqly tries again in a few minutes"}
	case status >= 500, has("Service Unavailable"):
		return &Failure{Msg: fmt.Sprintf("X is unavailable (%d); staqly tries again in a few minutes", status)}
	case has("duplicate"), has("You are not allowed to create a Tweet"):
		return rejected(xDuplicateMsg + "; change it and try again")
	case has("usage-capped"):
		return rejected("the app reached its monthly X API cap")
	case has("user-suspended"):
		return rejected("this X account is suspended")
	case has("temporarily locked"):
		return rejected("this X account is temporarily locked; log in to x.com to unlock it")
	case has("Your media IDs are invalid"):
		return rejected("X rejected the attached media; upload it again")
	case has("maximum of 4 items"):
		return rejected("at most 4 pictures or one video")
	case has("invalid URL"):
		return rejected("the post contains a link X does not allow")
	case has("longer than 2 minutes"), has("longer than 10 minutes"):
		return rejected("the video is longer than this account may post")
	case has("Crypto addresses are prohibited"):
		return rejected("X does not allow crypto addresses in the first 7 days after connecting")
	case has("not permitted"), status == http.StatusForbidden:
		return rejected("X refused the post (" + xDetail(body) + ")")
	case status == http.StatusBadRequest:
		return rejected(xDetail(body))
	}
	return &Failure{Msg: fmt.Sprintf("X answered %d: %s", status, xDetail(body))}
}

// xDetail picks the readable part of an X error body.
func xDetail(body string) string {
	var e struct {
		Detail           string `json:"detail"`
		Title            string `json:"title"`
		ErrorDescription string `json:"error_description"`
		Errors           []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &e) == nil {
		switch {
		case e.Detail != "":
			return e.Detail
		case len(e.Errors) > 0 && e.Errors[0].Message != "":
			return e.Errors[0].Message
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Title != "":
			return e.Title
		}
	}
	if len(body) > 200 {
		return body[:200] + "..."
	}
	return body
}
