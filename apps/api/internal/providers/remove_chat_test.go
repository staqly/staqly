package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// chatRemoveServer records every request ("METHOD path?query auth") and answers with reply(r).
func chatRemoveServer(t *testing.T, reply func(w http.ResponseWriter, r *http.Request, body map[string]any)) *[]string {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		reply(w, r, body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RM_URL", srv.URL)
	return &seen
}

func chatRemoveURL() string { return os.Getenv("RM_URL") }

func TestRemoveSlackMany(t *testing.T) {
	var got []string
	chatRemoveServer(t, func(w http.ResponseWriter, r *http.Request, b map[string]any) {
		got = append(got, b["channel"].(string)+"/"+b["ts"].(string)+" "+r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	old := slackAPI
	slackAPI = chatRemoveURL()
	t.Cleanup(func() { slackAPI = old })
	c := Credentials{"access_token": "xoxb-1"}
	if err := (Slack{}).Remove(context.Background(), c, "C1:1.2,C2:3.4"); err != nil {
		t.Fatal(err)
	}
	if err := (Slack{}).Remove(context.Background(), c, "C1:thread/5.6"); err != nil {
		t.Fatal(err)
	}
	want := "C1/1.2 Bearer xoxb-1,C2/3.4 Bearer xoxb-1,C1/5.6 Bearer xoxb-1"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v", got)
	}
}

func TestRemoveSlackErrors(t *testing.T) {
	code := "message_not_found"
	var tokens []string
	chatRemoveServer(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"ok":false,"error":"` + code + `"}`))
	})
	old := slackAPI
	slackAPI = chatRemoveURL()
	t.Cleanup(func() { slackAPI = old })
	c := Credentials{"access_token": "xoxb-secret", "user_token": "xoxp-secret", "channel_id": "C1"}
	if err := (Slack{}).Remove(context.Background(), c, "1.2"); err != nil {
		t.Fatalf("already gone must be nil: %v", err)
	}
	code = "cant_delete_message"
	tokens = nil
	err := (Slack{}).Remove(context.Background(), c, "C1:1.2")
	var f *Failure
	if !errors.As(err, &f) || !errors.Is(err, ErrManual) {
		t.Fatalf("want ErrManual, got %v", err)
	}
	if len(tokens) != 2 || tokens[1] != "Bearer xoxp-secret" {
		t.Fatalf("user token not tried: %v", tokens)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("token in error")
	}
	code = "invalid_auth"
	if err := (Slack{}).Remove(context.Background(), c, "C1:1.2"); !errors.Is(err, ErrReauth) {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveDiscordBot(t *testing.T) {
	status := 204
	seen := chatRemoveServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		if status == 404 {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Unknown Message","code":10008}`))
			return
		}
		w.WriteHeader(status)
	})
	old := discordAPI
	discordAPI = chatRemoveURL()
	t.Cleanup(func() { discordAPI = old })
	ctx := discordBotCtx("bot-token")
	if err := (Discord{}).Remove(ctx, nil, "901:m1,902:m2"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*seen, "|") != "DELETE /channels/901/messages/m1 Bot bot-token|DELETE /channels/902/messages/m2 Bot bot-token" {
		t.Fatalf("got %v", *seen)
	}
	status = 404
	if err := (Discord{}).Remove(ctx, nil, "901:m1"); err != nil {
		t.Fatalf("404 is already gone: %v", err)
	}
	status = 403
	if err := (Discord{}).Remove(ctx, nil, "901:m1"); !errors.Is(err, ErrRejected) {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveDiscordWebhookThread(t *testing.T) {
	seen := chatRemoveServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { w.WriteHeader(204) })
	old := discordAPI
	discordAPI = chatRemoveURL()
	t.Cleanup(func() { discordAPI = old })
	c := Credentials{"webhook_url": "https://discord.com/api/webhooks/111222333444555666/zz-secret"}
	if err := (DiscordWebhook{}).Remove(context.Background(), c, "th/9"); err != nil {
		t.Fatal(err)
	}
	if err := (DiscordWebhook{}).Remove(context.Background(), c, "7"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*seen, "|"); got != "DELETE /webhooks/111222333444555666/zz-secret/messages/9?thread_id=th |DELETE /webhooks/111222333444555666/zz-secret/messages/7 " {
		t.Fatalf("got %q", got)
	}
}

func TestRemoveTelegramOld(t *testing.T) {
	desc := "Bad Request: message can't be deleted"
	var params map[string]any
	chatRemoveServer(t, func(w http.ResponseWriter, r *http.Request, b map[string]any) {
		params = b
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"` + desc + `"}`))
	})
	old := telegramAPI
	telegramAPI = chatRemoveURL()
	t.Cleanup(func() { telegramAPI = old })
	c := telegramCreds()
	err := (Telegram{}).Remove(context.Background(), c, "55")
	if !errors.Is(err, ErrManual) {
		t.Fatalf("want ErrManual, got %v", err)
	}
	if params["chat_id"] != "-1001234567890" || params["message_id"] != float64(55) {
		t.Fatalf("params %v", params)
	}
	if strings.Contains(err.Error(), "123:abc") {
		t.Fatal("token in error")
	}
	desc = "Bad Request: message to delete not found"
	if err := (Telegram{}).Remove(context.Background(), c, "55"); err != nil {
		t.Fatalf("gone is nil: %v", err)
	}
}

func TestRemoveTelegramOK(t *testing.T) {
	f := useFakeTelegram(t)
	if err := (Telegram{}).Remove(context.Background(), telegramCreds(), "55"); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "deleteMessage" {
		t.Fatalf("calls %v", f.calls)
	}
}
