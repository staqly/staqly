package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeKick answers like id.kick.com and api.kick.com.
type fakeKick struct {
	mu       sync.Mutex
	token    url.Values     // the last token request
	chat     map[string]any // the last chat message
	chatCode int            // status for /public/v1/chat (0 = 200)
	chatBody string
	notSent  bool
	drop     bool
	srv      *httptest.Server
}

func (f *fakeKick) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/oauth/token":
		_ = r.ParseForm()
		f.token = r.PostForm
		switch {
		case r.PostForm.Get("client_secret") != "secret":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client authentication failed"}`))
		case r.PostForm.Get("grant_type") == "client_credentials":
			_, _ = w.Write([]byte(`{"access_token":"app","token_type":"Bearer","expires_in":3600}`))
		case r.PostForm.Get("grant_type") == "refresh_token" && r.PostForm.Get("refresh_token") != "r1":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token is invalid"}`))
		case r.PostForm.Get("grant_type") == "refresh_token":
			_, _ = w.Write([]byte(`{"access_token":"a2","refresh_token":"r2","expires_in":7200}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":7200,"scope":"user:read channel:read chat:write"}`))
		}
	case "/public/v1/users":
		_, _ = w.Write([]byte(`{"data":[{"user_id":4242,"name":"Jane_Doe","profile_picture":"https://files.example.com/jane.webp"}],"message":"OK"}`))
	case "/public/v1/channels":
		_, _ = w.Write([]byte(`{"data":[{"broadcaster_user_id":4242,"slug":"jane-doe"}],"message":"OK"}`))
	case "/public/v1/chat":
		if r.Header.Get("Authorization") != "Bearer a1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"data":{},"message":"Unauthorized"}`))
			return
		}
		f.chat = nil
		_ = json.NewDecoder(r.Body).Decode(&f.chat)
		if f.drop {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		if f.chatCode != 0 {
			w.WriteHeader(f.chatCode)
			_, _ = w.Write([]byte(f.chatBody))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"is_sent":` + map[bool]string{true: "false", false: "true"}[f.notSent] + `,"message_id":"m-1"},"message":"OK"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeKick(t *testing.T) *fakeKick {
	t.Helper()
	f := &fakeKick{}
	f.srv = httptest.NewServer(f)
	id, api := kickID, kickAPI
	kickID, kickAPI = f.srv.URL, f.srv.URL
	t.Cleanup(func() { f.srv.Close(); kickID, kickAPI = id, api })
	return f
}

func kickCreds() Credentials {
	return Credentials{"access_token": "a1", "refresh_token": "r1", "user_id": "4242", "slug": "jane-doe"}
}

func TestKickConnect(t *testing.T) {
	f := useFakeKick(t)
	ctx := context.Background()
	app := App{ClientID: "cid", ClientSecret: "secret"}
	k := Kick{}

	if _, err := k.AuthURL(app, "https://app.example/cb", "st", nil); err == nil {
		t.Fatal("PKCE verifier is required")
	}
	u, err := k.AuthURL(app, "https://app.example/cb", "st", map[string]string{"code_verifier": "v"})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(u[strings.Index(u, "?")+1:])
	sum := sha256.Sum256([]byte("v"))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) || q.Get("scope") != kickScopes || !strings.HasPrefix(u, f.srv.URL+"/oauth/authorize?") {
		t.Fatalf("auth url %s", u)
	}

	creds, profile, err := k.Exchange(ctx, app, "https://app.example/cb", "code", map[string]string{"code_verifier": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if f.token.Get("code_verifier") != "v" || f.token.Get("grant_type") != "authorization_code" {
		t.Fatalf("token form %v", f.token)
	}
	if creds["access_token"] != "a1" || creds["user_id"] != "4242" || creds["slug"] != "jane-doe" || creds["expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@jane-doe" || profile.Name != "Jane_Doe" || profile.ProfileURL != "https://kick.com/jane-doe" {
		t.Fatalf("profile %+v", profile)
	}

	c, err := k.Refresh(ctx, app, kickCreds())
	if err != nil || c["access_token"] != "a2" || c["refresh_token"] != "r2" || c["user_id"] != "4242" {
		t.Fatalf("refresh %v %v", c, err)
	}
	bad := kickCreds()
	bad["refresh_token"] = "used"
	if _, err := k.Refresh(ctx, app, bad); !errors.Is(err, ErrReauth) {
		t.Fatalf("spent refresh token: %v", err)
	}

	if ok, err := k.CheckApp(ctx, app, ""); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := k.CheckApp(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, ""); err == nil {
		t.Fatal("bad keys passed")
	}
}

func TestKickPublish(t *testing.T) {
	f := useFakeKick(t)
	ctx := context.Background()
	res, err := Kick{}.Publish(ctx, kickCreds(), PostContent{Text: "Live now"}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "m-1" || res.URL != "https://kick.com/jane-doe" {
		t.Fatalf("%+v", res)
	}
	if f.chat["type"] != "user" || f.chat["content"] != "Live now" || f.chat["broadcaster_user_id"] != float64(4242) {
		t.Fatalf("chat %v", f.chat)
	}
	if _, err := (Kick{}).Reply(ctx, kickCreds(), "m-1", PostContent{Text: "Link in bio"}, Settings{}); err != nil || f.chat["reply_to_message_id"] != "m-1" {
		t.Fatalf("reply %v %v", err, f.chat)
	}
}

func TestKickFailures(t *testing.T) {
	f := useFakeKick(t)
	ctx := context.Background()
	post := PostContent{Text: "Live now"}

	c := kickCreds()
	c["access_token"] = "expired"
	if _, err := (Kick{}).Publish(ctx, c, post, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
	f.notSent = true
	if _, err := (Kick{}).Publish(ctx, kickCreds(), post, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("not sent: %v", err)
	}
	f.notSent = false
	f.chatCode, f.chatBody = 403, `{"data":{},"message":"user is banned"}`
	if _, err := (Kick{}).Publish(ctx, kickCreds(), post, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("banned: %v", err)
	}
	f.chatCode, f.chatBody = 429, `{"message":"Too Many Requests"}`
	if _, err := (Kick{}).Publish(ctx, kickCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("rate limit: %v", err)
	}
	f.chatCode, f.chatBody = 503, ``
	if _, err := (Kick{}).Publish(ctx, kickCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("outage: %v", err)
	}
	f.chatCode = 0
	f.drop = true
	if _, err := (Kick{}).Publish(ctx, kickCreds(), post, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestKickValidate(t *testing.T) {
	k, _ := Get("kick")
	if k.Status != StatusAvailable {
		t.Fatal("kick is not available")
	}
	if err := k.Validate(PostContent{Text: strings.Repeat("a", 500)}, Settings{}); err != nil {
		t.Fatal(err)
	}
	if err := k.Validate(PostContent{Text: strings.Repeat("a", 501)}, Settings{}); err == nil {
		t.Fatal("501 characters")
	}
	if err := k.Validate(PostContent{Text: "a", Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "a")}}, Settings{}); err == nil {
		t.Fatal("media on Kick")
	}
}
