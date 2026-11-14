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
	// Reopened from the user menu and finished again with a name: the person (the sidebar) takes it.
	e.as(owner, "POST", "/v1/session/onboarded", map[string]any{"name": "Ana Creator"}, 204, nil)
	if s := e.session(owner); s.User.Name != "Ana Creator" {
		t.Fatalf("name: %+v", s.User)
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

// A local install never gets the welcome: its empty pages guide instead.
func TestOnboardingLocal(t *testing.T) {
	_, srv := newLocal(t)
	if code, body := localReq(t, srv, "GET", "/v1/session", "", "", ""); code != 200 || !onboarded(t, body) {
		t.Fatalf("fresh install: %d %s", code, body)
	}
}
