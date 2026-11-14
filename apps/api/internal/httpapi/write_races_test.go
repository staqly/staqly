package httpapi_test

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// PATCH changes only the supplied fields, even when another writer changes a different field at once.
func TestParallelWebhookPatchesPreserveIndependentFields(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		var hook app.Webhook
		e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/before"}, 201, &hook)
		patches := []map[string]any{
			{"url": "https://example.com/after"},
			{"description": "updated"},
			{"events": []string{"post.created"}},
			{"active": false},
			{"timeout_ms": 20000},
			{"max_attempts": 3},
		}
		parallelPatches(t, e, "/v1/webhooks/"+hook.ID, e.key, patches)
		var got app.Webhook
		e.do(nil, "GET", "/v1/webhooks/"+hook.ID, e.key, nil, 200, &got)
		if got.URL != "https://example.com/after" || got.Description != "updated" ||
			!slices.Equal(got.Events, []string{"post.created"}) || got.Active || got.TimeoutMS != 20000 || got.MaxAttempts != 3 {
			t.Fatalf("a concurrent patch reverted another field: %+v", got)
		}
	}
}

func parallelPatches(t *testing.T, e *env, path, key string, patches []map[string]any) {
	t.Helper()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var failed atomic.Int64
	for _, patch := range patches {
		wg.Go(func() {
			<-start
			if e.do(nil, "PATCH", path, key, patch, 0, nil).StatusCode != 200 {
				failed.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d patches failed", failed.Load())
	}
}

func TestParallelAlertPatchesPreserveIndependentFields(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		var alert app.Alert
		e.do(nil, "POST", "/v1/alerts", e.admin, map[string]any{
			"name": "Before", "type": "ntfy", "config": map[string]any{"topic": "before"},
		}, 201, &alert)
		parallelPatches(t, e, "/v1/alerts/"+alert.ID, e.admin, []map[string]any{
			{"name": "After"}, {"enabled": false}, {"events": []string{"post.created"}},
			{"digest_window_seconds": 120}, {"config": map[string]any{"topic": "after"}},
			{"config": map[string]any{"priority": 4}},
		})
		var got app.Alert
		e.do(nil, "GET", "/v1/alerts/"+alert.ID, e.admin, nil, 200, &got)
		if got.Name != "After" || got.Enabled || !slices.Equal(got.Events, []string{"post.created"}) ||
			got.DigestWindowSeconds != 120 || got.Config["topic"] != "after" || got.Config["priority"] != float64(4) {
			t.Fatalf("a concurrent patch reverted another alert field: %+v", got)
		}
	}
}

func TestParallelMediaPatchesPreserveIndependentFields(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		var media app.Media
		e.upload("/v1/media", e.key, nil, "before.png", realPNG(t, 2, 2), 201, &media)
		parallelPatches(t, e, "/v1/media/"+media.ID, e.key, []map[string]any{
			{"name": "after.png"}, {"alt_text": "A description"}, {"width": 100}, {"height": 200}, {"duration_ms": 300},
		})
		var got app.Media
		e.do(nil, "GET", "/v1/media/"+media.ID+"/info", e.key, nil, 200, &got)
		if got.OriginalName != "after.png" || got.AltText != "A description" || got.Width != 100 || got.Height != 200 || got.DurationMs != 300 {
			t.Fatalf("a concurrent patch reverted another media field: %+v", got)
		}
	}
}

// Parallel creates against the Free plan's monthly post limit: exactly the limit gets through, the rest
// get 402, and the usage counter matches. On SQLite the single writer serialises them; on PostgreSQL the
// SERIALIZABLE transaction (retried on serialization failures) must give the same answer.
func TestParallelCreatesStopAtThePlanLimit(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Race")
	e.free()
	limit := 50
	var created, refused, other atomic.Int64
	var wg sync.WaitGroup
	for i := range 80 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"title": "Post", "collection": c.ID, "content": "race " + string(rune('a'+i%26))}, 0, nil)
			switch resp.StatusCode {
			case 201:
				created.Add(1)
			case 402:
				refused.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != int64(limit) || refused.Load() != int64(80-limit) || other.Load() != 0 {
		t.Fatalf("created %d, refused %d, other %d; want %d / %d / 0", created.Load(), refused.Load(), other.Load(), limit, 80-limit)
	}
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if got := u.Usage[app.LimitPostsPerMonth]; got != int64(limit) {
		t.Fatalf("usage counter %d, want %d", got, limit)
	}
}

// Parallel edits of one post with the same expected_version: one wins, every other one is a 409, and
// the version moves by exactly one.
func TestParallelEditsWithExpectedVersion(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Edits")
	p := e.post(c.ID, map[string]any{"content": "v1"})
	var ok, conflict, other atomic.Int64
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := e.do(nil, "PATCH", "/v1/posts/"+p.ID, e.key, map[string]any{"content": "edit " + string(rune('a'+i)), "expected_version": p.Version}, 0, nil)
			switch resp.StatusCode {
			case 200:
				ok.Add(1)
			case 409:
				conflict.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 19 || other.Load() != 0 {
		t.Fatalf("ok %d, conflict %d, other %d; want 1 / 19 / 0", ok.Load(), conflict.Load(), other.Load())
	}
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
	if got.Version != p.Version+1 {
		t.Fatalf("version %d, want %d", got.Version, p.Version+1)
	}
}

// A NUL character in text never becomes a 500: SQLite stores it, PostgreSQL text cannot hold it and
// the API answers 422 there.
func TestNULInTextIsNot500(t *testing.T) {
	e := newEnv(t)
	c := e.collection("NUL")
	resp := e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"title": "Post", "collection": c.ID, "content": "nul\x00byte"}, 0, nil)
	if resp.StatusCode >= 500 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// The workspace still works after it (no aborted transaction left behind).
	e.post(c.ID, map[string]any{"content": "fine"})
}
