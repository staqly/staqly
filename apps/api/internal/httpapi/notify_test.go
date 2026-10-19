package httpapi_test

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
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
	database, err := storetest.Open(t, dir+"/workspace.db")
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
		"PUBLIC_DASHBOARD_URL": "https://app.example.com", "RATE_LIMIT_DISABLED": "true",
	}}
	svc.Getenv = vars.get
	h := httpapi.New(svc, log, httpapi.Config{MCP: mcpserver.HTTPHandler(svc, "test"), OpenAPISpec: []byte("openapi: 3.1.0")})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, svc: svc, srv: srv, browser: &http.Client{Jar: jar}}
	e.signup()
	return e, vars
}

func (e *env) channel(body map[string]any, want int) app.Alert {
	e.t.Helper()
	var ch app.Alert
	e.do(e.browser, "POST", "/v1/alerts", "", body, want, &ch, "X-CSRF-Token", e.csrf)
	return ch
}

func (e *env) process() {
	e.t.Helper()
	if err := e.svc.ProcessNotifyOutbox(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) postState(id string) app.Post {
	var p app.Post
	e.do(nil, "GET", "/v1/posts/"+id, e.key, nil, 200, &p)
	return p
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
	if len(ch.Events) != 3 || ch.Events[0] != "post.published" || !ch.Enabled {
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
	e.do(nil, "GET", "/v1/alerts", e.key, nil, 403, nil)
	e.do(nil, "POST", "/v1/alerts", e.key, map[string]any{"name": "x", "type": "ntfy", "config": map[string]any{"topic": "t"}}, 403, nil)
	admin := e.keyWith("admin")
	var list struct{ Data []app.Alert }
	e.do(nil, "GET", "/v1/alerts", admin, nil, 200, &list)
	if len(list.Data) != 1 {
		t.Fatalf("list: %+v", list.Data)
	}

	// Echoing the masked token keeps the stored secret...
	var upd app.Alert
	e.do(nil, "PATCH", "/v1/alerts/"+ch.ID, admin, map[string]any{"name": "Phone 2", "config": map[string]any{"bot_token": ch.Config["bot_token"]}}, 200, &upd)
	// ...but retargeting (audit M3: new chat_id) requires the token again.
	e.do(nil, "PATCH", "/v1/alerts/"+ch.ID, admin, map[string]any{"config": map[string]any{"bot_token": ch.Config["bot_token"], "chat_id": "43"}}, 422, nil)
	e.do(nil, "PATCH", "/v1/alerts/"+ch.ID, admin, map[string]any{"config": map[string]any{"chat_id": "43"}}, 422, nil)
	e.do(nil, "PATCH", "/v1/alerts/"+ch.ID, admin, map[string]any{"config": map[string]any{"bot_token": tgToken, "chat_id": "43"}}, 200, &upd)
	if upd.Config["chat_id"] != "43" {
		t.Fatalf("update: %+v", upd.Config)
	}
	var res map[string]any
	e.do(nil, "POST", "/v1/alerts/"+ch.ID+"/test", admin, nil, 200, &res)
	if res["ok"] != true || len(fake.find("/bot"+tgToken+"/sendMessage")) != 1 {
		t.Fatalf("test send: %v", res)
	}
	// Failing test send reports the error without leaking the token.
	fake.fail.Store(1)
	e.do(nil, "POST", "/v1/alerts/"+ch.ID+"/test", admin, nil, 200, &res)
	if res["ok"] != false || strings.Contains(res["error"].(string), "ABCDEF") || !strings.Contains(res["error"].(string), "boom") {
		t.Fatalf("failed test: %v", res)
	}

	// Validation.
	e.do(nil, "POST", "/v1/alerts", admin, map[string]any{"name": "x", "type": "telegram", "config": map[string]any{"bot_token": "nope", "chat_id": "1"}}, 422, nil)
	e.do(nil, "POST", "/v1/alerts", admin, map[string]any{"name": "x", "type": "ntfy", "config": map[string]any{"topic": "t", "bogus": 1}}, 422, nil)
	e.do(nil, "POST", "/v1/alerts", admin, map[string]any{"name": "x", "type": "sms", "config": map[string]any{}}, 422, nil)
	e.do(nil, "PATCH", "/v1/alerts/"+ch.ID, admin, map[string]any{"type": "slack"}, 422, nil)
	e.do(nil, "POST", "/v1/alerts", admin, map[string]any{"name": "x", "type": "email", "config": map[string]any{"host": "smtp.example.com", "from": "a@example.com", "to": []string{"bad\r\nBcc: x@y.z"}}}, 422, nil)

	// Workspace isolation.
	var other struct {
		APIKey app.APIKey `json:"api_key"`
	}
	e.do(nil, "POST", "/v1/workspaces", "", map[string]string{"name": "Other", "setup_token": "tok"}, 201, &other)
	e.do(nil, "GET", "/v1/alerts/"+ch.ID, other.APIKey.Key, nil, 404, nil) // the other workspace's admin key
	jar, _ := cookiejar.New(nil)
	ob := &http.Client{Jar: jar}
	var ores struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(ob, "POST", "/v1/workspaces", "", map[string]string{"name": "Other2", "setup_token": "tok"}, 201, &ores)
	e.do(ob, "GET", "/v1/alerts/"+ch.ID, "", nil, 404, nil)
	e.do(ob, "PATCH", "/v1/alerts/"+ch.ID, "", map[string]any{"name": "pwn"}, 404, nil, "X-CSRF-Token", ores.CSRF)
	e.do(ob, "DELETE", "/v1/alerts/"+ch.ID, "", nil, 404, nil, "X-CSRF-Token", ores.CSRF)
	var olist struct{ Data []app.Alert }
	e.do(ob, "GET", "/v1/alerts", "", nil, 200, &olist)
	if len(olist.Data) != 0 {
		t.Fatal("other workspace sees channels")
	}

	// Free plan: 2 channels.
	e.free()
	e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"topic": "t1"}}, 201)
	e.channel(map[string]any{"name": "N2", "type": "ntfy", "config": map[string]any{"topic": "t2"}}, 402)

	e.do(nil, "DELETE", "/v1/alerts/"+ch.ID, admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/alerts/"+ch.ID, admin, nil, 404, nil)
}

func TestNotifyImages(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "TG", "type": "telegram", "events": []string{"post.created"}, "config": map[string]any{"bot_token": tgToken, "chat_id": "42"}}, 201)

	// A post's first image is uploaded to Telegram directly (our media needs auth).
	var m app.Media
	e.upload("/v1/media", e.key, nil, "pic.png", pngBytes, 201, &m)
	e.post(col.ID, map[string]any{"content": "pic", "media": []string{m.ID}})
	e.process()
	photos := fake.find("/sendPhoto")
	if len(photos) != 1 || !strings.Contains(photos[0].Header.Get("Content-Type"), "multipart/form-data") ||
		!strings.Contains(string(photos[0].Body), "PNG") || !strings.Contains(string(photos[0].Body), "inline_keyboard") {
		t.Fatalf("photo upload: %d", len(photos))
	}
}

func TestNotifyDigestRetriesAutoDisable(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "Push", "type": "ntfy", "digest_window_seconds": 1, "events": []string{"post.created"},
		"config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "mytopic", "access_token": "tk_secretsecretsecret"}}, 201)
	e.post(col.ID, map[string]any{"content": "one"})
	e.post(col.ID, map[string]any{"content": "two"})
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
	e.do(e.browser, "PATCH", "/v1/alerts/"+ch.ID, "", map[string]any{"digest_window_seconds": 0}, 200, nil, "X-CSRF-Token", e.csrf)
	fake.fail.Store(-1)
	e.post(col.ID, map[string]any{"content": "three"})
	var got app.Alert
	waitFor(t, func() bool {
		e.process()
		e.do(e.browser, "GET", "/v1/alerts/"+ch.ID, "", nil, 200, &got)
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
	var dl struct{ Data []app.AlertDelivery }
	e.do(e.browser, "GET", "/v1/alerts/"+ch.ID+"/deliveries", "", nil, 200, &dl)
	if dl.Data[0].Status != "pending" || dl.Data[0].Attempts != 3 || !strings.Contains(dl.Data[0].LastError, "boom") {
		t.Fatalf("delivery: %+v", dl.Data[0])
	}
	// Disabled: nothing more is attempted, and new events aren't queued.
	n := len(fake.find("/ntfy"))
	e.post(col.ID, map[string]any{"content": "four"})
	time.Sleep(50 * time.Millisecond)
	e.process()
	if len(fake.find("/ntfy")) != n {
		t.Fatal("disabled channel still sends")
	}
	// Re-enabling resets the failure count; the queued row is delivered.
	fake.fail.Store(0)
	e.do(e.browser, "PATCH", "/v1/alerts/"+ch.ID, "", map[string]any{"enabled": true}, 200, &got, "X-CSRF-Token", e.csrf)
	if got.FailCount != 0 || got.DisabledReason != "" {
		t.Fatalf("re-enabled: %+v", got)
	}
	waitFor(t, func() bool { e.process(); return len(fake.find("/ntfy")) > n })
	e.do(e.browser, "GET", "/v1/alerts/"+ch.ID, "", nil, 200, &got)
	if got.LastSentAt == nil || got.LastError != "" {
		t.Fatalf("after success: %+v", got)
	}
}

// A send cut off by a shutdown (the worker's context ends while the request is in flight) is not a failure of
// the channel: the rows stay pending, due and at their attempt count, the channel's failure count is not
// raised, and the next run sends them at once, as after a crash. Notification rows have no lease, so nothing
// holds them back after a restart. Before, every graceful stop during a send logged "notification failed"
// and an ERROR "notification state update failed ... context canceled".
func TestNotifySendInterruptedByShutdown(t *testing.T) {
	fake := newFakeChat(t)
	logs := &lockedBuf{}
	e, _ := newNotifyEnv(t, fake)
	arrived, release := make(chan struct{}, 4), make(chan struct{})
	var held atomic.Bool
	held.Store(true)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		arrived <- struct{}{}
		if held.Load() {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(func() { close(release); slow.Close() })
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "Push", "type": "ntfy", "events": []string{"post.created"}, "config": map[string]any{"server_url": slow.URL, "topic": "t"}}, 201)
	e.post(col.ID, map[string]any{"content": "in flight at shutdown"})

	// The worker logs through its context's logger.
	ctx, cancel := context.WithCancel(logctx.NewContext(context.Background(), slog.New(slog.NewTextHandler(logs, nil))))
	done := make(chan error, 1)
	go func() { done <- e.svc.ProcessNotifyOutbox(ctx) }()
	<-arrived
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var dl struct{ Data []app.AlertDelivery }
	e.do(e.browser, "GET", "/v1/alerts/"+ch.ID+"/deliveries", "", nil, 200, &dl)
	if len(dl.Data) != 1 || dl.Data[0].Status != "pending" || dl.Data[0].Attempts != 0 || dl.Data[0].LastError != "" {
		t.Fatalf("row after an interrupted send: %+v", dl.Data)
	}
	var got app.Alert
	e.do(e.browser, "GET", "/v1/alerts/"+ch.ID, "", nil, 200, &got)
	if got.FailCount != 0 || got.LastError != "" {
		t.Fatalf("an interrupted send counted as a channel failure: %+v", got)
	}
	if strings.Contains(logs.String(), "notification state update failed") || strings.Contains(logs.String(), "notification failed") {
		t.Fatalf("interrupted send logged as a failure:\n%s", logs.String())
	}

	// The next run (after the restart) sends it at once, exactly one more time.
	held.Store(false)
	e.process()
	e.do(e.browser, "GET", "/v1/alerts/"+ch.ID+"/deliveries", "", nil, 200, &dl)
	if dl.Data[0].Status != "sent" || dl.Data[0].Attempts != 1 {
		t.Fatalf("row after the next run: %+v", dl.Data[0])
	}
	if n := len(arrived); n != 1 {
		t.Fatalf("want one more request after the restart, got %d", n)
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

func slackSign(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("v0:" + ts + ":"))
	m.Write(body)
	return "v0=" + hex.EncodeToString(m.Sum(nil))
}

func mustMarshal(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestDiscordWebhookLinksAndImage(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "DW", "type": "discord", "events": []string{"post.created"}, "config": map[string]any{"webhook_url": fake.srv.URL + "/dhook/1/abc"}}, 201)
	var m app.Media
	e.upload("/v1/media", e.key, nil, "pic.png", pngBytes, 201, &m)
	post := e.post(col.ID, map[string]any{"content": "pic", "media": []string{m.ID}})
	e.process()
	got := fake.find("/dhook/1/abc")
	if len(got) != 1 || !strings.Contains(got[0].Header.Get("Content-Type"), "multipart/form-data") {
		t.Fatalf("discord webhook: %d", len(got))
	}
	body := string(got[0].Body)
	if !strings.Contains(body, "payload_json") || !strings.Contains(body, "attachment://pic.png") || !strings.Contains(body, "https://app.example.com/posts/"+post.ID) {
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
	ch := e.channel(map[string]any{"name": "Mail", "type": "email", "events": []string{"post.created"}, "config": map[string]any{
		"host": host, "port": port, "tls": "none", "from": "staqly <bot@example.com>", "to": []string{"me@example.com"}, "password": "smtp-password-123456"}}, 201)
	if ch.Config["password"] != "••••3456" {
		t.Fatalf("password not masked: %v", ch.Config["password"])
	}
	// STARTTLS is required by default: a server without it is refused.
	starttls := e.channel(map[string]any{"name": "Mail2", "type": "email", "config": map[string]any{
		"host": host, "port": port, "from": "bot@example.com", "to": []string{"me@example.com"}}}, 201)
	var res map[string]any
	e.do(e.browser, "POST", "/v1/alerts/"+starttls.ID+"/test", "", nil, 200, &res, "X-CSRF-Token", e.csrf)
	if res["ok"] != false || !strings.Contains(res["error"].(string), "STARTTLS") {
		t.Fatalf("starttls: %v", res)
	}
	e.do(e.browser, "DELETE", "/v1/alerts/"+starttls.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)

	it := e.post(col.ID, map[string]any{"content": "Newsletter draft: dear readers"})
	e.process()
	var m smtpMsg
	select {
	case m = <-inbox:
	case <-time.After(30 * time.Second):
		t.Fatal("no email")
	}
	if !strings.HasPrefix(m.From, "<bot@example.com>") || len(m.To) != 1 || !strings.Contains(m.Data, "Subject: ") ||
		!strings.Contains(m.Data, "multipart/alternative") {
		t.Fatalf("email: %+v", m)
	}
	if !strings.Contains(strings.ReplaceAll(m.Data, "=\r\n", ""), "https://app.example.com/posts/"+it.ID) {
		t.Fatal("no dashboard link")
	}
}
