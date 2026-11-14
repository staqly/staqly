package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
	"github.com/staqly/staqly/apps/api/internal/web"
)

// A code takes five tries: the fifth may still be the right one, a fifth wrong one kills it, and what is not a
// code costs no try.
func TestEmailCodeAttemptBoundaries(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	wrongFor := func(code string) string {
		if code == "000000" {
			return "111111"
		}
		return "000000"
	}
	// Four wrong tries count down; a malformed code in between costs nothing; the fifth try, right, signs in.
	e.sendCode("four@example.com", 202)
	code := e.devCode("four@example.com")
	for left := 4; left >= 1; left-- {
		var bad apiErr
		e.verifyCode("four@example.com", wrongFor(code), nil, 401, &bad)
		if bad.Error.Code != "code_invalid" || bad.Error.Details["attempts_left"] != float64(left) {
			t.Fatalf("wrong try with %d left: %+v", left, bad)
		}
		e.verifyCode("four@example.com", "12ab", nil, 422, nil)
	}
	e.verifyCode("four@example.com", code, nil, 201, nil)
	// It worked once.
	var used apiErr
	e.verifyCode("four@example.com", code, nil, 410, &used)
	if used.Error.Code != "code_expired" {
		t.Fatalf("a used code: %+v", used)
	}

	// The fifth wrong try is the last: it and the right code after it answer 410.
	e.sendCode("five@example.com", 202)
	code = e.devCode("five@example.com")
	for range 4 {
		e.verifyCode("five@example.com", wrongFor(code), nil, 401, nil)
	}
	e.verifyCode("five@example.com", wrongFor(code), nil, 410, nil)
	e.verifyCode("five@example.com", code, nil, 410, nil)

	// The cooldown is 30 seconds and the code lives 10 minutes, to the second.
	e.sendCode("time@example.com", 202)
	clock.add(29 * time.Second)
	e.sendCode("time@example.com", 429)
	clock.add(time.Second)
	e.sendCode("time@example.com", 202)
	code = e.devCode("time@example.com")
	clock.add(10*time.Minute - time.Second)
	e.verifyCode("time@example.com", code, nil, 201, nil)
	clock.add(31 * time.Second)
	e.sendCode("late@example.com", 202)
	code = e.devCode("late@example.com")
	clock.add(10 * time.Minute)
	e.verifyCode("late@example.com", code, nil, 410, nil)
}

// An invitation works for seven days, for its address, once; a removed member's old link is no way back in, and
// an admin never handles an invitation that makes an owner.
func TestInvitationBoundaries(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	owner := e.mustSignIn(ana, "")
	if err := e.svc.SetPlan(context.Background(), e.session(owner).Workspace.ID, "pro", ""); err != nil { // the trial is a week too
		t.Fatal(err)
	}
	lookup := func(token string) string {
		var prev app.InvitationPreview
		e.do(nil, "GET", "/v1/invitations/lookup?token="+url.QueryEscape(token), "", nil, 200, &prev)
		return prev.Status
	}
	tokenOf := func(inv app.Invitation) string {
		u, _ := url.Parse(inv.Link)
		return u.Query().Get("token")
	}

	// One minute before the week is up it is open; at the week, expired, for the lookup, for a signed-in person
	// and at sign-in. Nobody joined.
	inv, token := e.invite(owner, bob.Email, app.RoleEditor)
	clock.add(7*24*time.Hour - time.Minute)
	if got := lookup(token); got != "open" {
		t.Fatalf("a minute before the week: %s", got)
	}
	clock.add(time.Minute)
	if got := lookup(token); got != "expired" {
		t.Fatalf("at the week: %s", got)
	}
	owner = e.mustSignIn(ana, "") // sessions last an hour here
	bobP := e.mustSignIn(bob, "") // his own account, not yet a member
	var expired apiErr
	e.as(bobP, "POST", "/v1/invitations/accept", map[string]any{"token": token}, 410, &expired)
	if expired.Error.Code != "invitation_expired" {
		t.Fatalf("accepting an expired invitation: %+v", expired)
	}
	var members struct{ Data []app.Member }
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	if len(members.Data) != 1 {
		t.Fatalf("members after an expired invitation: %+v", members.Data)
	}
	// Resend: a new week and a new link; the expired link stays dead.
	var again app.Invitation
	e.as(owner, "POST", "/v1/invitations/"+inv.ID+"/resend", nil, 200, &again)
	e.as(bobP, "POST", "/v1/invitations/accept", map[string]any{"token": token}, 410, nil)
	var ws app.MyWorkspace
	e.as(bobP, "POST", "/v1/invitations/accept", map[string]any{"token": tokenOf(again)}, 200, &ws)
	if ws.Role != app.RoleEditor {
		t.Fatalf("accepted as: %+v", ws)
	}

	// Used: someone else gets nothing from it, not even with the invited address's link in hand.
	eveP := e.mustSignIn(eve, "")
	e.as(eveP, "POST", "/v1/invitations/accept", map[string]any{"token": tokenOf(again)}, 403, nil)
	// Removed from the workspace, Bob's accepted link does not let him back in.
	e.as(owner, "DELETE", "/v1/members/"+e.session(bobP).User.ID, nil, 204, nil)
	bobP = e.mustSignIn(bob, "")
	var gone apiErr
	e.as(bobP, "POST", "/v1/invitations/accept", map[string]any{"token": tokenOf(again)}, 410, &gone)
	if gone.Error.Code != "invitation_invalid" {
		t.Fatalf("a removed member's old link: %+v", gone)
	}
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	if len(members.Data) != 1 {
		t.Fatalf("members after the old link: %+v", members.Data)
	}

	// Revoked: dead for a signed-in person too.
	inv2, token2 := e.invite(owner, cleo.Email, app.RoleViewer)
	e.as(owner, "DELETE", "/v1/invitations/"+inv2.ID, nil, 204, nil)
	if got := lookup(token2); got != "revoked" {
		t.Fatalf("revoked: %s", got)
	}
	cleoP := e.mustSignIn(cleo, "")
	e.as(cleoP, "POST", "/v1/invitations/accept", map[string]any{"token": token2}, 410, nil)
	e.as(owner, "POST", "/v1/invitations/"+inv2.ID+"/resend", nil, 409, nil) // closed for good

	// An admin invites up to admin, and neither re-sends nor revokes an invitation that makes an owner.
	_, adminToken := e.invite(owner, dan.Email, app.RoleAdmin)
	adminP := e.mustSignIn(dan, "invite="+url.QueryEscape(adminToken))
	ownerInv, _ := e.invite(owner, frank.Email, app.RoleOwner)
	e.as(adminP, "POST", "/v1/invitations", map[string]any{"email": "o@example.com", "role": app.RoleOwner}, 403, nil)
	e.as(adminP, "POST", "/v1/invitations/"+ownerInv.ID+"/resend", nil, 403, nil)
	e.as(adminP, "DELETE", "/v1/invitations/"+ownerInv.ID, nil, 403, nil)
	e.as(adminP, "POST", "/v1/invitations", map[string]any{"email": "a@example.com", "role": app.RoleAdmin}, 201, nil)
	e.as(owner, "DELETE", "/v1/invitations/"+ownerInv.ID, nil, 204, nil)
}

// A workspace keeps an owner: the last one neither steps down nor leaves, whoever asks.
func TestLastOwnerRules(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, _, _ := e.team()
	me, bobID := e.session(owner).User.ID, e.session(admin).User.ID
	var last apiErr
	e.as(owner, "PATCH", "/v1/members/"+me, map[string]any{"role": app.RoleAdmin}, 409, &last)
	if last.Error.Code != "last_owner" {
		t.Fatalf("the last owner stepping down: %+v", last)
	}
	e.as(owner, "DELETE", "/v1/members/"+me, nil, 409, &last)
	if last.Error.Code != "last_owner" {
		t.Fatalf("the last owner leaving: %+v", last)
	}
	if s := e.session(owner); s.Role != app.RoleOwner {
		t.Fatalf("still the owner: %+v", s.Role)
	}
	// With a second owner, either may go; then the one left is the last again.
	e.as(owner, "PATCH", "/v1/members/"+bobID, map[string]any{"role": app.RoleOwner}, 200, nil)
	e.as(admin, "PATCH", "/v1/members/"+me, map[string]any{"role": app.RoleViewer}, 200, nil) // Bob, an owner now, demotes Ana
	e.as(admin, "PATCH", "/v1/members/"+bobID, map[string]any{"role": app.RoleAdmin}, 409, &last)
	e.as(admin, "DELETE", "/v1/members/"+bobID, nil, 409, &last)
	e.as(owner, "DELETE", "/v1/members/"+bobID, nil, 403, nil) // Ana, a viewer now, removes nobody
	// An owner leaving while another stays: fine, and their session there ends.
	e.as(admin, "PATCH", "/v1/members/"+me, map[string]any{"role": app.RoleOwner}, 200, nil)
	e.as(admin, "DELETE", "/v1/members/"+bobID, nil, 204, nil)
	e.as(admin, "GET", "/v1/session", nil, 401, nil)
	if s := e.session(owner); s.Role != app.RoleOwner {
		t.Fatalf("the owner who stayed: %+v", s.Role)
	}
}

// The per-address limits key on the address the server can trust: the socket's without TRUST_PROXY (a client's
// X-Forwarded-For buys nothing), the right-most X-Forwarded-For entry with it, and the embedded app's relay
// only with its token.
func TestRateLimitKeysPerClientAddress(t *testing.T) {
	const perHour = 20 // email codes one address may ask for (httpapi.New)
	n := 0
	for _, c := range []struct {
		name    string
		cfg     httpapi.Config
		hdr     func(i int) []string // the headers of the i-th request
		limited bool                 // the 21st request is refused
	}{
		{"no proxy: X-Forwarded-For is the client's word", httpapi.Config{},
			func(i int) []string { return []string{"X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i)} }, true},
		{"trusted proxy: one client behind it", httpapi.Config{TrustProxy: true},
			func(i int) []string { return []string{"X-Forwarded-For", fmt.Sprintf("203.0.113.%d, 198.51.100.7", i)} }, true},
		{"trusted proxy: a client each", httpapi.Config{TrustProxy: true},
			func(i int) []string { return []string{"X-Forwarded-For", fmt.Sprintf("198.51.100.7, 203.0.113.%d", i)} }, false},
		{"the app's relay names the browser", httpapi.Config{RelayToken: "relay-secret"},
			func(i int) []string {
				return []string{web.RelayHeader, "relay-secret", "X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i)}
			}, false},
		{"a wrong relay token names nobody", httpapi.Config{RelayToken: "relay-secret"},
			func(i int) []string {
				return []string{web.RelayHeader, "guess", "X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i)}
			}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			database, err := storetest.Open(t, t.TempDir()+"/workspace.db")
			if err != nil {
				t.Fatal(err)
			}
			e := newAccountsEnvCfg(t, "", database, c.cfg)
			e.svc.Cfg.EmailSignIn, e.svc.Cfg.DevSignIn = true, true
			ask := func(i int) int {
				n++
				return e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": fmt.Sprintf("p%d@example.com", n)}, 0, nil, c.hdr(i)...).StatusCode
			}
			for i := range perHour {
				if got := ask(i); got != 202 {
					t.Fatalf("request %d: %d", i, got)
				}
			}
			if got := ask(perHour); (got == http.StatusTooManyRequests) != c.limited {
				t.Fatalf("request %d: %d, limited should be %v", perHour, got, c.limited)
			}
		})
	}
}

// The person behind every credential is read on each request: a demotion lowers their sessions, their keys and
// their MCP calls at once; a removal ends them.
func TestDemotionAndRemovalReachEveryCredential(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, _, _ := e.team()
	bobID := e.session(admin).User.ID
	var key app.APIKey
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "bob", "scopes": []string{app.ScopeAdmin}}, 201, &key)
	second := e.mustSignIn(bob, "") // another browser of Bob's
	check := func(when string, session, write, manage int, toolRefused bool) {
		t.Helper()
		for _, p := range []*person{admin, second} {
			e.as(p, "GET", "/v1/session", nil, session, nil)
			if session == 200 {
				e.as(p, "POST", "/v1/collections", map[string]any{"name": when + p.csrf[:6]}, write, nil)
				e.as(p, "GET", "/v1/members", nil, manage, nil)
			}
		}
		e.do(nil, "POST", "/v1/collections", key.Key, map[string]any{"name": when + " key"}, write, nil)
		e.do(nil, "GET", "/v1/members", key.Key, nil, manage, nil)
		code, out := mcpCall(e.env, key.Key, "create_collection", map[string]any{"name": when + " tool"})
		if refused := code != 200 || isToolError(out); refused != toolRefused {
			t.Fatalf("%s: MCP create_collection refused = %v (%d %v)", when, refused, code, out)
		}
	}
	var link app.DeviceLink
	e.as(second, "POST", "/v1/device-links", nil, 201, &link) // a link Bob made while he was a member
	check("admin", 200, 201, 200, false)
	e.as(owner, "PATCH", "/v1/members/"+bobID, map[string]any{"role": app.RoleEditor}, 200, nil)
	check("editor", 200, 201, 403, false)
	e.as(owner, "PATCH", "/v1/members/"+bobID, map[string]any{"role": app.RoleViewer}, 200, nil)
	check("viewer", 200, 403, 403, true)
	e.as(owner, "DELETE", "/v1/members/"+bobID, nil, 204, nil)
	check("removed", 401, 401, 401, true)
	var dead apiErr
	e.do(noRedirects(nil), "POST", "/v1/device-links/redeem", "", map[string]any{"token": link.URL[strings.Index(link.URL, "#")+1:]}, 401, &dead)
	if dead.Error.Code != "invalid_link" {
		t.Fatalf("a removed member's connect link: %+v", dead)
	}
	// The key and the sessions are revoked, not only refused: putting Bob back does not bring them back.
	_, token := e.invite(owner, bob.Email, app.RoleAdmin)
	e.mustSignIn(bob, "invite="+url.QueryEscape(token))
	e.do(nil, "GET", "/v1/posts", key.Key, nil, 401, nil)
	e.as(admin, "GET", "/v1/session", nil, 401, nil)
	e.as(second, "GET", "/v1/session", nil, 401, nil)
}
