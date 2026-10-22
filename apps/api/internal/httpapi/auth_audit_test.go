package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// People are managed by people, never by API keys: a key carries its creator's user id (to stay within
// their role), but that never makes it the person. A read-only agent key must not take its creator out
// of the workspace, rename their account or read their sign-in addresses.
func TestAPIKeysNeverActAsTheirCreator(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, _, _ := e.team()
	var members struct {
		Data []app.Member `json:"data"`
	}
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	ids := map[string]string{}
	for _, m := range members.Data {
		ids[m.Email] = m.UserID
	}

	var bobKey app.APIKey
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "reader", "scopes": []string{"posts:read"}}, 201, &bobKey)
	e.do(nil, "DELETE", "/v1/members/"+ids[bob.Email], bobKey.Key, nil, 403, nil)
	e.do(nil, "GET", "/v1/posts", bobKey.Key, nil, 200, nil) // Bob is still a member, so his key still works
	if s := e.session(admin); s.Role != app.RoleAdmin {
		t.Fatalf("bob after his key tried to remove him: %+v", s.Role)
	}

	var anaKey app.APIKey
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "agent", "scopes": []string{"posts:read"}}, 201, &anaKey)
	e.do(nil, "PATCH", "/v1/account", anaKey.Key, map[string]any{"name": "Renamed by a key"}, 403, nil)
	e.do(nil, "GET", "/v1/account/sign-in-methods", anaKey.Key, nil, 403, nil)

	// Nor does it see the creator's other workspaces or their account: only its own workspace.
	var other app.MyWorkspace
	e.as(owner, "POST", "/v1/account/workspaces", map[string]any{"name": "Hidden"}, 201, &other)
	var wss struct {
		Data []app.MyWorkspace `json:"data"`
	}
	e.do(nil, "GET", "/v1/workspaces", anaKey.Key, nil, 200, &wss)
	if len(wss.Data) != 1 || wss.Data[0].ID == other.ID || !wss.Data[0].Current || wss.Data[0].Role != app.RoleOwner {
		t.Fatalf("workspaces seen by a key: %+v", wss.Data)
	}
	e.as(owner, "GET", "/v1/workspaces", nil, 200, &wss)
	if len(wss.Data) != 2 {
		t.Fatalf("workspaces seen by the person: %+v", wss.Data)
	}

	// The people themselves still can.
	e.as(owner, "PATCH", "/v1/account", map[string]any{"name": "Ana's studio"}, 200, nil)
	e.as(owner, "GET", "/v1/account/sign-in-methods", nil, 200, nil)
	e.as(admin, "DELETE", "/v1/members/"+ids[bob.Email], nil, 204, nil)
}

// Local mode without the access token: whatever a browser or a DNS-rebinding page sends (an opaque Origin,
// a page on another localhost port, names that only resolve to this machine, forwarded hosts), there is
// no keyless way in.
func TestLocalGuardEdgeCases(t *testing.T) {
	_, srv := newLocal(t)
	body := `{"name":"x"}`
	for _, c := range []struct{ host, origin string }{
		{"localhost:3333", ""},
		{"localhost:3333", "null"},
		{"localhost:3333", "http://localhost:8080"},
		{"localhost:3333", "http://localhost:5173"},
		{"0.0.0.0:3333", ""},
		{"127.0.0.2:3333", ""},
		{"evil.example", "http://localhost:8080"},
	} {
		if code, b := localReqWith(t, srv, "POST", "/v1/collections", c.host, c.origin, body, ""); code != 401 {
			t.Fatalf("host %q origin %q: %d %s", c.host, c.origin, code, b)
		}
	}
	for _, fwd := range []string{"localhost:8080", "evil.example", ""} {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/collections", nil)
		req.Host = "localhost:3333"
		req.Header["X-Forwarded-Host"] = []string{fwd}
		req.Header.Set(noTokenHeader, "1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("forwarded host %q: %d", fwd, resp.StatusCode)
		}
	}
}

// Admins never touch owners: they neither see nor end the sessions of an owner or the account owner, as a
// person or with an admin key. Owners keep full control.
func TestAdminsCannotSignOutOwners(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, _, _ := e.team()
	_, tok := e.invite(owner, frank.Email, app.RoleOwner)
	second := e.mustSignIn(frank, "invite="+url.QueryEscape(tok))
	type list struct {
		Data []app.Session `json:"data"`
	}
	current := func(p *person) string {
		var l list
		e.as(p, "GET", "/v1/sessions", nil, 200, &l)
		for _, s := range l.Data {
			if s.Current {
				return s.ID
			}
		}
		t.Fatalf("no current session: %+v", l.Data)
		return ""
	}
	anaSess, frankSess, bobSess := current(owner), current(second), current(admin)
	var all, seen list
	e.as(owner, "GET", "/v1/sessions", nil, 200, &all)
	e.as(admin, "GET", "/v1/sessions", nil, 200, &seen)
	if len(all.Data) != 5 || len(seen.Data) != 3 {
		t.Fatalf("owner sees %d sessions, admin %d: %+v", len(all.Data), len(seen.Data), seen.Data)
	}
	other := ""
	for _, s := range seen.Data {
		if s.ID == anaSess || s.ID == frankSess {
			t.Fatalf("admin sees an owner's session: %+v", s)
		}
		if s.ID != bobSess {
			other = s.ID
		}
	}
	var key app.APIKey
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "bob admin", "scopes": []string{"admin"}}, 201, &key)
	var byKey list
	e.do(nil, "GET", "/v1/sessions", key.Key, nil, 200, &byKey)
	if len(byKey.Data) != 3 {
		t.Fatalf("admin key sees %d sessions", len(byKey.Data))
	}
	for _, id := range []string{anaSess, frankSess} {
		e.as(admin, "DELETE", "/v1/sessions/"+id, nil, 403, nil)
		e.do(nil, "DELETE", "/v1/sessions/"+id, key.Key, nil, 403, nil)
	}
	e.as(owner, "GET", "/v1/session", nil, 200, nil) // still signed in
	e.as(admin, "DELETE", "/v1/sessions/"+other, nil, 204, nil)
	// Owners sign out anyone, the other owner included.
	e.as(owner, "DELETE", "/v1/sessions/"+frankSess, nil, 204, nil)
	e.as(owner, "DELETE", "/v1/sessions/"+bobSess, nil, 204, nil)
	e.as(admin, "GET", "/v1/session", nil, 401, nil)
}

// Codes to one address are capped per rolling 24 hours too (on top of 5 an hour), sign-in and link codes
// together, with the hourly cap's answer.
func TestEmailCodesPerDayLimit(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	p := e.mustSignIn(ana, "")
	addr := "daily@example.com"
	for hour := 0; hour < 3; hour++ {
		for i := 0; i < 5; i++ {
			clock.add(31 * time.Second)
			if hour == 0 && i == 0 {
				e.as(p, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": addr}, 202, nil)
				continue
			}
			e.sendCode(addr, 202)
		}
		clock.add(time.Hour)
	}
	var limited apiErr
	e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": addr}, 429, &limited)
	if limited.Error.Code != "rate_limited" {
		t.Fatalf("daily cap: %+v", limited)
	}
	p = e.mustSignIn(ana, "") // the first session expired meanwhile
	e.as(p, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": addr}, 429, nil)
	e.sendCode("other@example.com", 202) // per address
	// The first codes leave the window after 24 hours.
	clock.add(21 * time.Hour)
	e.sendCode(addr, 202)
}
