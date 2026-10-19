package httpapi_test

import (
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// testClock: tests move time forward (cooldowns, expiry) without sleeping.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newEmailEnv: cloud accounts with Google and email codes; AUTH_DEV_SIGNIN shows codes to the test.
func newEmailEnv(t *testing.T, setupToken string) (*acctEnv, *testClock) {
	e := newAccountsEnv(t, setupToken)
	e.svc.Cfg.EmailSignIn, e.svc.Cfg.DevSignIn = true, true
	clock := &testClock{t: time.Now().UTC()}
	e.svc.Now = clock.now
	return e, clock
}

type apiErr struct {
	Error struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func (e *acctEnv) sendCode(email string, want int) app.EmailCodeSent {
	e.t.Helper()
	var out app.EmailCodeSent
	e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": email}, want, &out)
	return out
}

func (e *acctEnv) devCode(email string) string {
	e.t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	e.do(nil, "GET", "/v1/auth/email/dev-code?email="+url.QueryEscape(email), "", nil, 200, &out)
	return out.Code
}

// verifyCode signs a new browser in with a code; it returns the person and the session cookie.
func (e *acctEnv) verifyCode(email, code string, extra map[string]any, want int, out any) (*person, string) {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	c := noRedirects(jar)
	body := map[string]any{"email": email, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	var res struct {
		CSRF string `json:"csrf_token"`
		Next string `json:"next"`
	}
	resp := e.do(c, "POST", "/v1/auth/email/verify", "", body, want, out)
	if want != 201 {
		return nil, ""
	}
	cookie := ""
	for _, ck := range resp.Cookies() {
		if ck.Name == "staqly_session" {
			cookie = ck.Value
		}
	}
	p := &person{c: c, id: fakeIdentity{Email: email}}
	e.do(c, "GET", "/v1/session", "", nil, 200, &res)
	p.csrf = res.CSRF
	return p, cookie
}

// emailSignIn: the whole flow for one address (send, read the code, verify).
func (e *acctEnv) emailSignIn(email string, extra map[string]any) *person {
	e.t.Helper()
	e.sendCode(email, 202)
	p, _ := e.verifyCode(email, e.devCode(email), extra, 201, nil)
	return p
}

func TestEmailSignInCreatesAccountThenSignsInAgain(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	var st map[string]any
	e.do(nil, "GET", "/v1/setup", "", nil, 200, &st)
	if st["email_sign_in"] != true || st["google_sign_in"] != true || st["legacy_sign_in"] != true {
		t.Fatalf("setup: %v", st)
	}
	sent := e.sendCode(" Zoe@Example.com ", 202)
	if sent.ResendIn != 30 || sent.ExpiresIn != 600 {
		t.Fatalf("sent: %+v", sent)
	}
	code := e.devCode("zoe@example.com")
	if !regexp.MustCompile(`^\d{6}$`).MatchString(code) {
		t.Fatalf("code: %q", code)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	var bad apiErr
	e.verifyCode("zoe@example.com", wrong, nil, 401, &bad)
	if bad.Error.Code != "code_invalid" || bad.Error.Details["attempts_left"] != float64(4) {
		t.Fatalf("wrong code: %+v", bad)
	}
	var res struct {
		Next string `json:"next"`
	}
	p, first := e.verifyCode("zoe@example.com", code, map[string]any{"next": "/posts?view=list"}, 201, &res)
	if res.Next != "/posts?view=list" || first == "" {
		t.Fatalf("verify: %+v %q", res, first)
	}
	s := e.session(p)
	if s.User.Email != "zoe@example.com" || s.Role != app.RoleOwner || !s.AccountOwner || len(s.Workspaces) != 1 {
		t.Fatalf("session: %+v", s)
	}
	// Single use: the same code again is dead.
	var used apiErr
	e.verifyCode("zoe@example.com", code, nil, 410, &used)
	if used.Error.Code != "code_expired" {
		t.Fatalf("reuse: %+v", used)
	}
	// Returning: a new code, a new session, the same person and workspace (no second account).
	clock.add(31 * time.Second)
	e.sendCode("zoe@example.com", 202)
	p2, second := e.verifyCode("zoe@example.com", e.devCode("zoe@example.com"), nil, 201, nil)
	s2 := e.session(p2)
	if second == "" || second == first || s2.User.ID != s.User.ID || s2.Workspace.ID != s.Workspace.ID || len(s2.Workspaces) != 1 {
		t.Fatalf("second sign-in: %+v (cookie rotated %v)", s2, second != first)
	}
}

func TestEmailCodeLimits(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	// The answer never depends on whether the address has an account.
	known := e.emailSignIn("known@example.com", nil)
	_ = known
	clock.add(31 * time.Second)
	a, b := e.sendCode("known@example.com", 202), e.sendCode("nobody@example.com", 202)
	if a != b {
		t.Fatalf("different answers: %+v %+v", a, b)
	}
	// Cooldown.
	var cool apiErr
	e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": "nobody@example.com"}, 429, &cool)
	if cool.Error.Code != "code_cooldown" || cool.Error.Details["resend_in"] == nil {
		t.Fatalf("cooldown: %+v", cool)
	}
	// Five wrong tries kill the code: even the right one fails after them.
	code := e.devCode("nobody@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		e.verifyCode("nobody@example.com", wrong, nil, 0, nil)
	}
	var dead apiErr
	e.verifyCode("nobody@example.com", code, nil, 410, &dead)
	if dead.Error.Code != "code_expired" {
		t.Fatalf("after 5 tries: %+v", dead)
	}
	// A malformed code costs no try.
	clock.add(31 * time.Second)
	e.sendCode("nobody@example.com", 202)
	e.verifyCode("nobody@example.com", "12ab", nil, 422, nil)
	// Expiry: 10 minutes.
	code = e.devCode("nobody@example.com")
	clock.add(10*time.Minute + time.Second)
	e.verifyCode("nobody@example.com", code, nil, 410, nil)
	// A new code replaces the open one.
	e.sendCode("nobody@example.com", 202)
	old := e.devCode("nobody@example.com")
	clock.add(31 * time.Second)
	e.sendCode("nobody@example.com", 202)
	if fresh := e.devCode("nobody@example.com"); fresh != old {
		e.verifyCode("nobody@example.com", old, nil, 401, nil)
		e.verifyCode("nobody@example.com", fresh, nil, 201, nil)
	}
	// At most 5 codes per address per hour.
	var limited apiErr
	for i := 0; ; i++ {
		clock.add(31 * time.Second)
		resp := e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": "busy@example.com"}, 0, &limited)
		if resp.StatusCode == 429 {
			if i != 5 || limited.Error.Code != "rate_limited" {
				t.Fatalf("limit after %d codes: %+v", i, limited)
			}
			break
		}
		if i > 5 {
			t.Fatal("no per-address limit")
		}
	}
	clock.add(time.Hour)
	e.sendCode("busy@example.com", 202)
	e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": "not an address"}, 422, nil)
}

func TestEmailCodesPerIPLimit(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	n := 0
	for ; n < 30; n++ {
		clock.add(31 * time.Second)
		resp := e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": "ip" + itoa(n) + "@example.com"}, 0, nil)
		if resp.StatusCode == 429 {
			break
		}
	}
	if n != 20 {
		t.Fatalf("per-IP limit after %d codes, want 20", n)
	}
}

// A Google identity and an email identity with the same address are two people unless linked.
func TestEmailSignInNeverMergesWithGoogle(t *testing.T) {
	e, _ := newEmailEnv(t, "")
	g := e.session(e.mustSignIn(ana, ""))
	m := e.session(e.emailSignIn(ana.Email, nil))
	if g.User.ID == m.User.ID || g.Workspace.ID == m.Workspace.ID || !m.AccountOwner {
		t.Fatalf("merged: google %+v email %+v", g.User, m.User)
	}
}

func TestEmailCodeAcceptsInvitation(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	owner := e.mustSignIn(ana, "")
	ws := e.session(owner).Workspace.ID
	_, tok := e.invite(owner, "zoe@example.com", app.RoleEditor)
	// Someone else's address cannot take the invitation.
	e.sendCode("yan@example.com", 202)
	var mismatch apiErr
	e.verifyCode("yan@example.com", e.devCode("yan@example.com"), map[string]any{"invite": tok}, 403, &mismatch)
	if mismatch.Error.Code != "invitation_email_mismatch" {
		t.Fatalf("mismatch: %+v", mismatch)
	}
	clock.add(31 * time.Second)
	zoe := e.emailSignIn("zoe@example.com", map[string]any{"invite": tok})
	s := e.session(zoe)
	if s.Workspace.ID != ws || s.Role != app.RoleEditor || s.AccountOwner || len(s.Workspaces) != 1 {
		t.Fatalf("invited: %+v", s)
	}
	// The editor cannot manage people.
	e.as(zoe, "GET", "/v1/members", nil, 403, nil)
	e.as(zoe, "POST", "/v1/invitations", map[string]any{"email": "x@example.com", "role": app.RoleViewer}, 403, nil)
}

func TestEmailSignUpClosedBySetupToken(t *testing.T) {
	e, _ := newEmailEnv(t, "tok")
	e.sendCode("new@example.com", 202)
	code := e.devCode("new@example.com")
	var closed apiErr
	e.verifyCode("new@example.com", code, nil, 403, &closed)
	if closed.Error.Code != "signup_closed" {
		t.Fatalf("closed: %+v", closed)
	}
	e.verifyCode("new@example.com", code, map[string]any{"setup_token": "tok"}, 201, nil)
}

func TestSignInMethodsLinkAndRemove(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	zoe := e.emailSignIn("zoe@example.com", nil)
	me := e.session(zoe).User.ID
	var list struct {
		Data []app.SignInMethod `json:"data"`
	}
	e.as(zoe, "GET", "/v1/account/sign-in-methods", nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].Provider != app.ProviderEmail || list.Data[0].Subject != "zoe@example.com" {
		t.Fatalf("methods: %+v", list)
	}
	var last apiErr
	e.as(zoe, "DELETE", "/v1/account/sign-in-methods/email/zoe@example.com", nil, 409, &last)
	if last.Error.Code != "last_sign_in_method" {
		t.Fatalf("last: %+v", last)
	}

	// Add Google (link mode keeps the current session).
	linkGoogle := func(p *person, id fakeIdentity) string {
		t.Helper()
		var start struct {
			URL string `json:"url"`
		}
		e.as(p, "POST", "/v1/account/sign-in-methods/google", nil, 200, &start)
		state, code := e.google.authorize(start.URL, id, nil)
		resp, err := p.c.Get(e.srv.URL + "/v1/auth/google/callback?state=" + url.QueryEscape(state) + "&code=" + url.QueryEscape(code))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Location")
	}
	zoeGoogle := fakeIdentity{Sub: "sub-zoe", Email: "zoe.work@example.com", Name: "Zoe"}
	if loc := linkGoogle(zoe, zoeGoogle); loc != "/settings/account?linked=google" {
		t.Fatalf("link google: %q", loc)
	}
	if e.session(zoe).User.ID != me {
		t.Fatal("link mode replaced the session")
	}
	if g := e.session(e.mustSignIn(zoeGoogle, "")); g.User.ID != me || g.Workspace.ID == "" || len(g.Workspaces) != 1 {
		t.Fatalf("google after linking: %+v", g)
	}

	// One address per person: Zoe signs in with one, so she changes it instead of adding another.
	var exists apiErr
	e.as(zoe, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": "zoe@home.example"}, 409, &exists)
	if exists.Error.Code != "email_exists" {
		t.Fatalf("second address: %+v", exists)
	}

	// Taken by another person: refused, never moved.
	bob := e.mustSignIn(bob, "")
	clock.add(31 * time.Second)
	e.as(bob, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": "zoe@example.com"}, 202, nil)
	var taken apiErr
	e.as(bob, "POST", "/v1/account/sign-in-methods/email/verify", map[string]any{"email": "zoe@example.com", "code": e.devCode("zoe@example.com")}, 409, &taken)
	if taken.Error.Code != "identity_taken" {
		t.Fatalf("taken: %+v", taken)
	}
	if loc := linkGoogle(bob, zoeGoogle); loc != "/settings/account?error=identity_taken" {
		t.Fatalf("google taken: %q", loc)
	}
	// A code for linking does not sign anyone in, and a sign-in code does not link.
	clock.add(31 * time.Second)
	e.as(bob, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": "spare@example.com"}, 202, nil)
	e.verifyCode("spare@example.com", e.devCode("spare@example.com"), nil, 410, nil)

	// Google-only Bob adds an address: it then signs him in.
	clock.add(31 * time.Second)
	e.as(bob, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": "bob@home.example"}, 202, nil)
	e.as(bob, "POST", "/v1/account/sign-in-methods/email/verify", map[string]any{"email": "bob@home.example", "code": e.devCode("bob@home.example")}, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("bob after linking: %+v", list)
	}
	clock.add(31 * time.Second)
	if h := e.session(e.emailSignIn("bob@home.example", nil)); h.User.ID != e.session(bob).User.ID {
		t.Fatalf("linked address signs in someone else: %+v", h.User)
	}

	// Remove a method: that address then signs in a new person.
	e.as(zoe, "DELETE", "/v1/account/sign-in-methods/email/zoe@example.com", nil, 200, &list)
	if len(list.Data) != 1 {
		t.Fatalf("after remove: %+v", list)
	}
	clock.add(31 * time.Second)
	if n := e.session(e.emailSignIn("zoe@example.com", nil)); n.User.ID == me {
		t.Fatal("a removed method still signs in")
	}
	e.as(bob, "DELETE", "/v1/account/sign-in-methods/email/nope@example.com", nil, 404, nil)
}

func TestEmailCodeIsMailed(t *testing.T) {
	e, _ := newEmailEnv(t, "")
	e.svc.Cfg.DevSignIn = false
	host, port, inbox := fakeSMTP(t)
	e.svc.Getenv = func(k string) string {
		return map[string]string{"RATE_LIMIT_DISABLED": "true", "MAIL_SMTP_HOST": host, "MAIL_SMTP_PORT": itoa(port), "MAIL_SMTP_TLS": "none",
			"MAIL_FROM": "staqly <noreply@example.com>"}[k]
	}
	e.sendCode("mail@example.com", 202)
	e.do(nil, "GET", "/v1/auth/email/dev-code?email=mail%40example.com", "", nil, 404, nil)
	var code string
	select {
	case m := <-inbox:
		found := regexp.MustCompile(`sign-in code is (\d{6})`).FindStringSubmatch(m.Data)
		if len(m.To) != 1 || !strings.Contains(m.To[0], "mail@example.com") || found == nil {
			t.Fatalf("mail: %+v", m)
		}
		code = found[1]
	case <-time.After(5 * time.Second):
		t.Fatal("no code email")
	}
	e.verifyCode("mail@example.com", code, nil, 201, nil)
}

func TestEmailSignInOff(t *testing.T) {
	e := newAccountsEnv(t, "")
	var off apiErr
	e.do(nil, "POST", "/v1/auth/email/start", "", map[string]any{"email": "a@example.com"}, 409, &off)
	if off.Error.Code != "email_sign_in_disabled" {
		t.Fatalf("off: %+v", off)
	}
	e.do(nil, "GET", "/v1/auth/email/dev-code?email=a%40example.com", "", nil, 404, nil)
	var st map[string]any
	e.do(nil, "GET", "/v1/setup", "", nil, 200, &st)
	if st["email_sign_in"] != false || st["legacy_sign_in"] != false {
		t.Fatalf("setup: %v", st)
	}
}

// Concurrent wrong guesses never get more than five tries: the try is counted before the comparison.
func TestEmailCodeConcurrentGuesses(t *testing.T) {
	e, _ := newEmailEnv(t, "")
	e.sendCode("race@example.com", 202)
	code := e.devCode("race@example.com")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wrongAnswers := 0
	for i := 0; i < 12; i++ {
		guess := "000000"
		if guess == code {
			guess = "111111"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := e.do(nil, "POST", "/v1/auth/email/verify", "", map[string]any{"email": "race@example.com", "code": guess}, 0, nil)
			if resp.StatusCode == 401 {
				mu.Lock()
				wrongAnswers++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wrongAnswers > 5 {
		t.Fatalf("%d tries counted as wrong, at most 5 allowed", wrongAnswers)
	}
	e.verifyCode("race@example.com", code, nil, 410, nil)
}
