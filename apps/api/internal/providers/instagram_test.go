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

// fakeInstagram answers like graph.facebook.com for the publishing calls the Instagram provider makes.
type fakeInstagram struct {
	mu          sync.Mutex
	calls       []string
	query       url.Values        // the last GET's query
	containers  []url.Values      // forms of POST /{user}/media, in order
	published   url.Values        // form of media_publish
	comments    []url.Values      // forms of POST /{media}/comments
	polls       int               // status checks so far
	publishCode int               // status for media_publish (0 = 200)
	publishBody string            // its body then
	dropPublish bool              // close the connection on media_publish (an answer that never came)
	statusFor   map[string]string // container id -> status_code (default: IN_PROGRESS once, then FINISHED)
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
	graph, poll := facebookGraph, instagramPoll
	facebookGraph, instagramPoll = f.srv.URL, time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		facebookGraph, instagramPoll = graph, poll
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
