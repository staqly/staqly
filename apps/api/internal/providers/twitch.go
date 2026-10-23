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

// Twitch over the Helix API: OAuth 2.0 (authorization code) on id.twitch.tv, refreshable tokens, and a "post" is a
// chat message the account sends in its own channel's chat (POST /helix/chat/messages, broadcaster = sender = the
// account). Chat takes text only and a message is not a lasting post: it scrolls away and has no link of its own.
// A post's thread goes out as chat replies. Every Helix call names the app (Client-Id) next to the token.
type Twitch struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	twitchID  = "https://id.twitch.tv"
	twitchAPI = "https://api.twitch.tv"
)

// twitchScopes: send chat messages as the account.
const twitchScopes = "user:write:chat"

func init() {
	Register(Twitch{NewBase("twitch", "Twitch", AuthOAuth2, Limits{MaxChars: 500})}, StatusAvailable)
}

func (Twitch) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"scope":         {twitchScopes},
		"state":         {state},
		"force_verify":  {"true"}, // let the person pick the account instead of reusing the browser's sign-in
	}
	return twitchID + "/oauth2/authorize?" + q.Encode(), nil
}

type twitchToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (t twitchToken) into(c Credentials, now time.Time) Credentials {
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
	return out
}

func (Twitch) token(ctx context.Context, app App, form url.Values) (twitchToken, error) {
	var t twitchToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := checkForm(ctx, twitchID+"/oauth2/token", form)
	if err != nil {
		return t, err
	}
	if err := twitchDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Twitch returned no access token")
	}
	return t, nil
}

func (tw Twitch) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := tw.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	var users struct {
		Data []struct {
			ID          string `json:"id"`
			Login       string `json:"login"`
			DisplayName string `json:"display_name"`
			Picture     string `json:"profile_image_url"`
		} `json:"data"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, twitchAPI+"/helix/users", nil)
	if err != nil {
		return nil, Profile{}, err
	}
	twitchAuth(req, app.ClientID, creds["access_token"])
	if err := twitchDo(ctx, req, &users); err != nil {
		return nil, Profile{}, err
	}
	if len(users.Data) == 0 || users.Data[0].ID == "" {
		return nil, Profile{}, errors.New("Twitch did not say which account signed in")
	}
	u := users.Data[0]
	// Helix wants the app's Client-Id on every call: keep it with the channel, so a publish needs no app.
	creds["user_id"], creds["login"], creds["client_id"] = u.ID, u.Login, app.ClientID
	return creds, Profile{Name: u.DisplayName, Handle: "@" + u.Login, AvatarURL: u.Picture, ProfileURL: "https://www.twitch.tv/" + u.Login}, nil
}

func (tw Twitch) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Twitch gave no refresh token; reconnect the account"}
	}
	t, err := tw.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	out := t.into(c, time.Now())
	out["client_id"] = app.ClientID
	return out, nil
}

// Twitch publishes a post's thread as chat replies.
var _ Replier = Twitch{}

func (tw Twitch) Publish(ctx context.Context, c Credentials, p PostContent, _ Settings) (Result, error) {
	return tw.chat(ctx, c, p, "")
}

// Reply sends a thread part as a chat reply to the message parentExternalID (the post, or the part before).
func (tw Twitch) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Twitch: nothing to reply to (the post has no id)"}
	}
	return tw.chat(ctx, c, p, parentExternalID)
}

func (Twitch) chat(ctx context.Context, c Credentials, p PostContent, replyTo string) (Result, error) {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" || c["client_id"] == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Twitch: no account stored; reconnect it"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Twitch chat takes text only"}
	}
	body := map[string]string{"broadcaster_id": user, "sender_id": user, "message": p.Text}
	if replyTo != "" {
		body["reply_parent_message_id"] = replyTo
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitchAPI+"/helix/chat/messages", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	twitchAuth(req, c["client_id"], token)
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data []struct {
			MessageID  string `json:"message_id"`
			IsSent     bool   `json:"is_sent"`
			DropReason *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"drop_reason"`
		} `json:"data"`
	}
	if err := twitchDo(ctx, req, &out); err != nil {
		// Helix takes no idempotency key: sending again after an answer that never came could post twice.
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Twitch may have published the message, but its answer was lost (" + err.Error() + "); check the chat before posting it again"}
		}
		return Result{}, err
	}
	if len(out.Data) == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Twitch did not say whether the message was sent; check the chat"}
	}
	d := out.Data[0]
	if !d.IsSent {
		why := "AutoMod or a chat setting held it back"
		if d.DropReason != nil && d.DropReason.Message != "" {
			why = d.DropReason.Message
		}
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Twitch did not send the message to the chat (" + why + ")"}
	}
	return Result{ExternalID: d.MessageID, URL: "https://www.twitch.tv/" + c["login"]}, nil
}

func twitchAuth(req *http.Request, clientID, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Id", clientID)
}

// twitchDo sends a request and decodes a 2xx reply into out; Twitch's errors become messages a person can act on.
func twitchDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Twitch could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return twitchError(resp.StatusCode, raw)
}

// twitchError maps Twitch's replies ({"error","status","message"}) to what staqly does next: a sign-in problem
// needs reconnecting, a refused message is final, rate limits and outages are retried.
func twitchError(status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = string(raw)
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	lower := strings.ToLower(detail)
	switch {
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Twitch rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Twitch is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusUnauthorized, strings.Contains(lower, "invalid refresh token"):
		return &Failure{Kind: ErrReauth, Msg: "Twitch no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case strings.Contains(lower, "invalid client"), strings.Contains(lower, "client secret"):
		// For whoever runs the server: PROVIDER_TWITCH_CLIENT_ID and _SECRET are on the app's page in the Twitch
		// developer console.
		return &Failure{Kind: ErrRejected, Msg: "Twitch did not accept this server's app (" + detail + ")"}
	case strings.Contains(lower, "redirect"):
		return &Failure{Kind: ErrRejected, Msg: "Twitch did not accept the return address (" + detail + ")"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Twitch refused the message (" + detail + ")"}
	case status == http.StatusUnprocessableEntity:
		return &Failure{Kind: ErrRejected, Msg: "Twitch: the message is too long (" + detail + ")"}
	case status >= 400 && status < 500:
		return &Failure{Kind: ErrRejected, Msg: "Twitch: " + detail}
	}
	return &Failure{Msg: fmt.Sprintf("Twitch answered %d: %s", status, detail)}
}

// Twitch gives an app token for the keys alone (client credentials), so the keys are checked for real.
func (Twitch) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	req, err := checkForm(ctx, twitchID+"/oauth2/token", url.Values{"grant_type": {"client_credentials"},
		"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Twitch")
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK && strings.Contains(body, "access_token"):
		return true, nil
	case status == http.StatusBadRequest, status == http.StatusForbidden, status == http.StatusUnauthorized:
		return false, errors.New("Twitch did not accept the app keys: check the Client ID and Client Secret in the Twitch developer console")
	}
	return false, unexpected("Twitch", status, body)
}
