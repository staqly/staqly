package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// AppChecker is implemented by platforms that can be asked, without any account, whether the server's app keys
// work. verified is false when the platform answered but only checks the keys once an account connects.
type AppChecker interface {
	CheckApp(ctx context.Context, app App, redirect string) (verified bool, err error)
}

// Needs is what a platform asks of the server beyond its app keys.
type Needs struct {
	HTTPS       bool // the return address registered with the platform must be https
	PublicMedia bool // the platform downloads a post's media from a link
	// BotToken: beside the OAuth keys, the server's own bot token (PROVIDER_<ID>_BOT_TOKEN) does the posting, so the
	// app is the server's alone (a workspace can't bring its own) and the platform waits for both.
	BotToken bool
	// OwnClient: the server is its own OAuth client (Bluesky): no keys to register, its address is the client id.
	OwnClient bool
}

// AuthURLContexter is implemented by platforms whose sign-in URL needs the network (Bluesky pushes the request first).
type AuthURLContexter interface {
	AuthURLContext(ctx context.Context, app App, redirect, state string, extra map[string]string) (string, error)
}

// Needer is implemented by platforms with Needs.
type Needer interface{ Needs() Needs }

// checkCode is the made-up sign-in code a check sends: the platform refuses it, and how says whether it took the keys.
const checkCode = "staqly-setup-check"

// probe sends a check request and returns the platform's answer (status and body), or why it could not be reached.
func probe(ctx context.Context, req *http.Request, name string) (int, string, error) {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("%s could not be reached (%s)", name, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, string(raw), nil
}

func checkForm(ctx context.Context, endpoint string, form url.Values) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func unexpected(name string, status int, body string) error {
	if len(body) > 200 {
		body = body[:200] + "..."
	}
	return fmt.Errorf("%s answered %d: %s", name, status, body)
}

// X takes the keys before the code: a made-up code with good keys is "invalid_request", with bad ones "unauthorized_client".
func (X) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {checkCode}, "redirect_uri": {redirect}, "code_verifier": {checkCode}}
	if app.ClientSecret == "" {
		form.Set("client_id", app.ClientID)
	}
	req, err := checkForm(ctx, xAPI+"/2/oauth2/token", form)
	if err != nil {
		return false, err
	}
	if app.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret))
	}
	status, body, err := probe(ctx, req, "X")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "unauthorized_client"), strings.Contains(body, "invalid_client"):
		return false, fmt.Errorf("X did not accept the app keys: check the Client ID and Client Secret (%s)", xDetail(body))
	case strings.Contains(body, "invalid_request"), strings.Contains(body, "invalid_grant"):
		return true, nil
	}
	return false, unexpected("X", status, body)
}

// LinkedIn checks a sign-in code before the keys, so the check asks for an app token instead: refused for the app
// ("access_denied") means the keys are right, "invalid_client" that they are not.
func (LinkedIn) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	req, err := checkForm(ctx, linkedinOAuth+"/accessToken", url.Values{"grant_type": {"client_credentials"},
		"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "LinkedIn")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client"):
		return false, fmt.Errorf("LinkedIn did not accept the app keys: check the Client ID and Primary Client Secret on the app's Auth tab")
	case status == http.StatusOK, strings.Contains(body, "access_denied"):
		return true, nil
	}
	return false, unexpected("LinkedIn", status, body)
}

// Instagram checks a sign-in code before the keys and has no app token for these apps: the check only finds out that
// it answers; the keys are proven when an account connects.
func (Instagram) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, instagramToken, url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret},
		"grant_type": {"authorization_code"}, "redirect_uri": {redirect}, "code": {checkCode}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Instagram")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "authorization code"):
		return false, nil
	}
	return false, unexpected("Instagram", status, body)
}

// Instagram calls back over https only and downloads every picture and video itself.
func (Instagram) Needs() Needs { return Needs{HTTPS: true, PublicMedia: true} }
