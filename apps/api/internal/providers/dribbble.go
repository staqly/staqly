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

// Dribbble over API v2: OAuth 2.0 with the upload scope (tokens that do not expire until the person revokes them),
// a shot per post through POST /shots: one picture of exactly 400x300 or 800x600, a title, a description and tags.
// Dribbble creates the shot after it answers; the API has no comments, so a post's thread is not sent.
type Dribbble struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	dribbbleAPI   = "https://api.dribbble.com"
	dribbbleOAuth = "https://dribbble.com/oauth"
	// dribbblePoll is how long to wait between looks for a shot whose answer was lost.
	dribbblePoll = 3 * time.Second
)

const (
	dribbbleScopes  = "public upload"
	dribbbleMaxTags = 12
)

func init() {
	l := Limits{MaxChars: 1000, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true, NeedsMedia: true,
		Image: &ImageRules{MaxBytes: 8 << 20, Formats: []string{"image/jpeg", "image/png", "image/gif"}, MinRatio: 4.0 / 3, MaxRatio: 4.0 / 3,
			MinWidth: 400, MaxPixels: 800 * 600, Note: "Dribbble takes exactly 400x300 or 800x600 pixels."}}
	Register(Dribbble{NewBase("dribbble", "Dribbble", AuthOAuth2, l).WithSettings(soonSettings("dribbble", l)...)}, StatusAvailable)
}

func (d Dribbble) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(d.Limits(), p, s); err != nil {
		return err
	}
	for _, m := range p.Media {
		switch m.MimeType {
		case "image/jpeg", "image/png", "image/gif":
		default:
			return invalid("media", "Dribbble takes JPEG, PNG or GIF pictures; save "+m.Name+" as one of them")
		}
		if m.Size > 8<<20 {
			return invalid("media", "Dribbble takes pictures up to 8 MB")
		}
	}
	if len(p.Tags) > dribbbleMaxTags {
		return invalid("tags", fmt.Sprintf("Dribbble takes at most %d tags", dribbbleMaxTags))
	}
	return CheckFields(d.SettingsFields(), s)
}

func (Dribbble) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "scope": {dribbbleScopes}, "state": {state}}
	return dribbbleOAuth + "/authorize?" + q.Encode(), nil
}

func (Dribbble) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	req, err := checkForm(ctx, dribbbleOAuth+"/token", url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret},
		"code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	var t refreshableToken
	if err := dribbbleDo(ctx, req, &t); err != nil && t.Error == "" {
		return nil, Profile{}, err
	}
	switch {
	case t.Error == "invalid_client":
		// For whoever runs the server: PROVIDER_DRIBBBLE_CLIENT_ID and _SECRET are the application's Client ID and Secret.
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Dribbble did not accept this server's app keys"}
	case t.Error != "":
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Dribbble refused the sign-in (" + t.Error + "); connect again"}
	case t.AccessToken == "":
		return nil, Profile{}, errors.New("Dribbble returned no access token")
	case t.Scope != "" && !hasScope(t.Scope, "upload"):
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Dribbble did not allow uploading shots: allow it when you connect, then try again"}
	}
	var me struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		HTMLURL   string `json:"html_url"`
		CanUpload *bool  `json:"can_upload_shot"`
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, dribbbleAPI+"/v2/user", nil)
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)
	if err := dribbbleDo(ctx, req, &me); err != nil {
		return nil, Profile{}, err
	}
	if me.Login == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Dribbble did not say which account signed in"}
	}
	if me.CanUpload != nil && !*me.CanUpload {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Dribbble does not let this account upload shots yet; post a shot on dribbble.com first (Dribbble opens uploading to new accounts itself)"}
	}
	profileURL := me.HTMLURL
	if profileURL == "" {
		profileURL = "https://dribbble.com/" + me.Login
	}
	name := me.Name
	if name == "" {
		name = me.Login
	}
	return Credentials{"access_token": t.AccessToken, "username": me.Login},
		Profile{Handle: "@" + me.Login, Name: name, AvatarURL: me.AvatarURL, ProfileURL: profileURL}, nil
}

// Refresh checks the token (Dribbble's do not expire), so a channel test finds a revoked sign-in.
func (Dribbble) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	if c["access_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Dribbble: no account stored; reconnect the account"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dribbbleAPI+"/v2/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c["access_token"])
	if err := dribbbleDo(ctx, req, nil); err != nil {
		return nil, err
	}
	return c, nil
}

// Dribbble checks the keys before the code: a made-up code with good keys is "invalid_grant", bad keys "invalid_client".
func (Dribbble) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, dribbbleOAuth+"/token", url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret},
		"code": {checkCode}, "redirect_uri": {redirect}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Dribbble")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client"):
		return false, errors.New("Dribbble did not accept the app keys: check the Client ID and Client Secret of the application")
	case strings.Contains(body, "invalid_grant"):
		return true, nil
	}
	return false, unexpected("Dribbble", status, body)
}

func (d Dribbble) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Dribbble: no account stored; reconnect the account"}
	}
	if len(p.Media) != 1 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Dribbble: a shot is one picture"}
	}
	m := p.Media[0]
	data, err := readMedia(ctx, m, 8<<20)
	if err != nil {
		return Result{}, fmt.Errorf("Dribbble: %w", err)
	}
	if len(data) > 8<<20 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Dribbble takes pictures up to 8 MB"}
	}
	if w, h, ok := imageSize(data); ok && !(w == 400 && h == 300) && !(w == 800 && h == 600) {
		return Result{}, &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("Dribbble takes pictures of exactly 400x300 or 800x600 pixels; %s is %dx%d (the media editor can crop it)", m.Name, w, h)}
	}
	title := strings.TrimSpace(s["title"])
	fields := [][2]string{{"title", title}}
	if strings.TrimSpace(p.Text) != "" {
		fields = append(fields, [2]string{"description", p.Text})
	}
	for _, t := range p.Tags {
		fields = append(fields, [2]string{"tags[]", strings.TrimPrefix(t, "#")})
	}
	m.Open = func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	m.Size = int64(len(data))
	body, n, ct, closeFiles, err := sizedForm(ctx, fields, []formFile{{Field: "image", M: m}})
	defer closeFiles()
	if err != nil {
		return Result{}, fmt.Errorf("Dribbble: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dribbbleAPI+"/v2/shots", body)
	if err != nil {
		return Result{}, err
	}
	req.ContentLength = n
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+token)
	sent := time.Now()
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// Dribbble has no idempotency key: a shot made before the answer was lost is among the account's shots.
		if id := d.recent(ctx, token, title, sent); id != "" {
			return Result{ExternalID: id, URL: "https://dribbble.com/shots/" + id}, nil
		}
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Dribbble may have published the shot, but its answer was lost (Dribbble could not be reached); check the profile before posting it again"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, dribbbleError(resp.StatusCode, raw)
	}
	// 202: the shot is being made; its address names it.
	loc := strings.TrimRight(resp.Header.Get("Location"), "/")
	id := loc[strings.LastIndex(loc, "/")+1:]
	if id == "" {
		return Result{URL: "https://dribbble.com/" + c["username"]}, nil
	}
	return Result{ExternalID: id, URL: "https://dribbble.com/shots/" + id}, nil
}

// recent finds the account's shot with this title made since sent, looking a few times (Dribbble makes it after
// it takes the upload); "" when none.
func (Dribbble) recent(ctx context.Context, token, title string, sent time.Time) string {
	for i := 0; i < 3; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ""
			case <-time.After(dribbblePoll):
			}
		}
		var shots []struct {
			ID          int64     `json:"id"`
			Title       string    `json:"title"`
			PublishedAt time.Time `json:"published_at"`
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dribbbleAPI+"/v2/user/shots?per_page=10", nil)
		if err != nil {
			return ""
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if dribbbleDo(ctx, req, &shots) != nil {
			continue
		}
		for _, sh := range shots {
			if sh.Title == title && !sh.PublishedAt.Before(sent.Add(-time.Minute)) {
				return fmt.Sprint(sh.ID)
			}
		}
	}
	return ""
}

// dribbbleDo sends a request and decodes the reply into out (also on errors, for the token endpoint's "error");
// Dribbble's errors become messages a person can act on.
func dribbbleDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Dribbble could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return dribbbleError(resp.StatusCode, raw)
}

// dribbbleError maps Dribbble's {"message", "errors": [{"attribute","message"}]} replies.
func dribbbleError(status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Attribute string `json:"attribute"`
			Message   string `json:"message"`
		} `json:"errors"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if len(e.Errors) > 0 {
		var parts []string
		for _, x := range e.Errors {
			parts = append(parts, strings.TrimSpace(x.Attribute+" "+x.Message))
		}
		detail = strings.Join(parts, "; ")
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
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Dribbble no longer accepts this account's sign-in; reconnect it"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Dribbble rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Dribbble is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Dribbble refused it (" + detail + "): the account may not upload shots"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Dribbble refused the shot: " + detail}
}
