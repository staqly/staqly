package providers

import (
	"context"
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

// fakeThreads answers like graph.threads.net for the calls the provider makes.
type fakeThreads struct {
	mu          sync.Mutex
	calls       []string
	tokenForm   url.Values
	containers  []url.Values // forms of POST /{user}/threads, in order
	published   []url.Values // forms of threads_publish
	polls       int
	statusFor   map[string]string // container id -> status (default: IN_PROGRESS once, then FINISHED)
	publishCode int
	publishBody string
	dropPublish bool
	srv         *httptest.Server
}

func (f *fakeThreads) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(raw))
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	token := q.Get("access_token")
	if token == "" {
		token = form.Get("access_token")
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1.0")
	switch {
	case r.URL.Path == "/oauth/access_token" && q.Get("grant_type") == "client_credentials":
		if q.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Error validating client secret.","type":"OAuthException","code":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"TH|app","token_type":"bearer"}`))
	case r.URL.Path == "/oauth/access_token":
		f.tokenForm = form
		if form.Get("code") != "code1" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid authorization code","type":"OAuthException","code":100}}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"short","user_id":1784}`))
	case r.URL.Path == "/access_token":
		_, _ = w.Write([]byte(`{"access_token":"long","token_type":"bearer","expires_in":5183944}`))
	case r.URL.Path == "/refresh_access_token":
		_, _ = w.Write([]byte(`{"access_token":"renewed","token_type":"bearer","expires_in":5183944}`))
	case token == "revoked":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Error validating access token: Session has expired","type":"OAuthException","code":190}}`))
	case path == "/me":
		_, _ = w.Write([]byte(`{"id":"1784","username":"janedoe","name":"Jane Doe","threads_profile_picture_url":"https://cdn.example/jane.jpg"}`))
	case path == "/1784/threads":
		f.containers = append(f.containers, form)
		_, _ = w.Write([]byte(`{"id":"c` + string(rune('0'+len(f.containers))) + `"}`))
	case path == "/1784/threads_publish":
		f.published = append(f.published, form)
		if f.dropPublish {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		if f.publishCode != 0 {
			w.WriteHeader(f.publishCode)
			_, _ = w.Write([]byte(f.publishBody))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1790` + string(rune('0'+len(f.published))) + `"}`))
	case q.Get("fields") == "status,error_message":
		f.polls++
		st := f.statusFor[strings.TrimPrefix(path, "/")]
		if st == "" {
			st = "FINISHED"
			if f.polls == 1 {
				st = "IN_PROGRESS"
			}
		}
		_, _ = w.Write([]byte(`{"status":"` + st + `","error_message":"FILE_FORMAT"}`))
	case q.Get("fields") == "permalink":
		_, _ = w.Write([]byte(`{"permalink":"https://www.threads.com/@janedoe/post/DAbc"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeThreads) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeThreads) seen() (containers, published []url.Values, polls int, calls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.containers, f.published, f.polls, f.calls
}

func useFakeThreads(t *testing.T) *fakeThreads {
	t.Helper()
	f := &fakeThreads{statusFor: map[string]string{}}
	f.srv = httptest.NewServer(f)
	auth, tok, graph, poll := threadsAuthorize, threadsToken, threadsGraph, threadsPoll
	threadsAuthorize, threadsToken, threadsGraph, threadsPoll = f.srv.URL+"/oauth/authorize", f.srv.URL+"/oauth/access_token", f.srv.URL, time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		threadsAuthorize, threadsToken, threadsGraph, threadsPoll = auth, tok, graph, poll
	})
	return f
}

func threadsCreds(expires time.Duration) Credentials {
	return Credentials{"access_token": "long", "user_id": "1784", "expires_at": time.Now().Add(expires).UTC().Format(time.RFC3339)}
}

func TestThreadsConnect(t *testing.T) {
	f := useFakeThreads(t)
	th, _ := Get("threads")
	if th.Status != StatusAvailable || th.Name() != "Threads" {
		t.Fatalf("threads is %q %q", th.Status, th.Name())
	}
	if n, ok := th.Provider.(Needer); !ok || !n.Needs().HTTPS || !n.Needs().PublicMedia {
		t.Fatal("threads needs https and public media")
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := th.AuthURL(app, "https://app.example/cb", "st", nil)
	if q, _ := url.Parse(u); q.Query().Get("scope") != threadsScopes || q.Query().Get("state") != "st" || q.Query().Get("response_type") != "code" {
		t.Fatalf("auth url %s", u)
	}
	ctx := context.Background()
	creds, profile, err := th.Exchange(ctx, app, "https://app.example/cb", "code1#_", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenForm.Get("code") != "code1" || f.tokenForm.Get("client_secret") != "secret" || f.tokenForm.Get("grant_type") != "authorization_code" {
		t.Fatalf("token form %v", f.tokenForm)
	}
	if creds["access_token"] != "long" || creds["user_id"] != "1784" || creds["expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe" || profile.Name != "Jane Doe" || profile.ProfileURL != "https://www.threads.com/@janedoe" {
		t.Fatalf("profile %+v", profile)
	}
	if _, _, err := th.Exchange(ctx, app, "https://app.example/cb", "bad", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad code: %v", err)
	}

	// Refresh: over a month left only checks, under a month renews, run out or revoked needs reconnecting.
	if c, err := th.Refresh(ctx, app, threadsCreds(50*24*time.Hour)); err != nil || c["access_token"] != "long" {
		t.Fatalf("young: %v %v", c, err)
	}
	if c, err := th.Refresh(ctx, app, threadsCreds(5*24*time.Hour)); err != nil || c["access_token"] != "renewed" || c["user_id"] != "1784" {
		t.Fatalf("old: %v %v", c, err)
	}
	if _, err := th.Refresh(ctx, app, threadsCreds(-time.Hour)); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
	revoked := threadsCreds(50 * 24 * time.Hour)
	revoked["access_token"] = "revoked"
	if _, err := th.Refresh(ctx, app, revoked); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}

	// The app keys.
	if ok, err := (Threads{}).CheckApp(ctx, app, ""); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (Threads{}).CheckApp(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, ""); err == nil {
		t.Fatal("bad keys passed")
	}
}

func TestThreadsPublish(t *testing.T) {
	ctx := context.Background()
	th, _ := Get("threads")

	t.Run("text with reply control", func(t *testing.T) {
		f := useFakeThreads(t)
		res, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Text: "Hello"}, Settings{"reply_control": "mentioned_only"})
		if err != nil {
			t.Fatal(err)
		}
		containers, published, polls, _ := f.seen()
		c := containers[0]
		if c.Get("media_type") != "TEXT" || c.Get("text") != "Hello" || c.Get("reply_control") != "mentioned_only" || c.Get("access_token") != "long" {
			t.Fatalf("container %v", c)
		}
		if published[0].Get("creation_id") != "c1" || polls < 2 {
			t.Fatalf("published %v after %d checks", published, polls)
		}
		if res.ExternalID != "17901" || res.URL != "https://www.threads.com/@janedoe/post/DAbc" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("one picture, and a carousel", func(t *testing.T) {
		f := useFakeThreads(t)
		if _, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Text: "Pic", Media: []MediaFile{jpeg("a.jpg")}}, Settings{}); err != nil {
			t.Fatal(err)
		}
		if _, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Text: "Three", Media: []MediaFile{jpeg("a.jpg"), mp4("b.mp4"), jpeg("c.jpg")}}, Settings{}); err != nil {
			t.Fatal(err)
		}
		containers, published, _, _ := f.seen()
		if c := containers[0]; c.Get("media_type") != "IMAGE" || c.Get("image_url") != "https://bucket.example/a.jpg" || c.Get("text") != "Pic" {
			t.Fatalf("picture %v", c)
		}
		if c := containers[2]; c.Get("media_type") != "VIDEO" || c.Get("is_carousel_item") != "true" || c.Get("video_url") == "" || c.Has("text") {
			t.Fatalf("child %v", c)
		}
		if p := containers[4]; p.Get("media_type") != "CAROUSEL" || p.Get("children") != "c2,c3,c4" || p.Get("text") != "Three" {
			t.Fatalf("carousel %v", p)
		}
		if published[1].Get("creation_id") != "c5" {
			t.Fatalf("published %v", published)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		f := useFakeThreads(t)
		if _, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Media: []MediaFile{{Name: "a.jpg", MimeType: "image/jpeg"}}}, Settings{}); !errors.Is(err, ErrRejected) ||
			strings.Contains(err.Error(), "STORAGE_DRIVER") {
			t.Fatalf("no public link: %v", err)
		}
		f.set(func() { f.statusFor["c1"] = "ERROR" })
		if _, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Media: []MediaFile{jpeg("a.jpg")}}, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "FILE_FORMAT") {
			t.Fatalf("container error: %v", err)
		}
		for body, want := range map[string]error{
			`{"error":{"message":"Session has expired","code":190}}`:                                  ErrReauth,
			`{"error":{"message":"Application request limit reached","code":4}}`:                      nil,
			`{"error":{"message":"Media not found","code":24,"error_subcode":4279009}}`:               nil,
			`{"error":{"message":"restricted","code":10,"error_subcode":2207051}}`:                    ErrRejected,
			`{"error":{"message":"Param text must be at most 500 characters long.","code":100}}`:      ErrRejected,
			`{"error":{"message":"An unknown error has occurred.","code":1,"type":"OAuthException"}}`: nil,
		} {
			f.set(func() { f.statusFor["c1"], f.publishCode, f.publishBody = "", http.StatusBadRequest, body })
			_, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Text: "x"}, Settings{})
			var fail *Failure
			if !errors.As(err, &fail) || fail.Kind != want {
				t.Fatalf("%s: %v", body, err)
			}
		}
		if _, err := th.Publish(ctx, Credentials{}, PostContent{Text: "x"}, Settings{}); !errors.Is(err, ErrReauth) {
			t.Fatalf("no account: %v", err)
		}
	})

	t.Run("an answer that never came is not retried", func(t *testing.T) {
		f := useFakeThreads(t)
		f.set(func() { f.dropPublish = true })
		_, err := th.Publish(ctx, threadsCreds(time.Hour), PostContent{Text: "x"}, Settings{})
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") || strings.Contains(err.Error(), "long") {
			t.Fatalf("lost answer: %v", err)
		}
	})
}

func TestThreadsReply(t *testing.T) {
	f := useFakeThreads(t)
	th, _ := Get("threads")
	r := th.Provider.(Replier)
	ctx := context.Background()
	first, err := r.Reply(ctx, threadsCreds(time.Hour), "17900", PostContent{Text: "First"}, Settings{"reply_control": "everyone"})
	if err != nil || first.ExternalID != "17901" {
		t.Fatalf("first %+v %v", first, err)
	}
	// The next part replies to the one before, and may carry a picture.
	if _, err := r.Reply(ctx, threadsCreds(time.Hour), first.ExternalID, PostContent{Text: "Second", Media: []MediaFile{jpeg("a.jpg")}}, nil); err != nil {
		t.Fatal(err)
	}
	containers, _, _, _ := f.seen()
	if containers[0].Get("reply_to_id") != "17900" || containers[0].Has("reply_control") || containers[1].Get("reply_to_id") != "17901" || containers[1].Get("media_type") != "IMAGE" {
		t.Fatalf("replies %v", containers)
	}
}

func TestThreadsValidate(t *testing.T) {
	th, _ := Get("threads")
	for _, c := range []struct {
		name   string
		text   string
		media  []MediaFile
		s      Settings
		thread []ThreadPart
		ok     bool
	}{
		{"text", "Hello", nil, Settings{}, nil, true},
		{"too long", strings.Repeat("a", 501), nil, Settings{}, nil, false},
		{"png", "x", []MediaFile{{Name: "a.png", MimeType: "image/png"}}, Settings{}, nil, true},
		{"webp", "x", []MediaFile{{Name: "a.webp", MimeType: "image/webp"}}, Settings{}, nil, false},
		{"webp in a reply", "x", nil, Settings{}, []ThreadPart{{Text: "r", Media: []MediaFile{{Name: "a.webp", MimeType: "image/webp"}}}}, false},
		{"reply control", "x", nil, Settings{"reply_control": "accounts_you_follow"}, nil, true},
		{"unknown reply control", "x", nil, Settings{"reply_control": "nobody"}, nil, false},
	} {
		err := th.Validate(PostContent{Text: c.text, Media: c.media, Thread: c.thread}, c.s)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
