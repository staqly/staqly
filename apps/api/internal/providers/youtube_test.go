package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// fakeGoogle answers like oauth2.googleapis.com, openidconnect.googleapis.com, the YouTube Data API and the
// Business Profile APIs for the calls the providers make.
type fakeGoogle struct {
	mu       sync.Mutex
	srv      *httptest.Server
	calls    []string
	form     url.Values
	scope    string // the scope the token answer grants (default: everything asked)
	tokenErr string // a token endpoint error body (400)

	// YouTube
	channels  string // the channels?mine=true items (JSON list)
	meta      map[string]any
	uploadHdr http.Header
	video     []byte // bytes the upload session has
	size      int64
	dropFinal bool // close the connection on the last chunk without answering
	dropProbe bool // also close it on the probe that follows
	partial   bool // commit one byte less than the first chunk sent
	uploadErr int  // status for the session start (0 = 200)
	uploadMsg string
	playlist  map[string]any
	plOwner   string // the channel the playlist belongs to ("" = none found)
	comment   map[string]any
	noYTA     bool // YouTube Analytics refuses (a connection without yt-analytics.readonly)

	// Google Business Profile
	post     map[string]any
	postCode int
	postBody string
	dropPost bool
}

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/token" && r.Header.Get("Authorization") == "Bearer revoked" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"Request had invalid authentication credentials.","status":"UNAUTHENTICATED"}}`))
		return
	}
	switch {
	case r.URL.Path == "/token":
		f.form, _ = url.ParseQuery(string(raw))
		if f.tokenErr != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(f.tokenErr))
			return
		}
		scope := f.scope
		if scope == "" {
			scope = youtubeScopes + " " + gbpScope
		}
		refresh := ""
		if f.form.Get("grant_type") == "authorization_code" {
			refresh = `"refresh_token":"rt-1",`
		}
		_, _ = w.Write([]byte(`{"access_token":"at-` + f.form.Get("grant_type") + `",` + refresh + `"expires_in":3599,"scope":"` + scope + `","token_type":"Bearer"}`))
	case r.URL.Path == "/v1/userinfo":
		_, _ = w.Write([]byte(`{"sub":"1080","name":"Jane Doe","email":"jane@example.com","picture":"https://example.com/jane.jpg"}`))
	case r.URL.Path == "/youtube/v3/channels":
		if r.URL.Query().Get("mine") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"items":` + f.channels + `}`))
	case r.URL.Path == "/youtube/v3/playlists":
		if f.plOwner == "" {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"` + r.URL.Query().Get("id") + `","snippet":{"channelId":"` + f.plOwner + `"}}]}`))
	case r.URL.Path == "/youtube/v3/playlistItems":
		f.playlist = map[string]any{}
		_ = json.Unmarshal(raw, &f.playlist)
		_, _ = w.Write([]byte(`{"id":"pli"}`))
	case r.URL.Path == "/upload/youtube/v3/videos" && r.Method == http.MethodPost:
		if r.URL.Query().Get("uploadType") != "resumable" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if f.uploadErr != 0 {
			w.WriteHeader(f.uploadErr)
			_, _ = w.Write([]byte(f.uploadMsg))
			return
		}
		f.meta = map[string]any{}
		_ = json.Unmarshal(raw, &f.meta)
		f.uploadHdr = r.Header.Clone()
		f.size, _ = strconv.ParseInt(r.Header.Get("X-Upload-Content-Length"), 10, 64)
		f.video = nil
		w.Header().Set("Location", f.srv.URL+"/upload/session/1")
	case r.URL.Path == "/upload/session/1" && r.Method == http.MethodPut:
		cr := r.Header.Get("Content-Range")
		if strings.HasPrefix(cr, "bytes */") { // a probe
			if f.dropProbe {
				hijack(w)
				return
			}
			f.uploadAnswer(w)
			return
		}
		var start, end, total int64
		_, _ = fmt.Sscanf(cr, "bytes %d-%d/%d", &start, &end, &total)
		if start != int64(len(f.video)) || int64(len(raw)) != end-start+1 || total != f.size {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad range ` + cr + `"}}`))
			return
		}
		if f.partial && start == 0 && len(raw) > 1 {
			f.partial = false
			raw = raw[:len(raw)-1]
		}
		f.video = append(f.video, raw...)
		if int64(len(f.video)) == f.size && f.dropFinal {
			hijack(w)
			return
		}
		f.uploadAnswer(w)
	case r.URL.Path == "/youtube/v3/commentThreads":
		f.comment = map[string]any{}
		_ = json.Unmarshal(raw, &f.comment)
		if strings.Contains(string(raw), "kids") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"The video identified by the <code>videoId</code> parameter has disabled comments.","errors":[{"reason":"commentsDisabled"}]}}`))
			return
		}
		if strings.Contains(string(raw), "private video") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"The comment thread could not be created due to insufficient permissions. The request might not be properly authorized.","errors":[{"reason":"forbidden"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"Ugz1"}`))
	case r.URL.Path == "/youtube/v3/videos":
		_, _ = w.Write([]byte(`{"items":[{"id":"vid1","statistics":{"viewCount":"1200","likeCount":"30","commentCount":"4"}}]}`))
	case r.URL.Path == "/v2/reports":
		if f.noYTA || r.URL.Query().Get("filters") != "video==vid1" || r.URL.Query().Get("ids") != "channel==MINE" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Insufficient permission","status":"PERMISSION_DENIED"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"columnHeaders":[{"name":"estimatedMinutesWatched"},{"name":"averageViewDuration"},{"name":"shares"},{"name":"subscribersGained"}],"rows":[[340,71.6,5,2]]}`))

	// Google Business Profile
	case r.URL.Path == "/v1/accounts":
		_, _ = w.Write([]byte(`{"accounts":[{"name":"accounts/111"},{"name":"accounts/222"}]}`))
	case r.URL.Path == "/v1/accounts/111/locations":
		if r.URL.Query().Get("readMask") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"locations":[{"name":"locations/9001","title":"Jane's Bakery","storefrontAddress":{"locality":"Springfield"},"metadata":{"mapsUri":"https://maps.google.com/?cid=1"}}]}`))
	case r.URL.Path == "/v1/accounts/222/locations":
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`))
	case r.URL.Path == "/v1/locations/9001":
		_, _ = w.Write([]byte(`{"name":"locations/9001","title":"Jane's Bakery","storefrontAddress":{"locality":"Springfield"},"metadata":{"mapsUri":"https://maps.google.com/?cid=1"}}`))
	case r.URL.Path == "/v4/accounts/111/locations/9001/localPosts":
		f.post = map[string]any{}
		_ = json.Unmarshal(raw, &f.post)
		if f.dropPost {
			hijack(w)
			return
		}
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
			return
		}
		_, _ = w.Write([]byte(`{"name":"accounts/111/locations/9001/localPosts/555","state":"LIVE","searchUrl":"https://local.google.com/place?id=1&use=posts&lpsid=555"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found: ` + r.URL.Path + `"}}`))
	}
}

// uploadAnswer: the video once the session has every byte, otherwise 308 with what it has.
func (f *fakeGoogle) uploadAnswer(w http.ResponseWriter) {
	if int64(len(f.video)) == f.size {
		_, _ = w.Write([]byte(`{"id":"vid1","status":{"privacyStatus":"public"}}`))
		return
	}
	if len(f.video) > 0 {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(f.video)-1))
	}
	w.WriteHeader(http.StatusPermanentRedirect)
}

// hijack closes the connection without an answer.
func hijack(w http.ResponseWriter) {
	hj, _ := w.(http.Hijacker)
	c, _, _ := hj.Hijack()
	_ = c.Close()
}

func useFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	f := &fakeGoogle{channels: `[{"id":"UC1","snippet":{"title":"Jane Runs","customUrl":"@janeruns","thumbnails":{"default":{"url":"https://example.com/c.jpg"}}}}]`}
	f.srv = httptest.NewServer(f)
	old := []string{googleAuthorize, googleTokenURL, googleUserinfo, youtubeAPI, gbpAccountsAPI, gbpInfoAPI, gbpPostsAPI, youtubeAnalyticsAPI}
	chunk := youtubeChunk
	googleAuthorize, googleTokenURL, googleUserinfo = f.srv.URL+"/auth", f.srv.URL+"/token", f.srv.URL+"/v1/userinfo"
	youtubeAPI, gbpAccountsAPI, gbpInfoAPI, gbpPostsAPI, youtubeAnalyticsAPI = f.srv.URL, f.srv.URL, f.srv.URL, f.srv.URL, f.srv.URL
	youtubeChunk = 4
	t.Cleanup(func() {
		f.srv.Close()
		googleAuthorize, googleTokenURL, googleUserinfo, youtubeAPI = old[0], old[1], old[2], old[3]
		gbpAccountsAPI, gbpInfoAPI, gbpPostsAPI, youtubeAnalyticsAPI = old[4], old[5], old[6], old[7]
		youtubeChunk = chunk
	})
	return f
}

func youtubeCreds() Credentials {
	return Credentials{"access_token": "at", "refresh_token": "rt-1", "channel_id": "UC1",
		"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}

func youtubeVideo(content string) MediaFile {
	return MediaFile{ID: "m1", Name: "run.mp4", MimeType: "video/mp4", Size: int64(len(content)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(content)), nil }}
}

func failureKind(t *testing.T, err error) error {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("not a Failure: %v", err)
	}
	return f.Kind
}

func TestYouTubeConnect(t *testing.T) {
	f := useFakeGoogle(t)
	yt, _ := Get("youtube")
	if yt.Status != StatusAvailable {
		t.Fatalf("youtube is %q, want available", yt.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, err := yt.AuthURL(app, "http://localhost:8080/channels/callback/youtube", "st", map[string]string{"code_verifier": "verifier"})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	if q.Query().Get("access_type") != "offline" || q.Query().Get("prompt") != "consent select_account" || q.Query().Get("code_challenge") == "" ||
		!strings.Contains(q.Query().Get("scope"), "youtube.upload") || q.Query().Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}

	creds, profile, err := yt.Exchange(context.Background(), app, "http://localhost:8080/channels/callback/youtube", "code1", map[string]string{"code_verifier": "verifier"})
	if err != nil {
		t.Fatal(err)
	}
	if f.form.Get("client_secret") != "secret" || f.form.Get("code") != "code1" || f.form.Get("code_verifier") != "verifier" {
		t.Fatalf("token form %v", f.form)
	}
	if creds["access_token"] != "at-authorization_code" || creds["refresh_token"] != "rt-1" || creds["expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Name != "Jane Doe" || profile.Handle != "jane@example.com" {
		t.Fatalf("profile %+v", profile)
	}

	lister := yt.Provider.(AccountLister)
	accounts, err := lister.Accounts(context.Background(), creds)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "UC1" || accounts[0].Handle != "@janeruns" ||
		accounts[0].ProfileURL != "https://www.youtube.com/@janeruns" {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	ch, chProfile, err := lister.UseAccount(context.Background(), creds, "UC1")
	if err != nil || ch["channel_id"] != "UC1" || ch["refresh_token"] != "rt-1" || chProfile.Name != "Jane Runs" || chProfile.NoComments {
		t.Fatalf("use account %v %+v %v", ch, chProfile, err)
	}
	if _, _, err := lister.UseAccount(context.Background(), creds, "UC-other"); failureKind(t, err) != ErrRejected {
		t.Fatalf("someone else's channel: %v", err)
	}

	// A refresh keeps the refresh token (Google sends none back) and the channel.
	fresh, err := yt.Refresh(context.Background(), app, ch)
	if err != nil || fresh["access_token"] != "at-refresh_token" || fresh["refresh_token"] != "rt-1" || fresh["channel_id"] != "UC1" {
		t.Fatalf("refresh %v %v", fresh, err)
	}
	f.tokenErr = `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`
	if _, err := yt.Refresh(context.Background(), app, ch); failureKind(t, err) != ErrReauth {
		t.Fatalf("revoked refresh: %v", err)
	}
	f.tokenErr = ""

	// No channel on the Google account, or upload unticked on the consent screen.
	f.channels = `[]`
	if _, err := lister.Accounts(context.Background(), creds); failureKind(t, err) != ErrRejected || !strings.Contains(err.Error(), "no YouTube channel") {
		t.Fatalf("no channel: %v", err)
	}
	f.scope = "openid email profile https://www.googleapis.com/auth/youtube.readonly"
	if _, _, err := yt.Exchange(context.Background(), app, "http://localhost:8080/cb", "code2", nil); failureKind(t, err) != ErrRejected {
		t.Fatalf("no upload scope: %v", err)
	}
}

func TestYouTubePublish(t *testing.T) {
	f := useFakeGoogle(t)
	f.plOwner = "UC1"
	yt, _ := Get("youtube")
	content := "0123456789" // three chunks of 4, 4 and 2 bytes
	s := Settings{"title": "Morning run", "privacy": "unlisted", "made_for_kids": "false", "tags": "running, sport", "category": "17",
		"playlist": "https://www.youtube.com/playlist?list=PL123"}
	p := PostContent{Text: "10 km before breakfast", Media: []MediaFile{youtubeVideo(content)}}
	if err := yt.Validate(p, s); err != nil {
		t.Fatal(err)
	}
	res, err := yt.Publish(context.Background(), youtubeCreds(), p, s)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "vid1" || res.URL != "https://www.youtube.com/watch?v=vid1" || string(f.video) != content {
		t.Fatalf("result %+v, uploaded %q", res, f.video)
	}
	snippet, _ := f.meta["snippet"].(map[string]any)
	status, _ := f.meta["status"].(map[string]any)
	if snippet["title"] != "Morning run" || snippet["description"] != "10 km before breakfast" || snippet["categoryId"] != "17" ||
		fmt.Sprint(snippet["tags"]) != "[running sport]" || status["privacyStatus"] != "unlisted" || status["selfDeclaredMadeForKids"] != false {
		t.Fatalf("metadata %v", f.meta)
	}
	if f.uploadHdr.Get("X-Upload-Content-Length") != "10" || f.uploadHdr.Get("X-Upload-Content-Type") != "video/mp4" {
		t.Fatalf("upload headers %v", f.uploadHdr)
	}
	pl, _ := f.playlist["snippet"].(map[string]any)
	if pl["playlistId"] != "PL123" {
		t.Fatalf("playlist item %v", f.playlist)
	}

	// YouTube takes fewer bytes than sent: the rest goes again from where it stopped.
	f.partial = true
	if res, err := yt.Publish(context.Background(), youtubeCreds(), p, Settings{"title": "Morning run"}); err != nil || string(f.video) != content || res.ExternalID != "vid1" {
		t.Fatalf("partial chunk: %v %q", err, f.video)
	}

	// A playlist of another channel is refused before anything is uploaded.
	f.plOwner = "UC-other"
	f.calls = nil
	if _, err := yt.Publish(context.Background(), youtubeCreds(), p, s); failureKind(t, err) != ErrRejected || strings.Contains(strings.Join(f.calls, " "), "upload") {
		t.Fatalf("foreign playlist: %v %v", err, f.calls)
	}
}

// The answer to the last chunk is lost: staqly asks the session, and only when that fails too says it may be out.
func TestYouTubeLostAnswer(t *testing.T) {
	f := useFakeGoogle(t)
	yt, _ := Get("youtube")
	p := PostContent{Text: "", Media: []MediaFile{youtubeVideo("0123456789")}}
	s := Settings{"title": "Morning run"}

	f.dropFinal = true
	res, err := yt.Publish(context.Background(), youtubeCreds(), p, s)
	if err != nil || res.ExternalID != "vid1" {
		t.Fatalf("probe after a lost answer: %+v %v", res, err)
	}

	f.dropProbe = true
	_, err = yt.Publish(context.Background(), youtubeCreds(), p, s)
	if failureKind(t, err) != ErrRejected || !strings.Contains(err.Error(), "may have published") || strings.Contains(err.Error(), "session/1") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestYouTubeFailures(t *testing.T) {
	f := useFakeGoogle(t)
	yt, _ := Get("youtube")
	p := PostContent{Media: []MediaFile{youtubeVideo("0123")}}
	s := Settings{"title": "Morning run"}

	c := youtubeCreds()
	c["access_token"] = "revoked"
	if _, err := yt.Publish(context.Background(), c, p, s); failureKind(t, err) != ErrReauth {
		t.Fatalf("revoked: %v", err)
	}
	for _, tc := range []struct {
		status int
		body   string
		kind   error
		says   string
	}{
		{403, `{"error":{"code":403,"message":"The request cannot be completed because you have exceeded your quota.","errors":[{"reason":"quotaExceeded","domain":"youtube.quota"}]}}`, ErrRejected, "daily limit"},
		{403, `{"error":{"code":403,"message":"YouTube Data API v3 has not been used in project 1 before or it is disabled.","status":"PERMISSION_DENIED","details":[{"reason":"SERVICE_DISABLED"}]}}`, ErrRejected, "not turned on"},
		{401, `{"error":{"code":401,"message":"Unauthorized","errors":[{"reason":"youtubeSignupRequired"}]}}`, ErrRejected, "no YouTube channel"},
		{429, `{"error":{"code":429,"message":"Too many requests","errors":[{"reason":"rateLimitExceeded"}]}}`, nil, "rate limit"},
		{503, `{"error":{"code":503,"message":"Backend Error"}}`, nil, "unavailable"},
		{400, `{"error":{"code":400,"message":"The request metadata specifies an invalid video title.","errors":[{"reason":"invalidTitle"}]}}`, ErrRejected, "invalid video title"},
	} {
		f.uploadErr, f.uploadMsg = tc.status, tc.body
		_, err := yt.Publish(context.Background(), youtubeCreds(), p, s)
		if failureKind(t, err) != tc.kind || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%d %s: %v", tc.status, tc.body, err)
		}
	}
}

func TestYouTubeReplyAndMetrics(t *testing.T) {
	f := useFakeGoogle(t)
	yt, _ := Get("youtube")
	r := yt.Provider.(Replier)
	res, err := r.Reply(context.Background(), youtubeCreds(), "vid1", PostContent{Text: "Route in the description"}, nil)
	if err != nil || res.ExternalID != "comment:vid1:Ugz1" {
		t.Fatalf("reply %+v %v", res, err)
	}
	sn, _ := f.comment["snippet"].(map[string]any)
	if sn["videoId"] != "vid1" || sn["channelId"] != "UC1" {
		t.Fatalf("comment %v", f.comment)
	}
	// The second part goes under the video too.
	if _, err := r.Reply(context.Background(), youtubeCreds(), res.ExternalID, PostContent{Text: "Thanks"}, nil); err != nil {
		t.Fatal(err)
	}
	if sn, _ := f.comment["snippet"].(map[string]any); sn["videoId"] != "vid1" {
		t.Fatalf("second comment %v", f.comment)
	}
	if _, err := r.Reply(context.Background(), youtubeCreds(), "vid1", PostContent{Text: "private video"}, nil); failureKind(t, err) != ErrRejected ||
		!strings.Contains(err.Error(), "usually because it is private") {
		t.Fatalf("private video: %v", err)
	}
	if _, err := r.Reply(context.Background(), youtubeCreds(), "vid1", PostContent{Text: "made for kids"}, nil); failureKind(t, err) != ErrRejected ||
		!strings.Contains(err.Error(), "comments turned off") {
		t.Fatalf("comments off: %v", err)
	}

	m, err := yt.Provider.(MetricsReader).Metrics(context.Background(), youtubeCreds(), "vid1")
	if err != nil || *m.Impressions != 1200 || *m.Likes != 30 || *m.Replies != 4 || m.Reposts != nil ||
		*m.WatchMinutes != 340 || *m.AvgViewSeconds != 72 || *m.Shares != 5 || *m.SubscribersGained != 2 {
		t.Fatalf("metrics %+v %v", m, err)
	}
	// Connected before staqly asked for YouTube Analytics: the public numbers alone.
	f.mu.Lock()
	f.noYTA = true
	f.mu.Unlock()
	m, err = yt.Provider.(MetricsReader).Metrics(context.Background(), youtubeCreds(), "vid1")
	if err != nil || *m.Impressions != 1200 || m.WatchMinutes != nil || m.Shares != nil {
		t.Fatalf("metrics without analytics %+v %v", m, err)
	}
}

func TestYouTubeValidate(t *testing.T) {
	yt, _ := Get("youtube")
	video := youtubeVideo("0123")
	ok := Settings{"title": "Morning run"}
	for _, tc := range []struct {
		name  string
		p     PostContent
		s     Settings
		field string
	}{
		{"no video", PostContent{Text: "hi"}, ok, "media"},
		{"a picture", PostContent{Media: []MediaFile{{Name: "a.jpg", MimeType: "image/jpeg"}}}, ok, "media"},
		{"no title", PostContent{Media: []MediaFile{video}}, Settings{}, "title"},
		{"angle brackets", PostContent{Text: "a <b> c", Media: []MediaFile{video}}, ok, "content"},
		{"5000 bytes", PostContent{Text: strings.Repeat("é", 2600), Media: []MediaFile{video}}, ok, "content"},
		{"playlist link", PostContent{Media: []MediaFile{video}}, Settings{"title": "x", "playlist": "https://www.youtube.com/@janeruns"}, "playlist"},
		{"privacy", PostContent{Media: []MediaFile{video}}, Settings{"title": "x", "privacy": "friends"}, "privacy"},
	} {
		err := yt.Validate(tc.p, tc.s)
		var v *ValidationError
		if !errors.As(err, &v) || v.Field != tc.field {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if err := yt.Validate(PostContent{Media: []MediaFile{video}}, Settings{"title": "x", "playlist": "https://www.youtube.com/playlist?list=PL1"}); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleCheckApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Unauthorized"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Malformed auth code."}`))
	}))
	defer srv.Close()
	old := googleTokenURL
	googleTokenURL = srv.URL
	defer func() { googleTokenURL = old }()
	for _, c := range []AppChecker{YouTube{}, GoogleBusiness{}} {
		if ok, err := c.CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "secret"}, "http://localhost:8080/cb"); !ok || err != nil {
			t.Fatalf("good keys: %v %v", ok, err)
		}
		if _, err := c.CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "wrong"}, "http://localhost:8080/cb"); err == nil {
			t.Fatal("bad keys passed")
		}
	}
}
