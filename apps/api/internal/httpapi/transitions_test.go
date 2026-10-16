package httpapi_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// A state that is already set cannot be set again: these answer 409 with a
// clear message and write no event; the opposite transition still works.

// wantConflict sends a request that must fail with 409 and the given message.
func (e *env) wantConflict(c *http.Client, method, path, auth string, body any, msg string, hdr ...string) {
	e.t.Helper()
	var out struct {
		Error app.Error `json:"error"`
	}
	e.do(c, method, path, auth, body, 409, &out, hdr...)
	if out.Error.Code != "conflict" || out.Error.Message != msg {
		e.t.Fatalf("%s %s: got %q (%s), want %q", method, path, out.Error.Message, out.Error.Code, msg)
	}
}

// eventCount counts activity events matching a query such as "type=rule.updated".
func (e *env) eventCount(query string) int {
	e.t.Helper()
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?limit=200&"+query, e.key, nil, 200, &page)
	return len(page.Data)
}

func TestNoopItemStatusTransitions(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	it := e.item(c.ID, map[string]any{"title": "Draft me"})
	path := "/v1/items/" + it.ID
	evs := e.eventCount("item_id=" + it.ID)

	// Status alone, equal to the current one: refused, no version, no event.
	e.wantConflict(nil, "PATCH", path, e.key, map[string]any{"status": "pending"}, "item is already pending")
	e.wantConflict(nil, "PATCH", path, e.key, map[string]any{"status": "pending", "expected_version": it.Version}, "item is already pending")
	var got app.Item
	e.do(nil, "GET", path, e.key, nil, 200, &got)
	if got.Version != it.Version || e.eventCount("item_id="+it.ID) != evs {
		t.Fatalf("no-op status wrote: version %d→%d, events %d→%d", it.Version, got.Version, evs, e.eventCount("item_id="+it.ID))
	}
	// Alongside a real change the unchanged status is ignored.
	e.do(nil, "PATCH", path, e.key, map[string]any{"status": "pending", "title": "Renamed"}, 200, &got)
	if got.Title != "Renamed" || got.Status != "pending" {
		t.Fatalf("combined patch: %+v", got)
	}
	// Archive, then archive again.
	e.do(nil, "PATCH", path, e.key, map[string]any{"status": "archived"}, 200, &got)
	e.wantConflict(nil, "PATCH", path, e.key, map[string]any{"status": "archived"}, "item is already archived")
	if n := e.eventCount("item_id=" + it.ID + "&type=item.status_changed"); n != 1 {
		t.Fatalf("status_changed events: %d", n)
	}
	// Restoring (the opposite transition) works.
	e.do(nil, "PATCH", path, e.key, map[string]any{"status": "pending"}, 200, &got)
	if got.Status != "pending" {
		t.Fatalf("restore: %s", got.Status)
	}
}

func TestNoopReportResultAndBulk(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	a := e.item(c.ID, map[string]any{"title": "A"})
	b := e.item(c.ID, map[string]any{"title": "B"})
	e.do(nil, "POST", "/v1/items/"+a.ID+"/actions/approve", e.key, map[string]any{}, 200, nil)

	// Bulk approve over an already approved item: per-item 409, one event only.
	var bulk struct {
		Data []app.BulkResult `json:"data"`
	}
	e.do(nil, "POST", "/v1/items/bulk-actions", e.key, map[string]any{"ids": []string{a.ID, b.ID}, "action": "approve"}, 200, &bulk)
	if len(bulk.Data) != 2 || bulk.Data[0].OK || bulk.Data[0].Error == nil || bulk.Data[0].Error.Code != "conflict" ||
		!strings.Contains(bulk.Data[0].Error.Message, "already approved") || !bulk.Data[1].OK {
		t.Fatalf("bulk: %+v", bulk.Data)
	}
	if n := e.eventCount("item_id=" + a.ID + "&type=item.approved"); n != 1 {
		t.Fatalf("item.approved events for a: %d", n)
	}

	// Reporting the same result twice.
	e.do(nil, "POST", "/v1/items/"+a.ID+"/result", e.key, map[string]any{"status": "completed"}, 200, nil)
	e.wantConflict(nil, "POST", "/v1/items/"+a.ID+"/result", e.key, map[string]any{"status": "completed"}, "item is already completed")
	if n := e.eventCount("item_id=" + a.ID + "&type=item.completed"); n != 1 {
		t.Fatalf("item.completed events: %d", n)
	}
}

func TestNoopCollectionArchive(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	path := "/v1/collections/" + c.ID
	e.wantConflict(nil, "PATCH", path, e.key, map[string]any{"archived": false}, "collection is not archived")
	e.do(nil, "PATCH", path, e.key, map[string]any{"archived": true}, 200, nil)
	e.wantConflict(nil, "PATCH", path, e.key, map[string]any{"archived": true}, "collection is already archived")
	if n := e.eventCount("type=collection.archived"); n != 1 {
		t.Fatalf("collection.archived events: %d", n)
	}
	// With another change the unchanged flag is ignored.
	var got app.Collection
	e.do(nil, "PATCH", path, e.key, map[string]any{"archived": true, "description": "old"}, 200, &got)
	if !got.Archived || got.Description != "old" {
		t.Fatalf("combined: %+v", got)
	}
	e.do(nil, "PATCH", path, e.key, map[string]any{"archived": false}, 200, &got)
	if got.Archived {
		t.Fatal("unarchive failed")
	}
}

func TestNoopWatcherWebhookRuleTransitions(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	c := e.collection("Feed")
	feed := &feedServer{items: []string{rssItem("1", "Old", "x")}}
	srv := httptest.NewServer(feed)
	defer srv.Close()

	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", admin, map[string]any{"name": "w", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 201, &w)
	wp := "/v1/watchers/" + w.ID
	e.wantConflict(nil, "PATCH", wp, admin, map[string]any{"status": "active"}, "watcher is already active")
	e.do(nil, "PATCH", wp, admin, map[string]any{"status": "paused"}, 200, nil)
	e.wantConflict(nil, "PATCH", wp, admin, map[string]any{"status": "paused"}, "watcher is already paused")
	if n := e.eventCount("type=watcher.updated"); n != 1 {
		t.Fatalf("watcher.updated events: %d", n)
	}
	e.do(nil, "PATCH", wp, admin, map[string]any{"status": "active"}, 200, &w)
	if w.Status != "active" {
		t.Fatalf("resume: %s", w.Status)
	}

	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", admin, map[string]any{"url": "https://example.com/h"}, 201, &wh)
	hp := "/v1/webhooks/" + wh.ID
	e.wantConflict(nil, "PATCH", hp, admin, map[string]any{"active": true}, "webhook is already active")
	e.do(nil, "PATCH", hp, admin, map[string]any{"active": false}, 200, nil)
	e.wantConflict(nil, "PATCH", hp, admin, map[string]any{"active": false}, "webhook is already inactive")
	e.do(nil, "PATCH", hp, admin, map[string]any{"active": false, "description": "x"}, 200, nil) // other change: fine
	if n := e.eventCount("type=webhook.updated"); n != 2 {
		t.Fatalf("webhook.updated events: %d", n)
	}

}

func TestNoopChannelKeySessionPlanTransitions(t *testing.T) {
	e := newEnv(t)
	admin := e.keyWith(app.ScopeAdmin)
	ch := e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"topic": "t1"}}, 201)
	cp := "/v1/notification-channels/" + ch.ID
	e.wantConflict(nil, "PATCH", cp, admin, map[string]any{"enabled": true}, "notification channel is already enabled")
	e.wantConflict(nil, "PATCH", cp, admin, map[string]any{"enabled": true, "type": "ntfy"}, "notification channel is already enabled")
	e.do(nil, "PATCH", cp, admin, map[string]any{"enabled": false}, 200, nil)
	e.wantConflict(nil, "PATCH", cp, admin, map[string]any{"enabled": false}, "notification channel is already disabled")
	if n := e.eventCount("type=notification_channel.updated"); n != 1 {
		t.Fatalf("notification_channel.updated events: %d", n)
	}

	// API keys: revoking twice.
	var k app.APIKey
	e.do(nil, "POST", "/v1/api-keys", admin, map[string]any{"name": "tmp", "scopes": []string{"items:read"}}, 201, &k)
	e.do(nil, "DELETE", "/v1/api-keys/"+k.ID, admin, nil, 204, nil)
	e.wantConflict(nil, "DELETE", "/v1/api-keys/"+k.ID, admin, nil, "api key is already revoked")
	e.do(nil, "DELETE", "/v1/api-keys/key_missing", admin, nil, 404, nil)
	var revoked app.EventPage
	e.do(nil, "GET", "/v1/events?type=api_key.revoked", admin, nil, 200, &revoked)
	if len(revoked.Data) != 1 {
		t.Fatalf("api_key.revoked events: %d", len(revoked.Data))
	}

	// Sessions: recovering signs the first browser out; revoking it again is a conflict.
	var sessions struct {
		Data []app.Session `json:"data"`
	}
	e.do(e.browser, "GET", "/v1/sessions", "", nil, 200, &sessions)
	if len(sessions.Data) != 1 {
		t.Fatalf("sessions: %+v", sessions.Data)
	}
	first := sessions.Data[0].ID
	jar, _ := cookiejar.New(nil)
	b2 := &http.Client{Jar: jar}
	var rec struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(b2, "POST", "/v1/sessions/recover", "", map[string]string{"recovery_key": e.recovery}, 201, &rec)
	e.wantConflict(b2, "DELETE", "/v1/sessions/"+first, "", nil, "session is already revoked", "X-CSRF-Token", rec.CSRF)

	// Plans: choosing Free while on Free.
	e.free()
	e.wantConflict(nil, "POST", "/v1/workspace/plan", admin, map[string]any{"plan": "free"}, "workspace is already on the Free plan")
}
