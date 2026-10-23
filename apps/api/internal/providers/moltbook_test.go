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

// fakeMoltbook answers like www.moltbook.com/api/v1 for the calls the provider makes.
type fakeMoltbook struct {
	mu       sync.Mutex
	auth     string
	post     map[string]any
	comment  map[string]any
	postCode int    // status for POST /posts (0 = 201)
	postBody string // body for POST /posts (default: a verified post)
	drop     bool   // close the connection on POST /posts
}

func (f *fakeMoltbook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = r.Header.Get("Authorization")
	w.Header().Set("Content-Type", "application/json")
	raw, _ := io.ReadAll(r.Body)
	if f.auth == "Bearer revoked" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":"Invalid API key","hint":"Check your key"}`))
		return
	}
	switch {
	case r.URL.Path == "/agents/me":
		_, _ = w.Write([]byte(`{"success":true,"agent":{"id":"a1","name":"janebot","display_name":"Jane's Bot","avatar_url":"https://example.com/a.png"}}`))
	case r.URL.Path == "/posts" && r.Method == http.MethodPost:
		if f.drop {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		f.post = map[string]any{}
		_ = json.Unmarshal(raw, &f.post)
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		body := f.postBody
		if body == "" {
			body = `{"success":true,"post":{"id":"p-123","title":"Hello"}}`
		}
		_, _ = w.Write([]byte(body))
	case strings.HasPrefix(r.URL.Path, "/posts/") && strings.HasSuffix(r.URL.Path, "/comments"):
		f.comment = map[string]any{"_path": r.URL.Path}
		_ = json.Unmarshal(raw, &f.comment)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"comment":{"id":"c-9"}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeMoltbook(t *testing.T) *fakeMoltbook {
	t.Helper()
	f := &fakeMoltbook{}
	srv := httptest.NewServer(f)
	api := moltbookAPI
	moltbookAPI = srv.URL
	t.Cleanup(func() {
		srv.Close()
		moltbookAPI = api
	})
	return f
}

func TestMoltbookConnect(t *testing.T) {
	f := useFakeMoltbook(t)
	mb, _ := Get("moltbook")
	if mb.Status != StatusAvailable || mb.Auth() != AuthToken {
		t.Fatalf("moltbook is %q/%q", mb.Status, mb.Auth())
	}
	creds, profile, err := mb.Connect(context.Background(), map[string]string{"api_key": " moltbook_sk_1 "})
	if err != nil {
		t.Fatal(err)
	}
	if f.auth != "Bearer moltbook_sk_1" || creds["api_key"] != "moltbook_sk_1" {
		t.Fatalf("auth %q creds %v", f.auth, creds)
	}
	if profile.Handle != "u/janebot" || profile.Name != "Jane's Bot" || profile.ProfileURL != "https://www.moltbook.com/u/janebot" {
		t.Fatalf("profile %+v", profile)
	}
	var ve *ValidationError
	if _, _, err := mb.Connect(context.Background(), map[string]string{"api_key": "revoked"}); !errors.As(err, &ve) || ve.Field != "api_key" {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := mb.Refresh(context.Background(), App{}, Credentials{"api_key": "revoked"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("refresh with a revoked key: %v", err)
	}
}

func TestMoltbookPublish(t *testing.T) {
	f := useFakeMoltbook(t)
	mb, _ := Get("moltbook")
	c := Credentials{"api_key": "k"}
	res, err := mb.Publish(context.Background(), c, PostContent{Text: "Agents, assemble"}, Settings{"title": "Hello", "submolt": "m/AgentOps"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "p-123" || res.URL != "https://www.moltbook.com/post/p-123" {
		t.Fatalf("result %+v", res)
	}
	if f.post["submolt_name"] != "agentops" || f.post["title"] != "Hello" || f.post["content"] != "Agents, assemble" || f.post["type"] != "text" {
		t.Fatalf("post %v", f.post)
	}

	// A link post; no submolt set goes to general.
	if _, err := mb.Publish(context.Background(), c, PostContent{}, Settings{"title": "Read this", "link": "https://example.com/a"}); err != nil {
		t.Fatal(err)
	}
	if f.post["type"] != "link" || f.post["url"] != "https://example.com/a" || f.post["submolt_name"] != "general" || f.post["content"] != nil {
		t.Fatalf("link post %v", f.post)
	}

	// Held for a verification challenge: staqly cannot answer it.
	f.postBody = `{"success":true,"post":{"id":"p-2","verification_status":"pending","verification":{"verification_code":"moltbook_verify_abc","challenge_text":"tW0 lObStErS...","expires_at":"2026-10-23T10:05:00Z"}}}`
	if _, err := mb.Publish(context.Background(), c, PostContent{Text: "x"}, Settings{"title": "t"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "moltbook_verify_abc") {
		t.Fatalf("held post: %v", err)
	}
	f.postBody = ""

	// Rate limit: retried.
	f.postCode, f.postBody = http.StatusTooManyRequests, `{"success":false,"error":"Slow down","retry_after_seconds":1500}`
	err = func() error {
		_, err := mb.Publish(context.Background(), c, PostContent{Text: "x"}, Settings{"title": "t"})
		return err
	}()
	if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) || !strings.Contains(err.Error(), "1500 seconds") {
		t.Fatalf("rate limit: %v", err)
	}
	// Unknown submolt: refused.
	f.postCode, f.postBody = http.StatusNotFound, `{"success":false,"error":"Submolt not found"}`
	if _, err := mb.Publish(context.Background(), c, PostContent{Text: "x"}, Settings{"title": "t", "submolt": "nope"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("unknown submolt: %v", err)
	}
	// Outage: retried.
	f.postCode, f.postBody = http.StatusBadGateway, `bad gateway`
	if _, err := mb.Publish(context.Background(), c, PostContent{Text: "x"}, Settings{"title": "t"}); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("outage: %v", err)
	}
	f.postCode, f.postBody = 0, ""
	// Revoked key: reconnect.
	if _, err := mb.Publish(context.Background(), Credentials{"api_key": "revoked"}, PostContent{Text: "x"}, Settings{"title": "t"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
	// Lost answer: not sent again.
	f.drop = true
	if _, err := mb.Publish(context.Background(), c, PostContent{Text: "x"}, Settings{"title": "t"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestMoltbookReply(t *testing.T) {
	f := useFakeMoltbook(t)
	mb, _ := Get("moltbook")
	r := mb.Provider.(Replier)
	res, err := r.Reply(context.Background(), Credentials{"api_key": "k"}, "p-123", PostContent{Text: "first"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "comment:p-123:c-9" || f.comment["_path"] != "/posts/p-123/comments" || f.comment["parent_id"] != nil {
		t.Fatalf("first comment %+v %v", res, f.comment)
	}
	if _, err := r.Reply(context.Background(), Credentials{"api_key": "k"}, res.ExternalID, PostContent{Text: "second"}, nil); err != nil {
		t.Fatal(err)
	}
	if f.comment["_path"] != "/posts/p-123/comments" || f.comment["parent_id"] != "c-9" || f.comment["content"] != "second" {
		t.Fatalf("second comment %v", f.comment)
	}
}

func TestMoltbookValidate(t *testing.T) {
	mb, _ := Get("moltbook")
	for _, tc := range []struct {
		name  string
		p     PostContent
		s     Settings
		field string
	}{
		{"ok", PostContent{Text: "hi"}, Settings{"title": "Hello"}, ""},
		{"no title", PostContent{Text: "hi"}, Settings{}, "title"},
		{"long title", PostContent{Text: "hi"}, Settings{"title": strings.Repeat("a", 301)}, "title"},
		{"media", PostContent{Text: "hi", Media: []MediaFile{{MimeType: "image/png"}}}, Settings{"title": "t"}, "media"},
		{"bad submolt", PostContent{Text: "hi"}, Settings{"title": "t", "submolt": "two words"}, "submolt"},
		{"bad link", PostContent{Text: "hi"}, Settings{"title": "t", "link": "example.com"}, "link"},
	} {
		err := mb.Validate(tc.p, tc.s)
		var ve *ValidationError
		if tc.field == "" && err != nil || tc.field != "" && (!errors.As(err, &ve) || ve.Field != tc.field) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
