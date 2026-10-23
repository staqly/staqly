package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeTwitch answers like id.twitch.tv and api.twitch.tv.
type fakeTwitch struct {
	mu       sync.Mutex
	token    url.Values
	chat     map[string]string
	clientID string // the Client-Id of the last Helix call
	chatCode int
	chatBody string
	dropped  bool // is_sent false with a drop reason
	drop     bool
	srv      *httptest.Server
}

func (f *fakeTwitch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/oauth2/token":
		_ = r.ParseForm()
		f.token = r.PostForm
		switch {
		case r.PostForm.Get("client_secret") != "secret":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":403,"message":"invalid client secret"}`))
		case r.PostForm.Get("grant_type") == "client_credentials":
			_, _ = w.Write([]byte(`{"access_token":"app","expires_in":5000000,"token_type":"bearer"}`))
		case r.PostForm.Get("grant_type") == "refresh_token" && r.PostForm.Get("refresh_token") != "r1":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":400,"message":"Invalid refresh token"}`))
		case r.PostForm.Get("grant_type") == "refresh_token":
			_, _ = w.Write([]byte(`{"access_token":"a2","refresh_token":"r2","expires_in":14000,"token_type":"bearer"}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":14000,"scope":["user:write:chat"],"token_type":"bearer"}`))
		}
	case "/helix/users":
		f.clientID = r.Header.Get("Client-Id")
		_, _ = w.Write([]byte(`{"data":[{"id":"141981764","login":"janedoe","display_name":"JaneDoe","profile_image_url":"https://static-cdn.example.com/jane.png"}]}`))
	case "/helix/chat/messages":
		f.clientID = r.Header.Get("Client-Id")
		if r.Header.Get("Authorization") != "Bearer a1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized","status":401,"message":"Invalid OAuth token"}`))
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
		if f.dropped {
			_, _ = w.Write([]byte(`{"data":[{"message_id":"","is_sent":false,"drop_reason":{"code":"msg_duplicate","message":"Your message is identical to the one you sent within the last 30 seconds."}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"message_id":"abc-123","is_sent":true,"drop_reason":null}]}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeTwitch(t *testing.T) *fakeTwitch {
	t.Helper()
	f := &fakeTwitch{}
	f.srv = httptest.NewServer(f)
	id, api := twitchID, twitchAPI
	twitchID, twitchAPI = f.srv.URL, f.srv.URL
	t.Cleanup(func() { f.srv.Close(); twitchID, twitchAPI = id, api })
	return f
}

func twitchCreds() Credentials {
	return Credentials{"access_token": "a1", "refresh_token": "r1", "user_id": "141981764", "login": "janedoe", "client_id": "cid"}
}

func TestTwitchConnect(t *testing.T) {
	f := useFakeTwitch(t)
	ctx := context.Background()
	app := App{ClientID: "cid", ClientSecret: "secret"}
	tw := Twitch{}

	u, _ := tw.AuthURL(app, "https://app.example/cb", "st", nil)
	q, _ := url.ParseQuery(u[strings.Index(u, "?")+1:])
	if q.Get("scope") != "user:write:chat" || q.Get("client_id") != "cid" || q.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := tw.Exchange(ctx, app, "https://app.example/cb", "code", nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds["user_id"] != "141981764" || creds["client_id"] != "cid" || creds["expires_at"] == "" || f.clientID != "cid" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe" || profile.Name != "JaneDoe" || profile.ProfileURL != "https://www.twitch.tv/janedoe" {
		t.Fatalf("profile %+v", profile)
	}
	if _, _, err := tw.Exchange(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, "x", "code", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad keys: %v", err)
	}

	c, err := tw.Refresh(ctx, app, twitchCreds())
	if err != nil || c["access_token"] != "a2" || c["refresh_token"] != "r2" {
		t.Fatalf("refresh %v %v", c, err)
	}
	bad := twitchCreds()
	bad["refresh_token"] = "revoked"
	if _, err := tw.Refresh(ctx, app, bad); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}

	if ok, err := tw.CheckApp(ctx, app, ""); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := tw.CheckApp(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, ""); err == nil {
		t.Fatal("bad keys passed")
	}
}

func TestTwitchPublish(t *testing.T) {
	f := useFakeTwitch(t)
	ctx := context.Background()
	res, err := Twitch{}.Publish(ctx, twitchCreds(), PostContent{Text: "Going live"}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "abc-123" || res.URL != "https://www.twitch.tv/janedoe" {
		t.Fatalf("%+v", res)
	}
	if f.chat["broadcaster_id"] != "141981764" || f.chat["sender_id"] != "141981764" || f.chat["message"] != "Going live" || f.clientID != "cid" {
		t.Fatalf("chat %v %q", f.chat, f.clientID)
	}
	if _, err := (Twitch{}).Reply(ctx, twitchCreds(), "abc-123", PostContent{Text: "Come say hi"}, Settings{}); err != nil || f.chat["reply_parent_message_id"] != "abc-123" {
		t.Fatalf("reply %v %v", err, f.chat)
	}
}

func TestTwitchFailures(t *testing.T) {
	f := useFakeTwitch(t)
	ctx := context.Background()
	post := PostContent{Text: "Going live"}

	c := twitchCreds()
	c["access_token"] = "expired"
	if _, err := (Twitch{}).Publish(ctx, c, post, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
	f.dropped = true
	if _, err := (Twitch{}).Publish(ctx, twitchCreds(), post, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("dropped: %v", err)
	}
	f.dropped = false
	f.chatCode, f.chatBody = 403, `{"error":"Forbidden","status":403,"message":"The sender is banned from the broadcaster's chat room."}`
	if _, err := (Twitch{}).Publish(ctx, twitchCreds(), post, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("banned: %v", err)
	}
	f.chatCode, f.chatBody = 429, `{"error":"Too Many Requests","status":429,"message":""}`
	if _, err := (Twitch{}).Publish(ctx, twitchCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("rate limit: %v", err)
	}
	f.chatCode, f.chatBody = 500, `{"error":"Internal Server Error","status":500,"message":""}`
	if _, err := (Twitch{}).Publish(ctx, twitchCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("outage: %v", err)
	}
	f.chatCode = 0
	f.drop = true
	if _, err := (Twitch{}).Publish(ctx, twitchCreds(), post, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestTwitchValidate(t *testing.T) {
	tw, _ := Get("twitch")
	if tw.Status != StatusAvailable {
		t.Fatal("twitch is not available")
	}
	if err := tw.Validate(PostContent{Text: strings.Repeat("a", 501)}, Settings{}); err == nil {
		t.Fatal("501 characters")
	}
	if err := tw.Validate(PostContent{Text: "a", Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "a")}}, Settings{}); err == nil {
		t.Fatal("media on Twitch")
	}
	if err := tw.Validate(PostContent{Text: "a"}, Settings{}); err != nil {
		t.Fatal(err)
	}
}
