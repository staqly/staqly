package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// autoPublishCollection creates a collection whose publish starts right after approval.
func autoPublishCollection(e *env, name string) app.Collection {
	e.t.Helper()
	c := e.collection(name)
	var upd app.Collection
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "regenerate"}, {"name": "publish", "auto_run": true}, {"name": "retry"}, {"name": "archive"}}}, 200, &upd)
	if !upd.Actions[1].AutoRun {
		e.t.Fatalf("auto_run not saved: %+v", upd.Actions[1])
	}

	return upd
}

// expectAutoPublished checks an approved item went straight into the publish run: item.approved, then
// item.publish_requested by the same actor with data.auto_run.
func expectAutoPublished(e *env, id string) {
	e.t.Helper()
	it := e.itemState(id)
	if it.Status != "processing" || it.RunAction == nil || *it.RunAction != "publish" || it.Decision == nil || *it.Decision != "approved" {
		e.t.Fatalf("not auto-published: %s %v %v", it.Status, it.RunAction, it.Decision)
	}
	var page app.EventPage
	e.do(nil, "GET", "/v1/items/"+id+"/events", e.key, nil, 200, &page)
	if len(page.Data) < 2 || page.Data[0].Type != "item.publish_requested" || page.Data[1].Type != "item.approved" {
		e.t.Fatalf("events: %+v", page.Data)
	}
	if page.Data[0].Actor != page.Data[1].Actor {
		e.t.Fatalf("actor: %+v vs %+v", page.Data[0].Actor, page.Data[1].Actor)
	}
	var data map[string]any
	json.Unmarshal(page.Data[0].Data, &data)
	if data["auto_run"] != true || data["action"] != "publish" || data["previous_status"] != "approved" {
		e.t.Fatalf("publish event data: %v", data)
	}
}

func TestAutoPublishSetting(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Flow")
	if c.Actions[1].AutoRun {
		t.Fatal("auto_run must default to off")
	}
	// Only when publish may run from approved; on other actions it is ignored.
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "publish", "auto_run": true, "allowed_statuses": []string{"completed"}}}}, 422, nil)
	var upd app.Collection
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "retry", "auto_run": true}, {"name": "publish", "allowed_statuses": []string{"approved", "completed"}, "auto_run": true}}}, 200, &upd)
	for _, a := range upd.Actions {
		if a.AutoRun != (a.Name == "publish") {
			t.Fatalf("auto_run on %s: %v", a.Name, a.AutoRun)
		}
	}
	// Stored data that no longer qualifies loses the setting on read.
	e.svc.DB.Exec(`UPDATE collections SET actions = '[{"name":"publish","allowed_statuses":["completed"],"auto_run":true}]' WHERE id = ?`, c.ID)
	var got app.Collection
	e.do(nil, "GET", "/v1/collections/"+c.ID, e.key, nil, 200, &got)
	if got.Actions[0].Name != "publish" || got.Actions[0].AutoRun {
		t.Fatalf("legacy auto_run: %+v", got.Actions[0])
	}
	// Off: approval just approves.
	it := e.item(c.ID, map[string]any{"title": "x"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, &got)
	if s := e.itemState(it.ID); s.Status != "approved" {
		t.Fatalf("approve without auto_run: %s", s.Status)
	}
}

func TestAutoPublishOnEveryApprovePath(t *testing.T) {
	e := newEnv(t)
	c := autoPublishCollection(e, "Posts")
	usage := func() int64 {
		var u app.Usage
		e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)

		return u.Usage["decisions_per_month"]
	}
	before := usage()

	// REST approve: the response already shows the run.
	a := e.item(c.ID, map[string]any{"title": "a"})
	var out app.Item
	e.do(nil, "POST", "/v1/items/"+a.ID+"/actions/approve", e.key, nil, 200, &out)
	if out.Status != "processing" || out.RunAction == nil || *out.RunAction != "publish" {
		t.Fatalf("approve response: %s %v", out.Status, out.RunAction)
	}
	expectAutoPublished(e, a.ID)

	// Edit & approve keeps the edits.
	b := e.item(c.ID, map[string]any{"title": "b"})
	e.do(nil, "POST", "/v1/items/"+b.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{"title": "b2"}}, 200, &out)
	if out.Title != "b2" {
		t.Fatalf("edits: %s", out.Title)
	}
	expectAutoPublished(e, b.ID)

	// Bulk.
	c1, c2 := e.item(c.ID, map[string]any{"title": "c1"}), e.item(c.ID, map[string]any{"title": "c2"})
	var bulk struct{ Data []app.BulkResult }
	e.do(nil, "POST", "/v1/items/bulk-actions", e.key, map[string]any{"ids": []string{c1.ID, c2.ID}, "action": "approve"}, 200, &bulk)
	expectAutoPublished(e, c1.ID)
	expectAutoPublished(e, c2.ID)

	// MCP.
	d := e.item(c.ID, map[string]any{"title": "d"})
	res := mcpResult(e, e.key, "approve_item", map[string]any{"item_id": d.ID})
	if res["status"] != "processing" {
		t.Fatalf("mcp approve: %v", res)
	}
	expectAutoPublished(e, d.ID)

	// Each approval is one decision; the publish run is not another.
	if got := usage() - before; got != 5 {
		t.Fatalf("decisions counted: %d, want 5", got)
	}
	// The run reports a result as usual.
	e.do(nil, "POST", "/v1/items/"+a.ID+"/result", e.key, map[string]any{"status": "completed"}, 200, &out)
	if out.Status != "completed" {
		t.Fatalf("result: %s", out.Status)
	}
}

func TestAutoPublishFromLinksAndChat(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	c := autoPublishCollection(e, "Posts")

	// Decision link (ntfy).
	e.channel(map[string]any{"name": "Push", "type": "ntfy", "config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "t"}}, 201)
	it := e.item(c.ID, map[string]any{"title": "linked"})
	e.process()
	approve := jsonBody(t, fake.find("/ntfy")[0])["actions"].([]any)[0].(map[string]any)["url"].(string)
	resp, err := http.PostForm(approve, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	expectAutoPublished(e, it.ID)

	// Slack button.
	const signing = "slack-signing-secret-0123456789"
	e.channel(map[string]any{"name": "Slack", "type": "slack", "config": map[string]any{
		"webhook_url": fake.srv.URL + "/slack-hook", "signing_secret": signing, "approver_user_ids": []string{"U1"}}}, 201)
	chat := e.item(c.ID, map[string]any{"title": "chat"})
	e.process()
	sent := fake.find("/slack-hook")
	var msg struct{ Blocks []map[string]any }
	json.Unmarshal(sent[len(sent)-1].Body, &msg)
	value := ""
	for _, b := range msg.Blocks {
		if b["type"] == "actions" {
			value = b["elements"].([]any)[0].(map[string]any)["value"].(string)
		}
	}
	if !strings.Contains(value, chat.ID) {
		t.Fatalf("button value: %q", value)
	}
	p, _ := json.Marshal(map[string]any{"type": "block_actions", "user": map[string]any{"id": "U1", "username": "ann"},
		"actions": []any{map[string]any{"action_id": "staqly_a", "value": value}}, "response_url": fake.srv.URL + "/slack-api/response"})
	form := []byte(url.Values{"payload": {string(p)}}.Encode())
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/integrations/slack/interactions", bytes.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", slackSign(signing, ts, form))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitFor(t, func() bool { return e.itemState(chat.ID).Status == "processing" })
	expectAutoPublished(e, chat.ID)
}
