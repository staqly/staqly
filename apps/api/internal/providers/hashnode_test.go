package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeHashnode answers like gql.hashnode.com.
type fakeHashnode struct {
	mu       sync.Mutex
	pubs     string         // the publications' edges JSON
	input    map[string]any // the last publishPost input
	answer   string         // a body for publishPost instead of the post (GraphQL errors)
	httpCode int            // status for publishPost (0 = 200)
	drop     bool
}

func (f *fakeHashnode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var body struct {
		Query     string `json:"query"`
		Variables struct {
			Input map[string]any `json:"input"`
		} `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if r.Header.Get("Authorization") != "good" {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Invalid token provided","extensions":{"code":"UNAUTHENTICATED"}}],"data":null}`))
		return
	}
	switch {
	case strings.Contains(body.Query, "me {"):
		_, _ = w.Write([]byte(`{"data":{"me":{"id":"u1","username":"janedoe","name":"Jane Doe","profilePicture":"https://cdn.example/jane.png","publications":{"edges":` + f.pubs + `}}}}`))
	case strings.Contains(body.Query, "publishPost"):
		f.input = body.Variables.Input
		if f.drop {
			blogDropConn(w)
			return
		}
		if f.httpCode != 0 {
			w.WriteHeader(f.httpCode)
		}
		if f.answer != "" {
			_, _ = w.Write([]byte(f.answer))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"publishPost":{"post":{"id":"post1","url":"https://blog.example.com/hello"}}}}`))
	}
}

const (
	hashnodeOnePub  = `[{"node":{"id":"pub1","title":"Jane's blog","url":"https://blog.example.com"}}]`
	hashnodeTwoPubs = `[{"node":{"id":"pub1","title":"Jane's blog","url":"https://blog.example.com"}},{"node":{"id":"pub2","title":"Notes","url":"https://janedoe.hashnode.dev"}}]`
)

func newFakeHashnode(t *testing.T) *fakeHashnode {
	f := &fakeHashnode{pubs: hashnodeOnePub}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := hashnodeAPI
	hashnodeAPI = srv.URL
	t.Cleanup(func() { hashnodeAPI = old })
	return f
}

func TestHashnodeConnect(t *testing.T) {
	f := newFakeHashnode(t)
	h, _ := Get("hashnode")
	if h.Status != StatusAvailable {
		t.Fatal("hashnode is not available")
	}
	c, p, err := h.Connect(context.Background(), map[string]string{"token": "good"})
	if err != nil {
		t.Fatal(err)
	}
	if c["publication"] != "pub1" || p.Handle != "blog.example.com" || p.Name != "Jane's blog" {
		t.Fatalf("creds %v profile %+v", c, p)
	}

	f.pubs = hashnodeTwoPubs
	_, _, err = h.Connect(context.Background(), map[string]string{"token": "good"})
	blogWantInvalid(t, err, "publication")
	if !strings.Contains(err.Error(), "janedoe.hashnode.dev") {
		t.Fatalf("the choices are not listed: %v", err)
	}
	c, p, err = h.Connect(context.Background(), map[string]string{"token": "good", "publication": "https://JaneDoe.hashnode.dev/"})
	if err != nil || c["publication"] != "pub2" || p.Handle != "janedoe.hashnode.dev" {
		t.Fatalf("creds %v profile %+v err %v", c, p, err)
	}
	// A test of the channel connects again with the stored credentials (the publication id).
	if _, _, err := h.Connect(context.Background(), map[string]string(c)); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.Connect(context.Background(), map[string]string{"token": "good", "publication": "nope.example.com"})
	blogWantInvalid(t, err, "publication")

	f.pubs = `[]`
	_, _, err = h.Connect(context.Background(), map[string]string{"token": "good"})
	blogWantInvalid(t, err, "publication")

	_, _, err = h.Connect(context.Background(), map[string]string{"token": "bad"})
	blogWantKind(t, err, ErrReauth, "Invalid token")
}

func TestHashnodePublish(t *testing.T) {
	f := newFakeHashnode(t)
	h, _ := Get("hashnode")
	creds := Credentials{"token": "good", "publication": "pub1"}
	s := Settings{"title": "Hello", "subtitle": "A start", "tags": "Web Development, #go", "canonical_url": "https://example.com/hello"}
	pic := MediaFile{Name: "c.png", MimeType: "image/png", SignedURL: "https://bucket.example/c.png?sig=1"}
	res, err := h.Publish(context.Background(), creds, PostContent{Text: "# Hi", Media: []MediaFile{pic}}, s)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "post1" || res.URL != "https://blog.example.com/hello" {
		t.Fatalf("result %+v", res)
	}
	in := f.input
	tags, _ := in["tags"].([]any)
	if in["publicationId"] != "pub1" || in["contentMarkdown"] != "# Hi" || in["subtitle"] != "A start" ||
		in["originalArticleURL"] != "https://example.com/hello" || len(tags) != 2 {
		t.Fatalf("input %v", in)
	}
	if tag := tags[0].(map[string]any); tag["slug"] != "web-development" || tag["name"] != "Web Development" {
		t.Fatalf("tag %v", tag)
	}
	if cover := in["coverImageOptions"].(map[string]any); cover["coverImageURL"] != pic.SignedURL {
		t.Fatalf("cover %v", cover)
	}

	pic.SignedURL = ""
	_, err = h.Publish(context.Background(), creds, PostContent{Text: "x", Media: []MediaFile{pic}}, s)
	blogWantKind(t, err, ErrRejected, "cover picture")
}

func TestHashnodeFailures(t *testing.T) {
	f := newFakeHashnode(t)
	h, _ := Get("hashnode")
	creds := Credentials{"token": "good", "publication": "pub1"}
	post, s := PostContent{Text: "Body"}, Settings{"title": "Hello"}

	f.answer = `{"errors":[{"message":"Tag not found","extensions":{"code":"BAD_USER_INPUT"}}],"data":null}`
	_, err := h.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "Tag not found")

	f.answer = `{"errors":[{"message":"You are not allowed to publish","extensions":{"code":"FORBIDDEN"}}],"data":null}`
	_, err = h.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "not allowed")

	f.httpCode, f.answer = http.StatusTooManyRequests, `{"errors":[{"message":"Too many requests"}]}`
	_, err = h.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "rate limit")

	f.httpCode, f.answer = http.StatusBadGateway, `bad gateway`
	_, err = h.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "unavailable")

	f.httpCode, f.answer, f.drop = 0, "", true
	_, err = h.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "may have published")

	_, err = h.Publish(context.Background(), Credentials{"token": "revoked", "publication": "pub1"}, post, s)
	blogWantKind(t, err, ErrReauth, "")
}

func TestHashnodeValidate(t *testing.T) {
	h, _ := Get("hashnode")
	blogWantInvalid(t, h.Validate(PostContent{Text: "x"}, Settings{}), "title")
	blogWantInvalid(t, h.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "a,b,c,d,e,f"}), "tags")
	blogWantInvalid(t, h.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "--"}), "tags")
	blogWantInvalid(t, h.Validate(PostContent{Text: "x", Media: []MediaFile{{Name: "v.mp4", MimeType: "video/mp4"}}}, Settings{"title": "T"}), "media")
	if err := h.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "go"}); err != nil {
		t.Fatal(err)
	}
	if hashnodeSlug("  C++ & Rust ") != "c-rust" {
		t.Fatal(hashnodeSlug("  C++ & Rust "))
	}
}
