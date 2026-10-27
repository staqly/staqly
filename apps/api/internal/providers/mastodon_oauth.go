package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
)

// Signing in to Mastodon: every server keeps its own apps, so PrepareOAuth registers staqly on the person's server
// (POST /api/v1/apps) with this sign-in's return address. The app's id and secret travel sealed in the state to the
// sign-in URL and the exchange; nothing is stored but the account's token, which does not expire.
// ponytail: one app per sign-in, a per-server app cache if a server ever limits registrations.

const mastodonScopes = "write:statuses write:media read:accounts"

// OAuthPreparer is implemented by platforms that need a step before their sign-in URL. What it returns is sealed
// in the state and comes back as extras to AuthURL and Exchange.
type OAuthPreparer interface {
	PrepareOAuth(ctx context.Context, redirect string, extra map[string]string) (map[string]string, error)
}

func (Mastodon) PrepareOAuth(ctx context.Context, redirect string, extra map[string]string) (map[string]string, error) {
	instance, err := fediInstance(extra["instance"], mastodonDefault, "instance")
	if err != nil {
		return nil, err
	}
	form := url.Values{"client_name": {"staqly"}, "redirect_uris": {redirect}, "scopes": {mastodonScopes},
		"website": {"https://staqly.ai"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instance+"/api/v1/apps", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var app struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := mastodonDo(ctx, req, &app); err != nil {
		return nil, err
	}
	if app.ClientID == "" || app.ClientSecret == "" {
		return nil, &Failure{Kind: ErrRejected, Msg: "this server did not answer like Mastodon; check the server's address"}
	}
	return map[string]string{"instance": instance, "client_id": app.ClientID, "client_secret": app.ClientSecret}, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instance+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := mastodonDo(ctx, req, &tok); err != nil {
		return nil, Profile{}, err
	}
	if tok.AccessToken == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Mastodon did not give an access token; connect again"}
	}
	return mastodonSignedIn(ctx, instance, tok.AccessToken)
}
