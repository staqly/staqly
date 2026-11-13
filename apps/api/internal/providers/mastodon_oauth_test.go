package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Signing in to a Mastodon server: registering staqly's client there, checking it is still known, and the code
// exchange, against a fake server that answers each step as set.

type fakeMastodonOAuth struct {
	srv    *httptest.Server
	reqs   []*http.Request
	forms  []url.Values
	status map[string]int    // path -> status (200 when unset)
	body   map[string]string // path -> body
}

func useFakeMastodonOAuth(t *testing.T) *fakeMastodonOAuth {
	t.Helper()
	f := &fakeMastodonOAuth{status: map[string]int{}, body: map[string]string{
		"/api/v1/apps":                        `{"client_id":"cid","client_secret":"csec"}`,
		"/oauth/token":                        `{"access_token":"tok","token_type":"Bearer"}`,
		"/api/v1/accounts/verify_credentials": `{"id":"1","username":"jane","display_name":"","avatar":"https://pic"}`,
	}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.reqs = append(f.reqs, r)
		f.forms = append(f.forms, r.PostForm)
		if s := f.status[r.URL.Path]; s != 0 {
			w.WriteHeader(s)
		}
		_, _ = io.WriteString(w, f.body[r.URL.Path])
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestMastodonRegisterClient(t *testing.T) {
	f := useFakeMastodonOAuth(t)
	ctx := context.Background()
	client, err := Mastodon{}.RegisterClient(ctx, f.srv.URL, "https://app.example/cb")
	if err != nil || client["client_id"] != "cid" || client["client_secret"] != "csec" || len(client) != 2 {
		t.Fatalf("client %v %v", client, err)
	}
	r, form := f.reqs[0], f.forms[0]
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/apps" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("Accept") != "application/json" {
		t.Fatalf("request %s %s %v", r.Method, r.URL, r.Header)
	}
	for k, v := range map[string]string{"client_name": "staqly", "redirect_uris": "https://app.example/cb", "scopes": mastodonScopes, "website": "https://staqly.ai"} {
		if form.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, form.Get(k), v)
		}
	}

	for _, c := range []struct {
		name, body string
		status     int
		class      string
	}{
		{"no secret", `{"client_id":"cid"}`, 200, "rejected"},
		{"no id", `{"client_secret":"s"}`, 200, "rejected"},
		{"not Mastodon", `<html>hello</html>`, 200, "rejected"},
		{"refused", `{"error":"Validation failed: Redirect URI must be an absolute URI."}`, 422, "rejected"},
		{"down", `oops`, 503, "retry"},
		{"rate limit", `{"error":"Too many requests"}`, 429, "retry"},
	} {
		f.status["/api/v1/apps"], f.body["/api/v1/apps"] = c.status, c.body
		client, err := Mastodon{}.RegisterClient(ctx, f.srv.URL, "https://app.example/cb")
		if client != nil || errorClass(err) != c.class || err == nil {
			t.Errorf("%s: %v %s %v", c.name, client, errorClass(err), err)
		}
	}
}

// Only a refused client counts as gone: a server that is down or unreachable keeps it.
func TestMastodonClientWorks(t *testing.T) {
	f := useFakeMastodonOAuth(t)
	ctx := context.Background()
	client := map[string]string{"client_id": "cid", "client_secret": "csec"}
	for _, c := range []struct {
		status int
		body   string
		works  bool
	}{
		{200, `{"access_token":"app"}`, true},
		{401, `{"error":"invalid_client"}`, false},
		{400, `{"error":"invalid_client"}`, false},
		{403, `{"error":"unauthorized_client"}`, false},
		{503, `down`, true},
		{429, `{}`, true},
	} {
		f.status["/oauth/token"], f.body["/oauth/token"] = c.status, c.body
		if got := (Mastodon{}).ClientWorks(ctx, f.srv.URL, client); got != c.works {
			t.Errorf("%d %s: works=%v", c.status, c.body, got)
		}
	}
	form := f.forms[0]
	if form.Get("grant_type") != "client_credentials" || form.Get("client_id") != "cid" || form.Get("client_secret") != "csec" || form.Get("scope") != "read:accounts" {
		t.Fatalf("form %v", form)
	}
	f.srv.Close()
	if !(Mastodon{}).ClientWorks(ctx, f.srv.URL, client) {
		t.Fatal("an unreachable server lost its client")
	}
}

func TestMastodonAuthURLShape(t *testing.T) {
	m := Mastodon{}
	for _, extra := range []map[string]string{{}, {"instance": "https://m.example"}, {"client_id": "cid"}} {
		var ve *ValidationError
		if _, err := m.AuthURL(App{}, "r", "s", extra); !errors.As(err, &ve) || ve.Field != "instance" {
			t.Errorf("%v: %v", extra, err)
		}
	}
	raw, err := m.AuthURL(App{}, "https://app.example/cb", "st", map[string]string{"instance": "https://m.example", "client_id": "cid", "client_secret": "csec", "code_verifier": "v"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	sum := sha256.Sum256([]byte("v"))
	for k, v := range map[string]string{"response_type": "code", "client_id": "cid", "redirect_uri": "https://app.example/cb", "scope": mastodonScopes,
		"state": "st", "force_login": "true", "code_challenge": base64.RawURLEncoding.EncodeToString(sum[:]), "code_challenge_method": "S256"} {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://m.example/oauth/authorize" || strings.Contains(raw, "csec") {
		t.Errorf("auth url %s", raw)
	}
	raw, _ = m.AuthURL(App{}, "r", "s", map[string]string{"instance": "https://m.example", "client_id": "cid"})
	if strings.Contains(raw, "code_challenge") {
		t.Errorf("a challenge without a verifier: %s", raw)
	}
}

func TestMastodonExchangeSteps(t *testing.T) {
	f := useFakeMastodonOAuth(t)
	ctx := context.Background()
	extra := map[string]string{"instance": f.srv.URL, "client_id": "cid", "client_secret": "csec"}

	creds, prof, err := Mastodon{}.Exchange(ctx, App{}, "https://app.example/cb", "the-code", extra)
	if err != nil || creds["access_token"] != "tok" || creds["instance"] != f.srv.URL || prof.Name != "jane" || prof.AvatarURL != "https://pic" {
		t.Fatalf("exchange %v %+v %v", creds, prof, err)
	}
	form := f.forms[0]
	for k, v := range map[string]string{"grant_type": "authorization_code", "code": "the-code", "client_id": "cid", "client_secret": "csec",
		"redirect_uri": "https://app.example/cb", "scope": mastodonScopes} {
		if form.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, form.Get(k), v)
		}
	}
	if form.Has("code_verifier") {
		t.Errorf("a verifier it did not have: %v", form)
	}
	if got := f.reqs[1].Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("verify_credentials auth %q", got)
	}

	for _, bad := range []map[string]string{{"client_id": "cid"}, {"instance": f.srv.URL}} {
		var ve *ValidationError
		if _, _, err := (Mastodon{}).Exchange(ctx, App{}, "r", "c", bad); !errors.As(err, &ve) || ve.Field != "state" {
			t.Errorf("%v: %v", bad, err)
		}
	}

	for _, c := range []struct {
		name, body string
		status     int
		class      string
	}{
		{"no token", `{"token_type":"Bearer"}`, 200, "rejected"},
		{"code refused", `{"error":"invalid_grant"}`, 400, "rejected"},
		{"client gone", `{"error":"invalid_client"}`, 401, "reauth"},
		{"down", `oops`, 502, "retry"},
	} {
		f.status["/oauth/token"], f.body["/oauth/token"] = c.status, c.body
		creds, _, err := Mastodon{}.Exchange(ctx, App{}, "r", "c", extra)
		if creds != nil || err == nil || errorClass(err) != c.class {
			t.Errorf("%s: %v %s %v", c.name, creds, errorClass(err), err)
		}
	}
}
