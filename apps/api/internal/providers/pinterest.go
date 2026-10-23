package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Pinterest over API v5: OAuth 2.0 (access tokens for 30 days, refresh tokens for a year), a pin per post with one
// picture sent as its bytes (no public link needed), saved to the board the post names (by name or id). Pinterest
// has no comments API for pins, so a post's thread is not sent.
type Pinterest struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	pinterestAPI       = "https://api.pinterest.com"
	pinterestAuthorize = "https://www.pinterest.com/oauth/"
)

// boards:read finds the board by name, pins:read finds a pin whose answer was lost, pins:write creates it.
const pinterestScopes = "boards:read,pins:read,pins:write,user_accounts:read"

func init() {
	l := Limits{MaxChars: 500, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true, NeedsMedia: true, NeedsLink: true}
	Register(Pinterest{NewBase("pinterest", "Pinterest", AuthOAuth2, l).WithSettings(soonSettings("pinterest", l)...)}, StatusAvailable)
}

func (p Pinterest) Validate(post PostContent, s Settings) error {
	if err := CheckLimits(p.Limits(), post, s); err != nil {
		return err
	}
	rules := p.Limits().Image
	for _, m := range post.Media {
		if !strings.HasPrefix(m.MimeType, "image/") {
			return invalid("media", "Pinterest pins take one picture")
		}
		if rules != nil && len(rules.Formats) > 0 && !containsString(rules.Formats, m.MimeType) {
			return invalid("media", "Pinterest takes JPEG, PNG or WebP pictures; save "+m.Name+" as one of them")
		}
		if rules != nil && rules.MaxBytes > 0 && m.Size > rules.MaxBytes {
			return invalid("media", "Pinterest takes pictures up to 20 MB")
		}
	}
	return CheckFields(p.SettingsFields(), s)
}

func (Pinterest) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {pinterestScopes},
		"state":         {state},
	}
	return pinterestAuthorize + "?" + q.Encode(), nil
}

// token calls the token endpoint, the app authenticating with its keys (HTTP basic).
func (Pinterest) token(ctx context.Context, app App, form url.Values) (refreshableToken, error) {
	var t refreshableToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pinterestAPI+"/v5/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(app.ClientID, app.ClientSecret)
	if err := pinterestDo(ctx, req, &t); err != nil {
		var f *Failure
		if errors.As(err, &f) && f.Kind == ErrReauth && form.Get("grant_type") == "authorization_code" {
			// For whoever runs the server: PROVIDER_PINTEREST_CLIENT_ID and _SECRET are the App id and App secret key.
			return t, &Failure{Kind: ErrRejected, Msg: "Pinterest did not accept this server's app keys or the sign-in (" + f.Msg + "); connect again"}
		}
		if errors.As(err, &f) && f.Kind == ErrRejected && form.Get("grant_type") == "refresh_token" {
			return t, &Failure{Kind: ErrReauth, Msg: "Pinterest no longer accepts this sign-in (" + f.Msg + "); reconnect the account"}
		}
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Pinterest returned no access token")
	}
	return t, nil
}

func (p Pinterest) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := p.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	if t.Scope != "" && !hasScope(t.Scope, "pins:write") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Pinterest did not allow creating pins: allow it when you connect, then try again"}
	}
	var me struct {
		Username     string `json:"username"`
		BusinessName string `json:"business_name"`
		ProfileImage string `json:"profile_image"`
	}
	if err := pinterestGet(ctx, t.AccessToken, "/v5/user_account", &me); err != nil {
		return nil, Profile{}, err
	}
	if me.Username == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Pinterest did not say which account signed in"}
	}
	name := me.BusinessName
	if name == "" {
		name = me.Username
	}
	return t.into(Credentials{"username": me.Username}, time.Now()), Profile{Handle: "@" + me.Username, Name: name,
		AvatarURL: me.ProfileImage, ProfileURL: "https://www.pinterest.com/" + me.Username + "/"}, nil
}

// Refresh renews the 30-day access token (Pinterest may also hand out a new refresh token).
func (p Pinterest) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Pinterest: no lasting sign-in stored; reconnect the account"}
	}
	t, err := p.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// Pinterest refuses an app with wrong keys (401) before it looks at the code; with right keys it refuses the
// made-up code. Its wording for that is not documented, so the check proves the keys only by the first answer.
func (Pinterest) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, pinterestAPI+"/v5/oauth/token", url.Values{"grant_type": {"authorization_code"},
		"code": {checkCode}, "redirect_uri": {redirect}})
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(app.ClientID, app.ClientSecret)
	status, body, err := probe(ctx, req, "Pinterest")
	switch {
	case err != nil:
		return false, err
	case status == http.StatusUnauthorized, strings.Contains(body, "invalid_client"):
		return false, errors.New("Pinterest did not accept the app keys: check the App id and App secret key on the app's page")
	case status == http.StatusBadRequest:
		return false, nil
	}
	return false, unexpected("Pinterest", status, body)
}

type pinterestBoard struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// board finds the account's board named (or with the id) v.
func (Pinterest) board(ctx context.Context, token, v string) (string, error) {
	var names []string
	bookmark := ""
	for page := 0; page < 20; page++ {
		path := "/v5/boards?page_size=250"
		if bookmark != "" {
			path += "&bookmark=" + url.QueryEscape(bookmark)
		}
		var out struct {
			Items    []pinterestBoard `json:"items"`
			Bookmark string           `json:"bookmark"`
		}
		if err := pinterestGet(ctx, token, path, &out); err != nil {
			return "", err
		}
		for _, b := range out.Items {
			if b.ID == v || strings.EqualFold(strings.TrimSpace(b.Name), v) {
				return b.ID, nil
			}
			names = append(names, b.Name)
		}
		if bookmark = out.Bookmark; bookmark == "" {
			break
		}
	}
	if len(names) == 0 {
		return "", &Failure{Kind: ErrRejected, Msg: "Pinterest: this account has no boards; create one on pinterest.com first"}
	}
	if len(names) > 30 {
		names = append(names[:30], "...")
	}
	return "", &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("Pinterest: no board %q on this account; use one of: %s", v, strings.Join(names, ", "))}
}

func (p Pinterest) Publish(ctx context.Context, c Credentials, post PostContent, s Settings) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Pinterest: no account stored; reconnect the account"}
	}
	if len(post.Media) != 1 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Pinterest: a pin is one picture"}
	}
	board, err := p.board(ctx, token, strings.TrimSpace(s["board"]))
	if err != nil {
		return Result{}, err
	}
	m := post.Media[0]
	data, err := readMedia(ctx, m, 0)
	if err != nil {
		return Result{}, fmt.Errorf("Pinterest: %w", err)
	}
	title, link := strings.TrimSpace(s["title"]), strings.TrimSpace(s["link"])
	pin := map[string]any{
		"board_id":    board,
		"title":       title,
		"description": post.Text,
		"link":        link,
		"media_source": map[string]any{"source_type": "image_base64", "content_type": m.MimeType,
			"data": base64.StdEncoding.EncodeToString(data)},
	}
	if alt := strings.TrimSpace(s["alt_text"]); alt != "" {
		pin["alt_text"] = alt
	}
	b, _ := json.Marshal(pin)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pinterestAPI+"/v5/pins", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	sent := time.Now()
	var out struct {
		ID string `json:"id"`
	}
	if err := pinterestDo(ctx, req, &out); err != nil {
		var f *Failure
		if errors.As(err, &f) {
			return Result{}, err
		}
		// Pinterest has no idempotency key: a pin made before the answer was lost is on the board.
		if id := p.recent(ctx, token, board, title, link, sent); id != "" {
			return Result{ExternalID: id, URL: "https://www.pinterest.com/pin/" + id + "/"}, nil
		}
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Pinterest may have published the pin, but its answer was lost (" + err.Error() + "); check the board before posting it again"}
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Pinterest made the pin but did not say its id; check the board"}
	}
	return Result{ExternalID: out.ID, URL: "https://www.pinterest.com/pin/" + out.ID + "/"}, nil
}

// recent finds the board's pin with this title and link made since sent ("" when none or Pinterest can't be asked).
func (Pinterest) recent(ctx context.Context, token, board, title, link string, sent time.Time) string {
	var out struct {
		Items []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Link      string `json:"link"`
			CreatedAt string `json:"created_at"`
		} `json:"items"`
	}
	if pinterestGet(ctx, token, "/v5/boards/"+url.PathEscape(board)+"/pins?page_size=25", &out) != nil {
		return ""
	}
	for _, it := range out.Items {
		at, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(it.CreatedAt, "Z"))
		if err != nil {
			continue
		}
		// A minute of slack for the two clocks; an older pin with the same title is not this one.
		if it.Title == title && strings.TrimRight(it.Link, "/") == strings.TrimRight(link, "/") && !at.Before(sent.UTC().Add(-time.Minute)) {
			return it.ID
		}
	}
	return ""
}

func pinterestGet(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pinterestAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return pinterestDo(ctx, req, out)
}

// pinterestDo sends a request and decodes a 2xx reply into out; Pinterest's errors become messages a person can act
// on. A request that never got an answer is a plain error (not a Failure), so Publish can tell it apart.
func pinterestDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Pinterest could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Msg: "Pinterest answered something unexpected; staqly tries again in a few minutes"}
			}
		}
		return nil
	}
	return pinterestError(resp.StatusCode, raw)
}

// pinterestError maps Pinterest's {"code": n, "message": "..."} errors to what staqly does next.
func pinterestError(status int, raw []byte) error {
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = e.Error
	}
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Pinterest no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Pinterest rate limit reached; staqly tries again in a few minutes"}
	case status >= 500, strings.Contains(detail, "Something went wrong on our end"):
		return &Failure{Msg: fmt.Sprintf("Pinterest is unavailable (%s); staqly tries again in a few minutes", detail)}
	case strings.Contains(detail, "Board not found"):
		return &Failure{Kind: ErrRejected, Msg: "Pinterest: the board is not there any more; choose another"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Pinterest refused it (" + detail + "); the account may not save pins to this board"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Pinterest refused the pin: " + detail}
}
