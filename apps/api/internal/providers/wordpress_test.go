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

// fakeWordPress answers like a WordPress site's REST API, at /wp-json or (plain) at ?rest_route= only.
type fakeWordPress struct {
	mu          sync.Mutex
	plain       bool
	post        map[string]any
	media       string // the uploaded picture's bytes
	disposition string
	madeTags    []string
	postCode    int
	postBody    string
	drop        bool
	srv         *httptest.Server
}

func (f *fakeWordPress) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	route := strings.TrimPrefix(r.URL.Path, "/wp-json")
	if f.plain {
		if r.URL.Path != "/" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<html>Not found</html>`))
			return
		}
		route = r.URL.Query().Get("rest_route")
	}
	w.Header().Set("Content-Type", "application/json")
	if u, p, ok := r.BasicAuth(); !ok || u != "jane" || p != "abcd efgh ijkl" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"incorrect_password","message":"The provided password is an invalid application password.","data":{"status":401}}`))
		return
	}
	switch {
	case route == "/wp/v2/users/me":
		if r.URL.Query().Get("context") != "edit" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"id":3,"name":"Jane &amp; Co","slug":"jane","link":"` + f.srv.URL + `/author/jane","avatar_urls":{"96":"https://secure.gravatar.com/jane"},"capabilities":{"edit_posts":true,"publish_posts":true}}`))
	case route == "/wp/v2/categories" && r.Method == http.MethodGet:
		if strings.EqualFold(r.URL.Query().Get("search"), "news") {
			_, _ = w.Write([]byte(`[{"id":5,"name":"News &amp; more"},{"id":4,"name":"News"}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	case route == "/wp/v2/tags" && r.Method == http.MethodGet:
		if strings.EqualFold(r.URL.Query().Get("search"), "running") {
			_, _ = w.Write([]byte(`[{"id":11,"name":"Running"}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	case route == "/wp/v2/tags" && r.Method == http.MethodPost:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.madeTags = append(f.madeTags, body["name"])
		if body["name"] == "Go" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"term_exists","message":"A term with the name provided already exists.","data":{"status":400,"term_id":13}}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":12,"name":"` + body["name"] + `"}`))
	case route == "/wp/v2/media":
		b, _ := io.ReadAll(r.Body)
		f.media, f.disposition = string(b), r.Header.Get("Content-Disposition")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":77,"source_url":"https://blog.example.com/wp-content/uploads/pic.png"}`))
	case route == "/wp/v2/posts":
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
		_, _ = w.Write([]byte(`{"id":120,"link":"https://blog.example.com/2026/10/hello/"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"rest_no_route","message":"No route was found matching the URL and request method.","data":{"status":404}}`))
	}
}

func newFakeWordPress(t *testing.T) *fakeWordPress {
	f := &fakeWordPress{}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func TestWordPressConnect(t *testing.T) {
	f := newFakeWordPress(t)
	wp, _ := Get("wordpress")
	if wp.Status != StatusAvailable {
		t.Fatal("wordpress is not available")
	}
	fields := map[string]string{"site_url": f.srv.URL + "/", "username": "jane", "app_password": "abcd efgh ijkl"}
	c, p, err := wp.Connect(context.Background(), fields)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(f.srv.URL, "http://")
	if c["site_url"] != f.srv.URL || c["rest"] != "" || p.Handle != "jane@"+host || p.Name != "Jane & Co" || p.AvatarURL == "" {
		t.Fatalf("creds %v profile %+v", c, p)
	}
	if _, _, err := wp.Connect(context.Background(), map[string]string(c)); err != nil {
		t.Fatal(err)
	}

	// Plain permalinks: the API answers at ?rest_route= only.
	f.plain = true
	c, _, err = wp.Connect(context.Background(), fields)
	if err != nil || c["rest"] != "plain" {
		t.Fatalf("plain: %v %v", c, err)
	}

	fields["app_password"] = "wrong"
	_, _, err = wp.Connect(context.Background(), fields)
	blogWantKind(t, err, ErrReauth, "application password")
	_, _, err = wp.Connect(context.Background(), map[string]string{"site_url": "ftp://x", "username": "jane", "app_password": "x"})
	blogWantInvalid(t, err, "site_url")
}

func TestWordPressPublish(t *testing.T) {
	for _, plain := range []bool{false, true} {
		f := newFakeWordPress(t)
		f.plain = plain
		wp, _ := Get("wordpress")
		creds := Credentials{"site_url": f.srv.URL, "username": "jane", "app_password": "abcd efgh ijkl"}
		if plain {
			creds["rest"] = "plain"
		}
		s := Settings{"title": "Hello", "status": "draft", "categories": "news", "tags": "running, Marathon, Go"}
		post := PostContent{Text: "Hello **world**", Media: []MediaFile{blogMemFile("pic.png", "image/png", "PNGDATA")}}
		if err := wp.Validate(post, s); err != nil {
			t.Fatal(err)
		}
		res, err := wp.Publish(context.Background(), creds, post, s)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExternalID != "120" || res.URL != "https://blog.example.com/2026/10/hello/" {
			t.Fatalf("result %+v", res)
		}
		p := f.post
		if p["title"] != "Hello" || p["status"] != "draft" || !strings.Contains(p["content"].(string), "<strong>world</strong>") ||
			p["featured_media"] != float64(77) {
			t.Fatalf("post %v", p)
		}
		cats, tags := p["categories"].([]any), p["tags"].([]any)
		if len(cats) != 1 || cats[0] != float64(4) || len(tags) != 3 || tags[0] != float64(11) || tags[1] != float64(12) || tags[2] != float64(13) {
			t.Fatalf("categories %v tags %v", cats, tags)
		}
		if f.media != "PNGDATA" || f.disposition != `attachment; filename="pic.png"` || strings.Join(f.madeTags, ",") != "Marathon,Go" {
			t.Fatalf("media %q %q made %v", f.media, f.disposition, f.madeTags)
		}
	}
}

func TestWordPressFailures(t *testing.T) {
	f := newFakeWordPress(t)
	wp, _ := Get("wordpress")
	creds := Credentials{"site_url": f.srv.URL, "username": "jane", "app_password": "abcd efgh ijkl"}
	post, s := PostContent{Text: "Body"}, Settings{"title": "Hello"}

	res, err := wp.Publish(context.Background(), creds, post, s)
	if err != nil || f.post["status"] != "publish" || res.ExternalID != "120" {
		t.Fatalf("default status: %v %v", f.post, err)
	}

	_, err = wp.Publish(context.Background(), creds, post, Settings{"title": "Hello", "categories": "Missing"})
	blogWantKind(t, err, ErrRejected, "no category named Missing")

	f.postCode, f.postBody = http.StatusForbidden, `{"code":"rest_cannot_create","message":"Sorry, you are not allowed to create posts as this user.","data":{"status":403}}`
	_, err = wp.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "not allowed")

	f.postCode, f.postBody = http.StatusTooManyRequests, ``
	_, err = wp.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "rate limit")

	f.postCode, f.postBody = http.StatusInternalServerError, `{"code":"internal_server_error","message":"There has been a critical error.","data":{"status":500}}`
	_, err = wp.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "unavailable")

	f.postCode, f.drop = 0, true
	_, err = wp.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "may have published")

	creds["app_password"] = "revoked"
	_, err = wp.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrReauth, "")
}

func TestWordPressValidate(t *testing.T) {
	wp, _ := Get("wordpress")
	blogWantInvalid(t, wp.Validate(PostContent{Text: "x"}, Settings{}), "title")
	blogWantInvalid(t, wp.Validate(PostContent{Text: "x"}, Settings{"title": "T", "status": "private"}), "status")
	blogWantInvalid(t, wp.Validate(PostContent{Text: "x", Media: []MediaFile{{Name: "v.mp4", MimeType: "video/mp4"}}}, Settings{"title": "T"}), "media")
	if err := wp.Validate(PostContent{Text: "x"}, Settings{"title": "T", "status": "pending"}); err != nil {
		t.Fatal(err)
	}
}
