package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

type env struct {
	t        testing.TB
	svc      *app.Service
	srv      *httptest.Server
	key      string // agent key with the non-admin scopes (app.DefaultKeyScopes)
	admin    string // the admin key sign-up returns (dashboard sign-in)
	recovery string
	browser  *http.Client // holds the session cookie
	csrf     string
}

// newEnv starts every test from scratch: a new SQLite file in its own temp dir, a new Service (no state
// shared with other tests) and a new HTTP server.
func newEnv(t testing.TB) *env {
	t.Helper()
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, SetupToken: "tok", UploadDir: dir + "/uploads", MaxUploadBytes: 1 << 20, PublicURL: "http://test",
		Webhook: app.WebhookConfig{AllowLoopback: true, RetryBase: 20 * time.Millisecond, RetryMax: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond}})
	h := httpapi.New(svc, log, httpapi.Config{MCP: mcpserver.HTTPHandler(svc, "test"), OpenAPISpec: []byte("openapi: 3.1.0")})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, svc: svc, srv: srv, browser: &http.Client{Jar: jar}}
	e.signup()
	return e
}

// signup creates the workspace: e.admin is the admin key sign-up returns, e.key an agent key with
// app.DefaultKeyScopes (what most tests act as).
func (e *env) signup() {
	e.t.Helper()
	var res struct {
		APIKey      app.APIKey `json:"api_key"`
		RecoveryKey string     `json:"recovery_key"`
		CSRF        string     `json:"csrf_token"`
	}
	e.do(e.browser, "POST", "/v1/workspaces", "", map[string]string{"name": "Test", "setup_token": "tok"}, 201, &res)
	e.admin, e.recovery, e.csrf = res.APIKey.Key, res.RecoveryKey, res.CSRF
	var k app.APIKey
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "Default key", "scopes": app.DefaultKeyScopes}, 201, &k, "X-CSRF-Token", e.csrf)
	e.key = k.Key
}

// do sends a JSON request. auth is a bearer key; "" means none (cookies may apply).
func (e *env) do(c *http.Client, method, path, auth string, body any, want int, out any, hdr ...string) *http.Response {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if want != 0 && resp.StatusCode != want {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("decode %s: %v: %s", path, err, raw)
		}
	}
	return resp
}

// paid moves the test workspace to Pro with no end date (new workspaces start
// on a 7-day Pro trial): Free allows only the setup key. free moves it back
// (keys created meanwhile stay valid; limits only block new resources).
func (e *env) paid() { e.plan("pro") }
func (e *env) free() { e.plan("free") }

func (e *env) plan(id string) {
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if err := e.svc.SetPlan(context.Background(), ws.ID, id, ""); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) keyWith(scopes ...string) string {
	e.paid()
	var k app.APIKey
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "k", "scopes": scopes}, 201, &k, "X-CSRF-Token", e.csrf)
	return k.Key
}

func (e *env) collection(name string) app.Collection {
	var c app.Collection
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": name}, 201, &c)
	return c
}

// post creates a draft post in a collection (content "hello" unless the body says otherwise).
func (e *env) post(colID string, body map[string]any) app.Post {
	if body == nil {
		body = map[string]any{}
	}
	body["collection"] = colID
	if _, ok := body["content"]; !ok {
		body["content"] = "hello"
	}
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, body, 201, &post)
	return post
}

// customChannel connects a channel on "your own automation" (no credentials) with the admin key.
func (e *env) customChannel(name string) app.Channel {
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": name}, 201, &ch)
	return ch
}

func TestCloudSignupCreatesIsolatedWorkspacesAndIsRateLimited(t *testing.T) {
	e := newEnv(t) // one signup already used from 127.0.0.1
	e.collection("Mine")
	var created, limited atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res struct {
				APIKey    app.APIKey    `json:"api_key"`
				Workspace app.Workspace `json:"workspace"`
			}
			resp := e.do(nil, "POST", "/v1/workspaces", "", map[string]string{"name": "other", "setup_token": "tok"}, 0, &res)
			switch resp.StatusCode {
			case 201:
				created.Add(1)
				if res.Workspace.Plan != "pro" { // 7-day trial
					t.Errorf("new workspace plan %q", res.Workspace.Plan)
				}
				// A new workspace sees none of the first workspace's data.
				var list struct{ Data []app.Collection }
				e.do(nil, "GET", "/v1/collections", res.APIKey.Key, nil, 200, &list)
				if len(list.Data) != 1 || !list.Data[0].IsDefault { // only its own Default collection
					t.Errorf("workspace isolation broken: %d collections visible", len(list.Data))
				}
			case 429:
				limited.Add(1)
			default:
				t.Errorf("unexpected status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 4 || limited.Load() != 6 {
		t.Fatalf("want 4 signups + 6 rate-limited (5/hour/IP), got %d/%d", created.Load(), limited.Load())
	}
	var plans struct{ Data []app.Plan }
	e.do(nil, "GET", "/v1/plans", "", nil, 200, &plans)
	if len(plans.Data) != 2 || plans.Data[0].Limits.PostsPerMonth != 50 {
		t.Fatalf("plans: %+v", plans.Data)
	}
}

func TestConcurrentFirstSetup(t *testing.T) {
	database, _ := storetest.Open(t, t.TempDir()+"/w.db")
	defer database.Close()
	svc := app.New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	ids := sync.Map{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Setup(context.Background(), "ws", "", "ua")
			if err != nil {
				t.Error(err)
				return
			}
			ids.Store(res.Workspace.ID, true)
		}()
	}
	wg.Wait()
	n := 0
	ids.Range(func(_, _ any) bool { n++; return true })
	if n != 8 {
		t.Fatalf("want 8 distinct workspaces, got %d", n)
	}
}

func TestSetupTokenRequired(t *testing.T) {
	database, _ := storetest.Open(t, t.TempDir()+"/w.db")
	defer database.Close()
	svc := app.New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	if !svc.SignupOpen() {
		t.Fatal("signup should be open without SETUP_TOKEN")
	}
	svc.Cfg.SetupToken = "s3cret"
	if _, err := svc.Setup(context.Background(), "ws", "wrong", ""); err == nil {
		t.Fatal("setup without the token must fail")
	}
	if _, err := svc.Setup(context.Background(), "ws", "s3cret", ""); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeyAuthAndScopes(t *testing.T) {
	e := newEnv(t)
	e.do(nil, "GET", "/v1/collections", "", nil, 401, nil)
	e.do(nil, "GET", "/v1/collections", "stk_bogus", nil, 401, nil)
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, nil)

	ro := e.keyWith(app.ScopeCollectionsRead, app.ScopePostsRead)
	e.do(nil, "GET", "/v1/collections", ro, nil, 200, nil)
	e.do(nil, "POST", "/v1/collections", ro, map[string]any{"name": "nope"}, 403, nil)

	// The default key is not admin: it cannot manage keys.
	e.do(nil, "GET", "/v1/api-keys", e.key, nil, 403, nil)

	// Revocation takes effect immediately.
	var keys struct{ Data []app.APIKey }
	e.do(e.browser, "GET", "/v1/api-keys", "", nil, 200, &keys)
	var roID string
	for _, k := range keys.Data {
		if strings.HasPrefix(ro, k.Prefix) && k.Name == "k" {
			roID = k.ID
		}
	}
	e.do(e.browser, "DELETE", "/v1/api-keys/"+roID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
	e.do(nil, "GET", "/v1/collections", ro, nil, 401, nil)

	// Rotation: old key dies, new key works with the same scopes.
	var rotated app.APIKey
	var defID string
	for _, k := range keys.Data {
		if k.Name == "Default key" {
			defID = k.ID
		}
	}
	e.do(e.browser, "POST", "/v1/api-keys/"+defID+"/rotate", "", nil, 201, &rotated, "X-CSRF-Token", e.csrf)
	e.do(nil, "GET", "/v1/collections", e.key, nil, 401, nil)
	e.do(nil, "POST", "/v1/collections", rotated.Key, map[string]any{"name": "after rotate"}, 201, nil)

	// Raw keys are never stored.
	var n int
	e.svc.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE key_hash = ? OR prefix = ?", rotated.Key, rotated.Key).Scan(&n)
	if n != 0 {
		t.Fatal("raw API key found in database")
	}
}

func TestSessionsCSRFAndRecovery(t *testing.T) {
	e := newEnv(t)
	// Cookie auth works for reads, but unsafe requests need the CSRF token.
	e.do(e.browser, "GET", "/v1/collections", "", nil, 200, nil)
	e.do(e.browser, "POST", "/v1/collections", "", map[string]any{"name": "a"}, 403, nil)
	e.do(e.browser, "POST", "/v1/collections", "", map[string]any{"name": "a"}, 403, nil, "X-CSRF-Token", "wrong")
	e.do(e.browser, "POST", "/v1/collections", "", map[string]any{"name": "a"}, 201, nil, "X-CSRF-Token", e.csrf)

	var sess map[string]any
	e.do(e.browser, "GET", "/v1/session", "", nil, 200, &sess)
	if sess["csrf_token"] != e.csrf {
		t.Fatal("session endpoint must return the csrf token")
	}

	// The recovery key is not an API key.
	e.do(nil, "GET", "/v1/collections", e.recovery, nil, 401, nil)
	// API keys cannot rotate the recovery key, even admin ones.
	admin := e.keyWith(app.ScopeAdmin)
	e.do(nil, "POST", "/v1/recovery-key/rotate", admin, nil, 403, nil)

	// Logout revokes the session server-side.
	e.do(e.browser, "DELETE", "/v1/session", "", nil, 204, nil, "X-CSRF-Token", e.csrf)
	e.do(e.browser, "GET", "/v1/collections", "", nil, 401, nil)

	// Recover with a wrong key fails; with the right key (any case/spacing) works.
	e.do(nil, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": "staqly-recovery-AAAAA"}, 401, nil)
	jar, _ := cookiejar.New(nil)
	b2 := &http.Client{Jar: jar}
	var rec struct {
		CSRF        string     `json:"csrf_token"`
		APIKey      app.APIKey `json:"api_key"`
		RecoveryKey string     `json:"recovery_key"`
	}
	e.do(b2, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": " " + strings.ToLower(e.recovery) + " "}, 201, &rec)
	e.do(b2, "GET", "/v1/collections", "", nil, 200, nil)

	// Rotating the recovery key from the dashboard invalidates the previous one.
	var rot struct {
		RecoveryKey string `json:"recovery_key"`
	}
	e.do(b2, "POST", "/v1/recovery-key/rotate", "", nil, 200, &rot, "X-CSRF-Token", rec.CSRF)
	e.do(nil, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": rec.RecoveryKey}, 401, nil)
	e.do(nil, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": rot.RecoveryKey}, 201, nil)
}

// Sign-in with an API key: admin keys open a session, agent keys never do; revoked and expired keys are
// refused; other sessions stay signed in; the sign-in is recorded.
func TestSignInWithAPIKey(t *testing.T) {
	e := newEnv(t)
	signIn := func(key string, want int) (*http.Client, string) {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar}
		var out struct {
			CSRF string `json:"csrf_token"`
		}
		var body struct{ Error app.Error }
		res := any(&out)
		if want != 201 {
			res = &body
		}
		e.do(c, "POST", "/v1/sessions/api-key", "", map[string]string{"api_key": key}, want, res)
		return c, out.CSRF
	}

	// The key sign-up returns is an admin key and signs in (surrounding spaces are ignored).
	c, csrf := signIn(" "+e.admin+" ", 201)
	e.do(c, "GET", "/v1/session", "", nil, 200, nil)
	e.do(c, "POST", "/v1/collections", "", map[string]any{"name": "from key session"}, 201, nil, "X-CSRF-Token", csrf)
	e.do(e.browser, "GET", "/v1/session", "", nil, 200, nil) // the sign-up browser is still signed in

	// Agent keys (no admin scope) cannot open the dashboard.
	jar, _ := cookiejar.New(nil)
	agent := &http.Client{Jar: jar}
	var refused struct{ Error app.Error }
	e.do(agent, "POST", "/v1/sessions/api-key", "", map[string]string{"api_key": e.key}, 403, &refused)
	if refused.Error.Code != "forbidden" || !strings.Contains(refused.Error.Message, "admin scope") {
		t.Fatalf("agent key: %+v", refused.Error)
	}
	e.do(agent, "GET", "/v1/session", "", nil, 401, nil)
	signIn(e.keyWith(app.ScopeCollectionsRead, app.ScopePostsRead, app.ScopeChannelsRead), 403)

	// Unknown, revoked and expired keys: 401.
	signIn("stk_bogus", 401)
	signIn("", 401)
	signIn(e.recovery, 401) // the recovery key is not an API key
	extra := e.keyWith(app.ScopeAdmin)
	var keys struct{ Data []app.APIKey }
	e.do(e.browser, "GET", "/v1/api-keys", "", nil, 200, &keys)
	for _, k := range keys.Data {
		if strings.HasPrefix(extra, k.Prefix) {
			e.do(e.browser, "DELETE", "/v1/api-keys/"+k.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
		}
	}
	signIn(extra, 401)
	var expiring app.APIKey
	// A key can't be created already expired (422); one that expires meanwhile can't sign in.
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "soon", "scopes": []string{"admin"}, "expires_at": "2000-01-01T00:00:00Z"}, 422, nil, "X-CSRF-Token", e.csrf)
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "soon", "scopes": []string{"admin"},
		"expires_at": time.Now().UTC().Add(300 * time.Millisecond).Format(time.RFC3339Nano)}, 201, &expiring, "X-CSRF-Token", e.csrf)
	time.Sleep(400 * time.Millisecond)
	signIn(expiring.Key, 401)

	// Recorded in the activity log as the key that signed in, without the key itself.
	var evs struct{ Data []app.Event }
	e.do(e.browser, "GET", "/v1/events?type=session.signed_in", "", nil, 200, &evs)
	if len(evs.Data) != 1 || evs.Data[0].Actor.Type != app.PrincipalAPIKey || evs.Data[0].Actor.Name != "Admin key" {
		t.Fatalf("sign-in events: %+v", evs.Data)
	}
	raw, _ := json.Marshal(evs.Data)
	if strings.Contains(string(raw), e.admin) {
		t.Fatal("event leaks the API key")
	}
}

// Recovery is single-use: it returns a new admin key and the next recovery key, the old recovery key stops
// working, existing API keys keep working, and every other session is signed out.
func TestRecoveryRotatesAndIssuesAdminKey(t *testing.T) {
	e := newEnv(t)
	e.free() // Free (one key): the new admin key replaces the lost one
	jar, _ := cookiejar.New(nil)
	b := &http.Client{Jar: jar}
	var rec struct {
		CSRF        string     `json:"csrf_token"`
		APIKey      app.APIKey `json:"api_key"`
		RecoveryKey string     `json:"recovery_key"`
	}
	e.do(b, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": e.recovery}, 201, &rec)
	if rec.CSRF == "" || !strings.HasPrefix(rec.APIKey.Key, "stk_") || !strings.HasPrefix(rec.RecoveryKey, "staqly-recovery-") ||
		rec.RecoveryKey == e.recovery || len(rec.APIKey.Scopes) != 1 || rec.APIKey.Scopes[0] != app.ScopeAdmin {
		t.Fatalf("recover response: %+v", rec)
	}
	// The old recovery key is gone; the new one works once.
	e.do(nil, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": e.recovery}, 401, nil)
	// On Free the lost admin key is revoked (the limit holds); other keys keep working; the new admin key
	// signs in; the old browser session is signed out.
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, nil)
	e.do(nil, "GET", "/v1/collections", e.admin, nil, 401, nil)
	e.do(e.browser, "GET", "/v1/session", "", nil, 401, nil)
	e.do(b, "GET", "/v1/session", "", nil, 200, nil)
	e.do(nil, "POST", "/v1/sessions/api-key", "", map[string]string{"api_key": rec.APIKey.Key}, 201, nil)
	// Raw secrets are never stored.
	var n int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE recovery_verifier = ?`, rec.RecoveryKey).Scan(&n)
	if n != 0 {
		t.Fatal("raw recovery key stored")
	}

	// Concurrent use of the same recovery key: exactly one wins.
	key := rec.RecoveryKey
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 5 {
		wg.Go(func() {
			if _, err := e.svc.Recover(context.Background(), key, "ua"); err == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d concurrent recoveries succeeded with one key, want 1", ok.Load())
	}
	// Every admin key recovery replaced was revoked; exactly one stays active (the Free plan's limit).
	var recovered, admins int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type = 'session.recovered'`).Scan(&recovered)
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL AND scopes LIKE '%admin%'`).Scan(&admins)
	if recovered != 2 || admins != 1 {
		t.Fatalf("session.recovered events: %d, active admin keys: %d", recovered, admins)
	}
}

func TestRecoveryKeepsKeysOnUnlimitedPlans(t *testing.T) {
	e := newEnv(t) // Pro trial: no key limit
	e.do(nil, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": e.recovery}, 201, nil)
	e.do(nil, "GET", "/v1/collections", e.admin, nil, 200, nil)
}

func TestCollectionCRUD(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Generated Images")
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "generated images"}, 409, nil)
	e.do(nil, "GET", "/v1/collections/Generated%20Images", e.key, nil, 200, nil)
	if c.PostCount != 0 || c.ProcessingTimeoutSeconds != nil {
		t.Fatalf("new collection: %+v", c)
	}
	var upd app.Collection
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"name": "Renamed", "processing_timeout_seconds": 600}, 200, &upd)
	if upd.Name != "Renamed" || upd.ProcessingTimeoutSeconds == nil || *upd.ProcessingTimeoutSeconds != 600 {
		t.Fatalf("update failed: %+v", upd)
	}
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"processing_timeout_seconds": 10}, 422, nil)
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"processing_timeout_seconds": nil}, 200, &upd)
	if upd.ProcessingTimeoutSeconds != nil {
		t.Fatalf("null should clear the timeout: %+v", upd)
	}
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"archived": true}, 200, &upd)
	var list struct{ Data []app.Collection }
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &list)
	if len(list.Data) != 1 || !list.Data[0].IsDefault {
		t.Fatal("archived collection should be hidden by default")
	}
	e.do(nil, "GET", "/v1/collections?include_archived=true", e.key, nil, 200, &list)
	if len(list.Data) != 2 {
		t.Fatal("archived collection should be listed with include_archived")
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "x"}, 409, nil)
	// Deleting needs collections:delete: collections:write (the default key) is not enough.
	e.do(nil, "DELETE", "/v1/collections/"+c.ID+"?confirm=Renamed", e.key, nil, 403, nil)
	e.do(nil, "DELETE", "/v1/collections/"+c.ID+"?confirm=Renamed", e.keyWith(app.ScopeCollectionsWrite), nil, 403, nil)
	del := e.keyWith(app.ScopeCollectionsDelete)
	e.do(nil, "DELETE", "/v1/collections/"+c.ID, del, nil, 422, nil)
	e.do(nil, "DELETE", "/v1/collections/"+c.ID+"?confirm=Renamed", del, nil, 204, nil)
	e.do(nil, "GET", "/v1/collections/"+c.ID, e.key, nil, 404, nil)
}

func TestDefaultCollection(t *testing.T) {
	e := newEnv(t)
	var list struct{ Data []app.Collection }
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &list)
	if len(list.Data) != 1 || !list.Data[0].IsDefault || list.Data[0].Name != "Default" {
		t.Fatalf("new workspace should have exactly the Default collection: %+v", list.Data)
	}
	d := list.Data[0]
	e.do(nil, "PATCH", "/v1/collections/"+d.ID, e.key, map[string]any{"archived": true}, 409, nil)
	e.do(nil, "DELETE", "/v1/collections/"+d.ID+"?confirm=Default", e.admin, nil, 409, nil)
	var upd app.Collection
	e.do(nil, "PATCH", "/v1/collections/"+d.ID, e.key, map[string]any{"name": "Inbox"}, 200, &upd)
	if upd.Name != "Inbox" || !upd.IsDefault {
		t.Fatalf("rename should keep it the default: %+v", upd)
	}
	// Listed first, before alphabetically earlier names.
	e.collection("Aardvark")
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &list)
	if !list.Data[0].IsDefault {
		t.Fatalf("Default collection should be listed first: %s", list.Data[0].Name)
	}
	// Idempotent backfill on start.
	if err := e.svc.EnsureDefaultCollections(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("backfill must not add a second Default: %d", len(list.Data))
	}
}

func TestIdempotencyKeys(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Idem")
	body := map[string]any{"collection": c.ID, "content": "once"}
	var a, b app.Post
	r1 := e.do(nil, "POST", "/v1/posts", e.key, body, 201, &a, "Idempotency-Key", "k1")
	r2 := e.do(nil, "POST", "/v1/posts", e.key, body, 201, &b, "Idempotency-Key", "k1")
	if a.ID != b.ID || r1.Header.Get("Idempotent-Replayed") != "" || r2.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay failed: %s vs %s", a.ID, b.ID)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "other"}, 422, nil, "Idempotency-Key", "k1")
	var page app.PostPage
	e.do(nil, "GET", "/v1/collections/"+c.ID+"/posts", e.key, nil, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("want 1 post, got %d", len(page.Data))
	}
}

func multipartBody(t testing.TB, fields map[string]string, filename string, content []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	fw, _ := w.CreateFormFile("file", filename)
	fw.Write(content)
	w.Close()
	return &buf, w.FormDataContentType()
}

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

func (e *env) upload(path, key string, fields map[string]string, filename string, content []byte, want int, out any) {
	e.t.Helper()
	body, ct := multipartBody(e.t, fields, filename, content)
	req, _ := http.NewRequest("POST", e.srv.URL+path, body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		e.t.Fatalf("upload: %d want %d: %s", resp.StatusCode, want, raw)
	}
	if out != nil {
		json.Unmarshal(raw, out)
	}
}

func TestMediaUploadAndAuthorization(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Images")
	var m app.Media
	e.upload("/v1/media", e.key, nil, "../../etc/passwd.png", pngBytes, 201, &m)
	if m.MimeType != "image/png" || m.OriginalName != "passwd.png" {
		t.Fatalf("media: %+v", m)
	}
	it := e.post(c.ID, map[string]any{"media": []string{m.ID}})
	if len(it.MediaItems) != 1 || it.MediaItems[0].ID != m.ID {
		t.Fatalf("post media: %+v", it)
	}
	it.Media = []string{m.ID}
	e.upload("/v1/media", e.key, nil, "x.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 415, nil)
	e.upload("/v1/media", e.key, nil, "x.html", []byte(`<html><script>alert(1)</script></html>`), 415, nil)
	ro := e.keyWith(app.ScopePostsRead, app.ScopeMediaRead)
	e.upload("/v1/media", ro, nil, "x.png", pngBytes, 403, nil) // media:write is separate
	e.upload("/v1/media", e.key, nil, "big.png", append(pngBytes, make([]byte, 2<<20)...), 413, nil)

	e.do(nil, "GET", "/v1/media/"+m.ID, "", nil, 401, nil)
	resp := e.do(nil, "GET", "/v1/media/"+m.ID, ro, nil, 200, nil)
	if resp.Header.Get("Content-Type") != "image/png" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("media headers: %v", resp.Header)
	}
	e.do(e.browser, "GET", "/v1/media/"+m.ID, "", nil, 200, nil) // cookie auth for <img>
	e.do(nil, "DELETE", "/v1/media/"+m.ID, e.key, nil, 403, nil) // items:write is not enough
	e.do(nil, "DELETE", "/v1/media/"+m.ID, e.keyWith(app.ScopePostsWrite), nil, 403, nil)
	e.do(nil, "DELETE", "/v1/media/"+m.ID, e.keyWith(app.ScopePostsDelete), nil, 204, nil)
	e.do(nil, "GET", "/v1/media/"+m.ID, e.key, nil, 404, nil)
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+it.ID, e.key, nil, 200, &after)
	if len(after.Media) != 0 || len(after.MediaItems) != 0 {
		t.Fatal("post should lose deleted media")
	}
}

// A second workspace (multi-workspace isn't exposed yet, but data must already
// be isolated by workspace_id) cannot see or touch the first one's data.
func TestTenantIsolation(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Mine")
	it := e.post(c.ID, map[string]any{"content": "secret"})
	var med app.Media
	e.upload("/v1/media", e.key, nil, "a.png", pngBytes, 201, &med)

	ctx := context.Background()
	q := db.New(e.svc.DB)
	if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_other", Name: "Other", RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := app.Principal{WorkspaceID: "ws_other", Kind: app.PrincipalAPIKey, ID: "key_other", Name: "other", Scopes: app.DefaultKeyScopes}
	if _, err := e.svc.GetPost(ctx, other, it.ID); err == nil {
		t.Fatal("cross-workspace post read")
	}
	if _, err := e.svc.GetCollection(ctx, other, c.ID); err == nil {
		t.Fatal("cross-workspace collection read")
	}
	if _, err := e.svc.PublishPost(ctx, other, it.ID); err == nil {
		t.Fatal("cross-workspace publish")
	}
	if _, err := e.svc.CreatePost(ctx, other, app.PostInput{Collection: c.ID, Content: "x"}); err == nil {
		t.Fatal("cross-workspace create")
	}
	if _, _, err := e.svc.OpenMedia(ctx, other, med.ID); err == nil {
		t.Fatal("cross-workspace media read")
	}
	page, _ := e.svc.ListPosts(ctx, other, app.PostFilter{})
	evs, _ := e.svc.ListEvents(ctx, other, app.EventFilter{})
	if len(page.Data) != 0 || len(evs.Data) != 0 {
		t.Fatal("cross-workspace listing")
	}
}

type hookReq struct {
	Headers http.Header
	Body    []byte
}

func TestWebhookDeliveryRetriesAndSignature(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var got []hookReq
	var calls atomic.Int32
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, hookReq{r.Header.Clone(), b})
		mu.Unlock()
		if calls.Add(1) <= 2 {
			w.WriteHeader(500) // first two attempts fail
			return
		}
		w.WriteHeader(204)
	}))
	defer recv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	c := e.collection("Hooks")
	ch := e.customChannel("Bot")
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": recv.URL, "events": []string{"post.published"}}, 201, &wh)
	it := e.post(c.ID, map[string]any{"content": "x", "targets": []map[string]any{{"channel_id": ch.ID}}}) // post.created: not subscribed
	e.do(nil, "POST", "/v1/posts/"+it.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx) // the custom target now waits for the automation's result
	e.do(nil, "POST", "/v1/posts/"+it.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "published", "url": "https://example.com/p/1"}, 200, nil)

	var dl []app.Delivery
	waitFor(t, func() bool {
		var res struct{ Data []app.Delivery }
		e.do(nil, "GET", "/v1/webhook-deliveries?webhook_id="+wh.ID, e.key, nil, 200, &res)
		dl = res.Data
		return len(dl) == 1 && dl[0].Status == "succeeded"
	})
	if dl[0].Attempts != 3 || dl[0].EventType != "post.published" {
		t.Fatalf("delivery: %+v", dl[0])
	}
	mu.Lock()
	last := got[len(got)-1]
	mu.Unlock()
	ts := last.Headers.Get("X-Staqly-Timestamp")
	var tsn int64
	fmt.Sscan(ts, &tsn)
	if last.Headers.Get("X-Staqly-Signature") != app.Sign(wh.Secret, tsn, last.Body) {
		t.Fatal("bad signature")
	}
	if got[0].Headers.Get("X-Staqly-Delivery") != last.Headers.Get("X-Staqly-Delivery") || last.Headers.Get("X-Staqly-Event-Id") == "" {
		t.Fatal("delivery id must be stable across retries for dedupe")
	}
	var payload struct {
		Type string
		Data struct{ Post app.Post }
	}
	json.Unmarshal(last.Body, &payload)
	if payload.Type != "post.published" || payload.Data.Post.Status != "published" || payload.Data.Post.Targets[0].ExternalURL != "https://example.com/p/1" {
		t.Fatalf("payload: %s", last.Body)
	}

	// Terminal failure after max_attempts, then manual retry.
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer gone.Close()
	var wh2 app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": gone.URL, "events": []string{"post.*"}, "max_attempts": 2}, 201, &wh2)
	e.post(c.ID, map[string]any{"content": "y"})
	waitFor(t, func() bool {
		var res struct{ Data []app.Delivery }
		e.do(nil, "GET", "/v1/webhook-deliveries?status=failed&webhook_id="+wh2.ID, e.key, nil, 200, &res)
		dl = res.Data
		return len(dl) == 1
	})
	if dl[0].Attempts != 2 || dl[0].LastStatusCode == nil || *dl[0].LastStatusCode != 503 {
		t.Fatalf("failed delivery: %+v", dl[0])
	}
	e.do(nil, "POST", "/v1/webhook-deliveries/"+dl[0].ID+"/retry", e.key, nil, 202, nil)
	waitFor(t, func() bool {
		var d struct{ Data []app.Delivery }
		e.do(nil, "GET", "/v1/webhook-deliveries?status=failed&webhook_id="+wh2.ID, e.key, nil, 200, &d)
		return len(d.Data) == 1 && d.Data[0].Attempts == 2 && d.Data[0].LastAttemptAt != nil && *d.Data[0].LastAttemptAt > *dl[0].LastAttemptAt
	})
}

func TestWebhookSSRFPolicy(t *testing.T) {
	database, _ := storetest.Open(t, t.TempDir()+"/w.db")
	defer database.Close()
	svc := app.New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), app.Config{Mode: app.ModeCloud, SetupToken: "tok", UploadDir: t.TempDir(),
		Webhook: app.WebhookConfig{AllowLoopback: false, RetryBase: time.Millisecond, PollInterval: 5 * time.Millisecond}})
	res, err := svc.Setup(context.Background(), "w", "tok", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := svc.AuthenticateAPIKey(context.Background(), res.APIKey.Key)
	var hit atomic.Bool
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Store(true) }))
	defer recv.Close()
	for _, bad := range []string{"ftp://x", "http://user:pw@host/", "not a url"} {
		if _, err := svc.CreateWebhook(context.Background(), p, app.WebhookInput{URL: &bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	u := recv.URL
	one := int64(1)
	wh, err := svc.CreateWebhook(context.Background(), p, app.WebhookInput{URL: &u, MaxAttempts: &one})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.RunWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	svc.TestWebhook(context.Background(), p, wh.ID)
	waitFor(t, func() bool {
		d, _ := svc.ListDeliveries(context.Background(), p, wh.ID, "failed", "", 10)
		return len(d) == 1 && strings.Contains(d[0].LastError, "loopback")
	})
	if hit.Load() {
		t.Fatal("loopback receiver was contacted despite policy")
	}
}

// runWorker runs the webhook worker until the returned stop is called.
func runWorker(e *env) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunWorker(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	// Generous: it returns as soon as cond holds, and a loaded CI machine can be slow.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// ---- MCP ----

func mcpCall(e *env, key, tool string, args map[string]any) (int, map[string]any) {
	e.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}}
	var out map[string]any
	resp := e.do(nil, "POST", "/mcp", key, body, 0, &out, "Accept", "application/json, text/event-stream")
	return resp.StatusCode, out
}

func isToolError(out map[string]any) bool {
	res, _ := out["result"].(map[string]any)
	return res == nil || res["isError"] == true
}

// Codex (OpenAI models) sends unset optional parameters as null; they count as omitted.
func TestMCPNullArgumentsAreOmitted(t *testing.T) {
	e := newEnv(t)
	e.collection("Drafts")
	_, out := mcpCall(e, e.key, "create_post", map[string]any{"collection": "Drafts", "content": "from codex",
		"media": nil, "targets": nil, "publish_at": nil, "metadata": nil, "tags": nil, "external_ref": nil})
	if isToolError(out) {
		t.Fatalf("create_post with nulls: %v", out)
	}
	post := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	if post["status"] != "draft" || post["content"] != "from codex" {
		t.Fatalf("created post: %v", post)
	}
}

func TestMCPHTTPAuthorization(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Instagram Avatar")
	code, _ := mcpCall(e, "", "list_collections", nil)
	if code != 401 {
		t.Fatalf("unauthenticated MCP: %d", code)
	}
	// Session cookies are not accepted for MCP (no CSRF surface).
	resp := e.do(e.browser, "POST", "/mcp", "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, 0, nil)
	if resp.StatusCode != 401 {
		t.Fatalf("cookie MCP: %d", resp.StatusCode)
	}

	ro := e.keyWith(app.ScopeCollectionsRead, app.ScopePostsRead)
	_, out := mcpCall(e, ro, "list_collections", nil)
	if isToolError(out) {
		t.Fatalf("read-only list failed: %v", out)
	}
	_, out = mcpCall(e, ro, "create_post", map[string]any{"collection": "Instagram Avatar", "content": "x"})
	if !isToolError(out) {
		t.Fatal("read-only key created a post via MCP")
	}
	_, out = mcpCall(e, ro, "create_collection", map[string]any{"name": "nope"})
	if !isToolError(out) {
		t.Fatal("read-only key created a collection via MCP")
	}

	ch := e.customChannel("Bot")
	creator := e.keyWith(app.ScopeCollectionsRead, app.ScopePostsRead, app.ScopePostsWrite)
	_, out = mcpCall(e, creator, "create_post", map[string]any{"collection": "Instagram Avatar", "content": "from agent",
		"targets": []map[string]any{{"channel_id": ch.ID}}})
	if isToolError(out) {
		t.Fatalf("create_post: %v", out)
	}
	post := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	if post["status"] != "draft" || post["collection_id"] != c.ID {
		t.Fatalf("created post: %v", post)
	}
	id := post["id"].(string)
	// Writing is separate from publishing.
	_, out = mcpCall(e, creator, "publish_post", map[string]any{"post_id": id})
	if !isToolError(out) || !strings.Contains(fmt.Sprint(out), "posts:publish") {
		t.Fatalf("creator key published via MCP: %v", out)
	}
	_, out = mcpCall(e, e.key, "publish_post", map[string]any{"post_id": id})
	if isToolError(out) {
		t.Fatalf("publish: %v", out)
	}
	_, out = mcpCall(e, e.key, "get_post_history", map[string]any{"post_id": id})
	h := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	if len(h["data"].([]any)) < 1 {
		t.Fatalf("history: %v", h)
	}
}

// The STDIO binary uses mcpserver.New with a key-bound principal; exercise
// that path end-to-end with a real MCP client over an in-memory transport.
func TestMCPClientEndToEnd(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.svc.AuthenticateAPIKey(ctx, e.key)
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	go mcpserver.New(e.svc, p, "test").Run(ctx, st)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "list_collections create_collection get_collection list_posts get_post create_post update_post search_posts schedule_post publish_post cancel_post get_post_history list_channels list_requested_work report_publish_result"
	have := map[string]bool{}
	for _, tl := range tools.Tools {
		have[tl.Name] = true
	}
	for _, n := range strings.Fields(want) {
		if !have[n] {
			t.Fatalf("missing tool %s", n)
		}
	}
	call := func(name string, args map[string]any) map[string]any {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		var m map[string]any
		b, _ := json.Marshal(res.StructuredContent)
		json.Unmarshal(b, &m)
		return m
	}
	call("create_collection", map[string]any{"name": "Instagram Avatar"})
	ch := e.customChannel("Bot")
	it := call("create_post", map[string]any{"collection": "instagram avatar", "content": "caption", "tags": []string{"launch"},
		"targets": []map[string]any{{"channel_id": ch.ID}}})
	drafts := call("list_posts", map[string]any{"status": "draft", "tag": "launch"})
	if len(drafts["data"].([]any)) != 1 {
		t.Fatalf("drafts: %v", drafts)
	}
	out := call("publish_post", map[string]any{"post_id": it["id"]})
	if out["status"] != "publishing" {
		t.Fatalf("publish: %v", out)
	}
	e.svc.RunDueJobsOnce(ctx)
	work := call("list_requested_work", nil)
	if w := work["work"].([]any); len(w) != 1 || w[0].(map[string]any)["kind"] != "target" {
		t.Fatalf("requested work: %v", work)
	}
	out = call("report_publish_result", map[string]any{"post_id": it["id"], "channel_id": ch.ID, "status": "published", "url": "https://example.com/p/1"})
	if out["status"] != "published" {
		t.Fatalf("report: %v", out)
	}
	found := call("search_posts", map[string]any{"query": "caption", "status": "published"})
	if len(found["data"].([]any)) != 1 {
		t.Fatalf("search published: %v", found)
	}
}

func TestBackupIsConsistent(t *testing.T) {
	if storetest.Postgres() {
		t.Skip("SQLite backup (VACUUM INTO); on PostgreSQL store.Backup refuses and points at pg_dump")
	}
	e := newEnv(t)
	c := e.collection("Backup")
	e.post(c.ID, map[string]any{"content": "keep me"})
	dest := t.TempDir() + "/backup.db"
	if err := store.Backup(context.Background(), e.svc.DB, dest); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(context.Background(), dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var title string
	if err := restored.QueryRow("SELECT content FROM posts").Scan(&title); err != nil || title != "keep me" {
		t.Fatalf("restored: %q %v", title, err)
	}
}

// ---- local mode ----

func newLocal(t *testing.T) (*app.Service, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.New(database, log, app.Config{UploadDir: dir + "/uploads", LocalHosts: []string{"staqly-api"}})
	if err := svc.EnsureLocalWorkspace(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureLocalWorkspace(context.Background()); err != nil { // idempotent
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.New(svc, log, httpapi.Config{MCP: mcpserver.HTTPHandler(svc, "test")}))
	t.Cleanup(srv.Close)
	return svc, srv
}

func localReq(t *testing.T, srv *httptest.Server, method, path, host, origin, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// A collection is addressed by ID or name. Names with "/", "&" or "+" escape to a URL that keeps a
// RawPath (n8n's encodeURIComponent, curl), which chi hands over still escaped; a literal "%20" in a
// name must not be decoded twice.
func TestCollectionByEscapedName(t *testing.T) {
	_, srv := newLocal(t)
	for _, c := range []struct{ name, path string }{
		{`Drafts a/b & c+d`, "Drafts%20a%2Fb%20%26%20c%2Bd"},
		{`Sale 50%20off`, "Sale%2050%2520off"},
		{`Avatars`, "avatars"},
	} {
		if code, body := localReq(t, srv, "POST", "/v1/collections", "", "", `{"name":`+jsonString(c.name)+`}`); code != 201 {
			t.Fatalf("create %q: %d %s", c.name, code, body)
		}
		code, body := localReq(t, srv, "GET", "/v1/collections/"+c.path, "", "", "")
		if code != 200 || !strings.Contains(body, `"name":`+jsonString(c.name)) {
			t.Fatalf("get %q by %s: %d %s", c.name, c.path, code, body)
		}
		if code, body := localReq(t, srv, "GET", "/v1/collections/"+c.path+"/posts", "", "", ""); code != 200 {
			t.Fatalf("list posts of %q by %s: %d %s", c.name, c.path, code, body)
		}
		if code, body := localReq(t, srv, "POST", "/v1/posts", "", "", `{"collection":`+jsonString(c.name)+`,"content":"t"}`); code != 201 {
			t.Fatalf("create post in %q by name: %d %s", c.name, code, body)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestLocalModeIsKeylessButLocalOnly(t *testing.T) {
	svc, srv := newLocal(t)
	code, body := localReq(t, srv, "GET", "/v1/setup", "", "", "")
	if code != 200 || !strings.Contains(body, `"mode":"local"`) || !strings.Contains(body, `"initialized":true`) || !strings.Contains(body, `"public_api_url":`) {
		t.Fatalf("setup status: %d %s", code, body)
	}
	if code, _ := localReq(t, srv, "POST", "/v1/workspaces", "", "", `{}`); code != 409 {
		t.Fatalf("setup must be disabled in local mode: %d", code)
	}
	if code, _ := localReq(t, srv, "POST", "/v1/sessions/api-key", "", "", `{"api_key":"stk_x"}`); code != 409 {
		t.Fatalf("API-key sign-in must be disabled in local mode: %d", code)
	}
	// No credentials needed from this machine (curl, agents, the dashboard proxy).
	for _, host := range []string{"", "localhost:3333", "api:3333", "staqly-api:3333", "host.docker.internal:3333", "[::1]:3333"} {
		if code, body := localReq(t, srv, "POST", "/v1/collections", host, "", `{"name":"c `+host+`"}`); code != 201 {
			t.Fatalf("host %q: %d %s", host, code, body)
		}
	}
	// The dashboard origin is fine; no CSRF token needed.
	if code, _ := localReq(t, srv, "POST", "/v1/collections", "localhost:3333", "http://localhost:8080", `{"name":"from dashboard"}`); code != 201 {
		t.Fatalf("local origin refused: %d", code)
	}
	// DNS rebinding (foreign Host) and cross-site pages (foreign Origin) are refused.
	if code, _ := localReq(t, srv, "GET", "/v1/collections", "evil.example:3333", "", ""); code != 403 {
		t.Fatalf("foreign host accepted: %d", code)
	}
	if code, _ := localReq(t, srv, "POST", "/v1/collections", "localhost:3333", "https://evil.example", `{"name":"x"}`); code != 403 {
		t.Fatalf("foreign origin accepted: %d", code)
	}
	if code, _ := localReq(t, srv, "POST", "/mcp", "evil.example", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); code != 403 {
		t.Fatalf("foreign host MCP accepted: %d", code)
	}
	// MCP works without a key.
	code, body = localReq(t, srv, "POST", "/mcp", "", "", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_post","arguments":{"collection":"from dashboard","content":"keyless"}}}`)
	if code != 200 || !strings.Contains(body, `"status":"draft"`) || strings.Contains(body, "isError") {
		t.Fatalf("local MCP: %d %s", code, body)
	}
	code, body = localReq(t, srv, "GET", "/v1/session", "", "", "")
	if code != 200 || !strings.Contains(body, `"type":"local"`) {
		t.Fatalf("session: %s", body)
	}
	// Events are attributed to the local principal.
	page, _ := svc.ListEvents(context.Background(), app.Principal{WorkspaceID: mustLocal(t, svc).WorkspaceID, Kind: app.PrincipalLocal}, app.EventFilter{Type: "post.created"})
	if len(page.Data) != 1 || page.Data[0].Actor.Type != "local" {
		t.Fatalf("events: %+v", page.Data)
	}
	// The CLI escape hatch issues a working recovery key (for moving to cloud mode).
	key, _, err := svc.ResetRecoveryKey(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	svc.Cfg.Mode = app.ModeCloud
	if _, err := svc.Recover(context.Background(), key, "ua"); err != nil {
		t.Fatalf("recover with CLI key: %v", err)
	}
	if code, _ := localReq(t, srv, "GET", "/v1/collections", "", "", ""); code != 401 {
		t.Fatalf("cloud mode must require credentials: %d", code)
	}
}

func mustLocal(t *testing.T, svc *app.Service) app.Principal {
	p, err := svc.LocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- plans & search ----

func TestLocalModeIsUnlocked(t *testing.T) {
	svc, srv := newLocal(t)
	p := mustLocal(t, svc)
	col, err := svc.CreateCollection(context.Background(), p, app.CollectionInput{Name: ptrTo("Big")})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 120 {
		if _, err := svc.CreatePost(context.Background(), p, app.PostInput{Collection: col.ID, Content: "x"}); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	code, body := localReq(t, srv, "GET", "/v1/workspace/usage", "", "", "")
	if code != 200 || !strings.Contains(body, `"id":"unlocked"`) || !strings.Contains(body, `"posts_per_month":120`) {
		t.Fatalf("usage: %d %s", code, body)
	}
}

func ptrTo[T any](v T) *T { return &v }

// ---- connect a device (QR) ----

func TestConnectDeviceLocalMode(t *testing.T) {
	svc, srv := newLocal(t)
	svc.Cfg.PublicDashboardURL = "http://192.168.1.20:8080"
	phoneHdr := func(req *http.Request) { req.Header.Set("X-Forwarded-Host", "192.168.1.20:8080") }
	jar, _ := cookiejar.New(nil)
	phone := &http.Client{Jar: jar}
	call := func(c *http.Client, method, path, body string, hdr ...func(*http.Request)) (int, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for _, h := range hdr {
			h(req)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// Through the dashboard proxy from the LAN, a phone is not local.
	code, out := call(phone, "GET", "/v1/collections", "", phoneHdr)
	if code != 403 || out["error"].(map[string]any)["code"] != "not_local" {
		t.Fatalf("unconnected phone: %d %v", code, out)
	}
	// The computer (local principal) creates a link.
	code, link := call(http.DefaultClient, "POST", "/v1/device-links", "{}")
	if code != 201 || link["reachable"] != true || !strings.HasPrefix(link["url"].(string), "http://192.168.1.20:8080/connect#") {
		t.Fatalf("link: %d %v", code, link)
	}
	token := strings.SplitN(link["url"].(string), "#", 2)[1]
	code, red := call(phone, "POST", "/v1/device-links/redeem", `{"token":"`+token+`"}`, phoneHdr)
	if code != 201 {
		t.Fatalf("redeem: %d %v", code, red)
	}
	csrf := red["csrf_token"].(string)
	if code, _ := call(phone, "GET", "/v1/collections", "", phoneHdr); code != 200 {
		t.Fatalf("connected phone read: %d", code)
	}
	if code, _ := call(phone, "POST", "/v1/collections", `{"name":"from phone"}`, phoneHdr); code != 403 {
		t.Fatalf("phone write without CSRF: %d", code)
	}
	if code, _ := call(phone, "POST", "/v1/collections", `{"name":"from phone"}`, phoneHdr, func(r *http.Request) { r.Header.Set("X-CSRF-Token", csrf) }); code != 201 {
		t.Fatalf("phone write: %d", code)
	}
	// Single use.
	jar2, _ := cookiejar.New(nil)
	if code, out := call(&http.Client{Jar: jar2}, "POST", "/v1/device-links/redeem", `{"token":"`+token+`"}`, phoneHdr); code != 410 || out["error"].(map[string]any)["code"] != "link_used" {
		t.Fatalf("reuse: %d %v", code, out)
	}
	if code, _ := call(http.DefaultClient, "POST", "/v1/device-links/redeem", `{"token":"nope"}`); code != 401 {
		t.Fatalf("bad token: %d", code)
	}
	// Expiry.
	_, link2 := call(http.DefaultClient, "POST", "/v1/device-links", "{}")
	svc.Now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	tok2 := strings.SplitN(link2["url"].(string), "#", 2)[1]
	if code, out := call(http.DefaultClient, "POST", "/v1/device-links/redeem", `{"token":"`+tok2+`"}`); code != 410 || out["error"].(map[string]any)["code"] != "link_expired" {
		t.Fatalf("expired: %d %v", code, out)
	}
	svc.Now = time.Now
	// Signing the same browser in again replaces its session instead of adding another one.
	_, again := call(http.DefaultClient, "POST", "/v1/device-links", "{}")
	tok3 := strings.SplitN(again["url"].(string), "#", 2)[1]
	code, red = call(phone, "POST", "/v1/device-links/redeem", `{"token":"`+tok3+`"}`, phoneHdr, func(r *http.Request) { r.Header.Set("X-CSRF-Token", csrf) })
	if code != 201 {
		t.Fatalf("redeem again: %d %v", code, red)
	}
	if code, _ := call(phone, "GET", "/v1/collections", "", phoneHdr); code != 200 {
		t.Fatalf("re-signed phone read: %d", code)
	}
	// The device shows up in the sessions list and can be revoked from the computer.
	_, sess := call(http.DefaultClient, "GET", "/v1/sessions", "")
	list := sess["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("sessions: %v", sess)
	}
	id := list[0].(map[string]any)["id"].(string)
	if code, _ := call(http.DefaultClient, "DELETE", "/v1/sessions/"+id, ""); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _ := call(phone, "GET", "/v1/collections", "", phoneHdr); code != 403 {
		t.Fatalf("revoked phone still has access: %d", code)
	}
	// Without PUBLIC_DASHBOARD_URL the computer's own (localhost) origin is used and flagged as unreachable.
	svc.Cfg.PublicDashboardURL = ""
	_, link3 := call(http.DefaultClient, "POST", "/v1/device-links", "{}", func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8080") })
	if link3["reachable"] != false || link3["hint"] == nil {
		t.Fatalf("unreachable link: %v", link3)
	}
	// An exposed dashboard opened as localhost: the link uses this computer's address on the network.
	orig := app.LANIP
	t.Cleanup(func() { app.LANIP = orig })
	svc.Cfg.DashboardExposed = true
	app.LANIP = func() string { return "192.168.1.7" }
	_, link4 := call(http.DefaultClient, "POST", "/v1/device-links", "{}", func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8080") })
	if link4["reachable"] != true || !strings.HasPrefix(link4["url"].(string), "http://192.168.1.7:8080/connect#") {
		t.Fatalf("LAN link: %v", link4)
	}
	// A phone opening that address (unknown to the config) is signed in by the link.
	jar4, _ := cookiejar.New(nil)
	phone4 := &http.Client{Jar: jar4}
	lanHdr := func(r *http.Request) { r.Header.Set("X-Forwarded-Host", "192.168.1.7:8080") }
	token4 := strings.SplitN(link4["url"].(string), "#", 2)[1]
	if code, out := call(phone4, "POST", "/v1/device-links/redeem", `{"token":"`+token4+`"}`, lanHdr); code != 201 {
		t.Fatalf("LAN redeem: %d %v", code, out)
	}
	if code, _ := call(phone4, "GET", "/v1/collections", "", lanHdr); code != 200 {
		t.Fatalf("LAN phone read: %d", code)
	}
	// No address found (in a container): unreachable, with a hint that needs no checkout.
	app.LANIP = func() string { return "" }
	_, link5 := call(http.DefaultClient, "POST", "/v1/device-links", "{}", func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8080") })
	if hint, _ := link5["hint"].(string); link5["reachable"] != false || strings.Contains(hint, ".env") || strings.Contains(hint, "compose") {
		t.Fatalf("container link: %v", link5)
	}
}

func TestConnectDeviceCloudMode(t *testing.T) {
	e := newEnv(t)
	var link app.DeviceLink
	e.do(e.browser, "POST", "/v1/device-links", "", map[string]any{}, 201, &link, "X-CSRF-Token", e.csrf, "Origin", "https://staqly.example.com")
	if !strings.HasPrefix(link.URL, "https://staqly.example.com/connect#") || !link.Reachable {
		t.Fatalf("link: %+v", link)
	}
	e.do(nil, "POST", "/v1/device-links", e.key, map[string]any{}, 403, nil) // API keys cannot mint sessions
	jar, _ := cookiejar.New(nil)
	phone := &http.Client{Jar: jar}
	e.do(phone, "POST", "/v1/device-links/redeem", "", map[string]string{"token": strings.SplitN(link.URL, "#", 2)[1]}, 201, nil)
	var sess map[string]any
	e.do(phone, "GET", "/v1/session", "", nil, 200, &sess)
	if sess["workspace"].(map[string]any)["name"] != "Test" {
		t.Fatalf("phone session: %v", sess)
	}
}

// With the dashboard open to the network (the compose default), a request arriving via
// the dashboard proxy can't be proven local even with a forged local Host, so
// it needs a session; agents talking to the API directly stay keyless.
func TestExposedDashboardRequiresSessions(t *testing.T) {
	svc, srv := newLocal(t)
	svc.Cfg.DashboardExposed = true
	forged := func(r *http.Request) { r.Header.Set("X-Forwarded-Host", "localhost:8080") }
	get := func(c *http.Client, path string, hdr ...func(*http.Request)) int {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		for _, h := range hdr {
			h(req)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(http.DefaultClient, "/v1/collections", forged); code != 403 {
		t.Fatalf("forged local Host through exposed dashboard: %d", code)
	}
	if code := get(http.DefaultClient, "/v1/collections"); code != 200 {
		t.Fatalf("direct agent access must stay keyless: %d", code)
	}
	// `server connect-link` (system principal) signs a browser in.
	link, err := svc.CreateDeviceLink(context.Background(), app.Principal{WorkspaceID: mustLocal(t, svc).WorkspaceID, Kind: app.PrincipalSystem, ID: "cli"}, "")
	if err != nil || !strings.HasPrefix(link.URL, "http://localhost:") {
		t.Fatalf("cli link: %v %+v", err, link)
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	body := strings.NewReader(`{"token":"` + strings.SplitN(link.URL, "#", 2)[1] + `"}`)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/device-links/redeem", body)
	req.Header.Set("Content-Type", "application/json")
	forged(req)
	resp, _ := browser.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("redeem: %d", resp.StatusCode)
	}
	if code := get(browser, "/v1/collections", forged); code != 200 {
		t.Fatalf("signed-in browser: %d", code)
	}
}

// ---- watchers ----

type feedServer struct {
	mu      sync.Mutex
	items   []string
	page    string
	robots  string
	failing bool
	json    string // served at /data.json
	xmlDoc  string // served at /data.xml
}

func (f *feedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/robots.txt":
		fmt.Fprint(w, f.robots)
	case f.failing:
		w.WriteHeader(500)
	case r.URL.Path == "/feed.xml":
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>t</title>`+strings.Join(f.items, "")+`</channel></rss>`)
	case r.URL.Path == "/page":
		fmt.Fprint(w, "<html><head><title>Status</title><script>var x=1</script></head><body>"+f.page+"</body></html>")
	case r.URL.Path == "/data.json":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, f.json)
	case r.URL.Path == "/data.xml":
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, f.xmlDoc)
	default:
		w.WriteHeader(404)
	}
}

func rssItem(guid, title, desc string) string {
	return fmt.Sprintf(`<item><guid>%s</guid><title>%s</title><link>https://example.com/%s</link><description><![CDATA[<p>%s</p>]]></description></item>`, guid, title, guid, desc)
}

// Audit C1: with the dashboard exposed, a request relayed by it must never get
// keyless local access — not with an empty X-Forwarded-Host (what the Next
// proxy sends when the client omits Host), and not from the dashboard
// container's address even with no forwarding header at all.
func TestExposedDashboardCannotBeBypassed(t *testing.T) {
	svc, srv := newLocal(t)
	svc.Cfg.DashboardExposed = true
	get := func(hdr func(*http.Request)) int {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/collections", nil)
		if hdr != nil {
			hdr(req)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(func(r *http.Request) { r.Header["X-Forwarded-Host"] = []string{""} }); code != 403 {
		t.Fatalf("empty X-Forwarded-Host: %d", code)
	}
	if code := get(nil); code != 200 {
		t.Fatalf("direct local agent: %d", code)
	}
	// The test client connects from 127.0.0.1; pretend that is the dashboard container.
	svc.Getenv = func(k string) string {
		if k == "DASHBOARD_HOST" {
			return "localhost"
		}
		return ""
	}
	srv2 := httptest.NewServer(httpapi.New(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Config{}))
	defer srv2.Close()
	req, _ := http.NewRequest("GET", srv2.URL+"/v1/collections", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("request from the dashboard container without forwarding headers: %d", resp.StatusCode)
	}
	// Not exposed (dashboard on localhost only): an empty forwarded host is still refused.
	svc.Cfg.DashboardExposed = false
	if code := get(func(r *http.Request) { r.Header["X-Forwarded-Host"] = []string{""} }); code != 403 {
		t.Fatalf("empty X-Forwarded-Host when not exposed: %d", code)
	}
}
