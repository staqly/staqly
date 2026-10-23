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
	"path"
	"strings"
	"time"
)

// MeWe over its Open API (developer preview, https://dev.mewe.com; endpoint reference: the OpenAPI document the portal
// loads, https://dev.mewe.com/api/docs/openapi/docs.yaml). A Standalone app's sign-in: the person signs in on
// mewe.com/login and comes back with a loginRequestToken, which the app's ID and API key trade for an API token
// (no refresh: the person reconnects when it runs out). The token posts to the person's own timeline or to any of
// their groups, so the sign-in lists those as accounts. Pictures are uploaded first; MeWe's API takes no videos and
// answers a post with 204 and no id, so there are no comments either.
type MeWe struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	meweHost = "https://mewe.com"
)

// meweTimeline is the account id of the person's own timeline (groups use their MeWe ids).
const meweTimeline = "me"

func init() {
	Register(MeWe{NewBase("mewe", "MeWe", AuthOAuth2, Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: []string{"image"}})}, StatusAvailable)
}

func (m MeWe) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(m.Limits(), p, s); err != nil {
		return err
	}
	for _, f := range p.Media {
		if !strings.HasPrefix(f.MimeType, "image/") {
			return invalid("media", "MeWe's API takes pictures only; "+f.Name+" is not one")
		}
	}
	return CheckFields(m.SettingsFields(), s)
}

// AuthURL sends the person to MeWe's sign-in for Standalone apps. The App ID is the client id; the API key (the
// client secret) only goes with the token request.
func (MeWe) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "state": {state}}
	return meweHost + "/login?" + q.Encode(), nil
}

// Exchange trades the loginRequestToken MeWe sent back (as "loginRequestToken", not "code") for an API token.
func (MeWe) Exchange(ctx context.Context, app App, _, code string, extra map[string]string) (Credentials, Profile, error) {
	login := extra["loginRequestToken"]
	if login == "" {
		login = code
	}
	if login == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "MeWe sent no sign-in back; connect again"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meweHost+"/api/dev/token?"+url.Values{"loginRequestToken": {login}}.Encode(), nil)
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("X-App-Id", app.ClientID)
	req.Header.Set("X-Api-Key", app.ClientSecret)
	var tok struct {
		Pending   bool   `json:"pending"`
		APIToken  string `json:"apiToken"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := meweDo(ctx, req, &tok); err != nil {
		var f *Failure
		if errors.As(err, &f) && f.Kind != nil {
			// For whoever runs the server: PROVIDER_MEWE_CLIENT_ID is the app's App ID, _SECRET its API key.
			return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "MeWe did not accept the sign-in or this server's app (" + f.Msg + "); connect again"}
		}
		return nil, Profile{}, err
	}
	if tok.Pending {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "MeWe is still waiting for you to allow the app; allow it in MeWe, then connect again"}
	}
	if tok.APIToken == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "MeWe returned no API token; connect again"}
	}
	creds := Credentials{"access_token": tok.APIToken, "app_id": app.ClientID}
	if t, err := time.Parse(time.RFC3339, tok.ExpiresAt); err == nil {
		creds["expires_at"] = t.UTC().Format(time.RFC3339)
	}
	me, err := meweMe(ctx, creds)
	if err != nil {
		return nil, Profile{}, err
	}
	creds["user_id"], creds["handle"] = me.UserID, me.Handle
	return creds, me.profile(), nil
}

type meweProfile struct {
	UserID       string `json:"userId"`
	Name         string `json:"name"`
	Handle       string `json:"handle"`
	ProfilePhoto struct {
		Href string `json:"href"`
	} `json:"profilePhoto"`
}

func (p meweProfile) profile() Profile {
	return Profile{Handle: "@" + p.Handle, Name: p.Name, AvatarURL: meweLink(p.ProfilePhoto.Href), ProfileURL: meweProfileURL(p.Handle)}
}

func meweProfileURL(handle string) string { return "https://mewe.com/i/" + url.PathEscape(handle) }

// meweLink makes MeWe's relative picture paths absolute.
func meweLink(href string) string {
	if strings.HasPrefix(href, "/") {
		return meweHost + href
	}
	return href
}

func meweMe(ctx context.Context, c Credentials) (meweProfile, error) {
	var me meweProfile
	if err := meweCall(ctx, c, http.MethodGet, "/api/dev/me", nil, "", &me); err != nil {
		return me, err
	}
	if me.UserID == "" || me.Handle == "" {
		return me, &Failure{Kind: ErrRejected, Msg: "MeWe did not say who signed in; check that the app may read the profile (user_profile)"}
	}
	return me, nil
}

// MeWe signs in a person who may post to their timeline and their groups.
var _ AccountLister = MeWe{}

// Accounts lists the person's timeline and their groups.
func (MeWe) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	me, err := meweMe(ctx, c)
	if err != nil {
		return nil, err
	}
	out := []Account{{ID: meweTimeline, Name: me.Name + " (your timeline)", Handle: "@" + me.Handle,
		AvatarURL: meweLink(me.ProfilePhoto.Href), ProfileURL: meweProfileURL(me.Handle)}}
	next := "/api/dev/groups?limit=50"
	for page := 0; page < 20 && next != ""; page++ {
		var res struct {
			Groups []struct {
				GroupID    string `json:"groupId"`
				Name       string `json:"name"`
				CoverPhoto struct {
					Href string `json:"href"`
				} `json:"coverPhoto"`
			} `json:"groups"`
			NextPage string `json:"nextPage"`
		}
		if err := meweCall(ctx, c, http.MethodGet, next, nil, "", &res); err != nil {
			// The app may not ask for groups (user_groups): the timeline is still there.
			var f *Failure
			if page == 0 && errors.As(err, &f) && f.Kind == ErrRejected {
				break
			}
			return nil, err
		}
		for _, g := range res.Groups {
			out = append(out, Account{ID: g.GroupID, Name: g.Name, Handle: "group/" + g.GroupID,
				AvatarURL: meweLink(g.CoverPhoto.Href), ProfileURL: meweGroupURL(g.GroupID)})
		}
		next = meweNext(res.NextPage)
	}
	return out, nil
}

// meweNext turns a nextPage link (absolute or a path) into a path on the API host, "" at the end.
func meweNext(link string) string {
	if link == "" {
		return ""
	}
	if u, err := url.Parse(link); err == nil && u.IsAbs() {
		return u.RequestURI()
	}
	return link
}

func meweGroupURL(id string) string { return "https://mewe.com/group/" + url.PathEscape(id) }

// UseAccount narrows the sign-in to the timeline or one group.
func (MeWe) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	if accountID == meweTimeline || accountID == "" {
		me, err := meweMe(ctx, c)
		if err != nil {
			return nil, Profile{}, err
		}
		delete(out, "group_id")
		return out, me.profile(), nil
	}
	var g struct {
		GroupID    string `json:"groupId"`
		Name       string `json:"name"`
		CoverPhoto struct {
			Href string `json:"href"`
		} `json:"coverPhoto"`
	}
	if err := meweCall(ctx, c, http.MethodGet, "/api/dev/group/"+url.PathEscape(accountID), nil, "", &g); err != nil {
		return nil, Profile{}, err
	}
	if g.GroupID == "" {
		g.GroupID = accountID
	}
	out["group_id"] = g.GroupID
	return out, Profile{Handle: "group/" + g.GroupID, Name: g.Name, AvatarURL: meweLink(g.CoverPhoto.Href), ProfileURL: meweGroupURL(g.GroupID)}, nil
}

// Refresh: MeWe's token cannot be renewed. A token still valid is checked (a removed app is found out), an expired
// one needs reconnecting.
func (MeWe) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	if c["access_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "MeWe: no account stored; reconnect it"}
	}
	if exp, ok := credentialsExpiry(c); ok && !exp.After(time.Now()) {
		return nil, &Failure{Kind: ErrReauth, Msg: "MeWe's sign-in ran out; reconnect the account"}
	}
	if _, err := meweMe(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (MeWe) Publish(ctx context.Context, c Credentials, p PostContent, _ Settings) (Result, error) {
	if c["access_token"] == "" || c["app_id"] == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "MeWe: no account stored; reconnect it"}
	}
	if exp, ok := credentialsExpiry(c); ok && !exp.After(time.Now()) {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "MeWe's sign-in ran out; reconnect the account"}
	}
	var photos []string
	for _, m := range p.Media {
		id, err := meweUpload(ctx, c, m)
		if err != nil {
			return Result{}, err
		}
		photos = append(photos, id)
	}
	body := map[string]any{"text": p.Text}
	if len(photos) > 0 {
		body["uploadedPhotoIds"] = strings.Join(photos, ",")
	}
	endpoint, link := "/api/dev/me/post", meweProfileURL(c["handle"])
	if g := c["group_id"]; g != "" {
		endpoint, link = "/api/dev/group/"+url.PathEscape(g)+"/post", meweGroupURL(g)
	}
	// MeWe has no idempotency key: a post whose answer was lost may be out, so it is not sent again.
	if err := meweCall(ctx, c, http.MethodPost, endpoint, body, "", nil); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "MeWe may have published the post, but its answer was lost (" + err.Error() + "); check the timeline or group before posting it again"}
		}
		return Result{}, err
	}
	// MeWe answers 204 without the post's id: the target links to the timeline or the group.
	return Result{URL: link}, nil
}

// meweUpload sends one picture (multipart, field "file") and returns its id. It comes before the post, so a lost
// answer is retried.
func meweUpload(ctx context.Context, c Credentials, m MediaFile) (string, error) {
	rc, err := m.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	name := m.Name
	if name == "" {
		name = path.Base(m.Path)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename=%q`, name)}
	if m.MimeType != "" {
		h["Content-Type"] = []string{m.MimeType}
	}
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, rc); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := meweCall(ctx, c, http.MethodPost, "/api/dev/photo/upload", &buf, mw.FormDataContentType(), &out); err != nil {
		var f *Failure
		if errors.As(err, &f) {
			return "", err
		}
		return "", &Failure{Msg: err.Error() + "; staqly tries again in a few minutes"}
	}
	if out.ID == "" {
		return "", &Failure{Msg: "MeWe did not say the id of the uploaded picture " + name + "; staqly tries again in a few minutes"}
	}
	return out.ID, nil
}

// meweCall sends a request with the app id and the person's token. body is JSON unless contentType names another
// type (then body is an io.Reader).
func meweCall(ctx context.Context, c Credentials, method, p string, body any, contentType string, out any) error {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case io.Reader:
		r = b
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		r, contentType = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, meweHost+p, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-App-Id", c["app_id"])
	req.Header.Set("Authorization", "Bearer "+c["access_token"])
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return meweDo(ctx, req, out)
}

// meweDo sends a request and decodes a 2xx reply into out. A request that never got an answer is a plain error
// (not a Failure), so Publish can tell it apart.
func meweDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("MeWe could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: "MeWe answered in a way staqly does not understand"}
			}
		}
		return nil
	}
	return meweError(resp.StatusCode, raw)
}

// meweError maps MeWe's {"id","message","requiredPermissions"} answers to what staqly does next. 420 ("Enhance
// Your Calm") is MeWe's rate limit.
func meweError(status int, raw []byte) error {
	var e struct {
		Message     string   `json:"message"`
		Permissions []string `json:"requiredPermissions"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	if detail == "" {
		detail = http.StatusText(status)
	}
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "MeWe no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case status == 420, status == http.StatusTooManyRequests:
		return &Failure{Msg: "MeWe rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: "MeWe is unavailable (" + detail + "); staqly tries again in a few minutes"}
	case status == http.StatusForbidden && len(e.Permissions) > 0:
		// For whoever runs the server: the MeWe app needs these permissions in the developer settings.
		return &Failure{Kind: ErrRejected, Msg: "MeWe refused it: the app lacks the permission " + strings.Join(e.Permissions, ", ") + " (" + detail + ")"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "MeWe refused it (" + detail + "); check that you may post there"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "MeWe did not find it (" + detail + "); reconnect the channel if you left the group"}
	}
	return &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("MeWe refused it (%d %s)", status, detail)}
}
