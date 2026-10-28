package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func future(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }

// A post's life as REST sees it: draft, scheduled (one publish job, replaced on reschedule), back to draft,
// duplicated, previewed, listed by every filter, edited until published, deleted with its history kept.
func TestPostLifecycleREST(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Launch")
	ch := e.customChannel("Bot")

	// Validation.
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"content": "no collection"}, 422, &errBody)
	if errBody.Error.Fields["collection"] == "" {
		t.Fatalf("missing collection: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "x", "publish_at": "2001-01-01T00:00:00Z"}, 422, &errBody)
	if errBody.Error.Fields["publish_at"] == "" {
		t.Fatalf("past publish_at: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "x", "status": "scheduled", "publish_at": future(time.Hour)}, 422, &errBody)
	if errBody.Error.Fields["targets"] == "" {
		t.Fatalf("scheduled without a target: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "x", "targets": []map[string]any{{"channel_id": "ch_nope"}}}, 422, &errBody)
	if errBody.Error.Fields["targets.ch_nope.channel_id"] == "" && errBody.Error.Fields["targets"] == "" {
		t.Fatalf("unknown channel: %+v", errBody.Error)
	}

	// A draft with a target, tags and metadata.
	post := e.post(c.ID, map[string]any{"content": "Hello launch", "tags": []string{"launch", " launch ", "news"}, "metadata": map[string]any{"k": 1},
		"targets": []map[string]any{{"channel_id": ch.ID, "settings": map[string]string{"note": "x"}}}})
	if post.Status != "draft" || post.CollectionName != "Launch" || len(post.Targets) != 1 || post.Targets[0].ChannelName != "Bot" ||
		post.Targets[0].Provider != "custom" || post.Targets[0].Status != "pending" || len(post.Tags) != 2 || post.Version != 1 {
		t.Fatalf("draft: %+v", post)
	}

	// Scheduling queues one publish job; rescheduling replaces it (same key).
	at := future(2 * time.Hour)
	var got app.Post
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": at}, 200, &got)
	if got.Status != "scheduled" || got.PublishAt == nil {
		t.Fatalf("scheduled: %+v", got)
	}
	var jobs app.JobPage
	e.do(nil, "GET", "/v1/jobs?kind=publish_post&status=queued", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 || jobs.Data[0].Payload.PostID != post.ID {
		t.Fatalf("publish job: %+v", jobs)
	}
	at2 := future(3 * time.Hour)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": at2}, 200, &got)
	e.do(nil, "GET", "/v1/jobs?kind=publish_post&status=queued", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 || !strings.HasPrefix(jobs.Data[0].RunAt, at2[:16]) {
		t.Fatalf("rescheduled job: %+v", jobs.Data)
	}
	var job app.Job
	e.do(nil, "GET", "/v1/jobs/"+jobs.Data[0].ID, e.key, nil, 200, &job)
	if job.Kind != "publish_post" || job.Key != "publish_post:"+post.ID {
		t.Fatalf("get job: %+v", job)
	}
	// Listing by publish window and by status.
	var page app.PostPage
	e.do(nil, "GET", "/v1/posts?status=scheduled&publish_after="+time.Now().UTC().Format("2006-01-02"), e.key, nil, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("scheduled listing: %d", len(page.Data))
	}
	e.do(nil, "GET", "/v1/collections/"+c.ID+"/posts?status=draft", e.key, nil, 200, &page)
	if len(page.Data) != 0 {
		t.Fatalf("draft listing while scheduled: %d", len(page.Data))
	}
	// Cancel: back to draft, the job is gone.
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/cancel", e.key, nil, 200, &got)
	if got.Status != "draft" {
		t.Fatalf("cancelled: %+v", got)
	}
	e.do(nil, "GET", "/v1/jobs?kind=publish_post&status=queued", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 0 {
		t.Fatalf("job after cancel: %+v", jobs.Data)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/cancel", e.key, nil, 409, nil) // already a draft

	// Duplicate, preview, history, tags.
	var dup app.Post
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/duplicate", e.key, nil, 201, &dup)
	if dup.ID == post.ID || dup.Status != "draft" || dup.Content != post.Content || len(dup.Targets) != 1 || len(dup.Tags) != 2 {
		t.Fatalf("duplicate: %+v", dup)
	}
	var preview app.PostPreview
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/preview", e.key, nil, 200, &preview)
	if len(preview.Targets) != 1 || !preview.Targets[0].OK || preview.Targets[0].Text != "Hello launch" {
		t.Fatalf("preview: %+v", preview)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/events", e.key, nil, 200, &evs)
	types := map[string]int{}
	for _, ev := range evs.Data {
		types[ev.Type]++
		if ev.PostID == nil || *ev.PostID != post.ID {
			t.Fatalf("event of another post: %+v", ev)
		}
	}
	if types["post.created"] != 1 || types["post.scheduled"] != 2 || types["post.unscheduled"] != 1 {
		t.Fatalf("history: %v", types)
	}
	var tags struct{ Data []app.Tag }
	e.do(nil, "GET", "/v1/tags", e.key, nil, 200, &tags)
	if len(tags.Data) != 2 || tags.Data[0].Count != 2 {
		t.Fatalf("tags: %+v", tags.Data)
	}

	// Filters: tag, channel, text, sort; a bad sort is a 422.
	for q, want := range map[string]int{"tag=launch": 2, "tag=nope": 0, "channel_id=" + ch.ID: 2, "q=hello": 2, "q=zebra": 0,
		"collection_id=" + c.ID + "&sort=created_desc": 2, "collection_id=Launch": 2} {
		e.do(nil, "GET", "/v1/posts?"+q, e.key, nil, 200, &page)
		if len(page.Data) != want {
			t.Fatalf("%s: %d posts, want %d", q, len(page.Data), want)
		}
	}
	e.do(nil, "GET", "/v1/posts?sort=sideways", e.key, nil, 422, nil)
	e.do(nil, "GET", "/v1/posts?publish_after=yesterday", e.key, nil, 422, nil)
	// Pages.
	e.do(nil, "GET", "/v1/posts?limit=1&sort=created_asc", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.NextCursor == "" || page.Data[0].ID != post.ID {
		t.Fatalf("first page: %+v", page)
	}
	e.do(nil, "GET", "/v1/posts?limit=1&sort=created_asc&cursor="+page.NextCursor, e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].ID != dup.ID {
		t.Fatalf("second page: %+v", page)
	}

	// Edits: content and the version check; publish_at null clears; targets replace.
	e.do(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"content": "Hello v2", "expected_version": 99}, 409, nil)
	e.do(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"content": "Hello v2", "targets": []map[string]any{}}, 200, &got)
	if got.Content != "Hello v2" || len(got.Targets) != 0 || got.Version <= post.Version {
		t.Fatalf("edited: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"bogus": 1}, 400, nil)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 422, nil) // no target

	// Publish the duplicate through the custom channel; a published post is read-only.
	e.do(nil, "POST", "/v1/posts/"+dup.ID+"/publish", e.key, nil, 202, &got)
	e.svc.RunDueJobsOnce(context.Background())
	e.do(nil, "POST", "/v1/posts/"+dup.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "published", "url": "https://example.com/p/1"}, 200, &got)
	if got.Status != "published" || got.PublishedAt == nil || got.Targets[0].ExternalURL != "https://example.com/p/1" {
		t.Fatalf("published: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/posts/"+dup.ID, e.key, map[string]any{"content": "too late"}, 409, nil)
	e.do(nil, "POST", "/v1/posts/"+dup.ID+"/publish", e.key, nil, 409, nil)
	e.do(nil, "POST", "/v1/posts/"+dup.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "published"}, 409, nil)

	// Deleting needs posts:delete and keeps the history.
	e.do(nil, "DELETE", "/v1/posts/"+post.ID, e.key, nil, 403, nil)
	e.do(nil, "DELETE", "/v1/posts/"+post.ID, e.keyWith(app.ScopePostsDelete), nil, 204, nil)
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/events?post_id="+post.ID+"&type=post.deleted", e.key, nil, 200, &evs)
	if len(evs.Data) != 1 {
		t.Fatalf("post.deleted event: %+v", evs.Data)
	}
	// Purging is admin only and removes the history too.
	e.do(nil, "POST", "/v1/posts/"+dup.ID+"/purge", e.key, nil, 403, nil)
	var purged app.PurgeResult
	e.do(nil, "POST", "/v1/posts/"+dup.ID+"/purge", e.admin, nil, 200, &purged)
	if purged.PostID != dup.ID || purged.EventsDeleted == 0 {
		t.Fatalf("purge: %+v", purged)
	}
	e.do(nil, "GET", "/v1/events?post_id="+dup.ID, e.key, nil, 200, &evs)
	if len(evs.Data) != 1 || evs.Data[0].Type != "post.purged" || strings.Contains(string(evs.Data[0].Data), "Hello") {
		t.Fatalf("events after purge: %+v", evs.Data)
	}
}

// Your own automation: publishing emits post.publish_requested and lists the target as requested work until
// a result is reported; a failed target can be retried; a target nobody reports on fails after the
// collection's processing timeout.
func TestCustomTargetRoundTrip(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Automation")
	ch := e.customChannel("My worker")
	worker := e.keyWith(app.ScopePostsRead, app.ScopePostsPublish) // the "Publisher automation" preset

	post := e.post(c.ID, map[string]any{"content": "ship it", "targets": []map[string]any{{"channel_id": ch.ID}}})
	var got app.Post
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 202, &got)
	if got.Status != "publishing" || got.Targets[0].Status != "publishing" {
		t.Fatalf("publishing: %+v", got)
	}
	// Before the job ran nothing is requested yet; after it, the target waits for the automation.
	e.svc.RunDueJobsOnce(ctx)
	var work struct{ Data []app.WorkItem }
	e.do(nil, "GET", "/v1/work", worker, nil, 200, &work)
	if len(work.Data) != 1 || work.Data[0].Kind != "target" || work.Data[0].Post.ID != post.ID || work.Data[0].Channel.ID != ch.ID ||
		work.Data[0].Collection.ID != c.ID || work.Data[0].RequestedAt == "" {
		t.Fatalf("requested work: %+v", work.Data)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?type=post.publish_requested&channel_id="+ch.ID, e.key, nil, 200, &evs)
	if len(evs.Data) != 1 || evs.Data[0].ChannelID == nil || *evs.Data[0].ChannelID != ch.ID {
		t.Fatalf("publish_requested: %+v", evs.Data)
	}

	// The automation reports a failure, a person retries, the automation reports success.
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "nope"}, 422, nil)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "failed", "error": "rate limited"}, 200, &got)
	if got.Status != "failed" || got.Targets[0].Status != "failed" || got.Targets[0].Error != "rate limited" {
		t.Fatalf("failed: %+v", got)
	}
	e.do(nil, "GET", "/v1/work", worker, nil, 200, &work)
	if len(work.Data) != 0 {
		t.Fatalf("work after a result: %+v", work.Data)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/retry", e.key, nil, 202, &got)
	if got.Status != "publishing" || got.Targets[0].Status != "publishing" || got.Targets[0].Error != "" {
		t.Fatalf("retrying: %+v", got)
	}
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "published", "url": "https://example.com/p/9", "external_id": "9"}, 200, &got)
	if got.Status != "published" || got.Targets[0].ExternalID != "9" || got.Targets[0].PublishedAt == nil {
		t.Fatalf("published: %+v", got)
	}
	e.do(nil, "GET", "/v1/events?post_id="+post.ID+"&type_prefix=post.", e.key, nil, 200, &evs)
	types := map[string]int{}
	for _, ev := range evs.Data {
		types[ev.Type]++
		// Per-target events name the channel and its platform (notifications say where a post went).
		if ev.Type == "post.target_published" || ev.Type == "post.target_failed" {
			var d struct {
				Channel map[string]any `json:"channel"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			c := d.Channel
			if c["id"] != ch.ID || c["name"] != "My worker" || c["provider"] != "custom" || c["provider_name"] != "Your own automation" {
				t.Fatalf("%s channel: %+v", ev.Type, c)
			}
			if _, ok := c["credentials"]; ok {
				t.Fatalf("credentials in an event: %+v", c)
			}
		}
	}
	if types["post.publish_requested"] != 2 || types["post.target_failed"] != 1 || types["post.failed"] != 1 ||
		types["post.target_published"] != 1 || types["post.published"] != 1 {
		t.Fatalf("history: %v", types)
	}

	// Nobody reports: the collection's timeout fails the target.
	e.do(nil, "PATCH", "/v1/collections/"+c.ID, e.key, map[string]any{"processing_timeout_seconds": 60}, 200, nil)
	slow := e.post(c.ID, map[string]any{"content": "forgotten", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+slow.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	var jobs app.JobPage
	e.do(nil, "GET", "/v1/jobs?kind=target_timeout&status=queued", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 || jobs.Data[0].Payload.PostID != slow.ID {
		t.Fatalf("timeout job: %+v", jobs.Data)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+slow.ID, e.key, nil, 200, &got)
	if got.Status != "failed" || !strings.Contains(got.Targets[0].Error, "No result reported") {
		t.Fatalf("timed out: %+v", got)
	}
	e.do(nil, "POST", "/v1/posts/"+slow.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "published"}, 409, nil)
	var stats app.JobStats
	e.do(nil, "GET", "/v1/jobs/stats", e.key, nil, 200, &stats)
	if stats.Done24h < 3 || stats.Queued != 0 || stats.Running != 0 {
		t.Fatalf("stats: %+v", stats)
	}
}

// Webhooks over REST: list, get, update, test, delete.
func TestWebhookCRUDREST(t *testing.T) {
	e := newEnv(t)
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/hook", "events": []string{"post.*"}}, 201, &wh)
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/hook", "events": []string{"item.*"}}, 422, nil)
	var list struct{ Data []app.Webhook }
	e.do(nil, "GET", "/v1/webhooks", e.key, nil, 200, &list)
	if len(list.Data) != 1 || !strings.HasPrefix(list.Data[0].Secret, "whsec_") || !strings.Contains(list.Data[0].Secret, "*") {
		t.Fatalf("list: %+v", list.Data)
	}
	var got app.Webhook
	e.do(nil, "GET", "/v1/webhooks/"+wh.ID, e.key, nil, 200, &got)
	e.do(nil, "PATCH", "/v1/webhooks/"+wh.ID, e.key, map[string]any{"description": "deploys", "events": []string{"post.published", "schedule.*"}}, 200, &got)
	if got.Description != "deploys" || len(got.Events) != 2 {
		t.Fatalf("patched: %+v", got)
	}
	stop := runWorker(e)
	defer stop()
	var dl app.Delivery
	e.do(nil, "POST", "/v1/webhooks/"+wh.ID+"/test", e.key, nil, 202, &dl)
	if dl.EventType != "webhook.test" {
		t.Fatalf("test delivery: %+v", dl)
	}
	e.do(nil, "DELETE", "/v1/webhooks/"+wh.ID, e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/webhooks/"+wh.ID, e.key, nil, 404, nil)
}

// Agents (REST clients by User-Agent) and renaming the workspace are admin operations.
func TestAgentsAndWorkspaceRenameREST(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Agents")
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/posts", strings.NewReader(`{"collection":"`+c.ID+`","content":"from a script"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("User-Agent", "my-agent/2.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("post as agent: %v %v", err, resp)
	}
	resp.Body.Close()
	e.do(nil, "GET", "/v1/agents", e.key, nil, 403, nil)
	var agents struct{ Data []app.Agent }
	e.do(nil, "GET", "/v1/agents", e.admin, nil, 200, &agents)
	var mine *app.Agent
	for i := range agents.Data {
		if agents.Data[i].Name == "my-agent" {
			mine = &agents.Data[i]
		}
	}
	if mine == nil || mine.PostsCreated != 1 || mine.Kind != "api" {
		t.Fatalf("agents: %+v", agents.Data)
	}
	e.do(nil, "DELETE", "/v1/agents/"+mine.ID, e.admin, nil, 204, nil)

	var ws app.Workspace
	e.do(nil, "PATCH", "/v1/workspace", e.key, map[string]any{"name": "Renamed"}, 403, nil)
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"name": "Renamed"}, 200, &ws)
	if ws.Name != "Renamed" {
		t.Fatalf("renamed: %+v", ws)
	}
}

var _ = httptest.NewServer

// Repeat: a published post with repeat_every gets its next occurrence once, one interval later, with its
// channels, texts and settings; failed posts and cleared repeats stop the chain.
func TestRepeatPostREST(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Tips")
	ch := e.customChannel("Bot")
	worker := e.keyWith(app.ScopePostsRead, app.ScopePostsPublish)

	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "x", "repeat_every": "weekly"}, 422, &errBody)
	if !strings.Contains(errBody.Error.Fields["repeat_every"], "1w") {
		t.Fatalf("bad repeat_every: %+v", errBody.Error)
	}
	draft := e.post(c.ID, map[string]any{"content": "x", "repeat_every": "2d"})
	if draft.RepeatEvery != "2d" {
		t.Fatalf("created: %+v", draft)
	}
	var got app.Post
	e.do(nil, "PATCH", "/v1/posts/"+draft.ID, e.key, map[string]any{"repeat_every": "1y"}, 422, &errBody)
	e.do(nil, "PATCH", "/v1/posts/"+draft.ID, e.key, map[string]any{"repeat_every": nil}, 200, &got)
	if got.RepeatEvery != "" {
		t.Fatalf("null clears: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/posts/"+draft.ID, e.key, map[string]any{"repeat_every": "1mo"}, 200, &got)
	e.do(nil, "PATCH", "/v1/posts/"+draft.ID, e.key, map[string]any{"content": "y"}, 200, &got)
	if got.RepeatEvery != "1mo" {
		t.Fatalf("an unrelated edit keeps the repeat: %+v", got)
	}

	// A weekly post goes out; its result is reported; the repeat job makes the next one.
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	text := "Bot text"
	post := e.post(c.ID, map[string]any{"content": "Weekly tip", "tags": []string{"tips"}, "repeat_every": "1w", "status": "scheduled",
		"publish_at": at.Format(time.RFC3339), "targets": []map[string]any{{"channel_id": ch.ID, "content": text, "settings": map[string]string{"k": "v"}}}})
	e.svc.Now = func() time.Time { return at.Add(time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	e.svc.RunDueJobsOnce(ctx) // publish_post
	e.svc.RunDueJobsOnce(ctx) // publish_target: the automation is asked
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "published"}, 200, &got)
	if got.Status != "published" {
		t.Fatalf("published: %+v", got)
	}
	e.svc.RunDueJobsOnce(ctx) // repeat_post
	copies := func() []app.Post {
		var page app.PostPage
		e.do(nil, "GET", "/v1/posts?status=scheduled&collection_id="+c.ID, e.key, nil, 200, &page)
		return page.Data
	}
	next := copies()
	if len(next) != 1 {
		t.Fatalf("one next occurrence: %+v", next)
	}
	n := next[0]
	want := at.AddDate(0, 0, 7)
	if n.PublishAt == nil || !strings.HasPrefix(*n.PublishAt, want.Format("2006-01-02T15:04")) || n.RepeatEvery != "1w" || n.Source != "repeat" ||
		n.Content != "Weekly tip" || len(n.Tags) != 1 || len(n.Targets) != 1 || n.Targets[0].Content == nil || *n.Targets[0].Content != text ||
		n.Targets[0].Settings["k"] != "v" {
		t.Fatalf("next occurrence: %+v", n)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?post_id="+n.ID, e.key, nil, 200, &evs)
	if len(evs.Data) < 2 || evs.Data[len(evs.Data)-1].Type != "post.created" || evs.Data[len(evs.Data)-1].Actor.Name != "Repeater" {
		t.Fatalf("copy history: %+v", evs.Data)
	}

	// Settling twice (the job queued again) still makes one copy.
	src, err := e.svc.Q.GetPostByID(ctx, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.QueueJob(ctx, src.WorkspaceID, "repeat_post", "repeat_post:"+post.ID, map[string]any{"post_id": post.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	e.svc.RunDueJobsOnce(ctx)
	if len(copies()) != 1 {
		t.Fatalf("idempotent: %+v", copies())
	}

	// A failed post does not repeat.
	failing := e.post(c.ID, map[string]any{"content": "fails", "repeat_every": "1d", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+failing.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+failing.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "failed", "error": "x"}, 200, nil)
	e.svc.RunDueJobsOnce(ctx)
	if len(copies()) != 1 {
		t.Fatalf("failed posts don't repeat: %+v", copies())
	}

	// Late: published 20 days after its time, a weekly post's next occurrence is the first week still ahead.
	late := e.post(c.ID, map[string]any{"content": "late", "repeat_every": "1w", "status": "scheduled", "publish_at": at.Add(time.Hour).Format(time.RFC3339),
		"targets": []map[string]any{{"channel_id": ch.ID}}})
	e.svc.Now = func() time.Time { return at.AddDate(0, 0, 20) }
	e.svc.RunDueJobsOnce(ctx)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "POST", "/v1/posts/"+late.ID+"/targets/"+ch.ID+"/result", worker, map[string]any{"status": "published"}, 200, nil)
	e.svc.RunDueJobsOnce(ctx)
	var page app.PostPage
	e.do(nil, "GET", "/v1/posts?status=scheduled&q=late", e.key, nil, 200, &page)
	if len(page.Data) != 1 || !strings.HasPrefix(*page.Data[0].PublishAt, at.Add(time.Hour).AddDate(0, 0, 21).Format("2006-01-02T15:04")) {
		t.Fatalf("catch-up: %+v", page.Data)
	}
}
