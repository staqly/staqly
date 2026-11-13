package httpapi_test

import (
	"net/url"
	"testing"
)

// Was a bug: adding Google to a person was finished by the public callback with nothing but the sealed state,
// so a session signed out meanwhile (a stolen one) still left its holder's Google account as a sign-in method.
func TestGoogleLinkRefusedAfterSessionRevoked(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	me := e.session(owner).User.ID
	stolen := e.mustSignIn(ana, "") // a second session of the same person: the one that leaks
	stolenID := e.session(stolen).Principal.ID

	var start struct {
		URL string `json:"url"`
	}
	e.as(stolen, "POST", "/v1/account/sign-in-methods/google", nil, 200, &start)

	e.as(owner, "DELETE", "/v1/sessions/"+stolenID, nil, 204, nil)
	e.do(stolen.c, "GET", "/v1/session", "", nil, 401, nil) // the session is gone

	mallory := fakeIdentity{Sub: "sub-mallory", Email: "mallory@evil.example", Name: "Mallory"}
	state, code := e.google.authorize(start.URL, mallory, nil)
	resp, err := stolen.c.Get(e.srv.URL + "/v1/auth/google/callback?state=" + url.QueryEscape(state) + "&code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc == "/settings/account?linked=google" {
		t.Errorf("the callback of a revoked session linked Google: %q", loc)
	}
	p, loc := e.signIn(mallory, "", nil)
	if p.csrf != "" && e.session(p).User.ID == me {
		t.Fatalf("Mallory's Google account now signs in as the owner (%s)", loc)
	}
}
