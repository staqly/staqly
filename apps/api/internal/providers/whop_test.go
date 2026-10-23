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
	"time"
)

// fakeWhop answers like api.whop.com/api/v1 (and the file storage it hands out) for the calls the provider makes.
type fakeWhop struct {
	mu       sync.Mutex
	srv      *httptest.Server
	forums   string // the data of GET /forums
	posts    []map[string]any
	puts     map[string]string // upload path -> body
	putHdr   http.Header
	polls    int
	fileFail bool
	postCode int
	postBody string
	drop     bool
}

func (f *fakeWhop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	raw, _ := io.ReadAll(r.Body)
	if strings.HasPrefix(r.URL.Path, "/storage/") {
		f.puts[r.URL.Path] = string(raw)
		f.putHdr = r.Header.Clone()
		return
	}
	if r.Header.Get("Authorization") == "Bearer revoked" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"unauthorized","message":"Invalid API key"}}`))
		return
	}
	switch {
	case r.URL.Path == "/accounts/me":
		_, _ = w.Write([]byte(`{"id":"biz_1","title":"Acme Studio","route":"acme-studio","logo_url":"https://example.com/logo.png"}`))
	case r.URL.Path == "/forums":
		if r.URL.Query().Get("account_id") != "biz_1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[` + f.forums + `],"page_info":{"has_next_page":false}}`))
	case r.URL.Path == "/files" && r.Method == http.MethodPost:
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"id":"file_` + body["filename"] + `","upload_status":"pending","upload_url":"` + f.srv.URL + `/storage/` + body["filename"] + `?sig=secret","upload_headers":{"x-amz-acl":"private"}}`))
	case strings.HasPrefix(r.URL.Path, "/files/"):
		f.polls++
		switch {
		case f.fileFail:
			_, _ = w.Write([]byte(`{"upload_status":"failed"}`))
		case f.polls%2 == 1:
			_, _ = w.Write([]byte(`{"upload_status":"processing"}`))
		default:
			_, _ = w.Write([]byte(`{"upload_status":"ready"}`))
		}
	case r.URL.Path == "/forum_posts":
		if f.drop {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		post := map[string]any{}
		_ = json.Unmarshal(raw, &post)
		f.posts = append(f.posts, post)
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
			return
		}
		_, _ = w.Write([]byte(`{"id":"post_` + string(rune('0'+len(f.posts))) + `"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const whopTwoForums = `{"id":"forum_1","experience":{"id":"exp_A","name":"General"}},{"id":"forum_2","experience":{"id":"exp_B","name":"Announcements"}}`

func useFakeWhop(t *testing.T) *fakeWhop {
	t.Helper()
	f := &fakeWhop{puts: map[string]string{}, forums: whopTwoForums}
	f.srv = httptest.NewServer(f)
	api, poll := whopAPI, whopPoll
	whopAPI, whopPoll = f.srv.URL, time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		whopAPI, whopPoll = api, poll
	})
	return f
}

func TestWhopConnect(t *testing.T) {
	f := useFakeWhop(t)
	wh, _ := Get("whop")
	if wh.Status != StatusAvailable || wh.Auth() != AuthToken {
		t.Fatalf("whop is %q/%q", wh.Status, wh.Auth())
	}
	ctx := context.Background()
	// Two forums and none named: list them.
	var ve *ValidationError
	_, _, err := wh.Connect(ctx, map[string]string{"api_key": "key"})
	if !errors.As(err, &ve) || ve.Field != "experience_id" || !strings.Contains(ve.Msg, "General (exp_A)") || !strings.Contains(ve.Msg, "Announcements (exp_B)") {
		t.Fatalf("no forum named: %v", err)
	}
	if _, _, err := wh.Connect(ctx, map[string]string{"api_key": "key", "experience_id": "exp_Z"}); !errors.As(err, &ve) || ve.Field != "experience_id" {
		t.Fatalf("unknown forum: %v", err)
	}
	creds, profile, err := wh.Connect(ctx, map[string]string{"api_key": "key", "experience_id": "exp_B"})
	if err != nil {
		t.Fatal(err)
	}
	if creds["experience_id"] != "exp_B" || creds["account_id"] != "biz_1" || creds["api_key"] != "key" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "acme-studio/exp_B" || profile.Name != "Acme Studio: Announcements" || profile.ProfileURL != "https://whop.com/acme-studio/" {
		t.Fatalf("profile %+v", profile)
	}
	// One forum: it is used.
	f.forums = `{"id":"forum_1","experience":{"id":"exp_A","name":"General"}}`
	if creds, _, err = wh.Connect(ctx, map[string]string{"api_key": "key"}); err != nil || creds["experience_id"] != "exp_A" {
		t.Fatalf("one forum: %v %v", creds, err)
	}
	// None: say what to add.
	f.forums = ``
	if _, _, err := wh.Connect(ctx, map[string]string{"api_key": "key"}); !errors.As(err, &ve) || !strings.Contains(ve.Msg, "no forum") {
		t.Fatalf("no forums: %v", err)
	}
	if _, _, err := wh.Connect(ctx, map[string]string{"api_key": "revoked"}); !errors.As(err, &ve) || ve.Field != "api_key" {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := wh.Refresh(ctx, App{}, Credentials{"api_key": "revoked"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("refresh with a revoked key: %v", err)
	}
}

// whopFile is an attachment served from memory.
func whopFile(name, mime, body string) MediaFile {
	return MediaFile{Name: name, MimeType: mime, Size: int64(len(body)), Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}}
}

func whopCreds() Credentials {
	return Credentials{"api_key": "key", "account_id": "biz_1", "route": "acme-studio", "experience_id": "exp_A"}
}

func TestWhopPublish(t *testing.T) {
	f := useFakeWhop(t)
	wh, _ := Get("whop")
	ctx := context.Background()
	res, err := wh.Publish(ctx, whopCreds(), PostContent{Text: "Weekly **update**"}, Settings{"title": "Week 42", "notify": "true", "pinned": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "post_1" || res.URL != "https://whop.com/acme-studio/" {
		t.Fatalf("result %+v", res)
	}
	p := f.posts[0]
	if p["experience_id"] != "exp_A" || p["content"] != "Weekly **update**" || p["title"] != "Week 42" || p["is_mention"] != true || p["pinned"] != nil || p["attachments"] != nil {
		t.Fatalf("post %v", p)
	}

	// Pictures and a video are uploaded first and attached.
	media := []MediaFile{whopFile("a.png", "image/png", "png!"), whopFile("b.mp4", "video/mp4", "video")}
	if _, err := wh.Publish(ctx, whopCreds(), PostContent{Text: "files", Media: media}, nil); err != nil {
		t.Fatal(err)
	}
	if f.puts["/storage/a.png"] != "png!" || f.puts["/storage/b.mp4"] != "video" || f.putHdr.Get("X-Amz-Acl") != "private" || f.putHdr.Get("Content-Type") != "video/mp4" {
		t.Fatalf("uploads %v %v", f.puts, f.putHdr)
	}
	att, _ := f.posts[1]["attachments"].([]any)
	if len(att) != 2 || att[0].(map[string]any)["id"] != "file_a.png" {
		t.Fatalf("attachments %v", f.posts[1])
	}

	// A file Whop could not process: refused, and no post is made.
	f.fileFail = true
	if _, err := wh.Publish(ctx, whopCreds(), PostContent{Media: media[:1]}, nil); !errors.Is(err, ErrRejected) || len(f.posts) != 2 {
		t.Fatalf("failed file: %v", err)
	}
	f.fileFail = false

	for _, tc := range []struct {
		code int
		body string
		kind error
	}{
		{http.StatusForbidden, `{"error":{"type":"forbidden","message":"Missing permission forum:post:create"}}`, ErrRejected},
		{http.StatusUnprocessableEntity, `{"error":{"message":"Content is too long"}}`, ErrRejected},
		{http.StatusTooManyRequests, `{"error":{"message":"Too many requests"}}`, nil},
		{http.StatusInternalServerError, `oops`, nil},
	} {
		f.postCode, f.postBody = tc.code, tc.body
		_, err := wh.Publish(ctx, whopCreds(), PostContent{Text: "x"}, nil)
		if err == nil || (tc.kind != nil && !errors.Is(err, tc.kind)) || (tc.kind == nil && (errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth))) {
			t.Errorf("%d: %v", tc.code, err)
		}
	}
	f.postCode = 0
	c := whopCreds()
	c["api_key"] = "revoked"
	if _, err := wh.Publish(ctx, c, PostContent{Text: "x"}, nil); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
	f.drop = true
	if _, err := wh.Publish(ctx, whopCreds(), PostContent{Text: "x"}, nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestWhopReply(t *testing.T) {
	f := useFakeWhop(t)
	wh, _ := Get("whop")
	res, err := wh.Provider.(Replier).Reply(context.Background(), whopCreds(), "post_9", PostContent{Text: "a comment"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "post_1" || f.posts[0]["parent_id"] != "post_9" || f.posts[0]["experience_id"] != "exp_A" || f.posts[0]["content"] != "a comment" {
		t.Fatalf("comment %+v %v", res, f.posts)
	}
}

func TestWhopValidate(t *testing.T) {
	wh, _ := Get("whop")
	if err := wh.Validate(PostContent{Text: "hi"}, Settings{"notify": "true"}); err != nil {
		t.Fatal(err)
	}
	var ve *ValidationError
	if err := wh.Validate(PostContent{Text: strings.Repeat("a", 5001)}, nil); !errors.As(err, &ve) || ve.Field != "content" {
		t.Fatalf("too long: %v", err)
	}
	if err := wh.Validate(PostContent{Text: "hi"}, Settings{"notify": "yes"}); !errors.As(err, &ve) || ve.Field != "notify" {
		t.Fatalf("bad boolean: %v", err)
	}
	if err := wh.Validate(PostContent{Text: "hi", Media: make([]MediaFile, 5)}, nil); !errors.As(err, &ve) || ve.Field != "media" {
		t.Fatalf("too many files: %v", err)
	}
}
