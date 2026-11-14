package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
	hooks    map[string]map[string]any // webhooks by id
	token    map[string]string         // the last token request
	hookSeq  int
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
	case r.URL.Path == "/oauth/token":
		var in map[string]string
		_ = json.Unmarshal(raw, &in)
		f.token = in
		if in["client_secret"] != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client_secret lacks oauth:token_exchange permission"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"user-tok","refresh_token":"r2","expires_in":3600,"token_type":"bearer"}`))
	case r.URL.Path == "/oauth/userinfo":
		_, _ = w.Write([]byte(`{"sub":"user_1","name":"Pete K.","preferred_username":"pete","picture":"https://img.example/p.png"}`))
	case r.URL.Path == "/accounts" && r.Header.Get("Authorization") == "Bearer user-tok":
		_, _ = w.Write([]byte(`{"data":[{"id":"biz_1","title":"Staqly","route":"staqly","logo_url":"https://img.example/s.png"},{"id":"biz_2","title":"Other","route":"other"}]}`))
	case r.URL.Path == "/webhooks" && r.Method == http.MethodPost:
		var in map[string]any
		_ = json.Unmarshal(raw, &in)
		if f.hooks == nil {
			f.hooks = map[string]map[string]any{}
		}
		f.hookSeq++
		id := "hook_" + strconv.Itoa(f.hookSeq)
		h := map[string]any{"id": id, "url": in["url"], "events": in["events"], "resource_id": in["resource_id"], "enabled": true}
		f.hooks[id] = h
		out := map[string]any{"webhook_secret": "ws_secret_" + id}
		for k, v := range h {
			out[k] = v
		}
		b, _ := json.Marshal(out)
		_, _ = w.Write(b)
	case strings.HasPrefix(r.URL.Path, "/webhooks/"):
		h, ok := f.hooks[strings.TrimPrefix(r.URL.Path, "/webhooks/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"not_found","message":"Webhook not found"}}`))
			return
		}
		if r.Method == http.MethodPatch {
			var in map[string]any
			_ = json.Unmarshal(raw, &in)
			h["enabled"] = in["enabled"]
		}
		b, _ := json.Marshal(h)
		_, _ = w.Write(b)
	case r.URL.Path == "/experiences/exp_A":
		_, _ = w.Write([]byte(`{"id":"exp_A","company":{"id":"biz_1","title":"Staqly","route":"staqly"}}`))
	case r.URL.Path == "/accounts/me":
		_, _ = w.Write([]byte(`{"id":"biz_1","title":"Acme Studio","route":"acme-studio","logo_url":"https://example.com/logo.png"}`))
	case r.URL.Path == "/forums":
		switch r.URL.Query().Get("account_id") {
		case "biz_1":
			_, _ = w.Write([]byte(`{"data":[` + f.forums + `],"page_info":{"has_next_page":false}}`))
		case "biz_2": // a business without a forum
			_, _ = w.Write([]byte(`{"data":[],"page_info":{"has_next_page":false}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
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
	case r.URL.Path == "/forum_posts/post_9":
		_, _ = w.Write([]byte(`{"id":"post_9","view_count":12,"like_count":3,"comment_count":2}`))
	case r.URL.Path == "/forum_posts" && r.Method == http.MethodGet:
		if r.URL.Query().Get("experience_id") != "exp_A" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Query().Get("parent_id") {
		case "post_9":
			_, _ = w.Write([]byte(`{"data":[{"id":"c1","content":"Nice","created_at":"2026-11-04T18:00:00Z","comment_count":1,"user":{"id":"user_2","username":"ann","name":"Ann"}},` +
				`{"id":"c2","content":"Thanks","created_at":"2026-11-04T19:00:00Z","comment_count":0,"user":{"id":"user_1","username":"pete","name":null}}],"page_info":{"has_next_page":false}}`))
		case "c1":
			_, _ = w.Write([]byte(`{"data":[{"id":"r1","content":"+1","created_at":"2026-11-04T18:10:00Z","comment_count":0,"user":{"id":"user_3","username":"bob","name":"Bob"}}],"page_info":{"has_next_page":false}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[],"page_info":{"has_next_page":false}}`))
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
	api, poll, oauth := whopAPI, whopPoll, whopOAuth
	whopAPI, whopPoll, whopOAuth = f.srv.URL, time.Millisecond, f.srv.URL+"/oauth"
	t.Cleanup(func() {
		f.srv.Close()
		whopAPI, whopPoll, whopOAuth = api, poll, oauth
	})
	return f
}

func TestWhopConnect(t *testing.T) {
	f := useFakeWhop(t)
	wh, _ := Get("whop_key")
	if o, _ := Get("whop"); wh.Status != StatusAvailable || wh.Auth() != AuthToken || o.Auth() != AuthOAuth2 {
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
	if res.ExternalID != "exp_A:post_1" || res.URL != "https://whop.com/acme-studio/" {
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
	if res.ExternalID != "exp_A:post_1" || f.posts[0]["parent_id"] != "post_9" || f.posts[0]["experience_id"] != "exp_A" || f.posts[0]["content"] != "a comment" {
		t.Fatalf("comment %+v %v", res, f.posts)
	}
	// A post stored with its forum: the comment goes to that forum, under the bare post id.
	if _, err := wh.Provider.(Replier).Reply(context.Background(), Credentials{"access_token": "user-tok"}, "exp_B:post_9", PostContent{Text: "b"}, nil); err != nil ||
		f.posts[1]["parent_id"] != "post_9" || f.posts[1]["experience_id"] != "exp_B" {
		t.Fatalf("comment in the post's forum %v %v", err, f.posts)
	}
}

func TestWhopMetricsAndResponses(t *testing.T) {
	useFakeWhop(t)
	wh, _ := Get("whop")
	ctx := context.Background()
	creds := Credentials{"access_token": "user-tok", "user_id": "user_1"}
	m, err := wh.Provider.(MetricsReader).Metrics(ctx, creds, "exp_A:post_9")
	if err != nil || m.Impressions == nil || *m.Impressions != 12 || *m.Likes != 3 || *m.Replies != 2 {
		t.Fatalf("metrics %+v %v", m, err)
	}
	rs, err := wh.Provider.(ResponseReader).Responses(ctx, creds, "exp_A:post_9", time.Time{})
	if err != nil || len(rs) != 3 {
		t.Fatalf("responses %+v %v", rs, err)
	}
	if rs[0].ID != "c1" || rs[0].ParentID != "" || rs[0].Author != "Ann" || !rs[1].Own || rs[2].ID != "r1" || rs[2].ParentID != "c1" {
		t.Fatalf("responses %+v", rs)
	}
	// since: only newer comments.
	if rs, _ := wh.Provider.(ResponseReader).Responses(ctx, creds, "exp_A:post_9", time.Date(2026, 11, 4, 18, 30, 0, 0, time.UTC)); len(rs) != 1 || rs[0].ID != "c2" {
		t.Fatalf("since %+v", rs)
	}
}

func TestWhopValidate(t *testing.T) {
	wh, _ := Get("whop")
	var ve *ValidationError
	// A sign-in posts where the post says: the business is required.
	if err := wh.Validate(PostContent{Text: "hi"}, Settings{"notify": "true"}); !errors.As(err, &ve) || ve.Field != "forum" {
		t.Fatalf("no business: %v", err)
	}
	if err := wh.Validate(PostContent{Text: "hi"}, Settings{"forum": "exp_1", "notify": "true"}); err != nil {
		t.Fatal(err)
	}
	// An API-key channel is one forum: nothing to pick.
	if wk, _ := Get("whop_key"); wk.Validate(PostContent{Text: "hi"}, Settings{"notify": "true"}) != nil {
		t.Fatal("whop_key needs no business")
	}
	if err := wh.Validate(PostContent{Text: strings.Repeat("a", 5001)}, Settings{"forum": "exp_1"}); !errors.As(err, &ve) || ve.Field != "content" {
		t.Fatalf("too long: %v", err)
	}
	if err := wh.Validate(PostContent{Text: "hi"}, Settings{"forum": "exp_1", "notify": "yes"}); !errors.As(err, &ve) || ve.Field != "notify" {
		t.Fatalf("bad boolean: %v", err)
	}
	if err := wh.Validate(PostContent{Text: "hi", Media: make([]MediaFile, 5)}, nil); !errors.As(err, &ve) || ve.Field != "media" {
		t.Fatalf("too many files: %v", err)
	}
}

// A Whop channel's webhook is made once with its secret, kept while it exists, turned back on when Whop disabled it, and
// made again when it is gone or pointed elsewhere.
func TestWhopSubscribeEvents(t *testing.T) {
	f := useFakeWhop(t)
	ctx := context.Background()
	creds := Credentials{"api_key": "good", "account_id": "biz_1", "experience_id": "exp_1"}
	hook := "https://api.example.com/v1/whop/webhook/ch_1"
	if _, err := (Whop{}).SubscribeEvents(ctx, creds, ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("no public address: %v", err)
	}
	got, err := Whop{}.SubscribeEvents(ctx, creds, hook)
	if err != nil || got["webhook_id"] != "hook_1" || got["webhook_secret"] != "ws_secret_hook_1" || got["api_key"] != "good" {
		t.Fatalf("made: %v %v", got, err)
	}
	if h := f.hooks["hook_1"]; h["url"] != hook || h["resource_id"] != "biz_1" || len(h["events"].([]any)) != len(WhopEvents) {
		t.Fatalf("webhook %v", h)
	}
	if again, err := (Whop{}).SubscribeEvents(ctx, got, hook); err != nil || again != nil {
		t.Fatalf("kept: %v %v", again, err)
	}
	f.hooks["hook_1"]["enabled"] = false
	if again, err := (Whop{}).SubscribeEvents(ctx, got, hook); err != nil || again != nil || f.hooks["hook_1"]["enabled"] != true {
		t.Fatalf("turned back on: %v %v %v", again, err, f.hooks["hook_1"])
	}
	delete(f.hooks, "hook_1")
	if again, err := (Whop{}).SubscribeEvents(ctx, got, hook); err != nil || again["webhook_id"] != "hook_2" {
		t.Fatalf("made again: %v %v", again, err)
	}
	if _, err := (Whop{}).SubscribeEvents(ctx, Credentials{"api_key": "revoked", "account_id": "biz_1"}, hook); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestWhopSigned(t *testing.T) {
	sign := func(key []byte, id, ts, body string) string {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(id + "." + ts + "." + body))
		return "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
	}
	body := `{"type":"member.created"}`
	sig := sign([]byte("ws_abc"), "msg_1", "1700000000", body)
	if !WhopSigned("ws_abc", "msg_1", "1700000000", []byte(body), "v1,AAAA "+sig) {
		t.Fatal("a signature among several must pass")
	}
	if WhopSigned("ws_abc", "msg_1", "1700000001", []byte(body), sig) || WhopSigned("ws_other", "msg_1", "1700000000", []byte(body), sig) ||
		WhopSigned("", "msg_1", "1700000000", []byte(body), sig) {
		t.Fatal("another time, secret or none must fail")
	}
	if !WhopSigned("whsec_"+base64.StdEncoding.EncodeToString([]byte("raw-key")), "m", "1", []byte("b"), sign([]byte("raw-key"), "m", "1", "b")) {
		t.Fatal("the spec's whsec_ form")
	}
}

// Signing in with Whop: the authorize link asks for the permissions on all the person's businesses, the channel is the
// person, and the business setting lists their businesses' forums, named after the business.
func TestWhopSignIn(t *testing.T) {
	f := useFakeWhop(t)
	ctx := context.Background()
	wh, _ := Get("whop")
	app := App{ClientID: "app_1", ClientSecret: "secret"}
	u, err := wh.AuthURL(app, "https://app.example/channels/callback/whop", "st", map[string]string{"code_verifier": "v"})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(u[strings.Index(u, "?")+1:])
	if !strings.Contains(q.Get("scope"), "forum:post:create") || q.Get("code_challenge_method") != "S256" || q.Has("company_id") {
		t.Fatalf("authorize %s", u)
	}
	creds, profile, err := wh.Exchange(ctx, app, "https://app.example/channels/callback/whop", "code", map[string]string{"code_verifier": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if creds["access_token"] != "user-tok" || creds["refresh_token"] != "r2" || f.token["client_secret"] != "secret" || f.token["code_verifier"] != "v" ||
		profile.Name != "Pete K." || profile.Handle != "pete" {
		t.Fatalf("exchange %v %+v %v", creds, profile, f.token)
	}
	opts, err := wh.Provider.(OptionLister).Options(ctx, creds, "forum")
	if err != nil {
		t.Fatal(err)
	}
	// whopTwoForums: each business has two forums here, so the label names the forum too.
	if len(opts) == 0 || opts[0].Icon != "https://img.example/s.png" || !strings.HasPrefix(opts[0].Label, "Staqly: ") {
		t.Fatalf("options %+v", opts)
	}
	// A sign-in's channel has no business of its own: the post links to the page of the business picked for it.
	res, err := wh.Publish(ctx, creds, PostContent{Text: "hi"}, Settings{"forum": "exp_A"})
	if err != nil || res.URL != "https://whop.com/staqly/" {
		t.Fatalf("post %+v %v", res, err)
	}
	if _, _, err := wh.Exchange(ctx, App{ClientID: "app_1", ClientSecret: "wrong"}, "r", "code", map[string]string{"code_verifier": "v"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "server's app") {
		t.Fatalf("bad secret: %v", err)
	}
}
