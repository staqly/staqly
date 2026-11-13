package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The Graph API plumbing Instagram, Facebook Pages and Threads share, against a fake Graph host.

type fakeGraph struct {
	srv  *httptest.Server
	g    metaGraph
	reqs []*http.Request
	// answer answers one request (r.Form is parsed); nil answers {}.
	answer func(w http.ResponseWriter, r *http.Request)
}

func useFakeGraph(t *testing.T) *fakeGraph {
	t.Helper()
	f := &fakeGraph{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.reqs = append(f.reqs, r)
		w.Header().Set("Content-Type", "application/json")
		if f.answer == nil {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		f.answer(w, r)
	}))
	t.Cleanup(f.srv.Close)
	host := f.srv.URL
	f.g = metaGraph{name: "Facebook", host: &host, version: "v24.0"}
	return f
}

func TestMetaGetAndPost(t *testing.T) {
	f := useFakeGraph(t)
	f.answer = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"id":"42"}`) }
	ctx := context.Background()

	var out struct{ ID string }
	if err := f.g.get(ctx, "tok", "/me", url.Values{"fields": {"id,name"}}, &out); err != nil || out.ID != "42" {
		t.Fatalf("get %v %+v", err, out)
	}
	r := f.reqs[0]
	if r.Method != http.MethodGet || r.URL.Path != "/v24.0/me" || r.URL.Query().Get("access_token") != "tok" || r.URL.Query().Get("fields") != "id,name" {
		t.Fatalf("get request %s %s", r.Method, r.URL)
	}
	if err := f.g.get(ctx, "tok", "/me", nil, nil); err != nil || f.reqs[1].URL.Query().Get("access_token") != "tok" {
		t.Fatalf("get without a query: %v", err)
	}

	req, err := f.g.post(ctx, "tok", "/123/feed", url.Values{"message": {"hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.g.do(ctx, req, &out); err != nil {
		t.Fatal(err)
	}
	r = f.reqs[2]
	if r.Method != http.MethodPost || r.URL.Path != "/v24.0/123/feed" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
		r.PostForm.Get("access_token") != "tok" || r.PostForm.Get("message") != "hi" || r.URL.Query().Has("access_token") {
		t.Fatalf("post request %s %s %v", r.Method, r.URL, r.PostForm)
	}
}

func TestMetaMultipart(t *testing.T) {
	var fields map[string]string
	var fileName, fileType, fileBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		fields = map[string]string{}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FormName() == "source" {
				fileName, fileType, fileBody = p.FileName(), p.Header.Get("Content-Type"), string(b)
				continue
			}
			fields[p.FormName()] = string(b)
		}
		_, _ = io.WriteString(w, `{"id":"v1"}`)
	}))
	defer srv.Close()
	ctx := context.Background()
	file := MediaFile{MimeType: "video/mp4", Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("movie-bytes")), nil
	}}
	req, err := fbGraph.multipart(ctx, srv.URL, "tok", url.Values{"description": {"hi"}}, "source", file)
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ ID string }
	if err := fbGraph.do(ctx, req, &out); err != nil || out.ID != "v1" {
		t.Fatalf("do %v %+v", err, out)
	}
	if fields["access_token"] != "tok" || fields["description"] != "hi" || fileName != "file" || fileType != "video/mp4" || fileBody != "movie-bytes" {
		t.Fatalf("fields %v file %q %q %q", fields, fileName, fileType, fileBody)
	}

	// A file that can't be read ends the body with an error, so the request fails instead of sending half a form.
	broken := MediaFile{Name: "a.mp4", Open: func(context.Context) (io.ReadCloser, error) { return nil, errors.New("storage down") }}
	req, err = fbGraph.multipart(ctx, srv.URL, "tok", url.Values{}, "source", broken)
	if err != nil {
		t.Fatal(err)
	}
	if err := fbGraph.do(ctx, req, nil); err == nil {
		t.Fatal("a form without its file was sent")
	}
}

func TestMetaDo(t *testing.T) {
	f := useFakeGraph(t)
	ctx := context.Background()
	body := ""
	f.answer = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
	var out struct{ ID string }
	if err := f.g.get(ctx, "tok", "/x", nil, &out); err != nil {
		t.Fatalf("empty 2xx: %v", err)
	}
	body = `{"id":`
	if err := f.g.get(ctx, "tok", "/x", nil, &out); err == nil {
		t.Fatal("a malformed answer was taken")
	}
	f.srv.Close()
	err := f.g.get(ctx, "secret-token", "/x", nil, &out)
	var fl *Failure
	if err == nil || errors.As(err, &fl) || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "could not be reached") {
		t.Fatalf("unreachable: %v", err)
	}
}

// A publish whose answer was lost is final (Meta has no idempotency key); a platform's own refusal stays as it is.
func TestMetaLost(t *testing.T) {
	err := fbGraph.lost(errors.New("connection reset"))
	if errorClass(err) != "rejected" || !strings.Contains(err.Error(), "may have published") || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("lost: %v", err)
	}
	retry := &Failure{Msg: "rate limit"}
	if got := fbGraph.lost(retry); got != retry {
		t.Fatalf("a Failure was changed: %v", got)
	}
}

func TestMetaErrorClasses(t *testing.T) {
	ig := metaGraph{name: "Instagram"}
	graph := func(code, sub int, msg string) string {
		return fmt.Sprintf(`{"error":{"message":%q,"type":"OAuthException","code":%d,"error_subcode":%d}}`, msg, code, sub)
	}
	for _, c := range []struct {
		name   string
		status int
		body   string
		class  string
		says   string
	}{
		{"token endpoint: return address", 400, `{"error_type":"OAuthException","code":400,"error_message":"Error validating verification code. Please make sure your redirect_uri is identical"}`, "rejected", "return address"},
		{"token endpoint: wrong app", 400, `{"error_type":"OAuthException","code":400,"error_message":"Invalid platform app"}`, "rejected", "this server's app"},
		{"token endpoint: bad client", 400, `{"error_type":"OAuthException","code":101,"error_message":"Error validating client secret."}`, "rejected", "this server's app"},
		{"token endpoint: code used", 400, `{"error_type":"OAuthException","code":400,"error_message":"This authorization code has been used"}`, "rejected", "connect again"},
		{"personal account", 400, graph(10, 0, "The user is not an Instagram Business account"), "reauth", "professional accounts"},
		{"token expired", 400, graph(190, 463, "Error validating access token: Session has expired"), "reauth", "reconnect it"},
		{"session key invalid", 400, graph(102, 0, "Session key invalid"), "reauth", "reconnect it"},
		{"app rate limit", 400, graph(4, 0, "Application request limit reached"), "retry", "rate limit"},
		{"user rate limit", 400, graph(17, 0, "User request limit reached"), "retry", "rate limit"},
		{"page rate limit", 400, graph(32, 0, "Page request limit reached"), "retry", "rate limit"},
		{"call rate limit", 400, graph(613, 0, "Calls within one hour have exceeded"), "retry", "rate limit"},
		{"instagram rate limit", 400, graph(80002, 0, "too many calls"), "retry", "rate limit"},
		{"429 alone", 429, `slow down`, "retry", "rate limit"},
		{"media not ready", 400, graph(9007, 2207052, "Media download has failed"), "retry", "could not download"},
		{"media fetch timeout", 400, graph(9007, 2207003, "It took too long"), "retry", "could not download"},
		{"daily post limit", 400, graph(9, 2207042, "limit reached"), "rejected", "posts a day"},
		{"restricted account", 400, graph(25, 2207050, "restricted"), "rejected", "restricted"},
		{"post deleted", 400, graph(100, 33, "Object does not exist"), "gone", "not there any more"},
		{"other code 100", 400, graph(100, 0, "Invalid parameter"), "rejected", "Invalid parameter"},
		{"unknown error", 500, graph(1, 0, "An unknown error occurred"), "retry", "unavailable"},
		{"service error", 400, graph(2, 0, "Service temporarily unavailable"), "retry", "unavailable"},
		{"5xx with HTML", 502, `<html>Bad Gateway</html>`, "retry", "unavailable"},
		{"permission denied", 400, graph(10, 0, "Permission denied"), "rejected", "refused it"},
		{"permission missing", 400, graph(200, 0, "Requires pages_manage_posts"), "rejected", "refused it"},
		{"403 alone", 403, `{}`, "rejected", "refused it"},
		{"unsupported media", 400, graph(9004, 0, "Only photo or video can be accepted"), "rejected", "could not use the media"},
		{"media in the message", 400, graph(36003, 0, "The aspect ratio of the media is not supported"), "rejected", "could not use the media"},
		{"user message wins", 400, `{"error":{"message":"(#100) x","error_user_msg":"The caption is too long","code":100}}`, "rejected", "The caption is too long"},
		{"plain refusal", 400, graph(368, 0, "Blocked"), "rejected", "Instagram: Blocked"},
	} {
		err := ig.fail(c.status, []byte(c.body))
		if errorClass(err) != c.class || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %s %q, want %s %q", c.name, errorClass(err), err, c.class, c.says)
		}
	}
	long := strings.Repeat("y", 300)
	if e := parseMetaError(400, []byte(long)); e.detail != strings.Repeat("y", 200)+"..." {
		t.Errorf("a long body is not cut: %q", e.detail)
	}

	// A platform's own codes come first; nil from special falls back to the shared ones.
	special := metaGraph{name: "Threads", special: func(e metaErr) error {
		if e.code == 190 {
			return &Failure{Kind: ErrRejected, Msg: "special"}
		}
		return nil
	}}
	if err := special.fail(400, []byte(graph(190, 0, "x"))); err.Error() != "special" {
		t.Errorf("special: %v", err)
	}
	if err := special.fail(400, []byte(graph(4, 0, "x"))); errorClass(err) != "retry" {
		t.Errorf("special fallback: %v", err)
	}
}

func TestMetaUserToken(t *testing.T) {
	f := useFakeGraph(t)
	ctx := context.Background()
	app := App{ClientID: "app", ClientSecret: "sec"}
	short, long := `{"access_token":"short"}`, `{"access_token":"long","expires_in":5184000}`
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		if r.Form.Get("grant_type") == "fb_exchange_token" {
			_, _ = io.WriteString(w, long)
			return
		}
		_, _ = io.WriteString(w, short)
	}
	tok, err := metaUserToken(ctx, f.g, app, "https://app.example/cb", "code-1")
	if err != nil || tok.AccessToken != "long" || tok.ExpiresIn != 5184000 {
		t.Fatalf("token %+v %v", tok, err)
	}
	q1, q2 := f.reqs[0].URL.Query(), f.reqs[1].URL.Query()
	if f.reqs[0].URL.Path != "/v24.0/oauth/access_token" || q1.Get("client_id") != "app" || q1.Get("client_secret") != "sec" ||
		q1.Get("redirect_uri") != "https://app.example/cb" || q1.Get("code") != "code-1" {
		t.Fatalf("code step %s", f.reqs[0].URL)
	}
	if q2.Get("fb_exchange_token") != "short" || q2.Get("client_secret") != "sec" || q2.Get("client_id") != "app" {
		t.Fatalf("long step %s", f.reqs[1].URL)
	}

	for _, c := range []struct {
		name, short, long, says string
		class                   string
	}{
		{"no short token", `{}`, long, "returned no access token", "retry"},
		{"no long token", short, `{}`, "returned no access token", "retry"},
		{"code refused", `{"error":{"message":"Invalid verification code format.","code":100}}`, long, "trading the sign-in's code", "rejected"},
		{"exchange refused", short, `{"error":{"message":"expired","code":190}}`, "60-day token", "reauth"},
	} {
		short, long = c.short, c.long
		f.answer = func(w http.ResponseWriter, r *http.Request) {
			body := short
			if r.Form.Get("grant_type") == "fb_exchange_token" {
				body = long
			}
			if strings.Contains(body, `"error"`) {
				w.WriteHeader(400)
			}
			_, _ = io.WriteString(w, body)
		}
		_, err := metaUserToken(ctx, f.g, app, "r", "c")
		if err == nil || errorClass(err) != c.class || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %s %v", c.name, errorClass(err), err)
		}
	}
}

func TestMetaGranted(t *testing.T) {
	f := useFakeGraph(t)
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"permission":"pages_manage_posts","status":"granted"},{"permission":"pages_read_engagement","status":"declined"}]}`)
	}
	got, err := metaGranted(context.Background(), f.g, "tok")
	if err != nil || !got["pages_manage_posts"] || got["pages_read_engagement"] || len(got) != 1 {
		t.Fatalf("granted %v %v", got, err)
	}
	if f.reqs[0].URL.Path != "/v24.0/me/permissions" || f.reqs[0].URL.Query().Get("access_token") != "tok" {
		t.Fatalf("request %s", f.reqs[0].URL)
	}
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"expired","code":190}}`)
	}
	if _, err := metaGranted(context.Background(), f.g, "tok"); errorClass(err) != "reauth" || !strings.Contains(err.Error(), "reading the permissions") {
		t.Fatalf("refused: %v", err)
	}
}

func TestMetaPermissions(t *testing.T) {
	f := useFakeGraph(t)
	ctx := context.Background()
	app := App{ClientID: "app", ClientSecret: "sec"}
	debug := ""
	f.answer = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, debug) }
	for _, c := range []struct {
		name       string
		debug      string
		class      string // "" for no error
		noComments bool
	}{
		{"all there", `{"data":{"is_valid":true,"scopes":["post","comment"]}}`, "", false},
		{"invalid", `{"data":{"is_valid":false,"scopes":["post"]}}`, "reauth", false},
		{"no scopes listed", `{"data":{"is_valid":true}}`, "", true}, // c left as it was
		{"posting taken away", `{"data":{"is_valid":true,"scopes":["comment"]}}`, "reauth", true},
		{"comments taken away", `{"data":{"is_valid":true,"scopes":["post"]}}`, "", true},
		{"posting for other accounts only", `{"data":{"is_valid":true,"scopes":["post","comment"],"granular_scopes":[{"scope":"post","target_ids":["other"]}]}}`, "reauth", true},
		{"posting for this account", `{"data":{"is_valid":true,"scopes":["post","comment"],"granular_scopes":[{"scope":"post","target_ids":["other","page-1"]}]}}`, "", false},
		{"granular with no targets: every account", `{"data":{"is_valid":true,"scopes":["post","comment"],"granular_scopes":[{"scope":"post"}]}}`, "", false},
		{"comments for another account only", `{"data":{"is_valid":true,"scopes":["post","comment"],"granular_scopes":[{"scope":"comment","target_ids":["other"]}]}}`, "", true},
	} {
		debug = c.debug
		creds := Credentials{"no_comments": "true"}
		err := metaPermissions(ctx, f.g, app, "user-token", "page-1", "post", "comment", creds)
		if (c.class == "" && err != nil) || (c.class != "" && errorClass(err) != c.class) {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.class == "" && (creds["no_comments"] == "true") != c.noComments {
			t.Errorf("%s: no_comments %q", c.name, creds["no_comments"])
		}
	}
	r := f.reqs[0]
	if r.URL.Path != "/v24.0/debug_token" || r.URL.Query().Get("access_token") != "app|sec" || r.URL.Query().Get("input_token") != "user-token" {
		t.Fatalf("debug_token request %s", r.URL)
	}

	// Without the app's keys there is nothing to ask with.
	n := len(f.reqs)
	if err := metaPermissions(ctx, f.g, App{ClientID: "app"}, "t", "p", "post", "comment", Credentials{}); err != nil || len(f.reqs) != n {
		t.Fatalf("no secret: %v, %d requests", err, len(f.reqs)-n)
	}
	// A refused app token says nothing about this channel: a plain error, not reauth.
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"bad app","code":190}}`)
	}
	err := metaPermissions(ctx, f.g, app, "t", "p", "post", "comment", Credentials{})
	var fl *Failure
	if err == nil || errors.As(err, &fl) || !strings.Contains(err.Error(), "checking the permissions failed") {
		t.Fatalf("refused app token: %v", err)
	}
}

func TestMetaPages(t *testing.T) {
	f := useFakeGraph(t)
	page := func(id string) string {
		return `{"id":"` + id + `","name":"Page ` + id + `","access_token":"pt-` + id + `","picture":{"data":{"url":"https://pic/` + id + `"}}}`
	}
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v24.0/me/accounts":
			_, _ = io.WriteString(w, `{"data":[`+page("1")+`,`+page("2")+`]}`)
		case "/v24.0/me/businesses":
			_, _ = io.WriteString(w, `{"data":[{"id":"b1"},{"id":"b2"},{}]}`)
		case "/v24.0/b1/owned_pages":
			_, _ = io.WriteString(w, `{"data":[`+page("2")+`,`+page("3")+`,{"name":"no id"}]}`)
		case "/v24.0/b1/client_pages":
			_, _ = io.WriteString(w, `{"data":[`+page("4")+`]}`)
		default: // b2: a portfolio the person may not read
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"error":{"message":"no","code":10}}`)
		}
	}
	pages, err := metaPages(context.Background(), f.g, "tok", "id,name,access_token")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range pages {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "1,2,3,4" || pages[0].AccessToken != "pt-1" || pages[0].Picture.Data.URL != "https://pic/1" {
		t.Fatalf("pages %v %+v", ids, pages[0])
	}
	if q := f.reqs[0].URL.Query(); q.Get("fields") != "id,name,access_token" || q.Get("limit") != "100" {
		t.Fatalf("query %v", q)
	}

	// /me/accounts itself refused: the sign-in is the problem.
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"expired","code":190}}`)
	}
	if _, err := metaPages(context.Background(), f.g, "tok", "id"); errorClass(err) != "reauth" || !strings.Contains(err.Error(), "listing the Pages") {
		t.Fatalf("refused: %v", err)
	}
}

// Paging follows "next" on the Graph host only (it carries the token), and stops after 20 more pages.
func TestMetaEach(t *testing.T) {
	f := useFakeGraph(t)
	ctx := context.Background()
	next := func(r *http.Request, n int) string {
		return f.srv.URL + r.URL.Path + "?page=" + fmt.Sprint(n)
	}
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		n := 0
		_, _ = fmt.Sscan(r.URL.Query().Get("page"), &n)
		_, _ = fmt.Fprintf(w, `{"data":[%d],"paging":{"next":%q}}`, n, next(r, n+1))
	}
	var got int
	if err := metaEach(ctx, f.g, "tok", "/me/accounts", nil, func(json.RawMessage) { got++ }); err != nil {
		t.Fatal(err)
	}
	if got != 21 || len(f.reqs) != 21 {
		t.Fatalf("pages read %d, requests %d", got, len(f.reqs))
	}

	for _, off := range []string{"https://evil.example/v24.0/me?page=1", "http://127.0.0.1:1/v24.0/me?page=1", strings.Replace(f.srv.URL, "http://", "https://", 1) + "/x", "::bad"} {
		f.reqs = nil
		f.answer = func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"data":[1],"paging":{"next":%q}}`, off)
		}
		if err := metaEach(ctx, f.g, "tok", "/me/accounts", nil, func(json.RawMessage) {}); err != nil || len(f.reqs) != 1 {
			t.Fatalf("followed %s: %v, %d requests", off, err, len(f.reqs))
		}
	}

	// A later page refused ends with its error.
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "" {
			w.WriteHeader(500)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[1],"paging":{"next":%q}}`, next(r, 1))
	}
	if err := metaEach(ctx, f.g, "tok", "/me/accounts", nil, func(json.RawMessage) {}); errorClass(err) != "retry" || err == nil {
		t.Fatalf("second page: %v", err)
	}
}

func TestMetaWait(t *testing.T) {
	if err := metaWait(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := metaWait(ctx, time.Hour); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("cancelled wait: %v", err)
	}
}

func TestMetaCheckAppAnswers(t *testing.T) {
	f := useFakeGraph(t)
	ctx := context.Background()
	app := App{ClientID: "app", ClientSecret: "sec"}
	for _, c := range []struct {
		status int
		body   string
		ok     bool
		says   string
	}{
		{200, `{"access_token":"app|tok","token_type":"bearer"}`, true, ""},
		{200, `{}`, false, "answered 200"},
		{400, `{"error":{"message":"Error validating client secret.","code":1}}`, false, "App ID and App secret"},
		{400, `{"error":{"message":"Error validating application. Invalid application ID.","code":101}}`, false, "App ID and App secret"},
		{400, `{"error":{"message":"Invalid Client ID","code":100}}`, false, "App ID and App secret"},
		{500, `{"error":{"message":"oops","code":2}}`, false, "answered 500"},
	} {
		f.answer = func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}
		ok, err := metaCheckApp(ctx, f.g, f.g.url("/oauth/access_token"), app)
		if ok != c.ok || (c.ok && err != nil) || (!c.ok && (err == nil || !strings.Contains(err.Error(), c.says))) {
			t.Errorf("%d %s: %v %v", c.status, c.body, ok, err)
		}
	}
	q := f.reqs[0].URL.Query()
	if q.Get("client_id") != "app" || q.Get("client_secret") != "sec" || q.Get("grant_type") != "client_credentials" {
		t.Fatalf("check query %v", q)
	}
	f.srv.Close()
	if ok, err := metaCheckApp(ctx, f.g, f.g.url("/oauth/access_token"), app); ok || err == nil {
		t.Fatalf("unreachable: %v %v", ok, err)
	}
}
