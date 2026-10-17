package httpapi_test

// Regression tests for the backend security/robustness audit (IDs in each
// test's comment refer to the audit findings).

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
)

func principalOf(t *testing.T, e *env, key string) app.Principal {
	t.Helper()
	p, err := e.svc.AuthenticateAPIKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// INFO: local-mode Origin allowlist — only the dashboard's own origins,
// PUBLIC_DASHBOARD_URL and LOCAL_ALLOWED_ORIGINS; other localhost ports are refused.
func TestLocalOriginAllowlist(t *testing.T) {
	svc, srv := newLocal(t)
	n := 0
	post := func(origin string) int {
		n++
		code, _ := localReq(t, srv, "POST", "/v1/collections", "localhost:3333", origin, `{"name":"c `+strconv.Itoa(n)+`"}`)
		return code
	}
	for _, o := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080", "https://localhost:8080"} {
		if c := post(o); c != 201 {
			t.Fatalf("dashboard origin %s refused: %d", o, c)
		}
	}
	for _, o := range []string{"http://localhost:3000", "http://evil.localhost:8080", "http://localhost", "null", "http://192.168.1.20:8080"} {
		if c := post(o); c != 403 {
			t.Fatalf("origin %s accepted: %d", o, c)
		}
	}
	svc.Cfg.PublicDashboardURL = "http://192.168.1.20:8080/"
	svc.Cfg.LocalOrigins = []string{"http://localhost:6274"}
	svc.Cfg.DashboardPort = "3000"
	for _, o := range []string{"http://192.168.1.20:8080", "http://localhost:6274", "http://localhost:3000"} {
		if c := post(o); c != 201 {
			t.Fatalf("configured origin %s refused: %d", o, c)
		}
	}
	if c := post("http://localhost:8080"); c != 403 {
		t.Fatalf("old DASHBOARD_PORT origin still accepted: %d", c)
	}
}

// H2: in cloud mode webhook response bodies are never stored, and network
// errors are reduced to a short, address-free reason.
func TestCloudWebhookResponseNotStored(t *testing.T) {
	e := newEnv(t)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"internal":"secret-admin-panel"}`)
	}))
	defer recv.Close()
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": recv.URL, "max_attempts": 1}, 201, &wh)
	stop := runWorker(e)
	defer stop()
	e.do(nil, "POST", "/v1/webhooks/"+wh.ID+"/test", e.key, nil, 202, nil)
	var dl struct{ Data []app.Delivery }
	waitFor(t, func() bool {
		e.do(nil, "GET", "/v1/webhook-deliveries?webhook_id="+wh.ID, e.key, nil, 200, &dl)
		return len(dl.Data) == 1 && dl.Data[0].Status == "failed"
	})
	d := dl.Data[0]
	if d.LastResponse != "" || d.LastStatusCode == nil || *d.LastStatusCode != 500 || d.LastError != "receiver responded 500" {
		t.Fatalf("delivery: %+v", d)
	}
}

// H3: a search that exceeds the time budget fails fast (422 for full-text
// searches, 503 for plain listings) instead of holding the database.
func TestSearchTimeout(t *testing.T) {
	e := newEnv(t)
	c := e.collection("C")
	e.item(c.ID, map[string]any{"title": "hello world"})
	var page app.ItemPage
	e.do(nil, "GET", "/v1/items?q=hello", e.key, nil, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("search: %+v", page)
	}
	e.svc.SearchTimeout = time.Nanosecond
	defer func() { e.svc.SearchTimeout = 0 }()
	var errBody struct{ Error app.Error }
	e.do(nil, "GET", "/v1/items?q=hello", e.key, nil, 422, &errBody)
	if errBody.Error.Code != "search_too_broad" {
		t.Fatalf("code: %+v", errBody)
	}
	e.do(nil, "GET", "/v1/items", e.key, nil, 503, &errBody)
	if errBody.Error.Code != "timeout" {
		t.Fatalf("code: %+v", errBody)
	}
}

// H4/M14: the new indexes are used by the hot queries.
func TestQueryPlansUseIndexes(t *testing.T) {
	e := newEnv(t)
	plan := func(q string, args ...any) string {
		rows, err := e.svc.DB.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			rows.Scan(&id, &parent, &notused, &detail)
			out = append(out, detail)
		}
		return strings.Join(out, " | ")
	}
	for q, idx := range map[string]string{
		`SELECT * FROM events WHERE workspace_id = 'w' AND item_id = 'i' AND seq < 10 ORDER BY seq DESC LIMIT 5`: "events_item",
		`SELECT 1 FROM notification_outbox WHERE event_id = 'e'`:                                                 "notification_outbox_event",
		`SELECT 1 FROM notification_outbox WHERE workspace_id = 'w'`:                                             "notification_outbox_workspace",
		`SELECT COUNT(*) FROM items WHERE media_id = 'm'`:                                                        "items_media",
		`SELECT 1 FROM decision_links WHERE channel_id = 'c'`:                                                    "decision_links_channel",
		`SELECT * FROM webhook_deliveries WHERE workspace_id = 'w' ORDER BY created_at DESC LIMIT 50`:            "webhook_deliveries_workspace",
		`SELECT * FROM items i WHERE i.workspace_id = 'w' ORDER BY i.created_at DESC, i.id DESC LIMIT 50`:        "items_workspace_created",
		`SELECT * FROM items i WHERE i.workspace_id = 'w' ORDER BY i.updated_at DESC, i.id DESC LIMIT 50`:        "items_workspace_updated",
		`DELETE FROM idempotency_keys WHERE created_at < 'x'`:                                                    "idempotency_keys_created",
		`DELETE FROM watcher_seen WHERE seen_at < 'x'`:                                                           "watcher_seen_seen_at",
	} {
		if p := plan(q); !strings.Contains(p, idx) {
			t.Errorf("%s\n  plan: %s\n  want index %s", q, p, idx)
		}
	}
}

// H4: item history uses its own keyset query (newest first, paginated).
func TestItemHistoryPagination(t *testing.T) {
	e := newEnv(t)
	c := e.collection("C")
	it := e.item(c.ID, map[string]any{"title": "v1"})
	for i := range 3 {
		e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": fmt.Sprintf("v%d", i+2)}, 200, nil)
	}
	var seen []string
	cur := ""
	for {
		var page app.EventPage
		e.do(nil, "GET", "/v1/items/"+it.ID+"/events?limit=2&cursor="+cur, e.key, nil, 200, &page)
		for _, ev := range page.Data {
			seen = append(seen, ev.Type)
		}
		if page.NextCursor == "" {
			break
		}
		cur = page.NextCursor
	}
	if len(seen) != 4 || seen[0] != "item.updated" || seen[3] != "item.created" {
		t.Fatalf("history: %v", seen)
	}
}

// M1: after a decision, writers can't change what was approved, and status
// transitions beyond the agent lifecycle need items:review. The n8n demo
// flow keeps working.
func TestPostApprovalTamperingNeedsReview(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	writer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite, app.ScopeCollectionsRead)
	it := e.item(c.ID, map[string]any{"title": "draft caption"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	patch := func(body map[string]any, want int) {
		t.Helper()
		e.do(nil, "PATCH", "/v1/items/"+it.ID, writer, body, want, nil)
	}
	// Approved content is locked for everyone (409), not just for writers.
	patch(map[string]any{"title": "swapped after approval"}, 409)
	patch(map[string]any{"content": "x"}, 409)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "reviewer swap"}, 409, nil)
	patch(map[string]any{"metadata": map[string]any{"a": 1}}, 403)
	patch(map[string]any{"status": "pending"}, 403) // approved → pending
	// n8n demo: approved → processing → completed with metadata_patch.
	patch(map[string]any{"status": "processing"}, 200)
	patch(map[string]any{"status": "completed", "metadata_patch": map[string]any{"instagram_post_id": "123"}}, 200)
	if got := e.itemState(it.ID); got.Status != "completed" || got.Title != "draft caption" {
		t.Fatalf("item: %+v", got)
	}
	// Reviewers may still correct decided content.
	it2 := e.item(c.ID, map[string]any{"title": "second"})
	e.do(nil, "POST", "/v1/items/"+it2.ID+"/actions/reject", e.key, map[string]any{"feedback": "no"}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it2.ID, writer, map[string]any{"metadata_patch": map[string]any{"note": "ok"}}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it2.ID, writer, map[string]any{"title": "x"}, 403, nil)
	e.do(nil, "PATCH", "/v1/items/"+it2.ID, writer, map[string]any{"status": "processing"}, 403, nil) // rejected → processing
	// A reviewer's fix sends the rejected item back to pending (a new version needs a review).
	var fixed app.Item
	e.do(nil, "PATCH", "/v1/items/"+it2.ID, e.key, map[string]any{"title": "fixed by reviewer"}, 200, &fixed)
	if fixed.Status != "pending" {
		t.Fatalf("resubmitted: %s", fixed.Status)
	}
	// pending → completed skips review entirely.
	it3 := e.item(c.ID, map[string]any{"title": "third"})
	e.do(nil, "PATCH", "/v1/items/"+it3.ID, writer, map[string]any{"status": "completed"}, 409, nil)
	e.do(nil, "PATCH", "/v1/items/"+it3.ID, writer, map[string]any{"status": "draft"}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it3.ID, writer, map[string]any{"status": "pending"}, 200, nil)
	// Archiving untouched pending work rejects it: that is the reviewer's call.
	e.do(nil, "PATCH", "/v1/items/"+it3.ID, writer, map[string]any{"status": "archived"}, 403, nil)
	// reject → regenerate (processing) → archived, as the demo does on regeneration.
	e.do(nil, "POST", "/v1/items/"+it3.ID+"/actions/reject", e.key, map[string]any{"feedback": "again"}, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it3.ID+"/actions/regenerate", e.key, map[string]any{"feedback": "again"}, 200, nil)
	var archived app.Item
	e.do(nil, "PATCH", "/v1/items/"+it3.ID, writer, map[string]any{"status": "archived", "metadata_patch": map[string]any{"superseded_by": "x"}}, 200, &archived)
	if archived.Decision == nil || *archived.Decision != "rejected" || archived.ArchivedAt == nil {
		t.Fatalf("archived regenerated item: decision %v", archived.Decision)
	}
}

// M1: one-tap links approve exactly the version the message showed.
func TestDecisionLinkBoundToItemVersion(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "Push", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t"}}, 201)
	it := e.item(col.ID, map[string]any{"title": "Harmless"})
	e.process()
	approve := jsonBody(t, fake.find("/ntfy")[0])["actions"].([]any)[0].(map[string]any)["url"].(string)
	// The agent swaps the content after the notification went out.
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "Something else"}, 200, nil)
	resp, err := http.Get(approve)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 409 || !strings.Contains(string(page), "changed since this message") {
		t.Fatalf("GET stale link: %d %s", resp.StatusCode, page)
	}
	resp, err = http.PostForm(approve, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 409 || e.itemState(it.ID).Status != "pending" {
		t.Fatalf("stale approve: %d %s", resp.StatusCode, e.itemState(it.ID).Status)
	}
}

// M1 + M11: Slack buttons are bound to the version, and interactive Slack
// messages carry no unauthenticated decide links.
func TestSlackButtonsVersionBoundNoLinks(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	const signing = "slack-signing-secret-0123456789"
	ch := e.channel(map[string]any{"name": "Slack", "type": "slack", "config": map[string]any{
		"webhook_url": fake.srv.URL + "/slack-hook", "signing_secret": signing, "approver_user_ids": []string{"U1"}}}, 201)
	it := e.item(col.ID, map[string]any{"title": "v1"})
	e.process()
	body := string(fake.find("/slack-hook")[0].Body)
	if strings.Contains(body, "/v1/decide/") {
		t.Fatalf("interactive message carries decide links: %s", body)
	}
	var msg struct{ Blocks []map[string]any }
	json.Unmarshal(fake.find("/slack-hook")[0].Body, &msg)
	value := ""
	for _, b := range msg.Blocks {
		if b["type"] == "actions" {
			value = b["elements"].([]any)[0].(map[string]any)["value"].(string)
		}
	}
	if !strings.Contains(value, it.ID+".1|") {
		t.Fatalf("button value not version-bound: %q", value)
	}
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "v2"}, 200, nil)
	p, _ := json.Marshal(map[string]any{"type": "block_actions", "user": map[string]any{"id": "U1", "username": "ann"},
		"actions": []any{map[string]any{"action_id": "staqly_a", "value": value}}, "response_url": fake.srv.URL + "/slack-api/response"})
	form := []byte(url.Values{"payload": {string(p)}}.Encode())
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/integrations/slack/interactions", bytes.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", slackSign(signing, ts, form))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if e.itemState(it.ID).Status != "pending" {
		t.Fatal("stale button approved changed content")
	}
	waitFor(t, func() bool { return len(fake.find("/slack-api/response")) == 1 })
	if !strings.Contains(string(fake.find("/slack-api/response")[0].Body), "changed since this message") {
		t.Fatalf("response: %s", fake.find("/slack-api/response")[0].Body)
	}
	// Signing secret but no approvers: buttons would be refused, so links are used.
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"config": map[string]any{"approver_user_ids": nil}}, 200, nil, "X-CSRF-Token", e.csrf)
	e.item(col.ID, map[string]any{"title": "links"})
	e.process()
	last := fake.find("/slack-hook")
	if s := string(last[len(last)-1].Body); !strings.Contains(s, "/v1/decide/dl_") || strings.Contains(s, `"value"`) {
		t.Fatalf("no-approver fallback: %s", s)
	}
}

// M1: Telegram "Reject with reason" is bound to the version the message
// showed: the button, the force-reply prompt and the decision after the reply.
func TestTelegramReasonFlowVersionBound(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	e.channel(map[string]any{"name": "TG", "type": "telegram", "config": map[string]any{"bot_token": tgToken, "chat_id": "42"}}, 201)
	send := "/bot" + tgToken + "/sendMessage"
	it := e.item(col.ID, map[string]any{"title": "v1"})
	e.process()
	kb := jsonBody(t, fake.find(send)[0])["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	reasonData := kb[1].([]any)[0].(map[string]any)["callback_data"].(string)
	if !strings.HasPrefix(reasonData, "f|"+it.ID+".1|") {
		t.Fatalf("reason button not version-bound: %s", reasonData)
	}
	press := func(id int64) {
		fake.queue(map[string]any{"update_id": id, "callback_query": map[string]any{"id": "c" + strconv.FormatInt(id, 10), "from": map[string]any{"id": 42},
			"data": reasonData, "message": map[string]any{"message_id": 8, "chat": map[string]any{"id": 42}}}})
		e.svc.PollTelegramOnce(context.Background())
	}
	press(1)
	prompt := jsonBody(t, fake.find(send)[1])["text"].(string)
	if !strings.Contains(prompt, "ref: "+it.ID+".1.") {
		t.Fatalf("prompt not version-bound: %s", prompt)
	}
	// Forging another version in the prompt breaks the signature.
	forged := strings.Replace(prompt, it.ID+".1.", it.ID+".2.", 1)
	reply := func(id int64, replyTo string) {
		fake.queue(map[string]any{"update_id": id, "message": map[string]any{"message_id": 9, "from": map[string]any{"id": 42, "first_name": "Ann"},
			"chat": map[string]any{"id": 42}, "text": "Nope",
			"reply_to_message": map[string]any{"message_id": 8, "from": map[string]any{"id": 123456, "is_bot": true}, "chat": map[string]any{"id": 42}, "text": replyTo}}})
		e.svc.PollTelegramOnce(context.Background())
	}
	// The agent swaps the content after the prompt went out.
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "v2"}, 200, nil)
	reply(2, forged)
	if n := len(fake.find(send)); n != 2 {
		t.Fatalf("forged ref answered (%d messages)", n)
	}
	reply(3, prompt)
	if got := e.itemState(it.ID); got.Status != "pending" {
		t.Fatalf("stale reason rejected changed content: %+v", got)
	}
	all := fake.find(send)
	if txt := jsonBody(t, all[len(all)-1])["text"].(string); !strings.Contains(txt, "item changed since this message") {
		t.Fatalf("reply: %s", txt)
	}
	// Pressing the stale button again doesn't even prompt.
	press(4)
	if len(fake.find(send)) != len(all) {
		t.Fatal("stale reason button prompted")
	}
	answers := fake.find("/answerCallbackQuery")
	if !strings.Contains(string(answers[len(answers)-1].Body), "changed since this message") {
		t.Fatalf("answer: %s", answers[len(answers)-1].Body)
	}
}

// M1: the Discord "Reject with reason" modal carries the version-bound ref.
func TestDiscordReasonModalVersionBound(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	e.channel(map[string]any{"name": "Discord", "type": "discord", "config": map[string]any{
		"bot_token": "discord-bot-token-0123456789", "channel": "123", "application_id": "999",
		"public_key": hex.EncodeToString(pub), "approver_user_ids": []string{"555"}}}, 201)
	it := e.item(col.ID, map[string]any{"title": "v1"})
	e.process()
	msg := jsonBody(t, fake.find("/discord-api/channels/123/messages")[0])
	comps := msg["components"].([]any)
	reasonID := comps[0].(map[string]any)["components"].([]any)[2].(map[string]any)["custom_id"].(string)
	if !strings.Contains(reasonID, "|"+it.ID+".1|") {
		t.Fatalf("reason button not version-bound: %s", reasonID)
	}
	call := func(body map[string]any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/integrations/discord/interactions", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), raw...))))
		req.Header.Set("X-Signature-Timestamp", ts)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	member := map[string]any{"user": map[string]any{"id": "555", "username": "bob"}}
	press := func() map[string]any {
		return call(map[string]any{"type": 3, "application_id": "999", "data": map[string]any{"custom_id": reasonID},
			"member": member, "message": map[string]any{"components": comps}})
	}
	submit := func(modalID string) map[string]any {
		return call(map[string]any{"type": 5, "application_id": "999", "member": member,
			"data": map[string]any{"custom_id": modalID, "components": []any{map[string]any{"type": 1, "components": []any{
				map[string]any{"type": 4, "custom_id": "reason", "value": "Wrong tone"}}}}},
			"message": map[string]any{"components": comps}})
	}
	out := press()
	modalID := out["data"].(map[string]any)["custom_id"].(string)
	if out["type"] != float64(9) || !strings.Contains(modalID, "|"+it.ID+".1|") {
		t.Fatalf("modal: %v", out)
	}
	if out := submit(strings.Replace(modalID, it.ID+".1|", it.ID+".2|", 1)); !strings.Contains(mustMarshal(out), "Invalid form") {
		t.Fatalf("forged version accepted: %v", out)
	}
	// The agent swaps the content while the modal is open.
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "v2"}, 200, nil)
	out = submit(modalID)
	if out["type"] != float64(4) || !strings.Contains(mustMarshal(out), "item changed since this message") || e.itemState(it.ID).Status != "pending" {
		t.Fatalf("stale modal submit: %v %s", out, e.itemState(it.ID).Status)
	}
	// The stale button no longer opens the modal.
	if out := press(); out["type"] != float64(4) || !strings.Contains(mustMarshal(out), "changed since this message") {
		t.Fatalf("stale button: %v", out)
	}
}

// M2: collection actions can't emit events staqly itself emits: the catalog fixes every event.
func TestActionsCannotEmitReservedEvents(t *testing.T) {
	e := newEnv(t)
	for _, ev := range []string{"item.approved", "item.rejected", "item.created", "item.completed"} {
		e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "x" + ev, "actions": []any{map[string]any{"name": "evil", "event": ev}}}, 422, nil)
	}
	c := e.collection("OK")
	var upd app.Collection
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []any{
		map[string]any{"name": "archive", "event": "item.approved", "set_status": "approved"}}}, 200, &upd)
	for _, a := range upd.Actions {
		if a.Event == "item.approved" || a.SetStatus == "approved" {
			t.Fatalf("submitted event kept: %+v", a)
		}
	}
	// Data stored before the catalog is dropped on read and can't run.
	if _, err := e.svc.DB.Exec(`UPDATE collections SET actions = '[{"name":"evil","label":"Evil","event":"item.approved","requires_feedback":false}]' WHERE id = ?`, c.ID); err != nil {
		t.Fatal(err)
	}
	it := e.item(c.ID, map[string]any{"title": "t"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/evil", e.key, nil, 404, nil)
}

// M3: changing where a channel sends requires the secret it would send.
func TestChannelRetargetRequiresSecret(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	patch := func(id string, cfg map[string]any, want int) {
		t.Helper()
		e.do(e.browser, "PATCH", "/v1/notification-channels/"+id, "", map[string]any{"config": cfg}, want, nil, "X-CSRF-Token", e.csrf)
	}
	ntfy := e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t", "access_token": "tk_secretsecretsecret"}}, 201)
	patch(ntfy.ID, map[string]any{"server_url": "https://attacker.example"}, 422)
	patch(ntfy.ID, map[string]any{"server_url": "https://attacker.example", "access_token": ntfy.Config["access_token"]}, 422) // echoed mask
	patch(ntfy.ID, map[string]any{"topic": "t2"}, 200)                                                                         // not a destination change
	patch(ntfy.ID, map[string]any{"server_url": "https://ntfy.example", "access_token": "tk_newsecretnewsecret"}, 200)
	e.do(e.browser, "DELETE", "/v1/notification-channels/"+ntfy.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf) // Free plan: 2 channels
	mail := e.channel(map[string]any{"name": "M", "type": "email", "config": map[string]any{"host": "smtp.example.com", "from": "a@example.com",
		"to": []string{"b@example.com"}, "username": "u", "password": "pw-secret"}}, 201)
	patch(mail.ID, map[string]any{"host": "evil.example.com"}, 422)
	patch(mail.ID, map[string]any{"port": 2525}, 422)
	patch(mail.ID, map[string]any{"username": "other"}, 422)
	patch(mail.ID, map[string]any{"host": "evil.example.com", "password": "pw-secret"}, 200)
	e.do(e.browser, "DELETE", "/v1/notification-channels/"+mail.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
	// Without a stored secret there is nothing to protect.
	plain := e.channel(map[string]any{"name": "P", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t"}}, 201)
	patch(plain.ID, map[string]any{"server_url": "https://ntfy.example"}, 200)
}

// M7: responses carrying secrets are never cached for idempotent replay;
// ordinary ones still are.
func TestIdempotencyDoesNotStoreSecrets(t *testing.T) {
	e := newEnv(t)
	e.paid()
	create := func(key string) (string, http.Header) {
		var k app.APIKey
		resp := e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "k", "scopes": []string{"items:read"}}, 201, &k,
			"X-CSRF-Token", e.csrf, "Idempotency-Key", key)
		return k.Key, resp.Header
	}
	k1, _ := create("same")
	k2, h2 := create("same")
	if k1 == "" || k2 == "" || k1 == k2 || h2.Get("Idempotent-Replayed") != "" {
		t.Fatalf("secret replayed or not created: %q %q %v", k1, k2, h2)
	}
	var n int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM idempotency_keys WHERE CAST(response_body AS TEXT) LIKE '%stk_%' OR CAST(response_body AS TEXT) LIKE '%whsec_%'`).Scan(&n)
	if n != 0 {
		t.Fatal("a secret was stored in idempotency_keys")
	}
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h"}, 201, nil, "Idempotency-Key", "wh")
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM idempotency_keys WHERE CAST(response_body AS TEXT) LIKE '%whsec_%'`).Scan(&n)
	if n != 0 {
		t.Fatal("webhook secret stored in idempotency_keys")
	}
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "a"}, 201, nil, "Idempotency-Key", "col")
	resp := e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "a"}, 201, nil, "Idempotency-Key", "col")
	if resp.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("ordinary responses must still replay")
	}
}

// M9: one workspace's slow receiver can't take every delivery slot.
func TestWebhookWorkerFairness(t *testing.T) {
	e := newEnv(t)
	other := newWorkspace(e, "B")
	release := make(chan struct{})
	var inflight, peak atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		inflight.Add(-1)
	}))
	defer slow.Close()
	var fastHit atomic.Bool
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fastHit.Store(true) }))
	defer fast.Close()
	ctx := context.Background()
	pa, pb := principalOf(t, e, e.key), principalOf(t, other, other.key)
	slowURL, fastURL := slow.URL, fast.URL
	wa, err := e.svc.CreateWebhook(ctx, pa, app.WebhookInput{URL: &slowURL})
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := e.svc.TestWebhook(ctx, pa, wa.ID); err != nil {
			t.Fatal(err)
		}
	}
	stop := runWorker(e)
	defer func() { close(release); stop() }()
	waitFor(t, func() bool { return inflight.Load() == 2 })
	wb, _ := e.svc.CreateWebhook(ctx, pb, app.WebhookInput{URL: &fastURL})
	if _, err := e.svc.TestWebhook(ctx, pb, wb.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, fastHit.Load)
	time.Sleep(50 * time.Millisecond)
	if p := peak.Load(); p > 2 {
		t.Fatalf("workspace A had %d deliveries in flight (cap 2)", p)
	}
}

// M9: the test endpoints are rate-limited per principal.
func TestTestEndpointsRateLimited(t *testing.T) {
	e := newEnv(t)
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h"}, 201, &wh)
	limited := 0
	for range 7 {
		code, hdr, _ := e.raw(nil, "POST", "/v1/webhooks/"+wh.ID+"/test", e.key, nil)
		if code == 429 {
			limited++
			if hdr.Get("Retry-After") == "" {
				t.Fatal("429 without Retry-After")
			}
		}
	}
	if limited != 2 {
		t.Fatalf("want 2 of 7 limited (burst 5), got %d", limited)
	}
	// Another principal has its own budget.
	other := e.keyWith(app.ScopeWebhooksManage)
	e.do(nil, "POST", "/v1/webhooks/"+wh.ID+"/test", other, nil, 202, nil)
}

// L1: running an action with edits needs items:write.
func TestActionEditsNeedItemsWrite(t *testing.T) {
	e := newEnv(t)
	c := e.collection("C")
	it := e.item(c.ID, map[string]any{"title": "t"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/reject", e.key, map[string]any{"feedback": "f"}, 200, nil)
	exec := e.keyWith(app.ScopeActionsExecute, app.ScopeItemsRead, app.ScopeCollectionsRead)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/regenerate", exec, map[string]any{"feedback": "f", "edits": map[string]any{"title": "hijack"}}, 403, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/regenerate", exec, map[string]any{"feedback": "f"}, 200, nil)
	if e.itemState(it.ID).Title != "t" {
		t.Fatal("edit applied")
	}
}

// L3: deleting or disabling a channel kills its decision links.
func TestDecisionLinksDieWithChannel(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	mk := func(name string) (app.NotificationChannel, string) {
		ch := e.channel(map[string]any{"name": name, "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": name}}, 201)
		return ch, ""
	}
	a, _ := mk("a")
	e.item(col.ID, map[string]any{"title": "one"})
	e.process()
	link := jsonBody(t, fake.find("/ntfy")[0])["actions"].([]any)[0].(map[string]any)["url"].(string)
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+a.ID, "", map[string]any{"enabled": false}, 200, nil, "X-CSRF-Token", e.csrf)
	if resp, _ := http.PostForm(link, nil); resp.StatusCode != 404 {
		t.Fatalf("link of disabled channel: %d", resp.StatusCode)
	}
	b, _ := mk("b")
	e.item(col.ID, map[string]any{"title": "two"})
	e.process()
	sent := fake.find("/ntfy")
	link2 := jsonBody(t, sent[len(sent)-1])["actions"].([]any)[0].(map[string]any)["url"].(string)
	// Auto-disabled (not through the API): the link is refused at redeem time.
	e.svc.DB.Exec(`UPDATE notification_channels SET enabled = 0 WHERE id = ?`, b.ID)
	if resp, _ := http.PostForm(link2, nil); resp.StatusCode != 410 {
		t.Fatalf("link of auto-disabled channel: %d", resp.StatusCode)
	}
	e.svc.DB.Exec(`UPDATE notification_channels SET enabled = 1 WHERE id = ?`, b.ID)
	e.do(e.browser, "DELETE", "/v1/notification-channels/"+b.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
	if resp, _ := http.PostForm(link2, nil); resp.StatusCode != 404 {
		t.Fatalf("link of deleted channel: %d", resp.StatusCode)
	}
}

// L4: a finished build can't resurrect an export that was expired while it
// ran, and orphaned ZIPs are swept.
func TestExportFinishConditionalAndSweep(t *testing.T) {
	e := newEnv(t)
	dir := withDataDir(e)
	ctx := context.Background()
	p := principalOf(t, e, e.key)
	p.Scopes = append(p.Scopes, app.ScopeAdmin)
	x, err := e.svc.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := e.svc.Q.ClaimWorkspaceExport(ctx, db.ClaimWorkspaceExportParams{ID: x.ID}); n != 1 {
		t.Fatal("claim")
	}
	c := e.collection("C")
	it := e.item(c.ID, map[string]any{"title": "personal data"})
	if _, err := e.svc.PurgeItem(ctx, p, it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.GetWorkspaceExport(ctx, p, x.ID)
	if got.Status != "expired" {
		t.Fatalf("running export not expired by purge: %s", got.Status)
	}
	if n, err := e.svc.Q.FinishWorkspaceExport(ctx, db.FinishWorkspaceExportParams{Status: "ready", ID: x.ID}); err != nil || n != 0 {
		t.Fatalf("finish must not revive an expired export: %d %v", n, err)
	}
	os.MkdirAll(filepath.Join(dir, "exports"), 0o750)
	orphan := filepath.Join(dir, "exports", "exp_orphan.zip")
	os.WriteFile(orphan, []byte("zip"), 0o640)
	e.svc.ProcessExports(ctx)
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphaned export file not swept")
	}
}

// L5: a Discord component interaction without custom_id is answered, not a panic.
func TestDiscordEmptyCustomIDNoPanic(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	pub, priv := discordKeys(t)
	e.channel(map[string]any{"name": "D", "type": "discord", "config": map[string]any{
		"bot_token": "discord-bot-token-0123456789", "channel": "123", "application_id": "999", "public_key": pub, "approver_user_ids": []string{"555"}}}, 201)
	code, out := discordCall(t, e, priv, map[string]any{"type": 3, "application_id": "999", "data": map[string]any{},
		"member": map[string]any{"user": map[string]any{"id": "555"}}})
	if code != 200 || !strings.Contains(mustMarshal(out), "Unsupported") {
		t.Fatalf("empty custom_id: %d %v", code, out)
	}
	// Unknown application id: rejected without trying other channels' keys.
	if code, _ := discordCall(t, e, priv, map[string]any{"type": 1, "application_id": "111"}); code != 401 {
		t.Fatalf("foreign application id: %d", code)
	}
}

// L6: deliveries queued for a webhook that is then disabled finish as failed.
func TestDisabledWebhookDeliveriesFail(t *testing.T) {
	e := newEnv(t)
	var hits atomic.Int32
	recv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer recv.Close()
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": recv.URL}, 201, &wh)
	e.do(nil, "POST", "/v1/webhooks/"+wh.ID+"/test", e.key, nil, 202, nil)
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.key, map[string]any{"active": false}, 200, nil)
	stop := runWorker(e)
	defer stop()
	var dl struct{ Data []app.Delivery }
	waitFor(t, func() bool {
		e.do(nil, "GET", "/v1/webhook-deliveries?webhook_id="+wh.ID, e.key, nil, 200, &dl)
		return len(dl.Data) == 1 && dl.Data[0].Status == "failed"
	})
	if dl.Data[0].LastError != "webhook disabled" || hits.Load() != 0 {
		t.Fatalf("delivery: %+v hits=%d", dl.Data[0], hits.Load())
	}
}

// L7: a watcher entry is only marked seen together with its item, and the
// per-run cap leaves the rest for the next run.
func TestWatcherSeenAtomicWithItem(t *testing.T) {
	e := newEnv(t)
	e.svc.Fetcher = http.DefaultClient
	feed := &feedServer{items: []string{rssItem("1", "Old", "x")}}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	c := e.collection("Inspiration")
	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "w", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 201, &w)
	run := func() app.WatcherRun {
		var r app.WatcherRun
		e.do(nil, "POST", "/v1/watchers/"+w.ID+"/run", e.key, nil, 200, &r)
		return r
	}
	run() // baseline
	feed.mu.Lock()
	feed.items = append([]string{rssItem("2", "New", "y")}, feed.items...)
	feed.mu.Unlock()
	e.svc.DB.Exec(`UPDATE collections SET archived_at = '2026-01-01T00:00:00.000000Z' WHERE id = ?`, c.ID)
	if r := run(); r.Status != "error" || r.NewItems != 0 {
		t.Fatalf("archived collection: %+v", r)
	}
	e.svc.DB.Exec(`UPDATE collections SET archived_at = NULL WHERE id = ?`, c.ID)
	if r := run(); r.NewItems != 1 {
		t.Fatalf("entry lost after a failed run: %+v", r)
	}
	feed.mu.Lock()
	for i := range 25 {
		feed.items = append([]string{rssItem(fmt.Sprintf("n%d", i), fmt.Sprintf("Post %d", i), "z")}, feed.items...)
	}
	feed.mu.Unlock()
	if r := run(); r.NewItems != 20 {
		t.Fatalf("cap: %+v", r)
	}
	if r := run(); r.NewItems != 5 {
		t.Fatalf("entries beyond the cap must come next run: %+v", r)
	}
}

// L9: a crafted cursor is a client error, not a 500.
func TestCraftedCursorIs422(t *testing.T) {
	e := newEnv(t)
	for _, raw := range []string{`{"v":{"a":1},"id":"x"}`, `{"v":[1,2],"id":"x"}`, `{"v":true,"id":"x"}`} {
		cur := base64.RawURLEncoding.EncodeToString([]byte(raw))
		e.do(nil, "GET", "/v1/items?cursor="+cur, e.key, nil, 422, nil)
	}
	cur := base64.RawURLEncoding.EncodeToString([]byte(`{"v":"2026-01-01","id":"x"}`))
	e.do(nil, "GET", "/v1/items?cursor="+cur, e.key, nil, 200, nil)
}

// L15: last_used_at is written at most once a minute per key.
func TestAPIKeyTouchThrottled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	lastUsed := func() string {
		var v string
		e.svc.DB.QueryRow(`SELECT COALESCE(last_used_at, '') FROM api_keys WHERE name = 'Default key'`).Scan(&v)
		return v
	}
	e.svc.AuthenticateAPIKey(ctx, e.key)
	first := lastUsed()
	time.Sleep(5 * time.Millisecond)
	e.svc.AuthenticateAPIKey(ctx, e.key)
	if first == "" || lastUsed() != first {
		t.Fatalf("touched again within a minute: %q → %q", first, lastUsed())
	}
	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	e.svc.AuthenticateAPIKey(ctx, e.key)
	if lastUsed() == first {
		t.Fatal("not touched after a minute")
	}
}

// L16: one file per multipart item; a failed create doesn't leak the upload.
func TestMultipartCreateItemCleanup(t *testing.T) {
	e := newEnv(t)
	c := e.collection("C")
	mediaCount := func() int {
		var n int
		e.svc.DB.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n)
		return n
	}
	send := func(build func(w *multipart.Writer)) int {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		build(w)
		w.Close()
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/collections/"+c.ID+"/items", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+e.key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64))
	file := func(w *multipart.Writer, name string) {
		fw, _ := w.CreateFormFile("file", name)
		fw.Write(png)
	}
	if code := send(func(w *multipart.Writer) { file(w, "a.png"); file(w, "b.png") }); code != 400 || mediaCount() != 0 {
		t.Fatalf("two files: %d, media=%d", code, mediaCount())
	}
	if code := send(func(w *multipart.Writer) { file(w, "a.png"); w.WriteField("item", `{"status":"bogus"}`) }); code != 422 || mediaCount() != 0 {
		t.Fatalf("failed create: %d, media=%d", code, mediaCount())
	}
	if code := send(func(w *multipart.Writer) { file(w, "a.png"); w.WriteField("item", `{"nope":1}`) }); code != 400 || mediaCount() != 0 {
		t.Fatalf("bad item JSON: %d, media=%d", code, mediaCount())
	}
	if code := send(func(w *multipart.Writer) { file(w, "a.png"); w.WriteField("item", `{"title":"ok"}`) }); code != 201 || mediaCount() != 1 {
		t.Fatalf("happy path: %d, media=%d", code, mediaCount())
	}
}

// L19: the stdio MCP server re-validates its key; revocation takes effect.
func TestMCPStdioKeyRevalidation(t *testing.T) {
	e := newEnv(t)
	key := e.keyWith(app.ScopeCollectionsRead)
	auth := mcpserver.KeyAuth(e.svc, key, 0)
	if p := auth(context.Background()); p.WorkspaceID == "" {
		t.Fatal("valid key rejected")
	}
	var keys struct{ Data []app.APIKey }
	e.do(e.browser, "GET", "/v1/api-keys", "", nil, 200, &keys)
	for _, k := range keys.Data {
		if strings.HasPrefix(key, k.Prefix) && k.RevokedAt == nil {
			e.do(e.browser, "DELETE", "/v1/api-keys/"+k.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
		}
	}
	if p := auth(context.Background()); p.WorkspaceID != "" {
		t.Fatal("revoked key still authenticates")
	}
	cached := mcpserver.KeyAuth(e.svc, e.key, time.Hour)
	for range 2 { // second call is served from the cache
		if cached(context.Background()).WorkspaceID == "" {
			t.Fatal("cached auth")
		}
	}
}

// INFO: credential/session audit events are visible to admins only.
func TestAuditEventsAdminOnly(t *testing.T) {
	e := newEnv(t)
	types := func(c *http.Client, key string) map[string]bool {
		var page app.EventPage
		e.do(c, "GET", "/v1/events?limit=200", key, nil, 200, &page)
		out := map[string]bool{}
		for _, ev := range page.Data {
			out[ev.Type] = true
		}
		return out
	}
	if got := types(e.browser, ""); !got["api_key.created"] {
		t.Fatalf("session should see audit events: %v", got)
	}
	if got := types(nil, e.key); got["api_key.created"] || !got["workspace.created"] {
		t.Fatalf("non-admin key sees audit events: %v", got)
	}
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?type=api_key.created", e.key, nil, 200, &page)
	if len(page.Data) != 0 {
		t.Fatal("type filter bypasses the audit restriction")
	}
	admin := e.keyWith(app.ScopeAdmin)
	if got := types(nil, admin); !got["api_key.created"] {
		t.Fatalf("admin key should see audit events: %v", got)
	}
}

// INFO: the webhook signing secret is shown only on create and rotate.
func TestWebhookSecretMasked(t *testing.T) {
	e := newEnv(t)
	var wh, got app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h"}, 201, &wh)
	if !strings.HasPrefix(wh.Secret, "whsec_") || strings.Contains(wh.Secret, "****") {
		t.Fatalf("create must return the secret: %q", wh.Secret)
	}
	e.do(nil, "GET", "/v1/webhooks/"+wh.ID, e.key, nil, 200, &got)
	if got.Secret == wh.Secret || !strings.HasPrefix(got.Secret, "whsec_****") || !strings.HasSuffix(wh.Secret, got.Secret[len(got.Secret)-4:]) {
		t.Fatalf("GET secret: %q", got.Secret)
	}
	var list struct{ Data []app.Webhook }
	e.do(nil, "GET", "/v1/webhooks", e.key, nil, 200, &list)
	if list.Data[0].Secret == wh.Secret {
		t.Fatal("list leaks the secret")
	}
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.key, map[string]any{"description": "d"}, 200, &got)
	if got.Secret == wh.Secret {
		t.Fatal("update leaks the secret")
	}
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.key, map[string]any{"rotate_secret": true}, 200, &got)
	if strings.Contains(got.Secret, "****") || got.Secret == wh.Secret {
		t.Fatalf("rotate must return the new secret: %q", got.Secret)
	}
}

// INFO: using or rotating the recovery key signs out every other session.
func TestRecoveryRevokesOtherSessions(t *testing.T) {
	e := newEnv(t)
	e.do(e.browser, "GET", "/v1/session", "", nil, 200, nil)
	jar, _ := cookiejar.New(nil)
	b := &http.Client{Jar: jar}
	var rec struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(b, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": e.recovery}, 201, &rec)
	e.do(e.browser, "GET", "/v1/session", "", nil, 401, nil)
	e.do(b, "GET", "/v1/session", "", nil, 200, nil)
	// A third browser via a device link, then B rotates the recovery key.
	var link app.DeviceLink
	e.do(b, "POST", "/v1/device-links", "", map[string]any{}, 201, &link, "X-CSRF-Token", rec.CSRF, "Origin", "https://staqly.example.com")
	jar3, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar3}
	e.do(c, "POST", "/v1/device-links/redeem", "", map[string]string{"token": strings.SplitN(link.URL, "#", 2)[1]}, 201, nil)
	e.do(c, "GET", "/v1/session", "", nil, 200, nil)
	e.do(b, "POST", "/v1/recovery-key/rotate", "", nil, 200, nil, "X-CSRF-Token", rec.CSRF)
	e.do(c, "GET", "/v1/session", "", nil, 401, nil)
	e.do(b, "GET", "/v1/session", "", nil, 200, nil)
}

// `staqly-api recovery-key` signs out every browser session of the workspace,
// like a dashboard rotation; API keys keep working.
func TestCLIRecoveryKeyRevokesSessions(t *testing.T) {
	e := newEnv(t)
	other := newEnv(t) // another workspace's sessions are untouched
	e.do(e.browser, "GET", "/v1/session", "", nil, 200, nil)
	ws := principalOf(t, e, e.key).WorkspaceID
	key, revoked, err := e.svc.ResetRecoveryKey(context.Background(), ws)
	if err != nil || revoked != 1 {
		t.Fatalf("reset: revoked=%d err=%v", revoked, err)
	}
	e.do(e.browser, "GET", "/v1/session", "", nil, 401, nil)
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, nil)
	other.do(other.browser, "GET", "/v1/session", "", nil, 200, nil)
	var data string
	e.svc.DB.QueryRow(`SELECT data FROM events WHERE workspace_id = ? AND type = 'recovery_key.rotated'`, ws).Scan(&data)
	if !strings.Contains(data, `"sessions_revoked":1`) || !strings.Contains(data, `"via":"cli"`) {
		t.Fatalf("event data: %q", data)
	}
	// The new key recovers; nothing left to revoke on a second reset but that session.
	if _, err := e.svc.Recover(context.Background(), key, "ua"); err != nil {
		t.Fatal(err)
	}
	if _, revoked, _ := e.svc.ResetRecoveryKey(context.Background(), ws); revoked != 1 {
		t.Fatalf("second reset revoked %d", revoked)
	}
	if _, _, err := e.svc.ResetRecoveryKey(context.Background(), "ws_missing"); err == nil {
		t.Fatal("unknown workspace accepted")
	}
}

// H4: notification outbox rows are pruned by retention.
func TestNotificationOutboxPruned(t *testing.T) {
	fake := newFakeChat(t)
	e, vars := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t"}}, 201)
	e.item(col.ID, map[string]any{"title": "sent"})
	e.process()
	// A channel that stays disabled keeps pending rows; they are skipped, then pruned.
	e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"enabled": false}, 200, nil, "X-CSRF-Token", e.csrf)
	e.item(col.ID, map[string]any{"title": "stuck"}) // not queued: the channel is disabled
	// A row queued just before the channel was disabled.
	if _, err := e.svc.DB.Exec(`INSERT INTO notification_outbox (id, workspace_id, channel_id, event_id, event_type, next_attempt_at, created_at)
		SELECT 'ntf_extra', workspace_id, channel_id, (SELECT id FROM events WHERE type = 'item.created' ORDER BY seq DESC LIMIT 1), 'item.created', created_at, created_at
		FROM notification_outbox LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	count := func(where string) int {
		var n int
		e.svc.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE ` + where).Scan(&n)
		return n
	}
	if count("status = 'sent'") != 1 || count("status = 'pending'") != 1 {
		t.Fatalf("setup: sent=%d pending=%d", count("status = 'sent'"), count("status = 'pending'"))
	}
	vars.set("RETENTION_NOTIFICATIONS_DAYS", "30")
	e.svc.Now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	defer func() { e.svc.Now = time.Now }()
	rep, err := e.svc.RunRetention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Notifications != 2 || count("1=1") != 0 {
		t.Fatalf("pruned %d, left %d", rep.Notifications, count("1=1"))
	}
}

// A disabled channel keeps its queued rows for NOTIFY_DISABLED_GRACE
// (default 24h): re-enabling within it delivers them; after it, retention
// marks them skipped. Re-enabling resets the clock.
func TestDisabledChannelGracePeriod(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t"}}, 201)
	start := time.Now()
	at := func(d time.Duration) { e.svc.Now = func() time.Time { return start.Add(d) } }
	defer func() { e.svc.Now = time.Now }()
	setEnabled := func(on bool) {
		t.Helper()
		e.do(e.browser, "PATCH", "/v1/notification-channels/"+ch.ID, "", map[string]any{"enabled": on}, 200, nil, "X-CSRF-Token", e.csrf)
	}
	retention := func() {
		t.Helper()
		if _, err := e.svc.RunRetention(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rowStatus := func(itemID string) (status, lastErr string) {
		e.svc.DB.QueryRow(`SELECT status, last_error FROM notification_outbox WHERE item_id = ?`, itemID).Scan(&status, &lastErr)
		return
	}
	disabledAt := func() (v *string) {
		e.svc.DB.QueryRow(`SELECT disabled_at FROM notification_channels WHERE id = ?`, ch.ID).Scan(&v)
		return
	}

	// Path 1: re-enabled within the grace period — queued rows are delivered.
	a := e.item(col.ID, map[string]any{"title": "queued A"})
	setEnabled(false)
	if disabledAt() == nil {
		t.Fatal("disabled_at not set")
	}
	e.process()
	if len(fake.find("/ntfy")) != 0 {
		t.Fatal("disabled channel sent")
	}
	at(23 * time.Hour)
	retention()
	if st, _ := rowStatus(a.ID); st != "pending" {
		t.Fatalf("within grace: %s", st)
	}
	setEnabled(true)
	if disabledAt() != nil {
		t.Fatal("disabled_at not cleared on re-enable")
	}
	e.process()
	if st, _ := rowStatus(a.ID); st != "sent" || len(fake.find("/ntfy")) != 1 {
		t.Fatalf("re-enabled within grace: %s, %d sent", st, len(fake.find("/ntfy")))
	}

	// Path 2: disabled again (the clock restarts) and kept off past the grace.
	b := e.item(col.ID, map[string]any{"title": "queued B"})
	setEnabled(false)
	at(46 * time.Hour) // 23h after the second disable, 46h after the first
	retention()
	if st, _ := rowStatus(b.ID); st != "pending" {
		t.Fatalf("grace measured from the first disable: %s", st)
	}
	at(48 * time.Hour)
	retention()
	if st, le := rowStatus(b.ID); st != "skipped" || le != "channel disabled" {
		t.Fatalf("past grace: %s %q", st, le)
	}
	setEnabled(true)
	e.process()
	if len(fake.find("/ntfy")) != 1 {
		t.Fatal("skipped row was delivered after re-enable")
	}
}

func discordKeys(t *testing.T) (string, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(pub), priv
}

func discordCall(t *testing.T, e *env, priv ed25519.PrivateKey, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/integrations/discord/interactions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), raw...))))
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

// Admin changes are recorded in the activity log (webhooks, workspace, plan).
func TestAdminChangesAreAudited(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", admin, map[string]any{"url": "https://example.com/hook"}, 201, &wh)
	e.do(nil, "DELETE", "/v1/webhooks/"+wh.ID, admin, nil, 204, nil)
	e.do(nil, "PATCH", "/v1/workspace", admin, map[string]any{"name": "Renamed"}, 200, nil)
	e.paid()
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?limit=200", admin, nil, 200, &page)
	got := map[string]bool{}
	for _, ev := range page.Data {
		got[ev.Type] = true
	}
	for _, want := range []string{"webhook.created", "webhook.deleted", "workspace.updated", "workspace.plan_changed"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}

// The activity list filters like the export: actor, type prefix and date range.
func TestEventListFilters(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", admin, map[string]any{"url": "https://example.com/hook"}, 201, &wh)
	e.collection("Filters")
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?type_prefix=webhook.", admin, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].Type != "webhook.created" {
		t.Fatalf("type_prefix: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/events?actor_id=nobody", admin, nil, 200, &page)
	if len(page.Data) != 0 {
		t.Fatalf("actor_id filter ignored: %d events", len(page.Data))
	}
	e.do(nil, "GET", "/v1/events?to=2000-01-01", admin, nil, 200, &page)
	if len(page.Data) != 0 {
		t.Fatalf("to filter ignored: %d events", len(page.Data))
	}
	e.do(nil, "GET", "/v1/events?from=2000-01-01&type=collection.created", admin, nil, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("from filter: %d events", len(page.Data))
	}
	e.do(nil, "GET", "/v1/events?from=yesterday", admin, nil, 422, nil)
}
