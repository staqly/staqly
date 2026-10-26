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

// fakeSlack answers like slack.com/api for the bot token "xoxb-1" and the user token "xoxp-1".
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
	if method != "oauth.v2.access" && r.Header.Get("Authorization") != "Bearer xoxb-1" && r.Header.Get("Authorization") != "Bearer xoxb-2" && r.Header.Get("Authorization") != "Bearer xoxp-1" {
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
		if r.Form.Get("refresh_token") == "xoxe-gone" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_refresh_token"}`))
			return
		}
		if r.Form.Get("refresh_token") == "xoxe-u1" {
			_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxp-2","token_type":"user","refresh_token":"xoxe-u2","expires_in":43200}`))
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
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxb-1","token_type":"bot","bot_user_id":"U9","team":{"id":"T1","name":"Acme"},"authed_user":{"id":"U1","access_token":"xoxp-1","token_type":"user"}}`))
	case "auth.test":
		_, _ = w.Write([]byte(`{"ok":true,"url":"https://acme.slack.com/","team":"Acme","team_id":"T1","user_id":"U9"}`))
	case "conversations.list":
		if r.Form.Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"C1","name":"general","is_member":false},{"id":"G1","name":"secret","is_private":true,"is_member":false}],"response_metadata":{"next_cursor":"p2"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"G2","name":"team","is_private":true,"is_member":true},{"id":"C3","name":"random","is_member":true}],"response_metadata":{"next_cursor":""}}`))
	case "conversations.info":
		ch := r.Form.Get("channel")
		private := strings.HasPrefix(ch, "G")
		b, _ := json.Marshal(map[string]any{"ok": true, "channel": map[string]any{"id": ch, "name": "general", "is_private": private, "is_member": f.member}})
		_, _ = w.Write(b)
	case "team.info":
		_, _ = w.Write([]byte(`{"ok":true,"team":{"id":"T1","name":"Acme","icon":{"image_132":"https://avatars.slack-edge.com/acme_132.png","image_default":false}}}`))
	case "reactions.get":
		if r.Form.Get("full") != "true" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_arguments"}`))
			return
		}
		if r.Form.Get("channel") == "C2" {
			_, _ = w.Write([]byte(`{"ok":true,"type":"message","message":{"reply_count":1,"reactions":[{"name":"tada","count":1,"users":["U2"]}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"type":"message","message":{"reply_count":2,"reactions":[{"name":"thumbsup","count":2,"users":["U1","U2"]},{"name":"tada","count":1,"users":["U1"]}]}}`))
	case "users.info":
		names := map[string]string{"U1": `{"name":"jane","profile":{"display_name":"Jane","real_name":"Jane Doe"}}`, "U2": `{"name":"john","profile":{"display_name":"","real_name":"John Roe"}}`}
		_, _ = w.Write([]byte(`{"ok":true,"user":` + names[r.Form.Get("user")] + `}`))
	case "users.profile.get":
		_, _ = w.Write([]byte(`{"ok":true,"profile":{"real_name":"Jane Doe","image_48":"https://avatars.slack-edge.com/jane_48.png"}}`))
	case "conversations.join":
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "chat.postMessage":
		if f.bodies[method]["channel"] == "C404" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"not_in_channel"}`))
			return
		}
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
	if q.Get("scope") != slackScopes || q.Get("user_scope") != slackUserScopes || q.Get("client_id") != "cid" || q.Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}

	creds, profile, err := s.Exchange(ctx, app, "https://app.example/channels/callback/slack", "good", nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds["access_token"] != "xoxb-1" || creds["team_url"] != "https://acme.slack.com/" || creds["expires_at"] != "" || profile.Name != "Acme" || profile.Handle != "acme.slack.com" || profile.AvatarURL != "https://avatars.slack-edge.com/acme_132.png" ||
		creds["user_token"] != "xoxp-1" || creds["user_id"] != "U1" {
		t.Fatalf("%v %+v", creds, profile)
	}
	if _, _, err := s.Exchange(ctx, App{ClientID: "cid", ClientSecret: "wrong"}, "x", "good", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad keys: %v", err)
	}

	// The channels to post in: every public one and the private ones the app was invited to.
	opts, err := s.Options(ctx, creds, "channel")
	if err != nil || len(opts) != 3 || opts[0] != (Option{Value: "C1", Label: "#general"}) || opts[1].Value != "G2" {
		t.Fatalf("options %+v %v", opts, err)
	}

	// Post as: the app with staqly's mark, the person with their Slack photo.
	who, err := s.Options(ctx, creds, "post_as")
	if err != nil || len(who) != 2 || who[0].Icon != "staqly" || who[1].Value != "me" || who[1].Icon != "https://avatars.slack-edge.com/jane_48.png" {
		t.Fatalf("post as %+v %v", who, err)
	}
	if slackPostAs[1].Icon != "" {
		t.Fatal("the shared list got the photo")
	}

	f.errFor["conversations.list"] = "invalid_auth"
	if _, err := s.Options(ctx, creds, "channel"); !errors.Is(err, ErrReauth) {
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
	// Rotation renews the person's token too; when that fails only posting as them goes.
	rot["user_token"], rot["user_refresh_token"] = "xoxp-1", "xoxe-u1"
	c, err = Slack{}.Refresh(ctx, app, rot)
	if err != nil || c["access_token"] != "xoxb-2" || c["user_token"] != "xoxp-2" || c["user_refresh_token"] != "xoxe-u2" {
		t.Fatalf("rotated user %v %v", c, err)
	}
	rot["user_refresh_token"] = "xoxe-gone"
	c, err = Slack{}.Refresh(ctx, app, rot)
	if err != nil || c["access_token"] != "xoxb-2" || c["user_token"] != "" {
		t.Fatalf("user refresh failed %v %v", c, err)
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
	if res.ExternalID != "C1:1700000000.000200" || res.URL != "https://acme.slack.com/archives/C1/p1700000000000200" {
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
	if f.bodies["chat.postMessage"]["thread_ts"] != "1700000000.000200" || reply.ExternalID != "C1:1700000000.000200/1700000000.000200" ||
		!strings.Contains(reply.URL, "thread_ts=1700000000.000200") {
		t.Fatalf("reply %+v %v", reply, f.bodies["chat.postMessage"])
	}
	// A post from before several channels (a bare ts) still takes replies in its channel.
	if r, err := s.Reply(ctx, slackCreds(), "1700000000.000200", PostContent{Text: "Old"}, Settings{}); err != nil || r.ExternalID != "C1:1700000000.000200/1700000000.000200" {
		t.Fatalf("legacy reply %+v %v", r, err)
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
	if res.ExternalID != "C1:1700000000.000300" || f.uploaded["Fa.jpg"] != "jpg" || f.uploaded["Fb.mp4"] != "mp4!" || f.forms["files.getUploadURLExternal"].Get("length") != "4" {
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

func TestSlackPostAsAndChannel(t *testing.T) {
	f := useFakeSlack(t)
	ctx := context.Background()
	s := Slack{}
	auth := ""
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "chat.postMessage") {
			auth = r.Header.Get("Authorization")
		}
		f.ServeHTTP(w, r)
	})

	// Not reconnected since posting as the person came: the post fails, the channel stays fine.
	if _, err := s.Publish(ctx, slackCreds(), PostContent{Text: "Hi"}, Settings{"post_as": "me"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("no user token: %v", err)
	}
	c := slackCreds()
	c["user_token"] = "xoxp-1"
	if _, err := s.Publish(ctx, c, PostContent{Text: "Hi"}, Settings{"post_as": "me"}); err != nil || auth != "Bearer xoxp-1" {
		t.Fatalf("as me: %v %s", err, auth)
	}

	// A workspace connection posts where the post says, and only there.
	ws := slackCreds()
	delete(ws, "channel_id")
	// A workspace post names its channels (#names work like ids); none named is refused.
	if _, err := s.Publish(ctx, ws, PostContent{Text: "Hi"}, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "pick the Slack channels") {
		t.Fatalf("no channel: %v", err)
	}
	if res, err := s.Publish(ctx, ws, PostContent{Text: "Hi"}, Settings{"channel": "#random"}); err != nil || f.bodies["chat.postMessage"]["channel"] != "C3" || !strings.HasPrefix(res.ExternalID, "C3:") {
		t.Fatalf("by name %+v %v", res, err)
	}
	if _, err := s.Publish(ctx, ws, PostContent{Text: "Hi"}, Settings{"channel": "#nope"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "#nope") {
		t.Fatalf("unknown name: %v", err)
	}
	// Several channels: one message each, replies under each.
	f.calls = nil
	multi, err := s.Publish(ctx, ws, PostContent{Text: "Hi"}, Settings{"channel": "C2,#random"})
	if err != nil || multi.ExternalID != "C2:1700000000.000200,C3:1700000000.000200" || !strings.Contains(multi.URL, "/archives/C2/") {
		t.Fatalf("several %+v %v", multi, err)
	}
	r, err := s.Reply(ctx, ws, multi.ExternalID, PostContent{Text: "More"}, Settings{"channel": "C2,#random"})
	if err != nil || r.ExternalID != "C2:1700000000.000200/1700000000.000200,C3:1700000000.000200/1700000000.000200" || f.bodies["chat.postMessage"]["channel"] != "C3" {
		t.Fatalf("replies %+v %v", r, err)
	}
	// One refused after another took it: final, so retrying can't post twice.
	if _, err := s.Publish(ctx, ws, PostContent{Text: "Hi"}, Settings{"channel": "C2,C404"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "published in C2") {
		t.Fatalf("partly: %v", err)
	}
	if _, err := s.Publish(ctx, ws, PostContent{Text: "Hi"}, Settings{"channel": "C404"}); !errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("refused channel on a workspace: %v", err)
	}

	// Another channel: the app joins it first, and the link points there.
	res, err := s.Publish(ctx, c, PostContent{Text: "Hi"}, Settings{"channel": "C2"})
	if err != nil || auth != "Bearer xoxb-1" || f.bodies["chat.postMessage"]["channel"] != "C2" || f.bodies["conversations.join"]["channel"] != "C2" ||
		!strings.Contains(res.URL, "/archives/C2/") {
		t.Fatalf("other channel %+v %v %v", res, err, f.bodies)
	}
	// A reply follows the post into that channel.
	if _, err := s.Reply(ctx, c, res.ExternalID, PostContent{Text: "More"}, Settings{"channel": "C2"}); err != nil || f.bodies["chat.postMessage"]["channel"] != "C2" {
		t.Fatalf("reply %v %v", err, f.bodies["chat.postMessage"])
	}

	// A channel Slack refuses fails the post, not the connection.
	for _, set := range []Settings{{"channel": "C404"}, {"channel": "C404", "post_as": "me"}} {
		if _, err := s.Publish(ctx, c, PostContent{Text: "Hi"}, set); !errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("%v: %v", set, err)
		}
	}
	c["channel_id"] = "C404"
	if _, err := s.Publish(ctx, c, PostContent{Text: "Hi"}, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("own channel gone: %v", err)
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
	if err := s.Validate(PostContent{Text: "a", Media: make([]MediaFile, 11)}, Settings{"channel": "C1"}); err == nil {
		t.Fatal("11 files")
	}
	if err := s.Validate(PostContent{Text: "a"}, Settings{}); err == nil {
		t.Fatal("a post names its Slack channels")
	}
	if err := s.Validate(PostContent{Text: "a"}, Settings{"post_as": "someone"}); err == nil {
		t.Fatal("post_as is app or me")
	}
	if err := s.Validate(PostContent{Text: "a"}, Settings{"channel": "C1,C2,C3,C4,C5,C6,C7,C8,C9,C10,C11"}); err == nil {
		t.Fatal("11 channels")
	}
	if err := s.Validate(PostContent{Text: strings.Repeat("a", 40000)}, Settings{"channel": "C1"}); err != nil {
		t.Fatal(err)
	}
}

func TestSlackMetrics(t *testing.T) {
	f := useFakeSlack(t)
	ctx := context.Background()
	c := slackCreds()
	delete(c, "channel_id")

	// A post in two channels: reactions summed by emoji with who reacted, replies of both threads.
	m, err := Slack{}.Metrics(ctx, c, "C1:1700000000.000200,C2:1700000000.000300/x")
	if err != nil {
		t.Fatal(err)
	}
	if *m.Likes != 4 || *m.Replies != 3 || len(m.Reactions) != 2 {
		t.Fatalf("%+v", m)
	}
	if r := m.Reactions[0]; r.Emoji != "thumbsup" || r.Count != 2 || strings.Join(r.People, ",") != "Jane,John Roe" {
		t.Fatalf("thumbsup %+v", r)
	}
	if r := m.Reactions[1]; r.Emoji != "tada" || r.Count != 2 || strings.Join(r.People, ",") != "Jane,John Roe" {
		t.Fatalf("tada %+v", r)
	}
	// A post from before several channels reads its connected channel; without users:read only counts.
	f.errFor["users.info"] = "missing_scope"
	m, err = Slack{}.Metrics(ctx, slackCreds(), "1700000000.000200")
	if err != nil || *m.Likes != 3 || m.Reactions[0].People != nil {
		t.Fatalf("legacy %+v %v", m, err)
	}
	f.errFor["reactions.get"] = "message_not_found"
	if _, err := (Slack{}).Metrics(ctx, slackCreds(), "1700000000.000200"); !errors.Is(err, ErrRejected) {
		t.Fatalf("gone: %v", err)
	}
}
