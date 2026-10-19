package httpapi_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

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
			resp := e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "race " + string(rune('a'+i%26))}, 0, nil)
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
	resp := e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "nul\x00byte"}, 0, nil)
	if resp.StatusCode >= 500 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// The workspace still works after it (no aborted transaction left behind).
	e.post(c.ID, map[string]any{"content": "fine"})
}
