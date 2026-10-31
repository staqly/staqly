package httpapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Returning a mutation's result must not require a read scope after the write has already committed.
func TestWriteOnlyMutationResponses(t *testing.T) {
	t.Run("brand kit", func(t *testing.T) {
		e := newEnv(t)
		writer := e.keyWith(app.ScopeMediaWrite)
		e.do(nil, "PUT", "/v1/design/brand", writer, map[string]any{"colors": []string{"#ffffff"}, "fonts": []string{}, "logos": []string{}}, 200, nil)
		e.do(nil, "GET", "/v1/design/brand", writer, nil, 403, nil)
	})
	t.Run("schedules", func(t *testing.T) {
		e := newEnv(t)
		writer := e.keyWith(app.ScopeSchedulesManage)
		c := e.collection("Schedules")
		var s app.Schedule
		e.do(nil, "POST", "/v1/schedules", writer, map[string]any{"collection": c.ID, "name": "Before", "kind": "recurring", "rule": map[string]any{"every_minutes": 60}}, 201, &s)
		e.do(nil, "PATCH", "/v1/schedules/"+s.ID, writer, map[string]any{"name": "After"}, 200, &s)
		e.do(nil, "POST", "/v1/schedules/"+s.ID+"/run", writer, nil, 202, nil)
		e.do(nil, "GET", "/v1/schedules/"+s.ID, writer, nil, 403, nil)
	})
	t.Run("jobs", func(t *testing.T) {
		e := newEnv(t)
		publisher := e.keyWith(app.ScopePostsPublish)
		ctx := context.Background()
		p, err := e.svc.AuthenticateAPIKey(ctx, e.admin)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.svc.QueueJob(ctx, p.WorkspaceID, "publish_post", "publish_post:missing", map[string]any{"post_id": "missing"}, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		var jobs app.JobPage
		e.do(nil, "GET", "/v1/jobs?kind=publish_post", e.key, nil, 200, &jobs)
		if len(jobs.Data) != 1 {
			t.Fatalf("jobs: %+v", jobs)
		}
		e.do(nil, "POST", "/v1/jobs/"+jobs.Data[0].ID+"/cancel", publisher, nil, 200, nil)
		e.do(nil, "GET", "/v1/jobs/"+jobs.Data[0].ID, publisher, nil, 403, nil)
	})
	t.Run("posts", func(t *testing.T) {
		e := newEnv(t)
		writer := e.keyWith(app.ScopePostsWrite)
		publisher := e.keyWith(app.ScopePostsPublish)
		c := e.collection("Write only")
		ch := e.customChannel("Own")
		var p app.Post
		e.do(nil, "POST", "/v1/posts", writer, map[string]any{"collection": c.ID, "title": "First", "content": "Body", "targets": []map[string]any{{"channel_id": ch.ID}}}, 201, &p)
		e.do(nil, "GET", "/v1/posts/"+p.ID, writer, nil, 403, nil)
		e.do(nil, "PATCH", "/v1/posts/"+p.ID, writer, map[string]any{"title": "Changed"}, 200, &p)
		if p.Title != "Changed" {
			t.Fatalf("mutation returned an old value: %+v", p)
		}
		e.do(nil, "POST", "/v1/posts/"+p.ID+"/duplicate", writer, nil, 201, nil)
		e.do(nil, "POST", "/v1/posts/"+p.ID+"/schedule", publisher, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
		e.do(nil, "POST", "/v1/posts/"+p.ID+"/cancel", publisher, nil, 200, nil)
		e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", publisher, nil, 202, nil)
		e.do(nil, "GET", "/v1/posts/"+p.ID, publisher, nil, 403, nil)
	})
	t.Run("collections", func(t *testing.T) {
		e := newEnv(t)
		writer := e.keyWith(app.ScopeCollectionsWrite)
		var c app.Collection
		e.do(nil, "POST", "/v1/collections", writer, map[string]any{"name": "Before"}, 201, &c)
		e.do(nil, "PATCH", "/v1/collections/"+c.ID, writer, map[string]any{"name": "After"}, 200, &c)
		if c.Name != "After" {
			t.Fatalf("mutation returned an old value: %+v", c)
		}
		e.do(nil, "GET", "/v1/collections/"+c.ID, writer, nil, 403, nil)
	})
	t.Run("media projects", func(t *testing.T) {
		e := newEnv(t)
		writer := e.keyWith(app.ScopeMediaWrite)
		var p app.MediaProject
		e.sendFile("POST", "/v1/media-projects", writer, map[string]string{"name": "Before"}, designFile("one"), 201, &p)
		e.sendFile("PUT", "/v1/media-projects/"+p.ID, writer, map[string]string{"name": "After"}, designFile("two"), 200, &p)
		if p.Name != "After" {
			t.Fatalf("mutation returned an old value: %+v", p)
		}
		e.do(nil, "GET", "/v1/media-projects/"+p.ID, writer, nil, 403, nil)
	})
	t.Run("replace media", func(t *testing.T) {
		e := newEnv(t)
		writer := e.keyWith(app.ScopeMediaWrite, app.ScopePostsWrite, app.ScopePostsDelete)
		var m app.Media
		e.upload("/v1/media", writer, nil, "before.png", realPNG(t, 2, 2), 201, &m)
		e.upload("/v1/media/"+m.ID+"/replace", writer, nil, "after.png", realPNG(t, 4, 4), 201, &m)
		e.do(nil, "GET", "/v1/media/"+m.ID+"/info", writer, nil, 403, nil)
	})
}
