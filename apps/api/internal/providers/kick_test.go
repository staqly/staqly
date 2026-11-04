package providers

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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
	subs     []string // subscribed events
	subCode  int      // status for /public/v1/events/subscriptions (0 = 200)
	pubKey   string   // PEM at /public/v1/public-key
	keyReads int
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
	case "/public/v1/public-key":
		f.keyReads++
		b, _ := json.Marshal(map[string]any{"data": map[string]string{"public_key": f.pubKey}, "message": "OK"})
		_, _ = w.Write(b)
	case "/public/v1/events/subscriptions":
		if f.subCode != 0 {
			w.WriteHeader(f.subCode)
			_, _ = w.Write([]byte(`{"data":null,"message":"missing scope events:subscribe"}`))
			return
		}
		if r.Method == http.MethodPost {
			var body struct {
				Events []struct {
					Name string `json:"name"`
				} `json:"events"`
				Method string `json:"method"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			out := []map[string]string{}
			for _, e := range body.Events {
				if body.Method == "webhook" {
					f.subs = append(f.subs, e.Name)
				}
				out = append(out, map[string]string{"name": e.Name, "subscription_id": "s-" + e.Name})
			}
			b, _ := json.Marshal(map[string]any{"data": out, "message": "OK"})
			_, _ = w.Write(b)
			return
		}
		list := []map[string]any{}
		for _, e := range f.subs {
			list = append(list, map[string]any{"event": e, "method": "webhook", "version": 1})
		}
		b, _ := json.Marshal(map[string]any{"data": list, "message": "OK"})
		_, _ = w.Write(b)
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

func TestKickSubscribeEvents(t *testing.T) {
	f := useFakeKick(t)
	ctx := context.Background()
	f.subs = []string{"chat.message.sent"}
	if err := (Kick{}).SubscribeEvents(ctx, kickCreds()); err != nil {
		t.Fatal(err)
	}
	if len(f.subs) != len(KickEvents) {
		t.Fatalf("subscribed %v", f.subs)
	}
	// Everything subscribed: nothing more is created.
	if err := (Kick{}).SubscribeEvents(ctx, kickCreds()); err != nil || len(f.subs) != len(KickEvents) {
		t.Fatalf("again: %v %v", err, f.subs)
	}
	// A channel connected before staqly asked for events:subscribe.
	f.subCode = http.StatusForbidden
	if err := (Kick{}).SubscribeEvents(ctx, kickCreds()); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("no scope: %v", err)
	}
	f.subCode = http.StatusUnauthorized
	if err := (Kick{}).SubscribeEvents(ctx, kickCreds()); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
}

func TestKickSigned(t *testing.T) {
	f := useFakeKick(t)
	ctx := context.Background()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	f.pubKey = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	t.Cleanup(UseKickKey(nil))
	body := []byte(`{"message_id":"m-2"}`)
	sum := sha256.Sum256([]byte("01ABC.2026-11-04T10:00:00Z." + string(body)))
	raw, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	sig := base64.StdEncoding.EncodeToString(raw)
	if !KickSigned(ctx, "01ABC", "2026-11-04T10:00:00Z", body, sig) || f.keyReads != 1 {
		t.Fatalf("a call Kick signed must pass (key reads %d)", f.keyReads)
	}
	if KickSigned(ctx, "01ABC", "2026-11-04T10:00:00Z", []byte(`{"message_id":"m-3"}`), sig) {
		t.Fatal("a changed body must fail")
	}
	if KickSigned(ctx, "01ABD", "2026-11-04T10:00:00Z", body, sig) || KickSigned(ctx, "01ABC", "2026-11-04T10:00:00Z", body, "not base64!") {
		t.Fatal("a changed id or a broken signature must fail")
	}
	// A bad signature right after reading the key doesn't read it again and again.
	if f.keyReads != 1 {
		t.Fatalf("key read %d times", f.keyReads)
	}
	if got := KickText("hi [emote:4148074:HYPERCLAP] there"); got != "hi HYPERCLAP there" {
		t.Fatal(got)
	}
}
