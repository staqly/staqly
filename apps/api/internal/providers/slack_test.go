package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSlack answers like slack.com/api for the bot token "xoxb-1".
type fakeSlack struct {
	mu        sync.Mutex
	calls     []string
	forms     map[string]url.Values     // method -> its last form or query
	bodies    map[string]map[string]any // method -> its last JSON body
	uploaded  map[string]string         // file id -> bytes
	errFor    map[string]string         // method -> the error code it answers with
	status    int                       // HTTP status for chat.postMessage (0 = 200)
	drop      bool                      // close the connection on chat.postMessage and completeUploadExternal
	shareLate int                       // files.info answers without shares this many times
	member    bool                      // conversations.info: the bot is in the channel
	srv       *httptest.Server
}

func (f *fakeSlack) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	f.calls = append(f.calls, method)
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(r.URL.Path, "/upload/") {
		b, _ := io.ReadAll(r.Body)
		f.uploaded[strings.TrimPrefix(r.URL.Path, "/upload/")] = string(b)
		_, _ = w.Write([]byte("OK - " + string(rune('0'+len(b)))))
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		f.bodies[method] = m
	} else {
		_ = r.ParseForm()
		f.forms[method] = r.Form
	}
	if code := f.errFor[method]; code != "" {
		_, _ = w.Write([]byte(`{"ok":false,"error":"` + code + `"}`))
		return
	}
	if method != "oauth.v2.access" && r.Header.Get("Authorization") != "Bearer xoxb-1" && r.Header.Get("Authorization") != "Bearer xoxb-2" {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
		return
	}
	if (method == "chat.postMessage" || method == "files.completeUploadExternal") && f.drop {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
		return
	}
	switch method {
	case "oauth.v2.access":
		if r.Form.Get("client_secret") != "secret" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"bad_client_secret"}`))
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxb-2","refresh_token":"xoxe-2","expires_in":43200,"team":{"id":"T1","name":"Acme"}}`))
			return
		}
		if r.Form.Get("code") != "good" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_code"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxb-1","token_type":"bot","bot_user_id":"U9","team":{"id":"T1","name":"Acme"}}`))
	case "auth.test":
		_, _ = w.Write([]byte(`{"ok":true,"url":"https://acme.slack.com/","team":"Acme","team_id":"T1","user_id":"U9"}`))
	case "conversations.list":
		if r.Form.Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"C1","name":"general","is_member":false},{"id":"G1","name":"secret","is_private":true,"is_member":false}],"response_metadata":{"next_cursor":"p2"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"G2","name":"team","is_private":true,"is_member":true}],"response_metadata":{"next_cursor":""}}`))
	case "conversations.info":
		ch := r.Form.Get("channel")
		private := strings.HasPrefix(ch, "G")
		b, _ := json.Marshal(map[string]any{"ok": true, "channel": map[string]any{"id": ch, "name": "general", "is_private": private, "is_member": f.member}})
		_, _ = w.Write(b)
	case "conversations.join":
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "chat.postMessage":
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1700000000.000200"}`))
	case "files.getUploadURLExternal":
		id := "F" + r.Form.Get("filename")
		_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + f.srv.URL + `/upload/` + id + `","file_id":"` + id + `"}`))
	case "files.completeUploadExternal":
		_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"Fa.jpg"}]}`))
	case "files.info":
		if f.shareLate > 0 {
			f.shareLate--
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"Fa.jpg","shares":{}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"Fa.jpg","shares":{"public":{"C1":[{"ts":"1700000000.000300"}]}}}}`))
	default:
		_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
	}
}

func useFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{forms: map[string]url.Values{}, bodies: map[string]map[string]any{}, uploaded: map[string]string{}, errFor: map[string]string{}}
	f.srv = httptest.NewServer(f)
	api, auth, poll := slackAPI, slackAuthorize, slackPoll
	slackAPI, slackAuthorize, slackPoll = f.srv.URL+"/api", f.srv.URL+"/oauth/v2/authorize", time.Millisecond
	t.Cleanup(func() { f.srv.Close(); slackAPI, slackAuthorize, slackPoll = api, auth, poll })
	return f
}

func slackCreds() Credentials {
	return Credentials{"access_token": "xoxb-1", "team_id": "T1", "team_name": "Acme", "team_url": "https://acme.slack.com/", "channel_id": "C1", "channel_name": "general"}
}

func TestSlackConnect(t *testing.T) {
	f := useFakeSlack(t)
	ctx := context.Background()
	app := App{ClientID: "cid", ClientSecret: "secret"}
	s := Slack{}

	u, _ := s.AuthURL(app, "https://app.example/channels/callback/slack", "st", nil)
	q, _ := url.ParseQuery(u[strings.Index(u, "?")+1:])
	if q.Get("scope") != slackScopes || q.Get("client_id") != "cid" || q.Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}

	creds, profile, err := s.Exchange(ctx, app, "https://app.example/channels/callback/slack", "good", nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds["access_token"] != "xoxb-1" || creds["team_url"] != "https://acme.slack.com/" || creds["expires_at"] != "" || profile.Name != "Acme" {
		t.Fatalf("%v %+v", creds, profile)
	}
	if _, _, err := s.Exchange(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, "x", "good", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad keys: %v", err)
	}

	accounts, err := s.Accounts(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].ID != "C1" || accounts[1].ID != "G2" || accounts[0].ProfileURL != "https://acme.slack.com/archives/C1" {
		t.Fatalf("accounts %+v", accounts) // the private channel the app was not invited to is left out
	}

	ch, p, err := s.UseAccount(ctx, creds, "C1")
	if err != nil {
		t.Fatal(err)
	}
	if ch["channel_id"] != "C1" || ch["access_token"] != "xoxb-1" || p.Handle != "#general (C1)" || p.Name != "#general" {
		t.Fatalf("%v %+v", ch, p)
	}
	if f.bodies["conversations.join"]["channel"] != "C1" {
		t.Fatal("the bot did not join the public channel")
	}
	if _, _, err := s.UseAccount(ctx, creds, "G1"); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "/invite") {
		t.Fatalf("private channel without the app: %v", err)
	}

	f.errFor["conversations.list"] = "invalid_auth"
	if _, err := s.Accounts(ctx, creds); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestSlackRefresh(t *testing.T) {
	f := useFakeSlack(t)
	ctx := context.Background()
	app := App{ClientID: "cid", ClientSecret: "secret"}

	// Without rotation the token is only checked.
	c, err := Slack{}.Refresh(ctx, app, slackCreds())
	if err != nil || c["access_token"] != "xoxb-1" {
		t.Fatalf("%v %v", c, err)
	}
	rot := slackCreds()
	rot["refresh_token"] = "xoxe-1"
	c, err = Slack{}.Refresh(ctx, app, rot)
	if err != nil || c["access_token"] != "xoxb-2" || c["refresh_token"] != "xoxe-2" || c["expires_at"] == "" || c["channel_id"] != "C1" {
		t.Fatalf("rotated %v %v", c, err)
	}
	f.errFor["auth.test"] = "token_revoked"
	if _, err := (Slack{}).Refresh(ctx, app, slackCreds()); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestSlackPublish(t *testing.T) {
	f := useFakeSlack(t)
	ctx := context.Background()
	s := Slack{}

	res, err := s.Publish(ctx, slackCreds(), PostContent{Text: "Hello <team> & co"}, Settings{"link_preview": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "1700000000.000200" || res.URL != "https://acme.slack.com/archives/C1/p1700000000000200" {
		t.Fatalf("%+v", res)
	}
	b := f.bodies["chat.postMessage"]
	if b["channel"] != "C1" || b["text"] != "Hello &lt;team&gt; &amp; co" || b["unfurl_links"] != false {
		t.Fatalf("postMessage %v", b)
	}

	reply, err := s.Reply(ctx, slackCreds(), res.ExternalID, PostContent{Text: "More"}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if f.bodies["chat.postMessage"]["thread_ts"] != "1700000000.000200" || reply.ExternalID != "1700000000.000200/1700000000.000200" ||
		!strings.Contains(reply.URL, "thread_ts=1700000000.000200") {
		t.Fatalf("reply %+v %v", reply, f.bodies["chat.postMessage"])
	}
	// A later part names the thread, not the reply before it.
	if _, err := s.Reply(ctx, slackCreds(), reply.ExternalID, PostContent{Text: "Even more"}, Settings{}); err != nil || f.bodies["chat.postMessage"]["thread_ts"] != "1700000000.000200" {
		t.Fatalf("second reply %v %v", err, f.bodies["chat.postMessage"])
	}

	f.shareLate = 2
	res, err = s.Publish(ctx, slackCreds(), PostContent{Text: "Pics", Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "jpg"), chatMedia("b.mp4", "video/mp4", "mp4!")}}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "1700000000.000300" || f.uploaded["Fa.jpg"] != "jpg" || f.uploaded["Fb.mp4"] != "mp4!" || f.forms["files.getUploadURLExternal"].Get("length") != "4" {
		t.Fatalf("files %+v %v", res, f.uploaded)
	}
	cb := f.bodies["files.completeUploadExternal"]
	if cb["channel_id"] != "C1" || cb["initial_comment"] != "Pics" || len(cb["files"].([]any)) != 2 {
		t.Fatalf("complete %v", cb)
	}

	// Slack never says where the files went: published, linked to the channel, no replies.
	f.shareLate = 100
	res, err = s.Publish(ctx, slackCreds(), PostContent{Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "jpg")}}, Settings{})
	if err != nil || res.ExternalID != "" || res.URL != "https://acme.slack.com/archives/C1" {
		t.Fatalf("no share %+v %v", res, err)
	}
}

func TestSlackFailures(t *testing.T) {
	f := useFakeSlack(t)
	ctx := context.Background()
	post := PostContent{Text: "Hello"}

	c := slackCreds()
	c["access_token"] = "xoxb-revoked"
	if _, err := (Slack{}).Publish(ctx, c, post, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
	f.errFor["chat.postMessage"] = "msg_too_long"
	if _, err := (Slack{}).Publish(ctx, slackCreds(), post, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("too long: %v", err)
	}
	f.errFor["chat.postMessage"] = "not_in_channel"
	if _, err := (Slack{}).Publish(ctx, slackCreds(), post, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("removed from the channel: %v", err)
	}
	delete(f.errFor, "chat.postMessage")
	f.status = 429
	if _, err := (Slack{}).Publish(ctx, slackCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("rate limit: %v", err)
	}
	f.status = 0
	f.drop = true
	if _, err := (Slack{}).Publish(ctx, slackCreds(), post, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
	if _, err := (Slack{}).Publish(ctx, slackCreds(), PostContent{Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "jpg")}}, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer with files: %v", err)
	}
	// An upload slot Slack refuses fails before anything is shared: retried.
	f.mu.Lock() // a closed connection orders nothing for the race detector
	f.drop = false
	f.errFor["files.getUploadURLExternal"] = "internal_error"
	f.mu.Unlock()
	if _, err := (Slack{}).Publish(ctx, slackCreds(), PostContent{Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "jpg")}}, Settings{}); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("upload outage: %v", err)
	}
}

func TestSlackCheckApp(t *testing.T) {
	useFakeSlack(t)
	ctx := context.Background()
	if ok, err := (Slack{}).CheckApp(ctx, App{ClientID: "cid", ClientSecret: "secret"}, "https://app.example/cb"); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (Slack{}).CheckApp(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, "https://app.example/cb"); err == nil {
		t.Fatal("bad keys passed")
	}
	if !(Slack{}).Needs().HTTPS {
		t.Fatal("Slack needs https")
	}
}

func TestSlackValidate(t *testing.T) {
	s, _ := Get("slack")
	if s.Status != StatusAvailable {
		t.Fatal("slack is not available")
	}
	if err := s.Validate(PostContent{Text: "a"}, Settings{"link_preview": "nope"}); err == nil {
		t.Fatal("link_preview must be a boolean")
	}
	if err := s.Validate(PostContent{Text: "a", Media: make([]MediaFile, 11)}, Settings{}); err == nil {
		t.Fatal("11 files")
	}
	if err := s.Validate(PostContent{Text: strings.Repeat("a", 40000)}, Settings{}); err != nil {
		t.Fatal(err)
	}
}
