package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMedium answers like api.medium.com/v1.
type fakeMedium struct {
	mu        sync.Mutex
	post      map[string]any // the last story's body
	postPath  string
	image     string // the uploaded picture's bytes
	imageType string
	postCode  int    // status for the story (0 = 201)
	postBody  string // its body then
	drop      bool   // close the connection on the story
}

func (f *fakeMedium) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer good" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Token was invalid.","code":6003}]}`))
		return
	}
	switch {
	case r.URL.Path == "/me":
		_, _ = w.Write([]byte(`{"data":{"id":"u1","username":"janedoe","name":"Jane Doe","url":"https://medium.com/@janedoe","imageUrl":"https://cdn.example/jane.png"}}`))
	case r.URL.Path == "/users/u1/publications":
		_, _ = w.Write([]byte(`{"data":[{"id":"p1","name":"Runner's Notes","url":"https://medium.com/runners-notes"}]}`))
	case r.URL.Path == "/images":
		file, hdr, err := r.FormFile("image")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(file)
		f.image, f.imageType = string(b), hdr.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"data":{"url":"https://cdn-images.example/abc.png","md5":"abc"}}`))
	case strings.HasSuffix(r.URL.Path, "/posts"):
		f.postPath = r.URL.Path
		f.post = nil
		_ = json.NewDecoder(r.Body).Decode(&f.post)
		if f.drop {
			blogDropConn(w)
			return
		}
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"e6f36a","url":"https://medium.com/@janedoe/my-story-e6f36a"}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFakeMedium(t *testing.T) *fakeMedium {
	f := &fakeMedium{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := mediumAPI
	mediumAPI = srv.URL
	t.Cleanup(func() { mediumAPI = old })
	return f
}

func TestMediumConnect(t *testing.T) {
	newFakeMedium(t)
	m, _ := Get("medium")
	if m.Status != StatusAvailable {
		t.Fatal("medium is not available")
	}
	c, p, err := m.Connect(context.Background(), map[string]string{"token": " good "})
	if err != nil {
		t.Fatal(err)
	}
	if c["token"] != "good" || c["user_id"] != "u1" || p.Handle != "@janedoe" || p.Name != "Jane Doe" {
		t.Fatalf("creds %v profile %+v", c, p)
	}
	// A publication by its link; a test of the channel connects again with the stored credentials.
	c, p, err = m.Connect(context.Background(), map[string]string{"token": "good", "publication": "medium.com/runners-notes/"})
	if err != nil {
		t.Fatal(err)
	}
	if c["publication"] != "p1" || p.Handle != "@janedoe in runners-notes" || p.Name != "Runner's Notes" {
		t.Fatalf("creds %v profile %+v", c, p)
	}
	if _, _, err := m.Connect(context.Background(), map[string]string(c)); err != nil {
		t.Fatal(err)
	}
	_, _, err = m.Connect(context.Background(), map[string]string{"token": "good", "publication": "other"})
	blogWantInvalid(t, err, "publication")
	_, _, err = m.Connect(context.Background(), map[string]string{"token": "revoked"})
	blogWantKind(t, err, ErrReauth, "integration token")
}

func TestMediumPublish(t *testing.T) {
	f := newFakeMedium(t)
	m, _ := Get("medium")
	creds := Credentials{"token": "good", "user_id": "u1"}
	s := Settings{"title": "My story", "tags": "running, go", "canonical_url": "https://example.com/a", "publish_status": "unlisted"}
	post := PostContent{Text: "Body **here**.", Media: []MediaFile{blogMemFile("pic.png", "image/png", "PNGDATA")}}
	if err := m.Validate(post, s); err != nil {
		t.Fatal(err)
	}
	res, err := m.Publish(context.Background(), creds, post, s)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "e6f36a" || !strings.Contains(res.URL, "my-story") {
		t.Fatalf("result %+v", res)
	}
	if f.image != "PNGDATA" || f.imageType != "image/png" || f.postPath != "/users/u1/posts" {
		t.Fatalf("image %q %q path %s", f.image, f.imageType, f.postPath)
	}
	want := "# My story\n\n![](https://cdn-images.example/abc.png)\n\nBody **here**."
	if f.post["content"] != want || f.post["contentFormat"] != "markdown" || f.post["publishStatus"] != "unlisted" ||
		f.post["canonicalUrl"] != "https://example.com/a" || len(f.post["tags"].([]any)) != 2 {
		t.Fatalf("post %v", f.post)
	}

	// To a publication, text only, public by default.
	creds["publication"] = "p1"
	if _, err := m.Publish(context.Background(), creds, PostContent{Text: "Hi"}, Settings{"title": "T"}); err != nil {
		t.Fatal(err)
	}
	if f.postPath != "/publications/p1/posts" || f.post["publishStatus"] != "public" || f.post["tags"] != nil {
		t.Fatalf("path %s post %v", f.postPath, f.post)
	}
}

func TestMediumFailures(t *testing.T) {
	f := newFakeMedium(t)
	m, _ := Get("medium")
	creds := Credentials{"token": "good", "user_id": "u1"}
	post, s := PostContent{Text: "Hi"}, Settings{"title": "T"}

	f.postCode, f.postBody = http.StatusBadRequest, `{"errors":[{"message":"Publication not found.","code":6010}]}`
	_, err := m.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "Publication not found")

	f.postCode, f.postBody = http.StatusTooManyRequests, `{"errors":[{"message":"Too many requests","code":-1}]}`
	_, err = m.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "rate limit")

	f.postCode, f.postBody = http.StatusBadGateway, ``
	_, err = m.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "unavailable")

	f.postCode = 0
	f.drop = true
	_, err = m.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "may have published")

	_, err = m.Publish(context.Background(), Credentials{"token": "revoked", "user_id": "u1"}, post, s)
	blogWantKind(t, err, ErrReauth, "")
}

func TestMediumValidate(t *testing.T) {
	m, _ := Get("medium")
	ok := Settings{"title": "T"}
	blogWantInvalid(t, m.Validate(PostContent{Text: "x"}, Settings{}), "title")
	blogWantInvalid(t, m.Validate(PostContent{Text: "x"}, Settings{"title": "  "}), "title")
	blogWantInvalid(t, m.Validate(PostContent{Text: "x", Media: []MediaFile{{Name: "a.webp", MimeType: "image/webp"}}}, ok), "media")
	blogWantInvalid(t, m.Validate(PostContent{Text: "x", Media: []MediaFile{{MimeType: "image/png"}, {MimeType: "image/png"}}}, ok), "media")
	blogWantInvalid(t, m.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "a,b,c,d"}), "tags")
	blogWantInvalid(t, m.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": strings.Repeat("a", 26)}), "tags")
	blogWantInvalid(t, m.Validate(PostContent{Text: "x"}, Settings{"title": "T", "publish_status": "secret"}), "publish_status")
	if err := m.Validate(PostContent{Text: "x"}, ok); err != nil {
		t.Fatal(err)
	}
}
