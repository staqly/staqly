package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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

// Kick over its public API: OAuth 2.1 with PKCE on id.kick.com (the app layer passes the verifier as
// extra["code_verifier"]), refreshable tokens, and a "post" is a chat message in the account's own channel
// (POST /public/v1/chat). Chat takes text only. A post's thread goes out as chat replies to the message before.
type Kick struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	kickID  = "https://id.kick.com"
	kickAPI = "https://api.kick.com"
)

// kickScopes: the account's name and picture (user:read), its channel's slug for links (channel:read), sending
// chat messages (chat:write).
const kickScopes = "user:read channel:read chat:write"

func init() {
	Register(Kick{NewBase("kick", "Kick", AuthOAuth2, Limits{MaxChars: 500})}, StatusAvailable)
}

func (Kick) AuthURL(app App, redirect, state string, extra map[string]string) (string, error) {
	verifier := extra["code_verifier"]
	if verifier == "" {
		return "", errors.New("Kick needs a PKCE code verifier")
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {app.ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {kickScopes},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	return kickID + "/oauth/authorize?" + q.Encode(), nil
}

type kickToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (t kickToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" { // Kick rotates refresh tokens
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

func (Kick) token(ctx context.Context, app App, form url.Values) (kickToken, error) {
	var t kickToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := checkForm(ctx, kickID+"/oauth/token", form)
	if err != nil {
		return t, err
	}
	if err := kickDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Kick returned no access token")
	}
	return t, nil
}

func (k Kick) Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	t, err := k.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {extra["code_verifier"]}})
	if err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	var users struct {
		Data []struct {
			UserID  int64  `json:"user_id"`
			Name    string `json:"name"`
			Picture string `json:"profile_picture"`
		} `json:"data"`
	}
	if err := kickGet(ctx, creds["access_token"], "/public/v1/users", &users); err != nil {
		return nil, Profile{}, err
	}
	if len(users.Data) == 0 || users.Data[0].UserID == 0 {
		return nil, Profile{}, errors.New("Kick did not say which account signed in")
	}
	u := users.Data[0]
	slug := strings.ToLower(u.Name)
	var channels struct {
		Data []struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if kickGet(ctx, creds["access_token"], "/public/v1/channels", &channels) == nil && len(channels.Data) > 0 && channels.Data[0].Slug != "" {
		slug = channels.Data[0].Slug
	}
	creds["user_id"], creds["slug"] = strconv.FormatInt(u.UserID, 10), slug
	return creds, Profile{Name: u.Name, Handle: "@" + slug, AvatarURL: u.Picture, ProfileURL: "https://kick.com/" + slug}, nil
}

func (k Kick) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Kick gave no refresh token; reconnect the account"}
	}
	t, err := k.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// Kick publishes a post's thread as chat replies.
var _ Replier = Kick{}

func (k Kick) Publish(ctx context.Context, c Credentials, p PostContent, _ Settings) (Result, error) {
	return k.chat(ctx, c, p, "")
}

// Reply sends a thread part as a chat reply to the message parentExternalID (the post, or the part before).
func (k Kick) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Kick: nothing to reply to (the post has no id)"}
	}
	return k.chat(ctx, c, p, parentExternalID)
}

func (Kick) chat(ctx context.Context, c Credentials, p PostContent, replyTo string) (Result, error) {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Kick: no account stored; reconnect it"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Kick chat takes text only"}
	}
	id, err := strconv.ParseInt(user, 10, 64)
	if err != nil {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Kick: the stored account is not valid; reconnect it"}
	}
	body := map[string]any{"type": "user", "content": p.Text, "broadcaster_user_id": id}
	if replyTo != "" {
		body["reply_to_message_id"] = replyTo
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kickAPI+"/public/v1/chat", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data struct {
			IsSent    bool   `json:"is_sent"`
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := kickDo(ctx, req, &out); err != nil {
		// Kick takes no idempotency key: sending again after an answer that never came could post twice.
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Kick may have published the message, but its answer was lost (" + err.Error() + "); check the chat before posting it again"}
		}
		return Result{}, err
	}
	if !out.Data.IsSent {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Kick did not send the message to the chat (slow mode, followers-only or a blocked word?)"}
	}
	return Result{ExternalID: out.Data.MessageID, URL: "https://kick.com/" + c["slug"]}, nil
}

func kickGet(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kickAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return kickDo(ctx, req, out)
}

// kickDo sends a request and decodes a 2xx reply into out; Kick's errors become messages a person can act on.
func kickDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Kick could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return kickError(resp.StatusCode, raw)
}

// kickError maps Kick's replies ({"message"} from the API, {"error","error_description"} from the token endpoint)
// to what staqly does next.
func kickError(status int, raw []byte) error {
	var e struct {
		Message     string `json:"message"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = e.Description
	}
	if detail == "" {
		detail = e.Error
	}
	if detail == "" {
		detail = string(raw)
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	switch {
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Kick rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Kick is unavailable (%d); staqly tries again in a few minutes", status)}
	case e.Error == "invalid_client", e.Error == "unauthorized_client":
		// For whoever runs the server: PROVIDER_KICK_CLIENT_ID and _SECRET are on the app's page in Kick's settings.
		return &Failure{Kind: ErrRejected, Msg: "Kick did not accept this server's app (" + detail + ")"}
	case e.Error == "invalid_grant", status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Kick no longer accepts this account's sign-in; reconnect it"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Kick refused the message (" + detail + "); a ban or a chat setting may stop the account from posting"}
	case status >= 400 && status < 500:
		return &Failure{Kind: ErrRejected, Msg: "Kick: " + detail}
	}
	return &Failure{Msg: fmt.Sprintf("Kick answered %d: %s", status, detail)}
}

// Kick gives an app token for the keys alone (client credentials), so the keys are checked for real.
func (Kick) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	req, err := checkForm(ctx, kickID+"/oauth/token", url.Values{"grant_type": {"client_credentials"},
		"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Kick")
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK && strings.Contains(body, "access_token"):
		return true, nil
	case status == http.StatusBadRequest, status == http.StatusUnauthorized, strings.Contains(body, "invalid_client"):
		return false, errors.New("Kick did not accept the app keys: check the Client ID and Client Secret in your Kick developer settings")
	}
	return false, unexpected("Kick", status, body)
}
