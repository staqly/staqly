package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// A file in a post's thread (a comment's media) is a use like the post's own files: purging another post keeps
// it, purging the post deletes it with the post, and deleting it for good takes it out of the thread.
func TestMediaInThreadsCountsAsUse(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Threads")
	var shared, own, gone app.Media
	e.upload("/v1/media", e.key, nil, "shared.png", realPNG(t, 10, 10), 201, &shared)
	e.upload("/v1/media", e.key, nil, "own.png", realPNG(t, 11, 11), 201, &own)
	e.upload("/v1/media", e.key, nil, "gone.png", realPNG(t, 12, 12), 201, &gone)

	purged := e.post(c.ID, map[string]any{"media": []string{shared.ID},
		"thread": []map[string]any{{"content": "more", "media": []string{own.ID}}}})
	other := e.post(c.ID, map[string]any{"thread": []map[string]any{{"content": "see", "media": []string{shared.ID, gone.ID}}}})

	var res app.PurgeResult
	e.do(nil, "POST", "/v1/posts/"+purged.ID+"/purge", e.admin, nil, 200, &res)
	e.do(nil, "GET", "/v1/media/"+shared.ID, e.key, nil, 200, nil) // still in other's thread
	e.do(nil, "GET", "/v1/media/"+own.ID, e.key, nil, 404, nil)    // only the purged post's thread had it
	if res.MediaDeleted != 1 {
		t.Fatalf("media_deleted: %d, want 1", res.MediaDeleted)
	}

	e.do(nil, "DELETE", "/v1/media/"+gone.ID+"?permanent=true", e.keyWith(app.ScopePostsDelete), nil, 204, nil)
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+other.ID, e.key, nil, 200, &after)
	if len(after.Thread) != 1 || len(after.Thread[0].Media) != 1 || after.Thread[0].Media[0] != shared.ID {
		t.Fatalf("thread keeps only the files that exist: %+v", after.Thread)
	}
	// The post stays editable with its thread as the API returns it.
	e.do(nil, "PATCH", "/v1/posts/"+other.ID, e.key, map[string]any{"expected_version": after.Version,
		"thread": []map[string]any{{"content": "see", "media": after.Thread[0].Media}}}, 200, nil)
}

// A file a scheduled post uses can't go to Deleted, however many other posts use it too (the check is not
// limited to the newest posts).
func TestTrashRefusedForScheduledUseBehindManyDrafts(t *testing.T) {
	e := newEnv(t)
	e.paid()
	c := e.collection("Busy")
	ch := e.customChannel("Own")
	var m app.Media
	e.upload("/v1/media", e.key, nil, "busy.png", realPNG(t, 10, 10), 201, &m)
	post := e.post(c.ID, map[string]any{"media": []string{m.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	time.Sleep(5 * time.Millisecond) // the drafts are newer
	for range 51 {
		e.post(c.ID, map[string]any{"media": []string{m.ID}})
	}
	e.do(nil, "DELETE", "/v1/media/"+m.ID, e.keyWith(app.ScopePostsDelete), nil, 409, nil)
}

// tinyStoragePlan: room for three 1000-byte files.
const tinyStoragePlan = "tiny-storage"

func init() {
	app.Plans = append(app.Plans, app.Plan{ID: tinyStoragePlan, Name: "Tiny storage", Hidden: true,
		Limits: app.PlanLimits{PostsPerMonth: app.Unlimited, Channels: 3, Alerts: 2, Webhooks: 3, APIKeys: 5,
			StorageBytes: 3000, MaxUploadBytes: 5 << 20}, Features: []string{}})
}

// The storage limit holds under concurrent uploads: it is checked again in the transaction that records the file.
func TestStorageLimitHoldsUnderConcurrentUploads(t *testing.T) {
	e := newEnv(t)
	e.plan(tinyStoragePlan)
	file := append(append([]byte{}, pngBytes...), make([]byte, 1000-len(pngBytes))...)
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, ct := multipartBody(t, nil, fmt.Sprintf("f%d.png", i), file)
			req, _ := http.NewRequest("POST", e.srv.URL+"/v1/media", body)
			req.Header.Set("Content-Type", ct)
			req.Header.Set("Authorization", "Bearer "+e.key)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			switch resp.StatusCode {
			case 201:
				ok.Add(1)
			case 402:
			default:
				t.Errorf("upload: %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	if n := ok.Load(); n != 3 {
		t.Fatalf("%d uploads stored, the plan has room for 3", n)
	}
	var page app.MediaPage
	e.do(nil, "GET", "/v1/media", e.key, nil, 200, &page)
	if len(page.Data) != 3 {
		t.Fatalf("library: %d files", len(page.Data))
	}
}

// Saving new versions of one project at the same time leaves exactly one project file in the store: each save
// removes the file it replaced as the transaction saw it, so no version is orphaned.
func TestConcurrentProjectSavesLeaveNoOrphanFiles(t *testing.T) {
	e := newEnv(t)
	var p app.MediaProject
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Poster"}, designFile("v1"), 201, &p)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, ct := multipartBody(t, map[string]string{"name": "Poster"}, "file.bin", designFile(fmt.Sprint("v", i+2)))
			req, _ := http.NewRequest("PUT", e.srv.URL+"/v1/media-projects/"+p.ID, body)
			req.Header.Set("Content-Type", ct)
			req.Header.Set("Authorization", "Bearer "+e.key)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("save: %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	files := 0
	if err := e.svc.Cfg.Blob.Walk(context.Background(), "workspaces/", func(k string, _ int64) error {
		if strings.Contains(k, "/projects/") {
			files++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("%d project files in the store, want 1", files)
	}
}

// A file in Deleted can't be attached to a post (files or thread) or go out on a schedule; restored, it can.
func TestDeletedMediaCannotBeAttached(t *testing.T) {
	e := newEnv(t)
	e.paid()
	c := e.collection("Deleted files")
	ch := e.customChannel("Own")
	deleter := e.keyWith(app.ScopePostsDelete)
	var m app.Media
	e.upload("/v1/media", e.key, nil, "old.png", realPNG(t, 10, 10), 201, &m)
	draft := e.post(c.ID, map[string]any{"media": []string{m.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "DELETE", "/v1/media/"+m.ID, deleter, nil, 204, nil)

	var verr struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"title": "Post", "collection": c.ID, "content": "x", "media": []string{m.ID}}, 422, &verr)
	if verr.Error.Code != "validation_failed" || !strings.Contains(verr.Error.Fields["media"], "Deleted") {
		t.Fatalf("error: %+v", verr)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"title": "Post", "collection": c.ID, "content": "x",
		"thread": []map[string]any{{"content": "y", "media": []string{m.ID}}}}, 422, nil)
	// The draft that kept it can't be scheduled with it (the sweep would take it away before it goes out).
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 422, nil)

	e.do(nil, "POST", "/v1/media/"+m.ID+"/restore", deleter, nil, 200, nil)
	e.post(c.ID, map[string]any{"media": []string{m.ID}})
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
}

// Design editor projects count toward storage everywhere: the usage shown and the upload check.
func TestStorageCountsProjects(t *testing.T) {
	e := newEnv(t)
	e.plan(tinyStoragePlan) // 3000 bytes
	big := designFile(strings.Repeat("x", 2400))
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Big"}, big, 201, nil)
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if u.Usage[app.LimitStorage] != int64(len(big)) {
		t.Fatalf("storage used: %d, want %d (the project)", u.Usage[app.LimitStorage], len(big))
	}
	file := append(append([]byte{}, pngBytes...), make([]byte, 1000-len(pngBytes))...)
	e.upload("/v1/media", e.key, nil, "f.png", file, 402, nil)
}

// Publish now, a retry and a schedule run refuse a post carrying a file in Deleted, like scheduling does.
func TestDeletedMediaCannotBePublished(t *testing.T) {
	e := newEnv(t)
	e.paid()
	ctx := context.Background()
	c := e.collection("Deleted out")
	ch := e.customChannel("Own")
	deleter := e.keyWith(app.ScopePostsDelete)
	var m app.Media
	e.upload("/v1/media", e.key, nil, "old.png", realPNG(t, 10, 10), 201, &m)

	// Publish now.
	draft := e.post(c.ID, map[string]any{"media": []string{m.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "DELETE", "/v1/media/"+m.ID, deleter, nil, 204, nil)
	var verr struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/publish", e.key, nil, 422, &verr)
	if !strings.Contains(verr.Error.Fields["media"], "Deleted") {
		t.Fatalf("publish now: %+v", verr)
	}

	// A schedule run picking the draft leaves it alone and says why.
	var sc app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Daily", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"08:00"}}}, 201, &sc)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if !strings.Contains(sc.LastResult, "Deleted") {
		t.Fatalf("schedule run: %q", sc.LastResult)
	}
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+draft.ID, e.key, nil, 200, &got)
	if got.Status != "draft" {
		t.Fatalf("the run published the post: %s", got.Status)
	}
	e.do(nil, "DELETE", "/v1/schedules/"+sc.ID, e.key, nil, 204, nil)

	// Retry of a failed target: the file went to Deleted after the post failed.
	e.do(nil, "POST", "/v1/media/"+m.ID+"/restore", deleter, nil, 200, nil)
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "failed", "error": "nope"}, 200, nil)
	e.do(nil, "DELETE", "/v1/media/"+m.ID, deleter, nil, 204, nil)
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/targets/"+ch.ID+"/retry", e.key, nil, 422, nil)
	e.do(nil, "POST", "/v1/media/"+m.ID+"/restore", deleter, nil, 200, nil)
	e.do(nil, "POST", "/v1/posts/"+draft.ID+"/targets/"+ch.ID+"/retry", e.key, nil, 202, nil)
}
