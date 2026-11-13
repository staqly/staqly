package adminapi_test

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// adminPublic: the panel's routes that answer without an admin session (the sign-in itself). Everything else the
// router serves needs one; a new route outside the session group fails TestEveryAdminRouteNeedsAnAdmin.
var adminPublic = []string{
	"POST /v1/admin/auth/code", "POST /v1/admin/auth/verify", "POST /v1/admin/auth/token", "GET /v1/admin/auth/methods",
}

// Every route of the admin API: nobody, a forged cookie and a workspace's own admin key get 401; an admin's
// unsafe request without (or with a wrong) CSRF token gets 403 and changes nothing.
func TestEveryAdminRouteNeedsAnAdmin(t *testing.T) {
	e := newEnv(t, "a@x.io")
	ws := e.workspace()
	res, err := e.svc.Setup(t.Context(), "Other", "tok", "ua")
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	_ = chi.Walk(e.h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	if len(routes) < 30 {
		t.Fatalf("only %d routes", len(routes))
	}
	param := regexp.MustCompile(`\{[^}]+\}`)
	send := func(route string, hdr ...string) (int, string) {
		method, path, _ := strings.Cut(route, " ")
		w := e.do(method, param.ReplaceAllString(path, ws), nil, hdr...)
		return w.Code, w.Body.String()
	}
	for _, route := range routes {
		if slices.Contains(adminPublic, route) {
			continue
		}
		e.ck = nil
		if code, body := send(route); code != 401 {
			t.Errorf("%s with no session: %d %s", route, code, body)
		}
		if code, body := send(route, "Authorization", "Bearer "+res.APIKey.Key); code != 401 {
			t.Errorf("%s with a workspace's admin key: %d %s", route, code, body)
		}
		e.ck = &http.Cookie{Name: "staqly_admin", Value: "forged"}
		if code, body := send(route); code != 401 {
			t.Errorf("%s with a forged cookie: %d %s", route, code, body)
		}
	}
	e.ck = nil
	e.signIn()
	good := e.csrf
	for _, route := range routes {
		if slices.Contains(adminPublic, route) || strings.HasPrefix(route, "GET ") {
			continue
		}
		for _, token := range []string{"", "wrong"} {
			e.csrf = token
			if code, body := send(route); code != 403 || !strings.Contains(body, "csrf_failed") {
				t.Errorf("%s with X-CSRF-Token %q: %d %s", route, token, code, body)
			}
		}
	}
	// Nothing ran: the workspace is there and the admin is still signed in.
	e.csrf = good
	e.want(e.do("GET", "/v1/admin/workspaces/"+ws, nil), 200)
}
