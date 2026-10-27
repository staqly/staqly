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
	"strings"
	"time"
)

// Google's OAuth 2.0, shared by YouTube and Google Business Profile: the person signs in on accounts.google.com
// ("offline" access with the consent screen every time, so Google always hands out a refresh token), the code is
// traded at oauth2.googleapis.com (with PKCE), and the hour-long access token is renewed with the refresh token.
// Google accepts http only for localhost return addresses; anything else must be https (the Cloud console refuses
// to save it otherwise, so a server on a LAN address needs https).

// Endpoints, variables so tests point them at httptest.
var (
	googleAuthorize = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL  = "https://oauth2.googleapis.com/token"
	googleUserinfo  = "https://openidconnect.googleapis.com/v1/userinfo"
)

// googleIdentity are the scopes that let staqly read who signed in.
const googleIdentity = "openid email profile"

// googleAuthURL sends the person to Google's consent screen.
func googleAuthURL(app App, redirect, state, scopes string, extra map[string]string) string {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"scope":         {scopes},
		"state":         {state},
		"access_type":   {"offline"}, // a refresh token
		// consent every time, or a second sign-in gets no refresh token; the account picker, so another account connects
		"prompt": {"consent select_account"},
	}
	if v := extra["code_verifier"]; v != "" {
		sum := sha256.Sum256([]byte(v))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("code_challenge_method", "S256")
	}
	return googleAuthorize + "?" + q.Encode()
}

type googleToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// googleTokenRequest calls the token endpoint (a code or a refresh token in form).
func googleTokenRequest(ctx context.Context, name string, app App, form url.Values) (googleToken, error) {
	var t googleToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := googleDo(ctx, name, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New(name + " returned no access token")
	}
	return t, nil
}

func (t googleToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" { // a refresh answers without one: keep the one from the sign-in
		out["refresh_token"] = t.RefreshToken
	}
	if t.Scope != "" {
		out["scope"] = t.Scope
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

// googleExchange trades the sign-in code for tokens, checks that the person allowed scope (Google lets people untick
// permissions on its consent screen) and reads who signed in.
func googleExchange(ctx context.Context, name string, app App, redirect, code string, extra map[string]string, scope, missing string) (Credentials, Profile, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}}
	if v := extra["code_verifier"]; v != "" {
		form.Set("code_verifier", v)
	}
	t, err := googleTokenRequest(ctx, name, app, form)
	if err != nil {
		return nil, Profile{}, err
	}
	if t.Scope != "" && !strings.Contains(" "+t.Scope+" ", " "+scope+" ") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: missing}
	}
	var me struct {
		Sub     string `json:"sub"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture string `json:"picture"`
	}
	req, err := googleRequest(ctx, t.AccessToken, http.MethodGet, googleUserinfo, nil)
	if err != nil {
		return nil, Profile{}, err
	}
	if _, err := googleDo(ctx, name, req, &me); err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	creds["google_id"] = me.Sub
	return creds, Profile{Handle: me.Email, Name: me.Name, AvatarURL: me.Picture}, nil
}

// googleRefresh renews the access token. Google answers a revoked or expired refresh token (the person removed
// the app, changed their password, or the app is in testing, whose refresh tokens last 7 days) with invalid_grant.
func googleRefresh(ctx context.Context, name string, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: name + " gave no refresh token; reconnect the account"}
	}
	t, err := googleTokenRequest(ctx, name, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// googleCheckApp asks the token endpoint to trade a made-up code: Google checks the keys first, so good keys are
// answered "invalid_grant" (the code) and bad ones "invalid_client".
func googleCheckApp(ctx context.Context, name string, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, googleTokenURL, url.Values{"grant_type": {"authorization_code"}, "code": {checkCode},
		"redirect_uri": {redirect}, "client_id": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, name)
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client"), strings.Contains(body, "unauthorized_client"):
		return false, fmt.Errorf("%s did not accept the app keys: check the Client ID and Client secret of the OAuth client in the Google Cloud console", name)
	case strings.Contains(body, "redirect_uri_mismatch"):
		return false, fmt.Errorf("%s did not accept the return address: add %s to the OAuth client's Authorized redirect URIs", name, redirect)
	case strings.Contains(body, "invalid_grant"):
		return true, nil
	}
	return false, unexpected(name, status, body)
}

// googleRequest builds an authorized request (a nil body sends none, anything else goes as JSON).
func googleRequest(ctx context.Context, token, method, endpoint string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	return req, nil
}

// googleGet reads one resource into out.
func googleGet(ctx context.Context, name, token, endpoint string, out any) error {
	req, err := googleRequest(ctx, token, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	_, err = googleDo(ctx, name, req, out)
	return err
}

// googleDo sends a request, decodes a 2xx reply into out and returns its headers; Google's errors become messages a
// person can act on. A request that never got an answer is a plain error (not a Failure), so a publish can tell a
// lost answer apart.
func googleDo(ctx context.Context, name string, req *http.Request, out any) (http.Header, error) {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// The error quotes the URL, which for an upload is the upload session itself: name the host only.
		return nil, fmt.Errorf("%s could not be reached (%s)", name, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return nil, err
			}
		}
		return resp.Header, nil
	}
	return nil, googleError(name, resp.StatusCode, raw)
}

// googleError maps Google's replies to what staqly does next: a sign-in problem needs reconnecting, a refused post
// is final, rate limits and outages are retried. The token endpoint answers {"error":"invalid_grant",
// "error_description"}; the APIs answer {"error":{"code","message","status","errors":[{"reason"}],"details":[{"reason"}]}}.
func googleError(name string, status int, raw []byte) error {
	var e struct {
		Error            json.RawMessage `json:"error"`
		ErrorDescription string          `json:"error_description"`
		ErrorSubtype     string          `json:"error_subtype"`
	}
	_ = json.Unmarshal(raw, &e)
	var token string
	var api struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		Errors  []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
		Details []struct {
			Reason string `json:"reason"`
		} `json:"details"`
	}
	if json.Unmarshal(e.Error, &token) != nil {
		_ = json.Unmarshal(e.Error, &api)
	}
	reasons := map[string]bool{api.Status: true}
	for _, r := range api.Errors {
		reasons[r.Reason] = true
	}
	for _, r := range api.Details {
		reasons[r.Reason] = true
	}
	detail := api.Message
	if detail == "" {
		detail = e.ErrorDescription
	}
	if detail == "" {
		detail = token
	}
	if detail == "" {
		detail = string(raw)
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	switch {
	// The token endpoint: the app's keys, the return address or the sign-in, not the API.
	case token == "invalid_client", token == "unauthorized_client":
		// For whoever runs the server: PROVIDER_<ID>_CLIENT_ID and _SECRET are the OAuth client's ID and secret in the
		// Google Cloud console (APIs & Services -> Credentials).
		return &Failure{Kind: ErrRejected, Msg: name + " did not accept this server's app (" + detail + ")"}
	case token == "redirect_uri_mismatch":
		// For whoever runs the server: the OAuth client's Authorized redirect URIs need <dashboard>/channels/callback/<id>.
		return &Failure{Kind: ErrRejected, Msg: name + " did not accept the return address (" + detail + ")"}
	case token == "invalid_grant" && (e.ErrorSubtype == "invalid_rapt" || strings.Contains(detail, "rapt")):
		return &Failure{Kind: ErrReauth, Msg: name + ": your Google Workspace's session policy signed the app out; reconnect the account, and ask the Workspace admin to mark the app as trusted so it stays signed in"}
	case token == "invalid_grant":
		return &Failure{Kind: ErrReauth, Msg: name + " no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case token != "":
		return &Failure{Kind: ErrRejected, Msg: name + " did not accept the sign-in (" + detail + "); connect again"}
	case reasons["youtubeSignupRequired"]: // answered 401, but reconnecting the same account would not help
		return &Failure{Kind: ErrRejected, Msg: "This Google account has no YouTube channel yet: create one on youtube.com, then reconnect the account"}
	case status == http.StatusUnauthorized, reasons["UNAUTHENTICATED"], reasons["authError"]:
		return &Failure{Kind: ErrReauth, Msg: name + " no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case reasons["ACCESS_TOKEN_SCOPE_INSUFFICIENT"], reasons["insufficientPermissions"]:
		return &Failure{Kind: ErrReauth, Msg: name + " says the sign-in did not allow this (" + detail + "); reconnect the account and allow everything Google asks for"}
	case reasons["SERVICE_DISABLED"], reasons["accessNotConfigured"]:
		// For whoever runs the server: enable the API named in the message in the Google Cloud project of the OAuth
		// client (APIs & Services -> Library).
		return &Failure{Kind: ErrRejected, Msg: name + " is not turned on for this server's app (" + detail + ")"}
	case reasons["quotaExceeded"], reasons["dailyLimitExceeded"], reasons["uploadLimitExceeded"]:
		// For whoever runs the server: YouTube gives every Cloud project 10,000 quota units a day (an upload costs
		// 1,600); more needs a quota extension request.
		return &Failure{Kind: ErrRejected, Msg: name + "'s daily limit is used up (" + detail + "); it resets at midnight Pacific time, so schedule the post for later"}
	case status == http.StatusTooManyRequests, reasons["RESOURCE_EXHAUSTED"], reasons["rateLimitExceeded"],
		reasons["userRateLimitExceeded"], reasons["RATE_LIMIT_EXCEEDED"]:
		return &Failure{Msg: name + " rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("%s is unavailable (%d); staqly tries again in a few minutes", name, status)}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: name + " refused it (" + detail + ")"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: name + " could not find it (" + detail + ")"}
	case status >= 400:
		return &Failure{Kind: ErrRejected, Msg: name + ": " + detail}
	}
	return &Failure{Msg: fmt.Sprintf("%s answered %d: %s", name, status, detail)}
}

// googleCopy copies credentials and adds key=value.
func googleCopy(c Credentials, key, value string) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out[key] = value
	return out
}
