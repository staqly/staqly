package httpapi_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The agent-side decision loop over MCP: wait (timeout), reject with a
// follow-up, report the outcome, archive/unarchive, and the scope and
// workspace boundaries of every step.
func TestMCPDecisionLifecycle(t *testing.T) {
	e := newEnv(t)
	writer := e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite) // no items:review
	c := e.collection("Posts")
	it := mcpResult(e, e.key, "create_item", map[string]any{"collection": "Posts", "title": "draft", "content": "hello", "tags": []string{"launch"}})
	id := it["id"].(string)

	start := time.Now()
	if got := mcpResult(e, e.key, "await_decision", map[string]any{"item_id": id, "timeout_seconds": 1}); got["decided"] != false {
		t.Fatalf("await_decision on a pending item: %v", got)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 5*time.Second {
		t.Fatalf("await_decision returned after %v", d)
	}

	// Scope and isolation: items:write can't decide, another workspace can't see the item.
	if _, out := mcpCall(e, writer, "approve_item", map[string]any{"item_id": id}); !isToolError(out) || !strings.Contains(fmt.Sprint(out), "items:review") {
		t.Fatalf("approve without items:review: %v", out)
	}
	other := newWorkspace(e, "Other")
	for _, tool := range []string{"get_item", "approve_item", "reject_item", "await_decision", "unarchive_item", "list_available_actions"} {
		if _, out := mcpCall(e, other.key, tool, map[string]any{"item_id": id, "feedback": "x", "timeout_seconds": 1}); !isToolError(out) {
			t.Fatalf("%s from another workspace: %v", tool, out)
		}
	}
	if got := e.itemState(id); got.Status != "pending" {
		t.Fatalf("item changed by refused calls: %+v", got)
	}

	acts := mcpResult(e, e.key, "list_available_actions", map[string]any{"item_id": id})
	if !strings.Contains(fmt.Sprint(acts["actions"]), "approve") {
		t.Fatalf("available actions on pending: %v", acts)
	}
	// Reject and ask for a regeneration in one call; the item's run is now processing.
	got := mcpResult(e, e.key, "reject_item", map[string]any{"item_id": id, "feedback": "too long", "follow_up": "regenerate"})
	if got["decision"] != "rejected" || got["run"] != "processing" {
		t.Fatalf("reject + regenerate: %v", got)
	}
	if _, out := mcpCall(e, e.key, "approve_item", map[string]any{"item_id": id}); !isToolError(out) {
		t.Fatalf("approve while processing: %v", out)
	}
	work := mcpResult(e, e.key, "list_requested_work", map[string]any{"action": "regenerate"})
	if len(work["data"].([]any)) != 1 {
		t.Fatalf("requested work: %v", work)
	}
	if got := mcpResult(e, e.key, "report_result", map[string]any{"item_id": id, "status": "completed", "result": map[string]any{"url": "https://example.com/p/1"}}); got["run"] != "completed" {
		t.Fatalf("report_result: %v", got)
	}
	if _, out := mcpCall(e, e.key, "report_result", map[string]any{"item_id": id, "status": "failed"}); !isToolError(out) {
		t.Fatalf("second report_result: %v", out)
	}
	if got := mcpResult(e, e.key, "await_decision", map[string]any{"item_id": id, "timeout_seconds": 1}); got["decided"] != true {
		t.Fatalf("await_decision on a decided item: %v", got)
	}

	// Archive, then unarchive brings the run back.
	mcpResult(e, e.key, "execute_custom_action", map[string]any{"item_id": id, "action": "archive"})
	if got := mcpResult(e, e.key, "unarchive_item", map[string]any{"item_id": id}); got["status"] == "archived" {
		t.Fatalf("unarchive: %v", got)
	}
	if _, out := mcpCall(e, e.key, "unarchive_item", map[string]any{"item_id": id}); !isToolError(out) {
		t.Fatalf("unarchive of a live item: %v", out)
	}
	tags := mcpResult(e, e.key, "list_tags", nil)
	if !strings.Contains(fmt.Sprint(tags["data"]), "launch") {
		t.Fatalf("list_tags: %v", tags)
	}
	if got := mcpResult(e, e.key, "get_collection", map[string]any{"collection": "Posts"}); got["id"] != c.ID {
		t.Fatalf("get_collection by name: %v", got)
	}
	if got := mcpResult(e, other.key, "list_items", nil); len(got["data"].([]any)) != 0 {
		t.Fatalf("other workspace lists our items: %v", got)
	}
}
