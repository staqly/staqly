package cli

import (
	"context"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostsListQueryAndTable(t *testing.T) {
	f := newFake(t)
	sched := post("pst_1", "scheduled")
	sched["publish_at"] = "2026-11-03T09:00:00.000Z"
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{sched, post("pst_2", "draft")}})
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "mastodon", "Brand")}})
	r := f.run("posts", "list", "--status", "scheduled", "--collection", "Posts", "--channel", "Brand", "--tag", "launch", "--q", "day",
		"--publish-after", "2026-11-02", "--limit", "10").mustCode(t, 0)
	q := url.Values(f.last("GET", "/v1/posts").Query)
	if q.Get("status") != "scheduled" || q.Get("collection_id") != "Posts" || q.Get("channel_id") != "ch_1" || q.Get("tag") != "launch" ||
		q.Get("q") != "day" || !strings.HasSuffix(q.Get("publish_after"), "Z") || q.Get("limit") != "10" {
		t.Fatalf("query = %v", q)
	}
	mustContain(t, r.out, "ID", "STATUS", "PUBLISH AT", "pst_1", "pst_2", "2026-11-03", "Brand on Mastodon", "Launch day pst_1")
	f.run("posts", "list", "--publish-after", "soon").mustCode(t, ExitUsage)
	f.run("posts", "list", "--channel", "Nope").mustCode(t, ExitError)
}

func TestPostsListAllFollowsCursors(t *testing.T) {
	f := newFake(t)
	calls := 0
	f.handle("GET /v1/posts", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, 200, map[string]any{"data": []any{post("pst_1", "draft")}, "next_cursor": "c2"})
			return
		}
		writeJSON(w, 200, map[string]any{"data": []any{post("pst_2", "draft")}})
	})
	r := f.run("posts", "list", "--all", "--json").mustCode(t, 0)
	if calls != 2 || !strings.Contains(r.out, "pst_1") || !strings.Contains(r.out, "pst_2") {
		t.Fatalf("calls=%d out=%s", calls, r.out)
	}
	r = f.run("posts", "list").mustCode(t, 0)
	mustContain(t, r.err, "More posts available")
}

func TestPostsGet(t *testing.T) {
	f := newFake(t)
	p := post("pst_1", "failed")
	tg := target("ch_1", "failed")
	tg["error"] = "HTTP 500 from the platform"
	tg["attempts"] = 2
	p["targets"] = []any{tg}
	p["tags"] = []any{"launch"}
	p["metadata"] = map[string]any{"campaign": "q4"}
	f.on("GET /v1/posts/pst_1", 200, p)
	r := f.run("posts", "get", "pst_1").mustCode(t, 0)
	mustContain(t, r.out, "pst_1", "failed", "Posts", "launch", "Brand on Mastodon", "mastodon", "HTTP 500", "campaign", "Launch day pst_1")
	m := mustJSON(t, f.run("posts", "get", "pst_1", "--json").mustCode(t, 0).out)
	if m["id"] != "pst_1" || m["metadata"].(map[string]any)["campaign"] != "q4" {
		t.Fatalf("json = %v", m)
	}
}

func TestPostsCreateBody(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/posts", 201, post("pst_1", "draft"))
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "mastodon", "Brand"), channel("ch_2", "bluesky", "Sky")}})
	r := f.run("posts", "create", "Blog posts", "--title", "Post", "--content", "Hello", "--channel", "Brand,ch_2", "--tag", "launch", "--tag", "q4",
		"--meta", "score=0.9", "--metadata", `{"campaign":"q4"}`, "--external-ref", "T-1", "--media", "med_1", "--source", "agent").mustCode(t, 0)
	mustContain(t, r.out, "Created pst_1")
	c := f.last("POST", "/v1/posts")
	b := c.json(t)
	if b["collection"] != "Blog posts" || b["title"] != "Post" || b["content"] != "Hello" || b["source"] != "agent" || b["external_ref"] != "T-1" {
		t.Fatalf("body = %v", b)
	}
	targets := b["targets"].([]any)
	if len(targets) != 2 || targets[0].(map[string]any)["channel_id"] != "ch_1" || targets[1].(map[string]any)["channel_id"] != "ch_2" {
		t.Fatalf("targets = %v", targets)
	}
	if tags := b["tags"].([]any); len(tags) != 2 || tags[0] != "launch" {
		t.Fatalf("tags = %v", tags)
	}
	meta := b["metadata"].(map[string]any)
	if meta["campaign"] != "q4" || meta["score"].(float64) != 0.9 {
		t.Fatalf("metadata = %v", meta)
	}
	if b["media"].([]any)[0] != "med_1" || b["status"] != nil || b["publish_now"] != nil {
		t.Fatalf("body = %v", b)
	}
	if c.Header.Get("Idempotency-Key") == "" {
		t.Fatal("create must send an Idempotency-Key")
	}

	// --publish-at makes it scheduled; --now publishes; both together is a usage error.
	f.run("posts", "create", "Blog posts", "--title", "Post", "--content", "x", "--channel", "ch_1", "--publish-at", "2026-11-03T09:00:00Z").mustCode(t, 0)
	b = f.last("POST", "/v1/posts").json(t)
	if b["status"] != "scheduled" || b["publish_at"] != "2026-11-03T09:00:00Z" {
		t.Fatalf("scheduled body = %v", b)
	}
	f.run("posts", "create", "Blog posts", "--title", "Post", "--content", "x", "--channel", "ch_1", "--now", "--idempotency-key", "job-1").mustCode(t, 0)
	c = f.last("POST", "/v1/posts")
	if c.json(t)["publish_now"] != true || c.Header.Get("Idempotency-Key") != "job-1" {
		t.Fatalf("now body = %v %v", c.json(t), c.Header)
	}
	f.run("posts", "create", "Blog posts", "--title", "Post", "--content", "x", "--now", "--publish-at", "+1h").mustCode(t, ExitUsage)
	f.run("posts", "create", "Blog posts", "--title", "Post", "--content", "x", "--publish-at", "next tuesday").mustCode(t, ExitUsage)
	f.run("posts", "create", "Blog posts", "--title", "Post", "--meta", "broken").mustCode(t, ExitUsage)
	f.run("posts", "create", "Blog posts", "--content", "x").mustCode(t, ExitUsage)                 // no title
	f.run("posts", "create", "Blog posts", "--title", " ", "--content", "x").mustCode(t, ExitUsage) // a blank one
}

func TestPostsCreateContentSourcesAndFile(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/posts", 201, post("pst_1", "draft"))
	var uploaded string
	f.handle("POST /v1/media", func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil || part.FormName() != "file" {
			writeJSON(w, 422, apiErr(422, "validation_failed", "file is required"))
			return
		}
		uploaded = part.FileName() + ":" + readAllString(part)
		writeJSON(w, 201, map[string]any{"id": "med_9", "mime_type": "image/png", "size_bytes": 4, "original_name": "cover.png", "url": "/v1/media/med_9"})
	})
	f.runWith(runOpts{stdin: "from stdin"}, "posts", "create", "Posts", "--title", "Post", "--stdin").mustCode(t, 0)
	if b := f.last("POST", "/v1/posts").json(t); b["content"] != "from stdin" {
		t.Fatalf("stdin body = %v", b)
	}
	f.runWith(runOpts{stdin: "dash"}, "posts", "create", "Posts", "--title", "Post", "--content", "-").mustCode(t, 0)
	if b := f.last("POST", "/v1/posts").json(t); b["content"] != "dash" {
		t.Fatalf("dash body = %v", b)
	}
	dir := t.TempDir()
	txt := filepath.Join(dir, "post.md")
	os.WriteFile(txt, []byte("# from file"), 0o600)
	png := filepath.Join(dir, "cover.png")
	os.WriteFile(png, []byte("\x89PNG"), 0o600)
	f.run("posts", "create", "Posts", "--title", "Post", "--content", "@"+txt, "--file", png).mustCode(t, 0)
	b := f.last("POST", "/v1/posts").json(t)
	if b["content"] != "# from file" || b["media"].([]any)[0] != "med_9" || uploaded != "cover.png:\x89PNG" {
		t.Fatalf("file body = %v uploaded=%q", b, uploaded)
	}
	f.run("posts", "create", "Posts", "--title", "Post", "--file", filepath.Join(dir, "missing.png")).mustCode(t, ExitError)
}

func TestPostsUpdate(t *testing.T) {
	f := newFake(t)
	f.on("PATCH /v1/posts/pst_1", 200, post("pst_1", "draft"))
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "mastodon", "Brand")}})
	f.run("posts", "update", "pst_1", "--title", "New title", "--content", "Edited", "--channel", "Brand", "--publish-at", "2026-11-04 10:00", "--expected-version", "3",
		"--collection", "Archive", "--tag", "a").mustCode(t, 0)
	b := f.last("PATCH", "/v1/posts/pst_1").json(t)
	if b["title"] != "New title" || b["content"] != "Edited" || b["expected_version"] != float64(3) || b["collection"] != "Archive" || b["publish_at"] == nil ||
		b["targets"].([]any)[0].(map[string]any)["channel_id"] != "ch_1" || b["tags"].([]any)[0] != "a" {
		t.Fatalf("body = %v", b)
	}
	f.run("posts", "update", "pst_1", "--clear-publish-at", "--no-channels").mustCode(t, 0)
	b = f.last("PATCH", "/v1/posts/pst_1").json(t)
	if v, ok := b["publish_at"]; !ok || v != nil {
		t.Fatalf("clear body = %v", b)
	}
	if len(b["targets"].([]any)) != 0 {
		t.Fatalf("no-channels body = %v", b)
	}
	f.run("posts", "update", "pst_1").mustCode(t, ExitUsage)
	f.run("posts", "update", "pst_1", "--clear-publish-at", "--publish-at", "+1d").mustCode(t, ExitUsage)
}

func TestPostsScheduleCancelDuplicateDelete(t *testing.T) {
	f := newFake(t)
	sched := post("pst_1", "scheduled")
	sched["publish_at"] = "2026-11-03T09:00:00.000Z"
	f.on("POST /v1/posts/pst_1/schedule", 200, sched)
	f.on("POST /v1/posts/pst_1/cancel", 200, post("pst_1", "draft"))
	f.on("POST /v1/posts/pst_1/duplicate", 201, post("pst_2", "draft"))
	f.handle("DELETE /v1/posts/pst_1", noContent)
	f.handle("DELETE /v1/posts/pst_2", noContent)
	f.on("DELETE /v1/posts/pst_3", 404, apiErr(404, "not_found", "no such post"))
	f.on("POST /v1/posts/pst_1/purge", 200, map[string]any{"post_id": "pst_1", "events_deleted": 7, "media_deleted": 1})

	r := f.run("posts", "schedule", "pst_1", "2026-11-03T09:00:00Z").mustCode(t, 0)
	mustContain(t, r.out, "Scheduled pst_1", "2026-11-03")
	if b := f.last("POST", "/v1/posts/pst_1/schedule").json(t); b["publish_at"] != "2026-11-03T09:00:00Z" {
		t.Fatalf("body = %v", b)
	}
	f.run("posts", "schedule", "pst_1", "whenever").mustCode(t, ExitUsage)
	mustContain(t, f.run("posts", "cancel", "pst_1").mustCode(t, 0).out, "Cancelled pst_1")
	mustContain(t, f.run("posts", "duplicate", "pst_1").mustCode(t, 0).out, "Created draft pst_2")

	mustContain(t, f.run("posts", "delete", "pst_1").mustCode(t, 0).out, "Deleted post pst_1")
	r = f.run("posts", "delete", "pst_1", "pst_2", "pst_3").mustCode(t, ExitError)
	mustContain(t, r.out, "Deleted post pst_1", "Deleted post pst_2")
	mustContain(t, r.err, "pst_3", "1 of 3 failed")
	m := mustJSON(t, f.run("posts", "delete", "pst_2", "--json").mustCode(t, 0).out)
	if m["deleted"] != true {
		t.Fatalf("json = %v", m)
	}

	f.run("posts", "purge", "pst_1").mustCode(t, ExitUsage)
	f.runWith(runOpts{stdinTTY: true, stdin: "n\n"}, "posts", "purge", "pst_1").mustCode(t, ExitError)
	if len(f.find("POST", "/v1/posts/pst_1/purge")) != 0 {
		t.Fatal("must not purge without confirmation")
	}
	mustContain(t, f.run("posts", "purge", "pst_1", "--yes").mustCode(t, 0).out, "7 events", "1 media")
}

func TestPostsPublishWait(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/posts/pst_1/publish", 202, post("pst_1", "publishing"))
	done := post("pst_1", "published")
	tg := target("ch_1", "published")
	tg["external_url"] = "https://mastodon.social/@brand/1"
	done["targets"] = []any{tg}
	f.seq("GET /v1/posts/pst_1",
		respond(200, post("pst_1", "publishing")),
		respond(503, apiErr(503, "busy", "restarting")),
		respond(200, done))

	r := f.run("posts", "publish", "pst_1").mustCode(t, 0)
	mustContain(t, r.out, "Publishing pst_1")

	r = f.run("posts", "publish", "pst_1", "--wait").mustCode(t, 0)
	mustContain(t, r.out, "published Brand on Mastodon https://mastodon.social/@brand/1", "Published pst_1")
	mustContain(t, r.err, "Waiting for pst_1", "warning")
	if len(r.sleeps) != 2 {
		t.Fatalf("sleeps = %v", r.sleeps)
	}

	// failed → exit 2 with the target error; --json prints the final post.
	failed := post("pst_1", "failed")
	ftg := target("ch_1", "failed")
	ftg["error"] = "token expired"
	failed["targets"] = []any{ftg}
	f.on("GET /v1/posts/pst_1", 200, failed)
	r = f.run("posts", "publish", "pst_1", "--wait").mustCode(t, ExitFailed)
	mustContain(t, r.out, "failed Brand on Mastodon: token expired")
	mustContain(t, r.err, "staqly posts retry pst_1")
	r = f.run("posts", "publish", "pst_1", "--wait", "--json").mustCode(t, ExitFailed)
	if mustJSON(t, r.out)["status"] != "failed" {
		t.Fatalf("json = %s", r.out)
	}

	// timeout → exit 3.
	f.on("GET /v1/posts/pst_1", 200, post("pst_1", "publishing"))
	r = f.run("posts", "publish", "pst_1", "--wait", "--timeout", "5s").mustCode(t, ExitTimeout)
	mustContain(t, r.err, "timed out after 5s")

	// Ctrl-C while waiting → 130.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.runWith(runOpts{ctx: ctx, sleep: func(context.Context, time.Duration) error { cancel(); return context.Canceled }},
		"posts", "publish", "pst_1", "--wait").mustCode(t, ExitInterrupted)

	// A fatal error from the server ends the wait.
	f.on("GET /v1/posts/pst_1", 404, apiErr(404, "not_found", "gone"))
	f.run("posts", "publish", "pst_1", "--wait").mustCode(t, ExitError)
}

func TestPostsCreateNowWait(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/posts", 201, post("pst_1", "publishing"))
	f.on("GET /v1/posts/pst_1", 200, post("pst_1", "published"))
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "mastodon", "Brand")}})
	f.run("posts", "create", "Posts", "--title", "Post", "--content", "x", "--channel", "Brand", "--now", "--wait").mustCode(t, 0)
	// A plain draft with --wait has nothing to wait for.
	f.on("POST /v1/posts", 201, post("pst_1", "draft"))
	r := f.run("posts", "create", "Posts", "--title", "Post", "--content", "x", "--wait").mustCode(t, 0)
	mustContain(t, r.out, "Created pst_1 (draft)")
}

func TestPostsRetryResultPreviewHistoryTags(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "custom", "Pipeline")}})
	f.on("POST /v1/posts/pst_1/targets/ch_1/retry", 202, post("pst_1", "publishing"))
	f.on("POST /v1/posts/pst_1/targets/ch_1/result", 200, post("pst_1", "published"))
	f.on("GET /v1/posts/pst_1/preview", 200, map[string]any{"targets": []any{map[string]any{"channel_id": "ch_1", "text": "Hello", "warnings": []any{"too long"}, "ok": false}}})
	f.on("GET /v1/posts/pst_1/events", 200, map[string]any{"data": []any{
		map[string]any{"id": "e2", "seq": 2, "type": "post.published", "actor": map[string]any{"type": "system", "name": "jobs"}, "data": map[string]any{"status": "published"}, "created_at": "2026-10-13T10:01:00Z"},
		map[string]any{"id": "e1", "seq": 1, "type": "post.created", "actor": map[string]any{"type": "local", "name": "Local"}, "data": map[string]any{}, "created_at": "2026-10-13T10:00:00Z"}}})
	f.on("GET /v1/tags", 200, map[string]any{"data": []any{map[string]any{"name": "launch", "count": 3}}})

	mustContain(t, f.run("posts", "retry", "pst_1", "Pipeline").mustCode(t, 0).out, "Retrying pst_1")
	r := f.run("posts", "result", "pst_1", "ch_1", "--status", "published", "--url", "https://x/1", "--external-id", "42").mustCode(t, 0)
	mustContain(t, r.out, "Recorded published", "published")
	if b := f.last("POST", "/v1/posts/pst_1/targets/ch_1/result").json(t); b["status"] != "published" || b["url"] != "https://x/1" || b["external_id"] != "42" {
		t.Fatalf("body = %v", b)
	}
	f.run("posts", "result", "pst_1", "ch_1", "--status", "failed", "--error", "boom").mustCode(t, 0)
	if b := f.last("POST", "/v1/posts/pst_1/targets/ch_1/result").json(t); b["status"] != "failed" || b["error"] != "boom" {
		t.Fatalf("body = %v", b)
	}
	f.run("posts", "result", "pst_1", "ch_1", "--status", "done").mustCode(t, ExitUsage)

	mustContain(t, f.run("posts", "preview", "pst_1").mustCode(t, 0).out, "ch_1", "no", "too long", "Hello")

	f.on("GET /v1/posts/pst_1/metrics", 200, map[string]any{"data": []any{
		map[string]any{"channel_id": "ch_1", "channel_name": "Bear on X", "available": true, "metrics": map[string]any{"impressions": 1200, "likes": 34}, "updated_at": "2026-10-13T10:00:00Z"},
		map[string]any{"channel_id": "ch_2", "channel_name": "News", "available": false, "metrics": nil, "updated_at": nil}}})
	mustContain(t, f.run("posts", "stats", "pst_1", "--refresh").mustCode(t, 0).out, "Bear on X", "1200", "34", "not available for this platform")
	if q := f.last("GET", "/v1/posts/pst_1/metrics").Query; len(q["refresh"]) != 1 || q["refresh"][0] != "true" {
		t.Fatalf("refresh query = %v", q)
	}
	f.on("GET /v1/posts/pst_1/responses", 200, map[string]any{
		"overall": map[string]any{"positive": 2, "neutral": 0, "negative": 1, "summary": "Mostly happy", "themes": []any{"pricing"}, "questions": []any{"When ships?"}},
		"channels": []any{map[string]any{"channel_id": "ch_1", "channel_name": "Bear on X", "available": true, "insight": nil, "responses": []any{
			map[string]any{"author": "Old", "text": "first reply", "sentiment": "positive", "created_at": "2026-10-13T10:00:00Z"},
			map[string]any{"author": "New", "text": "second reply", "sentiment": "negative", "created_at": "2026-10-14T10:00:00Z"}}}}})
	rr := f.run("posts", "responses", "pst_1").mustCode(t, 0)
	mustContain(t, rr.out, "Mostly happy", "pricing", "When ships?", "Bear on X", "first reply")
	if strings.Index(rr.out, "second reply") > strings.Index(rr.out, "first reply") {
		t.Fatalf("responses must be newest first:\n%s", rr.out)
	}
	f.on("POST /v1/posts/pst_1/responses/refresh", 202, map[string]any{"overall": nil, "channels": []any{}})
	f.run("posts", "responses", "pst_1", "--refresh").mustCode(t, 0)
	f.on("POST /v1/posts/pst_1/previews", 201, map[string]any{"id": "prv_1", "url": "http://localhost:8080/preview#tok"})
	mustContain(t, f.run("posts", "share", "pst_1", "--expires", "7").mustCode(t, 0).out, "http://localhost:8080/preview#tok", "shown only now")
	if b := f.last("POST", "/v1/posts/pst_1/previews").json(t); b["expires_in_days"] != float64(7) {
		t.Fatalf("share body = %v", b)
	}
	f.on("GET /v1/posts/pst_1/comments", 200, map[string]any{"data": []any{
		map[string]any{"author_name": "Ana", "author_email": "ana@example.com", "body": "Shorter, please", "quote": "v1.4", "created_at": "2026-10-13T10:00:00Z"}}})
	mustContain(t, f.run("posts", "comments", "pst_1").mustCode(t, 0).out, "Ana <ana@example.com>", "Shorter, please")
	r = f.run("posts", "history", "pst_1").mustCode(t, 0)
	if strings.Index(r.out, "post.created") > strings.Index(r.out, "post.published") {
		t.Fatalf("history must be oldest first:\n%s", r.out)
	}
	mustContain(t, r.out, "jobs", "status=published")
	mustContain(t, f.run("posts", "tags").mustCode(t, 0).out, "launch", "3")
}

func TestPostsRepeat(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/posts", 201, post("pst_1", "scheduled"))
	f.on("PATCH /v1/posts/pst_1", 200, post("pst_1", "scheduled"))
	f.run("posts", "create", "Tips", "--title", "Post", "--content", "x", "--channel", "ch_1", "--publish-at", "+1d", "--repeat", "1w").mustCode(t, 0)
	if b := f.last("POST", "/v1/posts").json(t); b["repeat_every"] != "1w" {
		t.Fatalf("create body = %v", b)
	}
	f.run("posts", "update", "pst_1", "--repeat", "none").mustCode(t, 0)
	if v, ok := f.last("PATCH", "/v1/posts/pst_1").json(t)["repeat_every"]; !ok || v != "" {
		t.Fatalf("none clears: %v", v)
	}
	f.run("posts", "create", "Tips", "--title", "Post", "--content", "x", "--repeat", "weekly").mustCode(t, ExitUsage)

	p := post("pst_1", "scheduled")
	p["repeat_every"] = "2w"
	f.on("GET /v1/posts/pst_1", 200, p)
	mustContain(t, f.run("posts", "get", "pst_1").mustCode(t, 0).out, "every 2 weeks")
}

func TestPostsThread(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/posts", 201, post("pst_1", "draft"))
	f.on("PATCH /v1/posts/pst_1", 200, post("pst_1", "draft"))
	f.run("posts", "create", "Tips", "--title", "Post", "--content", "x", "--reply", "first", "--reply-delay", "5m", "--reply", "second", "--reply", "third").mustCode(t, 0)
	th, _ := f.last("POST", "/v1/posts").json(t)["thread"].([]any)
	if len(th) != 3 {
		t.Fatalf("thread = %v", th)
	}
	// --reply-delay applies to the --reply that follows it only.
	for i, want := range []float64{0, 300, 0} {
		if part := th[i].(map[string]any); part["delay_seconds"] != want {
			t.Fatalf("part %d = %v", i, part)
		}
	}
	f.run("posts", "create", "Tips", "--title", "Post", "--content", "x", "--reply-delay", "2d", "--reply", "y").mustCode(t, ExitUsage)
	f.run("posts", "update", "pst_1", "--no-replies").mustCode(t, 0)
	if v, ok := f.last("PATCH", "/v1/posts/pst_1").json(t)["thread"].([]any); !ok || len(v) != 0 {
		t.Fatalf("--no-replies clears: %v", v)
	}

	p := post("pst_1", "published")
	p["thread"] = []any{map[string]any{"content": "the follow-up", "media": []any{}, "delay_seconds": 60}}
	p["targets"] = []any{map[string]any{"channel_id": "ch_1", "channel_name": "X account", "provider": "x", "status": "published",
		"replies": []any{map[string]any{"index": 0, "status": "published", "external_url": "https://x.com/a/status/2"}}}}
	f.on("GET /v1/posts/pst_1", 200, p)
	out := f.run("posts", "get", "pst_1").mustCode(t, 0).out
	mustContain(t, out, "Thread (1 reply)")
	mustContain(t, out, "after 1m0s")
	mustContain(t, out, "https://x.com/a/status/2")
}
