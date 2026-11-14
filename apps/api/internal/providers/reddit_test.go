package providers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeReddit answers like www.reddit.com, oauth.reddit.com and Reddit's media bucket for the calls the provider makes.
type fakeReddit struct {
	mu          sync.Mutex
	srv         *httptest.Server
	calls       []string
	tokenForm   url.Values
	basicUser   string
	agents      []string
	submit      url.Values   // the last /api/submit form
	submits     int          // how many submits
	comment     url.Values   // the last /api/comment form
	uploads     []url.Values // the bucket's form fields per upload (file part as "file" = its bytes)
	submitBody  string       // answer to /api/submit instead of the default
	submitCode  int
	dropSubmit  bool   // close the connection on /api/submit
	listed      bool   // the submitted listing has the post
	listedAfter int    // ...only from this look on
	looks       int    // listing looks so far
	flairs      string // answer of link_flair_v2
	refreshErr  string // "error" of a refresh answer
}

func (f *fakeReddit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.agents = append(f.agents, r.Header.Get("User-Agent"))
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/bucket" {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		form := url.Values{}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			form.Set(p.FormName(), string(b))
		}
		f.uploads = append(f.uploads, form)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`<PostResponse><Location>x</Location></PostResponse>`))
		return
	}
	raw, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(raw))
	auth := r.Header.Get("Authorization")
	switch {
	case r.URL.Path == "/api/v1/access_token":
		f.tokenForm = form
		f.basicUser, _, _ = r.BasicAuth()
		if form.Get("grant_type") == "refresh_token" && f.refreshErr != "" {
			_, _ = w.Write([]byte(`{"error":"` + f.refreshErr + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at-` + form.Get("grant_type") + `","token_type":"bearer","expires_in":86400,"refresh_token":"rt","scope":"identity submit read flair"}`))
	case auth == "Bearer revoked":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthorized","error":401}`))
	case r.URL.Path == "/api/v1/me":
		_, _ = w.Write([]byte(`{"name":"janedoe","icon_img":"https://styles.redditmedia.com/jane.png?width=256&s=abc"}`))
	case r.URL.Path == "/r/running/api/link_flair_v2":
		if f.flairs == "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Forbidden","error":403}`))
			return
		}
		_, _ = w.Write([]byte(f.flairs))
	case r.URL.Path == "/api/media/asset.json":
		_, _ = w.Write([]byte(`{"args":{"action":"` + f.srv.URL + `/bucket","fields":[{"name":"key","value":"rte/` + form.Get("filepath") + `"},{"name":"success_action_status","value":"201"}]},"asset":{"asset_id":"a1"}}`))
	case r.URL.Path == "/api/submit":
		f.submit, f.submits = form, f.submits+1
		switch {
		case f.dropSubmit:
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		case f.submitCode != 0:
			w.WriteHeader(f.submitCode)
			_, _ = w.Write([]byte(`{"message":"Too Many Requests","error":429}`))
		case f.submitBody != "":
			_, _ = w.Write([]byte(f.submitBody))
		case form.Get("kind") == "image" || form.Get("kind") == "video":
			_, _ = w.Write([]byte(`{"json":{"errors":[],"data":{"user_submitted_page":"https://www.reddit.com/user/janedoe/submitted/","websocket_url":"wss://ws.example/x"}}}`))
		default:
			_, _ = w.Write([]byte(`{"json":{"errors":[],"data":{"url":"https://www.reddit.com/r/running/comments/abc12/long_run/","drafts_count":0,"id":"abc12","name":"t3_abc12"}}}`))
		}
	case r.URL.Path == "/user/janedoe/submitted":
		f.looks++
		created := strconv.FormatInt(time.Now().Unix(), 10)
		old := strconv.FormatInt(time.Now().Add(-48*time.Hour).Unix(), 10)
		children := `{"data":{"name":"t3_old","subreddit":"running","title":"Long run","created_utc":` + old + `,"permalink":"/r/running/comments/old/"}}`
		if f.listed && f.looks > f.listedAfter {
			children = `{"data":{"name":"t3_new","subreddit":"Running","title":"Long run","created_utc":` + created + `,"permalink":"/r/running/comments/new/long_run/"}},` + children
		}
		_, _ = w.Write([]byte(`{"data":{"children":[` + children + `]}}`))
	case r.URL.Path == "/api/comment":
		f.comment = form
		_, _ = w.Write([]byte(`{"json":{"errors":[],"data":{"things":[{"kind":"t1","data":{"name":"t1_c` + strconv.Itoa(len(f.calls)) + `","permalink":"/r/running/comments/abc12/long_run/c1/"}}]}}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeReddit(t *testing.T) *fakeReddit {
	t.Helper()
	f := &fakeReddit{}
	f.srv = httptest.NewServer(f)
	www, api, poll, looks := redditWWW, redditOAuth, redditPoll, redditLooks
	redditWWW, redditOAuth, redditPoll, redditLooks = f.srv.URL, f.srv.URL, time.Millisecond, 5
	t.Cleanup(func() {
		f.srv.Close()
		redditWWW, redditOAuth, redditPoll, redditLooks = www, api, poll, looks
	})
	return f
}

func redditCreds() Credentials {
	return Credentials{"access_token": "at", "refresh_token": "rt", "username": "janedoe"}
}

func redditFile(name, mimeType, data string) MediaFile {
	return MediaFile{Name: name, MimeType: mimeType, Size: int64(len(data)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil }}
}

func TestRedditConnect(t *testing.T) {
	f := useFakeReddit(t)
	rd, _ := Get("reddit")
	if rd.Status != StatusAvailable {
		t.Fatalf("reddit is %q", rd.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := rd.AuthURL(app, "https://app.example/cb", "st", nil)
	q, _ := url.Parse(u)
	if q.Query().Get("duration") != "permanent" || q.Query().Get("scope") != redditScopes || q.Query().Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := rd.Exchange(context.Background(), app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.basicUser != "cid" || f.tokenForm.Get("code") != "code1" || f.tokenForm.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("token request %v %q", f.tokenForm, f.basicUser)
	}
	if creds["access_token"] != "at-authorization_code" || creds["refresh_token"] != "rt" || creds["username"] != "janedoe" || creds["expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "u/janedoe" || profile.AvatarURL != "https://styles.redditmedia.com/jane.png" {
		t.Fatalf("profile %+v", profile)
	}
	for _, a := range f.agents {
		if a != redditUserAgent {
			t.Fatalf("user agent %q", a)
		}
	}

	// Refresh keeps the refresh token; a revoked one needs reconnecting.
	c, err := rd.Refresh(context.Background(), app, redditCreds())
	if err != nil || c["access_token"] != "at-refresh_token" || c["refresh_token"] != "rt" {
		t.Fatalf("refresh %v %v", c, err)
	}
	f.refreshErr = "invalid_grant"
	if _, err := rd.Refresh(context.Background(), app, redditCreds()); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked refresh: %v", err)
	}
}

func TestRedditPublish(t *testing.T) {
	ctx := context.Background()
	rd, _ := Get("reddit")

	t.Run("text post with flair", func(t *testing.T) {
		f := useFakeReddit(t)
		f.flairs = `[{"id":"f-1","text":"Training"},{"id":"f-2","text":"Race report"}]`
		res, err := rd.Publish(ctx, redditCreds(), PostContent{Text: "20 miles today."},
			Settings{"title": "Long run", "subreddit": "r/running", "flair": "race report", "nsfw": "false", "spoiler": "true"})
		if err != nil {
			t.Fatal(err)
		}
		s := f.submit
		if s.Get("kind") != "self" || s.Get("sr") != "running" || s.Get("text") != "20 miles today." || s.Get("flair_id") != "f-2" || s.Get("spoiler") != "true" || s.Get("api_type") != "json" {
			t.Fatalf("submit %v", s)
		}
		if res.ExternalID != "t3_abc12" || res.URL != "https://www.reddit.com/r/running/comments/abc12/long_run/" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("unknown flair lists the choices", func(t *testing.T) {
		f := useFakeReddit(t)
		f.flairs = `[{"id":"f-1","text":"Training"}]`
		_, err := rd.Publish(ctx, redditCreds(), PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running", "flair": "Gear"})
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "Training") || f.submits != 0 {
			t.Fatalf("unknown flair: %v", err)
		}
	})

	t.Run("link post", func(t *testing.T) {
		f := useFakeReddit(t)
		if _, err := rd.Publish(ctx, redditCreds(), PostContent{}, Settings{"title": "Long run", "subreddit": "https://www.reddit.com/r/running/", "link": "https://example.com/run"}); err != nil {
			t.Fatal(err)
		}
		if f.submit.Get("kind") != "link" || f.submit.Get("url") != "https://example.com/run" || f.submit.Get("sr") != "running" {
			t.Fatalf("submit %v", f.submit)
		}
	})

	t.Run("picture: uploaded, then found in the account's posts", func(t *testing.T) {
		f := useFakeReddit(t)
		f.listed, f.listedAfter = true, 1
		res, err := rd.Publish(ctx, redditCreds(), PostContent{Media: []MediaFile{redditFile("a.jpg", "image/jpeg", "JPEGDATA")}},
			Settings{"title": "Long run", "subreddit": "running"})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.uploads) != 1 || f.uploads[0].Get("file") != "JPEGDATA" || f.uploads[0].Get("key") != "rte/a.jpg" || f.uploads[0].Get("success_action_status") != "201" {
			t.Fatalf("uploads %v", f.uploads)
		}
		if f.submit.Get("kind") != "image" || f.submit.Get("url") != f.srv.URL+"/bucket/rte/a.jpg" {
			t.Fatalf("submit %v", f.submit)
		}
		if res.ExternalID != "t3_new" || res.URL != "https://www.reddit.com/r/running/comments/new/long_run/" || f.looks < 2 {
			t.Fatalf("result %+v after %d looks", res, f.looks)
		}
	})

	t.Run("video gets a cover; never found is still published", func(t *testing.T) {
		f := useFakeReddit(t)
		res, err := rd.Publish(ctx, redditCreds(), PostContent{Media: []MediaFile{redditFile("v.mp4", "video/mp4", "MP4DATA")}},
			Settings{"title": "Long run", "subreddit": "running"})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.uploads) != 2 || !bytes.HasPrefix([]byte(f.uploads[1].Get("file")), []byte("\x89PNG")) {
			t.Fatalf("uploads %d", len(f.uploads))
		}
		if f.submit.Get("kind") != "video" || f.submit.Get("video_poster_url") != f.srv.URL+"/bucket/rte/cover.png" {
			t.Fatalf("submit %v", f.submit)
		}
		if res.ExternalID != "" || res.URL != "https://www.reddit.com/user/janedoe/submitted/" || f.looks != redditLooks {
			t.Fatalf("result %+v after %d looks", res, f.looks)
		}
	})

	t.Run("refused, rate limited, reauth", func(t *testing.T) {
		f := useFakeReddit(t)
		f.submitBody = `{"json":{"errors":[["SUBREDDIT_NOTALLOWED","only trusted members may post here","sr"]]}}`
		if _, err := rd.Publish(ctx, redditCreds(), PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "trusted members") {
			t.Fatalf("refused: %v", err)
		}
		f.submitBody = `{"json":{"errors":[["RATELIMIT","you are doing that too much. try again in 5 minutes.","ratelimit"]]}}`
		_, err := rd.Publish(ctx, redditCreds(), PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running"})
		if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("ratelimit is retried: %v", err)
		}
		f.submitBody, f.submitCode = "", http.StatusTooManyRequests
		_, err = rd.Publish(ctx, redditCreds(), PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running"})
		if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("429 is retried: %v", err)
		}
		c := redditCreds()
		c["access_token"] = "revoked"
		if _, err := rd.Publish(ctx, c, PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running"}); !errors.Is(err, ErrReauth) {
			t.Fatalf("revoked: %v", err)
		}
	})

	t.Run("lost answer", func(t *testing.T) {
		f := useFakeReddit(t)
		f.dropSubmit = true
		_, err := rd.Publish(ctx, redditCreds(), PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running"})
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
			t.Fatalf("lost answer: %v", err)
		}
		// The post went out before the answer was lost: it is recorded, not posted again.
		f.listed = true
		res, err := rd.Publish(ctx, redditCreds(), PostContent{Text: "x"}, Settings{"title": "Long run", "subreddit": "running"})
		if err != nil || res.ExternalID != "t3_new" || f.submits != 2 {
			t.Fatalf("found after a lost answer: %+v %v", res, err)
		}
	})
}

func TestRedditReply(t *testing.T) {
	f := useFakeReddit(t)
	rd, _ := Get("reddit")
	rep := rd.Provider.(Replier)
	res, err := rep.Reply(context.Background(), redditCreds(), "t3_abc12", PostContent{Text: "Splits below."}, nil)
	if err != nil || f.comment.Get("thing_id") != "t3_abc12" || f.comment.Get("text") != "Splits below." || !strings.HasPrefix(res.ExternalID, "t1_") {
		t.Fatalf("reply %+v %v %v", res, err, f.comment)
	}
	if _, err := rep.Reply(context.Background(), redditCreds(), res.ExternalID, PostContent{Text: "More."}, nil); err != nil || f.comment.Get("thing_id") != res.ExternalID {
		t.Fatalf("second part goes under the first: %v %v", err, f.comment)
	}
	if _, err := rep.Reply(context.Background(), redditCreds(), "t3_abc12", PostContent{Media: []MediaFile{redditFile("a.jpg", "image/jpeg", "x")}}, nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("media comment: %v", err)
	}
}

func TestRedditValidate(t *testing.T) {
	rd, _ := Get("reddit")
	ok := Settings{"title": "Long run", "subreddit": "r/running"}
	for name, tc := range map[string]struct {
		p     PostContent
		s     Settings
		field string
	}{
		"no title":         {PostContent{Text: "x"}, Settings{"subreddit": "running"}, "title"},
		"no subreddit":     {PostContent{Text: "x"}, Settings{"title": "t"}, "subreddit"},
		"bad subreddit":    {PostContent{Text: "x"}, Settings{"title": "t", "subreddit": "r/no spaces"}, "subreddit"},
		"link and media":   {PostContent{Media: []MediaFile{{MimeType: "image/png"}}}, Settings{"title": "t", "subreddit": "running", "link": "https://example.com"}, "link"},
		"text with media":  {PostContent{Text: "x", Media: []MediaFile{{MimeType: "image/png"}}}, ok, "content"},
		"two files":        {PostContent{Media: []MediaFile{{MimeType: "image/png"}, {MimeType: "image/png"}}}, ok, "media"},
		"title too long":   {PostContent{Text: "x"}, Settings{"title": strings.Repeat("a", 301), "subreddit": "running"}, "title"},
		"not a media kind": {PostContent{Media: []MediaFile{{MimeType: "application/pdf"}}}, ok, "media"},
	} {
		var ve *ValidationError
		if err := rd.Validate(tc.p, tc.s); !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("%s: %v, want a %s error", name, err, tc.field)
		}
	}
	if err := rd.Validate(PostContent{Text: "x"}, ok); err != nil {
		t.Fatalf("text post: %v", err)
	}
	if err := rd.Validate(PostContent{Media: []MediaFile{{MimeType: "video/mp4"}}}, ok); err != nil {
		t.Fatalf("video post: %v", err)
	}
}

func TestRedditCheckApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, secret, _ := r.BasicAuth(); secret != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Unauthorized","error":401}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	old := redditWWW
	redditWWW = srv.URL
	defer func() { redditWWW = old }()
	if ok, err := (Reddit{}).CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "secret"}, "https://app.example/cb"); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (Reddit{}).CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "wrong"}, "https://app.example/cb"); err == nil {
		t.Fatal("bad keys passed")
	}
}
