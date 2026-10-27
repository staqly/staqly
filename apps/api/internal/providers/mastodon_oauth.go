package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Signing in to Mastodon: every server keeps its own apps, so staqly registers itself on the person's server
// (POST /api/v1/apps) once per server and return address; the service keeps that client (oauth_clients) and hands
// its id and secret, sealed in the state, to the sign-in URL and the exchange. The account's token is all a channel
// stores; Mastodon's tokens don't expire.

const mastodonScopes = "write:statuses write:media read:accounts"

// OAuthPreparer is implemented by platforms whose OAuth client is registered on each of their servers (Mastodon).
// The service asks for the server a sign-in goes to, reuses the client it registered there while the server still
// knows it, and else registers one. The client's values, with "instance", reach AuthURL and Exchange as extras.
type OAuthPreparer interface {
	OAuthServer(extra map[string]string) (string, error)
	RegisterClient(ctx context.Context, server, redirect string) (map[string]string, error)
	// ClientWorks reports whether the server still knows a client registered earlier (servers delete unused ones).
	ClientWorks(ctx context.Context, server string, client map[string]string) bool
}

func (Mastodon) OAuthServer(extra map[string]string) (string, error) {
	return fediInstance(extra["instance"], mastodonDefault, "instance")
}

func (Mastodon) RegisterClient(ctx context.Context, instance, redirect string) (map[string]string, error) {
	form := url.Values{"client_name": {"staqly"}, "redirect_uris": {redirect}, "scopes": {mastodonScopes},
		"website": {"https://staqly.ai"}}
	var app struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := mastodonForm(ctx, instance+"/api/v1/apps", form, &app); err != nil {
		return nil, err
	}
	if app.ClientID == "" || app.ClientSecret == "" {
		return nil, &Failure{Kind: ErrRejected, Msg: "this server did not answer like Mastodon; check the server's address"}
	}
	return map[string]string{"client_id": app.ClientID, "client_secret": app.ClientSecret}, nil
}

// ClientWorks asks for an app token (client credentials): only a refused client is reported gone, so a server that
// is down keeps its client and the sign-in page says what is wrong.
func (Mastodon) ClientWorks(ctx context.Context, instance string, client map[string]string) bool {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {client["client_id"]},
		"client_secret": {client["client_secret"]}, "scope": {"read:accounts"}}
	err := mastodonForm(ctx, instance+"/oauth/token", form, nil)
	var f *Failure
	return err == nil || !errors.As(err, &f) || (f.Kind != ErrReauth && f.Kind != ErrRejected)
}

func mastodonForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return mastodonDo(ctx, req, out)
}

func (Mastodon) AuthURL(_ App, redirect, state string, extra map[string]string) (string, error) {
	if extra["instance"] == "" || extra["client_id"] == "" {
		return "", invalid("instance", "enter your Mastodon server")
	}
	q := url.Values{"response_type": {"code"}, "client_id": {extra["client_id"]}, "redirect_uri": {redirect},
		"scope": {mastodonScopes}, "state": {state}}
	if v := extra["code_verifier"]; v != "" { // servers without PKCE ignore it
		sum := sha256.Sum256([]byte(v))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("code_challenge_method", "S256")
	}
	return extra["instance"] + "/oauth/authorize?" + q.Encode(), nil
}

func (Mastodon) Exchange(ctx context.Context, _ App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	instance := extra["instance"]
	if instance == "" || extra["client_id"] == "" {
		return nil, Profile{}, invalid("state", "is invalid")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {extra["client_id"]},
		"client_secret": {extra["client_secret"]}, "redirect_uri": {redirect}, "scope": {mastodonScopes}}
	if v := extra["code_verifier"]; v != "" {
		form.Set("code_verifier", v)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := mastodonForm(ctx, instance+"/oauth/token", form, &tok); err != nil {
		return nil, Profile{}, err
	}
	if tok.AccessToken == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Mastodon did not give an access token; connect again"}
	}
	return mastodonSignedIn(ctx, instance, tok.AccessToken)
}
