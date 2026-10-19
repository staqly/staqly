package httpapi_test

import (
	"encoding/json"
	"testing"
)

func onboarded(t *testing.T, body string) bool {
	t.Helper()
	var s struct{ Onboarded bool }
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("session: %v %s", err, body)
	}
	return s.Onboarded
}

// The welcome wizard runs once per person in an account; finishing it can set how they're called.
func TestOnboardingAccounts(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	var s struct {
		Onboarded bool
		User      struct{ Name string }
	}
	e.as(owner, "GET", "/v1/session", nil, 200, &s)
	if s.Onboarded {
		t.Fatalf("a new person starts with the welcome: %+v", s)
	}
	e.as(owner, "POST", "/v1/session/onboarded", map[string]any{"name": "  "}, 204, nil)
	e.as(owner, "GET", "/v1/session", nil, 200, &s)
	if !s.Onboarded {
		t.Fatalf("finished: %+v", s)
	}
	// Reopened from the user menu and finished again with a name: the account (the sidebar) takes it.
	e.as(owner, "POST", "/v1/session/onboarded", map[string]any{"name": "Ana Creator"}, 204, nil)
	var acc struct{ Name string }
	e.as(owner, "GET", "/v1/account", nil, 200, &acc)
	if acc.Name != "Ana Creator" {
		t.Fatalf("account name: %+v", acc)
	}
	e.as(owner, "POST", "/v1/session/onboarded", map[string]any{"name": string(make([]byte, 101))}, 422, nil)
}

// A session without a person (cloud, development API-key sign-in) never sees the welcome.
func TestOnboardingWithoutPerson(t *testing.T) {
	e := newEnv(t)
	var s struct{ Onboarded bool }
	e.do(e.browser, "GET", "/v1/session", "", nil, 200, &s)
	if !s.Onboarded {
		t.Fatal("a session without a person is never sent to the welcome")
	}
}

// A fresh local install gets the welcome until it is finished or the workspace has channels or posts.
func TestOnboardingLocal(t *testing.T) {
	_, srv := newLocal(t)
	if code, body := localReq(t, srv, "GET", "/v1/session", "", "", ""); code != 200 || onboarded(t, body) {
		t.Fatalf("fresh install: %d %s", code, body)
	}
	if code, body := localReq(t, srv, "POST", "/v1/session/onboarded", "", "", `{}`); code != 204 {
		t.Fatalf("finish: %d %s", code, body)
	}
	if _, body := localReq(t, srv, "GET", "/v1/session", "", "", ""); !onboarded(t, body) {
		t.Fatalf("finished: %s", body)
	}

	_, srv2 := newLocal(t)
	if code, body := localReq(t, srv2, "POST", "/v1/posts", "", "", `{"collection":"Default","content":"first"}`); code != 201 {
		t.Fatalf("post: %d %s", code, body)
	}
	if _, body := localReq(t, srv2, "GET", "/v1/session", "", "", ""); !onboarded(t, body) {
		t.Fatalf("a workspace with posts is past the welcome: %s", body)
	}
}
