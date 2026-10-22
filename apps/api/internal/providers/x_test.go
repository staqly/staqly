package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeX answers like api.x.com for the calls the X provider makes.
type fakeX struct {
	mu        sync.Mutex
	calls     []string
	tweet     map[string]any
	tweetRaw  string // the last POST /2/tweets body as sent
	form      url.Values
	basicUser string
	tweetCode int // status for POST /2/tweets (0 = 201)
	tweetBody string
	appends   int
	timeline  string // the text of the account's latest post (GET /2/users/42/tweets)
}

func (f *fakeX) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/2/oauth2/token":
		_ = r.ParseForm()
		f.form = r.PostForm
		f.basicUser, _, _ = r.BasicAuth()
		_, _ = w.Write([]byte(`{"access_token":"at-` + f.form.Get("grant_type") + `","refresh_token":"rt-new","expires_in":7200,"scope":"tweet.write"}`))
	case r.URL.Path == "/2/users/me":
		if r.Header.Get("Authorization") != "Bearer at-authorization_code" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"42","name":"Jump The Bear","username":"jumpthebear","profile_image_url":"https://pbs.twimg.com/p/a_normal.jpg"}}`))
	case r.URL.Path == "/2/media/upload" && r.Method == http.MethodPost:
		if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("media_category") != "tweet_image" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"img1"}}`))
	case r.URL.Path == "/2/media/upload/initialize":
		_, _ = w.Write([]byte(`{"data":{"id":"vid1"}}`))
	case strings.HasSuffix(r.URL.Path, "/append"):
		f.appends++
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(r.URL.Path, "/finalize"):
		_, _ = w.Write([]byte(`{"data":{"id":"vid1","processing_info":{"state":"pending","check_after_secs":1}}}`))
	case r.URL.Path == "/2/media/upload" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"data":{"processing_info":{"state":"succeeded"}}}`))
	case strings.HasPrefix(r.URL.Path, "/2/users/") && strings.HasSuffix(r.URL.Path, "/retweets"):
		raw, _ := io.ReadAll(r.Body)
		f.tweetRaw = string(raw)
		_, _ = w.Write([]byte(`{"data":{"retweeted":true}}`))
	case r.URL.Path == "/2/tweets":
		// Each request decodes into a fresh map: decoding into the previous one would keep its keys
		// (a reply_settings from an earlier tweet), which once made this test fail for the wrong reason.
		raw, _ := io.ReadAll(r.Body)
		tweet := map[string]any{}
		_ = json.Unmarshal(raw, &tweet)
		f.tweet, f.tweetRaw = tweet, string(raw)
		if f.tweetCode != 0 {
			w.WriteHeader(f.tweetCode)
			_, _ = w.Write([]byte(f.tweetBody))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"1790000000000000000","text":"hi"}}`))
	case r.URL.Path == "/2/users/42/tweets" && f.timeline != "":
		b, _ := json.Marshal(map[string]any{"data": []map[string]string{{"id": "1789", "text": f.timeline}}})
		_, _ = w.Write(b)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// last returns the last tweet the fake received, read under its lock.
func (f *fakeX) last() (map[string]any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tweet, f.tweetRaw
}

func withFakeX(t *testing.T) *fakeX {
	t.Helper()
	f := &fakeX{}
	srv := httptest.NewServer(f)
	oldAPI, oldAuth, oldPoll := xAPI, xAuthorize, xPoll
	xAPI, xAuthorize, xPoll = srv.URL, srv.URL+"/i/oauth2/authorize", time.Millisecond
	t.Cleanup(func() { srv.Close(); xAPI, xAuthorize, xPoll = oldAPI, oldAuth, oldPoll })
	return f
}

func TestXConnect(t *testing.T) {
	f := withFakeX(t)
	e, ok := Get("x")
	if !ok || e.Status != StatusAvailable || e.Auth() != AuthOAuth2 {
		t.Fatalf("X should be an available OAuth provider: %+v", e)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	verifier := strings.Repeat("v", 43)

	if _, err := e.AuthURL(app, "https://app.test/channels/callback/x", "st", nil); err == nil {
		t.Fatal("no verifier must be refused")
	}
	raw, err := e.AuthURL(app, "https://app.test/channels/callback/x", "st", map[string]string{"code_verifier": verifier})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	sum := sha256.Sum256([]byte(verifier))
	q := u.Query()
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) || q.Get("code_challenge_method") != "S256" ||
		q.Get("scope") != xScopes || q.Get("state") != "st" || q.Get("client_id") != "cid" {
		t.Fatalf("auth URL: %s", raw)
	}

	creds, profile, err := e.Exchange(context.Background(), app, "https://app.test/channels/callback/x", "the-code", map[string]string{"code_verifier": verifier})
	if err != nil {
		t.Fatal(err)
	}
	if f.form.Get("code_verifier") != verifier || f.form.Get("code") != "the-code" || f.basicUser != "cid" || f.form.Get("client_id") != "" {
		t.Fatalf("token request: %v basic=%q", f.form, f.basicUser)
	}
	if profile.Handle != "@jumpthebear" || profile.ProfileURL != "https://x.com/jumpthebear" || !strings.Contains(profile.AvatarURL, "_400x400.") {
		t.Fatalf("profile: %+v", profile)
	}
	if creds["username"] != "jumpthebear" || creds["refresh_token"] != "rt-new" || creds["expires_at"] == "" {
		t.Fatalf("creds: %v", creds)
	}

	// Refresh keeps what the token reply does not carry (the username) and takes the rotated refresh token.
	fresh, err := e.Refresh(context.Background(), App{ClientID: "public"}, Credentials{"refresh_token": "rt-old", "username": "jumpthebear"})
	if err != nil {
		t.Fatal(err)
	}
	if f.form.Get("refresh_token") != "rt-old" || f.form.Get("client_id") != "public" || fresh["access_token"] != "at-refresh_token" ||
		fresh["refresh_token"] != "rt-new" || fresh["username"] != "jumpthebear" {
		t.Fatalf("refresh: form=%v creds=%v", f.form, fresh)
	}
	if _, err := e.Refresh(context.Background(), app, Credentials{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("no refresh token should need reconnecting: %v", err)
	}
}

func TestXPublish(t *testing.T) {
	f := withFakeX(t)
	e, _ := Get("x")
	ctx := context.Background()
	creds := Credentials{"access_token": "at", "username": "jumpthebear"}
	dir := t.TempDir()
	img := filepath.Join(dir, "cover.png")
	vid := filepath.Join(dir, "clip.mp4")
	_ = os.WriteFile(img, []byte("png"), 0o600)
	_ = os.WriteFile(vid, make([]byte, xChunkSize+10), 0o600) // two chunks

	// Text only, with who can reply and a community.
	res, err := e.Publish(ctx, creds, PostContent{Text: "Hello X"}, Settings{"reply_settings": "following", "community": "https://x.com/i/communities/1493446837214187523/"})
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://x.com/jumpthebear/status/1790000000000000000" || res.ExternalID != "1790000000000000000" {
		t.Fatalf("result: %+v", res)
	}
	if tw, _ := f.last(); tw["text"] != "Hello X" || tw["reply_settings"] != "following" || tw["community_id"] != "1493446837214187523" || tw["media"] != nil {
		t.Fatalf("tweet: %v", tw)
	}

	// A picture: one upload, then the tweet carries its id; "everyone" sends no reply_settings.
	if _, err := e.Publish(ctx, creds, PostContent{Text: "pic", Media: []MediaFile{{Name: "cover.png", MimeType: "image/png", Path: img, Size: 3}}}, Settings{"reply_settings": "everyone"}); err != nil {
		t.Fatal(err)
	}
	tw, raw := f.last()
	if ids := tw["media"].(map[string]any)["media_ids"].([]any); len(ids) != 1 || ids[0] != "img1" || tw["reply_settings"] != nil || tw["community_id"] != nil {
		t.Fatalf("tweet with media: %v", tw)
	}
	// What was sent, not only what the fake kept: "everyone" means no reply_settings on the wire.
	if strings.Contains(raw, "reply_settings") || strings.Contains(raw, "community_id") {
		t.Fatalf("tweet with media sent stale settings: %s", raw)
	}

	// A video: initialize, two appends, finalize, a status check while processing.
	if _, err := e.Publish(ctx, creds, PostContent{Text: "clip", Media: []MediaFile{{Name: "clip.mp4", MimeType: "video/mp4", Path: vid, Size: xChunkSize + 10}}}, nil); err != nil {
		t.Fatal(err)
	}
	if tw, _ := f.last(); f.appends != 2 || tw["media"].(map[string]any)["media_ids"].([]any)[0] != "vid1" {
		t.Fatalf("video upload: appends=%d tweet=%v calls=%v", f.appends, tw, f.calls)
	}

	// 401: reconnect. 429: retry (no kind). Duplicate: refused for good.
	for _, tc := range []struct {
		code int
		body string
		kind error
		msg  string
	}{
		{401, `{"title":"Unauthorized","status":401}`, ErrReauth, "reconnect"},
		{429, `{"title":"Too Many Requests"}`, nil, "rate limit"},
		{403, `{"detail":"You are not allowed to create a Tweet with duplicate content.","status":403}`, ErrRejected, "already posted"},
		{503, `Service Unavailable`, nil, "unavailable"},
	} {
		f.tweetCode, f.tweetBody = tc.code, tc.body
		_, err := e.Publish(ctx, creds, PostContent{Text: "again"}, nil)
		var fail *Failure
		if !errors.As(err, &fail) || !strings.Contains(err.Error(), tc.msg) || (tc.kind != nil && !errors.Is(err, tc.kind)) ||
			(tc.kind == nil && (errors.Is(err, ErrReauth) || errors.Is(err, ErrRejected))) {
			t.Errorf("%d: got %v", tc.code, err)
		}
	}

	// A retry after a lost answer is refused as a duplicate; the account's latest post with the text is the
	// post, published (X writes & as &amp; and appends the media link).
	f.timeline = "Q&amp;A is live https://t.co/abc"
	f.tweetCode, f.tweetBody = 403, `{"detail":"You are not allowed to create a Tweet with duplicate content.","status":403}`
	connected := Credentials{"access_token": "at", "username": "jumpthebear", "user_id": "42"}
	if res, err := e.Publish(ctx, connected, PostContent{Text: "Q&A is live"}, nil); err != nil || res.ExternalID != "1789" {
		t.Fatalf("duplicate after a lost answer: %+v %v", res, err)
	}
	f.timeline = ""

	// Disclosures ride on the post as booleans; off sends nothing.
	f.tweetCode = 0
	if _, err := e.Publish(ctx, creds, PostContent{Text: "ai"}, Settings{"made_with_ai": "true", "paid_partnership": "true"}); err != nil {
		t.Fatal(err)
	}
	if tw, _ := f.last(); tw["made_with_ai"] != true || tw["paid_partnership"] != true {
		t.Fatalf("disclosures: %v", tw)
	}
	if _, err := e.Publish(ctx, creds, PostContent{Text: "plain"}, Settings{"made_with_ai": "false"}); err != nil {
		t.Fatal(err)
	}
	if _, raw := f.last(); strings.Contains(raw, "made_with_ai") || strings.Contains(raw, "paid_partnership") {
		t.Fatalf("disclosures off still sent: %s", raw)
	}
}

// Another account reposts a published post as itself.
func TestXRepost(t *testing.T) {
	f := withFakeX(t)
	e, _ := Get("x")
	r, ok := e.Provider.(Reposter)
	if !ok {
		t.Fatal("X should repost")
	}
	if err := r.Repost(context.Background(), Credentials{"access_token": "at", "user_id": "42"}, "1790000000000000000"); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "POST /2/users/42/retweets" || f.tweetRaw != `{"tweet_id":"1790000000000000000"}` {
		t.Fatalf("repost: %v %s", f.calls, f.tweetRaw)
	}
	if err := r.Repost(context.Background(), Credentials{"access_token": "at"}, "1"); !errors.Is(err, ErrReauth) {
		t.Fatalf("no account id: %v", err)
	}
}

// A thread part is a reply under the tweet before it: no reply settings or community of its own.
func TestXReply(t *testing.T) {
	f := withFakeX(t)
	e, _ := Get("x")
	r, ok := e.Provider.(Replier)
	if !ok {
		t.Fatal("X should take replies")
	}
	creds := Credentials{"access_token": "at", "username": "jumpthebear"}
	res, err := r.Reply(context.Background(), creds, "1790000000000000000", PostContent{Text: "part two"}, Settings{"reply_settings": "following", "community": "1493446837214187523"})
	if err != nil || res.ExternalID == "" {
		t.Fatalf("reply: %+v %v", res, err)
	}
	tw, raw := f.last()
	reply, _ := tw["reply"].(map[string]any)
	if tw["text"] != "part two" || reply["in_reply_to_tweet_id"] != "1790000000000000000" || strings.Contains(raw, "reply_settings") || strings.Contains(raw, "community_id") {
		t.Fatalf("reply body: %s", raw)
	}
	if _, err := r.Reply(context.Background(), creds, "", PostContent{Text: "x"}, nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("no parent: %v", err)
	}
}

func TestXValidate(t *testing.T) {
	e, _ := Get("x")
	vid := MediaFile{ID: "v", MimeType: "video/mp4"}
	pic := MediaFile{ID: "p", MimeType: "image/png"}
	for _, tc := range []struct {
		p     PostContent
		s     Settings
		field string
	}{
		{PostContent{Text: strings.Repeat("a", 281)}, nil, "content"},
		{PostContent{Text: "a", Media: []MediaFile{vid, pic}}, nil, "media"},
		{PostContent{Text: "a"}, Settings{"community": "https://x.com/home"}, "community"},
		{PostContent{Text: "a"}, Settings{"reply_settings": "nobody"}, "reply_settings"},
		{PostContent{Text: "a"}, Settings{"made_with_ai": "yes"}, "made_with_ai"},
		{PostContent{Text: "a"}, Settings{"thread_finisher": strings.Repeat("a", 281)}, "thread_finisher"},
		{PostContent{Text: "a"}, Settings{"reposters": "c1,c2,c3,c4,c5,c6,c7,c8,c9,c10,c11"}, "reposters"},
	} {
		var ve *ValidationError
		if err := e.Validate(tc.p, tc.s); !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("want %s, got %v", tc.field, err)
		}
	}
	if err := e.Validate(PostContent{Text: "ok", Media: []MediaFile{pic, pic, pic, pic}}, Settings{"reply_settings": "verified"}); err != nil {
		t.Fatal(err)
	}
}
