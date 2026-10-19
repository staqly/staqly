package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// ---- a deterministic fake Google (OpenID Connect discovery, JWKS, token endpoint, RS256 ID tokens) ----

type fakeIdentity struct {
	Sub, Email, Name string
	Unverified       bool
}

type fakeGrant struct {
	id               fakeIdentity
	nonce, challenge string
	audience, issuer string
}

type fakeGoogle struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	grants map[string]fakeGrant
}

const testClientID = "client.apps.googleusercontent.com"

func newFakeGoogle(t *testing.T) *fakeGoogle {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGoogle{t: t, key: key, grants: map[string]fakeGrant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": g.srv.URL, "authorization_endpoint": g.srv.URL + "/auth",
			"token_endpoint": g.srv.URL + "/token", "jwks_uri": g.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", g.token)
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

// token checks the client secret and the PKCE verifier, then signs an ID token for the grant.
func (g *fakeGoogle) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	g.mu.Lock()
	gr, found := g.grants[r.PostForm.Get("code")]
	delete(g.grants, r.PostForm.Get("code")) // codes are single use
	g.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || id != testClientID || secret != "secret" || base64.RawURLEncoding.EncodeToString(sum[:]) != gr.challenge {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: g.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1").WithType("JWT"))
	aud, iss := testClientID, g.srv.URL
	if gr.audience != "" {
		aud = gr.audience
	}
	if gr.issuer != "" {
		iss = gr.issuer
	}
	now := time.Now()
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: iss, Subject: gr.id.Sub, Audience: jwt.Audience{aud},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour))}).Claims(map[string]any{
		"email": gr.id.Email, "email_verified": !gr.id.Unverified, "name": gr.id.Name, "picture": "https://example.com/a.png", "nonce": gr.nonce}).Serialize()
	if err != nil {
		g.t.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
}

// authorize stands in for the person approving at Google: a code bound to the start URL's nonce and PKCE challenge.
func (g *fakeGoogle) authorize(authURL string, id fakeIdentity, tweak func(*fakeGrant)) (state, code string) {
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, g.srv.URL+"/auth") {
		g.t.Fatalf("start did not redirect to Google: %q", authURL)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("state") == "" || q.Get("client_id") != testClientID {
		g.t.Fatalf("authorization request without PKCE, nonce or state: %s", authURL)
	}
	gr := fakeGrant{id: id, nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	if tweak != nil {
		tweak(&gr)
	}
	code = "code_" + id.Sub + "_" + q.Get("state")[:8]
	g.mu.Lock()
	g.grants[code] = gr
	g.mu.Unlock()
	return q.Get("state"), code
}

// ---- accounts env ----

type acctEnv struct {
	*env
	google *fakeGoogle
}

func newAccountsEnv(t *testing.T, setupToken string) *acctEnv {
	t.Helper()
	database, err := storetest.Open(t, t.TempDir()+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	return newAccountsEnvOn(t, setupToken, database)
}

// newAccountsEnvOn: the same server on a database the test prepared (e.g. one filled by import-sqlite).
func newAccountsEnvOn(t *testing.T, setupToken string, database *sql.DB) *acctEnv {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { database.Close() })
	g := newFakeGoogle(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if os.Getenv("ACCOUNTS_TEST_LOG") != "" {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, TrialPlan: "pro", SetupToken: setupToken, UploadDir: dir + "/uploads", MaxUploadBytes: 1 << 20,
		PublicURL: "http://test", PublicDashboardURL: "https://staqly.example.com", SessionTTL: time.Hour,
		Google:  &app.GoogleConfig{ClientID: testClientID, ClientSecret: "secret", Issuer: g.srv.URL},
		Webhook: app.WebhookConfig{AllowLoopback: true, RetryBase: 20 * time.Millisecond, RetryMax: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond}})
	svc.Getenv = func(k string) string { return map[string]string{"RATE_LIMIT_DISABLED": "true"}[k] }
	h := httpapi.New(svc, log, httpapi.Config{MCP: mcpserver.HTTPHandler(svc, "test"), OpenAPISpec: []byte("openapi: 3.1.0")})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	svc.Cfg.Google.RedirectURL = srv.URL + "/v1/auth/google/callback"
	return &acctEnv{env: &env{t: t, svc: svc, srv: srv}, google: g}
}

// person is a browser signed in with Google.
type person struct {
	c    *http.Client
	csrf string
	id   fakeIdentity
}

func noRedirects(jar http.CookieJar) *http.Client {
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// signIn runs the whole browser flow; it returns where the callback sent the browser.
func (e *acctEnv) signIn(id fakeIdentity, query string, tweak func(*fakeGrant)) (*person, string) {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	c := noRedirects(jar)
	resp, err := c.Get(e.srv.URL + "/v1/auth/google/start?" + query)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		e.t.Fatalf("start: %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if strings.HasPrefix(loc, "/login") {
		return &person{c: c, id: id}, loc
	}
	state, code := e.google.authorize(loc, id, tweak)
	resp, err = c.Get(e.srv.URL + "/v1/auth/google/callback?state=" + url.QueryEscape(state) + "&code=" + url.QueryEscape(code))
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		e.t.Fatalf("callback: %d", resp.StatusCode)
	}
	p := &person{c: c, id: id}
	if !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		var s struct {
			CSRF string `json:"csrf_token"`
		}
		e.do(c, "GET", "/v1/session", "", nil, 200, &s)
		p.csrf = s.CSRF
	}
	return p, resp.Header.Get("Location")
}

func (e *acctEnv) mustSignIn(id fakeIdentity, query string) *person {
	e.t.Helper()
	p, loc := e.signIn(id, query, nil)
	if strings.HasPrefix(loc, "/login") {
		e.t.Fatalf("sign-in of %s failed: %s", id.Email, loc)
	}
	return p
}

// as sends a request as the person (session cookie + CSRF header).
func (e *acctEnv) as(p *person, method, path string, body any, want int, out any) {
	e.t.Helper()
	e.do(p.c, method, path, "", body, want, out, "X-CSRF-Token", p.csrf)
}

type sessionOut struct {
	Workspace    app.Workspace     `json:"workspace"`
	User         app.User          `json:"user"`
	Role         string            `json:"role"`
	AccountOwner bool              `json:"account_owner"`
	Workspaces   []app.MyWorkspace `json:"workspaces"`
	Principal    struct {
		Scopes []string `json:"scopes"`
	} `json:"principal"`
}

func (e *acctEnv) session(p *person) sessionOut {
	e.t.Helper()
	var s sessionOut
	e.as(p, "GET", "/v1/session", nil, 200, &s)
	return s
}

var (
	ana   = fakeIdentity{Sub: "sub-ana", Email: "ana@example.com", Name: "Ana Owner"}
	bob   = fakeIdentity{Sub: "sub-bob", Email: "bob@example.com", Name: "Bob"}
	cleo  = fakeIdentity{Sub: "sub-cleo", Email: "cleo@example.com", Name: "Cleo"}
	dan   = fakeIdentity{Sub: "sub-dan", Email: "dan@example.com", Name: "Dan"}
	eve   = fakeIdentity{Sub: "sub-eve", Email: "eve@example.com", Name: "Eve Outsider"}
	frank = fakeIdentity{Sub: "sub-frank", Email: "frank@example.com", Name: "Frank"}
)

// invite returns the accept token of a new invitation.
func (e *acctEnv) invite(by *person, email, role string) (app.Invitation, string) {
	e.t.Helper()
	var inv app.Invitation
	e.as(by, "POST", "/v1/invitations", map[string]any{"email": email, "role": role}, 201, &inv)
	u, err := url.Parse(inv.Link)
	if err != nil || inv.Link == "" || !strings.HasPrefix(inv.Link, "https://staqly.example.com/invite?token=") {
		e.t.Fatalf("invitation link: %q", inv.Link)
	}
	return inv, u.Query().Get("token")
}

// team: Ana owns the account; Bob is admin, Cleo editor, Dan viewer in her workspace; Eve has her own account.
func (e *acctEnv) team() (owner, admin, editor, viewer, outsider *person) {
	e.t.Helper()
	owner = e.mustSignIn(ana, "")
	for _, m := range []struct {
		id   fakeIdentity
		role string
		out  **person
	}{{bob, app.RoleAdmin, &admin}, {cleo, app.RoleEditor, &editor}, {dan, app.RoleViewer, &viewer}} {
		_, tok := e.invite(owner, m.id.Email, m.role)
		*m.out = e.mustSignIn(m.id, "invite="+url.QueryEscape(tok))
	}
	outsider = e.mustSignIn(eve, "")
	return
}

// ---- sign-in ----

func TestGoogleSignInCreatesAccountAndWorkspace(t *testing.T) {
	e := newAccountsEnv(t, "")
	var st map[string]any
	e.do(nil, "GET", "/v1/setup", "", nil, 200, &st)
	if st["google_sign_in"] != true {
		t.Fatalf("setup: %v", st)
	}
	p, loc := e.signIn(ana, "next="+url.QueryEscape("/posts?view=list"), nil)
	if loc != "/posts?view=list" {
		t.Fatalf("landing: %q", loc)
	}
	s := e.session(p)
	if s.User.Email != ana.Email || s.User.Name != ana.Name || s.User.AvatarURL == "" || s.Role != app.RoleOwner || !s.AccountOwner ||
		len(s.Workspaces) != 1 || !s.Workspaces[0].Current || len(s.Principal.Scopes) != len(app.AllScopes) {
		t.Fatalf("session: %+v", s)
	}
	first := s.Workspace.ID

	// The account, renamed; a second workspace; switching between them.
	var acc app.Account
	e.as(p, "GET", "/v1/account", nil, 200, &acc)
	if acc.Owner.Email != ana.Email || len(acc.Workspaces) != 1 {
		t.Fatalf("account: %+v", acc)
	}
	e.as(p, "PATCH", "/v1/account", map[string]any{"name": "Ana's studio"}, 200, &acc)
	if acc.Name != "Ana's studio" {
		t.Fatalf("rename: %+v", acc)
	}
	var ws app.MyWorkspace
	e.as(p, "POST", "/v1/account/workspaces", map[string]any{"name": "Bad zone", "timezone": "Nowhere/Land"}, 422, nil)
	e.as(p, "POST", "/v1/account/workspaces", map[string]any{"name": "Clients", "timezone": "Europe/Warsaw"}, 201, &ws)
	if ws.Role != app.RoleOwner || ws.ID == first {
		t.Fatalf("new workspace: %+v", ws)
	}
	var mine struct {
		Data []app.MyWorkspace `json:"data"`
	}
	e.as(p, "GET", "/v1/workspaces", nil, 200, &mine)
	if len(mine.Data) != 2 {
		t.Fatalf("workspaces: %+v", mine)
	}
	e.as(p, "POST", "/v1/session/workspace", map[string]any{"workspace_id": ws.ID}, 204, nil)
	if s := e.session(p); s.Workspace.ID != ws.ID || s.Workspace.Name != "Clients" {
		t.Fatalf("switched: %+v", s.Workspace)
	}
	var cur app.Workspace
	e.as(p, "GET", "/v1/workspace", nil, 200, &cur)
	if cur.Timezone != "Europe/Warsaw" {
		t.Fatalf("new workspace's zone: %+v", cur)
	}
	// A workspace that is not theirs does not exist for them.
	e.as(p, "POST", "/v1/session/workspace", map[string]any{"workspace_id": "ws_nope"}, 404, nil)

	// Signing in again: the same person, the same account (no second account), a new session.
	jarCookie := func(p *person) string {
		u, _ := url.Parse(e.srv.URL)
		for _, c := range p.c.Jar.Cookies(u) {
			if c.Name == "staqly_session" {
				return c.Value
			}
		}
		return ""
	}
	again := e.mustSignIn(ana, "")
	if jarCookie(again) == jarCookie(p) || jarCookie(again) == "" {
		t.Fatal("a sign-in must issue a new session token")
	}
	e.as(again, "GET", "/v1/workspaces", nil, 200, &mine)
	if len(mine.Data) != 2 {
		t.Fatalf("returning person: %+v", mine)
	}

	// The same email under another Google identity is another person (never merged by email).
	other := e.mustSignIn(fakeIdentity{Sub: "sub-ana-2", Email: ana.Email, Name: "Other Ana"}, "")
	if s := e.session(other); s.Workspace.ID == first || s.Workspace.ID == ws.ID {
		t.Fatalf("merged by email: %+v", s.Workspace)
	}

	// Logout ends the session.
	e.as(p, "DELETE", "/v1/session", nil, 204, nil)
	e.as(p, "GET", "/v1/session", nil, 401, nil)
	e.as(again, "GET", "/v1/session", nil, 200, nil)
}

func TestGoogleSignInRejects(t *testing.T) {
	e := newAccountsEnv(t, "")
	for _, tc := range []struct {
		name  string
		id    fakeIdentity
		tweak func(*fakeGrant)
		want  string
	}{
		{"another nonce", ana, func(g *fakeGrant) { g.nonce = "replayed" }, "login_state_mismatch"},
		{"another audience", ana, func(g *fakeGrant) { g.audience = "someone-else" }, "google_failed"},
		{"another issuer", ana, func(g *fakeGrant) { g.issuer = "https://evil.example" }, "google_failed"},
		{"wrong PKCE", ana, func(g *fakeGrant) { g.challenge = "nope" }, "google_failed"},
		{"unverified email", fakeIdentity{Sub: "u", Email: "u@example.com", Unverified: true}, nil, "email_unverified"},
	} {
		if _, loc := e.signIn(tc.id, "", tc.tweak); loc != "/login?error="+tc.want {
			t.Errorf("%s: %s", tc.name, loc)
		}
	}

	// A callback whose state is not the one this browser started, or with no start at all.
	jar, _ := cookiejar.New(nil)
	c := noRedirects(jar)
	resp, _ := c.Get(e.srv.URL + "/v1/auth/google/start")
	resp.Body.Close()
	_, code := e.google.authorize(resp.Header.Get("Location"), ana, nil)
	for _, cl := range []*http.Client{c, noRedirects(nil)} {
		resp, _ = cl.Get(e.srv.URL + "/v1/auth/google/callback?state=forged&code=" + code)
		resp.Body.Close()
		if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=login_") {
			t.Errorf("forged state: %s", loc)
		}
	}
	resp, _ = noRedirects(nil).Get(e.srv.URL + "/v1/auth/google/callback?error=access_denied")
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/login?error=google_cancelled" {
		t.Errorf("cancelled: %s", loc)
	}

	// next never leaves the dashboard.
	for _, next := range []string{"//evil.example/x", "https://evil.example", "/\\evil.example", "javascript:alert(1)"} {
		if _, loc := e.signIn(bob, "next="+url.QueryEscape(next), nil); loc != "/schedule" {
			t.Errorf("open redirect %q: %s", next, loc)
		}
	}
	// Legacy sign-up and sign-in paths are closed in account mode.
	e.do(nil, "POST", "/v1/workspaces", "", map[string]any{"name": "x"}, 409, nil)
	e.do(nil, "POST", "/v1/account/workspaces", "", map[string]any{"name": "x"}, 401, nil)
}

func TestGoogleSignUpClosedBySetupToken(t *testing.T) {
	e := newAccountsEnv(t, "tok")
	if _, loc := e.signIn(ana, "", nil); loc != "/login?error=signup_closed" {
		t.Fatalf("closed sign-up: %s", loc)
	}
	owner := e.mustSignIn(ana, "setup_token=tok")
	// Invited people join without the token; they never get an account of their own.
	_, tok := e.invite(owner, bob.Email, app.RoleEditor)
	bobP := e.mustSignIn(bob, "invite="+url.QueryEscape(tok))
	if s := e.session(bobP); s.Role != app.RoleEditor || s.AccountOwner {
		t.Fatalf("invited: %+v", s)
	}
	e.as(bobP, "GET", "/v1/account", nil, 404, nil)
	e.as(bobP, "POST", "/v1/account/workspaces", map[string]any{"name": "Mine"}, 403, nil)
}

// ---- invitations ----

// Per user pricing: every member is a seat. No plan caps them; the subscription's quantity follows the members
// (checkout, a join, a removal), set by a job after the change is saved and prorated from the change: a seat
// added mid-period is charged at once for the rest of it (always_invoice), one removed is credited on the next
// invoice (create_prorations), added and removed in one period nets to the days in between, nothing is
// prorated during the trial, the same holds for a yearly plan, and a quantity changed at Stripe is put back.
func TestSeatsAreBilledPerMember(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	ws := e.session(owner).Workspace.ID
	if err := e.svc.SetPlan(context.Background(), ws, "plus", ""); err != nil {
		t.Fatal(err)
	}
	quantity, status := int64(1), "active"
	periodStart := time.Now().Add(-10 * 24 * time.Hour).Unix() // ten days into the period
	interval := "month"
	var checkout url.Values
	var updates []url.Values
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_plus_month"}]}`)
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_1"}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_1"}`)
		case "GET /v1/subscriptions/sub_1":
			fmt.Fprintf(w, `{"id":"sub_1","status":%q,"current_period_start":%d,"items":{"data":[{"id":"si_1","quantity":%d}]}}`,
				status, periodStart, quantity)
		case "POST /v1/subscriptions/sub_1":
			updates = append(updates, r.Form)
			fmt.Sscan(r.Form.Get("items[0][quantity]"), &quantity)
			fmt.Fprint(w, `{"id":"sub_1"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: stripe.URL}
	ctx := context.Background()
	subscription := func(id string) {
		t.Helper()
		sub := map[string]any{"id": "sub_1", "customer": "cus_1", "status": status, "metadata": map[string]string{"workspace_id": ws},
			"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": quantity,
				"current_period_end": time.Now().Add(30 * 24 * time.Hour).Unix(), "price": stripePriceObject("plus", interval)}}}}
		if code := e.stripeEvent(id, "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("webhook %s: %d", id, code)
		}
	}

	// No cap: Plus takes a fourth and fifth person (it used to stop at three).
	for _, addr := range []string{"x1@example.com", "x2@example.com", "x3@example.com", "x4@example.com"} {
		e.invite(owner, addr, app.RoleViewer)
	}
	// Open invitations are not billed: the checkout is for Ana alone.
	var out struct{ URL string }
	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "plus", "interval": "month"}, 200, &out)
	if checkout.Get("line_items[0][quantity]") != "1" {
		t.Fatalf("checkout quantity: %v", checkout)
	}
	subscription("evt_seat_1")
	e.svc.RunDueJobsOnce(ctx)
	if len(updates) != 0 {
		t.Fatalf("seats already right, nothing to change: %v", updates)
	}

	// prorated: the change's proration date is when it happened, inside the period.
	prorated := func(u url.Values, changedAt time.Time) {
		t.Helper()
		var at int64
		fmt.Sscan(u.Get("proration_date"), &at)
		if at < periodStart || at < changedAt.Unix()-1 || at > time.Now().Unix() {
			t.Fatalf("proration date %d, changed at %d, period from %d: %v", at, changedAt.Unix(), periodStart, u)
		}
	}

	// Bob accepts mid-period: two seats, the rest of the period charged at once; a second run changes nothing.
	_, tok := e.invite(owner, bob.Email, app.RoleEditor)
	joined := time.Now()
	e.mustSignIn(bob, "invite="+url.QueryEscape(tok))
	e.svc.RunDueJobsOnce(ctx)
	e.svc.RunDueJobsOnce(ctx)
	if len(updates) != 1 || updates[0].Get("items[0][quantity]") != "2" || updates[0].Get("items[0][id]") != "si_1" ||
		updates[0].Get("proration_behavior") != "always_invoice" {
		t.Fatalf("after Bob joined: %v", updates)
	}
	prorated(updates[0], joined)
	var b app.Billing
	e.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if b.Seats != 2 {
		t.Fatalf("billing seats = %d", b.Seats)
	}

	// Bob is removed in the same period: back to one seat, the unused rest credited on the next invoice. With
	// the charge above, he pays for the days in between.
	removed := time.Now()
	var members struct{ Data []app.Member }
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	for _, m := range members.Data {
		if m.Email == bob.Email {
			e.as(owner, "DELETE", "/v1/members/"+m.UserID, nil, 204, nil)
		}
	}
	e.svc.RunDueJobsOnce(ctx)
	if len(updates) != 2 || updates[1].Get("items[0][quantity]") != "1" || updates[1].Get("proration_behavior") != "create_prorations" {
		t.Fatalf("after Bob left: %v", updates)
	}
	prorated(updates[1], removed)
	if a, b := updates[0].Get("proration_date"), updates[1].Get("proration_date"); a > b {
		t.Fatalf("the credit starts after the charge: %s > %s", a, b)
	}

	// Changed at Stripe (3 seats for 1 member): the webhook queues the sync, which puts it back.
	quantity = 3
	subscription("evt_seat_2")
	e.svc.RunDueJobsOnce(ctx)
	if len(updates) != 3 || updates[2].Get("items[0][quantity]") != "1" {
		t.Fatalf("after a quantity change at Stripe: %v", updates)
	}

	// During the Starter trial seats are free: the quantity follows, nothing is prorated.
	status = "trialing"
	subscription("evt_seat_3")
	_, tok = e.invite(owner, cleo.Email, app.RoleViewer)
	e.mustSignIn(cleo, "invite="+url.QueryEscape(tok))
	e.svc.RunDueJobsOnce(ctx)
	if last := updates[len(updates)-1]; last.Get("items[0][quantity]") != "2" || last.Get("proration_behavior") != "none" ||
		last.Get("proration_date") != "" {
		t.Fatalf("during the trial: %v", updates)
	}

	// A yearly plan prorates the same way, over the year.
	status, interval = "active", "year"
	subscription("evt_seat_4")
	_, tok = e.invite(owner, dan.Email, app.RoleViewer)
	joined = time.Now()
	e.mustSignIn(dan, "invite="+url.QueryEscape(tok))
	e.svc.RunDueJobsOnce(ctx)
	if last := updates[len(updates)-1]; last.Get("items[0][quantity]") != "3" || last.Get("proration_behavior") != "always_invoice" {
		t.Fatalf("yearly: %v", updates)
	} else {
		prorated(last, joined)
	}

	// Quick add, remove, add before the sync runs: coalesced into one change, from the last one.
	n := len(updates)
	periodStart = time.Now().Add(-24 * time.Hour).Unix()
	_, tok = e.invite(owner, eve.Email, app.RoleViewer)
	e.mustSignIn(eve, "invite="+url.QueryEscape(tok))
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	for _, m := range members.Data {
		if m.Email == eve.Email {
			e.as(owner, "DELETE", "/v1/members/"+m.UserID, nil, 204, nil)
		}
	}
	e.svc.RunDueJobsOnce(ctx)
	if len(updates) != n {
		t.Fatalf("added and removed before the sync: nothing to bill, got %v", updates[n:])
	}
	_, tok = e.invite(owner, eve.Email, app.RoleViewer)
	again := time.Now()
	e.mustSignIn(eve, "invite="+url.QueryEscape(tok)) // her session ended with the removal
	e.svc.RunDueJobsOnce(ctx)
	if len(updates) != n+1 || updates[n].Get("items[0][quantity]") != "4" || updates[n].Get("proration_behavior") != "always_invoice" {
		t.Fatalf("added again: %v", updates[n:])
	}
	prorated(updates[n], again)

	// A change from before the period (a job that waited) is prorated from the period's start, never before.
	periodStart = time.Now().Unix() - 5
	quantity = 9
	subscription("evt_seat_5")
	e.svc.RunDueJobsOnce(ctx)
	if last := updates[len(updates)-1]; last.Get("items[0][quantity]") != "4" {
		t.Fatalf("put back: %v", updates)
	} else {
		prorated(last, time.Unix(periodStart, 0))
	}
}

func TestInvitationLifecycle(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	inv, tok := e.invite(owner, " Bob@Example.com ", app.RoleEditor)
	if inv.Email != bob.Email || inv.Emailed == nil || *inv.Emailed {
		t.Fatalf("invitation: %+v", inv)
	}
	e.as(owner, "POST", "/v1/invitations", map[string]any{"email": bob.Email, "role": app.RoleViewer}, 409, nil) // one open invitation per address
	e.as(owner, "POST", "/v1/invitations", map[string]any{"email": "not an address", "role": app.RoleViewer}, 422, nil)
	e.as(owner, "POST", "/v1/invitations", map[string]any{"email": "x@example.com", "role": "god"}, 422, nil)
	e.as(owner, "POST", "/v1/invitations", map[string]any{"email": ana.Email, "role": app.RoleViewer}, 409, nil) // already a member

	var prev app.InvitationPreview
	e.do(nil, "GET", "/v1/invitations/lookup?token="+url.QueryEscape(tok), "", nil, 200, &prev)
	if prev.Status != "open" || prev.Email != bob.Email || prev.Role != app.RoleEditor || prev.InvitedBy != ana.Name {
		t.Fatalf("lookup: %+v", prev)
	}
	e.do(nil, "GET", "/v1/invitations/lookup?token=nope", "", nil, 404, nil)

	// Resend: a new link; the old one stops working.
	var re app.Invitation
	e.as(owner, "POST", "/v1/invitations/"+inv.ID+"/resend", nil, 200, &re)
	newTok := strings.TrimPrefix(re.Link, "https://staqly.example.com/invite?token=")
	newTok, _ = url.QueryUnescape(newTok)
	e.do(nil, "GET", "/v1/invitations/lookup?token="+url.QueryEscape(tok), "", nil, 404, nil)

	// Someone else cannot use it (identity check), Bob can, once; a second accept is a no-op.
	eveP := e.mustSignIn(eve, "")
	e.as(eveP, "POST", "/v1/invitations/accept", map[string]any{"token": newTok}, 403, nil)
	if _, loc := e.signIn(frank, "invite="+url.QueryEscape(newTok), nil); loc != "/login?error=invitation_email_mismatch" {
		t.Fatalf("mismatch at sign-in: %s", loc)
	}
	bobP := e.mustSignIn(bob, "invite="+url.QueryEscape(newTok))
	var ws app.MyWorkspace
	e.as(bobP, "POST", "/v1/invitations/accept", map[string]any{"token": newTok}, 200, &ws)
	if ws.Role != app.RoleEditor || !ws.Current {
		t.Fatalf("accept again: %+v", ws)
	}
	e.do(nil, "GET", "/v1/invitations/lookup?token="+url.QueryEscape(newTok), "", nil, 200, &prev)
	if prev.Status != "accepted" {
		t.Fatalf("after accept: %+v", prev)
	}
	var list struct {
		Data []app.Invitation `json:"data"`
	}
	e.as(owner, "GET", "/v1/invitations", nil, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("open invitations: %+v", list)
	}

	// Revoke: the link stops working; revoking twice is a conflict.
	inv2, tok2 := e.invite(owner, cleo.Email, app.RoleViewer)
	e.as(owner, "DELETE", "/v1/invitations/"+inv2.ID, nil, 204, nil)
	e.as(owner, "DELETE", "/v1/invitations/"+inv2.ID, nil, 409, nil)
	if _, loc := e.signIn(cleo, "invite="+url.QueryEscape(tok2), nil); loc != "/login?error=invitation_invalid" {
		t.Fatalf("revoked: %s", loc)
	}

	// Expired.
	_, tok3 := e.invite(owner, dan.Email, app.RoleViewer)
	e.svc.Now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if _, loc := e.signIn(dan, "invite="+url.QueryEscape(tok3), nil); loc != "/login?error=invitation_expired" {
		t.Fatalf("expired: %s", loc)
	}
	e.svc.Now = time.Now
}

func TestInvitationEmail(t *testing.T) {
	e := newAccountsEnv(t, "")
	host, port, inbox := fakeSMTP(t)
	e.svc.Getenv = func(k string) string {
		return map[string]string{"MAIL_SMTP_HOST": host, "MAIL_SMTP_PORT": itoa(port), "MAIL_SMTP_TLS": "none", "MAIL_FROM": "staqly <noreply@example.com>"}[k]
	}
	owner := e.mustSignIn(ana, "")
	inv, tok := e.invite(owner, bob.Email, app.RoleEditor)
	if inv.Emailed == nil || !*inv.Emailed {
		t.Fatalf("not emailed: %+v", inv)
	}
	select {
	case m := <-inbox:
		if len(m.To) != 1 || !strings.Contains(m.To[0], bob.Email) || !strings.Contains(strings.NewReplacer("=\r\n", "", "=\n", "", "=3D", "=").Replace(m.Data), "invite?token="+tok) {
			t.Fatalf("mail: %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no invitation email")
	}
}

// Concurrent accepts of one invitation (double clicks, two tabs): every one succeeds, one membership.
// Concurrent accepts by two people of two invitations to the same workspace: both join.
func TestInvitationAcceptRace(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	_, tok := e.invite(owner, bob.Email, app.RoleEditor)
	tabs := []*person{}
	for range 6 {
		tabs = append(tabs, e.mustSignIn(bob, "")) // Bob has no workspace yet: his own account
	}
	var wg sync.WaitGroup
	codes := make([]int, len(tabs))
	for i, p := range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", e.srv.URL+"/v1/invitations/accept", strings.NewReader(`{"token":"`+tok+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", p.csrf)
			resp, err := p.c.Do(req)
			if err == nil {
				codes[i] = resp.StatusCode
				if resp.StatusCode != 200 {
					b, _ := io.ReadAll(resp.Body)
					t.Logf("tab %d: %s", i, b)
				}
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("tab %d: %d (%v)", i, c, codes)
		}
	}
	var members struct {
		Data []app.Member `json:"data"`
	}
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	if len(members.Data) != 2 {
		t.Fatalf("members: %+v", members.Data)
	}
}

// ---- RBAC ----

// The whole role matrix over REST: every role against every protected action, allowed and denied.
func TestRoleMatrix(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, editor, viewer, _ := e.team()
	var col app.Collection
	e.as(owner, "POST", "/v1/collections", map[string]any{"name": "Seed"}, 201, &col)
	people := map[string]*person{app.RoleOwner: owner, app.RoleAdmin: admin, app.RoleEditor: editor, app.RoleViewer: viewer}
	type action struct {
		name   string
		method string
		path   func() string
		body   func() any
		min    string // the smallest role allowed
		ok     int
	}
	n := 0
	uniq := func() string { n++; return "n" + itoa(n) }
	lastCol := ""
	newCol := func() string {
		var c app.Collection
		lastCol = uniq()
		e.as(owner, "POST", "/v1/collections", map[string]any{"name": lastCol}, 201, &c)
		return c.ID
	}
	newPost := func() string {
		var p app.Post
		e.as(owner, "POST", "/v1/posts", map[string]any{"collection": col.ID, "content": "x"}, 201, &p)
		return p.ID
	}
	static := func(p string) func() string { return func() string { return p } }
	actions := []action{
		{"read posts", "GET", static("/v1/posts"), nil, app.RoleViewer, 200},
		{"read collections", "GET", static("/v1/collections"), nil, app.RoleViewer, 200},
		{"read channels", "GET", static("/v1/channels"), nil, app.RoleViewer, 200},
		{"read events", "GET", static("/v1/events"), nil, app.RoleViewer, 200},
		{"read workspace", "GET", static("/v1/workspace"), nil, app.RoleViewer, 200},
		{"write post", "POST", static("/v1/posts"), func() any { return map[string]any{"collection": col.ID, "content": "hi"} }, app.RoleEditor, 201},
		{"create collection", "POST", static("/v1/collections"), func() any { return map[string]any{"name": uniq()} }, app.RoleEditor, 201},
		{"delete post", "DELETE", func() string { return "/v1/posts/" + newPost() }, nil, app.RoleEditor, 204},
		{"delete collection", "DELETE", func() string { id := newCol(); return "/v1/collections/" + id + "?confirm=" + lastCol }, nil, app.RoleAdmin, 204},
		{"connect channel", "POST", static("/v1/channels"), func() any { return map[string]any{"provider": "custom", "name": uniq()} }, app.RoleAdmin, 201},
		{"create webhook", "POST", static("/v1/webhooks"), func() any { return map[string]any{"url": "http://127.0.0.1:9/h", "events": []string{"post.*"}} }, app.RoleAdmin, 201},
		{"create API key", "POST", static("/v1/api-keys"), func() any { return map[string]any{"name": uniq(), "scopes": []string{"posts:read"}} }, app.RoleAdmin, 201},
		{"list sessions", "GET", static("/v1/sessions"), nil, app.RoleAdmin, 200},
		{"rename workspace", "PATCH", static("/v1/workspace"), func() any { return map[string]any{"name": "Studio"} }, app.RoleAdmin, 200},
		{"list members", "GET", static("/v1/members"), nil, app.RoleAdmin, 200},
		{"list invitations", "GET", static("/v1/invitations"), nil, app.RoleAdmin, 200},
		{"invite", "POST", static("/v1/invitations"), func() any { return map[string]any{"email": uniq() + "@example.com", "role": app.RoleViewer} }, app.RoleAdmin, 201},
		{"create workspace", "POST", static("/v1/account/workspaces"), func() any { return map[string]any{"name": uniq()} }, "account-owner", 201},
		{"read account", "GET", static("/v1/account"), nil, "account-owner", 200},
	}
	rank := map[string]int{app.RoleViewer: 0, app.RoleEditor: 1, app.RoleAdmin: 2, app.RoleOwner: 3, "account-owner": 3}
	for _, a := range actions {
		for role, p := range people {
			want := a.ok
			if rank[role] < rank[a.min] {
				want = 403
				if a.name == "read account" {
					want = 404 // only owners have an account
				}
			}
			t.Run(a.name+"/"+role, func(t *testing.T) {
				e.t = t
				path := a.path()
				var body any
				if a.body != nil {
					body = a.body()
				}
				e.as(p, a.method, path, body, want, nil)
			})
		}
	}
	e.t = t
	if s := e.session(viewer); s.Role != app.RoleViewer || len(s.Principal.Scopes) != len(app.RoleScopes[app.RoleViewer]) {
		t.Fatalf("viewer session: %+v", s)
	}
}

func TestRoleRules(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, editor, viewer, outsider := e.team()
	var members struct {
		Data []app.Member `json:"data"`
	}
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	ids := map[string]string{}
	for _, m := range members.Data {
		ids[m.Email] = m.UserID
		if m.Email == ana.Email && (!m.AccountOwner || m.Role != app.RoleOwner) {
			t.Fatalf("account owner: %+v", m)
		}
	}
	if len(ids) != 4 {
		t.Fatalf("members: %+v", members.Data)
	}
	// Admins manage up to admin; they never grant or touch the owner role.
	e.as(admin, "POST", "/v1/invitations", map[string]any{"email": "o@example.com", "role": app.RoleOwner}, 403, nil)
	e.as(admin, "PATCH", "/v1/members/"+ids[cleo.Email], map[string]any{"role": app.RoleOwner}, 403, nil)
	e.as(admin, "PATCH", "/v1/members/"+ids[ana.Email], map[string]any{"role": app.RoleViewer}, 409, nil) // account owner
	e.as(admin, "DELETE", "/v1/members/"+ids[ana.Email], nil, 409, nil)
	e.as(admin, "PATCH", "/v1/members/"+ids[dan.Email], map[string]any{"role": app.RoleEditor}, 200, nil)
	// Editors and viewers manage nobody; API keys never manage people.
	e.as(editor, "PATCH", "/v1/members/"+ids[dan.Email], map[string]any{"role": app.RoleViewer}, 403, nil)
	e.as(viewer, "DELETE", "/v1/members/"+ids[cleo.Email], nil, 403, nil)
	var key app.APIKey
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "admin key", "scopes": []string{"admin"}}, 201, &key)
	e.do(nil, "GET", "/v1/members", key.Key, nil, 200, nil)
	e.do(nil, "POST", "/v1/invitations", key.Key, map[string]any{"email": "k@example.com", "role": app.RoleViewer}, 403, nil)
	// An account key never signs a browser in (no role without a person).
	e.do(nil, "POST", "/v1/sessions/api-key", "", map[string]any{"api_key": key.Key}, 403, nil)

	// Owners make owners; a second owner can be demoted again (the account owner stays).
	e.as(owner, "PATCH", "/v1/members/"+ids[bob.Email], map[string]any{"role": app.RoleOwner}, 200, nil)
	e.as(admin, "PATCH", "/v1/members/"+ids[bob.Email], map[string]any{"role": app.RoleAdmin}, 200, nil) // Bob, now owner, steps down
	e.as(owner, "PATCH", "/v1/members/nobody", map[string]any{"role": app.RoleAdmin}, 404, nil)

	// A key never outlives or outranks its creator: Bob's admin key narrows when he is demoted, stops when he is removed.
	var bobKey app.APIKey
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "bob", "scopes": []string{"admin"}}, 201, &bobKey)
	e.do(nil, "POST", "/v1/channels", bobKey.Key, map[string]any{"provider": "custom", "name": "ok"}, 201, nil)
	e.as(owner, "PATCH", "/v1/members/"+ids[bob.Email], map[string]any{"role": app.RoleViewer}, 200, nil)
	e.do(nil, "GET", "/v1/posts", bobKey.Key, nil, 200, nil)
	e.do(nil, "POST", "/v1/channels", bobKey.Key, map[string]any{"provider": "custom", "name": "no"}, 403, nil)
	e.do(nil, "POST", "/v1/collections", bobKey.Key, map[string]any{"name": "no"}, 403, nil)
	if s := e.session(admin); s.Role != app.RoleViewer {
		t.Fatalf("demotion applies at once: %+v", s.Role)
	}
	// Removal: sessions in the workspace and keys end at once.
	e.as(owner, "DELETE", "/v1/members/"+ids[bob.Email], nil, 204, nil)
	e.as(admin, "GET", "/v1/session", nil, 401, nil)
	e.do(nil, "GET", "/v1/posts", bobKey.Key, nil, 401, nil)
	// Leaving: Cleo removes herself.
	e.as(editor, "DELETE", "/v1/members/"+ids[cleo.Email], nil, 204, nil)
	e.as(editor, "GET", "/v1/session", nil, 401, nil)

	// The outsider (another account) sees none of it: IDOR and cross-workspace.
	e.as(outsider, "PATCH", "/v1/members/"+ids[dan.Email], map[string]any{"role": app.RoleViewer}, 404, nil)
	e.as(outsider, "DELETE", "/v1/members/"+ids[dan.Email], nil, 404, nil)
	inv, _ := e.invite(owner, "z@example.com", app.RoleViewer)
	e.as(outsider, "DELETE", "/v1/invitations/"+inv.ID, nil, 404, nil)
	e.as(outsider, "POST", "/v1/invitations/"+inv.ID+"/resend", nil, 404, nil)
	var post app.Post
	var col app.Collection
	e.as(owner, "POST", "/v1/collections", map[string]any{"name": "Private"}, 201, &col)
	e.as(owner, "POST", "/v1/posts", map[string]any{"collection": col.ID, "content": "secret"}, 201, &post)
	e.as(outsider, "GET", "/v1/posts/"+post.ID, nil, 404, nil)
	e.as(outsider, "DELETE", "/v1/posts/"+post.ID, nil, 404, nil)
	e.as(outsider, "GET", "/v1/collections/"+col.ID, nil, 404, nil)
	mine := e.session(owner)
	e.as(outsider, "POST", "/v1/session/workspace", map[string]any{"workspace_id": mine.Workspace.ID}, 404, nil)

	// Connect a device signs the person who made the link in on the other device; recovery keys belong to the
	// sign-in from before accounts.
	var link app.DeviceLink
	e.as(owner, "POST", "/v1/device-links", nil, 201, &link)
	phoneJar, _ := cookiejar.New(nil)
	phone := &person{c: noRedirects(phoneJar), id: owner.id}
	var redeemed struct {
		CSRF string `json:"csrf_token"`
	}
	token := link.URL[strings.Index(link.URL, "#")+1:]
	e.as(phone, "POST", "/v1/device-links/redeem", map[string]any{"token": token}, 201, &redeemed)
	phone.csrf = redeemed.CSRF
	if s := e.session(phone); s.User.Email != owner.id.Email || s.Workspace.ID != mine.Workspace.ID {
		t.Fatalf("connected device session: %+v", s)
	}
	e.as(phone, "POST", "/v1/device-links/redeem", map[string]any{"token": token}, 410, nil) // used
	e.as(owner, "POST", "/v1/recovery-key/rotate", nil, 409, nil)

	// Deleting a workspace: the account owner only.
	var second app.MyWorkspace
	e.as(owner, "POST", "/v1/account/workspaces", map[string]any{"name": "Temp"}, 201, &second)
	_, tok := e.invite(owner, frank.Email, app.RoleAdmin)
	frankP := e.mustSignIn(frank, "invite="+url.QueryEscape(tok))
	e.as(frankP, "DELETE", "/v1/workspace", map[string]any{"confirm": mine.Workspace.Name}, 403, nil)
	e.as(owner, "POST", "/v1/session/workspace", map[string]any{"workspace_id": second.ID}, 204, nil)
	e.as(owner, "DELETE", "/v1/workspace", map[string]any{"confirm": "Temp"}, 204, nil)
}

func itoa(n int) string { return strconv.Itoa(n) }

// With billing on, a new workspace has no plan until its owner starts a subscription: it is read-only, Starter's
// checkout starts Stripe's card-required 7-day trial, trialing gives Starter, a trial cancelled for want of a
// card makes it read-only again, and a workspace that had a subscription gets no second trial.
func TestCardRequiredTrial(t *testing.T) {
	e := newAccountsEnv(t, "")
	var checkout url.Values
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":1500,"currency":"eur"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_t"}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/trial"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_trial", WebhookSecret: testWhsec, APIBase: stripe.URL}
	owner := e.mustSignIn(ana, "")
	ws := e.session(owner).Workspace.ID
	var b app.Billing
	e.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if !b.Suspended || b.SubscriptionStatus != nil {
		t.Fatalf("a new workspace has no plan until a subscription starts: %+v", b)
	}
	var errBody struct{ Error app.Error }
	e.as(owner, "POST", "/v1/collections", map[string]any{"name": "Blocked"}, 402, &errBody)
	if errBody.Error.Code != "plan_expired" {
		t.Fatalf("read-only: %+v", errBody.Error)
	}
	var out struct{ URL string }
	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "month"}, 200, &out)
	if checkout.Get("subscription_data[trial_period_days]") != "7" || checkout.Get("payment_method_collection") != "always" ||
		checkout.Get("automatic_tax[enabled]") != "true" {
		t.Fatalf("starter checkout: %v", checkout)
	}
	trialEnd := time.Now().Add(7 * 24 * time.Hour).Unix()
	sub := map[string]any{"id": "sub_t", "customer": "cus_t", "status": "trialing", "metadata": map[string]string{"workspace_id": ws},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_t", "quantity": 1, "current_period_end": trialEnd,
			"price": stripePriceObject("starter", "month")}}}}
	e.stripeEvent("evt_trial_1", "customer.subscription.created", sub, testWhsec)
	e.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if b.Suspended || b.Plan != "starter" || b.SubscriptionStatus == nil || *b.SubscriptionStatus != "trialing" {
		t.Fatalf("trialing: %+v", b)
	}
	e.as(owner, "POST", "/v1/collections", map[string]any{"name": "Works"}, 201, nil)
	// No card at the trial's end: Stripe cancels it, the workspace is read-only again.
	sub["status"] = "canceled"
	e.stripeEvent("evt_trial_2", "customer.subscription.deleted", sub, testWhsec)
	e.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if !b.Suspended {
		t.Fatalf("after a cancelled trial: %+v", b)
	}
	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "month"}, 200, &out)
	if checkout.Get("subscription_data[trial_period_days]") != "" {
		t.Fatalf("no second trial: %v", checkout)
	}
	// Plus, Pro and Business never have one.
	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "plus", "interval": "month"}, 200, &out)
	if checkout.Get("subscription_data[trial_period_days]") != "" {
		t.Fatalf("plus: %v", checkout)
	}
}

// Starter's trial goes to each workspace once and to one workspace of an account at a time: a second workspace
// made while the first is still trialing is charged at once, and gets its trial once the first one's has ended.
func TestTrialOncePerWorkspaceOneAtATime(t *testing.T) {
	e := newAccountsEnv(t, "")
	var checkout url.Values
	stripe := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":1500,"currency":"eur"}]}`, strings.TrimPrefix(r.URL.Query().Get("lookup_keys[]"), "staqly_"))
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_`+r.Form.Get("metadata[workspace_id]")+`"}`)
		case "POST /v1/checkout/sessions":
			checkout = r.Form
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/s"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_trial", WebhookSecret: testWhsec, APIBase: stripe.URL}
	owner := e.mustSignIn(ana, "")
	first := e.session(owner).Workspace.ID
	trialFor := func(want string) {
		t.Helper()
		var b app.Billing
		e.as(owner, "GET", "/v1/billing", nil, 200, &b)
		if b.Trial != want {
			t.Fatalf("trial: got %q, want %q", b.Trial, want)
		}
		e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "starter", "interval": "month"}, 200, nil)
		if got := checkout.Get("subscription_data[trial_period_days]") == "7"; got != (want == app.TrialAvailable) {
			t.Fatalf("checkout with trial = %v for %q: %v", got, want, checkout)
		}
	}
	trialFor(app.TrialAvailable)
	sub := map[string]any{"id": "sub_first", "customer": "cus_" + first, "status": "trialing", "metadata": map[string]string{"workspace_id": first},
		"items": map[string]any{"data": []any{map[string]any{"id": "si_1", "quantity": 1, "current_period_end": time.Now().Add(7 * 24 * time.Hour).Unix(),
			"price": stripePriceObject("starter", "month")}}}}
	e.stripeEvent("evt_first_1", "customer.subscription.created", sub, testWhsec)

	var second app.MyWorkspace
	e.as(owner, "POST", "/v1/account/workspaces", map[string]any{"name": "Second"}, 201, &second)
	e.as(owner, "POST", "/v1/session/workspace", map[string]any{"workspace_id": second.ID}, 204, nil)
	trialFor(app.TrialOtherWorkspace) // the first is still trialing: charged today

	// The first workspace's trial ends (it pays now): the second may start its own.
	sub["status"] = "active"
	e.stripeEvent("evt_first_2", "customer.subscription.updated", sub, testWhsec)
	trialFor(app.TrialAvailable)

	// Back on the first: it had a subscription, so no second trial there.
	e.as(owner, "POST", "/v1/session/workspace", map[string]any{"workspace_id": first}, 204, nil)
	var b app.Billing
	e.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if b.Trial != app.TrialNone { // it pays: no trial to offer
		t.Fatalf("paying workspace: %+v", b)
	}
	sub["status"] = "canceled"
	e.stripeEvent("evt_first_3", "customer.subscription.deleted", sub, testWhsec)
	trialFor(app.TrialUsed)
}
