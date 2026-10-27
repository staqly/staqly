package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeTelegramBot answers like the Bot API for one bot that is an administrator of the channel -100123 but may
// not delete messages there; it records what the bot said.
type fakeTelegramBot struct {
	mu   sync.Mutex
	said []string
}

func (f *fakeTelegramBot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	ok := func(v string) { _, _ = w.Write([]byte(`{"ok":true,"result":` + v + `}`)) }
	switch method {
	case "getMe":
		ok(`{"id":42,"username":"staqly_test_bot"}`)
	case "getUpdates":
		ok(`[{"update_id":5,"channel_post":{"message_id":7,"text":"/connect@staqly_test_bot abc234","chat":{"id":-100123}}},` +
			`{"update_id":6,"message":{"message_id":8,"text":"hello","chat":{"id":-100123}}}]`)
	case "getChat":
		ok(`{"id":-100123,"type":"channel","title":"News","username":"news"}`)
	case "getChatMember":
		ok(`{"status":"administrator","can_post_messages":true}`)
	case "deleteMessage":
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message can't be deleted"}`))
	case "sendMessage":
		f.mu.Lock()
		f.said = append(f.said, in["text"].(string))
		f.mu.Unlock()
		ok(`{"message_id":9}`)
	default:
		_, _ = w.Write([]byte(`{"ok":false,"error_code":404,"description":"Not Found"}`))
	}
}

// Connecting a chat through the server's bot: a code from the dashboard, /connect CODE in the chat, the chat
// becomes a channel on the bot's token (not stored with it), and the bot says so when it can't clean up.
func TestTelegramSharedBotConnects(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	fake := &fakeTelegramBot{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	providers.SetTelegramAPI(srv.URL)
	t.Cleanup(func() { providers.SetTelegramAPI("https://api.telegram.org"); providers.SetTelegramBot("", "") })
	s.Cfg.Webhook.AllowLoopback = true

	if _, err := s.StartTelegramConnect(ctx, p, ""); err == nil {
		t.Fatal("a code without a bot on the server")
	}
	providers.SetTelegramBot("T0KEN", "staqly_test_bot")
	code, err := s.StartTelegramConnect(ctx, p, "")
	if err != nil || len(code.Code) != 6 || code.Command != "/connect@staqly_test_bot "+code.Code || code.Bot != "@staqly_test_bot" {
		t.Fatalf("%+v %v", code, err)
	}

	// The bot's updates: the command (as Telegram writes it in a group with several bots), and chat it ignores.
	cmds, next, err := providers.TelegramCommands(ctx, "T0KEN", 0, 0)
	if err != nil || next != 7 || len(cmds) != 1 || cmds[0].Code != "ABC234" || cmds[0].ChatID != -100123 {
		t.Fatalf("commands %+v %d %v", cmds, next, err)
	}
	s.telegramConnectChat(ctx, "T0KEN", providers.TelegramCommand{ChatID: -100123, MessageID: 7, Code: code.Code})

	got, err := s.GetTelegramConnect(ctx, p, code.Code)
	if err != nil || got.Status != "connected" || got.ChannelID == "" {
		t.Fatalf("status %+v %v", got, err)
	}
	row, creds, err := s.storedCredentials(ctx, got.ChannelID)
	if err != nil || row.Name != "News" || row.Handle != "@news" || creds["bot"] != "shared" || creds["bot_token"] != "" || creds["chat_id"] != "-100123" {
		t.Fatalf("channel %+v %v %v", row, creds, err)
	}
	if len(fake.said) != 1 || !strings.Contains(fake.said[0], "delete the /connect message yourself") {
		t.Fatalf("said %v", fake.said)
	}

	// A used or unknown code is turned down in the chat.
	s.telegramConnectChat(ctx, "T0KEN", providers.TelegramCommand{ChatID: -100123, MessageID: 10, Code: code.Code})
	if len(fake.said) != 2 || !strings.Contains(fake.said[1], "doesn't work here") ||
		!strings.Contains(fake.said[1], "only works with the staqly that showed it") || !strings.HasPrefix(fake.said[1], "<b>") {
		t.Fatalf("said %v", fake.said)
	}
	// The channel records which bot it was connected through (never its token).
	if creds["bot_id"] != "42" || creds["bot_username"] != "staqly_test_bot" {
		t.Fatalf("bot not recorded: %v", creds)
	}

	// Was confusing: a /connect addressed to another bot (another staqly's) got "this code is not valid" from ours.
	s.telegramConnectChat(ctx, "T0KEN", providers.TelegramCommand{ChatID: -100123, MessageID: 11, Code: "ZZZ234", Bot: "staqly_dev_bot"})
	if len(fake.said) != 2 {
		t.Fatalf("answered another bot's command: %v", fake.said)
	}
	s.telegramConnectChat(ctx, "T0KEN", providers.TelegramCommand{ChatID: -100123, MessageID: 12, Code: "ZZZ234", Bot: "Staqly_Test_Bot"})
	if len(fake.said) != 3 || !strings.Contains(fake.said[2], "doesn't work here") {
		t.Fatalf("its own command: %v", fake.said)
	}
}

// Why a chat did not connect reads as one plain sentence with the next step, in Telegram HTML (escaped).
func TestTelegramConnectFailedReply(t *testing.T) {
	got := telegramConnectFailed(&providers.ValidationError{Field: "chat", Msg: "make @bot an administrator of the channel, allowed to post <messages>"})
	want := "<b>This chat is not connected yet.</b>\nMake @bot an administrator of the channel, allowed to post &lt;messages&gt;.\n\n" +
		"Fix that, then send the same /connect command again (the code lasts 10 minutes)."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// A code past its 10 minutes connects nothing and reads as expired; another workspace can't read a code at all.
func TestTelegramConnectCodeExpiresAndStaysInItsWorkspace(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	fake := &fakeTelegramBot{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	providers.SetTelegramAPI(srv.URL)
	t.Cleanup(func() { providers.SetTelegramAPI("https://api.telegram.org"); providers.SetTelegramBot("", "") })
	s.Cfg.Webhook.AllowLoopback = true
	providers.SetTelegramBot("T0KEN", "staqly_test_bot")
	code, err := s.StartTelegramConnect(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.W.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_other", Name: "Other", RecoveryVerifier: "x",
		RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := Principal{WorkspaceID: "ws_other", Kind: PrincipalAPIKey, ID: "key_other", Name: "other", Scopes: AllScopes}
	var appErr *Error
	if _, err := s.GetTelegramConnect(ctx, other, code.Code); !errors.As(err, &appErr) || appErr.Status != 404 {
		t.Fatalf("another workspace's code: %v, want 404", err)
	}

	later := time.Now().Add(telegramCodeTTL + time.Minute)
	s.Now = func() time.Time { return later }
	s.telegramConnectChat(ctx, "T0KEN", providers.TelegramCommand{ChatID: -100123, MessageID: 7, Code: code.Code})
	got, err := s.GetTelegramConnect(ctx, p, code.Code)
	if err != nil || got.Status != "expired" || got.ChannelID != "" {
		t.Fatalf("expired code: %+v %v", got, err)
	}
	chans, _ := s.Q.ListChannels(ctx, p.WorkspaceID)
	for _, c := range chans {
		if c.Provider == "telegram" {
			t.Fatalf("an expired code connected %+v", c)
		}
	}
	if len(fake.said) != 1 || !strings.Contains(fake.said[0], "doesn't work here") {
		t.Fatalf("said %v", fake.said)
	}
}
