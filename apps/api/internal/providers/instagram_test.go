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

// fakeInstagram answers like api.instagram.com and graph.instagram.com for the calls the provider makes.
type fakeInstagram struct {
	mu          sync.Mutex
	calls       []string
	tokenForm   url.Values
	query       url.Values        // the last GET's query
	containers  []url.Values      // forms of POST /{user}/media, in order
	published   url.Values        // form of media_publish
	comments    []url.Values      // forms of POST /{media}/comments
	permissions string            // the short token's permissions (default: all three scopes)
	polls       int               // status checks so far
	publishCode int               // status for media_publish (0 = 200)
	publishBody string            // its body then
	dropPublish bool              // close the connection on media_publish (an answer that never came)
	statusFor   map[string]string // container id -> status_code (default: IN_PROGRESS once, then FINISHED)
	unusable    bool              // the long-lived exchange refuses the token (an account the app may not serve)
	srv         *httptest.Server
}

func (f *fakeInstagram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(raw))
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		f.query = r.URL.Query()
	}
	path := strings.TrimPrefix(r.URL.Path, "/"+instagramVersion)
	switch {
	case r.URL.Path == "/oauth/access_token":
		f.tokenForm = form
		perms := f.permissions
		if perms == "" {
			perms = instagramScopes
		}
		_, _ = w.Write([]byte(`{"data":[{"access_token":"short","user_id":"1784","permissions":"` + perms + `"}]}`))
	case r.URL.Path == "/access_token" && f.unusable:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Unsupported request - method type: get","type":"IGApiException","code":100}}`))
	case r.URL.Path == "/access_token":
		_, _ = w.Write([]byte(`{"access_token":"long","token_type":"bearer","expires_in":5183944}`))
	case r.URL.Path == "/refresh_access_token":
		_, _ = w.Write([]byte(`{"access_token":"renewed","token_type":"bearer","expires_in":5183944}`))
	case path == "/me" && r.URL.Query().Get("access_token") == "revoked":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Error validating access token: The session has been invalidated","type":"OAuthException","code":190}}`))
	case path == "/me":
		_, _ = w.Write([]byte(`{"id":"app-scoped","user_id":"1784","username":"janedoe","name":"Jane Doe","profile_picture_url":"https://cdn.example/jane.jpg","account_type":"BUSINESS"}`))
	case path == "/1784/media":
		f.containers = append(f.containers, form)
		_, _ = w.Write([]byte(`{"id":"c` + string(rune('0'+len(f.containers))) + `"}`))
	case path == "/1784/media_publish":
		f.published = form
		if f.dropPublish {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		if f.publishCode != 0 {
			w.WriteHeader(f.publishCode)
			_, _ = w.Write([]byte(f.publishBody))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1790"}`))
	case strings.HasSuffix(path, "/comments"):
		f.comments = append(f.comments, form)
		_, _ = w.Write([]byte(`{"id":"1800` + string(rune('0'+len(f.comments))) + `"}`))
	case r.URL.Query().Get("fields") == "permalink":
		_, _ = w.Write([]byte(`{"permalink":"https://www.instagram.com/p/DAbc/"}`))
	case r.URL.Query().Get("fields") == "status_code,status":
		f.polls++
		id := strings.TrimPrefix(path, "/")
		st := f.statusFor[id]
		if st == "" {
			st = "FINISHED"
			if f.polls == 1 {
				st = "IN_PROGRESS"
			}
		}
		_, _ = w.Write([]byte(`{"status_code":"` + st + `","status":"` + st + `: details"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeInstagram(t *testing.T) *fakeInstagram {
	t.Helper()
	f := &fakeInstagram{statusFor: map[string]string{}}
	f.srv = httptest.NewServer(f)
	auth, tok, graph, poll := instagramAuthorize, instagramToken, instagramGraph, instagramPoll
	instagramAuthorize, instagramToken, instagramGraph, instagramPoll = f.srv.URL+"/oauth/authorize", f.srv.URL+"/oauth/access_token", f.srv.URL, time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		instagramAuthorize, instagramToken, instagramGraph, instagramPoll = auth, tok, graph, poll
	})
	return f
}

func instagramCreds(expires time.Duration) Credentials {
	return Credentials{"access_token": "long", "user_id": "1784", "expires_at": time.Now().Add(expires).UTC().Format(time.RFC3339)}
}

func jpeg(name string) MediaFile {
	return MediaFile{Name: name, MimeType: "image/jpeg", SignedURL: "https://bucket.example/" + name}
}

func mp4(name string) MediaFile {
	return MediaFile{Name: name, MimeType: "video/mp4", SignedURL: "https://bucket.example/" + name}
}

func TestInstagramConnect(t *testing.T) {
	f := useFakeInstagram(t)
	ig, _ := Get("instagram")
	if ig.Status != StatusAvailable || ig.Name() != "Instagram" {
		t.Fatalf("instagram is %q %q", ig.Status, ig.Name())
	}
	if b, _ := Get("instagram_business"); b.Status != StatusAvailable {
		t.Fatalf("instagram_business is %q, want available", b.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := ig.AuthURL(app, "https://app.example/cb", "st", nil)
	q, _ := url.Parse(u)
	if q.Query().Get("scope") != instagramScopes || q.Query().Get("state") != "st" || q.Query().Get("enable_fb_login") != "0" {
		t.Fatalf("auth url %s", u)
	}

	// Instagram sends the code back with "#_" on its end.
	creds, profile, err := ig.Exchange(context.Background(), app, "https://app.example/cb", "code1#_", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenForm.Get("code") != "code1" || f.tokenForm.Get("client_secret") != "secret" || f.tokenForm.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("token form %v", f.tokenForm)
	}
	// The hour-long token is traded for the 60-day one, which is what is stored.
	if creds["access_token"] != "long" || creds["user_id"] != "1784" || creds["expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe" || profile.Name != "Jane Doe" || profile.ProfileURL != "https://www.instagram.com/janedoe/" || profile.NoComments {
		t.Fatalf("profile %+v", profile)
	}

	// Comments not granted: the channel says so; publishing not granted: no channel.
	f.permissions = "instagram_business_basic,instagram_business_content_publish"
	if _, profile, err = ig.Exchange(context.Background(), app, "https://app.example/cb", "c2", nil); err != nil || !profile.NoComments {
		t.Fatalf("without comments: %+v %v", profile, err)
	}
	f.permissions = "instagram_business_basic"
	if _, _, err := ig.Exchange(context.Background(), app, "https://app.example/cb", "c3", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("without publishing: %v", err)
	}

	// An account the app may not serve (personal, or not a tester): say what to check.
	f.permissions, f.unusable = "", true
	if _, _, err := ig.Exchange(context.Background(), app, "https://app.example/cb", "c4", nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "tester") {
		t.Fatalf("unusable token: %v", err)
	}
}

func TestInstagramRefresh(t *testing.T) {
	f := useFakeInstagram(t)
	ig, _ := Get("instagram")
	ctx := context.Background()
	// Over a month left: only checked; under a month: renewed for 60 days; run out or revoked: reconnect.
	if c, err := ig.Refresh(ctx, App{}, instagramCreds(50*24*time.Hour)); err != nil || c["access_token"] != "long" {
		t.Fatalf("young token: %v %v", c, err)
	}
	if strings.Contains(strings.Join(f.calls, ","), "refresh_access_token") {
		t.Fatal("a young token was refreshed")
	}
	c, err := ig.Refresh(ctx, App{}, instagramCreds(5*24*time.Hour))
	if err != nil || c["access_token"] != "renewed" || c["user_id"] != "1784" {
		t.Fatalf("old token: %v %v", c, err)
	}
	if exp, _ := credentialsExpiry(c); exp.Before(time.Now().Add(50 * 24 * time.Hour)) {
		t.Fatalf("renewed until %v", exp)
	}
	if _, err := ig.Refresh(ctx, App{}, instagramCreds(-time.Hour)); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
	revoked := instagramCreds(50 * 24 * time.Hour)
	revoked["access_token"] = "revoked"
	if _, err := ig.Refresh(ctx, App{}, revoked); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestInstagramPublish(t *testing.T) {
	ctx := context.Background()
	ig, _ := Get("instagram")

	t.Run("picture with collaborators", func(t *testing.T) {
		f := useFakeInstagram(t)
		res, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Text: "Golden hour #run", Media: []MediaFile{jpeg("a.jpg")}},
			Settings{"post_type": "post", "collaborators": "@ana, ben"})
		if err != nil {
			t.Fatal(err)
		}
		c := f.containers[0]
		if c.Get("image_url") != "https://bucket.example/a.jpg" || c.Get("caption") != "Golden hour #run" || c.Get("collaborators") != `["ana","ben"]` {
			t.Fatalf("container %v", c)
		}
		if f.published.Get("creation_id") != "c1" || f.polls < 2 {
			t.Fatalf("published %v after %d checks", f.published, f.polls)
		}
		if res.ExternalID != "1790" || res.URL != "https://www.instagram.com/p/DAbc/" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("carousel", func(t *testing.T) {
		f := useFakeInstagram(t)
		if _, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Text: "Three", Media: []MediaFile{jpeg("a.jpg"), mp4("b.mp4"), jpeg("c.jpg")}},
			Settings{"collaborators": "ana"}); err != nil {
			t.Fatal(err)
		}
		if len(f.containers) != 4 {
			t.Fatalf("containers %v", f.containers)
		}
		// Children carry no caption and no collaborators; the parent lists them.
		if f.containers[1].Get("media_type") != "VIDEO" || f.containers[1].Get("is_carousel_item") != "true" || f.containers[0].Has("caption") || f.containers[0].Has("collaborators") {
			t.Fatalf("children %v", f.containers[:3])
		}
		p := f.containers[3]
		if p.Get("media_type") != "CAROUSEL" || p.Get("children") != "c1,c2,c3" || p.Get("caption") != "Three" || p.Get("collaborators") != `["ana"]` {
			t.Fatalf("parent %v", p)
		}
		if f.published.Get("creation_id") != "c4" {
			t.Fatalf("published %v", f.published)
		}
	})

	t.Run("reel and story", func(t *testing.T) {
		f := useFakeInstagram(t)
		if _, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Text: "Reel", Media: []MediaFile{mp4("r.mp4")}},
			Settings{"post_type": "reel", "share_to_feed": "false"}); err != nil {
			t.Fatal(err)
		}
		if c := f.containers[0]; c.Get("media_type") != "REELS" || c.Get("video_url") != "https://bucket.example/r.mp4" || c.Get("share_to_feed") != "false" {
			t.Fatalf("reel %v", c)
		}
		if _, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Text: "ignored", Media: []MediaFile{jpeg("s.jpg")}},
			Settings{"post_type": "story"}); err != nil {
			t.Fatal(err)
		}
		if c := f.containers[1]; c.Get("media_type") != "STORIES" || c.Get("image_url") == "" || c.Has("caption") {
			t.Fatalf("story %v", c)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		f := useFakeInstagram(t)
		// Media on the server's own disk has no link Instagram can fetch.
		if _, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Media: []MediaFile{{Name: "a.jpg", MimeType: "image/jpeg"}}}, Settings{}); !errors.Is(err, ErrRejected) ||
			strings.Contains(err.Error(), "STORAGE_DRIVER") {
			t.Fatalf("no public link: %v", err)
		}
		// A container Instagram could not process.
		f.statusFor["c1"] = "ERROR"
		if _, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Media: []MediaFile{jpeg("a.jpg")}}, Settings{}); !errors.Is(err, ErrRejected) || f.published != nil {
			t.Fatalf("container error: %v", err)
		}
		// The daily limit is final; a failed download is retried; a revoked sign-in needs reconnecting.
		for body, want := range map[string]error{
			`{"error":{"message":"limit","code":9,"error_subcode":2207042}}`:           ErrRejected,
			`{"error":{"message":"fetch failed","code":9004,"error_subcode":2207052}}`: nil,
			`{"error":{"message":"expired","code":190}}`:                               ErrReauth,
		} {
			f.statusFor["c1"] = ""
			f.publishCode, f.publishBody = http.StatusBadRequest, body
			_, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Media: []MediaFile{jpeg("a.jpg")}}, Settings{})
			var fail *Failure
			if !errors.As(err, &fail) || fail.Kind != want {
				t.Fatalf("%s: %v", body, err)
			}
		}
	})

	t.Run("an answer that never came is not retried", func(t *testing.T) {
		f := useFakeInstagram(t)
		f.dropPublish = true
		_, err := ig.Publish(ctx, instagramCreds(time.Hour), PostContent{Media: []MediaFile{jpeg("a.jpg")}}, Settings{})
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") || strings.Contains(err.Error(), "long") {
			t.Fatalf("lost answer: %v", err)
		}
	})
}

func TestInstagramValidate(t *testing.T) {
	ig, _ := Get("instagram")
	for _, c := range []struct {
		name  string
		media []MediaFile
		s     Settings
		ok    bool
	}{
		{"picture", []MediaFile{jpeg("a.jpg")}, Settings{}, true},
		{"png", []MediaFile{{Name: "a.png", MimeType: "image/png"}}, Settings{}, false},
		{"no media", nil, Settings{}, false},
		{"reel of a picture", []MediaFile{jpeg("a.jpg")}, Settings{"post_type": "reel"}, false},
		{"reel", []MediaFile{mp4("a.mp4")}, Settings{"post_type": "reel"}, true},
		{"story of two", []MediaFile{jpeg("a.jpg"), jpeg("b.jpg")}, Settings{"post_type": "story"}, false},
		{"four collaborators", []MediaFile{jpeg("a.jpg")}, Settings{"post_type": "post", "collaborators": "a,b,c,d"}, false},
	} {
		err := ig.Validate(PostContent{Text: "x", Media: c.media}, c.s)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestInstagramReply(t *testing.T) {
	f := useFakeInstagram(t)
	ig, _ := Get("instagram")
	r := ig.Provider.(Replier)
	ctx := context.Background()
	first, err := r.Reply(ctx, instagramCreds(time.Hour), "1790", PostContent{Text: "First"}, nil)
	if err != nil || first.ExternalID != "comment:1790:18001" {
		t.Fatalf("first %+v %v", first, err)
	}
	// The next part comments on the post too, after the one before.
	if _, err := r.Reply(ctx, instagramCreds(time.Hour), first.ExternalID, PostContent{Text: "Second"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.comments) != 2 || f.comments[1].Get("message") != "Second" || !strings.Contains(strings.Join(f.calls, ","), "/1790/comments") {
		t.Fatalf("comments %v calls %v", f.comments, f.calls)
	}
}
