package httpapi_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Your automation reports the same custom target several times at once (a retried request, two workers): one
// report settles it, every other one is a 409, and the post is settled (and announced) exactly once.
func TestParallelTargetResultsSettleOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Automation")
	ch := e.customChannel("My worker")
	worker := e.keyWith(app.ScopePostsPublish)
	post := e.post(c.ID, map[string]any{"content": "once", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)

	var ok, conflict, other atomic.Int64
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := "published"
			if i%2 == 1 {
				status = "failed"
			}
			resp := e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": status}, 0, nil)
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
	if ok.Load() != 1 || conflict.Load() != 11 || other.Load() != 0 {
		t.Fatalf("ok %d, conflict %d, other %d; want 1 / 11 / 0", ok.Load(), conflict.Load(), other.Load())
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?post_id="+post.ID+"&type_prefix=post.", e.key, nil, 200, &evs)
	types := map[string]int{}
	for _, ev := range evs.Data {
		types[ev.Type]++
	}
	if types["post.target_published"]+types["post.target_failed"] != 1 || types["post.published"]+types["post.failed"] != 1 {
		t.Fatalf("settled more than once: %v", types)
	}
}

// A failed publish job is retried from the job list after the target was already retried from the post (or
// the other way round): the target's work is queued once, so the platform gets the post once.
func TestRetryJobAfterRetryTargetPublishesOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Social")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &ch)
	post := e.post(c.ID, map[string]any{"content": "twice?", "targets": []map[string]any{{"channel_id": ch.ID}}})

	fakeTok.fail.Store(true)
	defer fakeTok.fail.Store(false)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 202, nil)
	base := time.Now()
	defer func() { e.svc.Now = time.Now }()
	for _, ahead := range []time.Duration{0, 2 * time.Minute, 10 * time.Minute} {
		e.svc.Now = func() time.Time { return base.Add(ahead) }
		e.svc.RunDueJobsOnce(ctx)
	}
	var jobs app.JobPage
	e.do(nil, "GET", "/v1/jobs?status=failed&kind=publish_target", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 {
		t.Fatalf("failed jobs: %+v", jobs.Data)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/retry", e.key, nil, 202, nil)
	e.do(nil, "POST", "/v1/jobs/"+jobs.Data[0].ID+"/retry", e.key, nil, 409, nil) // the target's work is queued already

	fakeTok.fail.Store(false)
	before := fakeTok.publishd.Load()
	e.svc.RunDueJobsOnce(ctx)
	if n := fakeTok.publishd.Load() - before; n != 1 {
		t.Fatalf("published %d times, want 1", n)
	}
}

// A preview link puts the post in front of anyone who has it: making and revoking one is a write
// (posts:write); a read-only key lists them and reads the preview, nothing more.
func TestPreviewLinksNeedPostsWrite(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Share")
	post := e.post(c.ID, nil)
	reader := e.keyWith(app.ScopePostsRead)
	writer := e.keyWith(app.ScopePostsWrite)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/previews", reader, map[string]any{}, 403, nil)
	var link app.PreviewLink
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/previews", writer, map[string]any{}, 201, &link)
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/previews", reader, nil, 200, nil)
	e.do(nil, "GET", "/v1/previews/"+link.Token, "", nil, 200, nil)
	e.do(nil, "DELETE", "/v1/posts/"+post.ID+"/previews/"+link.ID, reader, nil, 403, nil)
	e.do(nil, "DELETE", "/v1/posts/"+post.ID+"/previews/"+link.ID, writer, nil, 204, nil)
}
