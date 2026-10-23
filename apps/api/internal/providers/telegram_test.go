package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeTelegram answers like api.telegram.org for the bot token "123:abc".
type fakeTelegram struct {
	mu        sync.Mutex
	calls     []string
	params    map[string]map[string]string // method -> its last parameters (JSON fields or form fields)
	files     map[string]int               // method -> files uploaded in its last call
	member    string                       // getChatMember's answer (default: an administrator that may post)
	sendCode  int                          // status for send* methods (0 = 200)
	sendDesc  string                       // their description then
	dropSends bool                         // close the connection on send* (an answer that never came)
	srv       *httptest.Server
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	token, method, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
	f.calls = append(f.calls, method)
	params := map[string]string{}
	files := 0
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		_ = r.ParseMultipartForm(1 << 20)
		for k, v := range r.MultipartForm.Value {
			params[k] = v[0]
		}
		files = len(r.MultipartForm.File)
	} else {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		for k, v := range m {
			if s, ok := v.(string); ok {
				params[k] = s
			} else {
				b, _ := json.Marshal(v)
				params[k] = string(b)
			}
		}
	}
	f.params[method], f.files[method] = params, files
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, desc string) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":` + strconv.Itoa(code) + `,"description":"` + desc + `","parameters":{"retry_after":7}}`))
	}
	if token != "123:abc" {
		fail(401, "Unauthorized")
		return
	}
	if strings.HasPrefix(method, "send") {
		if f.dropSends {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		if f.sendCode != 0 {
			fail(f.sendCode, f.sendDesc)
			return
		}
	}
	switch method {
	case "getMe":
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":42,"is_bot":true,"username":"janedoe_bot"}}`))
	case "getChat":
		if params["chat_id"] == "@nowhere" {
			fail(400, "Bad Request: chat not found")
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":-1001234567890,"type":"channel","title":"Jane Doe News","username":"janedoe_news"}}`))
	case "getChatMember":
		m := f.member
		if m == "" {
			m = `{"status":"administrator","can_post_messages":true}`
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":` + m + `}`))
	case "sendMediaGroup":
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"message_id":70},{"message_id":71}]}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":55}}`))
	}
}

func useFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{params: map[string]map[string]string{}, files: map[string]int{}}
	f.srv = httptest.NewServer(f)
	old := telegramAPI
	telegramAPI = f.srv.URL
	t.Cleanup(func() { f.srv.Close(); telegramAPI = old })
	return f
}

func telegramCreds() Credentials {
	return Credentials{"bot_token": "123:abc", "chat": "@janedoe_news", "chat_id": "-1001234567890", "chat_type": "channel", "chat_username": "janedoe_news"}
}

func chatMedia(name, mime, body string) MediaFile {
	return MediaFile{Name: name, MimeType: mime, Size: int64(len(body)), Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}}
}

func TestTelegramConnect(t *testing.T) {
	f := useFakeTelegram(t)
	ctx := context.Background()
	creds, profile, err := Telegram{}.Connect(ctx, map[string]string{"bot_token": " 123:abc ", "chat": "https://t.me/janedoe_news"})
	if err != nil {
		t.Fatal(err)
	}
	if creds["chat_id"] != "-1001234567890" || creds["chat"] != "@janedoe_news" || creds["bot_token"] != "123:abc" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe_news" || profile.Name != "Jane Doe News" || profile.ProfileURL != "https://t.me/janedoe_news" {
		t.Fatalf("profile %+v", profile)
	}
	if f.params["getChatMember"]["user_id"] != "42" {
		t.Fatalf("member check %v", f.params["getChatMember"])
	}
	// The stored credentials connect again (the channel check calls Connect with them).
	if _, _, err := (Telegram{}).Connect(ctx, map[string]string(creds)); err != nil {
		t.Fatal(err)
	}

	var v *ValidationError
	if _, _, err := (Telegram{}).Connect(ctx, map[string]string{"bot_token": "999:bad", "chat": "@janedoe_news"}); !errors.As(err, &v) || v.Field != "bot_token" {
		t.Fatalf("bad token: %v", err)
	}
	if _, _, err := (Telegram{}).Connect(ctx, map[string]string{"bot_token": "123:abc", "chat": "@nowhere"}); !errors.As(err, &v) || v.Field != "chat" || !strings.Contains(v.Msg, "@janedoe_bot") {
		t.Fatalf("unknown chat: %v", err)
	}
	f.member = `{"status":"member"}`
	if _, _, err := (Telegram{}).Connect(ctx, map[string]string{"bot_token": "123:abc", "chat": "@janedoe_news"}); !errors.As(err, &v) || !strings.Contains(v.Msg, "administrator") {
		t.Fatalf("not an admin: %v", err)
	}
	f.member = `{"status":"administrator","can_post_messages":false}`
	if _, _, err := (Telegram{}).Connect(ctx, map[string]string{"bot_token": "123:abc", "chat": "@janedoe_news"}); !errors.As(err, &v) || !strings.Contains(v.Msg, "post messages") {
		t.Fatalf("may not post: %v", err)
	}
}

func TestTelegramPublish(t *testing.T) {
	f := useFakeTelegram(t)
	ctx := context.Background()
	tg := Telegram{}

	res, err := tg.Publish(ctx, telegramCreds(), PostContent{Text: "Hello https://example.com"}, Settings{"silent": "true", "link_preview": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "55" || res.URL != "https://t.me/janedoe_news/55" {
		t.Fatalf("result %+v", res)
	}
	p := f.params["sendMessage"]
	if p["text"] != "Hello https://example.com" || p["disable_notification"] != "true" || !strings.Contains(p["link_preview_options"], `"is_disabled":true`) {
		t.Fatalf("sendMessage %v", p)
	}

	if _, err := tg.Publish(ctx, telegramCreds(), PostContent{Text: "A picture", Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "jpg")}}, Settings{}); err != nil {
		t.Fatal(err)
	}
	if p := f.params["sendPhoto"]; p["caption"] != "A picture" || p["photo"] != "attach://file0" || f.files["sendPhoto"] != 1 {
		t.Fatalf("sendPhoto %v %d", p, f.files["sendPhoto"])
	}
	if _, err := tg.Publish(ctx, telegramCreds(), PostContent{Media: []MediaFile{chatMedia("a.mp4", "video/mp4", "mp4")}}, Settings{}); err != nil {
		t.Fatal(err)
	}
	if p := f.params["sendVideo"]; p["video"] != "attach://file0" || p["caption"] != "" {
		t.Fatalf("sendVideo %v", p)
	}
	if _, err := tg.Publish(ctx, telegramCreds(), PostContent{Media: []MediaFile{chatMedia("a.gif", "image/gif", "gif")}}, Settings{}); err != nil {
		t.Fatal(err)
	}
	if f.params["sendAnimation"]["animation"] != "attach://file0" {
		t.Fatalf("sendAnimation %v", f.params["sendAnimation"])
	}

	res, err = tg.Publish(ctx, telegramCreds(), PostContent{Text: "Album", Media: []MediaFile{chatMedia("a.jpg", "image/jpeg", "a"), chatMedia("b.mp4", "video/mp4", "b")}}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "70" {
		t.Fatalf("album %+v", res)
	}
	var group []map[string]string
	_ = json.Unmarshal([]byte(f.params["sendMediaGroup"]["media"]), &group)
	if len(group) != 2 || group[0]["type"] != "photo" || group[0]["caption"] != "Album" || group[1]["type"] != "video" || group[1]["media"] != "attach://file1" || f.files["sendMediaGroup"] != 2 {
		t.Fatalf("media group %v", group)
	}

	// A private channel links by id, a basic group not at all.
	c := telegramCreds()
	c["chat_username"] = ""
	if res, _ := tg.Publish(ctx, c, PostContent{Text: "x"}, Settings{}); res.URL != "https://t.me/c/1234567890/55" {
		t.Fatalf("private link %q", res.URL)
	}
}

func TestTelegramReply(t *testing.T) {
	f := useFakeTelegram(t)
	res, err := Telegram{}.Reply(context.Background(), telegramCreds(), "55", PostContent{Text: "More"}, Settings{})
	if err != nil || res.ExternalID != "55" {
		t.Fatalf("%+v %v", res, err)
	}
	if rp := f.params["sendMessage"]["reply_parameters"]; !strings.Contains(rp, `"message_id":55`) {
		t.Fatalf("reply_parameters %q", rp)
	}
	if _, err := (Telegram{}).Reply(context.Background(), telegramCreds(), "", PostContent{Text: "More"}, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("no parent: %v", err)
	}
}

func TestTelegramFailures(t *testing.T) {
	f := useFakeTelegram(t)
	ctx := context.Background()
	post := PostContent{Text: "Hello"}

	c := telegramCreds()
	c["bot_token"] = "123:revoked"
	_, err := Telegram{}.Publish(ctx, c, post, Settings{})
	if !errors.Is(err, ErrReauth) || strings.Contains(err.Error(), "123:revoked") {
		t.Fatalf("revoked: %v", err)
	}

	f.sendCode, f.sendDesc = 403, "Forbidden: bot was kicked from the channel chat"
	if _, err := (Telegram{}).Publish(ctx, telegramCreds(), post, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("kicked: %v", err)
	}
	f.sendCode, f.sendDesc = 400, "Bad Request: message is too long"
	if _, err := (Telegram{}).Publish(ctx, telegramCreds(), post, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("too long: %v", err)
	}
	f.sendCode, f.sendDesc = 429, "Too Many Requests: retry after 7"
	_, err = Telegram{}.Publish(ctx, telegramCreds(), post, Settings{})
	if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) || !strings.Contains(err.Error(), "7 s") {
		t.Fatalf("rate limit: %v", err)
	}
	f.sendCode, f.sendDesc = 502, "Bad Gateway"
	if _, err := (Telegram{}).Publish(ctx, telegramCreds(), post, Settings{}); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("outage: %v", err)
	}

	f.sendCode = 0
	f.dropSends = true
	_, err = Telegram{}.Publish(ctx, telegramCreds(), post, Settings{})
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") || strings.Contains(err.Error(), "123:abc") {
		t.Fatalf("lost answer: %v", err)
	}

	// A file that cannot be read fails before anything is sent: retried, not "may have published".
	broken := MediaFile{Name: "gone.jpg", MimeType: "image/jpeg", Open: func(context.Context) (io.ReadCloser, error) { return nil, errors.New("gone") }}
	_, err = Telegram{}.Publish(ctx, telegramCreds(), PostContent{Media: []MediaFile{broken}}, Settings{})
	if err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("unreadable file: %v", err)
	}
}

func TestTelegramValidate(t *testing.T) {
	tg, _ := Get("telegram")
	if tg.Status != StatusAvailable {
		t.Fatal("telegram is not available")
	}
	jpg := chatMedia("a.jpg", "image/jpeg", "a")
	gif := chatMedia("a.gif", "image/gif", "a")
	cases := []struct {
		name  string
		post  PostContent
		field string
	}{
		{"text", PostContent{Text: strings.Repeat("a", 4096)}, ""},
		{"text too long", PostContent{Text: strings.Repeat("a", 4097)}, "content"},
		{"caption", PostContent{Text: strings.Repeat("a", 1024), Media: []MediaFile{jpg}}, ""},
		{"caption too long", PostContent{Text: strings.Repeat("a", 1025), Media: []MediaFile{jpg}}, "content"},
		{"gif in album", PostContent{Media: []MediaFile{gif, jpg}}, "media"},
		{"big picture", PostContent{Media: []MediaFile{{Name: "big.png", MimeType: "image/png", Size: 11 << 20}}}, "media"},
		{"big video", PostContent{Media: []MediaFile{{Name: "big.mp4", MimeType: "video/mp4", Size: 51 << 20}}}, "media"},
		{"eleven files", PostContent{Media: make([]MediaFile, 11)}, "media"},
	}
	for _, c := range cases {
		err := tg.Validate(c.post, Settings{})
		var v *ValidationError
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.field != "" && (!errors.As(err, &v) || v.Field != c.field):
			t.Errorf("%s: want %s, got %v", c.name, c.field, err)
		}
	}
	if err := tg.Validate(PostContent{Text: "x"}, Settings{"silent": "maybe"}); err == nil {
		t.Error("silent must be a boolean")
	}
}
