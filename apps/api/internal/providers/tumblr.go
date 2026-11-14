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
	"regexp"
	"strings"
	"time"
)

// Tumblr over API v2 with OAuth 2.0 (access tokens for 42 minutes, renewed with the refresh token offline_access
// gives). One sign-in may post to every blog the person has; each chosen blog is its own channel (AccountLister).
// Posts are in the Neue Post Format: text blocks, then the pictures and the video sent in the same request
// (multipart), and the post's tags. Tumblr's API cannot reply to a post, so a post's thread is not sent.
type Tumblr struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	tumblrAPI       = "https://api.tumblr.com"
	tumblrAuthorize = "https://www.tumblr.com/oauth2/authorize"
)

const (
	tumblrScopes    = "basic write offline_access"
	tumblrBlockMax  = 4096 // characters in one text block
	tumblrUserAgent = "staqly/1.0 (+https://staqly.ai)"
)

func init() {
	Register(Tumblr{NewBase("tumblr", "Tumblr", AuthOAuth2,
		Limits{MaxChars: 4096, MaxMedia: 10, MediaKinds: []string{"image", "video"}})}, StatusAvailable)
}

func (t Tumblr) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(t.Limits(), p, s); err != nil {
		return err
	}
	videos := 0
	for _, m := range p.Media {
		switch {
		case strings.HasPrefix(m.MimeType, "video/"):
			videos++
		case strings.HasPrefix(m.MimeType, "image/"):
		default:
			return invalid("media", "Tumblr takes pictures and one video")
		}
	}
	if videos > 1 {
		return invalid("media", "a Tumblr post takes one video")
	}
	return CheckFields(t.SettingsFields(), s)
}

func (Tumblr) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{"client_id": {app.ClientID}, "response_type": {"code"}, "scope": {tumblrScopes},
		"state": {state}, "redirect_uri": {redirect}}
	return tumblrAuthorize + "?" + q.Encode(), nil
}

// token calls the token endpoint, which takes the app's keys in the form.
func (Tumblr) token(ctx context.Context, app App, form url.Values) (refreshableToken, error) {
	var t refreshableToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tumblrAPI+"/v2/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err = tumblrDo(ctx, req, &t)
	switch {
	case t.Error == "invalid_client":
		// For whoever runs the server: PROVIDER_TUMBLR_CLIENT_ID and _SECRET are the app's OAuth consumer key and secret.
		return t, &Failure{Kind: ErrRejected, Msg: "Tumblr did not accept this server's app keys"}
	case t.Error == "invalid_grant", t.Error == "invalid_request" && form.Get("grant_type") == "refresh_token":
		return t, &Failure{Kind: ErrReauth, Msg: "Tumblr no longer accepts this sign-in; reconnect the account"}
	case err != nil:
		return t, err
	case t.Error != "":
		return t, &Failure{Kind: ErrRejected, Msg: "Tumblr refused the sign-in (" + t.Error + ")"}
	case t.AccessToken == "":
		return t, errors.New("Tumblr returned no access token")
	}
	return t, nil
}

type tumblrBlog struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	UUID    string `json:"uuid"`
	Primary bool   `json:"primary"`
	Avatar  []struct {
		Width int    `json:"width"`
		URL   string `json:"url"`
	} `json:"avatar"`
}

func (b tumblrBlog) account() Account {
	a := Account{ID: b.UUID, Name: b.Title, Handle: b.Name, ProfileURL: b.URL}
	if a.ID == "" {
		a.ID = b.Name
	}
	if a.Name == "" {
		a.Name = b.Name
	}
	if a.ProfileURL == "" {
		a.ProfileURL = "https://www.tumblr.com/" + b.Name
	}
	best := 0
	for _, av := range b.Avatar {
		if av.Width > best {
			best, a.AvatarURL = av.Width, av.URL
		}
	}
	return a
}

// userInfo reads the person and their blogs.
func (Tumblr) userInfo(ctx context.Context, token string) (string, []tumblrBlog, error) {
	var out struct {
		Response struct {
			User struct {
				Name  string       `json:"name"`
				Blogs []tumblrBlog `json:"blogs"`
			} `json:"user"`
		} `json:"response"`
	}
	req, err := tumblrRequest(ctx, http.MethodGet, "/v2/user/info", token, nil, "")
	if err != nil {
		return "", nil, err
	}
	if err := tumblrDo(ctx, req, &out); err != nil {
		return "", nil, err
	}
	return out.Response.User.Name, out.Response.User.Blogs, nil
}

// Exchange signs the person in; the blogs to post to are picked next (Accounts, UseAccount).
func (t Tumblr) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	tok, err := t.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	if tok.Scope != "" && !hasScope(tok.Scope, "write") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Tumblr did not allow posting: allow it when you connect, then try again"}
	}
	if tok.RefreshToken == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Tumblr gave no lasting sign-in; connect again"}
	}
	name, blogs, err := t.userInfo(ctx, tok.AccessToken)
	if err != nil {
		return nil, Profile{}, err
	}
	if name == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Tumblr did not say which account signed in"}
	}
	profile := Profile{Handle: name, Name: name, ProfileURL: "https://www.tumblr.com/" + name}
	for _, b := range blogs {
		if b.Primary {
			profile.AvatarURL = b.account().AvatarURL
		}
	}
	return tok.into(Credentials{"user": name}, time.Now()), profile, nil
}

var _ AccountLister = Tumblr{}

// Accounts lists the person's blogs (the main one and any side blogs).
func (t Tumblr) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	_, blogs, err := t.userInfo(ctx, c["access_token"])
	if err != nil {
		return nil, err
	}
	if len(blogs) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "Tumblr: this account has no blog to post to; create one on tumblr.com, then connect again"}
	}
	out := make([]Account, 0, len(blogs))
	for _, b := range blogs {
		out = append(out, b.account())
	}
	return out, nil
}

// UseAccount narrows the sign-in to one blog: the same tokens plus the blog the channel posts to.
func (t Tumblr) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	_, blogs, err := t.userInfo(ctx, c["access_token"])
	if err != nil {
		return nil, Profile{}, err
	}
	for _, b := range blogs {
		a := b.account()
		if a.ID != accountID && b.Name != accountID {
			continue
		}
		out := Credentials{}
		for k, v := range c {
			out[k] = v
		}
		out["blog"], out["blog_name"] = a.ID, b.Name
		return out, Profile{Handle: a.Handle, Name: a.Name, AvatarURL: a.AvatarURL, ProfileURL: a.ProfileURL}, nil
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Tumblr: this account has no such blog any more; connect again"}
}

// Refresh renews the access token. Tumblr hands out a new refresh token with it, and it is stored.
// ponytail: every blog chosen from one sign-in holds a copy of the same refresh token; if Tumblr retires a refresh
// token once it is used, the second blog's next refresh fails and it needs reconnecting. The app would have to
// share one sign-in between those channels to avoid that.
func (t Tumblr) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Tumblr: no lasting sign-in stored; reconnect the account"}
	}
	tok, err := t.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return tok.into(c, time.Now()), nil
}

// Tumblr takes the keys before the code: a made-up code with good keys is "invalid_grant" (or "invalid_request"),
// bad keys "invalid_client".
func (Tumblr) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, tumblrAPI+"/v2/oauth2/token", url.Values{"grant_type": {"authorization_code"},
		"code": {checkCode}, "client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "redirect_uri": {redirect}})
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", tumblrUserAgent)
	status, body, err := probe(ctx, req, "Tumblr")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client"):
		return false, errors.New("Tumblr did not accept the app keys: check the OAuth consumer key and secret at tumblr.com/oauth/apps")
	case strings.Contains(body, "invalid_grant"), strings.Contains(body, "invalid_request"):
		return true, nil
	}
	return false, unexpected("Tumblr", status, body)
}

var tumblrParagraphs = regexp.MustCompile(`\n\s*\n`)

// tumblrText splits a post's text into NPF text blocks: one per paragraph, none longer than Tumblr takes.
func tumblrText(text string) []map[string]any {
	var out []map[string]any
	for _, para := range tumblrParagraphs.Split(strings.TrimSpace(text), -1) {
		r := []rune(strings.TrimSpace(para))
		for len(r) > 0 {
			n := min(len(r), tumblrBlockMax)
			out = append(out, map[string]any{"type": "text", "text": string(r[:n])})
			r = r[n:]
		}
	}
	return out
}

func (t Tumblr) Publish(ctx context.Context, c Credentials, p PostContent, _ Settings) (Result, error) {
	token, blog := c["access_token"], c["blog"]
	if token == "" || blog == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Tumblr: no blog stored; reconnect the account"}
	}
	content := tumblrText(p.Text)
	var files []formFile
	for i, m := range p.Media {
		id := fmt.Sprintf("media-%d", i)
		media := map[string]any{"type": m.MimeType, "identifier": id}
		if strings.HasPrefix(m.MimeType, "video/") {
			// Tumblr measures the video itself once it is transcoded; the size here only shapes the placeholder.
			media["width"], media["height"] = 540, 405
			content = append(content, map[string]any{"type": "video", "provider": "tumblr", "media": media})
		} else {
			data, err := readMedia(ctx, m, 0)
			if err != nil {
				return Result{}, fmt.Errorf("Tumblr: %w", err)
			}
			if w, h, ok := imageSize(data); ok {
				media["width"], media["height"] = w, h
			}
			content = append(content, map[string]any{"type": "image", "media": []any{media}})
			m.Open = func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
			m.Size = int64(len(data))
		}
		files = append(files, formFile{Field: id, M: m})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}
	payload := map[string]any{"content": content, "state": "published"}
	var tags []string
	for _, tag := range p.Tags {
		if tag = strings.TrimSpace(strings.TrimPrefix(tag, "#")); tag != "" {
			tags = append(tags, strings.ReplaceAll(tag, ",", " "))
		}
	}
	if len(tags) > 0 {
		payload["tags"] = strings.Join(tags, ",")
	}
	js, _ := json.Marshal(payload)

	path := "/v2/blog/" + url.PathEscape(blog) + "/posts"
	var req *http.Request
	var err error
	if len(files) == 0 {
		req, err = tumblrRequest(ctx, http.MethodPost, path, token, bytes.NewReader(js), "application/json")
	} else {
		// The JSON goes first, then each file under the identifier its block names.
		body, n, ct, closeFiles, ferr := sizedForm(ctx, nil, files)
		defer closeFiles()
		if ferr != nil {
			return Result{}, fmt.Errorf("Tumblr: %w", ferr)
		}
		jsonPart, jn, jct := tumblrJSONPart(ct, js)
		req, err = tumblrRequest(ctx, http.MethodPost, path, token, io.MultiReader(jsonPart, body), jct)
		if err == nil {
			req.ContentLength = n + jn
		}
	}
	if err != nil {
		return Result{}, err
	}
	sent := time.Now()
	var out struct {
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if err := tumblrDo(ctx, req, &out); err != nil {
		var f *Failure
		if errors.As(err, &f) {
			return Result{}, err
		}
		// Tumblr has no idempotency key: a post made before the answer was lost is the blog's newest.
		if id := t.recent(ctx, token, blog, content, sent); id != "" {
			return Result{ExternalID: id, URL: tumblrPostURL(c["blog_name"], blog, id)}, nil
		}
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Tumblr may have published the post, but its answer was lost (" + err.Error() + "); check the blog before posting it again"}
	}
	if out.Response.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Tumblr published the post but did not say its id; check the blog"}
	}
	return Result{ExternalID: out.Response.ID, URL: tumblrPostURL(c["blog_name"], blog, out.Response.ID)}, nil
}

// tumblrJSONPart is the multipart part carrying the post's JSON, written with the same boundary as the files that
// follow (sizedForm opens with a boundary line, so this part needs only its own).
func tumblrJSONPart(contentType string, js []byte) (io.Reader, int64, string) {
	boundary := contentType[strings.Index(contentType, "boundary=")+len("boundary="):]
	var b bytes.Buffer
	fmt.Fprintf(&b, "--%s\r\nContent-Disposition: form-data; name=\"json\"\r\nContent-Type: application/json\r\n\r\n", boundary)
	b.Write(js)
	b.WriteString("\r\n")
	return bytes.NewReader(b.Bytes()), int64(b.Len()), contentType
}

func tumblrPostURL(name, blog, id string) string {
	if name == "" {
		name = blog
	}
	return "https://www.tumblr.com/" + name + "/" + id
}

// recent finds the blog's post made since sent whose first block is this post's ("" when none or Tumblr can't be asked).
func (Tumblr) recent(ctx context.Context, token, blog string, content []map[string]any, sent time.Time) string {
	var out struct {
		Response struct {
			Posts []struct {
				ID        string `json:"id_string"`
				Timestamp int64  `json:"timestamp"`
				Content   []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"posts"`
		} `json:"response"`
	}
	req, err := tumblrRequest(ctx, http.MethodGet, "/v2/blog/"+url.PathEscape(blog)+"/posts?npf=true&limit=5", token, nil, "")
	if err != nil || tumblrDo(ctx, req, &out) != nil {
		return ""
	}
	first, _ := content[0]["text"].(string)
	for _, p := range out.Response.Posts {
		// A minute of slack for the two clocks; an older post is not this one.
		if p.Timestamp < sent.Add(-time.Minute).Unix() || len(p.Content) == 0 {
			continue
		}
		if content[0]["type"] != p.Content[0].Type || (first != "" && p.Content[0].Text != first) {
			continue
		}
		return p.ID
	}
	return ""
}

func tumblrRequest(ctx context.Context, method, path, token string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, tumblrAPI+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// tumblrDo sends a request and decodes the reply into out (also on errors, for the token endpoint's "error");
// Tumblr's errors become messages a person can act on. A request that never got an answer is a plain error (not a
// Failure), so Publish can tell it apart.
func tumblrDo(ctx context.Context, req *http.Request, out any) error {
	req.Header.Set("User-Agent", tumblrUserAgent)
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Tumblr could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return tumblrError(resp.StatusCode, raw)
}

// tumblrError maps Tumblr's {"meta":{"status","msg"},"errors":[{"title","code","detail"}]} replies: error codes
// 8001-8023 say what about the post Tumblr refused.
func tumblrError(status int, raw []byte) error {
	var e struct {
		Meta struct {
			Msg string `json:"msg"`
		} `json:"meta"`
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
			Code   int    `json:"code"`
		} `json:"errors"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &e)
	code, detail := 0, e.Meta.Msg
	if len(e.Errors) > 0 {
		code = e.Errors[0].Code
		detail = e.Errors[0].Title
		if e.Errors[0].Detail != "" {
			detail = e.Errors[0].Detail
		}
	}
	if detail == "" {
		detail = e.ErrorDescription
	}
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 || strings.HasPrefix(detail, "<") {
			detail = http.StatusText(status)
		}
	}
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "Tumblr: " + msg} }
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Tumblr no longer accepts this account's sign-in; reconnect it"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Tumblr rate limit reached; staqly tries again in a few minutes"}
	case code == 8010:
		return &Failure{Msg: "Tumblr is still processing this blog's last video; staqly tries again in a few minutes"}
	case code == 8006, code == 8009, status >= 500:
		return &Failure{Msg: fmt.Sprintf("Tumblr is unavailable (%s); staqly tries again in a few minutes", detail)}
	case code == 8023:
		return rejected("the blog reached its daily post limit; schedule it for tomorrow")
	case code == 8004, code == 8011:
		return rejected("the account reached Tumblr's daily limit for uploads; schedule it for tomorrow")
	case code == 8005:
		return rejected("one of the files is in a format Tumblr does not take (" + detail + ")")
	case status == http.StatusNotFound:
		return rejected("the blog is not there any more; reconnect the channel")
	case status == http.StatusForbidden:
		return rejected("refused (" + detail + ")")
	}
	return rejected(detail)
}
