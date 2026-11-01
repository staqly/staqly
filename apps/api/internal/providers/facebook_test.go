package providers

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFacebook answers like graph.facebook.com (and graph-video, and the upload host) for the calls Facebook Pages
// and Instagram make.
type fakeFacebook struct {
	mu       sync.Mutex
	calls    []string
	forms    map[string][]url.Values // POST path -> forms (multipart fields included), in order
	files    map[string][]string     // POST path -> uploaded file contents
	headers  []http.Header           // the upload host's requests
	granted  string                  // comma-separated granted permissions (default: all)
	noRole   bool                    // /me/accounts lists the Page without a token
	noPages  bool                    // /me/accounts is empty
	failPath string                  // POST path that answers failCode/failBody
	failCode int
	failBody string
	dropPath string // POST path whose connection is closed (an answer that never came)
	polls    int
	srv      *httptest.Server
}

func (f *fakeFacebook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/"+instagramVersion)
	token := r.URL.Query().Get("access_token")
	if r.Method == http.MethodPost {
		form := url.Values{}
		file := ""
		if mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); strings.HasPrefix(mt, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(part)
				if part.FileName() != "" {
					file = string(b)
				} else {
					form.Add(part.FormName(), string(b))
				}
			}
		} else {
			raw, _ := io.ReadAll(r.Body)
			form, _ = url.ParseQuery(string(raw))
		}
		if f.forms == nil {
			f.forms, f.files = map[string][]url.Values{}, map[string][]string{}
		}
		f.forms[path] = append(f.forms[path], form)
		if file != "" {
			f.files[path] = append(f.files[path], file)
		}
		token = form.Get("access_token")
		if path == f.dropPath {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		if path == f.failPath {
			w.WriteHeader(f.failCode)
			_, _ = w.Write([]byte(f.failBody))
			return
		}
	}
	if token == "revoked" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Error validating access token: The session has been invalidated","type":"OAuthException","code":190}}`))
		return
	}
	q := r.URL.Query()
	switch {
	case path == "/oauth/access_token" && q.Get("grant_type") == "client_credentials":
		if q.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Error validating client secret.","type":"OAuthException","code":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"app|token","token_type":"bearer"}`))
	case path == "/oauth/access_token" && q.Get("grant_type") == "fb_exchange_token":
		_, _ = w.Write([]byte(`{"access_token":"long-user","token_type":"bearer","expires_in":5183944}`))
	case path == "/oauth/access_token":
		if q.Get("code") != "code1" || q.Get("redirect_uri") != "https://app.example/cb" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid verification code format.","type":"OAuthException","code":100}}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"short-user","token_type":"bearer","expires_in":3600}`))
	case path == "/me/permissions":
		granted := f.granted
		if granted == "" {
			granted = facebookScopes + ",instagram_basic,instagram_content_publish,instagram_manage_comments"
		}
		var data []string
		for _, p := range strings.Split(granted, ",") {
			data = append(data, `{"permission":"`+p+`","status":"granted"}`)
		}
		_, _ = w.Write([]byte(`{"data":[` + strings.Join(data, ",") + `]}`))
	case path == "/me" && r.Method == http.MethodGet && token == "long-user":
		_, _ = w.Write([]byte(`{"id":"u1","name":"Jane Doe","picture":{"data":{"url":"https://cdn.example/jane.jpg"}}}`))
	case path == "/me/accounts" && q.Get("after") == "":
		if f.noPages {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		tok := `"access_token":"page-token-1",`
		if f.noRole {
			tok = ""
		}
		// The second page of results comes through paging.next, like Facebook's.
		_, _ = w.Write([]byte(`{"data":[{"id":"p1","name":"Jane's Bakery","username":"janesbakery",` + tok + `"picture":{"data":{"url":"https://cdn.example/p1.jpg"}},
			"instagram_business_account":{"id":"ig1","username":"janesbakery.ig","name":"Jane's Bakery","profile_picture_url":"https://cdn.example/ig1.jpg"}}],
			"paging":{"next":"` + f.srv.URL + `/` + instagramVersion + `/me/accounts?after=2&access_token=long-user"}}`))
	case path == "/me/accounts":
		_, _ = w.Write([]byte(`{"data":[{"id":"p2","name":"Jane's Shop","access_token":"page-token-2"}]}`))
	case path == "/me/assigned_instagram_accounts" && f.noPages:
		_, _ = w.Write([]byte(`{"data":[]}`))
	case path == "/me/assigned_instagram_accounts":
		_, _ = w.Write([]byte(`{"data":[{"id":"ig1","username":"janesbakery.ig","name":"Jane's Bakery","profile_picture_url":"https://cdn.example/ig1.jpg"}]}`))
	case path == "/me/businesses" && f.noPages:
		_, _ = w.Write([]byte(`{"data":[]}`))
	case path == "/me/businesses":
		_, _ = w.Write([]byte(`{"data":[{"id":"b1"}]}`))
	case path == "/b1/owned_pages":
		// A Page seen already, and one the person has no role on (no token).
		_, _ = w.Write([]byte(`{"data":[{"id":"p1","name":"Jane's Bakery","access_token":"page-token-1"},{"id":"p3","name":"Other"}]}`))
	case path == "/b1/client_pages":
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"(#200) Requires business_management","code":200}}`))
	case r.Method == http.MethodGet && (path == "/p1" || path == "/ig1") && q.Get("fields") == "id":
		_, _ = w.Write([]byte(`{"id":"` + strings.TrimPrefix(path, "/") + `"}`))

	// Facebook publishing.
	case path == "/p1/photos":
		_, _ = w.Write([]byte(`{"id":"ph` + string(rune('0'+len(f.forms[path]))) + `"}`))
	case path == "/p1/feed":
		_, _ = w.Write([]byte(`{"id":"p1_100"}`))
	case path == "/p1/videos":
		_, _ = w.Write([]byte(`{"id":"v1"}`))
	case path == "/p1/video_reels" || path == "/p1/video_stories":
		form := f.forms[path][len(f.forms[path])-1]
		switch form.Get("upload_phase") {
		case "start":
			_, _ = w.Write([]byte(`{"video_id":"v2","upload_url":"` + f.srv.URL + `/rupload/v2"}`))
		case "finish":
			_, _ = w.Write([]byte(`{"success":true,"post_id":"s2"}`))
		}
	case path == "/p1/photo_stories":
		_, _ = w.Write([]byte(`{"success":true,"post_id":"s1"}`))
	case r.URL.Path == "/rupload/v2":
		f.headers = append(f.headers, r.Header.Clone())
		_, _ = w.Write([]byte(`{"success":true}`))
	case path == "/v2" && q.Get("fields") == "status":
		f.polls++
		if f.polls == 1 {
			_, _ = w.Write([]byte(`{"status":{"video_status":"processing","uploading_phase":{"status":"in_progress"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":{"video_status":"upload_complete","uploading_phase":{"status":"complete"}}}`))
	case strings.HasSuffix(path, "/comments"):
		_, _ = w.Write([]byte(`{"id":"cm` + string(rune('0'+len(f.forms[path]))) + `"}`))
	case q.Get("fields") == "permalink_url":
		_, _ = w.Write([]byte(`{"permalink_url":"https://www.facebook.com/janesbakery/posts/100"}`))

	// Instagram publishing.
	case path == "/ig1/media":
		_, _ = w.Write([]byte(`{"id":"c` + string(rune('0'+len(f.forms[path]))) + `"}`))
	case q.Get("fields") == "status_code,status":
		_, _ = w.Write([]byte(`{"status_code":"FINISHED","status":"FINISHED"}`))
	case path == "/ig1/media_publish":
		_, _ = w.Write([]byte(`{"id":"1790"}`))
	case q.Get("fields") == "permalink":
		_, _ = w.Write([]byte(`{"permalink":"https://www.instagram.com/p/DAbc/"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Unknown path ` + path + `","code":803}}`))
	}
}

func (f *fakeFacebook) form(path string, i int) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.forms[path]) {
		return nil
	}
	return f.forms[path][i]
}

func (f *fakeFacebook) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

// snapshot reads what the server saw, under its lock.
func (f *fakeFacebook) snapshot() (files map[string][]string, headers []http.Header, polls int, forms map[string][]url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files, f.headers, f.polls, f.forms
}

func useFakeFacebook(t *testing.T) *fakeFacebook {
	t.Helper()
	f := &fakeFacebook{}
	f.srv = httptest.NewServer(f)
	auth, graph, video, poll := facebookAuthorize, facebookGraph, facebookVideo, facebookPoll
	facebookAuthorize, facebookGraph, facebookVideo, facebookPoll = f.srv.URL+"/dialog/oauth", f.srv.URL, f.srv.URL, time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		facebookAuthorize, facebookGraph, facebookVideo, facebookPoll = auth, graph, video, poll
	})
	return f
}

func bytesFile(name, mimeType, content string) MediaFile {
	return MediaFile{Name: name, MimeType: mimeType, Size: int64(len(content)), Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	}}
}

var pageCreds = Credentials{"access_token": "page-token-1", "page_id": "p1"}

func TestFacebookConnect(t *testing.T) {
	f := useFakeFacebook(t)
	fb, _ := Get("facebook")
	if fb.Status != StatusAvailable || fb.Name() != "Facebook Page" {
		t.Fatalf("facebook is %q %q", fb.Status, fb.Name())
	}
	if n, ok := fb.Provider.(Needer); !ok || !n.Needs().HTTPS || n.Needs().PublicMedia {
		t.Fatal("facebook needs https only")
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := fb.AuthURL(app, "https://app.example/cb", "st", nil)
	q, _ := url.Parse(u)
	if q.Query().Get("scope") != facebookScopes || q.Query().Get("state") != "st" || q.Query().Get("client_id") != "cid" {
		t.Fatalf("auth url %s", u)
	}
	ctx := context.Background()
	creds, person, err := fb.Exchange(ctx, app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds["access_token"] != "long-user" || creds["expires_at"] == "" || person.Name != "Jane Doe" || person.NoComments {
		t.Fatalf("creds %v person %+v", creds, person)
	}

	// The Pages: /me/accounts over two pages of results plus the business portfolio's, each once; a Page without
	// a token for this person is left out.
	lister := fb.Provider.(AccountLister)
	accounts, err := lister.Accounts(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].ID != "p1" || accounts[0].Handle != "@janesbakery" || accounts[1].Handle != "p2" {
		t.Fatalf("accounts %+v", accounts)
	}
	page, profile, err := lister.UseAccount(ctx, creds, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if page["access_token"] != "page-token-1" || page["page_id"] != "p1" || profile.Handle != "@janesbakery" ||
		profile.ProfileURL != "https://www.facebook.com/janesbakery" || profile.AvatarURL == "" {
		t.Fatalf("page %v profile %+v", page, profile)
	}
	if _, _, err := lister.UseAccount(ctx, creds, "p3"); !errors.Is(err, ErrRejected) {
		t.Fatalf("a Page without a role: %v", err)
	}

	// Refresh checks the Page token; a revoked one needs reconnecting.
	if _, err := fb.Refresh(ctx, app, page); err != nil {
		t.Fatal(err)
	}
	if _, err := fb.Refresh(ctx, app, Credentials{"access_token": "revoked", "page_id": "p1"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}

	// A wrong code, publishing not allowed, no Pages, no role on any Page.
	if _, _, err := fb.Exchange(ctx, app, "https://app.example/cb", "nope", nil); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("bad code: %v", err)
	}
	f.set(func() { f.granted = "pages_show_list,pages_read_engagement" })
	if _, _, err := fb.Exchange(ctx, app, "https://app.example/cb", "code1", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("without publishing: %v", err)
	}
	f.set(func() { f.granted = "pages_show_list,pages_manage_posts" })
	if c, p, err := fb.Exchange(ctx, app, "https://app.example/cb", "code1", nil); err != nil || !p.NoComments {
		t.Fatalf("without comments: %v %+v", err, p)
	} else if _, p, _ := lister.UseAccount(ctx, c, "p1"); !p.NoComments {
		t.Fatal("the Page channel should know it may not comment")
	}
	f.set(func() { f.noPages = true })
	if _, err := lister.Accounts(ctx, creds); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "no Pages") {
		t.Fatalf("no pages: %v", err)
	}
}

func TestFacebookPublish(t *testing.T) {
	ctx := context.Background()
	fb, _ := Get("facebook")

	t.Run("text", func(t *testing.T) {
		f := useFakeFacebook(t)
		res, err := fb.Publish(ctx, pageCreds, PostContent{Text: "Fresh bread"}, Settings{})
		if err != nil {
			t.Fatal(err)
		}
		if f.form("/p1/feed", 0).Get("message") != "Fresh bread" || f.form("/p1/feed", 0).Get("access_token") != "page-token-1" {
			t.Fatalf("feed %v", forms(f))
		}
		if res.ExternalID != "p1_100" || res.URL != "https://www.facebook.com/janesbakery/posts/100" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("pictures", func(t *testing.T) {
		f := useFakeFacebook(t)
		if _, err := fb.Publish(ctx, pageCreds, PostContent{Text: "Two", Media: []MediaFile{bytesFile("a.jpg", "image/jpeg", "AAA"), bytesFile("b.png", "image/png", "BBB")}}, Settings{}); err != nil {
			t.Fatal(err)
		}
		if got := files(f)["/p1/photos"]; len(got) != 2 || got[0] != "AAA" || f.form("/p1/photos", 0).Get("published") != "false" {
			t.Fatalf("photos %v %v", got, forms(f)["/p1/photos"])
		}
		feed := f.form("/p1/feed", 0)
		if feed.Get("attached_media[0]") != `{"media_fbid":"ph1"}` || feed.Get("attached_media[1]") != `{"media_fbid":"ph2"}` {
			t.Fatalf("feed %v", feed)
		}
	})

	t.Run("video post: bytes, or the link when there is one", func(t *testing.T) {
		f := useFakeFacebook(t)
		res, err := fb.Publish(ctx, pageCreds, PostContent{Text: "Watch", Media: []MediaFile{bytesFile("v.mp4", "video/mp4", "VVV")}}, Settings{})
		if err != nil || res.ExternalID != "v1" || files(f)["/p1/videos"][0] != "VVV" || f.form("/p1/videos", 0).Get("description") != "Watch" {
			t.Fatalf("video %+v %v %v", res, err, forms(f))
		}
		if _, err := fb.Publish(ctx, pageCreds, PostContent{Media: []MediaFile{mp4("v.mp4")}}, Settings{}); err != nil {
			t.Fatal(err)
		}
		if f.form("/p1/videos", 1).Get("file_url") != "https://bucket.example/v.mp4" {
			t.Fatalf("video by link %v", forms(f)["/p1/videos"])
		}
	})

	t.Run("reel", func(t *testing.T) {
		f := useFakeFacebook(t)
		res, err := fb.Publish(ctx, pageCreds, PostContent{Text: "Reel", Media: []MediaFile{bytesFile("r.mp4", "video/mp4", "RRRR")}}, Settings{"post_type": "reel"})
		if err != nil {
			t.Fatal(err)
		}
		h := headers(f)[0]
		if h.Get("Authorization") != "OAuth page-token-1" || h.Get("file_size") != "4" || h.Get("offset") != "0" {
			t.Fatalf("upload headers %v", h)
		}
		finish := f.form("/p1/video_reels", 1)
		if finish.Get("upload_phase") != "finish" || finish.Get("video_id") != "v2" || finish.Get("video_state") != "PUBLISHED" || finish.Get("description") != "Reel" || polls(f) < 2 {
			t.Fatalf("finish %v after %d checks", finish, polls(f))
		}
		if res.ExternalID != "v2" || res.URL != "https://www.facebook.com/reel/v2" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("stories", func(t *testing.T) {
		f := useFakeFacebook(t)
		res, err := fb.Publish(ctx, pageCreds, PostContent{Media: []MediaFile{bytesFile("s.jpg", "image/jpeg", "S")}}, Settings{"post_type": "story"})
		if err != nil || res.ExternalID != "s1" || f.form("/p1/photo_stories", 0).Get("photo_id") != "ph1" {
			t.Fatalf("photo story %+v %v", res, err)
		}
		res, err = fb.Publish(ctx, pageCreds, PostContent{Media: []MediaFile{mp4("s.mp4")}}, Settings{"post_type": "story"})
		if err != nil || res.ExternalID != "s2" || headers(f)[0].Get("file_url") != "https://bucket.example/s.mp4" {
			t.Fatalf("video story %+v %v %v", res, err, headers(f))
		}
	})

	t.Run("failures", func(t *testing.T) {
		f := useFakeFacebook(t)
		for body, want := range map[string]error{
			`{"error":{"message":"(#200) The user hasn't authorized the application to perform this action","code":200}}`: ErrRejected,
			`{"error":{"message":"Error validating access token","code":190,"error_subcode":460}}`:                        ErrReauth,
			`{"error":{"message":"(#32) Page request limit reached","code":32}}`:                                          nil,
			`{"error":{"message":"An unexpected error has occurred.","code":2}}`:                                          nil,
			`{"error":{"message":"Duplicate status message","code":506}}`:                                                 ErrRejected,
		} {
			f.set(func() { f.failPath, f.failCode, f.failBody = "/p1/feed", http.StatusBadRequest, body })
			_, err := fb.Publish(ctx, pageCreds, PostContent{Text: "x"}, Settings{})
			var fail *Failure
			if !errors.As(err, &fail) || fail.Kind != want {
				t.Fatalf("%s: %v", body, err)
			}
		}
		// A picture upload that got no answer: nothing was published, so it is retried.
		f.set(func() { f.failPath, f.dropPath = "", "/p1/photos" })
		_, err := fb.Publish(ctx, pageCreds, PostContent{Media: []MediaFile{bytesFile("a.jpg", "image/jpeg", "A")}}, Settings{})
		var fail *Failure
		if !errors.As(err, &fail) || fail.Kind != nil {
			t.Fatalf("lost upload: %v", err)
		}
	})

	t.Run("an answer that never came is not retried", func(t *testing.T) {
		f := useFakeFacebook(t)
		f.set(func() { f.dropPath = "/p1/feed" })
		_, err := fb.Publish(ctx, pageCreds, PostContent{Text: "x"}, Settings{})
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") || strings.Contains(err.Error(), "page-token") {
			t.Fatalf("lost answer: %v", err)
		}
	})
}

func TestFacebookReply(t *testing.T) {
	f := useFakeFacebook(t)
	fb, _ := Get("facebook")
	r := fb.Provider.(Replier)
	ctx := context.Background()
	first, err := r.Reply(ctx, pageCreds, "p1_100", PostContent{Text: "First"}, Settings{})
	if err != nil || first.ExternalID != "comment:p1_100:cm1" {
		t.Fatalf("first %+v %v", first, err)
	}
	// The next part comments on the post too, with a picture.
	if _, err := r.Reply(ctx, pageCreds, first.ExternalID, PostContent{Text: "Second", Media: []MediaFile{bytesFile("c.jpg", "image/jpeg", "C")}}, Settings{}); err != nil {
		t.Fatal(err)
	}
	if f.form("/p1_100/comments", 1).Get("message") != "Second" || files(f)["/p1_100/comments"][0] != "C" {
		t.Fatalf("comments %v", forms(f))
	}
	if _, err := r.Reply(ctx, pageCreds, "s1", PostContent{Text: "x"}, Settings{"post_type": "story"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("story comment: %v", err)
	}
}

func TestFacebookValidate(t *testing.T) {
	fb, _ := Get("facebook")
	pic, vid := bytesFile("a.jpg", "image/jpeg", "A"), bytesFile("v.mp4", "video/mp4", "V")
	for _, c := range []struct {
		name   string
		media  []MediaFile
		s      Settings
		thread []ThreadPart
		ok     bool
	}{
		{"text", nil, Settings{}, nil, true},
		{"pictures", []MediaFile{pic, pic}, Settings{}, nil, true},
		{"video", []MediaFile{vid}, Settings{}, nil, true},
		{"picture and video", []MediaFile{pic, vid}, Settings{}, nil, false},
		{"reel", []MediaFile{vid}, Settings{"post_type": "reel"}, nil, true},
		{"reel of a picture", []MediaFile{pic}, Settings{"post_type": "reel"}, nil, false},
		{"story", []MediaFile{pic}, Settings{"post_type": "story"}, nil, true},
		{"story without media", nil, Settings{"post_type": "story"}, nil, false},
		{"unknown type", nil, Settings{"post_type": "carousel"}, nil, false},
		{"comment with a video", nil, Settings{}, []ThreadPart{{Text: "c", Media: []MediaFile{vid}}}, false},
		{"pdf", []MediaFile{{Name: "a.pdf", MimeType: "application/pdf"}}, Settings{}, nil, false},
	} {
		err := fb.Validate(PostContent{Text: "x", Media: c.media, Thread: c.thread}, c.s)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestInstagramConnect(t *testing.T) {
	f := useFakeFacebook(t)
	ig, _ := Get("instagram")
	if ig.Status != StatusAvailable || ig.Name() != "Instagram" {
		t.Fatalf("instagram is %q %q", ig.Status, ig.Name())
	}
	if n, ok := ig.Provider.(Needer); !ok || !n.Needs().PublicMedia || !n.Needs().HTTPS {
		t.Fatal("instagram needs https and public media")
	}
	ctx := context.Background()
	app := App{ClientID: "cid", ClientSecret: "secret", ConfigID: "cfg1"}

	// Facebook Login for Business with the app's configuration, which says the permissions: no scope.
	u, err := ig.AuthURL(app, "https://app.example/cb", "st", nil)
	q, _ := url.Parse(u)
	if err != nil || q.Query().Get("config_id") != "cfg1" || q.Query().Get("scope") != "" || q.Query().Get("state") != "st" ||
		q.Query().Get("prompt") != "login" || q.Query().Get("response_type") != "code" || q.Query().Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("auth url %s %v", u, err)
	}
	if _, err := ig.AuthURL(App{ClientID: "cid", ClientSecret: "secret"}, "https://app.example/cb", "st", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("no configuration: %v", err)
	}
	creds, _, err := ig.Exchange(ctx, app, "https://app.example/cb", "code1#_", nil)
	if err != nil || creds["access_token"] != "short-user" {
		t.Fatalf("exchange %v %v", creds, err)
	}

	// The accounts the person shared; the channel keeps the sign-in's token.
	lister := ig.Provider.(AccountLister)
	accounts, err := lister.Accounts(ctx, creds)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "ig1" || accounts[0].Handle != "@janesbakery.ig" {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	ch, profile, err := lister.UseAccount(ctx, creds, "ig1")
	if err != nil || ch["access_token"] != "short-user" || ch["user_id"] != "ig1" || profile.ProfileURL != "https://www.instagram.com/janesbakery.ig/" || profile.Name != "Jane's Bakery" {
		t.Fatalf("use %v %+v %v", ch, profile, err)
	}
	if _, _, err := lister.UseAccount(ctx, creds, "ig9"); !errors.Is(err, ErrRejected) {
		t.Fatalf("an account not shared: %v", err)
	}
	if _, err := ig.Refresh(ctx, app, ch); err != nil {
		t.Fatal(err)
	}

	// Publishing on graph.facebook.com.
	res, err := ig.Publish(ctx, ch, PostContent{Text: "Hi", Media: []MediaFile{jpeg("a.jpg"), jpeg("b.jpg")}}, Settings{"collaborators": "@ana"})
	if err != nil || res.ExternalID != "1790" || res.URL != "https://www.instagram.com/p/DAbc/" {
		t.Fatalf("publish %+v %v", res, err)
	}
	if p := f.form("/ig1/media", 2); p.Get("media_type") != "CAROUSEL" || p.Get("children") != "c1,c2" || p.Get("collaborators") != `["ana"]` {
		t.Fatalf("carousel %v", forms(f)["/ig1/media"])
	}
	if r, err := ig.Provider.(Replier).Reply(ctx, ch, "1790", PostContent{Text: "First"}, nil); err != nil || r.ExternalID != "comment:1790:cm1" {
		t.Fatalf("reply %+v %v", r, err)
	}

	// No account shared; a configuration without publishing or comments; a revoked or expired sign-in.
	f.set(func() { f.noPages = true })
	if _, err := lister.Accounts(ctx, creds); !errors.Is(err, ErrRejected) {
		t.Fatalf("no account: %v", err)
	}
	f.set(func() { f.noPages, f.granted = false, "instagram_basic,instagram_manage_comments" })
	if _, _, err := ig.Exchange(ctx, app, "https://app.example/cb", "code1", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("without publishing: %v", err)
	}
	f.set(func() { f.granted = "instagram_basic,instagram_content_publish" })
	if c, p, err := ig.Exchange(ctx, app, "https://app.example/cb", "code1", nil); err != nil || c["no_comments"] != "true" || !p.NoComments {
		t.Fatalf("without comments: %v %+v %v", c, p, err)
	}
	if _, err := ig.Refresh(ctx, app, Credentials{"access_token": "revoked", "user_id": "ig1"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
	expired := Credentials{"access_token": "short-user", "user_id": "ig1", "expires_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}
	if _, err := ig.Refresh(ctx, app, expired); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
}

func TestMetaCheckApp(t *testing.T) {
	useFakeFacebook(t)
	ctx := context.Background()
	for _, p := range []AppChecker{Facebook{}, Instagram{}} {
		if ok, err := p.CheckApp(ctx, App{ClientID: "cid", ClientSecret: "secret"}, ""); !ok || err != nil {
			t.Fatalf("%T good keys: %v %v", p, ok, err)
		}
		if _, err := p.CheckApp(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, ""); err == nil || strings.Contains(err.Error(), "wrong") {
			t.Fatalf("%T bad keys: %v", p, err)
		}
	}
}

func files(f *fakeFacebook) map[string][]string     { a, _, _, _ := f.snapshot(); return a }
func headers(f *fakeFacebook) []http.Header         { _, b, _, _ := f.snapshot(); return b }
func polls(f *fakeFacebook) int                     { _, _, c, _ := f.snapshot(); return c }
func forms(f *fakeFacebook) map[string][]url.Values { _, _, _, d := f.snapshot(); return d }
