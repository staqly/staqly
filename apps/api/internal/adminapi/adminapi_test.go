package adminapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/adminapi"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

type env struct {
	t    *testing.T
	svc  *app.Service
	h    http.Handler
	ck   *http.Cookie
	csrf string
}

func newEnv(t *testing.T, admins ...string) *env {
	t.Helper()
	database, err := storetest.Open(t, t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, TrialPlan: "pro", SetupToken: "tok", PublicURL: "http://test", DevSignIn: true, AdminEnabled: true, AdminEmails: admins})
	if err := svc.EnsureAdmins(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, svc: svc, h: adminapi.New(svc, log, adminapi.Config{})}
}

func (e *env) do(method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = "192.0.2.1:1234"
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if e.ck != nil {
		r.AddCookie(e.ck)
		if e.csrf != "" {
			r.Header.Set("X-CSRF-Token", e.csrf)
		}
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) want(w *httptest.ResponseRecorder, status int) {
	e.t.Helper()
	if w.Code != status {
		e.t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body)
	}
}

func (e *env) signIn() *httptest.ResponseRecorder {
	e.t.Helper()
	e.want(e.do("POST", "/v1/admin/auth/code", map[string]string{"email": "a@x.io"}), 204)
	code, ok := e.svc.DevAdminCode("a@x.io")
	if !ok {
		e.t.Fatal("no dev code")
	}
	w := e.do("POST", "/v1/admin/auth/verify", map[string]string{"email": "a@x.io", "code": code})
	e.want(w, 200)
	var out struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	e.ck, e.csrf = w.Result().Cookies()[0], out.CSRF
	return w
}

func (e *env) workspace() string {
	e.t.Helper()
	res, err := e.svc.Setup(context.Background(), "Acme", "tok", "ua")
	if err != nil {
		e.t.Fatal(err)
	}
	var m map[string]any
	b, _ := json.Marshal(res)
	json.Unmarshal(b, &m)
	for _, k := range []string{"workspace_id", "id"} {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	if ws, ok := m["workspace"].(map[string]any); ok {
		return ws["id"].(string)
	}
	e.t.Fatalf("no workspace id in %s", b)
	return ""
}

func TestAdminCodeAlways204(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.want(e.do("POST", "/v1/admin/auth/code", map[string]string{"email": "nobody@x.io"}), 204)
	if _, ok := e.svc.DevAdminCode("nobody@x.io"); ok {
		t.Fatal("code made for an unlisted address")
	}
	w := e.do("POST", "/v1/admin/auth/verify", map[string]string{"email": "nobody@x.io", "code": "123456"})
	e.want(w, 401)
}

func TestAdminSignInFlow(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.signIn()
	w := e.do("GET", "/v1/admin/me", nil)
	e.want(w, 200)
	if !strings.Contains(w.Body.String(), `"email":"a@x.io"`) || !strings.Contains(w.Body.String(), e.csrf) {
		t.Fatal(w.Body)
	}
	e.want(e.do("POST", "/v1/admin/auth/sign-out", nil), 204)
	e.want(e.do("GET", "/v1/admin/me", nil), 401)
}

func TestAdminNoCookie401(t *testing.T) {
	e := newEnv(t, "a@x.io")
	w := e.do("GET", "/v1/admin/workspaces", nil)
	e.want(w, 401)
	if !strings.Contains(w.Body.String(), `"code":"unauthorized"`) {
		t.Fatal(w.Body)
	}
	e.ck = &http.Cookie{Name: "staqly_admin", Value: "forged"}
	e.want(e.do("GET", "/v1/admin/workspaces", nil), 401)
}

func TestAdminCSRFRequired(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.signIn()
	good := e.csrf
	e.csrf = ""
	w := e.do("POST", "/v1/admin/promotion-codes", map[string]string{})
	e.want(w, 403)
	if !strings.Contains(w.Body.String(), "csrf_failed") {
		t.Fatal(w.Body)
	}
	e.csrf = "wrong"
	e.want(e.do("DELETE", "/v1/admin/workspaces/x", map[string]string{"confirm": "x"}), 403)
	e.csrf = good
	e.want(e.do("GET", "/v1/admin/overview", nil), 200)
}

func TestAdminCookieStrict(t *testing.T) {
	e := newEnv(t, "a@x.io")
	c := e.signIn().Result().Cookies()[0]
	if c.SameSite != http.SameSiteStrictMode || !c.HttpOnly || c.Path != "/" || c.Expires.IsZero() {
		t.Fatalf("cookie %+v", c)
	}
}

func TestAdminAuthRateLimited(t *testing.T) {
	e := newEnv(t, "a@x.io")
	var last int
	for i := 0; i < 25; i++ {
		last = e.do("POST", "/v1/admin/auth/code", map[string]string{"email": "nobody@x.io"}).Code
	}
	if last != 429 {
		t.Fatalf("last %d", last)
	}
}

func TestAdminNoStore(t *testing.T) {
	e := newEnv(t, "a@x.io")
	for _, p := range []string{"/v1/admin/me", "/v1/admin/nope"} {
		if cc := e.do("GET", p, nil).Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("%s: %q", p, cc)
		}
	}
}

func TestAdminUnknownRoute404Json(t *testing.T) {
	e := newEnv(t, "a@x.io")
	w := e.do("GET", "/v1/admin/nope", nil)
	e.want(w, 404)
	if w.Header().Get("Content-Type") != "application/json" || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatal(w.Body)
	}
	e.want(e.do("PUT", "/v1/admin/me", nil), 405)
}

func TestAdminDisabled404(t *testing.T) {
	e := newEnv(t)
	e.svc.Cfg.AdminEnabled = false
	e.want(e.do("POST", "/v1/admin/auth/code", map[string]string{"email": "a@x.io"}), 404)
	e.want(e.do("GET", "/v1/admin/me", nil), 404)
}

func TestAdminRejectsNonJSONBody(t *testing.T) {
	e := newEnv(t, "a@x.io")
	r := httptest.NewRequest("POST", "/v1/admin/auth/code", strings.NewReader("email=a@x.io"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	e.want(w, 415)
}

func TestAdminWorkspacesUsersAudit(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.signIn()
	ws := e.workspace()
	w := e.do("GET", "/v1/admin/workspaces?q=Acme", nil)
	e.want(w, 200)
	if !strings.Contains(w.Body.String(), ws) {
		t.Fatal(w.Body)
	}
	e.want(e.do("GET", "/v1/admin/workspaces/"+ws, nil), 200)
	e.want(e.do("GET", "/v1/admin/workspaces/nope", nil), 404)
	e.want(e.do("GET", "/v1/admin/workspaces?limit=abc", nil), 422)
	e.want(e.do("GET", "/v1/admin/users", nil), 200)
	e.want(e.do("GET", "/v1/admin/overview", nil), 200)

	e.want(e.do("POST", "/v1/admin/workspaces/"+ws+"/trial-review/clear", map[string]string{}), 204)
	w = e.do("GET", "/v1/admin/audit?workspace_id="+ws, nil)
	e.want(w, 200)
	if !strings.Contains(w.Body.String(), "clear_trial_review") {
		t.Fatal(w.Body)
	}
	w = e.do("POST", "/v1/admin/workspaces/"+ws+"/sessions/revoke", map[string]string{})
	e.want(w, 200)
	if !strings.Contains(w.Body.String(), `"revoked"`) {
		t.Fatal(w.Body)
	}
}

func TestAdminDeleteWorkspaceConfirmMismatch(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.signIn()
	ws := e.workspace()
	w := e.do("DELETE", "/v1/admin/workspaces/"+ws, map[string]string{"confirm": "wrong"})
	e.want(w, 400)
	if !strings.Contains(w.Body.String(), "confirm_mismatch") {
		t.Fatal(w.Body)
	}
	e.want(e.do("DELETE", "/v1/admin/workspaces/"+ws, map[string]string{"confirm": "Acme"}), 204)
	e.want(e.do("GET", "/v1/admin/workspaces/"+ws, nil), 404)
}

func TestAdminPromoCreateValidation(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.signIn()
	w := e.do("POST", "/v1/admin/promotion-codes", map[string]any{"code": "", "percent_off": 500})
	// Without Stripe configured the service refuses first (409 billing_disabled); never a 5xx.
	if w.Code != 422 && w.Code != 400 && w.Code != 409 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

// withStripe turns billing on against a fake Stripe that answers every call with body.
func (e *env) withStripe(body string) *[]string {
	e.t.Helper()
	calls := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(body))
	}))
	e.t.Cleanup(srv.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test", WebhookSecret: "whsec", APIBase: srv.URL}
	return calls
}

func TestPromoCreateValidation422(t *testing.T) {
	e := newEnv(t, "a@x.io")
	calls := e.withStripe(`{}`)
	e.signIn()
	w := e.do("POST", "/v1/admin/promotion-codes", map[string]any{"code": "SAVE10", "percent_off": 10, "amount_off": 500, "currency": "usd", "duration": "once"})
	e.want(w, 422)
	if !strings.Contains(w.Body.String(), `"validation_failed"`) || len(*calls) != 0 {
		t.Fatalf("%s calls %v", w.Body, *calls)
	}
}

func TestPaymentsHappyPath(t *testing.T) {
	e := newEnv(t, "a@x.io")
	calls := e.withStripe(`{"has_more":false,"data":[{"id":"in_1","number":"N-1","customer":"cus_x","status":"paid","amount_paid":900,"currency":"usd","created":1700000000}]}`)
	e.signIn()
	w := e.do("GET", "/v1/admin/payments?status=paid", nil)
	e.want(w, 200)
	var out struct {
		Items []struct {
			ID, Status string
			AmountPaid int64 `json:"amount_paid"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Items) != 1 || out.Items[0].ID != "in_1" || out.Items[0].AmountPaid != 900 || out.NextCursor != "" {
		t.Fatalf("%v %s", err, w.Body)
	}
	if len(*calls) != 1 || (*calls)[0] != "GET /v1/invoices" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("calls %v headers %v", *calls, w.Header())
	}
}

func TestCodeCooldownAnswersLikeAStranger(t *testing.T) {
	e := newEnv(t, "a@x.io")
	for range 3 {
		e.want(e.do("POST", "/v1/admin/auth/code", map[string]string{"email": "a@x.io"}), 204)
	}
}

func TestAdminManagementAndMethods(t *testing.T) {
	e := newEnv(t, "a@x.io")
	w := e.do("GET", "/v1/admin/auth/methods", nil)
	e.want(w, 200)
	if strings.TrimSpace(w.Body.String()) != `{"access_token":false,"email_code":true}` {
		t.Fatal(w.Body)
	}
	e.signIn()
	e.want(e.do("POST", "/v1/admin/admins", map[string]string{"email": "b@x.io", "name": "B"}), 201)
	if w := e.do("POST", "/v1/admin/admins", map[string]string{"email": "b@x.io"}); w.Code != 409 || !strings.Contains(w.Body.String(), "admin_exists") {
		t.Fatal(w.Body)
	}
	w = e.do("GET", "/v1/admin/admins", nil)
	e.want(w, 200)
	if !strings.Contains(w.Body.String(), `"items"`) || !strings.Contains(w.Body.String(), "b@x.io") {
		t.Fatal(w.Body)
	}
	if w := e.do("DELETE", "/v1/admin/admins/a%40x.io", nil); w.Code != 409 || !strings.Contains(w.Body.String(), "cannot_remove_self") {
		t.Fatal(w.Body)
	}
	e.want(e.do("DELETE", "/v1/admin/admins/b%40x.io", nil), 204)
	if w := e.do("DELETE", "/v1/admin/admins/a%40x.io", nil); w.Code != 409 {
		t.Fatal(w.Body)
	}
}

func TestAdminTokenSignIn(t *testing.T) {
	e := newEnv(t, "a@x.io")
	e.want(e.do("POST", "/v1/admin/auth/token", map[string]string{"token": "x"}), 404) // cloud mode
	e.svc.Cfg.Mode, e.svc.Cfg.LocalToken = app.ModeLocal, app.LocalTokenPrefix+"t"
	if w := e.do("GET", "/v1/admin/auth/methods", nil); strings.TrimSpace(w.Body.String()) != `{"access_token":true,"email_code":true}` {
		t.Fatal(w.Body)
	}
	e.want(e.do("POST", "/v1/admin/auth/token", map[string]string{"token": "bad"}), 401)
	w := e.do("POST", "/v1/admin/auth/token", map[string]string{"token": e.svc.Cfg.LocalToken})
	e.want(w, 200)
	if len(w.Result().Cookies()) != 1 || !strings.Contains(w.Body.String(), "csrf_token") {
		t.Fatal(w.Body)
	}
	for i := 0; i < 25; i++ { // same per-IP limit as the code routes
		w = e.do("POST", "/v1/admin/auth/token", map[string]string{"token": "bad"})
	}
	e.want(w, 429)
}
