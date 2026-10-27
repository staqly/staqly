package httpapi_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Deleting a collection keeps its posts: every one moves to Default as it is, published ones included, and a
// scheduled one still goes out. The collection's schedules go with it.
func TestDeleteCollectionMovesPostsToDefault(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var cols struct{ Data []app.Collection }
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &cols)
	def := cols.Data[0]
	c := e.collection("Campaign")
	ch := e.customChannel("A")
	targets := []map[string]any{{"channel_id": ch.ID}}

	draft := e.post(c.ID, nil)
	scheduled := e.post(c.ID, map[string]any{"targets": targets})
	e.do(nil, "POST", "/v1/posts/"+scheduled.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	published := e.post(c.ID, map[string]any{"targets": targets})
	e.do(nil, "POST", "/v1/posts/"+published.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+published.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "published"}, 200, nil)
	failed := e.post(c.ID, map[string]any{"targets": targets})
	e.do(nil, "POST", "/v1/posts/"+failed.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+failed.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "failed"}, 200, nil)
	var sc app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Daily", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"08:00"}}}, 201, &sc)

	before := map[string]app.Post{}
	for _, p := range []app.Post{draft, scheduled, published, failed} {
		var got app.Post
		e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
		before[p.ID] = got
	}

	e.do(nil, "DELETE", "/v1/collections/"+c.ID+"?confirm=Campaign", e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/collections/"+c.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/schedules/"+sc.ID, e.key, nil, 404, nil)

	for id, was := range before {
		var got app.Post
		e.do(nil, "GET", "/v1/posts/"+id, e.key, nil, 200, &got)
		if got.CollectionID != def.ID || got.CollectionName != def.Name {
			t.Fatalf("post %s not moved to Default: %+v", id, got)
		}
		if got.Status != was.Status || got.Version != was.Version+1 || len(got.Targets) != len(was.Targets) {
			t.Fatalf("post %s changed beyond its collection: was %+v, now %+v", id, was, got)
		}
		if was.Status == "published" && (got.PublishedAt == nil || got.Targets[0].Status != "published") {
			t.Fatalf("published post lost its history: %+v", got)
		}
	}

	// One post.updated per moved post (with where it came from) and the collection's deletion with the count.
	var moved int
	var data []byte
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type = 'post.updated' AND collection_id = ? AND data LIKE '%moved_from%'`, def.ID).Scan(&moved)
	e.svc.DB.QueryRow(`SELECT data FROM events WHERE type = 'collection.deleted' AND collection_id = ?`, c.ID).Scan(&data)
	var deleted struct {
		MovedPosts int                 `json:"moved_posts"`
		MovedTo    struct{ ID string } `json:"moved_to"`
	}
	_ = json.Unmarshal(data, &deleted)
	if moved != 4 || deleted.MovedPosts != 4 || deleted.MovedTo.ID != def.ID {
		t.Fatalf("events: %d post.updated, collection.deleted %s", moved, data)
	}

	// The scheduled post still goes out, now from Default.
	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	defer func() { e.svc.Now = time.Now }()
	e.svc.RunDueJobsOnce(ctx)
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+scheduled.ID, e.key, nil, 200, &got)
	if got.Status != "publishing" {
		t.Fatalf("moved scheduled post did not go out: %s", got.Status)
	}
}

// hide_past leaves out posts that went out (published, failed after their time); drafts, scheduled and
// publishing posts stay, and pages stay whole.
func TestListPostsHidePast(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Mixed")
	ch := e.customChannel("A")
	targets := []map[string]any{{"channel_id": ch.ID}}
	goOut := func(status string) app.Post {
		t.Helper()
		p := e.post(c.ID, map[string]any{"targets": targets})
		e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
		e.svc.RunDueJobsOnce(ctx)
		if status != "" {
			e.do(nil, "POST", "/v1/posts/"+p.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": status}, 200, nil)
		}
		return p
	}
	draft := e.post(c.ID, nil)
	scheduled := e.post(c.ID, map[string]any{"targets": targets})
	e.do(nil, "POST", "/v1/posts/"+scheduled.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	publishing := goOut("")
	goOut("published")
	goOut("failed")

	ids := func(path string) map[string]bool {
		t.Helper()
		out := map[string]bool{}
		cursor := ""
		for {
			var page app.PostPage
			url := path + "&limit=1"
			if cursor != "" {
				url += "&cursor=" + cursor
			}
			e.do(nil, "GET", url, e.key, nil, 200, &page)
			for _, p := range page.Data {
				out[p.ID] = true
			}
			if page.NextCursor == "" {
				return out
			}
			cursor = page.NextCursor
		}
	}
	if all := ids("/v1/posts?collection_id=" + c.ID); len(all) != 5 {
		t.Fatalf("without hide_past: %d posts, want 5", len(all))
	}
	for _, path := range []string{"/v1/posts?hide_past=true&collection_id=" + c.ID, "/v1/collections/" + c.ID + "/posts?hide_past=true"} {
		got := ids(path)
		if len(got) != 3 || !got[draft.ID] || !got[scheduled.ID] || !got[publishing.ID] {
			t.Fatalf("%s: %v", path, got)
		}
	}

	// MCP takes the same filter.
	sc := mcpResult(e, e.admin, "list_posts", map[string]any{"collection": c.ID, "hide_past": true})
	if data, _ := sc["data"].([]any); len(data) != 3 {
		t.Fatalf("list_posts hide_past over MCP: %v", sc)
	}
}
