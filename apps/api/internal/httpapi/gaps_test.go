package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The event export follows the list rule: credential and session audit
// events only for admins (REST and MCP).
func TestEventExportHidesAuditEventsFromNonAdmins(t *testing.T) {
	e := newEnv(t)
	e.paid()
	e.collection("Visible")
	_, _, body := e.raw(nil, "GET", "/v1/events/export", e.key, nil)
	if strings.Contains(string(body), "api_key.") || !strings.Contains(string(body), "collection.created") {
		t.Fatalf("non-admin export:\n%s", body)
	}
	x := mcpResult(e, e.key, "export_events", nil)
	if strings.Contains(fmt.Sprint(x["content"]), "api_key.") {
		t.Fatalf("non-admin MCP export: %v", x)
	}
	_, _, body = e.raw(nil, "GET", "/v1/events/export", e.admin, nil)
	if !strings.Contains(string(body), "api_key.created") {
		t.Fatalf("admin export lacks audit events:\n%s", body)
	}
}

// approve_item and reject_item take expected_version like the REST actions:
// a stale version is a conflict and changes nothing.
func TestMCPDecisionExpectedVersion(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Drafts")
	it := e.item(c.ID, map[string]any{"title": "v1"})
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "v2"}, 200, nil)

	_, out := mcpCall(e, e.key, "approve_item", map[string]any{"item_id": it.ID, "expected_version": it.Version, "title": "edited"})
	if !isToolError(out) || !strings.Contains(fmt.Sprint(out), "version") {
		t.Fatalf("stale approve: %v", out)
	}
	if got := e.itemState(it.ID); got.Status != "pending" || got.Title != "v2" {
		t.Fatalf("stale approve changed the item: %+v", got)
	}
	_, out = mcpCall(e, e.key, "reject_item", map[string]any{"item_id": it.ID, "feedback": "no", "expected_version": it.Version})
	if !isToolError(out) {
		t.Fatalf("stale reject: %v", out)
	}
	got := mcpResult(e, e.key, "approve_item", map[string]any{"item_id": it.ID, "expected_version": it.Version + 1})
	if got["status"] != "approved" {
		t.Fatalf("approve with current version: %v", got)
	}
}

func TestWebhookEventPatternsValidated(t *testing.T) {
	e := newEnv(t)
	for _, ok := range [][]string{{"*"}, {"item.*"}, {"item.*_requested"}, {"item.approved", "item.rejected"},
		{"item.publish_requested", "item.regenerate_requested", "item.retry_requested", "item.archived"}, {"watcher.*", "workspace.plan_changed"}, {"webhook.test"}} {
		e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h", "events": ok}, 201, nil)
	}
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "item.*"
	}
	for _, bad := range [][]string{{"item.aproved"}, {"items.*"}, {"*.created"}, {"item."}, {"approved"}, {"item.[x"},
		{"item.shipped"}, {"item.upscale_requested"}, {"item.approved", "nope"}, {}, tooMany} {
		e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h", "events": bad}, 422, nil)
	}

	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h"}, 201, &wh)
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.key, map[string]any{"events": []string{"item.aprooved"}}, 422, nil)
	if _, out := mcpCall(e, e.key, "create_webhook", map[string]any{"url": "https://example.com/h", "events": []string{"itm.*"}}); !isToolError(out) {
		t.Fatalf("MCP create_webhook accepted an unknown pattern: %v", out)
	}

	// Notification channels share the rule.
	e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"topic": "t1"}, "events": []string{"item.creatd"}}, 422)
	e.channel(map[string]any{"name": "N", "type": "ntfy", "config": map[string]any{"topic": "t1"}, "events": []string{"item.created", "item.*_requested"}}, 201)

	// Credential and session audit events: only an admin may subscribe to them, by naming the family.
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h", "events": []string{"session.*"}}, 403, nil)
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.key, map[string]any{"events": []string{"api_key.revoked"}}, 403, nil)
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.admin, map[string]any{"events": []string{"session.*", "api_key.revoked"}}, 200, nil)
}

// Image and video items show a file: without media or url they are refused.
func TestImageAndVideoItemsNeedMedia(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Avatars")
	for _, typ := range []string{"image", "video"} {
		e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"type": typ, "title": "x"}, 422, nil)
	}
	if _, out := mcpCall(e, e.key, "create_item", map[string]any{"collection": "Avatars", "type": "image", "content": "caption"}); !isToolError(out) {
		t.Fatalf("MCP image without media: %v", out)
	}
	img := e.item(c.ID, map[string]any{"type": "image", "url": "https://cdn.example.com/a.png"})
	// Clearing the url leaves nothing to show; changing it is fine.
	e.do(nil, "PATCH", "/v1/items/"+img.ID, e.key, map[string]any{"url": ""}, 422, nil)
	e.do(nil, "PATCH", "/v1/items/"+img.ID, e.key, map[string]any{"url": "https://cdn.example.com/b.png"}, 200, nil)
	// Text items (and inferred types) are unaffected.
	if got := e.item(c.ID, map[string]any{"content": "hello"}); got.Type != "text" {
		t.Fatalf("inferred type %q", got.Type)
	}
	if got := e.item(c.ID, map[string]any{"url": "https://example.com"}); got.Type != "link" {
		t.Fatalf("inferred type %q", got.Type)
	}
}

// Long-poll waiters leave nothing behind: canceled, timed-out and decided
// waits all return their goroutines and timers.
func TestWaitDoesNotLeakGoroutines(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Waits")
	items := make([]app.Item, 6)
	for i := range items {
		items[i] = e.item(c.ID, map[string]any{"title": fmt.Sprint("w", i)})
	}
	tr := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: tr}
	before := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for i, it := range items {
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Odd items: the client gives up first; even items: the server-side timeout (or the approval) ends the wait.
				d := 300 * time.Millisecond
				if i%2 == 0 {
					d = 5 * time.Second
				}
				ctx, cancel := context.WithTimeout(context.Background(), d)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/v1/items/"+it.ID+"/wait?timeout=1", nil)
				req.Header.Set("Authorization", "Bearer "+e.key)
				if resp, err := client.Do(req); err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}()
		}
	}
	time.Sleep(100 * time.Millisecond)
	e.do(nil, "POST", "/v1/items/"+items[0].ID+"/actions/approve", e.key, nil, 200, nil)
	wg.Wait()
	tr.CloseIdleConnections()

	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines: %d before, %d after the waits\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
}

// upload_media returns an ID that create_item and update_item take as media_id.
func TestMCPItemFromUploadedMedia(t *testing.T) {
	e := newEnv(t)
	e.collection("Pics")
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	m := mcpResult(e, e.key, "upload_media", map[string]any{"media_base64": png, "filename": "dot.png"})
	it := mcpResult(e, e.key, "create_item", map[string]any{"collection": "Pics", "type": "image", "media_id": m["id"]})
	if it["type"] != "image" || it["media"] == nil {
		t.Fatalf("item from media_id: %v", it)
	}
	m2 := mcpResult(e, e.key, "upload_media", map[string]any{"media_base64": png, "filename": "dot2.png"})
	got := mcpResult(e, e.key, "update_item", map[string]any{"item_id": it["id"], "media_id": m2["id"]})
	if fmt.Sprint(got["media"]) == fmt.Sprint(it["media"]) {
		t.Fatalf("media not replaced: %v", got["media"])
	}
}
