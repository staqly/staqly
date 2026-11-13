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
	"strconv"
	"strings"
	"testing"
	"time"
)

// Google's shared sign-in (YouTube and Google Business Profile) against a fake of accounts.google.com, the token
// endpoint and the userinfo endpoint.

type fakeGoogleAuth struct {
	srv   *httptest.Server
	forms []url.Values // every form the token endpoint got
	auth  []string     // the Authorization header of every userinfo call
	token func(w http.ResponseWriter, f url.Values)
	me    string
}

func useGoogleAuth(t *testing.T) *fakeGoogleAuth {
	t.Helper()
	f := &fakeGoogleAuth{me: `{"sub":"g-1","name":"Mia","email":"mia@example.com","picture":"https://pic/1"}`}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				w.WriteHeader(400)
				return
			}
			_ = r.ParseForm()
			f.forms = append(f.forms, r.PostForm)
			f.token(w, r.PostForm)
		case "/userinfo":
			f.auth = append(f.auth, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, f.me)
		default:
			w.WriteHeader(404)
		}
	}))
	old := []string{googleAuthorize, googleTokenURL, googleUserinfo}
	googleAuthorize, googleTokenURL, googleUserinfo = f.srv.URL+"/auth", f.srv.URL+"/token", f.srv.URL+"/userinfo"
	t.Cleanup(func() {
		googleAuthorize, googleTokenURL, googleUserinfo = old[0], old[1], old[2]
		f.srv.Close()
	})
	return f
}

var googleApp = App{ClientID: "cid", ClientSecret: "csecret"}

func TestGoogleAuthURL(t *testing.T) {
	raw := googleAuthURL(googleApp, "https://app.example/cb", "st", "openid https://www.googleapis.com/auth/youtube", map[string]string{"code_verifier": "verifier"})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	sum := sha256.Sum256([]byte("verifier"))
	want := map[string]string{
		"response_type": "code", "client_id": "cid", "redirect_uri": "https://app.example/cb", "state": "st",
		"scope": "openid https://www.googleapis.com/auth/youtube", "access_type": "offline", "prompt": "consent select_account",
		"code_challenge": base64.RawURLEncoding.EncodeToString(sum[:]), "code_challenge_method": "S256",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if !strings.HasPrefix(raw, googleAuthorize+"?") {
		t.Errorf("auth url %s", raw)
	}
	q = mustQuery(t, googleAuthURL(googleApp, "r", "s", "x", nil))
	if q.Has("code_challenge") || q.Has("code_challenge_method") {
		t.Errorf("no verifier, yet a challenge: %v", q)
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestGoogleExchange(t *testing.T) {
	f := useGoogleAuth(t)
	f.token = func(w http.ResponseWriter, _ url.Values) {
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600,"scope":"openid https://scope/a email"}`)
	}
	before := time.Now()
	creds, prof, err := googleExchange(context.Background(), "YouTube", googleApp, "https://app.example/cb", "the-code",
		map[string]string{"code_verifier": "v"}, "https://scope/a", "allow YouTube")
	if err != nil {
		t.Fatal(err)
	}
	form := f.forms[0]
	for k, v := range map[string]string{"grant_type": "authorization_code", "code": "the-code", "redirect_uri": "https://app.example/cb",
		"code_verifier": "v", "client_id": "cid", "client_secret": "csecret"} {
		if form.Get(k) != v {
			t.Errorf("token form %s = %q, want %q", k, form.Get(k), v)
		}
	}
	if f.auth[0] != "Bearer at" {
		t.Errorf("userinfo auth %q", f.auth[0])
	}
	if creds["access_token"] != "at" || creds["refresh_token"] != "rt" || creds["google_id"] != "g-1" || creds["scope"] != "openid https://scope/a email" {
		t.Errorf("creds %v", creds)
	}
	exp, err := time.Parse(time.RFC3339, creds["expires_at"])
	if err != nil || exp.Before(before.Add(59*time.Minute)) || exp.After(time.Now().Add(61*time.Minute)) {
		t.Errorf("expires_at %q", creds["expires_at"])
	}
	if prof.Handle != "mia@example.com" || prof.Name != "Mia" || prof.AvatarURL != "https://pic/1" {
		t.Errorf("profile %+v", prof)
	}

	// No verifier: none is sent.
	f.forms = nil
	if _, _, err := googleExchange(context.Background(), "YouTube", googleApp, "r", "c", nil, "https://scope/a", "m"); err != nil {
		t.Fatal(err)
	}
	if f.forms[0].Has("code_verifier") {
		t.Errorf("sent a verifier it did not have: %v", f.forms[0])
	}
}

// Google lets people untick a permission on the consent screen: a granted list without the scope is refused, a
// scope only a prefix of a granted one does not count, and an answer with no scope list is taken as is.
func TestGoogleExchangeScope(t *testing.T) {
	f := useGoogleAuth(t)
	for _, c := range []struct {
		granted string
		ok      bool
	}{
		{"openid email", false},
		{"https://scope/a.readonly", false},
		{"https://scope/a", true},
		{"", true},
	} {
		f.token = func(w http.ResponseWriter, _ url.Values) {
			_, _ = io.WriteString(w, `{"access_token":"at","scope":"`+c.granted+`"}`)
		}
		_, _, err := googleExchange(context.Background(), "YouTube", googleApp, "r", "c", nil, "https://scope/a", "allow managing videos")
		if c.ok != (err == nil) {
			t.Errorf("granted %q: %v", c.granted, err)
		}
		if !c.ok && (errorClass(err) != "rejected" || err.Error() != "allow managing videos") {
			t.Errorf("granted %q: %s %v", c.granted, errorClass(err), err)
		}
	}
}

func TestGoogleExchangeFailures(t *testing.T) {
	f := useGoogleAuth(t)
	for _, c := range []struct {
		name   string
		status int
		body   string
		me     string
		class  string
		says   string
	}{
		{"no access token", 200, `{"refresh_token":"rt"}`, "", "retry", "returned no access token"},
		{"code used twice", 400, `{"error":"invalid_grant","error_description":"Bad Request"}`, "", "reauth", "reconnect"},
		{"wrong keys", 401, `{"error":"invalid_client","error_description":"The OAuth client was not found."}`, "", "rejected", "this server's app"},
		{"return address", 400, `{"error":"redirect_uri_mismatch"}`, "", "rejected", "return address"},
		{"malformed token answer", 200, `{"access_token":`, "", "retry", ""},
		{"malformed userinfo", 200, `{"access_token":"at"}`, `{"sub":`, "retry", ""},
	} {
		f.token = func(w http.ResponseWriter, _ url.Values) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}
		f.me = c.me
		if f.me == "" {
			f.me = `{"sub":"g"}`
		}
		_, _, err := googleExchange(context.Background(), "YouTube", googleApp, "r", "c", nil, "s", "m")
		if err == nil || errorClass(err) != c.class || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %s %v, want %s %q", c.name, errorClass(err), err, c.class, c.says)
		}
	}
}

func TestGoogleRefresh(t *testing.T) {
	f := useGoogleAuth(t)
	ctx := context.Background()
	old := Credentials{"access_token": "old", "refresh_token": "rt-1", "google_id": "g-1", "scope": "s"}

	if _, err := googleRefresh(ctx, "YouTube", googleApp, Credentials{"access_token": "a"}); errorClass(err) != "reauth" {
		t.Fatalf("no refresh token: %v", err)
	}
	if len(f.forms) != 0 {
		t.Fatal("asked Google without a refresh token")
	}

	// Google usually answers a refresh without a refresh token: the old one stays, the rest is kept.
	f.token = func(w http.ResponseWriter, _ url.Values) {
		_, _ = io.WriteString(w, `{"access_token":"new","expires_in":3599}`)
	}
	got, err := googleRefresh(ctx, "YouTube", googleApp, old)
	if err != nil || got["access_token"] != "new" || got["refresh_token"] != "rt-1" || got["google_id"] != "g-1" || got["scope"] != "s" || got["expires_at"] == "" {
		t.Fatalf("refresh %v %v", got, err)
	}
	if form := f.forms[0]; form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "rt-1" || form.Get("client_secret") != "csecret" {
		t.Fatalf("refresh form %v", form)
	}
	if old["access_token"] != "old" {
		t.Fatal("refresh changed the credentials it was given")
	}

	// A rotated refresh token replaces the old one.
	f.token = func(w http.ResponseWriter, _ url.Values) {
		_, _ = io.WriteString(w, `{"access_token":"new","refresh_token":"rt-2"}`)
	}
	if got, _ := googleRefresh(ctx, "YouTube", googleApp, old); got["refresh_token"] != "rt-2" || got["expires_at"] != "" {
		t.Fatalf("rotation %v", got)
	}

	for _, c := range []struct {
		status int
		body   string
		class  string
		says   string
	}{
		{400, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`, "reauth", "expired or revoked"},
		{400, `{"error":"invalid_grant","error_description":"reauth related error (invalid_rapt)","error_subtype":"invalid_rapt"}`, "reauth", "Workspace"},
		{503, `oops`, "retry", "unavailable (503)"},
		{429, `{}`, "retry", "rate limit"},
	} {
		f.token = func(w http.ResponseWriter, _ url.Values) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}
		_, err := googleRefresh(ctx, "YouTube", googleApp, old)
		if errorClass(err) != c.class || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%d %s: %s %v", c.status, c.body, errorClass(err), err)
		}
	}
}

func TestGoogleCheckAppAnswers(t *testing.T) {
	f := useGoogleAuth(t)
	for _, c := range []struct {
		status int
		body   string
		ok     bool
		says   string
	}{
		{400, `{"error":"invalid_grant","error_description":"Malformed auth code."}`, true, ""},
		{401, `{"error":"invalid_client"}`, false, "Client ID and Client secret"},
		{401, `{"error":"unauthorized_client"}`, false, "Client ID and Client secret"},
		{400, `{"error":"redirect_uri_mismatch"}`, false, "https://app.example/cb"},
		{500, `down`, false, "answered 500: down"},
	} {
		f.token = func(w http.ResponseWriter, _ url.Values) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}
		ok, err := googleCheckApp(context.Background(), "YouTube", googleApp, "https://app.example/cb")
		if ok != c.ok || (c.says != "" && (err == nil || !strings.Contains(err.Error(), c.says))) || (c.ok && err != nil) {
			t.Errorf("%s: %v %v", c.body, ok, err)
		}
	}
	form := f.forms[0]
	if form.Get("code") != checkCode || form.Get("client_id") != "cid" || form.Get("client_secret") != "csecret" || form.Get("redirect_uri") != "https://app.example/cb" {
		t.Errorf("check form %v", form)
	}
	f.srv.Close()
	if ok, err := googleCheckApp(context.Background(), "YouTube", googleApp, "r"); ok || err == nil || !strings.Contains(err.Error(), "could not be reached") {
		t.Errorf("unreachable: %v %v", ok, err)
	}
}

func TestGoogleRequestAndDo(t *testing.T) {
	var got *http.Request
	var body string
	status, answer := 200, `{"id":"v1"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	ctx := context.Background()

	req, err := googleRequest(ctx, "tok", http.MethodPost, srv.URL+"/x", map[string]string{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ ID string }
	h, err := googleDo(ctx, "YouTube", req, &out)
	if err != nil || out.ID != "v1" || h == nil {
		t.Fatalf("do %v %+v", err, out)
	}
	if got.Header.Get("Authorization") != "Bearer tok" || got.Header.Get("Content-Type") != "application/json; charset=UTF-8" || body != `{"a":"b"}` {
		t.Fatalf("request %v %q", got.Header, body)
	}

	// A GET has no body and no Content-Type.
	out.ID = ""
	if err := googleGet(ctx, "YouTube", "tok", srv.URL+"/y", &out); err != nil || out.ID != "v1" || got.Method != http.MethodGet || got.Header.Get("Content-Type") != "" || body != "" {
		t.Fatalf("get %v %v %q", err, got.Header, body)
	}

	// An empty 2xx answer is fine; a malformed one is an error.
	answer = ""
	if _, err := googleDo(ctx, "YouTube", mustGoogleReq(t, srv.URL), &out); err != nil {
		t.Fatalf("empty 204: %v", err)
	}
	answer = `{"id":`
	if _, err := googleDo(ctx, "YouTube", mustGoogleReq(t, srv.URL), &out); err == nil {
		t.Fatal("a malformed answer was taken")
	}
	status, answer = 404, `{"error":{"code":404,"message":"Video not found","status":"NOT_FOUND"}}`
	if _, err := googleDo(ctx, "YouTube", mustGoogleReq(t, srv.URL), &out); errorClass(err) != "rejected" || !strings.Contains(err.Error(), "Video not found") {
		t.Fatalf("404: %v", err)
	}

	// A lost answer is a plain error naming the host only (an upload session's URL carries its id).
	srv.Close()
	_, err = googleDo(ctx, "YouTube", mustGoogleReq(t, srv.URL+"/upload?upload_id=secret"), nil)
	var f *Failure
	if err == nil || errors.As(err, &f) || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "could not be reached") {
		t.Fatalf("unreachable: %v", err)
	}

	// A local capacity guard's Failure (with its retry time) passes through as is.
	retry := time.Now().Add(time.Hour)
	guard := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &Failure{Msg: "busy", RetryAt: retry}
	})}
	_, err = googleDo(WithHTTPClient(ctx, guard), "YouTube", mustGoogleReq(t, "http://example.invalid"), nil)
	if !RetryAt(err).Equal(retry) {
		t.Fatalf("guard: %v", err)
	}

	if _, err := googleRequest(ctx, "t", http.MethodPost, "http://x", func() {}); err == nil {
		t.Fatal("a body JSON can't encode was sent")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustGoogleReq(t *testing.T, endpoint string) *http.Request {
	t.Helper()
	req, err := googleRequest(context.Background(), "tok", http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// Every answer Google documents for its token endpoint and its APIs, in the class staqly acts on.
func TestGoogleErrorClasses(t *testing.T) {
	apiErr := func(code int, status, reason, msg string) string {
		return `{"error":{"code":` + strconv.Itoa(code) + `,"message":"` + msg + `","status":"` + status + `","errors":[{"reason":"` + reason + `"}]}}`
	}
	for _, c := range []struct {
		name   string
		status int
		body   string
		class  string
		says   string
	}{
		{"token: wrong keys", 401, `{"error":"invalid_client","error_description":"Unauthorized"}`, "rejected", "this server's app (Unauthorized)"},
		{"token: client not allowed", 400, `{"error":"unauthorized_client"}`, "rejected", "this server's app (unauthorized_client)"},
		{"token: return address", 400, `{"error":"redirect_uri_mismatch","error_description":"Bad redirect"}`, "rejected", "return address"},
		{"token: revoked", 400, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`, "reauth", "reconnect it"},
		{"token: workspace session policy", 400, `{"error":"invalid_grant","error_subtype":"invalid_rapt"}`, "reauth", "session policy"},
		{"token: rapt in the description", 400, `{"error":"invalid_grant","error_description":"reauth related error (invalid_rapt)"}`, "reauth", "session policy"},
		{"token: other", 400, `{"error":"invalid_request","error_description":"Missing code"}`, "rejected", "Missing code"},
		{"api: no YouTube channel", 401, apiErr(401, "UNAUTHENTICATED", "youtubeSignupRequired", "Unauthorized"), "rejected", "no YouTube channel"},
		{"api: sign-in expired", 401, apiErr(401, "UNAUTHENTICATED", "authError", "Invalid Credentials"), "reauth", "Invalid Credentials"},
		{"api: 401 without a body", 401, ``, "reauth", "reconnect"},
		{"api: scope missing", 403, apiErr(403, "PERMISSION_DENIED", "insufficientPermissions", "Insufficient Permission"), "reauth", "allow everything"},
		{"api: scope missing (v1)", 403, `{"error":{"code":403,"message":"m","status":"PERMISSION_DENIED","details":[{"reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`, "reauth", "allow everything"},
		{"api: API off", 403, apiErr(403, "PERMISSION_DENIED", "accessNotConfigured", "API not used"), "rejected", "not turned on"},
		{"api: API off (v1)", 403, `{"error":{"code":403,"message":"m","details":[{"reason":"SERVICE_DISABLED"}]}}`, "rejected", "not turned on"},
		{"api: daily quota", 403, apiErr(403, "", "quotaExceeded", "quota"), "rejected", "daily limit"},
		{"api: upload limit", 400, apiErr(400, "", "uploadLimitExceeded", "too many"), "rejected", "daily limit"},
		{"api: rate limit", 403, apiErr(403, "", "rateLimitExceeded", "slow"), "retry", "rate limit"},
		{"api: user rate limit", 403, apiErr(403, "", "userRateLimitExceeded", "slow"), "retry", "rate limit"},
		{"api: resource exhausted", 429, apiErr(429, "RESOURCE_EXHAUSTED", "", "slow"), "retry", "rate limit"},
		{"api: 429 without a body", 429, ``, "retry", "rate limit"},
		{"api: down", 503, `<html>Service Unavailable</html>`, "retry", "unavailable (503)"},
		{"api: forbidden", 403, apiErr(403, "PERMISSION_DENIED", "forbidden", "Not yours"), "rejected", "refused it (Not yours)"},
		{"api: not found", 404, apiErr(404, "NOT_FOUND", "notFound", "No video"), "rejected", "could not find it (No video)"},
		{"api: bad request", 400, apiErr(400, "INVALID_ARGUMENT", "invalidTitle", "Bad title"), "rejected", "Bad title"},
		{"api: a 3xx is not taken as success", 302, ``, "retry", "answered 302"},
	} {
		err := googleError("YouTube", c.status, []byte(c.body))
		if errorClass(err) != c.class || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %s %q, want %s %q", c.name, errorClass(err), err, c.class, c.says)
		}
	}
	long := strings.Repeat("x", 300)
	if err := googleError("YouTube", 400, []byte(long)); !strings.Contains(err.Error(), strings.Repeat("x", 200)+"...") || strings.Contains(err.Error(), strings.Repeat("x", 201)) {
		t.Errorf("a long body is not cut: %d", len(err.Error()))
	}
}

func TestGoogleCopy(t *testing.T) {
	c := Credentials{"a": "1"}
	got := googleCopy(c, "b", "2")
	if got["a"] != "1" || got["b"] != "2" || len(c) != 1 {
		t.Fatalf("copy %v, original %v", got, c)
	}
}
