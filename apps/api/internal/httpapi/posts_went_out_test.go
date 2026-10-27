package httpapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// A post that went out is history: deleting it (one, many, REST or MCP) is refused, purge still removes it.
func TestPostsThatWentOutCannotBeDeleted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("History")
	a, b := e.customChannel("A"), e.customChannel("B")
	deleter := e.keyWith("posts:read", "posts:delete")
	targets := func(chs ...app.Channel) []map[string]any {
		out := []map[string]any{}
		for _, ch := range chs {
			out = append(out, map[string]any{"channel_id": ch.ID})
		}
		return out
	}
	publish := func(post app.Post) {
		t.Helper()
		e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 202, nil)
		e.svc.RunDueJobsOnce(ctx)
	}
	report := func(post app.Post, ch app.Channel, status string) app.Post {
		t.Helper()
		var got app.Post
		e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": status}, 200, &got)
		return got
	}
	refused := func(id string) {
		t.Helper()
		var res struct {
			Error struct{ Code, Message string }
		}
		e.do(nil, "DELETE", "/v1/posts/"+id, deleter, nil, 409, &res)
		if res.Error.Code != "conflict" || !strings.Contains(res.Error.Message, "went out") {
			t.Fatalf("refusal: %+v", res.Error)
		}
		e.do(nil, "GET", "/v1/posts/"+id, e.key, nil, 200, nil)
	}

	// Published everywhere.
	published := e.post(c.ID, map[string]any{"targets": targets(a)})
	publish(published)
	if got := report(published, a, "published"); got.Status != "published" {
		t.Fatalf("published: %+v", got)
	}
	refused(published.ID)

	// Published on one channel, failed on the other: the post is failed, but part of it went out.
	partial := e.post(c.ID, map[string]any{"targets": targets(a, b)})
	publish(partial)
	report(partial, a, "published")
	if got := report(partial, b, "failed"); got.Status != "failed" {
		t.Fatalf("partially published: %+v", got)
	}
	refused(partial.ID)

	// Being published right now.
	publishing := e.post(c.ID, map[string]any{"targets": targets(a)})
	publish(publishing)
	refused(publishing.ID)

	// Failed everywhere: nothing went out, it can go.
	failed := e.post(c.ID, map[string]any{"targets": targets(a)})
	publish(failed)
	report(failed, a, "failed")
	e.do(nil, "DELETE", "/v1/posts/"+failed.ID, deleter, nil, 204, nil)

	// Many at once: drafts go, what went out is listed as kept; nothing fails.
	draft := e.post(c.ID, nil)
	var res struct{ Deleted, Missing, Kept []string }
	e.do(nil, "POST", "/v1/posts/delete", deleter, map[string]any{"ids": []string{draft.ID, published.ID, partial.ID, "pst_nope"}}, 200, &res)
	if len(res.Deleted) != 1 || res.Deleted[0] != draft.ID || len(res.Missing) != 1 || len(res.Kept) != 2 {
		t.Fatalf("bulk delete: %+v", res)
	}
	e.do(nil, "GET", "/v1/posts/"+published.ID, e.key, nil, 200, nil)

	// MCP calls the same service.
	if _, out := mcpCall(e, e.admin, "delete_post", map[string]any{"post_id": published.ID}); !isToolError(out) {
		t.Fatalf("delete_post over MCP: %v", out)
	}
	sc := mcpResult(e, e.admin, "delete_posts", map[string]any{"ids": []string{partial.ID}})
	if kept, _ := sc["kept"].([]any); len(kept) != 1 {
		t.Fatalf("delete_posts over MCP: %v", sc)
	}

	// Purge (admin) still removes it, history and all.
	e.do(nil, "POST", "/v1/posts/"+published.ID+"/purge", e.admin, nil, 200, nil)
	e.do(nil, "GET", "/v1/posts/"+published.ID, e.key, nil, 404, nil)
}
