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

	// A chat connected through the server's bot publishes with the server's token; without one it can't.
	shared := telegramCreds()
	delete(shared, "bot_token")
	shared["bot"] = "shared"
	if _, err := tg.Publish(ctx, shared, PostContent{Text: "Hi"}, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("no server bot: %v", err)
	}
	SetTelegramBot("123:abc", "janedoe_bot")
	t.Cleanup(func() { SetTelegramBot("", "") })
	if res, err := tg.Publish(ctx, shared, PostContent{Text: "Hi"}, Settings{}); err != nil || res.ExternalID != "55" {
		t.Fatalf("server bot: %+v %v", res, err)
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

// Was a bug: the server's bot is set only once Telegram answered at start, and a post due before that marked every
// chat connected through it as needing a reconnect (which could not help). It is tried again instead; without the
// setting at all, the chat does need connecting with a bot of its own.
func TestTelegramSharedBotNotReadyYetIsTriedAgain(t *testing.T) {
	f := useFakeTelegram(t)
	shared := Credentials{"bot": "shared", "chat_id": "-1001234567890", "chat_username": "janedoe_news"}
	configured := WithGetenv(context.Background(), func(k string) string {
		if k == "PROVIDER_TELEGRAM_BOT_TOKEN" {
			return "123:abc"
		}
		return ""
	})
	SetTelegramBot("", "")
	_, err := Telegram{}.Publish(configured, shared, PostContent{Text: "Hi"}, Settings{})
	if err == nil || errors.Is(err, ErrReauth) || errors.Is(err, ErrRejected) || len(f.calls) != 0 {
		t.Fatalf("bot not ready: %v (calls %v)", err, f.calls)
	}
	unset := WithGetenv(context.Background(), func(string) string { return "" })
	if _, err := (Telegram{}).Publish(unset, shared, PostContent{Text: "Hi"}, Settings{}); !errors.Is(err, ErrReauth) {
		t.Fatalf("no bot on the server: %v", err)
	}
}

// Was a race: the bot's poller records the server's bot while publish jobs read it (go test -race).
func TestTelegramSharedBotSetWhilePublishing(t *testing.T) {
	useFakeTelegram(t)
	t.Cleanup(func() { SetTelegramBot("", "") })
	shared := Credentials{"bot": "shared", "chat_id": "-1001234567890"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			SetTelegramBot("123:abc", "janedoe_bot")
			SetTelegramBotName("Jane")
		}
	}()
	for range 20 {
		_, _ = Telegram{}.Publish(context.Background(), shared, PostContent{Text: "Hi"}, Settings{})
		_, _ = TelegramBot()
	}
	wg.Wait()
}

// A chat connected through the server's bot records that bot (its id and @username, not its token).
func TestTelegramSharedBotIsRecorded(t *testing.T) {
	useFakeTelegram(t)
	t.Cleanup(func() { SetTelegramBot("", "") })
	SetTelegramBot("123:abc", "janedoe_bot")
	creds, _, err := Telegram{}.Connect(context.Background(), map[string]string{"bot": "shared", "chat": "-1001234567890"})
	if err != nil {
		t.Fatal(err)
	}
	if creds["bot"] != "shared" || creds["bot_id"] != "42" || creds["bot_username"] != "janedoe_bot" || creds["bot_token"] != "" {
		t.Fatalf("creds %v", creds)
	}
	for k, v := range creds {
		if strings.Contains(v, "abc") {
			t.Fatalf("%s holds the token: %q", k, v)
		}
	}
}

// Was a bug: the operator gave the server another bot (a new PROVIDER_TELEGRAM_BOT_TOKEN for a different bot), and
// chats connected through the old one failed with Telegram's "chat not found", which never said the bot changed.
// Publishing and checking now name both bots; the same bot under a new token, and chats connected before the bot
// was recorded, carry on as before.
func TestTelegramSharedBotChanged(t *testing.T) {
	f := useFakeTelegram(t)
	t.Cleanup(func() { SetTelegramBot("", "") })
	SetTelegramBot("123:abc", "janedoe_dev_bot")
	chat := Credentials{"bot": "shared", "chat": "-1001234567890", "chat_id": "-1001234567890", "chat_username": "janedoe_news"}
	with := func(kv ...string) Credentials {
		c := Credentials{}
		for k, v := range chat {
			c[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			c[kv[i]] = kv[i+1]
		}
		return c
	}
	want := "Telegram: this channel was connected through @janedoe_bot, but this server now posts as @janedoe_dev_bot: " +
		"add @janedoe_dev_bot to the chat and connect it again"

	for name, c := range map[string]Credentials{
		"by id":       with("bot_id", "77", "bot_username", "janedoe_bot"),
		"by username": with("bot_username", "janedoe_bot"),
	} {
		f.calls = nil
		_, err := Telegram{}.Publish(context.Background(), c, PostContent{Text: "Hi"}, Settings{})
		if !errors.Is(err, ErrReauth) || err.Error() != want || len(f.calls) != 0 {
			t.Errorf("publish %s: %v (calls %v)", name, err, f.calls)
		}
		// Checking the channel (TestChannel passes the stored credentials to Connect) says the same.
		if _, _, err := (Telegram{}).Connect(context.Background(), c); !errors.Is(err, ErrReauth) || err.Error() != want {
			t.Errorf("check %s: %v", name, err)
		}
	}

	for name, c := range map[string]Credentials{
		"same bot, new token":   with("bot_id", "123", "bot_username", "janedoe_bot_renamed"),
		"same username":         with("bot_username", "JaneDoe_Dev_Bot"),
		"connected before this": chat,
	} {
		if r, err := (Telegram{}).Publish(context.Background(), c, PostContent{Text: "Hi"}, Settings{}); err != nil || r.ExternalID != "55" {
			t.Errorf("publish %s: %+v %v", name, r, err)
		}
	}

	// The old bot's name unknown: the message still says what to do.
	err := telegramBotChanged(with("bot_id", "77"), "123:abc", "janedoe_dev_bot")
	if err == nil || !strings.Contains(err.Error(), "through another bot, but this server now posts as @janedoe_dev_bot") {
		t.Fatalf("unknown old bot: %v", err)
	}
	// A bot's own channels are not judged, nor a server whose bot is not ready yet.
	if telegramBotChanged(Credentials{"bot_token": "123:abc", "bot_id": "77"}, "123:abc", "x") != nil ||
		telegramBotChanged(with("bot_id", "77"), "", "") != nil {
		t.Fatal("judged a channel it should not")
	}
}

// What the Bot API answers outside the usual: a group that became a supergroup needs reconnecting (new id), an
// outage page that is not JSON is tried again, a 2xx that can't be read is an error.
func TestTelegramUnusualAnswers(t *testing.T) {
	if err := telegramError(400, "Bad Request: group chat was upgraded to a supergroup chat", 0); !errors.Is(err, ErrReauth) {
		t.Fatalf("upgraded group: %v", err)
	}
	if err := telegramError(429, "Too Many Requests", 0); err == nil || errors.Is(err, ErrRejected) || !RetryAt(err).IsZero() {
		t.Fatalf("rate limit without a wait: %v", err)
	}
	for _, c := range []struct {
		status int
		body   string
		retry  bool
	}{
		{502, `<html>Bad Gateway</html>`, true},
		{429, ``, true},
		{200, `<html>?</html>`, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
		err := telegramDo(context.Background(), req, nil)
		srv.Close()
		var f *Failure
		if err == nil || errors.As(err, &f) != c.retry || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Errorf("%d %q: %v", c.status, c.body, err)
		}
	}
}

// /connect CODE as people type it: any case, addressed to a bot, spaces around; anything else is not a command.
func TestTelegramConnectCommandParsing(t *testing.T) {
	for text, want := range map[string]string{
		"/connect ABC234":                "ABC234",
		"/connect abc234":                "abc234",
		"/connect@staqly_bot   ABC234  ": "ABC234",
		"/CONNECT ABC234":                "ABC234",
		"/connect":                       "",
		"/connect ABC234 please":         "",
		"please /connect ABC234":         "",
		"/connect AB":                    "",
		"/connect ABC-234":               "",
		"/connectABC234":                 "",
	} {
		got := ""
		if g := telegramConnect.FindStringSubmatch(strings.TrimSpace(text)); g != nil {
			got = g[2]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
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
