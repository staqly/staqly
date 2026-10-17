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
	"slices"
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
)

type env struct {
	t        *testing.T
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
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	database, err := store.Open(context.Background(), dir+"/workspace.db")
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

func (e *env) item(colID string, body map[string]any) app.Item {
	var it app.Item
	e.do(nil, "POST", "/v1/collections/"+colID+"/items", e.key, body, 201, &it)
	return it
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
	if len(plans.Data) != 2 || plans.Data[0].Limits.DecisionsPerMonth != 50 {
		t.Fatalf("plans: %+v", plans.Data)
	}
}

func TestConcurrentFirstSetup(t *testing.T) {
	database, _ := store.Open(context.Background(), t.TempDir()+"/w.db")
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
	database, _ := store.Open(context.Background(), t.TempDir()+"/w.db")
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

	ro := e.keyWith(app.ScopeCollectionsRead, app.ScopeItemsRead)
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
	signIn(e.keyWith(app.ScopeCollectionsRead, app.ScopeItemsRead, app.ScopeItemsReview), 403)

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
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "soon", "scopes": []string{"admin"}, "expires_at": "2000-01-01T00:00:00Z"}, 201, &expiring, "X-CSRF-Token", e.csrf)
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
	if len(c.Actions) != len(app.DefaultActions) {
		t.Fatalf("default actions not attached: %+v", c.Actions)
	}
	var upd app.Collection
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"name": "Renamed", "default_view": "table"}, 200, &upd)
	if upd.Name != "Renamed" || upd.DefaultView != "table" {
		t.Fatalf("update failed: %+v", upd)
	}
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{{"name": "approve"}}}, 422, nil)
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{{"name": "x", "set_status": "approved"}}}, 422, nil)
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
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x"}, 409, nil)
	e.do(nil, "DELETE", "/v1/collections/"+c.ID, e.key, nil, 422, nil)
	e.do(nil, "DELETE", "/v1/collections/"+c.ID+"?confirm=Renamed", e.key, nil, 204, nil)
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
	e.do(nil, "DELETE", "/v1/collections/"+d.ID+"?confirm=Default", e.key, nil, 409, nil)
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

func TestItemCRUDAndSearch(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Blog Posts")
	it := e.item(c.ID, map[string]any{"content": "Hello world\nbody", "metadata": map[string]any{"topic": "go"}, "source": "n8n"})
	if it.Type != "text" || it.Title != "Hello world" || it.Status != "pending" {
		t.Fatalf("unexpected defaults: %+v", it)
	}
	js := e.item(c.ID, map[string]any{"content": map[string]any{"a": 1}})
	if js.Type != "json" || js.Content != `{"a":1}` {
		t.Fatalf("json item: %+v", js)
	}
	link := e.item(c.ID, map[string]any{"url": "https://example.com"})
	if link.Type != "link" {
		t.Fatalf("link item: %+v", link)
	}
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"url": "javascript:alert(1)"}, 422, nil)
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"type": "banana"}, 422, nil)
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"bogus": 1}, 400, nil)
	// Collections can be addressed by name too.
	e.do(nil, "POST", "/v1/collections/Blog%20Posts/items", e.key, map[string]any{"content": "by name"}, 201, nil)

	var upd app.Item
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"metadata_patch": map[string]any{"topic": nil, "lang": "en"}, "expected_version": 1}, 200, &upd)
	if string(upd.Metadata) != `{"lang":"en"}` || upd.Version != 2 {
		t.Fatalf("metadata patch: %s v%d", upd.Metadata, upd.Version)
	}
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "stale", "expected_version": 1}, 409, nil)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"status": "approved"}, 422, nil)
	// A run starts after a decision: a pending item can't jump to completed.
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"status": "completed"}, 409, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)

	var page app.ItemPage
	e.do(nil, "GET", "/v1/items?q=hello", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != it.ID {
		t.Fatalf("search: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?q=lang", e.key, nil, 200, &page) // metadata is searchable
	if len(page.Data) != 1 {
		t.Fatalf("metadata search: %d", len(page.Data))
	}
	// Text with nothing searchable (symbols only, or one word over the length cap) matches nothing, not everything.
	for _, q := range []string{"%F0%9F%9A%80", "(", strings.Repeat("x", 300)} {
		e.do(nil, "GET", "/v1/items?q="+q, e.key, nil, 200, &page)
		if len(page.Data) != 0 {
			t.Fatalf("search %q: %d items", q, len(page.Data))
		}
	}
	e.do(nil, "GET", "/v1/collections/"+c.ID+"/items?limit=2", e.key, nil, 200, &page)
	if len(page.Data) != 2 || page.NextCursor == "" {
		t.Fatalf("pagination: %d %q", len(page.Data), page.NextCursor)
	}
	cursor := page.NextCursor
	page = app.ItemPage{}
	e.do(nil, "GET", "/v1/collections/"+c.ID+"/items?limit=2&cursor="+cursor, e.key, nil, 200, &page)
	if len(page.Data) != 2 || page.NextCursor != "" {
		t.Fatalf("page 2: %d %q", len(page.Data), page.NextCursor)
	}
	e.do(nil, "GET", "/v1/items?sort=title_asc", e.key, nil, 200, &page)
	e.do(nil, "GET", "/v1/items?sort=drop_table", e.key, nil, 422, nil)

	var vers struct{ Data []app.ItemVersion }
	e.do(nil, "GET", "/v1/items/"+it.ID+"/versions", e.key, nil, 200, &vers)
	if len(vers.Data) != 2 {
		t.Fatalf("versions: %d", len(vers.Data))
	}
	e.do(nil, "DELETE", "/v1/items/"+it.ID, e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 404, nil)
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?item_id="+it.ID, e.key, nil, 200, &evs)
	if len(evs.Data) != 4 || evs.Data[0].Type != "item.deleted" { // created, updated, status_changed, deleted
		t.Fatalf("history should survive deletion: %d events", len(evs.Data))
	}
}

func TestApprovalTransitionsAndScopes(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Instagram Avatar")
	it := e.item(c.ID, map[string]any{"title": "a1"})

	writer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", writer, nil, 403, nil)

	var out app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key,
		map[string]any{"feedback": "nice", "edits": map[string]any{"title": "a1 edited"}}, 200, &out)
	if out.Status != "approved" || out.DecidedAt == nil || out.Title != "a1 edited" || out.Feedback != "nice" {
		t.Fatalf("approve: %+v", out)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 409, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/reject", e.key, nil, 409, nil)

	// The decision is recorded as an immutable event.
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	if evs.Data[0].Type != "item.approved" || evs.Data[0].Actor.Type != "api_key" {
		t.Fatalf("events: %+v", evs.Data[0])
	}
	if _, err := e.svc.DB.Exec("UPDATE events SET type = 'tampered'"); err == nil {
		t.Fatal("events must be immutable")
	}

	it2 := e.item(c.ID, map[string]any{"title": "a2"})
	e.do(nil, "POST", "/v1/items/"+it2.ID+"/actions/reject", e.key, map[string]any{"feedback": "too dark"}, 200, &out)
	if out.Status != "rejected" || out.Feedback != "too dark" {
		t.Fatalf("reject: %+v", out)
	}

	// Bulk: one succeeds, one conflicts.
	it3 := e.item(c.ID, map[string]any{"title": "a3"})
	var bulk struct{ Data []app.BulkResult }
	e.do(nil, "POST", "/v1/items/bulk-actions", e.key, map[string]any{"ids": []string{it3.ID, it2.ID}, "action": "approve"}, 200, &bulk)
	if !bulk.Data[0].OK || bulk.Data[1].OK || bulk.Data[1].Error.Code != "conflict" {
		t.Fatalf("bulk: %+v", bulk.Data)
	}
}

func TestConcurrentDecisionsHaveOneWinner(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Race")
	it := e.item(c.ID, map[string]any{"title": "contested"})
	var ok, conflicts atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := "approve"
			if i%2 == 1 {
				action = "reject"
			}
			resp := e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/"+action, e.key, map[string]any{"feedback": "x"}, 0, nil)
			switch resp.StatusCode {
			case 200:
				ok.Add(1)
			case 409:
				conflicts.Add(1)
			default:
				t.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflicts.Load() != 19 {
		t.Fatalf("want 1 winner/19 conflicts, got %d/%d", ok.Load(), conflicts.Load())
	}
	var n int
	e.svc.DB.QueryRow("SELECT COUNT(*) FROM events WHERE item_id = ? AND type IN ('item.approved','item.rejected')", it.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("want exactly one decision event, got %d", n)
	}
}

func TestCustomActions(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Avatar")
	it := e.item(c.ID, map[string]any{"title": "x"})

	var acts struct{ Data []app.ActionDef }
	e.do(nil, "GET", "/v1/items/"+it.ID+"/actions", e.key, nil, 200, &acts)
	names := []string{}
	for _, a := range acts.Data {
		names = append(names, a.Name)
	}
	// Runs start after a decision: no regenerate while the item waits for review.
	if strings.Join(names, ",") != "approve,reject,archive" {
		t.Fatalf("available actions for pending item: %v", names)
	}

	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/regenerate", e.key, map[string]any{"feedback": "x"}, 409, nil) // not before a decision
	// Reject and ask to regenerate in one step.
	both := e.item(c.ID, map[string]any{"title": "y"})
	var regen app.Item
	e.do(nil, "POST", "/v1/items/"+both.ID+"/actions/reject", e.key, map[string]any{"feedback": "redo", "follow_up": "regenerate"}, 200, &regen)
	if regen.Decision == nil || *regen.Decision != "rejected" || regen.Run != "processing" {
		t.Fatalf("reject then regenerate: %v %s", regen.Decision, regen.Run)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"follow_up": "publish"}, 422, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/reject", e.key, map[string]any{"feedback": "too dark"}, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/regenerate", e.key, nil, 422, nil) // feedback required
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/publish", e.key, nil, 409, nil)    // only from approved
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/launch_rockets", e.key, nil, 404, nil)
	reviewer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsReview)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/regenerate", reviewer, map[string]any{"feedback": "x"}, 403, nil)

	var out app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/regenerate", e.key,
		map[string]any{"feedback": "more smiling", "payload": map[string]any{"seed": 7}}, 200, &out)
	if out.Status != "processing" {
		t.Fatalf("regenerate should set processing: %s", out.Status)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	var data map[string]any
	json.Unmarshal(evs.Data[0].Data, &data)
	if evs.Data[0].Type != "item.regenerate_requested" || data["feedback"] != "more smiling" || data["previous_status"] != "rejected" {
		t.Fatalf("event: %s %v", evs.Data[0].Type, data)
	}

	// Configurable without code: add a custom action.
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "upscale", "label": "Upscale", "event": "item.upscale_requested"}}}, 200, nil)
	// While processing nothing runs: the automation reports a result first.
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/upscale", e.key, nil, 409, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/result", e.key, map[string]any{"status": "failed", "error": "x"}, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/upscale", e.key, nil, 200, &out)
	if out.Status != "failed" {
		t.Fatal("action without set_status must keep status")
	}

	// An action that would set the status the item already has is hidden and refused.
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "shelve", "label": "Shelve", "event": "item.shelved", "set_status": "archived"}}}, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/shelve", e.key, nil, 200, &out)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/shelve", e.key, nil, 409, nil)
	e.do(nil, "GET", "/v1/items/"+it.ID+"/actions", e.key, nil, 200, &acts)
	for _, a := range acts.Data {
		if a.Name == "shelve" {
			t.Fatal("shelve must not be offered on an archived item")
		}
	}
}

func TestIdempotencyKeys(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Idem")
	body := map[string]any{"title": "once"}
	var a, b app.Item
	r1 := e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, body, 201, &a, "Idempotency-Key", "k1")
	r2 := e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, body, 201, &b, "Idempotency-Key", "k1")
	if a.ID != b.ID || r1.Header.Get("Idempotent-Replayed") != "" || r2.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay failed: %s vs %s", a.ID, b.ID)
	}
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "other"}, 422, nil, "Idempotency-Key", "k1")
	var page app.ItemPage
	e.do(nil, "GET", "/v1/collections/"+c.ID+"/items", e.key, nil, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("want 1 item, got %d", len(page.Data))
	}
}

func multipartBody(t *testing.T, fields map[string]string, filename string, content []byte) (*bytes.Buffer, string) {
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
	var it app.Item
	e.upload("/v1/collections/"+c.ID+"/items", e.key, map[string]string{"item": `{"title":"pic"}`}, "../../etc/passwd.png", pngBytes, 201, &it)
	if it.Type != "image" || it.Media == nil || it.Media.MimeType != "image/png" || it.Media.OriginalName != "passwd.png" {
		t.Fatalf("item: %+v", it.Media)
	}
	e.upload("/v1/media", e.key, nil, "x.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 415, nil)
	e.upload("/v1/media", e.key, nil, "x.html", []byte(`<html><script>alert(1)</script></html>`), 415, nil)
	ro := e.keyWith(app.ScopeItemsRead)
	e.upload("/v1/media", ro, nil, "x.png", pngBytes, 403, nil)
	e.upload("/v1/media", e.key, nil, "big.png", append(pngBytes, make([]byte, 2<<20)...), 413, nil)

	e.do(nil, "GET", "/v1/media/"+it.Media.ID, "", nil, 401, nil)
	resp := e.do(nil, "GET", "/v1/media/"+it.Media.ID, ro, nil, 200, nil)
	if resp.Header.Get("Content-Type") != "image/png" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("media headers: %v", resp.Header)
	}
	e.do(e.browser, "GET", "/v1/media/"+it.Media.ID, "", nil, 200, nil) // cookie auth for <img>
	e.do(nil, "DELETE", "/v1/media/"+it.Media.ID, e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/media/"+it.Media.ID, e.key, nil, 404, nil)
	var after app.Item
	e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &after)
	if after.Media != nil {
		t.Fatal("item should lose deleted media")
	}
}

// A second workspace (multi-workspace isn't exposed yet, but data must already
// be isolated by workspace_id) cannot see or touch the first one's data.
func TestTenantIsolation(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Mine")
	it := e.item(c.ID, map[string]any{"title": "secret"})
	var med app.Media
	e.upload("/v1/media", e.key, nil, "a.png", pngBytes, 201, &med)

	ctx := context.Background()
	q := db.New(e.svc.DB)
	if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_other", Name: "Other", RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := app.Principal{WorkspaceID: "ws_other", Kind: app.PrincipalAPIKey, ID: "key_other", Name: "other", Scopes: app.DefaultKeyScopes}
	if _, err := e.svc.GetItem(ctx, other, it.ID); err == nil {
		t.Fatal("cross-workspace item read")
	}
	if _, err := e.svc.GetCollection(ctx, other, c.ID); err == nil {
		t.Fatal("cross-workspace collection read")
	}
	if _, err := e.svc.RunAction(ctx, other, it.ID, "approve", app.ActionInput{}); err == nil {
		t.Fatal("cross-workspace approve")
	}
	if _, err := e.svc.CreateItem(ctx, other, c.ID, app.CreateItemInput{}); err == nil {
		t.Fatal("cross-workspace create")
	}
	if _, _, err := e.svc.OpenMedia(ctx, other, med.ID); err == nil {
		t.Fatal("cross-workspace media read")
	}
	page, _ := e.svc.ListItems(ctx, other, app.ItemFilter{})
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
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": recv.URL, "events": []string{"item.approved"}}, 201, &wh)
	it := e.item(c.ID, map[string]any{"title": "x"}) // item.created: not subscribed
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)

	var dl []app.Delivery
	waitFor(t, func() bool {
		var res struct{ Data []app.Delivery }
		e.do(nil, "GET", "/v1/webhook-deliveries?webhook_id="+wh.ID, e.key, nil, 200, &res)
		dl = res.Data
		return len(dl) == 1 && dl[0].Status == "succeeded"
	})
	if dl[0].Attempts != 3 || dl[0].EventType != "item.approved" {
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
		Data struct{ Item app.Item }
	}
	json.Unmarshal(last.Body, &payload)
	if payload.Type != "item.approved" || payload.Data.Item.Status != "approved" {
		t.Fatalf("payload: %s", last.Body)
	}

	// Terminal failure after max_attempts, then manual retry.
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer gone.Close()
	var wh2 app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": gone.URL, "events": []string{"item.*"}, "max_attempts": 2}, 201, &wh2)
	e.item(c.ID, map[string]any{"title": "y"})
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
	database, _ := store.Open(context.Background(), t.TempDir()+"/w.db")
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
	_, out := mcpCall(e, e.key, "create_item", map[string]any{"collection": "Drafts", "title": "from codex", "content": "hi",
		"media_base64": nil, "url": nil, "metadata": nil, "tags": nil, "expires_in": nil})
	if isToolError(out) {
		t.Fatalf("create_item with nulls: %v", out)
	}
	item := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	if item["type"] != "text" || item["title"] != "from codex" {
		t.Fatalf("created item: %v", item)
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

	ro := e.keyWith(app.ScopeCollectionsRead, app.ScopeItemsRead)
	_, out := mcpCall(e, ro, "list_collections", nil)
	if isToolError(out) {
		t.Fatalf("read-only list failed: %v", out)
	}
	_, out = mcpCall(e, ro, "create_item", map[string]any{"collection": "Instagram Avatar", "title": "x"})
	if !isToolError(out) {
		t.Fatal("read-only key created an item via MCP")
	}
	_, out = mcpCall(e, ro, "create_collection", map[string]any{"name": "nope"})
	if !isToolError(out) {
		t.Fatal("read-only key created a collection via MCP")
	}

	creator := e.keyWith(app.ScopeCollectionsRead, app.ScopeItemsRead, app.ScopeItemsWrite)
	_, out = mcpCall(e, creator, "create_item", map[string]any{"collection": "Instagram Avatar", "title": "from agent",
		"media_base64": "iVBORw0KGgoAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "media_filename": "a.png"})
	if isToolError(out) {
		t.Fatalf("create_item: %v", out)
	}
	item := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	if item["type"] != "image" || item["collection_id"] != c.ID {
		t.Fatalf("created item: %v", item)
	}
	id := item["id"].(string)
	// Creating is separate from reviewing.
	_, out = mcpCall(e, creator, "approve_item", map[string]any{"item_id": id})
	if !isToolError(out) {
		t.Fatal("creator key approved via MCP")
	}
	_, out = mcpCall(e, e.key, "reject_item", map[string]any{"item_id": id, "feedback": "off-brand"})
	if isToolError(out) {
		t.Fatalf("reject: %v", out)
	}
	_, out = mcpCall(e, e.key, "get_item_history", map[string]any{"item_id": id})
	h := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	if len(h["events"].([]any)) != 2 {
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
	want := "list_collections create_collection get_collection list_items get_item create_item update_item search_items list_pending_approvals approve_item reject_item list_available_actions execute_custom_action get_item_history"
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
	it := call("create_item", map[string]any{"collection": "instagram avatar", "url": "https://cdn.example.com/a.png", "type": "image", "content": "caption"})
	pending := call("list_pending_approvals", map[string]any{"type": "image"})
	if len(pending["data"].([]any)) != 1 {
		t.Fatalf("pending: %v", pending)
	}
	call("approve_item", map[string]any{"item_id": it["id"]})
	today := time.Now().UTC().Format("2006-01-02")
	found := call("search_items", map[string]any{"status": "approved", "type": "image", "decided_after": today})
	if len(found["data"].([]any)) != 1 {
		t.Fatalf("search approved today: %v", found)
	}
	acts := call("list_available_actions", map[string]any{"item_id": it["id"]})
	if len(acts["actions"].([]any)) != 2 { // publish, archive
		t.Fatalf("actions after approval: %v", acts)
	}
	out := call("execute_custom_action", map[string]any{"item_id": it["id"], "action": "publish"})
	if out["status"] != "processing" {
		t.Fatalf("publish: %v", out)
	}
	out = call("update_item", map[string]any{"item_id": it["id"], "status": "completed", "metadata_patch": map[string]any{"instagram_post_id": "123"}})
	if out["status"] != "completed" {
		t.Fatalf("update: %v", out)
	}
}

func TestBackupIsConsistent(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Backup")
	e.item(c.ID, map[string]any{"title": "keep me"})
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
	if err := restored.QueryRow("SELECT title FROM items").Scan(&title); err != nil || title != "keep me" {
		t.Fatalf("restored: %q %v", title, err)
	}
}

// ---- local mode ----

func newLocal(t *testing.T) (*app.Service, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	database, err := store.Open(context.Background(), dir+"/workspace.db")
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
	code, body = localReq(t, srv, "POST", "/mcp", "", "", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_item","arguments":{"collection":"from dashboard","title":"keyless"}}}`)
	if code != 200 || !strings.Contains(body, `"status":"pending"`) || strings.Contains(body, "isError") {
		t.Fatalf("local MCP: %d %s", code, body)
	}
	code, body = localReq(t, srv, "GET", "/v1/session", "", "", "")
	if code != 200 || !strings.Contains(body, `"type":"local"`) {
		t.Fatalf("session: %s", body)
	}
	// Events are attributed to the local principal.
	page, _ := svc.ListEvents(context.Background(), app.Principal{WorkspaceID: mustLocal(t, svc).WorkspaceID, Kind: app.PrincipalLocal}, app.EventFilter{Type: "item.created"})
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

func TestPlanLimitsAreEnforcedPerWorkspace(t *testing.T) {
	e := newEnv(t)
	e.svc.Getenv = func(k string) string { // 100+ requests in a burst: not what this test is about
		if k == "RATE_LIMIT_DISABLED" {
			return "true"
		}
		return ""
	}
	e.free() // new workspaces start on a Pro trial
	c := e.collection("Quota")
	p, _ := e.svc.AuthenticateAPIKey(context.Background(), e.key)
	// Items are unlimited; approvals and rejections are limited (50 a month on Free).
	ids := make([]string, 0, 52)
	for i := range 52 { // through the service: this tests quotas, not HTTP (which is rate limited)
		title := fmt.Sprintf("item %d", i)
		in := app.CreateItemInput{}
		in.Title = &title
		it, err := e.svc.CreateItem(context.Background(), p, c.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	for i, id := range ids[:50] {
		action := "approve"
		if i%2 == 1 {
			action = "reject"
		}
		if _, err := e.svc.RunAction(context.Background(), p, id, action, app.ActionInput{Feedback: "x"}); err != nil {
			t.Fatalf("decision %d: %v", i+1, err)
		}
	}
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/items/"+ids[50]+"/actions/approve", e.key, nil, 402, &errBody)
	if errBody.Error.Code != "plan_limit_exceeded" || errBody.Error.Details["limit"] != "decisions_per_month" {
		t.Fatalf("limit error: %+v", errBody.Error)
	}
	var still app.Item
	e.do(nil, "GET", "/v1/items/"+ids[50], e.key, nil, 200, &still)
	if still.Status != "pending" {
		t.Fatalf("a refused decision changed the item: %s", still.Status)
	}
	// Creating items, also already decided ones, is refused only for the decision part.
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "more"}, 201, nil)
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "decided", "status": "approved"}, 402, nil)

	// Webhooks: 3 on Free.
	for range 3 {
		e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "http://example.com/h"}, 201, nil)
	}
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "http://example.com/h"}, 402, nil)
	// API keys: 1 active on Free (the admin key; the agent key e.key was created during the trial).
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "k", "scopes": []string{"items:read"}}, 402, nil, "X-CSRF-Token", e.csrf)

	var usage app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &usage)
	if usage.Plan.ID != "free" || usage.Usage["decisions_per_month"] != 50 || usage.Usage["items_per_month"] != 53 ||
		usage.Usage["webhooks"] != 3 || usage.Usage["api_keys"] != 2 {
		t.Fatalf("usage: %+v", usage)
	}

	// The operator upgrades the workspace (no payments yet): limits lift.
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if err := e.svc.SetPlan(context.Background(), ws.ID, "pro", ""); err != nil {
		t.Fatal(err)
	}
	e.do(nil, "POST", "/v1/items/"+ids[50]+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "http://example.com/h"}, 201, nil)
	if err := e.svc.SetPlan(context.Background(), ws.ID, "platinum", ""); err == nil {
		t.Fatal("unknown plan accepted")
	}
}

func TestLocalModeIsUnlocked(t *testing.T) {
	svc, srv := newLocal(t)
	p := mustLocal(t, svc)
	col, err := svc.CreateCollection(context.Background(), p, app.CollectionInput{Name: ptrTo("Big")})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 120 {
		if _, err := svc.CreateItem(context.Background(), p, col.ID, app.CreateItemInput{}); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	code, body := localReq(t, srv, "GET", "/v1/workspace/usage", "", "", "")
	if code != 200 || !strings.Contains(body, `"id":"unlocked"`) || !strings.Contains(body, `"items_per_month":120`) {
		t.Fatalf("usage: %d %s", code, body)
	}
}

func ptrTo[T any](v T) *T { return &v }

func TestFullTextSearchAndKeysetPagination(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Search")
	e.item(c.ID, map[string]any{"title": "Zachód słońca na plaży", "content": "golden hour"})
	e.item(c.ID, map[string]any{"title": "Neon city", "content": "night walk", "metadata": map[string]any{"prompt": "cyberpunk sunset"}})
	e.item(c.ID, map[string]any{"title": "Sunset sunset sunset", "content": "sunset"})
	search := func(q string) []string {
		var page app.ItemPage
		e.do(nil, "GET", "/v1/items?q="+q, e.key, nil, 200, &page)
		titles := []string{}
		for _, it := range page.Data {
			titles = append(titles, it.Title)
		}
		return titles
	}
	if got := search("slonca"); len(got) != 1 { // diacritics-insensitive
		t.Fatalf("slonca: %v", got)
	}
	if got := search("gold"); len(got) != 1 { // prefix match
		t.Fatalf("gold: %v", got)
	}
	if got := search("sunset"); len(got) != 2 || got[0] != "Sunset sunset sunset" { // relevance; metadata searchable
		t.Fatalf("sunset: %v", got)
	}
	if got := search("sunset%20golden"); len(got) != 0 { // all words must match
		t.Fatalf("multi-word AND: %v", got)
	}
	if got := search("publish%20x"); len(got) != 0 { // word with "l" (ł variants) followed by another word: no 500
		t.Fatalf("multi-word with l: %v", got)
	}
	if got := search("zach%C3%B3d%20slonca"); len(got) != 1 {
		t.Fatalf("multi-word polish: %v", got)
	}
	if got := search(`"OR*(`); len(got) != 0 { // operators are neutralised, no 500
		t.Fatalf("operators: %v", got)
	}

	// Keyset pages stay stable when new items arrive between page loads.
	for i := range 7 {
		e.item(c.ID, map[string]any{"title": fmt.Sprintf("page item %d", i)})
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		var page app.ItemPage
		e.do(nil, "GET", "/v1/collections/"+c.ID+"/items?limit=3&cursor="+cursor, e.key, nil, 200, &page)
		for _, it := range page.Data {
			if seen[it.ID] {
				t.Fatalf("duplicate %s across pages", it.ID)
			}
			seen[it.ID] = true
		}
		if pages == 0 {
			e.item(c.ID, map[string]any{"title": "arrived mid-pagination"})
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 10 {
		t.Fatalf("want the 10 items that existed at page 1, got %d", len(seen))
	}
	e.do(nil, "GET", "/v1/items?cursor=garbage", e.key, nil, 422, nil)
	for _, sort := range []string{"title_asc", "decided_desc", "updated_asc", "relevance"} {
		var page app.ItemPage
		e.do(nil, "GET", "/v1/items?limit=2&sort="+sort, e.key, nil, 200, &page)
		e.do(nil, "GET", "/v1/items?limit=2&sort="+sort+"&cursor="+page.NextCursor, e.key, nil, 200, nil)
	}
}

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
	default:
		w.WriteHeader(404)
	}
}

func rssItem(guid, title, desc string) string {
	return fmt.Sprintf(`<item><guid>%s</guid><title>%s</title><link>https://example.com/%s</link><description><![CDATA[<p>%s</p>]]></description></item>`, guid, title, guid, desc)
}

func TestWatchers(t *testing.T) {
	e := newEnv(t)
	e.svc.Fetcher = http.DefaultClient // test server is on loopback, which the real client refuses
	feed := &feedServer{items: []string{rssItem("1", "Old post", "already there"), rssItem("2", "Older post", "x")}, page: "v1", robots: "User-agent: *\nDisallow: /private"}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	e.free() // new workspaces start on a Pro trial
	c := e.collection("Inspiration")
	var limit struct{ Error app.Error }
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "x", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.Name, "every": "1m"}, 402, &limit) // Free: >= 15m
	if !strings.HasSuffix(limit.Error.Message, "at most every 15m.") {
		t.Fatalf("interval message: %q", limit.Error.Message)
	}
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "x", "kind": "rss", "url": "ftp://x", "collection": c.Name}, 422, nil)

	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "Moodboard", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": "Inspiration"}, 201, &w)
	if w.Kind != "rss" || w.EverySeconds != 3600 || w.Status != "active" {
		t.Fatalf("watcher: %+v", w)
	}
	run := func(id string) app.WatcherRun {
		var r app.WatcherRun
		e.do(nil, "POST", "/v1/watchers/"+id+"/run", e.key, nil, 200, &r)
		return r
	}
	// First run is a baseline: existing entries are remembered, not published.
	if r := run(w.ID); !r.Baseline || r.NewItems != 0 || r.Seen != 2 {
		t.Fatalf("baseline: %+v", r)
	}
	feed.mu.Lock()
	feed.items = append([]string{rssItem("3", "Fresh &amp; new", "<b>golden hour</b> avatar idea")}, feed.items...)
	feed.mu.Unlock()
	if r := run(w.ID); r.NewItems != 1 || r.Baseline {
		t.Fatalf("second run: %+v", r)
	}
	if r := run(w.ID); r.NewItems != 0 {
		t.Fatalf("dedup: %+v", r)
	}
	var page app.ItemPage
	e.do(nil, "GET", "/v1/collections/"+c.ID+"/items", e.key, nil, 200, &page)
	it := page.Data[0]
	var meta map[string]any
	json.Unmarshal(it.Metadata, &meta)
	if len(page.Data) != 1 || it.Title != "Fresh & new" || it.Type != "link" || it.URL != "https://example.com/3" ||
		it.Content != "golden hour avatar idea" || it.Status != "pending" || meta["untrusted"] != true || it.Source != "watcher:Moodboard" {
		t.Fatalf("published item: %+v %v", it, meta)
	}

	// Keywords filter.
	kw := []string{"sunset"}
	e.do(nil, "PATCH", "/v1/watchers/"+w.ID, e.key, map[string]any{"keywords": kw}, 200, nil)
	feed.mu.Lock()
	feed.items = append([]string{rssItem("4", "Sunset shoot", "x"), rssItem("5", "Unrelated", "y")}, feed.items...)
	feed.mu.Unlock()
	if r := run(w.ID); r.NewItems != 1 {
		t.Fatalf("keywords: %+v", r)
	}

	// url_diff: baseline, unchanged, changed.
	var pw app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "Page", "kind": "url_diff", "url": srv.URL + "/page", "collection": c.ID}, 201, &pw)
	run(pw.ID)
	if r := run(pw.ID); r.NewItems != 0 {
		t.Fatalf("unchanged page: %+v", r)
	}
	feed.mu.Lock()
	feed.page = "v2 incident"
	feed.mu.Unlock()
	if r := run(pw.ID); r.NewItems != 1 {
		t.Fatalf("changed page: %+v", r)
	}

	// Without kind, staqly reads the URL and picks it: a feed is rss, a page url_diff.
	for path, want := range map[string]string{"/feed.xml": "rss", "/page": "url_diff"} {
		var dw app.Watcher
		e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "auto" + path, "url": srv.URL + path, "collection": c.ID}, 201, &dw)
		if dw.Kind != want {
			t.Fatalf("detected %s for %s, want %s", dw.Kind, path, want)
		}
		e.do(nil, "DELETE", "/v1/watchers/"+dw.ID, e.key, nil, 204, nil)
	}
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "gone", "url": srv.URL + "/private/x", "collection": c.ID}, 422, nil)

	// robots.txt is respected, for feeds too.
	var rw app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "private", "kind": "rss", "url": srv.URL + "/private/feed.xml", "collection": c.ID}, 201, &rw)
	if r := run(rw.ID); r.Status != "error" || !strings.Contains(r.Error, "robots_disallowed") {
		t.Fatalf("robots: %+v", r)
	}
	// Free plan: 3 active watchers.
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "4th", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 402, nil)

	// Failures degrade the watcher after 5 attempts; resuming resets it.
	feed.mu.Lock()
	feed.failing = true
	feed.mu.Unlock()
	for range 5 {
		run(w.ID)
	}
	var got app.Watcher
	e.do(nil, "GET", "/v1/watchers/"+w.ID, e.key, nil, 200, &got)
	if got.Status != "degraded" || got.FailCount != 5 || got.LastError == "" {
		t.Fatalf("degraded: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/watchers/"+w.ID, e.key, map[string]any{"status": "active"}, 200, &got)
	if got.Status != "active" || got.FailCount != 0 {
		t.Fatalf("resume: %+v", got)
	}
}

func TestWatcherFetchClientRefusesPrivateTargets(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(&feedServer{})
	defer srv.Close()
	c := e.collection("X")
	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "ssrf", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 201, &w)
	var r app.WatcherRun
	e.do(nil, "POST", "/v1/watchers/"+w.ID+"/run", e.key, nil, 200, &r)
	if r.Status != "error" || !strings.Contains(r.Error, "not allowed") {
		t.Fatalf("loopback fetch must be refused: %+v", r)
	}
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

func TestWatcherManyCollections(t *testing.T) {
	e := newEnv(t)
	e.svc.Fetcher = http.DefaultClient
	feed := &feedServer{items: []string{rssItem("1", "Old post", "x")}}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	a, b := e.collection("Blog"), e.collection("Social")

	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "News", "kind": "rss", "url": srv.URL + "/feed.xml",
		"collections": []string{a.ID, "Social", a.Name}}, 201, &w)
	if w.CollectionID != a.ID || !slices.Equal(w.CollectionIDs, []string{a.ID, b.ID}) {
		t.Fatalf("collections: %+v", w)
	}
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "x", "kind": "rss", "url": srv.URL + "/feed.xml", "collections": []string{}}, 422, nil)
	e.do(nil, "POST", "/v1/watchers/"+w.ID+"/run", e.key, nil, 200, nil) // baseline
	feed.mu.Lock()
	feed.items = append([]string{rssItem("2", "New post", "y")}, feed.items...)
	feed.mu.Unlock()
	var r app.WatcherRun
	e.do(nil, "POST", "/v1/watchers/"+w.ID+"/run", e.key, nil, 200, &r)
	if r.NewItems != 2 {
		t.Fatalf("run: %+v", r)
	}
	for _, c := range []app.Collection{a, b} {
		var page app.ItemPage
		e.do(nil, "GET", "/v1/collections/"+c.ID+"/items", e.key, nil, 200, &page)
		if len(page.Data) != 1 || page.Data[0].Title != "New post" {
			t.Fatalf("%s items: %+v", c.Name, page.Data)
		}
	}

	// Deleting the first collection keeps the watcher, adding to the rest.
	e.do(nil, "DELETE", "/v1/collections/"+a.ID+"?confirm=Blog", e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/watchers/"+w.ID, e.key, nil, 200, &w)
	if w.CollectionID != b.ID || !slices.Equal(w.CollectionIDs, []string{b.ID}) {
		t.Fatalf("after delete: %+v", w)
	}
	// The single collection field still works and replaces the list.
	e.do(nil, "PATCH", "/v1/watchers/"+w.ID, e.key, map[string]any{"collection": "Default"}, 200, &w)
	if len(w.CollectionIDs) != 1 || w.CollectionIDs[0] != w.CollectionID {
		t.Fatalf("collection: %+v", w)
	}
}
