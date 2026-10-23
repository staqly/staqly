package providers

import (
	"context"
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

// fakeTikTok answers like open.tiktokapis.com and its upload host for the calls the provider makes.
type fakeTikTok struct {
	mu         sync.Mutex
	srv        *httptest.Server
	calls      []string
	tokenForm  url.Values
	privacy    string         // privacy_level_options of creator_info (JSON list; default all four)
	initBody   map[string]any // the last video/init body
	chunks     []string       // Content-Range of each PUT
	received   int            // bytes received
	statuses   []string       // status answers in order (the last one repeats)
	statusIdx  int
	failReason string
	initCode   int    // status for video/init (0 = 200)
	initErr    string // its error code then
	dropLast   bool   // close the connection on the last chunk
}

func (f *fakeTikTok) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	ok := `"error":{"code":"ok","message":"","log_id":"l1"}`
	auth := r.Header.Get("Authorization")
	switch {
	case r.URL.Path == "/v2/oauth/token/":
		f.tokenForm, _ = url.ParseQuery(string(raw))
		if f.tokenForm.Get("client_secret") != "secret" {
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client key or secret is incorrect.","log_id":"l1"}`))
			return
		}
		if f.tokenForm.Get("grant_type") == "refresh_token" && f.tokenForm.Get("refresh_token") == "expired" {
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Refresh token is invalid or expired.","log_id":"l1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"act.1","expires_in":86400,"open_id":"oid-1","refresh_expires_in":31536000,"refresh_token":"rft.2","scope":"user.info.basic,user.info.profile,video.publish","token_type":"Bearer"}`))
	case auth == "Bearer revoked":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"data":{},"error":{"code":"access_token_invalid","message":"The access token is invalid or not found in the request.","log_id":"l1"}}`))
	case r.URL.Path == "/v2/user/info/":
		_, _ = w.Write([]byte(`{"data":{"user":{"open_id":"oid-1","display_name":"Jane Doe","username":"janedoe","avatar_url":"https://p16.example/jane.jpg"}},` + ok + `}`))
	case r.URL.Path == "/v2/post/publish/creator_info/query/":
		privacy := f.privacy
		if privacy == "" {
			privacy = `["PUBLIC_TO_EVERYONE","MUTUAL_FOLLOW_FRIENDS","FOLLOWER_OF_CREATOR","SELF_ONLY"]`
		}
		_, _ = w.Write([]byte(`{"data":{"creator_username":"janedoe","privacy_level_options":` + privacy + `,"comment_disabled":false,"duet_disabled":true,"stitch_disabled":false,"max_video_post_duration_sec":600},` + ok + `}`))
	case r.URL.Path == "/v2/post/publish/video/init/":
		_ = json.Unmarshal(raw, &f.initBody)
		if f.initCode != 0 {
			w.WriteHeader(f.initCode)
			_, _ = w.Write([]byte(`{"data":{},"error":{"code":"` + f.initErr + `","message":"refused","log_id":"l1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"publish_id":"v_pub_file~v2.123","upload_url":"` + f.srv.URL + `/upload/?upload_id=1"},` + ok + `}`))
	case r.URL.Path == "/upload/":
		cr := r.Header.Get("Content-Range")
		f.chunks = append(f.chunks, cr)
		f.received += len(raw)
		if f.dropLast && f.isLast(cr) {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		w.WriteHeader(http.StatusCreated)
	case r.URL.Path == "/v2/post/publish/status/fetch/":
		st := "PUBLISH_COMPLETE"
		if len(f.statuses) > 0 {
			st = f.statuses[min(f.statusIdx, len(f.statuses)-1)]
			f.statusIdx++
		}
		ids := `[]`
		if st == "PUBLISH_COMPLETE" {
			ids = `[7360000000000000123]`
		}
		_, _ = w.Write([]byte(`{"data":{"status":"` + st + `","fail_reason":"` + f.failReason + `","publicaly_available_post_id":` + ids + `,"uploaded_bytes":0},` + ok + `}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// isLast says whether a Content-Range ("bytes a-b/total") ends the file.
func (f *fakeTikTok) isLast(cr string) bool {
	rng, total, _ := strings.Cut(strings.TrimPrefix(cr, "bytes "), "/")
	_, end, _ := strings.Cut(rng, "-")
	e, _ := strconv.Atoi(end)
	n, _ := strconv.Atoi(total)
	return e == n-1
}

func useFakeTikTok(t *testing.T) *fakeTikTok {
	t.Helper()
	f := &fakeTikTok{}
	f.srv = httptest.NewServer(f)
	api, auth, poll := tiktokAPI, tiktokAuthorize, tiktokPoll
	tiktokAPI, tiktokAuthorize, tiktokPoll = f.srv.URL, f.srv.URL+"/auth", time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		tiktokAPI, tiktokAuthorize, tiktokPoll = api, auth, poll
	})
	return f
}

func tiktokCreds() Credentials {
	return Credentials{"access_token": "act.1", "refresh_token": "rft.1", "open_id": "oid-1", "username": "janedoe"}
}

func tiktokVideo(size int) MediaFile {
	data := strings.Repeat("v", size)
	return MediaFile{Name: "clip.mp4", MimeType: "video/mp4", Size: int64(size),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil }}
}

func TestTikTokConnect(t *testing.T) {
	f := useFakeTikTok(t)
	tt, _ := Get("tiktok")
	if tt.Status != StatusAvailable {
		t.Fatalf("tiktok is %q", tt.Status)
	}
	app := App{ClientID: "ckey", ClientSecret: "secret"}
	u, _ := tt.AuthURL(app, "https://app.example/cb", "st", map[string]string{"code_verifier": "v"})
	q, _ := url.Parse(u)
	if q.Query().Get("client_key") != "ckey" || q.Query().Get("scope") != tiktokScopes || q.Query().Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := tt.Exchange(context.Background(), app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenForm.Get("client_key") != "ckey" || f.tokenForm.Get("code") != "code1" || f.tokenForm.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("token form %v", f.tokenForm)
	}
	if creds["access_token"] != "act.1" || creds["refresh_token"] != "rft.2" || creds["open_id"] != "oid-1" || creds["expires_at"] == "" || creds["refresh_expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe" || profile.Name != "Jane Doe" || profile.ProfileURL != "https://www.tiktok.com/@janedoe" {
		t.Fatalf("profile %+v", profile)
	}

	c, err := tt.Refresh(context.Background(), app, tiktokCreds())
	if err != nil || c["refresh_token"] != "rft.2" || c["username"] != "janedoe" {
		t.Fatalf("refresh %v %v", c, err)
	}
	expired := tiktokCreds()
	expired["refresh_token"] = "expired"
	if _, err := tt.Refresh(context.Background(), app, expired); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired refresh: %v", err)
	}
	if _, _, err := tt.Exchange(context.Background(), App{ClientID: "ckey", ClientSecret: "wrong"}, "https://app.example/cb", "c", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad keys: %v", err)
	}
}

func TestTikTokPublish(t *testing.T) {
	ctx := context.Background()
	tt, _ := Get("tiktok")
	settings := Settings{"privacy_level": "PUBLIC_TO_EVERYONE", "allow_comments": "true", "allow_duet": "true", "allow_stitch": "false", "ai_generated": "true", "your_brand": "true"}

	t.Run("small video in one chunk", func(t *testing.T) {
		f := useFakeTikTok(t)
		f.statuses = []string{"PROCESSING_UPLOAD", "PROCESSING_UPLOAD", "PUBLISH_COMPLETE"}
		res, err := tt.Publish(ctx, tiktokCreds(), PostContent{Text: "Morning run #running", Media: []MediaFile{tiktokVideo(1000)}}, settings)
		if err != nil {
			t.Fatal(err)
		}
		pi := f.initBody["post_info"].(map[string]any)
		si := f.initBody["source_info"].(map[string]any)
		// The creator turned duets off: that wins over the post's setting.
		if pi["title"] != "Morning run #running" || pi["privacy_level"] != "PUBLIC_TO_EVERYONE" || pi["disable_duet"] != true || pi["disable_stitch"] != true ||
			pi["disable_comment"] != false || pi["is_aigc"] != true || pi["brand_organic_toggle"] != true || pi["brand_content_toggle"] != false {
			t.Fatalf("post_info %v", pi)
		}
		if si["source"] != "FILE_UPLOAD" || si["video_size"] != 1000.0 || si["chunk_size"] != 1000.0 || si["total_chunk_count"] != 1.0 {
			t.Fatalf("source_info %v", si)
		}
		if len(f.chunks) != 1 || f.chunks[0] != "bytes 0-999/1000" || f.received != 1000 {
			t.Fatalf("chunks %v (%d bytes)", f.chunks, f.received)
		}
		// TikTok's id is past 2^53: kept exact.
		if res.ExternalID != "7360000000000000123" || res.URL != "https://www.tiktok.com/@janedoe/video/7360000000000000123" || f.statusIdx != 3 {
			t.Fatalf("result %+v after %d checks", res, f.statusIdx)
		}
	})

	t.Run("large video in 10 MB chunks", func(t *testing.T) {
		f := useFakeTikTok(t)
		size := tiktokSingleChunk + 5<<20 // 69 MB: 6 chunks, the last one 19 MB
		if _, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(size)}}, settings); err != nil {
			t.Fatal(err)
		}
		si := f.initBody["source_info"].(map[string]any)
		if si["chunk_size"] != float64(tiktokChunk) || si["total_chunk_count"] != 6.0 || len(f.chunks) != 6 || f.received != size {
			t.Fatalf("source_info %v, chunks %v, %d bytes", si, f.chunks, f.received)
		}
		if f.chunks[5] != "bytes 52428800-72351743/72351744" {
			t.Fatalf("last chunk %s", f.chunks[5])
		}
	})

	t.Run("privacy the account may not use", func(t *testing.T) {
		f := useFakeTikTok(t)
		f.privacy = `["SELF_ONLY"]`
		_, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings)
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "Only you") || f.initBody != nil {
			t.Fatalf("privacy: %v", err)
		}
	})

	t.Run("refused, rate limited, reauth", func(t *testing.T) {
		f := useFakeTikTok(t)
		f.initCode, f.initErr = http.StatusForbidden, "spam_risk_too_many_posts"
		if _, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "daily post limit") {
			t.Fatalf("refused: %v", err)
		}
		f.initCode, f.initErr = http.StatusTooManyRequests, "rate_limit_exceeded"
		_, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings)
		if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("rate limit is retried: %v", err)
		}
		c := tiktokCreds()
		c["access_token"] = "revoked"
		if _, err := tt.Publish(ctx, c, PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings); !errors.Is(err, ErrReauth) {
			t.Fatalf("revoked: %v", err)
		}
	})

	t.Run("processing fails", func(t *testing.T) {
		f := useFakeTikTok(t)
		f.statuses, f.failReason = []string{"FAILED"}, "duration_check_failed"
		if _, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "longer") {
			t.Fatalf("failed: %v", err)
		}
	})

	t.Run("last chunk's answer lost: TikTok's verdict decides", func(t *testing.T) {
		f := useFakeTikTok(t)
		f.dropLast = true
		res, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings)
		if err != nil || res.ExternalID == "" {
			t.Fatalf("lost last chunk: %+v %v", res, err)
		}
	})

	t.Run("still processing when the job ends", func(t *testing.T) {
		f := useFakeTikTok(t)
		f.statuses = []string{"PROCESSING_UPLOAD"}
		ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		if _, err := tt.Publish(ctx, tiktokCreds(), PostContent{Media: []MediaFile{tiktokVideo(10)}}, settings); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
			t.Fatalf("timeout: %v", err)
		}
	})
}

func TestTikTokValidate(t *testing.T) {
	tt, _ := Get("tiktok")
	video := []MediaFile{{MimeType: "video/mp4"}}
	for name, tc := range map[string]struct {
		p     PostContent
		s     Settings
		field string
	}{
		"no privacy":           {PostContent{Media: video}, Settings{}, "privacy_level"},
		"picture":              {PostContent{Media: []MediaFile{{MimeType: "image/jpeg"}}}, Settings{"privacy_level": "SELF_ONLY"}, "media"},
		"no video":             {PostContent{Text: "x"}, Settings{"privacy_level": "SELF_ONLY"}, "media"},
		"private paid partner": {PostContent{Media: video}, Settings{"privacy_level": "SELF_ONLY", "branded_content": "true"}, "privacy_level"},
	} {
		var ve *ValidationError
		if err := tt.Validate(tc.p, tc.s); !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("%s: %v, want a %s error", name, err, tc.field)
		}
	}
	if err := tt.Validate(PostContent{Media: video}, Settings{"privacy_level": "FOLLOWER_OF_CREATOR"}); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokCheckApp(t *testing.T) {
	useFakeTikTok(t) // restores the endpoints afterwards
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "secret" {
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client key or secret is incorrect.","log_id":"l"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"clt.1","expires_in":7200,"token_type":"Bearer"}`))
	}))
	defer srv.Close()
	tiktokAPI = srv.URL
	if ok, err := (TikTok{}).CheckApp(context.Background(), App{ClientID: "ckey", ClientSecret: "secret"}, ""); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (TikTok{}).CheckApp(context.Background(), App{ClientID: "ckey", ClientSecret: "wrong"}, ""); err == nil {
		t.Fatal("bad keys passed")
	}
	if !(TikTok{}).Needs().HTTPS {
		t.Fatal("TikTok calls back over https only")
	}
}
