package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Reddit over its OAuth API: a permanent sign-in (a refresh token that does not expire, access tokens for an
// hour), posts through POST /api/submit as a text post, a link post or one picture or video uploaded to Reddit's
// media storage first. Every call names the app in its User-Agent, as Reddit asks. Comments are the post's thread.
type Reddit struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	redditWWW   = "https://www.reddit.com"   // sign-in and tokens
	redditOAuth = "https://oauth.reddit.com" // the API
	// redditPoll is how long to wait between looks at the account's posts for a picture or video post, which
	// Reddit creates after it answers; redditLooks is how many looks before staqly stops waiting.
	redditPoll  = 3 * time.Second
	redditLooks = 20
)

const (
	redditScopes = "identity submit read flair"
	// redditUserAgent: Reddit throttles or blocks generic agents and asks for platform:app:version.
	redditUserAgent = "web:staqly:1.0 (+https://staqly.ai)"
)

func init() {
	l := Limits{MaxChars: 40000, MaxMedia: 1, MediaKinds: []string{"image", "video"}, NeedsTitle: true}
	var fields []Field
	for _, f := range soonSettings("reddit", l) {
		fields = append(fields, f)
		if f.Key == "subreddit" {
			fields = append(fields, Field{Key: "link", Label: "Link", Kind: "url", Placeholder: "https://example.com/page",
				Help: "Makes it a link post: the title links here. Link posts take no text or media."})
		}
	}
	Register(Reddit{NewBase("reddit", "Reddit", AuthOAuth2, l).WithSettings(fields...)}, StatusAvailable)
}

var redditSubredditName = regexp.MustCompile(`^[A-Za-z0-9_]{2,21}$`)

// redditSubreddit turns "r/running", "/r/running/" or "https://www.reddit.com/r/running" into "running".
func redditSubreddit(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, "/r/"); i >= 0 {
		v = v[i+3:]
	}
	v = strings.TrimPrefix(strings.TrimPrefix(v, "r/"), "/")
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		v = v[:i]
	}
	return v
}

func (r Reddit) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(r.Limits(), p, s); err != nil {
		return err
	}
	if s["subreddit"] != "" && !redditSubredditName.MatchString(redditSubreddit(s["subreddit"])) {
		return invalid("subreddit", "use the subreddit's name, like r/running")
	}
	for _, m := range p.Media {
		if !strings.HasPrefix(m.MimeType, "image/") && !strings.HasPrefix(m.MimeType, "video/") {
			return invalid("media", "Reddit takes one picture or one video")
		}
	}
	link := strings.TrimSpace(s["link"]) != ""
	if link && len(p.Media) > 0 {
		return invalid("link", "a Reddit post is a link or a picture or video, not both")
	}
	if (link || len(p.Media) > 0) && strings.TrimSpace(p.Text) != "" {
		return invalid("content", "Reddit's link, picture and video posts have a title only: leave the text empty for this channel, or post it as a comment")
	}
	return CheckFields(r.SettingsFields(), s)
}

func (Reddit) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":     {app.ClientID},
		"response_type": {"code"},
		"state":         {state},
		"redirect_uri":  {redirect},
		"duration":      {"permanent"}, // a refresh token, so the channel stays connected
		"scope":         {redditScopes},
	}
	return redditWWW + "/api/v1/authorize?" + q.Encode(), nil
}

// token calls the token endpoint, the app authenticating with its keys (HTTP basic). Reddit answers some refusals
// with 200 and an "error" field.
func (Reddit) token(ctx context.Context, app App, form url.Values) (refreshableToken, error) {
	var t refreshableToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, redditWWW+"/api/v1/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(app.ClientID, app.ClientSecret)
	if err := redditDo(ctx, req, &t); err != nil {
		var f *Failure
		if errors.As(err, &f) && f.Kind == ErrReauth {
			// For whoever runs the server: PROVIDER_REDDIT_CLIENT_ID and _SECRET are the app's id and secret.
			return t, &Failure{Kind: ErrRejected, Msg: "Reddit did not accept this server's app keys"}
		}
		return t, err
	}
	switch {
	case t.Error == "invalid_grant":
		return t, &Failure{Kind: ErrReauth, Msg: "Reddit no longer accepts this sign-in; reconnect the account"}
	case t.Error != "":
		return t, &Failure{Kind: ErrRejected, Msg: "Reddit refused the sign-in (" + t.Error + ")"}
	case t.AccessToken == "":
		return t, errors.New("Reddit returned no access token")
	}
	return t, nil
}

func (r Reddit) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := r.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	if t.Scope != "" && !hasScope(t.Scope, "submit") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Reddit did not allow posting: allow it when you connect, then try again"}
	}
	if t.RefreshToken == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Reddit gave no lasting sign-in; connect again"}
	}
	var me struct {
		Name    string `json:"name"`
		IconImg string `json:"icon_img"`
	}
	req, err := redditRequest(ctx, http.MethodGet, redditOAuth+"/api/v1/me?raw_json=1", t.AccessToken, nil)
	if err != nil {
		return nil, Profile{}, err
	}
	if err := redditDo(ctx, req, &me); err != nil {
		return nil, Profile{}, err
	}
	if me.Name == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Reddit did not say which account signed in"}
	}
	creds := t.into(Credentials{"username": me.Name}, time.Now())
	avatar, _, _ := strings.Cut(me.IconImg, "?")
	return creds, Profile{Handle: "u/" + me.Name, Name: me.Name, AvatarURL: avatar,
		ProfileURL: "https://www.reddit.com/user/" + me.Name + "/"}, nil
}

// Refresh trades the permanent refresh token for an hour-long access token; Reddit keeps the refresh token.
func (r Reddit) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Reddit: no lasting sign-in stored; reconnect the account"}
	}
	t, err := r.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// Reddit takes the keys before the code: good keys and a made-up code answer {"error":"invalid_grant"}, bad keys 401.
func (Reddit) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, redditWWW+"/api/v1/access_token", url.Values{"grant_type": {"authorization_code"},
		"code": {checkCode}, "redirect_uri": {redirect}})
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(app.ClientID, app.ClientSecret)
	req.Header.Set("User-Agent", redditUserAgent)
	status, body, err := probe(ctx, req, "Reddit")
	switch {
	case err != nil:
		return false, err
	case status == http.StatusUnauthorized:
		return false, errors.New("Reddit did not accept the app keys: check the app's id (under its name) and secret at reddit.com/prefs/apps")
	case strings.Contains(body, "invalid_grant"):
		return true, nil
	}
	return false, unexpected("Reddit", status, body)
}

// Reddit publishes a post's thread as comments, each under the one before.
var _ Replier = Reddit{}

func (r Reddit) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, user := c["access_token"], c["username"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Reddit: no account stored; reconnect the account"}
	}
	sr := redditSubreddit(s["subreddit"])
	if sr == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Reddit: choose a subreddit"}
	}
	title := strings.TrimSpace(s["title"])
	form := url.Values{"api_type": {"json"}, "sr": {sr}, "title": {title}, "resubmit": {"true"},
		"nsfw": {fmt.Sprint(s["nsfw"] == "true")}, "spoiler": {fmt.Sprint(s["spoiler"] == "true")}}
	if f := strings.TrimSpace(s["flair"]); f != "" {
		id, err := r.flairID(ctx, token, sr, f)
		if err != nil {
			return Result{}, err
		}
		form.Set("flair_id", id)
	}
	media := len(p.Media) > 0
	switch {
	case media:
		m := p.Media[0]
		u, err := r.upload(ctx, token, m)
		if err != nil {
			return Result{}, err
		}
		form.Set("url", u)
		form.Set("kind", "image")
		if strings.HasPrefix(m.MimeType, "video/") {
			// Reddit shows a cover picture before a video plays and takes no video without one: a plain dark one.
			poster, err := r.upload(ctx, token, MediaFile{Name: "cover.png", MimeType: "image/png",
				Open: func(context.Context) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(redditPoster())), nil
				}})
			if err != nil {
				return Result{}, err
			}
			form.Set("kind", "video")
			form.Set("video_poster_url", poster)
		}
	case strings.TrimSpace(s["link"]) != "":
		form.Set("kind", "link")
		form.Set("url", strings.TrimSpace(s["link"]))
	default:
		form.Set("kind", "self")
		form.Set("text", p.Text)
	}

	sent := time.Now()
	req, err := redditRequest(ctx, http.MethodPost, redditOAuth+"/api/submit", token, form)
	if err != nil {
		return Result{}, err
	}
	var out redditJSON
	if err := redditDo(ctx, req, &out); err != nil {
		var f *Failure
		if errors.As(err, &f) {
			return Result{}, err
		}
		// Reddit has no idempotency key: a post that went out before the answer was lost is in the account's posts.
		if res, ok := r.find(ctx, token, user, sr, title, sent, 1); ok {
			return res, nil
		}
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Reddit may have published the post, but its answer was lost (" + err.Error() + "); check r/" + sr + " before posting it again"}
	}
	if err := out.failure("the post to r/" + sr); err != nil {
		return Result{}, err
	}
	if out.JSON.Data.Name != "" {
		u := out.JSON.Data.URL
		if u == "" {
			u = "https://www.reddit.com/r/" + sr + "/comments/" + out.JSON.Data.ID + "/"
		}
		return Result{ExternalID: out.JSON.Data.Name, URL: u}, nil
	}
	// Picture and video posts answer with a socket to wait on instead of the post: Reddit creates it shortly after.
	if res, ok := r.find(ctx, token, user, sr, title, sent, redditLooks); ok {
		return res, nil
	}
	// It was accepted; without its id the post has no comments, but it is not posted again.
	return Result{URL: "https://www.reddit.com/user/" + user + "/submitted/"}, nil
}

// find looks for the account's post with this title in sr made since sent, up to looks times.
func (Reddit) find(ctx context.Context, token, user, sr, title string, sent time.Time, looks int) (Result, bool) {
	for i := 0; i < looks; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return Result{}, false
			case <-time.After(redditPoll):
			}
		}
		var out struct {
			Data struct {
				Children []struct {
					Data struct {
						Name      string  `json:"name"`
						Subreddit string  `json:"subreddit"`
						Title     string  `json:"title"`
						Created   float64 `json:"created_utc"`
						Permalink string  `json:"permalink"`
					} `json:"data"`
				} `json:"children"`
			} `json:"data"`
		}
		req, err := redditRequest(ctx, http.MethodGet, redditOAuth+"/user/"+url.PathEscape(user)+"/submitted?sort=new&limit=10&raw_json=1", token, nil)
		if err != nil {
			return Result{}, false
		}
		if redditDo(ctx, req, &out) != nil {
			continue
		}
		for _, ch := range out.Data.Children {
			d := ch.Data
			// A minute of slack for the two clocks; an older post with the same title is not this one.
			if strings.EqualFold(d.Subreddit, sr) && d.Title == title && d.Created >= float64(sent.Add(-time.Minute).Unix()) {
				return Result{ExternalID: d.Name, URL: "https://www.reddit.com" + d.Permalink}, true
			}
		}
	}
	return Result{}, false
}

// flairID finds the subreddit's post flair whose text (or id) is name.
func (Reddit) flairID(ctx context.Context, token, sr, name string) (string, error) {
	req, err := redditRequest(ctx, http.MethodGet, redditOAuth+"/r/"+sr+"/api/link_flair_v2?raw_json=1", token, nil)
	if err != nil {
		return "", err
	}
	var flairs []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := redditDo(ctx, req, &flairs); err != nil {
		var f *Failure
		if errors.As(err, &f) && f.Kind == ErrRejected {
			return "", &Failure{Kind: ErrRejected, Msg: "Reddit: r/" + sr + " has no flairs to choose from; leave Flair empty"}
		}
		return "", err
	}
	var names []string
	for _, fl := range flairs {
		if strings.EqualFold(strings.TrimSpace(fl.Text), name) || fl.ID == name {
			return fl.ID, nil
		}
		names = append(names, fl.Text)
	}
	if len(names) == 0 {
		return "", &Failure{Kind: ErrRejected, Msg: "Reddit: r/" + sr + " has no flairs to choose from; leave Flair empty"}
	}
	return "", &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("Reddit: r/%s has no flair %q; use one of: %s", sr, name, strings.Join(names, ", "))}
}

// upload puts a file in Reddit's media storage: Reddit hands out an upload form for its bucket, the file goes there,
// and the post names it by its address.
func (Reddit) upload(ctx context.Context, token string, m MediaFile) (string, error) {
	req, err := redditRequest(ctx, http.MethodPost, redditOAuth+"/api/media/asset.json", token,
		url.Values{"filepath": {m.Name}, "mimetype": {m.MimeType}})
	if err != nil {
		return "", err
	}
	var lease struct {
		Args struct {
			Action string `json:"action"`
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"args"`
	}
	if err := redditDo(ctx, req, &lease); err != nil {
		return "", err
	}
	action := lease.Args.Action
	if strings.HasPrefix(action, "//") {
		action = "https:" + action
	}
	if action == "" {
		return "", &Failure{Msg: "Reddit gave no place to upload " + m.Name + "; staqly tries again in a few minutes"}
	}
	var fields [][2]string
	key := ""
	for _, f := range lease.Args.Fields {
		fields = append(fields, [2]string{f.Name, f.Value})
		if f.Name == "key" {
			key = f.Value
		}
	}
	body, n, ct, closeFiles, err := sizedForm(ctx, fields, []formFile{{Field: "file", M: m}})
	defer closeFiles()
	if err != nil {
		return "", fmt.Errorf("Reddit: %w", err)
	}
	up, err := http.NewRequestWithContext(ctx, http.MethodPost, action, body)
	if err != nil {
		return "", err
	}
	up.ContentLength = n
	up.Header.Set("Content-Type", ct)
	resp, err := httpClient(ctx).Do(up)
	if err != nil {
		return "", &Failure{Msg: "Reddit's media storage could not be reached; staqly tries again in a few minutes"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 {
			return "", &Failure{Msg: fmt.Sprintf("Reddit's media storage is unavailable (%d); staqly tries again in a few minutes", resp.StatusCode)}
		}
		msg := string(raw)
		if i, j := strings.Index(msg, "<Message>"), strings.Index(msg, "</Message>"); i >= 0 && j > i {
			msg = msg[i+9 : j]
		} else if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		return "", &Failure{Kind: ErrRejected, Msg: "Reddit did not take " + m.Name + " (" + msg + ")"}
	}
	return strings.TrimRight(action, "/") + "/" + key, nil
}

// redditPoster is the plain cover picture of a video post.
func redditPoster() []byte {
	img := image.NewPaletted(image.Rect(0, 0, 1280, 720), color.Palette{color.RGBA{0x1a, 0x1a, 0x1b, 0xff}})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// Reply comments under parentExternalID: the post (t3_...) or, from the second part on, the comment before (t1_...).
func (Reddit) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Reddit: no account stored; reconnect the account"}
	}
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Reddit: nothing to comment on (Reddit did not say the post's id)"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Reddit comments take text only"}
	}
	thing := parentExternalID
	if !strings.HasPrefix(thing, "t1_") && !strings.HasPrefix(thing, "t3_") {
		thing = "t3_" + thing
	}
	req, err := redditRequest(ctx, http.MethodPost, redditOAuth+"/api/comment", token,
		url.Values{"api_type": {"json"}, "thing_id": {thing}, "text": {p.Text}})
	if err != nil {
		return Result{}, err
	}
	var out redditJSON
	if err := redditDo(ctx, req, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Reddit may have published the comment, but its answer was lost (" + err.Error() + "); check the post before posting it again"}
		}
		return Result{}, err
	}
	if err := out.failure("the comment"); err != nil {
		return Result{}, err
	}
	if len(out.JSON.Data.Things) == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Reddit took the comment but did not say its id; check the post"}
	}
	d := out.JSON.Data.Things[0].Data
	return Result{ExternalID: d.Name, URL: "https://www.reddit.com" + d.Permalink}, nil
}

// redditJSON is what api_type=json calls answer: errors as [code, message, field] lists, even with 200.
type redditJSON struct {
	JSON struct {
		Errors [][]any `json:"errors"`
		Data   struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			URL    string `json:"url"`
			Things []struct {
				Data struct {
					Name      string `json:"name"`
					Permalink string `json:"permalink"`
				} `json:"data"`
			} `json:"things"`
		} `json:"data"`
	} `json:"json"`
}

// failure maps Reddit's error list for what (e.g. "the post to r/running").
func (o redditJSON) failure(what string) error {
	if len(o.JSON.Errors) == 0 {
		return nil
	}
	e := o.JSON.Errors[0]
	code, msg := "", ""
	if len(e) > 0 {
		code = fmt.Sprint(e[0])
	}
	if len(e) > 1 {
		msg = fmt.Sprint(e[1])
	}
	switch code {
	case "RATELIMIT":
		// Nothing was posted: Reddit asks to wait (new accounts and busy subreddits).
		return &Failure{Msg: "Reddit asks to wait before posting again (" + msg + "); staqly tries again in a few minutes"}
	case "USER_REQUIRED":
		return &Failure{Kind: ErrReauth, Msg: "Reddit no longer accepts this account's sign-in; reconnect it"}
	case "SUBREDDIT_NOEXIST", "BAD_SR_NAME":
		return &Failure{Kind: ErrRejected, Msg: "Reddit: that subreddit does not exist (" + msg + ")"}
	case "SUBMIT_VALIDATION_FLAIR_REQUIRED":
		return &Failure{Kind: ErrRejected, Msg: "Reddit: this subreddit asks for a flair; set Flair to one of its flairs"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Reddit refused " + what + ": " + msg + " (" + code + ")"}
}

// redditRequest builds an API call with the account's token; form (when set) is the body.
func redditRequest(ctx context.Context, method, endpoint, token string, form url.Values) (*http.Request, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req, nil
}

// redditDo sends a request and decodes a 2xx reply into out; Reddit's errors become messages a person can act on.
// A request that never got an answer is a plain error (not a Failure), so Publish can tell it apart.
func redditDo(ctx context.Context, req *http.Request, out any) error {
	req.Header.Set("User-Agent", redditUserAgent)
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Reddit could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Msg: "Reddit answered something unexpected; staqly tries again in a few minutes"}
			}
		}
		return nil
	}
	return redditError(resp.StatusCode, raw)
}

// redditError maps Reddit's HTTP errors: a sign-in problem needs reconnecting, rate limits and outages are retried,
// the rest is refused.
func redditError(status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
		Error   any    `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if e.Reason != "" {
		detail = e.Reason
	}
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 || strings.HasPrefix(detail, "<") {
			detail = http.StatusText(status)
		}
	}
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Reddit no longer accepts this account's sign-in; reconnect it"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Reddit rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Reddit is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Reddit refused it (" + detail + "): the subreddit may be private or restricted, or the account banned there"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "Reddit: not found (" + detail + "); check the subreddit's name"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Reddit: " + detail}
}
