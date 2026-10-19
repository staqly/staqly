package httpapi_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// replier is a platform that takes replies: posts get id p<n>, replies r<n>; a reply whose text is "bad"
// is refused for good.
type replier struct {
	providers.Base
	mu      sync.Mutex
	parents []string
	n       int
}

func (r *replier) Connect(context.Context, map[string]string) (providers.Credentials, providers.Profile, error) {
	return providers.Credentials{"token": "t"}, providers.Profile{Name: "Thread test"}, nil
}

func (r *replier) Publish(context.Context, providers.Credentials, providers.PostContent, providers.Settings) (providers.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return providers.Result{ExternalID: "p1", URL: "https://social.test/p1"}, nil
}

func (r *replier) Reply(_ context.Context, _ providers.Credentials, parent string, p providers.PostContent, _ providers.Settings) (providers.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.Text == "bad" {
		return providers.Result{}, &providers.Failure{Kind: providers.ErrRejected, Msg: "refused"}
	}
	r.parents = append(r.parents, parent)
	id := "r" + string(rune('0'+len(r.parents)))
	return providers.Result{ExternalID: id, URL: "https://social.test/" + id}, nil
}

func withReplier(t *testing.T) *replier {
	t.Helper()
	r := &replier{Base: providers.NewBase("test_replier", "Thread test", providers.AuthToken, providers.Limits{MaxChars: 20, MaxMedia: 1})}
	providers.Register(r, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister("test_replier") })
	return r
}

func TestThreadValidation(t *testing.T) {
	withReplier(t)
	e := newEnv(t)
	c := e.collection("Threads")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_replier", "name": "Replies"}, 201, &ch)
	targets := []map[string]any{{"channel_id": ch.ID}}
	many := make([]map[string]any, 26)
	for i := range many {
		many[i] = map[string]any{"content": "x"}
	}
	for _, tc := range []struct {
		thread any
		field  string
	}{
		{many, "thread"},
		{[]map[string]any{{"content": "  "}}, "thread.0.content"},
		{[]map[string]any{{"content": "ok"}, {"content": "ok", "delay_seconds": 90000}}, "thread.1.delay_seconds"},
		{[]map[string]any{{"content": "this part is far too long for the channel"}}, "thread.0.content"},
		{[]map[string]any{{"content": "ok", "media": []string{"med_nope"}}}, "thread.0.media"},
	} {
		var out struct {
			Error struct{ Fields map[string]string }
		}
		e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "hi", "targets": targets, "thread": tc.thread}, 422, &out)
		if out.Error.Fields[tc.field] == "" {
			t.Errorf("want an error on %s, got %v", tc.field, out.Error.Fields)
		}
	}
	// Edit replaces the thread; [] clears it.
	p := e.post(c.ID, map[string]any{"content": "hi", "targets": targets, "thread": []map[string]any{{"content": "one", "delay_seconds": 30}}})
	if len(p.Thread) != 1 || p.Thread[0].Content != "one" || p.Thread[0].DelaySeconds != 30 || len(p.Targets[0].Replies) != 1 || p.Targets[0].Replies[0].Status != "pending" {
		t.Fatalf("created: %+v", p)
	}
	var got app.Post
	e.do(nil, "PATCH", "/v1/posts/"+p.ID, e.key, map[string]any{"thread": []map[string]any{{"content": "a"}, {"content": "b"}}}, 200, &got)
	if len(got.Thread) != 2 || got.Thread[1].Content != "b" {
		t.Fatalf("edited: %+v", got.Thread)
	}
	e.do(nil, "PATCH", "/v1/posts/"+p.ID, e.key, map[string]any{"thread": []any{}}, 200, &got)
	if len(got.Thread) != 0 || len(got.Targets[0].Replies) != 0 {
		t.Fatalf("cleared: %+v", got)
	}
	// A duplicate keeps the thread.
	e.do(nil, "PATCH", "/v1/posts/"+p.ID, e.key, map[string]any{"thread": []map[string]any{{"content": "kept"}}}, 200, nil)
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/duplicate", e.key, nil, 201, &got)
	if len(got.Thread) != 1 || got.Thread[0].Content != "kept" {
		t.Fatalf("duplicate: %+v", got.Thread)
	}
}

func TestThreadPublishesRepliesInOrder(t *testing.T) {
	r := withReplier(t)
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Threads")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_replier", "name": "Replies"}, 201, &ch)
	p := e.post(c.ID, map[string]any{"content": "post", "targets": []map[string]any{{"channel_id": ch.ID}},
		"thread": []map[string]any{{"content": "one"}, {"content": "two", "delay_seconds": 60}}})
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx) // the post, then the first reply (no delay); the second waits a minute
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
	rs := got.Targets[0].Replies
	if got.Status != "published" || len(rs) != 2 || rs[0].Status != "published" || rs[0].ExternalID != "r1" || rs[1].Status != "pending" {
		t.Fatalf("after the first reply: %+v", got.Targets[0])
	}
	// Editing a published post's thread is refused like its text.
	e.do(nil, "PATCH", "/v1/posts/"+p.ID, e.key, map[string]any{"thread": []any{}}, 409, nil)

	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
	rs = got.Targets[0].Replies
	if rs[1].Status != "published" || rs[1].ExternalURL != "https://social.test/r2" || rs[1].PublishedAt == nil {
		t.Fatalf("after the second reply: %+v", rs)
	}
	if strings.Join(r.parents, ",") != "p1,r1" {
		t.Fatalf("each part replies to the one before: %v", r.parents)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?post_id="+p.ID+"&type=post.reply_published", e.key, nil, 200, &evs)
	if len(evs.Data) != 2 {
		t.Fatalf("reply events: %d", len(evs.Data))
	}
}

func TestThreadFailureStopsTheRest(t *testing.T) {
	withReplier(t)
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Threads")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_replier", "name": "Replies"}, 201, &ch)
	p := e.post(c.ID, map[string]any{"content": "post", "targets": []map[string]any{{"channel_id": ch.ID}},
		"thread": []map[string]any{{"content": "bad"}, {"content": "never"}}})
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
	rs := got.Targets[0].Replies
	if got.Status != "published" || got.Targets[0].Status != "published" || rs[0].Status != "failed" || rs[0].Error == "" || rs[1].Status != "skipped" {
		t.Fatalf("a refused reply ends the thread, the post stays out: %+v", got)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?post_id="+p.ID+"&type=post.reply_failed", e.key, nil, 200, &evs)
	if len(evs.Data) != 1 {
		t.Fatalf("reply_failed events: %d", len(evs.Data))
	}
}

// Your own automation gets the thread with the request and publishes it itself: its replies are skipped.
func TestThreadOnYourOwnAutomation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Threads")
	ch := e.customChannel("Worker")
	p := e.post(c.ID, map[string]any{"content": "post", "targets": []map[string]any{{"channel_id": ch.ID}},
		"thread": []map[string]any{{"content": "one", "delay_seconds": 5}}})
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	var evs struct {
		Data []struct {
			Data struct {
				Target struct {
					Thread []app.ThreadPartOut `json:"thread"`
				} `json:"target"`
			} `json:"data"`
		} `json:"data"`
	}
	e.do(nil, "GET", "/v1/events?post_id="+p.ID+"&type=post.publish_requested", e.key, nil, 200, &evs)
	if len(evs.Data) != 1 || len(evs.Data[0].Data.Target.Thread) != 1 || evs.Data[0].Data.Target.Thread[0].DelaySeconds != 5 {
		t.Fatalf("the request carries the thread: %+v", evs.Data)
	}
	var got app.Post
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/targets/"+ch.ID+"/result", e.key, map[string]any{"status": "published", "external_id": "x"}, 200, &got)
	if rs := got.Targets[0].Replies; len(rs) != 1 || rs[0].Status != "skipped" {
		t.Fatalf("replies on your own automation: %+v", rs)
	}
}

// reposter is a replier whose posts other accounts on it can repost, with a last-comment setting.
type reposter struct {
	replier
	reposts []string // "<channel token>:<external id>"
}

func (r *reposter) Repost(_ context.Context, c providers.Credentials, externalID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reposts = append(r.reposts, c["token"]+":"+externalID)
	return nil
}

// A channel's last comment closes its thread; the channels named in its reposters setting repost the post once
// it is out, and only channels of the same platform qualify.
func TestThreadFinisherAndReposts(t *testing.T) {
	r := &reposter{replier: replier{Base: providers.NewBase("test_reposter", "Repost test", providers.AuthToken, providers.Limits{MaxChars: 50, MaxMedia: 1}).
		WithSettings(
			providers.Field{Key: "thread_finisher", Label: "Last comment", Kind: "textarea", MaxLength: 50},
			providers.Field{Key: "reposters", Label: "Repost from", Kind: "channels", MaxItems: 3},
		)}}
	providers.Register(r, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister("test_reposter") })
	withReplier(t)
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Reposts")
	var main, second, other app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_reposter", "name": "Main"}, 201, &main)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_reposter", "name": "Second"}, 201, &second)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_replier", "name": "Elsewhere"}, 201, &other)

	// Reposters must be other channels of the same platform.
	for _, by := range []string{main.ID, other.ID, "ch_nope"} {
		var out struct {
			Error struct{ Fields map[string]string }
		}
		e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "hi",
			"targets": []map[string]any{{"channel_id": main.ID, "settings": map[string]string{"reposters": by}}}}, 422, &out)
		if out.Error.Fields["targets."+main.ID+".settings.reposters"] == "" {
			t.Errorf("reposter %s: want an error, got %v", by, out.Error.Fields)
		}
	}

	p := e.post(c.ID, map[string]any{"content": "post", "thread": []map[string]any{{"content": "one"}},
		"targets": []map[string]any{{"channel_id": main.ID, "settings": map[string]string{"thread_finisher": "Follow for more", "reposters": second.ID}}}})
	if len(p.Targets[0].Replies) != 2 {
		t.Fatalf("the last comment counts as a reply: %+v", p.Targets[0])
	}
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
	for range 3 {
		e.svc.RunDueJobsOnce(ctx)
	}
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
	rs := got.Targets[0].Replies
	if got.Status != "published" || len(rs) != 2 || rs[0].Status != "published" || rs[1].Status != "published" {
		t.Fatalf("thread with its last comment: %+v", got.Targets[0])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.Join(r.parents, ",") != "p1,r1" || len(r.reposts) != 1 || !strings.HasSuffix(r.reposts[0], ":p1") {
		t.Fatalf("replies under %v, reposts %v", r.parents, r.reposts)
	}
}
