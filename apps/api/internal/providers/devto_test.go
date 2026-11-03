package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDevTo answers like dev.to/api.
type fakeDevTo struct {
	mu       sync.Mutex
	article  map[string]any // the last article sent
	accept   string
	postCode int
	postBody string
	drop     bool
	existing string // body_markdown of the author's published article "Hello" (for duplicates)
}

func (f *fakeDevTo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	f.accept = r.Header.Get("Accept")
	if r.Header.Get("api-key") != "good" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","status":401}`))
		return
	}
	switch {
	case r.URL.Path == "/users/me":
		_, _ = w.Write([]byte(`{"type_of":"user","id":42,"username":"janedoe","name":"Jane Doe","profile_image":"https://cdn.example/jane.png"}`))
	case r.URL.Path == "/articles" && r.Method == http.MethodPost:
		var body struct {
			Article map[string]any `json:"article"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.article = body.Article
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
		_, _ = w.Write([]byte(`{"id":101,"url":"https://dev.to/janedoe/hello-1a2b"}`))
	case r.URL.Path == "/articles/me/all":
		b, _ := json.Marshal([]map[string]any{
			{"id": 7, "title": "Other", "url": "https://dev.to/janedoe/other", "body_markdown": "x"},
			{"id": 99, "title": "Hello", "url": "https://dev.to/janedoe/hello-99", "body_markdown": f.existing},
		})
		_, _ = w.Write(b)
	case r.URL.Path == "/articles/101":
		_, _ = w.Write([]byte(`{"id":101,"public_reactions_count":7,"comments_count":2}`))
	case r.URL.Path == "/articles/me/published":
		_, _ = w.Write([]byte(`[{"id":7,"page_views_count":1},{"id":101,"page_views_count":250}]`))
	case r.URL.Path == "/comments" && r.URL.Query().Get("a_id") == "101":
		_, _ = w.Write([]byte(`[{"id_code":"c1","created_at":"2026-11-03T10:00:00Z","body_html":"<p>Nice &amp; clear</p>","user":{"name":"Ann","username":"ann"},
			"children":[{"id_code":"c2","created_at":"2026-11-03T11:00:00Z","body_html":"<p>Thanks!</p>","user":{"name":"Jane Doe","username":"janedoe"},"children":[]}]},
			{"id_code":"c3","created_at":"2026-11-03T09:00:00Z","body_html":"<p>[deleted]</p>","user":{},"children":[]}]`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFakeDevTo(t *testing.T) *fakeDevTo {
	f := &fakeDevTo{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := devtoAPI
	devtoAPI = srv.URL
	t.Cleanup(func() { devtoAPI = old })
	return f
}

func TestDevToConnect(t *testing.T) {
	newFakeDevTo(t)
	d, _ := Get("devto")
	if d.Status != StatusAvailable {
		t.Fatal("devto is not available")
	}
	c, p, err := d.Connect(context.Background(), map[string]string{"api_key": "good"})
	if err != nil {
		t.Fatal(err)
	}
	if c["api_key"] != "good" || c["user_id"] != "42" || p.Handle != "@janedoe" || p.ProfileURL != "https://dev.to/janedoe" {
		t.Fatalf("creds %v profile %+v", c, p)
	}
	_, _, err = d.Connect(context.Background(), map[string]string{"api_key": "bad"})
	blogWantKind(t, err, ErrReauth, "API key")
}

func TestDevToPublish(t *testing.T) {
	f := newFakeDevTo(t)
	d, _ := Get("devto")
	creds := Credentials{"api_key": "good"}
	s := Settings{"title": "Hello", "tags": "Web Dev, go!", "canonical_url": "https://example.com/hello", "series": "Basics"}
	res, err := d.Publish(context.Background(), creds, PostContent{Text: "# Hi"}, s)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "101" || res.URL != "https://dev.to/janedoe/hello-1a2b" {
		t.Fatalf("result %+v", res)
	}
	tags, _ := f.article["tags"].([]any)
	if f.article["published"] != true || f.article["body_markdown"] != "# Hi" || len(tags) != 2 || tags[0] != "webdev" || tags[1] != "go" ||
		f.article["canonical_url"] != "https://example.com/hello" || f.article["series"] != "Basics" || f.article["main_image"] != nil {
		t.Fatalf("article %v", f.article)
	}
	if f.accept != "application/vnd.forem.api-v1+json" {
		t.Fatalf("accept %q", f.accept)
	}

	// A cover from a public link; none without one.
	pic := MediaFile{Name: "c.png", MimeType: "image/png", SignedURL: "https://bucket.example/c.png?sig=1"}
	if _, err := d.Publish(context.Background(), creds, PostContent{Text: "x", Media: []MediaFile{pic}}, s); err != nil {
		t.Fatal(err)
	}
	if f.article["main_image"] != pic.SignedURL {
		t.Fatalf("main_image %v", f.article["main_image"])
	}
	pic.SignedURL = ""
	_, err = d.Publish(context.Background(), creds, PostContent{Text: "x", Media: []MediaFile{pic}}, s)
	blogWantKind(t, err, ErrRejected, "cover picture")
}

func TestDevToFailures(t *testing.T) {
	f := newFakeDevTo(t)
	d, _ := Get("devto")
	creds := Credentials{"api_key": "good"}
	post, s := PostContent{Text: "Body"}, Settings{"title": "Hello"}

	// The same article again: when the author's article is there, that is the post.
	f.postCode, f.postBody = http.StatusUnprocessableEntity, `{"error":"Body markdown has already been taken","status":422}`
	f.existing = "Body"
	res, err := d.Publish(context.Background(), creds, post, s)
	if err != nil || res.ExternalID != "99" {
		t.Fatalf("duplicate: %+v %v", res, err)
	}
	f.existing = "Something else"
	_, err = d.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "already published")

	f.postCode, f.postBody = http.StatusUnprocessableEntity, `{"error":"Tag list exceed the maximum of 4 tags","status":422}`
	_, err = d.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "maximum of 4")

	f.postCode, f.postBody = http.StatusTooManyRequests, `{"error":"Rate limit reached, try again in 30 seconds","status":429}`
	_, err = d.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "rate limit")

	f.postCode, f.postBody = http.StatusServiceUnavailable, ``
	_, err = d.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "unavailable")

	f.postCode, f.drop = 0, true
	_, err = d.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "may have published")

	_, err = d.Publish(context.Background(), Credentials{"api_key": "revoked"}, post, s)
	blogWantKind(t, err, ErrReauth, "")
}

func TestDevToValidate(t *testing.T) {
	d, _ := Get("devto")
	blogWantInvalid(t, d.Validate(PostContent{Text: "x"}, Settings{}), "title")
	blogWantInvalid(t, d.Validate(PostContent{Text: "x"}, Settings{"title": strings.Repeat("a", 129)}), "title")
	blogWantInvalid(t, d.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "a,b,c,d,e"}), "tags")
	blogWantInvalid(t, d.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "!!"}), "tags")
	blogWantInvalid(t, d.Validate(PostContent{Text: "x", Media: []MediaFile{{MimeType: "video/mp4"}}}, Settings{"title": "T"}), "media")
	if err := d.Validate(PostContent{Text: "x"}, Settings{"title": "T", "tags": "go, web dev"}); err != nil {
		t.Fatal(err)
	}
}

func TestDevToNumbersAndComments(t *testing.T) {
	newFakeDevTo(t)
	d, _ := Get("devto")
	c := Credentials{"api_key": "good", "username": "janedoe"}
	m, err := d.Provider.(MetricsReader).Metrics(context.Background(), c, "101")
	if err != nil || *m.Likes != 7 || *m.Replies != 2 || m.Impressions == nil || *m.Impressions != 250 {
		t.Fatalf("metrics %+v %v", m, err)
	}
	// Deleted (or unpublished) on Dev.to: gone.
	if _, err := d.Provider.(MetricsReader).Metrics(context.Background(), c, "555"); !errors.Is(err, ErrGone) {
		t.Fatalf("deleted article: %v", err)
	}
	rs, err := d.Provider.(ResponseReader).Responses(context.Background(), c, "101", time.Time{})
	if err != nil || len(rs) != 2 {
		t.Fatalf("responses %+v %v", rs, err)
	}
	if rs[0].ID != "c1" || rs[0].Text != "Nice & clear" || rs[0].AuthorHandle != "@ann" || rs[0].Own || rs[0].URL != "https://dev.to/ann/comment/c1" {
		t.Fatalf("first %+v", rs[0])
	}
	if rs[1].ParentID != "c1" || !rs[1].Own {
		t.Fatalf("reply %+v", rs[1])
	}
	if rs, _ := d.Provider.(ResponseReader).Responses(context.Background(), c, "101", time.Date(2026, 11, 3, 10, 30, 0, 0, time.UTC)); len(rs) != 1 || rs[0].ID != "c2" {
		t.Fatalf("since: %+v", rs)
	}
}
