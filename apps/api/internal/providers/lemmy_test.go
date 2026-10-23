package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeLemmy answers like a Lemmy 0.19 server for the calls the provider makes.
type fakeLemmy struct {
	mu        sync.Mutex
	posts     []map[string]any
	comments  []map[string]any
	pictures  int
	resolved  bool // resolve_object was asked
	postCode  int  // answer to POST /api/v3/post (0 = 200)
	postBody  string
	dropPost  bool
	needsTOTP bool
	srv       *httptest.Server
}

func (f *fakeLemmy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	raw, _ := io.ReadAll(r.Body)
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	if r.URL.Path != "/api/v3/user/login" && r.Header.Get("Authorization") != "Bearer jwt-1" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"not_logged_in"}`))
		return
	}
	switch r.URL.Path {
	case "/api/v3/user/login":
		switch {
		case in["password"] != "secret":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"incorrect_login"}`))
		case f.needsTOTP && in["totp_2fa_token"] != "123456":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"missing_totp_token"}`))
		default:
			_, _ = w.Write([]byte(`{"jwt":"jwt-1","registration_created":false,"verify_email_sent":false}`))
		}
	case "/api/v3/site":
		_, _ = w.Write([]byte(`{"my_user":{"local_user_view":{"person":{"id":7,"name":"janedoe","display_name":"Jane Doe","avatar":"https://example.com/a.png","actor_id":"https://lemmy.example/u/janedoe"}}}}`))
	case "/api/v3/community":
		if r.URL.Query().Get("name") != "technology@lemmy.example" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"couldnt_find_community"}`))
			return
		}
		_, _ = w.Write([]byte(`{"community_view":{"community":{"id":42,"name":"technology"}}}`))
	case "/api/v3/resolve_object":
		f.resolved = true
		if r.URL.Query().Get("q") != "!remote@other.example" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"couldnt_find_object"}`))
			return
		}
		_, _ = w.Write([]byte(`{"community":{"community":{"id":43}}}`))
	case "/pictrs/image":
		f.pictures++
		// The body was read above: check the form field by its raw name.
		if !strings.Contains(string(raw), `name="images[]"`) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"msg":"ok","files":[{"file":"abc.png","delete_token":"d"}]}`))
	case "/api/v3/post":
		f.posts = append(f.posts, in)
		if f.dropPost {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
			return
		}
		_, _ = w.Write([]byte(`{"post_view":{"post":{"id":100,"name":"t","ap_id":"https://lemmy.example/post/100"}}}`))
	case "/api/v3/comment":
		f.comments = append(f.comments, in)
		_, _ = w.Write([]byte(`{"comment_view":{"comment":{"id":` + jsonInt(200+len(f.comments)) + `}}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeLemmy(t *testing.T) (*fakeLemmy, Credentials) {
	t.Helper()
	f := &fakeLemmy{}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f, Credentials{"instance": f.srv.URL, "jwt": "jwt-1"}
}

func TestLemmyConnect(t *testing.T) {
	f, _ := useFakeLemmy(t)
	e, _ := Get("lemmy")
	if e.Status != StatusAvailable {
		t.Fatalf("lemmy is %q", e.Status)
	}
	creds, prof, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL, "username": "janedoe", "password": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if creds["jwt"] != "jwt-1" || creds["password"] != "" || prof.Name != "Jane Doe" || !strings.HasPrefix(prof.Handle, "@janedoe@") {
		t.Fatalf("creds %v profile %+v", creds, prof)
	}
	// The channel check connects again with what was stored: the token alone.
	if _, _, err := e.Connect(context.Background(), map[string]string(creds)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL, "jwt": "old"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("an old token: %v", err)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL, "username": "janedoe", "password": "nope"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("a wrong password: %v", err)
	}
	f.needsTOTP = true
	if _, _, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL, "username": "janedoe", "password": "secret"}); err == nil || !strings.Contains(err.Error(), "two-factor") {
		t.Fatalf("2FA: %v", err)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string{"instance": f.srv.URL, "username": "janedoe", "password": "secret", "totp": "123456"}); err != nil {
		t.Fatal(err)
	}
}

func TestLemmyPublish(t *testing.T) {
	f, creds := useFakeLemmy(t)
	e, _ := Get("lemmy")
	img := MediaFile{Name: "a.png", MimeType: "image/png", Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("png")), nil
	}}
	res, err := e.Publish(context.Background(), creds, PostContent{Text: "Body", Media: []MediaFile{img}},
		Settings{"title": "Title", "community": "!technology@lemmy.example"})
	if err != nil {
		t.Fatal(err)
	}
	p := f.posts[0]
	if res.ExternalID != "100" || res.URL != f.srv.URL+"/post/100" || p["name"] != "Title" || p["community_id"] != 42.0 ||
		p["body"] != "Body" || p["url"] != f.srv.URL+"/pictrs/image/abc.png" {
		t.Fatalf("result %+v post %v", res, p)
	}
	// A link post keeps the picture as its thumbnail; an unknown remote community is fetched first.
	_, err = e.Publish(context.Background(), creds, PostContent{Media: []MediaFile{img}},
		Settings{"title": "T", "community": "remote@other.example", "link": "https://example.com/a"})
	if err != nil || !f.resolved || f.posts[1]["community_id"] != 43.0 || f.posts[1]["url"] != "https://example.com/a" || f.posts[1]["custom_thumbnail"] == nil {
		t.Fatalf("link post: %v %v", err, f.posts[1])
	}
	if _, err := e.Publish(context.Background(), creds, PostContent{}, Settings{"title": "T", "community": "nothing@nowhere.example"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("unknown community: %v", err)
	}

	// Comments chain under the post.
	c1, err := Lemmy{}.Reply(context.Background(), creds, res.ExternalID, PostContent{Text: "first"}, nil)
	if err != nil || c1.ExternalID != "comment:100:201" || f.comments[0]["post_id"] != 100.0 || f.comments[0]["parent_id"] != nil {
		t.Fatalf("comment: %v %+v %v", err, c1, f.comments)
	}
	c2, err := Lemmy{}.Reply(context.Background(), creds, c1.ExternalID, PostContent{Text: "second"}, nil)
	if err != nil || c2.ExternalID != "comment:100:202" || f.comments[1]["parent_id"] != 201.0 {
		t.Fatalf("second comment: %v %+v %v", err, c2, f.comments)
	}
}

func TestLemmyErrors(t *testing.T) {
	f, creds := useFakeLemmy(t)
	e, _ := Get("lemmy")
	s := Settings{"title": "T", "community": "technology@lemmy.example"}
	for _, c := range []struct {
		code int
		body string
		kind error
	}{
		{400, `{"error":"only_mods_can_post_in_community"}`, ErrRejected},
		{400, `{"error":"rate_limit_error"}`, nil},
		{502, `bad gateway`, nil},
		{401, `{"error":"not_logged_in"}`, ErrReauth},
	} {
		f.postCode, f.postBody = c.code, c.body
		_, err := e.Publish(context.Background(), creds, PostContent{}, s)
		var fail *Failure
		if !errors.As(err, &fail) || fail.Kind != c.kind {
			t.Fatalf("%s: %v", c.body, err)
		}
	}
	f.postCode = 0
	f.dropPost = true
	if _, err := e.Publish(context.Background(), creds, PostContent{}, s); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("a lost answer: %v", err)
	}
}

func TestLemmyValidate(t *testing.T) {
	e, _ := Get("lemmy")
	ok := Settings{"title": "T", "community": "technology@lemmy.world"}
	for _, c := range []struct {
		p    PostContent
		s    Settings
		pass bool
	}{
		{PostContent{Text: "hi"}, ok, true},
		{PostContent{}, ok, true}, // a title alone is a post
		{PostContent{Text: "hi"}, Settings{"community": "technology@lemmy.world"}, false},
		{PostContent{Text: "hi"}, Settings{"title": "T"}, false},
		{PostContent{Text: "hi"}, Settings{"title": "T", "community": "bad name"}, false},
		{PostContent{Text: "hi"}, Settings{"title": strings.Repeat("t", 201), "community": "c"}, false},
		{PostContent{Text: "hi", Media: []MediaFile{{Name: "v.mp4", MimeType: "video/mp4"}}}, ok, false},
		{PostContent{Text: "hi"}, Settings{"title": "T", "community": "c", "link": "not a link"}, false},
	} {
		if err := e.Validate(c.p, c.s); (err == nil) != c.pass {
			t.Errorf("%v: %v", c.s, err)
		}
	}
}
