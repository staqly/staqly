package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
)

func runWorker(e *env) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunWorker(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func TestWaitForDecisionLongPoll(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Gate")
	it := e.item(c.ID, map[string]any{"title": "deploy prod?"})

	// Times out while pending.
	var d app.Decision
	start := time.Now()
	e.do(nil, "GET", "/v1/items/"+it.ID+"/wait?timeout=1", e.key, nil, 200, &d)
	if d.Decided || d.Item.Status != "pending" || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("timeout: %+v after %s", d, time.Since(start))
	}
	// Returns as soon as a human decides.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Millisecond)
		e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"feedback": "go"}, 200, nil)
	}()
	start = time.Now()
	e.do(nil, "GET", "/v1/items/"+it.ID+"/wait?timeout=30", e.key, nil, 200, &d)
	wg.Wait()
	// Well under the 30s timeout: the wait returned because of the decision.
	if !d.Decided || d.Item.Status != "approved" || time.Since(start) > 20*time.Second {
		t.Fatalf("decision: %+v after %s", d, time.Since(start))
	}
	e.do(nil, "GET", "/v1/items/itm_nope/wait?timeout=1", e.key, nil, 404, nil)

	// Report the outcome.
	var done app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/result", e.key, map[string]any{"status": "completed", "result": map[string]any{"deploy_id": 42}}, 200, &done)
	var meta map[string]any
	json.Unmarshal(done.Metadata, &meta)
	if done.Status != "completed" || meta["result"].(map[string]any)["deploy_id"] != float64(42) {
		t.Fatalf("result: %+v %v", done, meta)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/result", e.key, map[string]any{"status": "failed"}, 409, nil) // already completed
	pending := e.item(c.ID, map[string]any{"title": "not approved"})
	e.do(nil, "POST", "/v1/items/"+pending.ID+"/result", e.key, map[string]any{"status": "completed"}, 409, nil)
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	if evs.Data[0].Type != "item.completed" {
		t.Fatalf("event: %s", evs.Data[0].Type)
	}
}

func TestExpiresInAndExpiredStatus(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Expiry")
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "expires_in": 5}, 422, nil)
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "expires_in": "soon"}, 422, nil)
	a := e.item(c.ID, map[string]any{"title": "seconds", "expires_in": 120})
	b := e.item(c.ID, map[string]any{"title": "duration", "expires_in": "1h"})
	if a.ExpiresAt == nil || b.ExpiresAt == nil || !(*a.ExpiresAt < *b.ExpiresAt) {
		t.Fatalf("expires_at: %v %v", a.ExpiresAt, b.ExpiresAt)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	stop := runWorker(e)
	defer stop()
	waitFor(t, func() bool {
		var it app.Item
		e.do(nil, "GET", "/v1/items/"+a.ID, e.key, nil, 200, &it)
		return it.Status == "expired"
	})
	var it app.Item
	e.do(nil, "GET", "/v1/items/"+b.ID, e.key, nil, 200, &it)
	if it.Status != "pending" {
		t.Fatalf("1h item expired too early: %s", it.Status)
	}
	var d app.Decision
	e.do(nil, "GET", "/v1/items/"+a.ID+"/wait?timeout=1", e.key, nil, 200, &d)
	if !d.Decided || d.Item.Status != "expired" {
		t.Fatalf("wait on expired: %+v", d)
	}
}

func TestEditableWhitelistAndDiff(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "editable": []string{"owner"}}, 422, nil)
	it := e.item(c.ID, map[string]any{"title": "Post", "content": "caption v1", "metadata": map[string]any{"caption": "c1", "seed": 7},
		"editable": []string{"content", "metadata.caption"}})
	if strings.Join(it.Editable, ",") != "content,metadata.caption" {
		t.Fatalf("editable: %v", it.Editable)
	}
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{"title": "Hacked"}}, 422, &errBody)
	if errBody.Error.Code != "not_editable" {
		t.Fatalf("whitelist: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{"metadata_patch": map[string]any{"seed": 1}}}, 422, nil)
	var out app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{
		"content": "caption v2", "metadata_patch": map[string]any{"caption": "c2"}}}, 200, &out)
	if out.Content != "caption v2" || out.Status != "approved" {
		t.Fatalf("approve with edits: %+v", out)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	var data struct {
		Edited bool
		Diff   map[string]struct{ Before, After any }
	}
	json.Unmarshal(evs.Data[0].Data, &data)
	if !data.Edited || data.Diff["content"].Before != "caption v1" || data.Diff["content"].After != "caption v2" ||
		data.Diff["metadata.caption"].After != "c2" || len(data.Diff) != 2 {
		t.Fatalf("diff: %+v", data)
	}
}

// An item left processing longer than PROCESSING_TIMEOUT (default 1h) fails.
func TestProcessingTimesOut(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Publishing")
	it := e.item(c.ID, map[string]any{"title": "Post"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	var processing app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/publish", e.key, nil, 200, &processing)
	if processing.Status != "processing" {
		t.Fatalf("publish: %s", processing.Status)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(61 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	stop := runWorker(e)
	defer stop()
	var got app.Item
	waitFor(t, func() bool {
		e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
		return got.Status == "failed"
	})
	if !strings.Contains(string(got.Metadata), "No result reported within 1h") {
		t.Fatalf("metadata: %s", got.Metadata)
	}
	var page app.EventPage
	e.do(nil, "GET", "/v1/events?item_id="+it.ID+"&type=item.failed", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].Actor.Name != "Processing timeout" {
		t.Fatalf("event: %+v", page.Data)
	}
}

// A workspace sets its own processing timeout (1 minute to 24 hours); items fail after it.
func TestWorkspaceProcessingTimeout(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if ws.ProcessingTimeoutSeconds != 3600 {
		t.Fatalf("default: %d", ws.ProcessingTimeoutSeconds)
	}
	admin := e.keyWith(app.ScopeAdmin)
	e.do(nil, "PATCH", "/v1/workspace", e.key, map[string]any{"processing_timeout_seconds": 7200}, 403, nil)
	e.do(nil, "PATCH", "/v1/workspace", admin, map[string]any{"processing_timeout_seconds": 30}, 422, nil)
	e.do(nil, "PATCH", "/v1/workspace", admin, map[string]any{"processing_timeout_seconds": 86401}, 422, nil)
	e.do(nil, "PATCH", "/v1/workspace", admin, map[string]any{}, 422, nil)
	e.do(nil, "PATCH", "/v1/workspace", admin, map[string]any{"processing_timeout_seconds": 7200}, 200, &ws)
	if ws.ProcessingTimeoutSeconds != 7200 || ws.Name == "" {
		t.Fatalf("set: %+v", ws)
	}

	c := e.collection("Publishing")
	it := e.item(c.ID, map[string]any{"title": "Post"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/publish", e.key, nil, 200, nil)
	// 61 minutes is past the server default but within this workspace's 2h: still processing.
	e.svc.Now = func() time.Time { return time.Now().Add(61 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	stop := runWorker(e)
	time.Sleep(300 * time.Millisecond)
	var got app.Item
	e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
	if got.Status != "processing" {
		t.Fatalf("failed too early: %s", got.Status)
	}
	stop()
	e.svc.Now = func() time.Time { return time.Now().Add(121 * time.Minute) }
	stop = runWorker(e)
	defer stop()
	waitFor(t, func() bool {
		e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
		return got.Status == "failed"
	})
	if !strings.Contains(string(got.Metadata), "No result reported within 2h") {
		t.Fatalf("metadata: %s", got.Metadata)
	}
}

// Content is locked once approved (metadata still changes); fixing a rejected item sends it back to review.
func TestContentLockAndResubmit(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	ok := e.item(c.ID, map[string]any{"title": "Caption"})
	e.do(nil, "POST", "/v1/items/"+ok.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+ok.ID, e.key, map[string]any{"title": "Changed"}, 409, nil)
	e.do(nil, "PATCH", "/v1/items/"+ok.ID, e.key, map[string]any{"metadata_patch": map[string]any{"url": "https://x"}}, 200, nil)

	bad := e.item(c.ID, map[string]any{"title": "Too dark"})
	e.do(nil, "POST", "/v1/items/"+bad.ID+"/actions/reject", e.key, map[string]any{"feedback": "darker"}, 200, nil)
	var got app.Item
	e.do(nil, "PATCH", "/v1/items/"+bad.ID, e.key, map[string]any{"title": "Brighter"}, 200, &got)
	if got.Status != "pending" || got.Title != "Brighter" {
		t.Fatalf("resubmit: %+v", got)
	}
}

// An item can have its own processing limit; it doesn't add a version or restart the clock.
func TestItemProcessingTimeout(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	it := e.item(c.ID, map[string]any{"title": "Post"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	var processing app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/publish", e.key, nil, 200, &processing)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"processing_timeout_seconds": 10}, 422, nil)
	var got app.Item
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"processing_timeout_seconds": 3 * 3600}, 200, &got)
	if got.ProcessingTimeoutSeconds == nil || *got.ProcessingTimeoutSeconds != 3*3600 || got.Version != processing.Version || got.UpdatedAt != processing.UpdatedAt {
		t.Fatalf("item: %+v", got)
	}
	// Past the workspace's 1h but within the item's 3h: still processing; after 3h it fails.
	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	defer func() { e.svc.Now = time.Now }()
	stop := runWorker(e)
	time.Sleep(300 * time.Millisecond)
	stop()
	e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
	if got.Status != "processing" {
		t.Fatalf("failed too early: %s", got.Status)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(3*time.Hour + time.Minute) }
	stop = runWorker(e)
	defer stop()
	waitFor(t, func() bool {
		e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
		return got.Status == "failed"
	})
}

// decision and run are separate: a run keeps the decision it followed, archiving keeps both, filters combine.
func TestDecisionAndRun(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	it := e.item(c.ID, map[string]any{"title": "Post"})
	other := e.item(c.ID, map[string]any{"title": "Other"})
	var got app.Item
	e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
	if got.Decision == nil || *got.Decision != "pending" || got.Run != "none" {
		t.Fatalf("new: %+v %s", got.Decision, got.Run)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/publish", e.key, nil, 200, &got)
	if *got.Decision != "approved" || got.Run != "processing" || got.Status != "processing" {
		t.Fatalf("publish: %s %s %s", *got.Decision, got.Run, got.Status)
	}
	var page app.ItemPage
	e.do(nil, "GET", "/v1/items?decision=approved&run=processing,failed", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != it.ID {
		t.Fatalf("filter: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?decision=pending", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != other.ID {
		t.Fatalf("pending: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?decision=nope", e.key, nil, 422, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/result", e.key, map[string]any{"status": "completed"}, 200, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/archive", e.key, nil, 200, &got)
	if *got.Decision != "approved" || got.Run != "completed" || got.ArchivedAt == nil {
		t.Fatalf("archived: %s %s %v", *got.Decision, got.Run, got.ArchivedAt)
	}
}

// An action that starts a run names it (run_labels) and the item remembers which action did (run_action).
func TestRunLabels(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	var col app.Collection
	e.do(nil, "GET", "/v1/collections/"+c.ID, e.key, nil, 200, &col)
	var publish app.ActionDef
	for _, a := range col.Actions {
		if a.Name == "publish" {
			publish = a
		}
	}
	if publish.RunLabels == nil || publish.RunLabels.Running != "Publishing" || publish.Event != "item.publish_requested" {
		t.Fatalf("default publish: %+v", publish)
	}
	it := e.item(c.ID, map[string]any{"title": "Post"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	var got app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/publish", e.key, nil, 200, &got)
	if got.RunAction == nil || *got.RunAction != "publish" || got.Run != "processing" {
		t.Fatalf("run: %+v %s", got.RunAction, got.Run)
	}
	// Labels only stick to an action that starts a run.
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "ping", "run_labels": map[string]any{"running": "Pinging"}},
		{"name": "send", "set_status": "processing", "run_labels": map[string]any{"running": "Sending", "done": "Sent"}}}}, 200, nil)
	var fresh app.Collection
	e.do(nil, "GET", "/v1/collections/"+c.ID, e.key, nil, 200, &fresh)
	if fresh.Actions[0].RunLabels != nil || fresh.Actions[1].RunLabels == nil || fresh.Actions[1].RunLabels.Done != "Sent" {
		t.Fatalf("labels: %+v", fresh.Actions)
	}
}

// Tags: set on create, replaced on update in any status, filtered with ?tag=, listed by use.
func TestItemTags(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	a := e.item(c.ID, map[string]any{"title": "A", "tags": []string{" client-a ", "urgent", "Urgent", ""}})
	if len(a.Tags) != 2 || a.Tags[0] != "client-a" || a.Tags[1] != "urgent" {
		t.Fatalf("tags: %v", a.Tags)
	}
	b := e.item(c.ID, map[string]any{"title": "B", "tags": []string{"client-b"}})
	var approved app.Item
	e.do(nil, "POST", "/v1/items/"+b.ID+"/actions/approve", e.key, nil, 200, &approved)
	var got app.Item
	e.do(nil, "PATCH", "/v1/items/"+b.ID, e.key, map[string]any{"tags": []string{"client-b", "published"}}, 200, &got)
	if len(got.Tags) != 2 || got.Version != approved.Version {
		t.Fatalf("approved items take tags, without a new version: %v v%d", got.Tags, got.Version)
	}
	e.do(nil, "PATCH", "/v1/items/"+b.ID, e.key, map[string]any{"tags": []string{"a,b"}}, 422, nil)
	// Limits count characters (as the dashboard's maxLength does), not UTF-8 bytes.
	e.do(nil, "PATCH", "/v1/items/"+b.ID, e.key, map[string]any{"tags": []string{"client-b", "published", strings.Repeat("ż", 40)}}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+b.ID, e.key, map[string]any{"tags": []string{strings.Repeat("ż", 41)}}, 422, nil)
	e.item(c.ID, map[string]any{"title": strings.Repeat("ł", 500)})
	e.do(nil, "PATCH", "/v1/items/"+b.ID, e.key, map[string]any{"tags": []string{"client-b", "published"}}, 200, nil)
	var page app.ItemPage
	e.do(nil, "GET", "/v1/items?tag=urgent,published", e.key, nil, 200, &page)
	if len(page.Data) != 2 {
		t.Fatalf("any of: %d", len(page.Data))
	}
	e.do(nil, "GET", "/v1/items?tag=client-a", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != a.ID {
		t.Fatalf("one tag: %+v", page.Data)
	}
	var tags struct{ Data []app.Tag }
	e.do(nil, "GET", "/v1/tags", e.key, nil, 200, &tags)
	if len(tags.Data) != 4 {
		t.Fatalf("tags: %+v", tags.Data)
	}
}

// Archived leaves every decision view (the Pending queue included) and has its own: decision=archived.
func TestDecisionFilterArchived(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	a := e.item(c.ID, map[string]any{"title": "A"})
	b := e.item(c.ID, map[string]any{"title": "B"})
	e.do(nil, "PATCH", "/v1/items/"+b.ID, e.key, map[string]any{"status": "archived"}, 200, nil)
	var page app.ItemPage
	e.do(nil, "GET", "/v1/items?decision=pending", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != a.ID {
		t.Fatalf("pending: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?decision=archived", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != b.ID {
		t.Fatalf("archived: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?decision=pending,archived", e.key, nil, 200, &page)
	if len(page.Data) != 2 {
		t.Fatalf("both: %d", len(page.Data))
	}
	e.do(nil, "GET", "/v1/items?archived=false", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != a.ID {
		t.Fatalf("archived=false: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?archived=true", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != b.ID {
		t.Fatalf("archived=true: %+v", page.Data)
	}
	// Unarchive brings it back where it was before archiving: waiting for review (archiving it recorded a
	// rejection, which unarchiving undoes).
	var back app.Item
	e.do(nil, "POST", "/v1/items/"+b.ID+"/unarchive", e.key, nil, 200, &back)
	if back.ArchivedAt != nil || back.Decision == nil || *back.Decision != "pending" || back.Status != "pending" || back.DecidedAt != nil {
		t.Fatalf("unarchived: %+v", back)
	}
	e.do(nil, "POST", "/v1/items/"+b.ID+"/unarchive", e.key, nil, 409, nil)
}

// A PATCH that loses on expected_version changes nothing: tags and the processing limit sent along with it
// are not applied (nor announced) on their own.
func TestUpdateItemVersionConflictAppliesNothing(t *testing.T) {
	e := newEnv(t)
	it := e.item(e.collection("Posts").ID, map[string]any{"title": "A", "tags": []string{"old"}})
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "B"}, 200, nil) // version 2
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"title": "C", "tags": []string{"new"},
		"processing_timeout_seconds": 120, "expected_version": 1}, 409, nil)
	var got app.Item
	e.do(nil, "GET", "/v1/items/"+it.ID, e.key, nil, 200, &got)
	if got.Title != "B" || !slices.Equal(got.Tags, []string{"old"}) || got.ProcessingTimeoutSeconds != nil {
		t.Fatalf("partly applied: %q %v %v", got.Title, got.Tags, got.ProcessingTimeoutSeconds)
	}
	if n := e.eventCount("type=item.tags_changed"); n != 0 {
		t.Fatalf("%d item.tags_changed events", n)
	}
	// Labels alone still need no version and change nothing else.
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"tags": []string{"new"}, "expected_version": 1}, 200, &got)
	if !slices.Equal(got.Tags, []string{"new"}) || got.Version != 2 {
		t.Fatalf("tags only: %v v%d", got.Tags, got.Version)
	}
}

// Changes made while an item is archived (metadata, another collection) add versions of the archived item;
// unarchiving still finds the state from before the archive (pending), so a writer can bring it back.
func TestUnarchivePendingAfterEditsWhileArchived(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	other := e.collection("Other")
	it := e.item(c.ID, map[string]any{"title": "A"})
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"status": "archived"}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"metadata_patch": map[string]any{"note": "x"}}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"collection_id": other.ID}, 200, nil)
	writer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite)
	var back app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/unarchive", writer, nil, 200, &back)
	if back.Status != "pending" || back.Decision == nil || *back.Decision != "pending" || back.DecidedAt != nil {
		t.Fatalf("unarchived: %s %v %v", back.Status, back.Decision, back.DecidedAt)
	}
}

// The flow view: items per state and events per type in the last 24 hours; run_action filters a run's items.
func TestFlow(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	a := e.item(c.ID, map[string]any{"title": "A"})
	e.item(c.ID, map[string]any{"title": "B"})
	e.do(nil, "POST", "/v1/items/"+a.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "POST", "/v1/items/"+a.ID+"/actions/publish", e.key, nil, 200, nil)
	var f app.Flow
	e.do(nil, "GET", "/v1/flow?collection_id="+c.ID, e.key, nil, 200, &f)
	got := map[string]int64{}
	for _, s := range f.States {
		got[s.Decision+"/"+s.Run+"/"+s.RunAction] = s.Items
	}
	if len(f.Sources) != 1 || f.Sources[0].Items != 2 {
		t.Fatalf("sources: %+v", f.Sources)
	}
	if got["pending/none/"] != 1 || got["approved/processing/publish"] != 1 || f.Events["item.approved"] != 1 || f.Events["item.created"] != 2 {
		t.Fatalf("flow: %v %v", got, f.Events)
	}
	var page app.ItemPage
	e.do(nil, "GET", "/v1/items?run_action=publish", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != a.ID {
		t.Fatalf("run_action: %+v", page.Data)
	}
	e.do(nil, "GET", "/v1/items?run_action=none", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID == a.ID {
		t.Fatalf("run_action=none: %+v", page.Data)
	}
}

// Agents: REST clients by User-Agent (the agent named after an SDK), MCP clients by clientInfo; items they add
// are counted; browsers are not agents.
func TestAgentsSeen(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	ua := []string{"User-Agent", "staqly-sdk-python/0.3.0 my-agent/1.2"}
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "A", "content": "x"}, 201, nil, ua...)
	e.do(nil, "GET", "/v1/items", e.key, nil, 200, nil, "User-Agent", "Mozilla/5.0 (Macintosh)")

	ctx := context.Background()
	p, err := e.svc.AuthenticateAPIKey(ctx, e.key)
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	go mcpserver.New(e.svc, p, "test").Run(ctx, st)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "2.1.0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_item", Arguments: map[string]any{"collection": c.ID, "title": "B", "content": "y"}})
	if err != nil || res.IsError {
		t.Fatalf("create_item: %v %+v", err, res)
	}
	if src := res.StructuredContent.(map[string]any)["source"]; src != "claude-code" {
		t.Fatalf("an MCP item without a source is named after the client: %v", src)
	}
	// Stateless HTTP calls without clientInfo (Codex) are named by their User-Agent.
	e.do(nil, "POST", "/mcp", e.key, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "list_collections", "arguments": map[string]any{}}}, 200, nil,
		"Accept", "application/json, text/event-stream", "User-Agent", "codex-mcp-client/0.162.1")

	var out struct{ Data []app.Agent }
	e.do(nil, "GET", "/v1/agents", e.key, nil, 403, nil) // names API keys: admin only
	e.do(nil, "GET", "/v1/agents", e.keyWith(app.ScopeAdmin), nil, 200, &out)
	got := map[string]app.Agent{}
	for _, a := range out.Data {
		got[a.Kind+":"+a.Name] = a
	}
	if a := got["api:my-agent"]; a.ItemsCreated != 1 || a.Version != "1.2 · staqly-sdk-python 0.3.0" {
		t.Fatalf("rest agent: %+v (all %+v)", a, out.Data)
	}
	if a := got["mcp:claude-code"]; a.ItemsCreated != 1 || a.Version != "2.1.0" {
		t.Fatalf("mcp agent: %+v", a)
	}
	if a := got["mcp:codex-mcp-client"]; a.Version != "0.162.1" {
		t.Fatalf("mcp agent by User-Agent: %+v (all %+v)", a, out.Data)
	}
	if _, ok := got["api:Mozilla"]; ok {
		t.Fatal("a browser was recorded as an agent")
	}
	e.do(nil, "DELETE", "/v1/agents/"+got["mcp:claude-code"].ID, e.keyWith(app.ScopeAdmin), nil, 204, nil)

	// Retention forgets connections that went quiet: 31 days later none is left.
	real := e.svc.Now
	e.svc.Now = func() time.Time { return real().Add(31 * 24 * time.Hour) }
	rep, err := e.svc.RunRetention(context.Background())
	e.svc.Now = real
	if err != nil || rep.Agents == 0 {
		t.Fatalf("retention: %+v %v", rep, err)
	}
	var left []app.Agent
	e.do(nil, "GET", "/v1/agents", e.keyWith(app.ScopeAdmin), nil, 200, &struct{ Data *[]app.Agent }{&left})
	if len(left) > 1 { // only the request just made can be listed again
		t.Fatalf("agents after retention: %+v", left)
	}

	// Events name the agent that caused them, and can be listed per agent.
	agentsOf := func(query string) []string {
		var page app.EventPage
		e.do(nil, "GET", "/v1/events?type=item.created"+query, e.key, nil, 200, &page)
		out := []string{}
		for _, ev := range page.Data {
			out = append(out, ev.Actor.Agent)
		}
		return out
	}
	if got := agentsOf(""); !slices.Equal(got, []string{"claude-code", "my-agent"}) {
		t.Fatalf("event agents: %v", got)
	}
	if got := agentsOf("&agent=my-agent"); !slices.Equal(got, []string{"my-agent"}) {
		t.Fatalf("one agent: %v", got)
	}
	if got := agentsOf("&agent=my-agent&agent=claude-code"); len(got) != 2 {
		t.Fatalf("either agent: %v", got)
	}
	if got := agentsOf("&agent=codex"); len(got) != 0 {
		t.Fatalf("other agent: %v", got)
	}
}

// MCP unarchive_item: an agent brings an archived item back.
func TestMCPUnarchive(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	it := e.item(c.ID, map[string]any{"title": "A"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"status": "archived"}, 200, nil)
	ctx := context.Background()
	p, err := e.svc.AuthenticateAPIKey(ctx, e.key)
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	go mcpserver.New(e.svc, p, "test").Run(ctx, st)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "unarchive_item", Arguments: map[string]any{"item_id": it.ID}})
	if err != nil || res.IsError {
		t.Fatalf("unarchive_item: %v %+v", err, res)
	}
	if got := e.itemState(it.ID); got.Status != "approved" || got.ArchivedAt != nil {
		t.Fatalf("after unarchive: %s %v", got.Status, got.ArchivedAt)
	}
}

// Security: unarchiving back to a decision needs items:review (metadata may have changed while archived) and
// uses no decision quota; filter lists are capped; the agent list per workspace is bounded.
func TestUnarchiveAndLimits(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	it := e.item(c.ID, map[string]any{"title": "A"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"status": "archived"}, 200, nil)
	writer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/unarchive", writer, nil, 403, nil)
	e.do(nil, "POST", "/v1/items/"+it.ID+"/unarchive", e.key, nil, 200, nil)

	many := strings.TrimSuffix(strings.Repeat("t,", 51), ",")
	e.do(nil, "GET", "/v1/items?tag="+many, e.key, nil, 422, nil)

	admin := e.keyWith(app.ScopeAdmin)
	p, err := e.svc.AuthenticateAPIKey(context.Background(), e.key)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 205 {
		e.svc.SeenAgent(context.Background(), p, "api", fmt.Sprintf("bot-%d", i), "1", false)
	}
	var out struct{ Data []app.Agent }
	e.do(nil, "GET", "/v1/agents", admin, nil, 200, &out)
	if len(out.Data) != 200 {
		t.Fatalf("agents: %d, want the cap of 200", len(out.Data))
	}
}

// Lifecycle rules found in review: each would otherwise leave an item in a state that makes no sense.
func TestLifecycleRules(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	writer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite)

	// An action that would only start runs from pending can't be saved (it would run from anywhere).
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"actions": []map[string]any{
		{"name": "go", "label": "Go", "event": "item.go_requested", "set_status": "processing", "allowed_statuses": []string{"pending"}}}}, 422, nil)

	// A new item can't start archived (or in a run).
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "content": "x", "status": "archived"}, 422, nil)

	// Approved content stays frozen while it is being published.
	pub := e.item(c.ID, map[string]any{"title": "pub"})
	e.do(nil, "POST", "/v1/items/"+pub.ID+"/actions/approve", e.key, nil, 200, nil)
	e.do(nil, "POST", "/v1/items/"+pub.ID+"/actions/publish", e.key, nil, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+pub.ID, writer, map[string]any{"content": "evil"}, 409, nil)
	// Archived mid-run: the run ends failed, so unarchive doesn't restart a run nobody reports.
	e.do(nil, "PATCH", "/v1/items/"+pub.ID, writer, map[string]any{"status": "archived"}, 200, nil)
	if got := e.itemState(pub.ID); got.Run != "failed" {
		t.Fatalf("archived mid-run: run %s", got.Run)
	}

	// A bad follow_up leaves the item untouched (no half-done reject).
	fu := e.item(c.ID, map[string]any{"title": "fu"})
	e.do(nil, "POST", "/v1/items/"+fu.ID+"/actions/reject", e.key, map[string]any{"feedback": "x", "follow_up": "nope"}, 404, nil)
	e.do(nil, "POST", "/v1/items/"+fu.ID+"/actions/reject", e.key, map[string]any{"follow_up": "regenerate"}, 422, nil)
	if got := e.itemState(fu.ID); got.Status != "pending" {
		t.Fatalf("after a bad follow_up: %s", got.Status)
	}

	// Regenerated in place: the agent sends the rejected item back to review.
	e.do(nil, "POST", "/v1/items/"+fu.ID+"/actions/reject", e.key, map[string]any{"feedback": "redo", "follow_up": "regenerate"}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+fu.ID, writer, map[string]any{"content": "better", "status": "pending"}, 200, nil)
	if got := e.itemState(fu.ID); got.Status != "pending" || got.Decision == nil || *got.Decision != "pending" {
		t.Fatalf("regenerated: %s %v", got.Status, got.Decision)
	}

}

// Coverage for recent features: tags_changed only on a real change, bulk reject with follow_up, and the MCP
// tools that were only counted so far.
func TestTagsEventBulkFollowUpAndMCPTools(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	it := e.item(c.ID, map[string]any{"title": "A"})
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"tags": []string{"x"}}, 200, nil)
	e.do(nil, "PATCH", "/v1/items/"+it.ID, e.key, map[string]any{"tags": []string{"x"}}, 200, nil)
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	n := 0
	for _, ev := range evs.Data {
		if ev.Type == "item.tags_changed" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("tags_changed events: %d, want 1 (the second PATCH changed nothing)", n)
	}

	b := e.item(c.ID, map[string]any{"title": "B"})
	var bulk struct{ Data []app.BulkResult }
	e.do(nil, "POST", "/v1/items/bulk-actions", e.key, map[string]any{"ids": []string{it.ID, b.ID}, "action": "reject",
		"feedback": "redo", "follow_up": "regenerate"}, 200, &bulk)
	for _, r := range bulk.Data {
		if !r.OK || r.Item == nil || r.Item.Run != "processing" {
			t.Fatalf("bulk reject + regenerate: %+v", r)
		}
	}

	ctx := context.Background()
	p, err := e.svc.AuthenticateAPIKey(ctx, e.key)
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	go mcpserver.New(e.svc, p, "test").Run(ctx, st)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) string {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		return string(raw)
	}
	if out := call("list_tags", map[string]any{}); !strings.Contains(out, `"x"`) {
		t.Fatalf("list_tags: %s", out)
	}
	if out := call("get_flow", map[string]any{"collection": c.ID}); !strings.Contains(out, `"states"`) {
		t.Fatalf("get_flow: %s", out)
	}
	if out := call("list_requested_work", map[string]any{}); !strings.Contains(out, it.ID) {
		t.Fatalf("list_requested_work: %s", out)
	}
	d := e.item(c.ID, map[string]any{"title": "D"})
	call("reject_item", map[string]any{"item_id": d.ID, "feedback": "redo", "follow_up": "regenerate"})
	if got := e.itemState(d.ID); got.Run != "processing" {
		t.Fatalf("MCP reject + follow_up: run %s", got.Run)
	}
}
