package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLinkedIn answers like api.linkedin.com and www.linkedin.com/oauth/v2 for the calls the provider makes.
type fakeLinkedIn struct {
	mu       sync.Mutex
	srv      *httptest.Server
	calls    []string
	form     url.Values
	scope    string // the scope the token answer grants (default: the provider's)
	post     map[string]any
	comment  map[string]any
	version  string
	puts     map[string]string // upload path -> body
	postCode int               // status for POST /rest/posts (0 = 201)
	postBody string
	polls    int
	uris     []string // request URIs as sent (escaping included)
}

func (f *fakeLinkedIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.uris = append(f.uris, r.RequestURI)
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(r.URL.Path, "/rest/") {
		f.version = r.Header.Get("LinkedIn-Version")
	}
	raw, _ := io.ReadAll(r.Body)
	switch {
	case r.URL.Path == "/oauth/v2/accessToken":
		f.form, _ = url.ParseQuery(string(raw))
		scope := f.scope
		if scope == "" {
			scope = "email,openid,profile,w_member_social"
		}
		_, _ = w.Write([]byte(`{"access_token":"at-` + f.form.Get("grant_type") + `","expires_in":5183999,"scope":"` + scope + `"}`))
	case r.URL.Path == "/v2/userinfo" && r.Header.Get("Authorization") == "Bearer revoked":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"serviceErrorCode":65601,"code":"REVOKED_ACCESS_TOKEN","status":401}`))
	case r.URL.Path == "/v2/userinfo":
		_, _ = w.Write([]byte(`{"sub":"782bbtaQ","name":"Jane Doe","picture":"https://media.licdn.com/jane.jpg"}`))
	case r.URL.Path == "/rest/images" && r.URL.Query().Get("action") == "initializeUpload":
		_, _ = w.Write([]byte(`{"value":{"uploadUrl":"` + f.srv.URL + `/upload/img","image":"urn:li:image:C4E10AQ"}}`))
	case r.URL.Path == "/rest/videos" && r.URL.Query().Get("action") == "initializeUpload":
		_, _ = w.Write([]byte(`{"value":{"video":"urn:li:video:C5F10AQ","uploadToken":"tok","uploadInstructions":[` +
			`{"uploadUrl":"` + f.srv.URL + `/upload/v1","firstByte":0,"lastByte":3},` +
			`{"uploadUrl":"` + f.srv.URL + `/upload/v2","firstByte":4,"lastByte":5}]}}`))
	case strings.HasPrefix(r.URL.Path, "/upload/"):
		f.puts[r.URL.Path] = string(raw)
		w.Header().Set("ETag", "etag-"+strings.TrimPrefix(r.URL.Path, "/upload/"))
	case r.URL.Path == "/rest/videos" && r.URL.Query().Get("action") == "finalizeUpload":
		var body map[string]map[string]any
		_ = json.Unmarshal(raw, &body)
		if ids, _ := body["finalizeUploadRequest"]["uploadedPartIds"].([]any); len(ids) != 2 || ids[0] != "etag-v1" {
			w.WriteHeader(http.StatusBadRequest)
		}
	case strings.HasPrefix(r.URL.Path, "/rest/videos/"):
		f.polls++
		if f.polls < 2 {
			_, _ = w.Write([]byte(`{"status":"PROCESSING"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"AVAILABLE"}`))
	case r.URL.Path == "/rest/posts":
		f.post = map[string]any{}
		_ = json.Unmarshal(raw, &f.post)
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
			return
		}
		w.Header().Set("X-RestLi-Id", "urn:li:share:7100")
		w.WriteHeader(http.StatusCreated)
	case strings.HasPrefix(r.URL.Path, "/rest/socialActions/"):
		f.comment = map[string]any{}
		_ = json.Unmarshal(raw, &f.comment)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"6636","$URN":"urn:li:comment:(urn:li:share:7100,6636)"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeLinkedIn(t *testing.T) *fakeLinkedIn {
	t.Helper()
	f := &fakeLinkedIn{puts: map[string]string{}}
	f.srv = httptest.NewServer(f)
	api, oauth, poll, wait := linkedinAPI, linkedinOAuth, linkedinPoll, linkedinImageWait
	linkedinAPI, linkedinOAuth, linkedinPoll, linkedinImageWait = f.srv.URL, f.srv.URL+"/oauth/v2", time.Millisecond, 0
	t.Cleanup(func() {
		f.srv.Close()
		linkedinAPI, linkedinOAuth, linkedinPoll, linkedinImageWait = api, oauth, poll, wait
	})
	return f
}

func linkedinCreds() Credentials {
	return Credentials{"access_token": "at", "person_id": "782bbtaQ", "expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
}

func TestLinkedInConnect(t *testing.T) {
	f := useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	if li.Status != StatusAvailable {
		t.Fatalf("linkedin is %q, want available", li.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, err := li.AuthURL(app, "https://app.example/cb", "st", nil)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	if got := q.Query().Get("scope"); got != "openid profile w_member_social" || q.Query().Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}

	creds, profile, err := li.Exchange(context.Background(), app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.form.Get("client_secret") != "secret" || f.form.Get("code") != "code1" || f.form.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("token form %v", f.form)
	}
	if creds["access_token"] != "at-authorization_code" || creds["person_id"] != "782bbtaQ" || creds["expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Name != "Jane Doe" || profile.AvatarURL != "https://media.licdn.com/jane.jpg" {
		t.Fatalf("profile %+v", profile)
	}

	// An app without "Share on LinkedIn" gets a token that cannot post: say what to add.
	f.scope = "openid,profile"
	if _, _, err := li.Exchange(context.Background(), app, "https://app.example/cb", "code2", nil); err == nil || !strings.Contains(err.Error(), "Share on LinkedIn") {
		t.Fatalf("token without w_member_social: %v", err)
	}
}

func TestLinkedInRefresh(t *testing.T) {
	useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	app := App{ClientID: "cid", ClientSecret: "secret"}
	// No refresh token (self-serve apps): a token still valid is kept, an expired one needs reconnecting.
	c := linkedinCreds()
	if got, err := li.Refresh(context.Background(), app, c); err != nil || got["access_token"] != "at" {
		t.Fatalf("valid token: %v %v", got, err)
	}
	// One LinkedIn no longer takes (the member removed the app) is found out too.
	if _, err := li.Refresh(context.Background(), app, Credentials{"access_token": "revoked", "expires_at": c["expires_at"]}); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked token: %v", err)
	}
	c["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if _, err := li.Refresh(context.Background(), app, c); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired token: %v", err)
	}
	// With one (partner apps) it is used.
	c["refresh_token"] = "rt"
	if got, err := li.Refresh(context.Background(), app, c); err != nil || got["access_token"] != "at-refresh_token" || got["refresh_token"] != "rt" {
		t.Fatalf("refresh: %v %v", got, err)
	}
}

func TestLinkedInPublishText(t *testing.T) {
	f := useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	res, err := li.Publish(context.Background(), linkedinCreds(), PostContent{Text: "v1.4 is out (finally) #release #dev_ops"}, Settings{"visibility": "connections"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "urn:li:share:7100" || res.URL != "https://www.linkedin.com/feed/update/urn:li:share:7100/" {
		t.Fatalf("result %+v", res)
	}
	if f.post["author"] != "urn:li:person:782bbtaQ" || f.post["visibility"] != "CONNECTIONS" || f.post["lifecycleState"] != "PUBLISHED" {
		t.Fatalf("post %v", f.post)
	}
	if want := `v1.4 is out \(finally\) {hashtag|\#|release} {hashtag|\#|dev\_ops}`; f.post["commentary"] != want {
		t.Fatalf("commentary %q, want %q", f.post["commentary"], want)
	}
	if _, ok := f.post["content"]; ok {
		t.Fatalf("a text post has no content: %v", f.post)
	}
	if f.version != linkedinVersion {
		t.Fatalf("LinkedIn-Version %q", f.version)
	}
}

func TestLinkedInPublishMedia(t *testing.T) {
	f := useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	pic := func(name string) MediaFile {
		return MediaFile{Name: name, MimeType: "image/png", Size: 3, Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("png")), nil
		}}
	}
	if _, err := li.Publish(context.Background(), linkedinCreds(), PostContent{Text: "two", Media: []MediaFile{pic("a.png"), pic("b.png")}}, nil); err != nil {
		t.Fatal(err)
	}
	images := f.post["content"].(map[string]any)["multiImage"].(map[string]any)["images"].([]any)
	if len(images) != 2 || f.puts["/upload/img"] != "png" {
		t.Fatalf("multiImage %v, puts %v", images, f.puts)
	}

	video := MediaFile{Name: "clip.mp4", MimeType: "video/mp4", Size: 6, Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("abcdef")), nil
	}}
	if _, err := li.Publish(context.Background(), linkedinCreds(), PostContent{Media: []MediaFile{video}}, nil); err != nil {
		t.Fatal(err)
	}
	// The parts LinkedIn asked for, by their byte ranges; the post waits until the video is AVAILABLE.
	if !slices.Contains(f.uris, "/rest/videos/urn%3Ali%3Avideo%3AC5F10AQ") {
		t.Fatalf("video status path: %v", f.uris)
	}
	if f.puts["/upload/v1"] != "abcd" || f.puts["/upload/v2"] != "ef" || f.polls < 2 {
		t.Fatalf("puts %v, polls %d", f.puts, f.polls)
	}
	if id := f.post["content"].(map[string]any)["media"].(map[string]any)["id"]; id != "urn:li:video:C5F10AQ" {
		t.Fatalf("media %v", f.post["content"])
	}

	if err := li.Validate(PostContent{Media: []MediaFile{video, pic("a.png")}}, nil); err == nil {
		t.Fatal("a video with a picture must be refused")
	}
}

func TestLinkedInDuplicateIsThePost(t *testing.T) {
	f := useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	// A retry after a lost answer: LinkedIn refuses it as a duplicate and names the post that went out.
	f.postCode, f.postBody = http.StatusUnprocessableEntity, `{"status":422,"message":"Content is a duplicate of urn:li:share:7099"}`
	res, err := li.Publish(context.Background(), linkedinCreds(), PostContent{Text: "again"}, nil)
	if err != nil || res.ExternalID != "urn:li:share:7099" {
		t.Fatalf("duplicate: %+v %v", res, err)
	}
}

func TestLinkedInErrors(t *testing.T) {
	f := useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	for _, tc := range []struct {
		code int
		body string
		kind error
	}{
		{http.StatusUnauthorized, `{"status":401,"serviceErrorCode":65601,"code":"REVOKED_ACCESS_TOKEN"}`, ErrReauth},
		{http.StatusTooManyRequests, `{"status":429}`, nil},
		{http.StatusBadGateway, `bad gateway`, nil},
		{http.StatusUnprocessableEntity, `{"status":422,"message":"/commentary :: text length exceeds the maximum"}`, ErrRejected},
		{http.StatusForbidden, `{"status":403,"message":"Not enough permissions to access: partnerApiPostsExternal.CREATE"}`, ErrRejected},
	} {
		f.postCode, f.postBody = tc.code, tc.body
		_, err := li.Publish(context.Background(), linkedinCreds(), PostContent{Text: "hi"}, nil)
		var fail *Failure
		if !errors.As(err, &fail) || fail.Kind != tc.kind {
			t.Fatalf("%d: %v, want kind %v", tc.code, err, tc.kind)
		}
	}
}

func TestLinkedInComments(t *testing.T) {
	f := useFakeLinkedIn(t)
	li, _ := Get("linkedin")
	r := li.Provider.(Replier)
	first, err := r.Reply(context.Background(), linkedinCreds(), "urn:li:share:7100", PostContent{Text: "Links in the comments: example.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.comment["object"] != "urn:li:share:7100" || f.comment["actor"] != "urn:li:person:782bbtaQ" {
		t.Fatalf("comment %v", f.comment)
	}
	// LinkedIn refuses the URN with plain colons in the path ("Syntax exception in path variables").
	if last := f.uris[len(f.uris)-1]; last != "/rest/socialActions/urn%3Ali%3Ashare%3A7100/comments" {
		t.Fatalf("comment path %s", last)
	}
	// The next part names the comment before; it still comments on the post.
	if _, err := r.Reply(context.Background(), linkedinCreds(), first.ExternalID, PostContent{Text: "and one more"}, nil); err != nil {
		t.Fatal(err)
	}
	if f.comment["object"] != "urn:li:share:7100" {
		t.Fatalf("second comment on %v", f.comment["object"])
	}
}
