package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMastodon answers like a Mastodon server for the calls the provider makes.
type fakeMastodon struct {
	mu         sync.Mutex
	statuses   []map[string]any
	keys       []string
	uploads    int
	polls      int
	statusCode int    // answer to POST /api/v1/statuses (0 = 200)
	statusBody string // its body then
	drop       bool   // close the connection on the first POST /api/v1/statuses
	byKey      map[string]string
	apps       []url.Values // POST /api/v1/apps
	tokens     []url.Values // POST /oauth/token
	srv        *httptest.Server
}

func (f *fakeMastodon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = r.ParseForm()
	switch r.URL.Path {
	case "/api/v1/apps": // registration, no token
		f.apps = append(f.apps, r.PostForm)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"csecret"}`))
		return
	case "/oauth/token":
		f.tokens = append(f.tokens, r.PostForm)
		if r.PostForm.Get("code") != "the-code" || r.PostForm.Get("client_secret") != "csecret" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
		return
	}
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"The access token is invalid"}`))
		return
	}
	switch {
	case r.URL.Path == "/api/v1/accounts/verify_credentials":
		_, _ = w.Write([]byte(`{"id":"1","username":"janedoe","display_name":"Jane Doe","avatar":"https://example.com/a.png","url":"https://example.com/@janedoe"}`))
	case r.URL.Path == "/api/v2/media":
		f.uploads++
		if err := r.ParseMultipartForm(1 << 20); err != nil || r.MultipartForm.File["file"] == nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		if strings.HasPrefix(r.MultipartForm.File["file"][0].Header.Get("Content-Type"), "video/") {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"m-video","url":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"m` + string(rune('0'+f.uploads)) + `","url":"https://example.com/m.png"}`))
	case r.URL.Path == "/api/v1/media/m-video":
		f.polls++
		if f.polls < 2 {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(`{"id":"m-video","url":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"m-video","url":"https://example.com/v.mp4"}`))
	case r.URL.Path == "/api/v1/statuses":
		raw, _ := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		f.keys = append(f.keys, key)
		if f.drop {
			f.drop = false
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			f.statuses = append(f.statuses, body)
			f.byKey[key] = "110"
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		if f.statusCode != 0 {
			w.WriteHeader(f.statusCode)
			_, _ = w.Write([]byte(f.statusBody))
			return
		}
		if id, ok := f.byKey[key]; ok { // Mastodon answers a repeated key with the status it made
			_, _ = w.Write([]byte(`{"id":"` + id + `","url":"https://example.com/@janedoe/` + id + `"}`))
			return
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.statuses = append(f.statuses, body)
		id := "11" + string(rune('0'+len(f.statuses)))
		f.byKey[key] = id
		_, _ = w.Write([]byte(`{"id":"` + id + `","url":"https://example.com/@janedoe/` + id + `"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeMastodon(t *testing.T) *fakeMastodon {
	t.Helper()
	f := &fakeMastodon{byKey: map[string]string{}}
	f.srv = httptest.NewServer(f)
	poll := mastodonPoll
	mastodonPoll = time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		mastodonPoll = poll
	})
	return f
}

func mastodonFile(name, mime string) MediaFile {
	return MediaFile{Name: name, MimeType: mime, Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("bytes")), nil
	}}
}

func TestMastodonConnect(t *testing.T) {
	f := useFakeMastodon(t)
	e, _ := Get("mastodon_token")
	if e.Status != StatusAvailable || e.Auth() != AuthToken {
		t.Fatalf("mastodon is %q %q", e.Status, e.Auth())
	}
	creds, prof, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL + "/", "access_token": "tok"})
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(f.srv.URL, "http://")
	if creds["instance"] != f.srv.URL || creds["access_token"] != "tok" || prof.Handle != "@janedoe@"+host || prof.Name != "Jane Doe" {
		t.Fatalf("creds %v profile %+v", creds, prof)
	}
	// The channel check connects again with the stored credentials.
	if _, _, err := e.Connect(context.Background(), map[string]string(creds)); err != nil {
		t.Fatal(err)
	}
	_, _, err = e.Connect(context.Background(), map[string]string{"instance": f.srv.URL, "access_token": "revoked"})
	if !errors.Is(err, ErrReauth) {
		t.Fatalf("a refused token: %v", err)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL}); err == nil {
		t.Fatal("no token connected")
	}
}

func TestMastodonPublish(t *testing.T) {
	f := useFakeMastodon(t)
	e, _ := Get("mastodon_token")
	creds := Credentials{"instance": f.srv.URL, "access_token": "tok"}
	post := PostContent{PostID: "p1", ChannelID: "c1", Text: "Hello", Media: []MediaFile{mastodonFile("a.png", "image/png"), mastodonFile("b.png", "image/png")}}
	res, err := e.Publish(context.Background(), creds, post, Settings{"visibility": "unlisted", "spoiler_text": "Spoilers"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "111" || !strings.HasSuffix(res.URL, "/111") {
		t.Fatalf("result %+v", res)
	}
	s := f.statuses[0]
	if s["status"] != "Hello" || s["visibility"] != "unlisted" || s["spoiler_text"] != "Spoilers" || s["sensitive"] != true || len(s["media_ids"].([]any)) != 2 {
		t.Fatalf("status %v", s)
	}
	if f.keys[0] == "" {
		t.Fatal("no Idempotency-Key")
	}

	// A video waits until the server has processed it.
	res, err = e.Publish(context.Background(), creds, PostContent{PostID: "p2", ChannelID: "c1", Text: "Clip", Media: []MediaFile{mastodonFile("v.mp4", "video/mp4")}}, nil)
	if err != nil || f.polls != 2 || f.statuses[1]["media_ids"].([]any)[0] != "m-video" {
		t.Fatalf("video: %v %+v polls %d", err, res, f.polls)
	}

	// Replies answer the post, each with its own key.
	r, err := Mastodon{}.Reply(context.Background(), creds, res.ExternalID, PostContent{PostID: "p2", ChannelID: "c1", Text: "More"}, nil)
	if err != nil || f.statuses[2]["in_reply_to_id"] != res.ExternalID || r.ExternalID == res.ExternalID {
		t.Fatalf("reply: %v %+v %v", err, r, f.statuses[2])
	}
}

func TestMastodonLostAnswerIsRetriedSafely(t *testing.T) {
	f := useFakeMastodon(t)
	e, _ := Get("mastodon_token")
	creds := Credentials{"instance": f.srv.URL, "access_token": "tok"}
	post := PostContent{PostID: "p1", ChannelID: "c1", Text: "Hello"}
	f.drop = true
	_, err := e.Publish(context.Background(), creds, post, nil)
	if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("a lost answer should be retried: %v", err)
	}
	res, err := e.Publish(context.Background(), creds, post, nil)
	if err != nil || res.ExternalID != "110" || len(f.statuses) != 1 || f.keys[0] != f.keys[1] {
		t.Fatalf("the retry posted again: %v %+v statuses %d keys %v", err, res, len(f.statuses), f.keys)
	}
}

func TestMastodonErrors(t *testing.T) {
	f := useFakeMastodon(t)
	e, _ := Get("mastodon_token")
	creds := Credentials{"instance": f.srv.URL, "access_token": "tok"}
	post := PostContent{PostID: "p1", ChannelID: "c1", Text: "Hello"}
	for _, c := range []struct {
		code int
		body string
		kind error
	}{
		{422, `{"error":"Validation failed: Text character limit of 500 exceeded"}`, ErrRejected},
		{403, `{"error":"This action is outside the authorized scopes"}`, ErrReauth},
		{429, `{"error":"Too many requests"}`, nil},
		{503, ``, nil},
	} {
		f.statusCode, f.statusBody = c.code, c.body
		_, err := e.Publish(context.Background(), creds, post, nil)
		var fail *Failure
		if !errors.As(err, &fail) || fail.Kind != c.kind {
			t.Fatalf("%d: %v", c.code, err)
		}
	}
	if _, err := e.Publish(context.Background(), Credentials{"instance": f.srv.URL, "access_token": "old"}, post, nil); !errors.Is(err, ErrReauth) {
		t.Fatalf("401: %v", err)
	}
}

func TestMastodonValidate(t *testing.T) {
	e, _ := Get("mastodon_token")
	img := MediaFile{Name: "a.png", MimeType: "image/png"}
	vid := MediaFile{Name: "v.mp4", MimeType: "video/mp4"}
	for _, c := range []struct {
		p  PostContent
		s  Settings
		ok bool
	}{
		{PostContent{Text: "hi"}, nil, true},
		{PostContent{Text: strings.Repeat("a", 501)}, nil, false},
		{PostContent{Text: "hi", Media: []MediaFile{img, img, img, img}}, nil, true},
		{PostContent{Text: "hi", Media: []MediaFile{img, img, img, img, img}}, nil, false},
		{PostContent{Text: "hi", Media: []MediaFile{vid}}, nil, true},
		{PostContent{Text: "hi", Media: []MediaFile{vid, img}}, nil, false},
		{PostContent{Text: "hi"}, Settings{"visibility": "direct"}, false},
		{PostContent{Text: "hi"}, Settings{"visibility": "private", "spoiler_text": "cw"}, true},
	} {
		if err := e.Validate(c.p, c.s); (err == nil) != c.ok {
			t.Errorf("%d files %v: %v", len(c.p.Media), c.s, err)
		}
	}
}

// Sign in with Mastodon: staqly registers itself on the person's server, sends the browser to its authorize page
// with PKCE, and trades the code for a token with the registered app.
func TestMastodonOAuth(t *testing.T) {
	f := useFakeMastodon(t)
	e, _ := Get("mastodon")
	if e.Status != StatusAvailable || e.Auth() != AuthOAuth2 || len(e.CredentialFields()) != 1 {
		t.Fatalf("mastodon is %q %q, fields %v", e.Status, e.Auth(), e.CredentialFields())
	}
	ctx := context.Background()
	redirect := "https://staqly.example.com/channels/callback/mastodon"
	carry, err := e.Provider.(OAuthPreparer).PrepareOAuth(ctx, redirect, map[string]string{"instance": f.srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.apps) != 1 || f.apps[0].Get("redirect_uris") != redirect || f.apps[0].Get("scopes") != mastodonScopes {
		t.Fatalf("registered %v", f.apps)
	}
	if carry["instance"] != f.srv.URL || carry["client_id"] != "cid" || carry["client_secret"] != "csecret" {
		t.Fatalf("carry %v", carry)
	}
	extra := map[string]string{"code_verifier": "verifier"}
	for k, v := range carry {
		extra[k] = v
	}
	u, err := e.AuthURL(App{}, redirect, "st", extra)
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(u)
	if !strings.HasPrefix(u, f.srv.URL+"/oauth/authorize?") || pu.Query().Get("client_id") != "cid" ||
		pu.Query().Get("state") != "st" || pu.Query().Get("code_challenge_method") != "S256" || strings.Contains(u, "csecret") {
		t.Fatalf("auth URL %s", u)
	}
	creds, prof, err := e.Exchange(ctx, App{}, redirect, "the-code", extra)
	if err != nil {
		t.Fatal(err)
	}
	if creds["instance"] != f.srv.URL || creds["access_token"] != "tok" || prof.Name != "Jane Doe" ||
		f.tokens[0].Get("code_verifier") != "verifier" || f.tokens[0].Get("redirect_uri") != redirect {
		t.Fatalf("creds %v profile %+v token request %v", creds, prof, f.tokens)
	}
	if _, _, err := e.Exchange(ctx, App{}, redirect, "stolen", extra); err == nil {
		t.Fatal("a wrong code connected")
	}
	if _, err := e.Provider.(OAuthPreparer).PrepareOAuth(ctx, redirect, map[string]string{"instance": "not a server"}); err == nil {
		t.Fatal("a made-up server was registered on")
	}
}
