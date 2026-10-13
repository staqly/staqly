package httpapi_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store"
)

// ---- fakes ----

type fakeReq struct {
	Path   string
	Header http.Header
	Body   []byte
}

// fakeChat stands in for the Telegram, Slack, Discord and ntfy APIs.
type fakeChat struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []fakeReq
	fail    atomic.Int32 // >0: fail the next n requests; -1: fail all
	updates []any        // queued Telegram getUpdates results
}

func newFakeChat(t *testing.T) *fakeChat {
	f := &fakeChat{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, fakeReq{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		upd := f.updates
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			f.updates = nil
		}
		f.mu.Unlock()
		if n := f.fail.Load(); n != 0 {
			if n > 0 {
				f.fail.Add(-1)
			}
			http.Error(w, `{"ok":false,"description":"boom"}`, 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			if upd == nil {
				upd = []any{}
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": upd})
		case strings.HasPrefix(r.URL.Path, "/bot"):
			w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeChat) find(substr string) []fakeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeReq
	for _, r := range f.reqs {
		if strings.Contains(r.Path, substr) {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeChat) queue(u any) {
	f.mu.Lock()
	f.updates = append(f.updates, u)
	f.mu.Unlock()
}

type envVars struct {
	mu sync.Mutex
	m  map[string]string
}

func (v *envVars) get(k string) string { v.mu.Lock(); defer v.mu.Unlock(); return v.m[k] }
func (v *envVars) set(k, val string)   { v.mu.Lock(); v.m[k] = val; v.mu.Unlock() }

// newNotifyEnv is newEnv with notification settings pointing at fake.
func newNotifyEnv(t *testing.T, fake *fakeChat, logw ...io.Writer) (*env, *envVars) {
	t.Helper()
	dir := t.TempDir()
	database, err := store.Open(context.Background(), dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	var w io.Writer = io.Discard
	if len(logw) > 0 {
		w = logw[0]
	}
	log := slog.New(slog.NewTextHandler(w, nil))
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, SetupToken: "tok", UploadDir: dir + "/uploads", MaxUploadBytes: 1 << 20, PublicURL: "http://test",
		Webhook: app.WebhookConfig{AllowLoopback: true, RetryBase: 20 * time.Millisecond, RetryMax: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond}})
	vars := &envVars{m: map[string]string{
		"TELEGRAM_API_BASE": fake.srv.URL, "SLACK_API_BASE": fake.srv.URL + "/slack-api", "DISCORD_API_BASE": fake.srv.URL + "/discord-api",
		"NOTIFY_ALLOW_LOOPBACK": "true", "NOTIFY_RETRY_BASE": "10ms", "NOTIFY_MAX_FAILS": "3",
		"PUBLIC_WEB_URL": "https://app.example.com", "RATE_LIMIT_DISABLED": "true",
	}}
	svc.Getenv = vars.get
	h := httpapi.New(svc, log, httpapi.Config{MCP: mcpserver.HTTPHandler(svc, "test"), OpenAPISpec: []byte("openapi: 3.1.0")})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	vars.set("NOTIFY_PUBLIC_API_URL", srv.URL)
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, svc: svc, srv: srv, browser: &http.Client{Jar: jar}}
	var res struct {
		APIKey app.APIKey `json:"api_key"`
		CSRF   string     `json:"csrf_token"`
	}
	e.do(e.browser, "POST", "/v1/workspaces", "", map[string]string{"name": "Test", "setup_token": "tok"}, 201, &res)
	e.key, e.csrf = res.APIKey.Key, res.CSRF
	return e, vars
}

func (e *env) channel(body map[string]any, want int) app.NotificationChannel {
	e.t.Helper()
	var ch app.NotificationChannel
	e.do(e.browser, "POST", "/v1/notification-channels", "", body, want, &ch, "X-CSRF-Token", e.csrf)
	return ch
}

func (e *env) process() {
	e.t.Helper()
	if err := e.svc.ProcessNotifyOutbox(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) itemState(id string) app.Item {
	var it app.Item
	e.do(nil, "GET", "/v1/items/"+id, e.key, nil, 200, &it)
	return it
}

func (e *env) lastActor(id string) string {
	var page app.EventPage
	e.do(nil, "GET", "/v1/items/"+id+"/events", e.key, nil, 200, &page)
	return page.Data[0].Actor.Name
}

const tgToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abc"

func jsonBody(t *testing.T, r fakeReq) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("decode %s: %v: %s", r.Path, err, r.Body)
	}
	return m
}

// ---- tests ----

func TestNotificationChannelCRUDMaskingScopesIsolation(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	ch := e.channel(map[string]any{"name": "Phone", "type": "telegram", "config": map[string]any{"bot_token": tgToken, "chat_id": "42"}}, 201)
	raw, _ := json.Marshal(ch)
	if strings.Contains(string(raw), "ABCDEFGHIJ") || ch.Config["bot_token"] != "••••_abc" {
		t.Fatalf("token not masked: %s", raw)
	}
	if !ch.Interactive || ch.Config["mode"] != "polling" || len(ch.Events) != 2 || !ch.Enabled {
		t.Fatalf("defaults: %+v", ch)
	}
	// Secrets are encrypted at rest.
	var cfg, sec string
	if err := e.svc.DB.QueryRow(`SELECT config, secrets FROM notification_channels WHERE id = ?`, ch.ID).Scan(&cfg, &sec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg+sec, "ABCDEFGHIJ") || !strings.HasPrefix(sec, "v1:") {
		t.Fatalf("secret stored in plaintext: %s / %s", cfg, sec)
	}

	// Scope: the default key lacks channels:manage; admin passes.
	e.do(nil, "GET", "/v1/notification-channels", e.key, nil, 403, nil)
	e.do(nil, "POST", "/v1/notification-channels", e.key, map[string]any{"name": "x", "type": "ntfy", "config": map[string]any{"topic": "t"}}, 403, nil)
	admin := e.keyWith("admin")
	var list struct{ Data []app.NotificationChannel }
	e.do(nil, "GET", "/v1/notification-channels", admin, nil, 200, &list)
	if len(list.Data) != 1 {
		t.Fatalf("list: %+v", list.Data)
	}

	// Echoing the masked token keeps the stored secret…
	var upd app.NotificationChannel
	e.do(nil, "PATCH", "/v1/notification-channels/"+ch.ID, admin, map[string]any{"name": "Phone 2", "config": map[string]any{"bot_token": ch.Config["bot_token"]}}, 200, &upd)
	// …but retargeting (audit M3: new chat_id) requires the token again.
	e.do(nil, "PATCH", "/v1/notification-channels/"+ch.ID, admin, map[string]any{"config": map[string]any{"bot_token": ch.Config["bot_token"], "chat_id": "43"}}, 422, nil)
	e.do(nil, "PATCH", "/v1/notification-channels/"+ch.ID, admin, map[string]any{"config": map[string]any{"chat_id": "43"}}, 422, nil)
	e.do(nil, "PATCH", "/v1/notification-channels/"+ch.ID, admin, map[string]any{"config": map[string]any{"bot_token": tgToken, "chat_id": "43"}}, 200, &upd)
	if upd.Config["chat_id"] != "43" {
		t.Fatalf("update: %+v", upd.Config)
	}
	var res map[string]any
	e.do(nil, "POST", "/v1/notification-channels/"+ch.ID+"/test", admin, nil, 200, &res)
	if res["ok"] != true || len(fake.find("/bot"+tgToken+"/sendMessage")) != 1 {
		t.Fatalf("test send: %v", res)
	}
	// Failing test send reports the error without leaking the token.
	fake.fail.Store(1)
	e.do(nil, "POST", "/v1/notification-channels/"+ch.ID+"/test", admin, nil, 200, &res)
	if res["ok"] != false || strings.Contains(res["error"].(string), "ABCDEF") || !strings.Contains(res["error"].(string), "boom") {
		t.Fatalf("failed test: %v", res)
	}

	// Validation.
	e.do(nil, "POST", "/v1/notification-channels", admin, map[string]any{"name": "x", "type": "telegram", "config": map[string]any{"bot_token": "nope", "chat_id": "1"}}, 422, nil)
	e.do(nil, "POST", "/v1/notification-channels", admin, map[string]any{"name": "x", "type": "ntfy", "config": map[string]any{"topic": "t", "bogus": 1}}, 422, nil)
	e.do(nil, "POST", "/v1/notification-channels", admin, map[string]any{"name": "x", "type": "sms", "config": map[string]any{}}, 422, nil)
	e.do(nil, "PATCH", "/v1/notification-channels/"+ch.ID, admin, map[string]any{"type": "slack"}, 422, nil)
	e.do(nil, "POST", "/v1/notification-channels", admin, map[string]any{"name": "x", "type": "email", "config": map[string]any{"host": "smtp.example.com", "from": "a@example.com", "to": []string{"bad\r\nBcc: x@y.z"}}}, 422, nil)

	// Workspace isolation.
	var other struct {
		APIKey app.APIKey `json:"api_key"`
	}
	e.do(nil, "POST", "/v1/workspaces", "", map[string]string{"name": "Other", "setup_token": "tok"}, 201, &other)
	e.do(nil, "GET", "/v1/notification-channels/"+ch.ID, other.APIKey.Key, nil, 403, nil) // default key: no scope
	jar, _ := cookiejar.New(nil)
	ob := &http.Client{Jar: jar}
	var ores struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(ob, "POST", "/v1/workspaces", "", map[string]string{"name": "Other2", "setup_token": "tok"}, 201, &ores)
	e.do(ob, "GET", "/v1/notification-channels/"+ch.ID, "", nil, 404, nil)
	e.do(ob, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"name": "pwn"}, 404, nil, "X-CSRF-Token", ores.CSRF)
	e.do(ob, "DELETE", "/v1/notification-channels/"+ch.ID, "", nil, 404, nil, "X-CSRF-Token", ores.CSRF)
	var olist struct{ Data []app.NotificationChannel }
	e.do(ob, "GET", "/v1/notification-channels", "", nil, 200, &olist)
	if len(olist.Data) != 0 {
		t.Fatal("other workspace sees channels")
	}

	// Free plan: 2 channels.
	e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"topic": "t1"}}, 201)
	e.channel(map[string]any{"name": "N2", "type": "ntfy", "config": map[string]any{"topic": "t2"}}, 402)

	e.do(nil, "DELETE", "/v1/notification-channels/"+ch.ID, admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/notification-channels/"+ch.ID, admin, nil, 404, nil)
}

func TestNotifyOutboxTelegramPollingDecisions(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "TG", "type": "telegram", "config": map[string]any{"bot_token": tgToken, "chat_id": "42"}}, 201)
	send := "/bot" + tgToken + "/sendMessage"

	// Non-pending items don't notify; pending ones do.
	e.item(col.ID, map[string]any{"title": "draft", "status": "draft"})
	it := e.item(col.ID, map[string]any{"title": "Launch post <b>", "content": "Hello world"})
	e.process()
	msgs := fake.find(send)
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	m := jsonBody(t, msgs[0])
	if !strings.Contains(m["text"].(string), "Launch post &lt;b&gt;") || !strings.Contains(m["text"].(string), "https://app.example.com/items/"+it.ID) {
		t.Fatalf("text: %v", m["text"])
	}
	kb := m["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	row := kb[0].([]any)
	approveData := row[0].(map[string]any)["callback_data"].(string)
	reasonData := kb[1].([]any)[0].(map[string]any)["callback_data"].(string)
	if !strings.HasPrefix(approveData, "a|"+it.ID+".1|") || !strings.HasPrefix(reasonData, "f|"+it.ID+".1|") { // all buttons bound to the version shown (audit M1)
		t.Fatalf("callback: %s %s", approveData, reasonData)
	}
	e.process()
	if len(fake.find(send)) != 1 {
		t.Fatal("sent twice")
	}
	var dl struct{ Data []app.NotificationDelivery }
	e.do(e.browser, "GET", "/v1/notification-channels/"+ch.ID+"/deliveries", "", nil, 200, &dl)
	if len(dl.Data) != 1 || dl.Data[0].Status != "sent" || dl.Data[0].Attempts != 1 {
		t.Fatalf("deliveries: %+v", dl.Data)
	}

	cb := func(updateID int64, from int64, data string) map[string]any {
		return map[string]any{"update_id": updateID, "callback_query": map[string]any{"id": "cb" + strconv.FormatInt(updateID, 10),
			"from": map[string]any{"id": from, "username": "boss"}, "data": data,
			"message": map[string]any{"message_id": 7, "chat": map[string]any{"id": 42, "type": "private"}, "reply_markup": m["reply_markup"]}}}
	}
	// A stranger and a forged signature are refused.
	fake.queue(cb(100, 99, approveData))
	fake.queue(cb(101, 42, "a|"+it.ID+"|AAAAAAAAAAA"))
	e.svc.PollTelegramOnce(context.Background())
	if e.itemState(it.ID).Status != "pending" {
		t.Fatal("unauthorized callback decided the item")
	}
	answers := fake.find("/answerCallbackQuery")
	if len(answers) != 2 || jsonBody(t, answers[0])["show_alert"] != true {
		t.Fatalf("answers: %d", len(answers))
	}
	// The chat owner approves.
	fake.queue(cb(102, 42, approveData))
	e.svc.PollTelegramOnce(context.Background())
	got := e.itemState(it.ID)
	if got.Status != "approved" || e.lastActor(it.ID) != "Telegram: @boss" {
		t.Fatalf("after approve: %s by %s", got.Status, e.lastActor(it.ID))
	}
	edits := fake.find("/editMessageReplyMarkup")
	if len(edits) != 1 || !strings.Contains(string(edits[0].Body), "Approved by @boss") {
		t.Fatalf("edit: %v", edits)
	}
	// Offset advanced past handled updates.
	e.svc.PollTelegramOnce(context.Background())
	polls := fake.find("/getUpdates")
	if off := jsonBody(t, polls[len(polls)-1])["offset"].(float64); off != 103 {
		t.Fatalf("offset %v", off)
	}
	// Pressing again: already decided.
	fake.queue(cb(103, 42, approveData))
	e.svc.PollTelegramOnce(context.Background())
	answers = fake.find("/answerCallbackQuery")
	if !strings.Contains(string(answers[len(answers)-1].Body), "Already approved") {
		t.Fatalf("answer: %s", answers[len(answers)-1].Body)
	}

	// Reject with reason: button -> ForceReply prompt -> reply.
	it2 := e.item(col.ID, map[string]any{"title": "Second"})
	e.process()
	m2 := jsonBody(t, fake.find(send)[1])
	kb2 := m2["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	reasonData = kb2[1].([]any)[0].(map[string]any)["callback_data"].(string)
	fake.queue(map[string]any{"update_id": 200, "callback_query": map[string]any{"id": "c", "from": map[string]any{"id": 42},
		"data": reasonData, "message": map[string]any{"message_id": 8, "chat": map[string]any{"id": 42}}}})
	e.svc.PollTelegramOnce(context.Background())
	all := fake.find(send)
	prompt := jsonBody(t, all[len(all)-1])
	ptext := prompt["text"].(string)
	if !strings.Contains(ptext, "ref: "+it2.ID+".1.") || prompt["reply_markup"].(map[string]any)["force_reply"] != true {
		t.Fatalf("prompt: %v", prompt)
	}
	reply := func(id int64, from int64, text, replyTo string) map[string]any {
		return map[string]any{"update_id": id, "message": map[string]any{"message_id": 9, "from": map[string]any{"id": from, "first_name": "Ann"},
			"chat": map[string]any{"id": 42}, "text": text,
			"reply_to_message": map[string]any{"message_id": 8, "from": map[string]any{"id": 123456, "is_bot": true}, "chat": map[string]any{"id": 42}, "text": replyTo}}}
	}
	fake.queue(reply(201, 42, "Too long", strings.Replace(ptext, it2.ID+".", it2.ID+".x", 1))) // tampered ref: ignored
	e.svc.PollTelegramOnce(context.Background())
	if e.itemState(it2.ID).Status != "pending" {
		t.Fatal("tampered ref decided")
	}
	fake.queue(reply(202, 42, "Too long, shorten it", ptext))
	e.svc.PollTelegramOnce(context.Background())
	got2 := e.itemState(it2.ID)
	if got2.Status != "rejected" || got2.Feedback != "Too long, shorten it" || e.lastActor(it2.ID) != "Telegram: Ann" {
		t.Fatalf("reject with reason: %+v", got2)
	}

	// Allowlist: only listed users may decide, even the chat owner is not implied.
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"config": map[string]any{"approver_user_ids": []any{7, "8"}}}, 200, nil, "X-CSRF-Token", e.csrf)
	it3 := e.item(col.ID, map[string]any{"title": "Third"})
	e.process()
	m3 := jsonBody(t, fake.find(send)[len(fake.find(send))-1])
	a3 := m3["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)["callback_data"].(string)
	fake.queue(cb(300, 42, a3))
	e.svc.PollTelegramOnce(context.Background())
	if e.itemState(it3.ID).Status != "pending" {
		t.Fatal("non-listed user decided")
	}
	fake.queue(cb(301, 8, a3))
	e.svc.PollTelegramOnce(context.Background())
	if e.itemState(it3.ID).Status != "approved" {
		t.Fatal("listed approver could not decide")
	}
}

func TestNotifyEscalationRulesAndImages(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "TG", "type": "telegram", "config": map[string]any{"bot_token": tgToken, "chat_id": "42"}}, 201)
	rule := func(body map[string]any) {
		e.do(e.browser, "POST", "/v1/rules", "", body, 201, nil, "X-CSRF-Token", e.csrf)
	}
	rule(map[string]any{"name": "auto", "action": "auto_approve", "match": map[string]any{"source_pattern": "trusted"}})
	rule(map[string]any{"name": "esc", "action": "escalate", "match": map[string]any{"source_pattern": "urgent"}, "params": map[string]any{"note": "CEO asked"}})
	auto := e.item(col.ID, map[string]any{"title": "auto", "source": "trusted"})
	esc := e.item(col.ID, map[string]any{"title": "hot", "source": "urgent"})
	e.process()
	msgs := fake.find("/sendMessage")
	if len(msgs) != 1 {
		t.Fatalf("want one message (auto-approved skipped, escalation merged), got %d", len(msgs))
	}
	text := jsonBody(t, msgs[0])["text"].(string)
	if !strings.Contains(text, "Escalated") || !strings.Contains(text, "CEO asked") || strings.Contains(text, auto.ID) {
		t.Fatalf("text: %s", text)
	}
	var n int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE status = 'skipped'`).Scan(&n)
	if n != 1 {
		t.Fatalf("skipped rows %d", n)
	}
	_ = esc

	// Image items are uploaded to Telegram directly (our media needs auth).
	var img app.Item
	e.upload("/v1/collections/"+col.ID+"/items", e.key, map[string]string{"item": `{"title":"pic"}`}, "pic.png", pngBytes, 201, &img)
	e.process()
	photos := fake.find("/sendPhoto")
	if len(photos) != 1 || !strings.Contains(photos[0].Header.Get("Content-Type"), "multipart/form-data") ||
		!strings.Contains(string(photos[0].Body), "PNG") || !strings.Contains(string(photos[0].Body), "inline_keyboard") {
		t.Fatalf("photo upload: %d", len(photos))
	}
}

func TestTelegramWebhookMode(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "TG", "type": "telegram", "config": map[string]any{"bot_token": tgToken, "chat_id": "42", "mode": "webhook"}}, 201)
	if ch.IntegrationURL != e.srv.URL+"/v1/integrations/telegram/"+ch.ID {
		t.Fatalf("integration url %q", ch.IntegrationURL)
	}
	sets := fake.find("/setWebhook")
	if len(sets) != 1 {
		t.Fatal("setWebhook not called")
	}
	reg := jsonBody(t, sets[0])
	secret := reg["secret_token"].(string)
	if reg["url"] != ch.IntegrationURL || len(secret) < 32 {
		t.Fatalf("setWebhook: %v", reg)
	}
	it := e.item(col.ID, map[string]any{"title": "x"})
	e.process()
	m := jsonBody(t, fake.find("/sendMessage")[0])
	data := m["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)["callback_data"].(string)
	upd := map[string]any{"update_id": 1, "callback_query": map[string]any{"id": "c", "from": map[string]any{"id": 42}, "data": data,
		"message": map[string]any{"message_id": 7, "chat": map[string]any{"id": 42}}}}
	path := "/v1/integrations/telegram/" + ch.ID
	e.do(nil, "POST", path, "", upd, 401, nil)
	e.do(nil, "POST", path, "", upd, 401, nil, "X-Telegram-Bot-Api-Secret-Token", "wrong")
	e.do(nil, "POST", "/v1/integrations/telegram/nch_unknown", "", upd, 404, nil, "X-Telegram-Bot-Api-Secret-Token", secret)
	if e.itemState(it.ID).Status != "pending" {
		t.Fatal("decided without secret")
	}
	e.do(nil, "POST", path, "", upd, 200, nil, "X-Telegram-Bot-Api-Secret-Token", secret)
	if e.itemState(it.ID).Status != "approved" {
		t.Fatal("webhook callback did not approve")
	}
	// Polling skips webhook-mode channels.
	e.svc.PollTelegramOnce(context.Background())
	if len(fake.find("/getUpdates")) != 0 {
		t.Fatal("polled a webhook-mode channel")
	}
}

func TestNotifyDigestRetriesAutoDisable(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "Push", "type": "ntfy", "digest_window_seconds": 1,
		"config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "mytopic", "access_token": "tk_secretsecretsecret"}}, 201)
	e.item(col.ID, map[string]any{"title": "one"})
	e.item(col.ID, map[string]any{"title": "two"})
	e.process()
	if len(fake.find("/ntfy")) != 0 {
		t.Fatal("sent before the digest window closed")
	}
	time.Sleep(1100 * time.Millisecond)
	e.process()
	sent := fake.find("/ntfy")
	if len(sent) != 1 {
		t.Fatalf("want one digest, got %d", len(sent))
	}
	b := jsonBody(t, sent[0])
	if b["title"] != "2 updates in staqly" || !strings.Contains(b["message"].(string), "one") || !strings.Contains(b["message"].(string), "two") ||
		sent[0].Header.Get("Authorization") != "Bearer tk_secretsecretsecret" || b["topic"] != "mytopic" {
		t.Fatalf("digest: %v", b)
	}

	// Failures retry with backoff, then auto-disable after NOTIFY_MAX_FAILS=3.
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"digest_window_seconds": 0}, 200, nil, "X-CSRF-Token", e.csrf)
	fake.fail.Store(-1)
	e.item(col.ID, map[string]any{"title": "three"})
	var got app.NotificationChannel
	waitFor(t, func() bool {
		e.process()
		e.do(e.browser, "GET", "/v1/notification-channels/"+ch.ID, "", nil, 200, &got)
		return !got.Enabled
	})
	if got.FailCount != 3 || !strings.Contains(got.DisabledReason, "3 consecutive failures") || !strings.Contains(got.LastError, "HTTP 500") {
		t.Fatalf("disabled channel: %+v", got)
	}
	var disabledAt *string
	e.svc.DB.QueryRow(`SELECT disabled_at FROM notification_channels WHERE id = ?`, ch.ID).Scan(&disabledAt)
	if disabledAt == nil { // starts the NOTIFY_DISABLED_GRACE clock
		t.Fatal("auto-disable did not set disabled_at")
	}
	var dl struct{ Data []app.NotificationDelivery }
	e.do(e.browser, "GET", "/v1/notification-channels/"+ch.ID+"/deliveries", "", nil, 200, &dl)
	if dl.Data[0].Status != "pending" || dl.Data[0].Attempts != 3 || !strings.Contains(dl.Data[0].LastError, "boom") {
		t.Fatalf("delivery: %+v", dl.Data[0])
	}
	// Disabled: nothing more is attempted, and new events aren't queued.
	n := len(fake.find("/ntfy"))
	e.item(col.ID, map[string]any{"title": "four"})
	time.Sleep(50 * time.Millisecond)
	e.process()
	if len(fake.find("/ntfy")) != n {
		t.Fatal("disabled channel still sends")
	}
	// Re-enabling resets the failure count; the queued row is delivered.
	fake.fail.Store(0)
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"enabled": true}, 200, &got, "X-CSRF-Token", e.csrf)
	if got.FailCount != 0 || got.DisabledReason != "" {
		t.Fatalf("re-enabled: %+v", got)
	}
	waitFor(t, func() bool { e.process(); return len(fake.find("/ntfy")) > n })
	e.do(e.browser, "GET", "/v1/notification-channels/"+ch.ID, "", nil, 200, &got)
	if got.LastSentAt == nil || got.LastError != "" {
		t.Fatalf("after success: %+v", got)
	}
}

type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func TestDecisionLinks(t *testing.T) {
	fake := newFakeChat(t)
	logs := &lockedBuf{}
	e, vars := newNotifyEnv(t, fake, logs)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "Push", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t"}}, 201)
	it := e.item(col.ID, map[string]any{"title": "Launch <script>", "content": "Body text"})
	e.process()
	b := jsonBody(t, fake.find("/ntfy")[0])
	acts := b["actions"].([]any)
	approve := acts[0].(map[string]any)["url"].(string)
	reject := acts[1].(map[string]any)["url"].(string)
	if acts[0].(map[string]any)["method"] != "POST" || b["click"] != "https://app.example.com/items/"+it.ID || !strings.HasPrefix(approve, e.srv.URL+"/v1/decide/dl_") {
		t.Fatalf("ntfy body: %v", b)
	}
	get := func(u string) (int, string, http.Header) {
		resp, err := http.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw), resp.Header
	}
	post := func(u string, form url.Values) (int, string) {
		resp, err := http.PostForm(u, form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	// GET shows a confirmation page and does not decide (link previews are harmless).
	for range 2 {
		code, page, hdr := get(approve)
		if code != 200 || !strings.Contains(page, `<form method="post">`) || !strings.Contains(page, "Launch &lt;script&gt;") ||
			strings.Contains(page, "<script>") || !strings.Contains(hdr.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Fatalf("page %d: %s", code, page)
		}
	}
	if e.itemState(it.ID).Status != "pending" {
		t.Fatal("GET decided")
	}
	code, page := post(approve, nil)
	if code != 200 || !strings.Contains(page, "approved") {
		t.Fatalf("approve: %d %s", code, page)
	}
	if e.itemState(it.ID).Status != "approved" || e.lastActor(it.ID) != "Link: Push" {
		t.Fatalf("not approved via link: %s", e.lastActor(it.ID))
	}
	if code, _ := post(approve, nil); code != 410 {
		t.Fatalf("second use: %d", code)
	}
	if code, _, _ := get(approve); code != 410 {
		t.Fatalf("used link GET: %d", code)
	}
	if code, page := post(reject, nil); code != 409 || !strings.Contains(page, "already approved") {
		t.Fatalf("sibling link: %d %s", code, page)
	}
	if code, _ := post(reject, nil); code != 410 {
		t.Fatalf("sibling reuse: %d", code)
	}
	// Tokens are credentials: request logs show only the route.
	tok := approve[strings.LastIndex(approve, "/")+1:]
	if strings.Contains(logs.String(), tok) || !strings.Contains(logs.String(), "/v1/decide/{token}") {
		t.Fatal("decision token leaked into request logs")
	}
	if code, _, _ := get(e.srv.URL + "/v1/decide/dl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); code != 404 {
		t.Fatalf("unknown token: %d", code)
	}

	// Reject with feedback (form field), via JSON Accept too.
	it2 := e.item(col.ID, map[string]any{"title": "Two"})
	e.process()
	rej2 := jsonBody(t, fake.find("/ntfy")[1])["actions"].([]any)[1].(map[string]any)["url"].(string)
	if code, _ := post(rej2, url.Values{"feedback": {"Off-brand"}}); code != 200 {
		t.Fatalf("reject: %d", code)
	}
	if got := e.itemState(it2.ID); got.Status != "rejected" || got.Feedback != "Off-brand" {
		t.Fatalf("reject: %+v", got)
	}

	// Expired links are refused.
	vars.set("NOTIFY_LINK_TTL", "1ms")
	it3 := e.item(col.ID, map[string]any{"title": "Three"})
	e.process()
	ap3 := jsonBody(t, fake.find("/ntfy")[2])["actions"].([]any)[0].(map[string]any)["url"].(string)
	time.Sleep(5 * time.Millisecond)
	if code, _, _ := get(ap3); code != 410 {
		t.Fatalf("expired GET: %d", code)
	}
	if code, _ := post(ap3, nil); code != 410 || e.itemState(it3.ID).Status != "pending" {
		t.Fatalf("expired POST: %d", code)
	}
	// A link only ever acts inside its own workspace: the token row pins
	// workspace + item, and nothing in the request can change them.
	var ws string
	e.svc.DB.QueryRow(`SELECT DISTINCT workspace_id FROM decision_links`).Scan(&ws)
	var n int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM decision_links WHERE workspace_id <> ? OR token_hash LIKE 'dl_%'`, ws).Scan(&n)
	if n != 0 {
		t.Fatal("links must store only hashes in the item's workspace")
	}
}

func slackSign(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("v0:" + ts + ":"))
	m.Write(body)
	return "v0=" + hex.EncodeToString(m.Sum(nil))
}

func TestSlackInteractions(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	const signing = "slack-signing-secret-0123456789"
	ch := e.channel(map[string]any{"name": "Slack", "type": "slack", "config": map[string]any{
		"webhook_url": fake.srv.URL + "/slack-hook", "signing_secret": signing, "approver_user_ids": []string{"U1"}}}, 201)
	if ch.Config["signing_secret"] == signing || !ch.Interactive || ch.IntegrationURL != e.srv.URL+"/v1/integrations/slack/interactions" {
		t.Fatalf("channel: %+v", ch)
	}
	it := e.item(col.ID, map[string]any{"title": "Hi <!channel>"})
	e.process()
	msg := jsonBody(t, fake.find("/slack-hook")[0])
	raw, _ := json.Marshal(msg["blocks"])
	if strings.Contains(string(raw), "<!channel>") {
		t.Fatalf("mention not escaped: %s", raw)
	}
	var blocks []map[string]any
	json.Unmarshal(raw, &blocks)
	var value string
	for _, b := range blocks {
		if b["type"] == "actions" {
			value = b["elements"].([]any)[0].(map[string]any)["value"].(string)
		}
	}
	if value == "" {
		t.Fatalf("no buttons: %s", raw)
	}
	payload := func(user string) []byte {
		p, _ := json.Marshal(map[string]any{"type": "block_actions", "user": map[string]any{"id": user, "username": "ann"},
			"actions":      []any{map[string]any{"action_id": "staqly_a", "value": value}},
			"response_url": fake.srv.URL + "/slack-api/response", "message": map[string]any{"blocks": blocks}})
		return []byte(url.Values{"payload": {string(p)}}.Encode())
	}
	send := func(body []byte, ts, sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/integrations/slack/interactions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Slack-Request-Timestamp", ts)
		req.Header.Set("X-Slack-Signature", sig)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	body := payload("U1")
	if c := send(body, now, "v0=deadbeef"); c != 401 {
		t.Fatalf("invalid signature: %d", c)
	}
	if c := send(body, stale, slackSign(signing, stale, body)); c != 401 {
		t.Fatalf("stale timestamp: %d", c)
	}
	if c := send(body, now, slackSign("other-secret-0123456789", now, body)); c != 401 {
		t.Fatalf("wrong secret: %d", c)
	}
	other := payload("U2")
	if c := send(other, now, slackSign(signing, now, other)); c != 200 || e.itemState(it.ID).Status != "pending" {
		t.Fatalf("non-approver: %d", c)
	}
	waitFor(t, func() bool { return len(fake.find("/slack-api/response")) == 1 })
	if !strings.Contains(string(fake.find("/slack-api/response")[0].Body), "approver list") {
		t.Fatal("non-approver not told")
	}
	if c := send(body, now, slackSign(signing, now, body)); c != 200 {
		t.Fatalf("valid: %d", c)
	}
	if e.itemState(it.ID).Status != "approved" || e.lastActor(it.ID) != "Slack: ann" {
		t.Fatal("slack approve failed")
	}
	waitFor(t, func() bool { return len(fake.find("/slack-api/response")) == 2 })
	upd := jsonBody(t, fake.find("/slack-api/response")[1])
	if upd["replace_original"] != true || !strings.Contains(string(fake.find("/slack-api/response")[1].Body), "Approved by ann") {
		t.Fatalf("message update: %v", upd)
	}

	// Without a signing secret, messages carry one-tap link buttons instead.
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"config": map[string]any{"signing_secret": nil}}, 200, nil, "X-CSRF-Token", e.csrf)
	e.item(col.ID, map[string]any{"title": "links"})
	e.process()
	last := fake.find("/slack-hook")
	if s := string(last[len(last)-1].Body); !strings.Contains(s, "/v1/decide/dl_") || strings.Contains(s, `"value"`) {
		t.Fatalf("link fallback: %s", s)
	}
}

func TestDiscordInteractions(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ch := e.channel(map[string]any{"name": "Discord", "type": "discord", "config": map[string]any{
		"bot_token": "discord-bot-token-0123456789", "channel": "123", "application_id": "999",
		"public_key": hex.EncodeToString(pub), "approver_user_ids": []string{"555"}}}, 201)
	if !ch.Interactive || ch.Config["bot_token"] == "discord-bot-token-0123456789" {
		t.Fatalf("channel: %+v", ch.Config)
	}
	it := e.item(col.ID, map[string]any{"title": "Hey @everyone"})
	e.process()
	posts := fake.find("/discord-api/channels/123/messages")
	if len(posts) != 1 || posts[0].Header.Get("Authorization") != "Bot discord-bot-token-0123456789" {
		t.Fatalf("discord post: %d", len(posts))
	}
	msg := jsonBody(t, posts[0])
	if !strings.Contains(string(posts[0].Body), `"allowed_mentions":{"parse":[]}`) || strings.Contains(string(posts[0].Body), "Hey @everyone") {
		t.Fatalf("mentions not neutralized: %s", posts[0].Body)
	}
	comps := msg["components"].([]any)
	btns := comps[0].(map[string]any)["components"].([]any)
	approveID := btns[0].(map[string]any)["custom_id"].(string)
	reasonID := btns[2].(map[string]any)["custom_id"].(string)
	if !strings.HasPrefix(reasonID, "sq|f|") || !strings.Contains(reasonID, "|"+it.ID+".1|") || !strings.Contains(approveID, "|"+it.ID+".1|") {
		t.Fatalf("custom ids: %s %s", approveID, reasonID)
	}

	call := func(body map[string]any, signer ed25519.PrivateKey, tsOffset time.Duration) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		ts := strconv.FormatInt(time.Now().Add(tsOffset).Unix(), 10)
		sig := hex.EncodeToString(ed25519.Sign(signer, append([]byte(ts), raw...)))
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/integrations/discord/interactions", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-Ed25519", sig)
		req.Header.Set("X-Signature-Timestamp", ts)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	_, wrong, _ := ed25519.GenerateKey(rand.Reader)
	if c, out := call(map[string]any{"type": 1, "application_id": "999"}, priv, 0); c != 200 || out["type"] != float64(1) {
		t.Fatalf("PING: %d %v", c, out)
	}
	if c, _ := call(map[string]any{"type": 1, "application_id": "999"}, wrong, 0); c != 401 {
		t.Fatalf("bad signature PING: %d", c)
	}
	press := func(user, customID string) map[string]any {
		return map[string]any{"type": 3, "application_id": "999", "data": map[string]any{"custom_id": customID},
			"member":  map[string]any{"user": map[string]any{"id": user, "username": "bob"}},
			"message": map[string]any{"components": comps}}
	}
	if c, _ := call(press("555", approveID), priv, -10*time.Minute); c != 401 {
		t.Fatalf("stale: %d", c)
	}
	if c, _ := call(press("555", approveID), wrong, 0); c != 401 {
		t.Fatalf("forged: %d", c)
	}
	if c, out := call(press("666", approveID), priv, 0); c != 200 || out["data"].(map[string]any)["flags"] != float64(64) || e.itemState(it.ID).Status != "pending" {
		t.Fatalf("non-approver: %d %v", c, out)
	}
	c, out := call(press("555", approveID), priv, 0)
	if c != 200 || out["type"] != float64(7) || e.itemState(it.ID).Status != "approved" || e.lastActor(it.ID) != "Discord: bob" {
		t.Fatalf("approve: %d %v", c, out)
	}
	if !strings.Contains(mustMarshal(out), "Approved by bob") {
		t.Fatalf("update: %v", out)
	}

	// Reject with reason: modal, then submit.
	it2 := e.item(col.ID, map[string]any{"title": "Two"})
	e.process()
	msg2 := jsonBody(t, fake.find("/discord-api/channels/123/messages")[1])
	comps = msg2["components"].([]any)
	reasonID = comps[0].(map[string]any)["components"].([]any)[2].(map[string]any)["custom_id"].(string)
	c, out = call(press("555", reasonID), priv, 0)
	if c != 200 || out["type"] != float64(9) {
		t.Fatalf("modal: %d %v", c, out)
	}
	modalID := out["data"].(map[string]any)["custom_id"].(string)
	submit := map[string]any{"type": 5, "application_id": "999", "member": map[string]any{"user": map[string]any{"id": "555", "username": "bob"}},
		"data": map[string]any{"custom_id": modalID, "components": []any{map[string]any{"type": 1, "components": []any{
			map[string]any{"type": 4, "custom_id": "reason", "value": "Wrong tone"}}}}},
		"message": map[string]any{"components": comps}}
	if c, out = call(submit, priv, 0); c != 200 || out["type"] != float64(7) {
		t.Fatalf("submit: %d %v", c, out)
	}
	if got := e.itemState(it2.ID); got.Status != "rejected" || got.Feedback != "Wrong tone" {
		t.Fatalf("modal reject: %+v", got)
	}
}

func mustMarshal(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestDiscordWebhookLinksAndImage(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "DW", "type": "discord", "config": map[string]any{"webhook_url": fake.srv.URL + "/dhook/1/abc"}}, 201)
	var img app.Item
	e.upload("/v1/collections/"+col.ID+"/items", e.key, map[string]string{"item": `{"title":"pic"}`}, "pic.png", pngBytes, 201, &img)
	e.process()
	got := fake.find("/dhook/1/abc")
	if len(got) != 1 || !strings.Contains(got[0].Header.Get("Content-Type"), "multipart/form-data") {
		t.Fatalf("discord webhook: %d", len(got))
	}
	body := string(got[0].Body)
	if !strings.Contains(body, "payload_json") || !strings.Contains(body, "attachment://pic.png") || !strings.Contains(body, "/v1/decide/dl_") {
		t.Fatalf("body: %s", body)
	}
}

// ---- SMTP ----

type smtpMsg struct {
	From string
	To   []string
	Data string
}

func fakeSMTP(t *testing.T) (string, int, chan smtpMsg) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan smtpMsg, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := func(s string) { c.Write([]byte(s + "\r\n")) }
				w("220 fake ESMTP")
				var m smtpMsg
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						w("250-fake\r\n250 8BITMIME")
					case strings.HasPrefix(cmd, "MAIL FROM:"):
						m.From = strings.TrimSpace(line[10:])
						w("250 ok")
					case strings.HasPrefix(cmd, "RCPT TO:"):
						m.To = append(m.To, strings.TrimSpace(line[8:]))
						w("250 ok")
					case cmd == "DATA":
						w("354 go")
						var sb strings.Builder
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							sb.WriteString(l)
						}
						m.Data = sb.String()
						out <- m
						w("250 queued")
					case cmd == "QUIT":
						w("221 bye")
						return
					default:
						w("250 ok")
					}
				}
			}(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, out
}

func TestEmailChannelSMTP(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	host, port, inbox := fakeSMTP(t)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "Mail", "type": "email", "config": map[string]any{
		"host": host, "port": port, "tls": "none", "from": "staqly <bot@example.com>", "to": []string{"me@example.com"}, "password": "smtp-password-123456"}}, 201)
	if ch.Config["password"] != "••••3456" {
		t.Fatalf("password not masked: %v", ch.Config["password"])
	}
	// STARTTLS is required by default: a server without it is refused.
	starttls := e.channel(map[string]any{"name": "Mail2", "type": "email", "config": map[string]any{
		"host": host, "port": port, "from": "bot@example.com", "to": []string{"me@example.com"}}}, 201)
	var res map[string]any
	e.do(e.browser, "POST", "/v1/notification-channels/"+starttls.ID+"/test", "", nil, 200, &res, "X-CSRF-Token", e.csrf)
	if res["ok"] != false || !strings.Contains(res["error"].(string), "STARTTLS") {
		t.Fatalf("starttls: %v", res)
	}
	e.do(e.browser, "DELETE", "/v1/notification-channels/"+starttls.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)

	it := e.item(col.ID, map[string]any{"title": "Newsletter draft", "content": "Dear readers"})
	e.process()
	var m smtpMsg
	select {
	case m = <-inbox:
	case <-time.After(5 * time.Second):
		t.Fatal("no email")
	}
	if !strings.HasPrefix(m.From, "<bot@example.com>") || len(m.To) != 1 || !strings.Contains(m.Data, "Subject: ") ||
		!strings.Contains(m.Data, "multipart/alternative") {
		t.Fatalf("email: %+v", m)
	}
	links := regexp.MustCompile(`/v1/decide/dl_[a-z2-7]+`).FindAllString(strings.ReplaceAll(m.Data, "=\r\n", ""), -1)
	if len(links) < 2 {
		t.Fatalf("no decision links in email: %s", m.Data)
	}
	if !strings.Contains(strings.ReplaceAll(m.Data, "=\r\n", ""), "https://app.example.com/items/"+it.ID) {
		t.Fatal("no dashboard link")
	}
}
