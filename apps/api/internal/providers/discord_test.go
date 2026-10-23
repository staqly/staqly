package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const discordHook = "https://discord.com/api/webhooks/111222333444555666/tok-en"

// fakeDiscord answers like discord.com/api/webhooks/{id}/{token} for the webhook above.
type fakeDiscord struct {
	mu       sync.Mutex
	payload  map[string]any // the last post's payload_json
	files    []string       // the last post's file field names
	query    string         // the last post's query
	postCode int            // status for posts (0 = 200)
	postBody string
	dropPost bool
	srv      *httptest.Server
}

func (f *fakeDiscord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/webhooks/111222333444555666/tok-en" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Unknown Webhook","code":10015}`))
		return
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(`{"type":1,"id":"111222333444555666","name":"Jane Doe News","avatar":"abc","channel_id":"900","guild_id":"800","token":"tok-en"}`))
		return
	}
	f.query = r.URL.RawQuery
	_ = r.ParseMultipartForm(1 << 20)
	f.payload = nil
	_ = json.Unmarshal([]byte(r.FormValue("payload_json")), &f.payload)
	f.files = nil
	for k := range r.MultipartForm.File {
		f.files = append(f.files, k)
	}
	if f.dropPost {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
		return
	}
	if f.postCode != 0 {
		w.WriteHeader(f.postCode)
		_, _ = w.Write([]byte(f.postBody))
		return
	}
	channel := "900"
	if f.payload["thread_name"] != nil {
		channel = "1500" // a new forum thread
	}
	if t := r.URL.Query().Get("thread_id"); t != "" {
		channel = t
	}
	_, _ = w.Write([]byte(`{"id":"1501","channel_id":"` + channel + `"}`))
}

func useFakeDiscord(t *testing.T) *fakeDiscord {
	t.Helper()
	f := &fakeDiscord{}
	f.srv = httptest.NewServer(f)
	old := discordAPI
	discordAPI = f.srv.URL
	t.Cleanup(func() { f.srv.Close(); discordAPI = old })
	return f
}

func TestDiscordConnect(t *testing.T) {
	useFakeDiscord(t)
	ctx := context.Background()
	creds, profile, err := Discord{}.Connect(ctx, map[string]string{"webhook_url": " https://discordapp.com/api/webhooks/111222333444555666/tok-en "})
	if err != nil {
		t.Fatal(err)
	}
	if creds["webhook_url"] != discordHook || creds["channel_id"] != "900" || creds["guild_id"] != "800" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "Jane Doe News (111222333444555666)" || profile.ProfileURL != "https://discord.com/channels/800/900" ||
		profile.AvatarURL != "https://cdn.discordapp.com/avatars/111222333444555666/abc.png" {
		t.Fatalf("profile %+v", profile)
	}
	if _, _, err := (Discord{}).Connect(ctx, map[string]string(creds)); err != nil {
		t.Fatal(err)
	}
	var v *ValidationError
	for _, bad := range []string{"https://example.com/api/webhooks/1/x", "discord.com/api/webhooks/1/x", "https://discord.com/api/webhooks/abc/x", ""} {
		if _, _, err := (Discord{}).Connect(ctx, map[string]string{"webhook_url": bad}); !errors.As(err, &v) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	_, _, err = Discord{}.Connect(ctx, map[string]string{"webhook_url": "https://discord.com/api/webhooks/111222333444555666/zzsecret"})
	if !errors.As(err, &v) || strings.Contains(err.Error(), "zzsecret") {
		t.Fatalf("unknown webhook: %v", err)
	}
}

func discordCreds() Credentials {
	return Credentials{"webhook_url": discordHook, "webhook_id": "111222333444555666", "channel_id": "900", "guild_id": "800"}
}

func TestDiscordPublish(t *testing.T) {
	f := useFakeDiscord(t)
	ctx := context.Background()
	d := Discord{}

	res, err := d.Publish(ctx, discordCreds(), PostContent{Text: "Hello"}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "1501" || res.URL != "https://discord.com/channels/800/900/1501" || f.query != "wait=true" || f.payload["content"] != "Hello" {
		t.Fatalf("%+v %q %v", res, f.query, f.payload)
	}
	// A text channel's message takes no webhook replies.
	if _, err := d.Reply(ctx, discordCreds(), res.ExternalID, PostContent{Text: "More"}, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("reply in a text channel: %v", err)
	}

	if _, err := d.Publish(ctx, discordCreds(), PostContent{Text: "Pics", Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "a"), chatMedia("b.mp4", "video/mp4", "b")}}, Settings{}); err != nil {
		t.Fatal(err)
	}
	att, _ := f.payload["attachments"].([]any)
	if len(att) != 2 || len(f.files) != 2 || att[1].(map[string]any)["filename"] != "b.mp4" {
		t.Fatalf("attachments %v files %v", att, f.files)
	}

	// A forum post starts a thread; its comments go into it.
	res, err = d.Publish(ctx, discordCreds(), PostContent{Text: "Launch"}, Settings{"thread_name": "Launch day"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "1500/1501" || f.payload["thread_name"] != "Launch day" || res.URL != "https://discord.com/channels/800/1500/1501" {
		t.Fatalf("forum %+v %v", res, f.payload)
	}
	reply, err := d.Reply(ctx, discordCreds(), res.ExternalID, PostContent{Text: "More"}, Settings{"thread_name": "Launch day"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.query, "thread_id=1500") || f.payload["thread_name"] != nil || reply.ExternalID != "1500/1501" {
		t.Fatalf("reply %+v %q %v", reply, f.query, f.payload)
	}
}

func TestDiscordFailures(t *testing.T) {
	f := useFakeDiscord(t)
	ctx := context.Background()
	post := PostContent{Text: "Hello"}

	c := discordCreds()
	c["webhook_url"] = "https://discord.com/api/webhooks/111222333444555666/zzsecret"
	_, err := Discord{}.Publish(ctx, c, post, Settings{})
	if !errors.Is(err, ErrReauth) || strings.Contains(err.Error(), "zzsecret") {
		t.Fatalf("deleted webhook: %v", err)
	}
	f.postCode, f.postBody = 400, `{"message":"Webhooks posted to forum channels must have a thread_name or thread_id","code":220001}`
	if _, err := (Discord{}).Publish(ctx, discordCreds(), post, Settings{}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "forum post title") {
		t.Fatalf("forum without title: %v", err)
	}
	f.postCode, f.postBody = 413, `{"message":"Request entity too large","code":40005}`
	if _, err := (Discord{}).Publish(ctx, discordCreds(), post, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("too large: %v", err)
	}
	f.postCode, f.postBody = 429, `{"message":"You are being rate limited.","retry_after":1.5,"global":false}`
	if _, err := (Discord{}).Publish(ctx, discordCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("rate limit: %v", err)
	}
	f.postCode, f.postBody = 503, `upstream`
	if _, err := (Discord{}).Publish(ctx, discordCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("outage: %v", err)
	}
	f.postCode = 0
	f.dropPost = true
	_, err = Discord{}.Publish(ctx, discordCreds(), post, Settings{})
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") || strings.Contains(err.Error(), "tok-en") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestDiscordValidate(t *testing.T) {
	d, _ := Get("discord")
	if d.Status != StatusAvailable {
		t.Fatal("discord is not available")
	}
	if err := d.Validate(PostContent{Text: strings.Repeat("a", 2000)}, Settings{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(PostContent{Text: strings.Repeat("a", 2001)}, Settings{}); err == nil {
		t.Fatal("2001 characters")
	}
	if err := d.Validate(PostContent{Text: "a", Media: make([]MediaFile, 11)}, Settings{}); err == nil {
		t.Fatal("11 files")
	}
	if err := d.Validate(PostContent{Text: "a"}, Settings{"thread_name": strings.Repeat("t", 101)}); err == nil {
		t.Fatal("forum title over 100")
	}
}
