package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeMetrics is a token platform that reports numbers for what it published and counts how often it was asked.
type fakeMetrics struct {
	providers.Base
	reads atomic.Int32
	fail  atomic.Bool
}

func (f *fakeMetrics) CredentialFields() []providers.Field {
	return []providers.Field{{Key: "token", Label: "Token", Kind: "secret", Required: true}}
}

func (f *fakeMetrics) Connect(_ context.Context, fields map[string]string) (providers.Credentials, providers.Profile, error) {
	return providers.Credentials{"token": fields["token"]}, providers.Profile{Handle: "@numbers", Name: "Numbers"}, nil
}

func (f *fakeMetrics) Publish(context.Context, providers.Credentials, providers.PostContent, providers.Settings) (providers.Result, error) {
	return providers.Result{ExternalID: "ext_m", URL: "https://metrics.example/p/1"}, nil
}

func (f *fakeMetrics) Metrics(_ context.Context, c providers.Credentials, externalID string) (providers.Metrics, error) {
	f.reads.Add(1)
	if f.fail.Load() || c["token"] != "good" || externalID != "ext_m" {
		return providers.Metrics{}, errors.New("platform down")
	}
	views, likes := int64(1200), int64(34)
	return providers.Metrics{Impressions: &views, Likes: &likes}, nil
}

var fakeMet = &fakeMetrics{Base: providers.NewBase("fakemetrics", "Fake Metrics", providers.AuthToken, providers.Limits{MaxChars: 500})}

func init() { providers.Register(fakeMet, providers.StatusAvailable) }

func previewToken(t *testing.T, link app.PreviewLink) string {
	t.Helper()
	i := strings.Index(link.URL, "/preview#")
	if i < 0 || link.Token == "" || link.URL[i+len("/preview#"):] != link.Token {
		t.Fatalf("preview url: %+v", link)
	}
	return link.Token
}

// A preview link opens the post as each channel shows it, without signing in; reviewers comment there, the team
// sees the comments (with emails) on the post; revoked or expired links stop working.
func TestPreviewLinksAndComments(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Launch")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &ch)
	var pic, unrelated app.Media
	e.upload("/v1/media", e.key, nil, "launch.png", pngBytes, 201, &pic)
	e.upload("/v1/media", e.key, nil, "unrelated.png", pngBytes, 201, &unrelated)
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "v1.4 is out", "media": []string{pic.ID},
		"tags": []string{"launch"}, "targets": []map[string]any{{"channel_id": ch.ID, "content": "v1.4!"}}}, 201, &post)

	e.do(nil, "POST", "/v1/posts/"+post.ID+"/previews", e.key, map[string]any{"expires_in_days": 400}, 422, nil)
	var link app.PreviewLink
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/previews", e.key, map[string]any{"expires_in_days": 7}, 201, &link)
	token := previewToken(t, link)
	if !link.Active || link.ExpiresAt == nil {
		t.Fatalf("new link: %+v", link)
	}
	var links struct{ Data []app.PreviewLink }
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/previews", e.key, nil, 200, &links)
	if len(links.Data) != 1 || links.Data[0].Token != "" || links.Data[0].URL != "" {
		t.Fatalf("links list must not repeat the token: %+v", links.Data)
	}

	// Public: no credentials.
	var pv app.PublicPreview
	e.do(nil, "GET", "/v1/previews/"+token, "", nil, 200, &pv)
	if pv.Content != "v1.4 is out" || len(pv.Targets) != 1 || pv.Targets[0].Content != "v1.4!" || pv.Targets[0].Handle != "@fake" ||
		pv.Targets[0].ProviderName != "Fake Token" || pv.Workspace == "" || pv.Targets[0].Hashtags != "#launch" {
		t.Fatalf("preview: %+v", pv)
	}
	e.do(nil, "GET", "/v1/previews/not-a-token", "", nil, 404, nil)
	// Its own pictures load without credentials; other media of the workspace does not.
	if res := e.do(nil, "GET", "/v1/previews/"+token+"/media/"+pic.ID, "", nil, 200, nil); res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("preview media type: %s", res.Header.Get("Content-Type"))
	}
	e.do(nil, "GET", "/v1/previews/"+token+"/media/"+unrelated.ID, "", nil, 404, nil)
	if len(pv.Media) != 1 || pv.Media[0].ID != pic.ID {
		t.Fatalf("preview media: %+v", pv.Media)
	}

	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/previews/"+token+"/comments", "", map[string]any{"body": "nice"}, 422, &errBody)
	if errBody.Error.Fields["name"] == "" {
		t.Fatalf("name required: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/previews/"+token+"/comments", "", map[string]any{"name": "Ana", "email": "nope", "body": "x"}, 422, nil)
	var cm app.PostComment
	e.do(nil, "POST", "/v1/previews/"+token+"/comments", "", map[string]any{"name": "Ana", "email": "ana@example.com",
		"body": "Shorter, please", "quote": "v1.4!", "channel_id": ch.ID}, 201, &cm)
	if cm.AuthorEmail != "" || cm.ChannelID != ch.ID || cm.Quote != "v1.4!" {
		t.Fatalf("public comment answer: %+v", cm)
	}
	e.do(nil, "GET", "/v1/previews/"+token, "", nil, 200, &pv)
	if len(pv.Comments) != 1 || pv.Comments[0].AuthorEmail != "" {
		t.Fatalf("preview comments hide emails: %+v", pv.Comments)
	}
	var comments struct{ Data []app.PostComment }
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/comments", e.key, nil, 200, &comments)
	if len(comments.Data) != 1 || comments.Data[0].AuthorEmail != "ana@example.com" || comments.Data[0].Body != "Shorter, please" {
		t.Fatalf("team comments: %+v", comments.Data)
	}
	var events struct{ Data []app.Event }
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/events", e.key, nil, 200, &events)
	seen := map[string]bool{}
	for _, ev := range events.Data {
		seen[ev.Type] = true
	}
	if !seen["post.preview_shared"] || !seen["post.feedback_added"] {
		t.Fatalf("events: %+v", seen)
	}

	// Another workspace can't see or revoke them.
	other := newEnv(t)
	other.do(nil, "GET", "/v1/posts/"+post.ID+"/comments", other.key, nil, 404, nil)
	other.do(nil, "DELETE", "/v1/posts/"+post.ID+"/previews/"+link.ID, other.key, nil, 404, nil)

	e.do(nil, "GET", "/v1/posts/"+post.ID+"/comments", e.key, nil, 200, &comments)
	other.do(nil, "DELETE", "/v1/posts/"+post.ID+"/comments/"+comments.Data[0].ID, other.key, nil, 404, nil)
	e.do(nil, "DELETE", "/v1/posts/"+post.ID+"/comments/"+comments.Data[0].ID, e.key, nil, 204, nil)

	e.do(nil, "DELETE", "/v1/posts/"+post.ID+"/previews/"+link.ID, e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/previews/"+token, "", nil, 404, nil)
	e.do(nil, "POST", "/v1/previews/"+token+"/comments", "", map[string]any{"name": "Ana", "body": "still?"}, 404, nil)
}

// Comments on a passage: a reviewer marks words in a channel's version or in a comment of the thread, others
// reply, the team replies as itself and resolves; offsets are checked against the text and follow it when it
// changes; a passage that's gone is marked as changed. Another workspace can't touch the threads.
func TestPreviewCommentThreads(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Threads")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "fakemetrics", "fields": map[string]string{"token": "good"}}, 201, &ch)
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "Our café opens Monday",
		"thread":  []map[string]any{{"content": "Doors at 9, coffee on us"}},
		"targets": []map[string]any{{"channel_id": ch.ID}}}, 201, &post)
	var link app.PreviewLink
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/previews", e.key, nil, 201, &link)
	tok := link.Token

	// "opens" starts after "Our café " (9 UTF-16 units: é is one).
	var root app.PostComment
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ana", "body": "Say the date",
		"quote": "opens Monday", "channel_id": ch.ID, "part": 0, "anchor_start": 9, "anchor_end": 21}, 201, &root)
	if root.AnchorStart != 9 || root.AnchorEnd != 21 || root.Part != 0 || root.AuthorKind != "reviewer" || root.TextChanged {
		t.Fatalf("anchored comment: %+v", root)
	}
	// Wrong offsets are not trusted: the words are found where they are.
	var onThread app.PostComment
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ben", "body": "Which coffee?",
		"quote": "coffee", "channel_id": ch.ID, "part": 1, "anchor_start": 999, "anchor_end": -4}, 201, &onThread)
	if onThread.Part != 1 || onThread.AnchorStart != 12 || onThread.AnchorEnd != 18 {
		t.Fatalf("re-found passage: %+v", onThread)
	}
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ana", "body": "x", "quote": "not there"}, 422, &errBody)
	if errBody.Error.Fields["quote"] == "" {
		t.Fatalf("a passage must be in the post: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ana", "body": "x", "quote": "coffee", "part": 7}, 422, &errBody)
	if errBody.Error.Fields["part"] == "" {
		t.Fatalf("no such part: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ana", "body": "x", "quote": strings.Repeat("a", 501)}, 422, &errBody)
	if errBody.Error.Fields["quote"] == "" {
		t.Fatalf("quote length: %+v", errBody.Error)
	}

	// Replies: a reviewer, then the team (as itself); a reply to a reply joins the same thread.
	var reply, teamReply app.PostComment
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ben", "body": "Agreed", "parent_id": root.ID}, 201, &reply)
	if reply.ParentID != root.ID || reply.Quote != "" {
		t.Fatalf("reply: %+v", reply)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/comments", e.key, map[string]any{"body": "Done, thanks", "parent_id": reply.ID}, 201, &teamReply)
	if teamReply.ParentID != root.ID || teamReply.AuthorKind != "team" || teamReply.AuthorName == "" {
		t.Fatalf("team reply: %+v", teamReply)
	}
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments", "", map[string]any{"name": "Ben", "body": "?", "parent_id": "cmt_nope"}, 422, nil)

	// Resolve and reopen: reviewers by name, the team as itself; only a thread's first comment.
	var resolved app.PostComment
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments/"+root.ID+"/resolve", "", map[string]any{}, 422, nil)
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments/"+root.ID+"/resolve", "", map[string]any{"name": "Ana"}, 200, &resolved)
	if resolved.ResolvedAt == nil || resolved.ResolvedBy != "Ana" {
		t.Fatalf("resolved: %+v", resolved)
	}
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments/"+reply.ID+"/resolve", "", map[string]any{"name": "Ana"}, 404, nil)
	e.do(nil, "POST", "/v1/previews/"+tok+"/comments/"+root.ID+"/reopen", "", nil, 200, &resolved)
	if resolved.ResolvedAt != nil {
		t.Fatalf("reopened on the preview: %+v", resolved)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/comments/"+root.ID+"/resolve", e.key, nil, 200, &resolved)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/comments/"+root.ID+"/reopen", e.key, nil, 200, &resolved)
	if resolved.ResolvedAt != nil || resolved.ResolvedBy != "" {
		t.Fatalf("reopened: %+v", resolved)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/comments/"+onThread.ID+"/resolve", e.key, nil, 200, &resolved)

	// The text changes: a moved passage follows, a removed one is marked as changed.
	e.do(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"content": "Big news: our café opens Monday",
		"thread": []map[string]any{{"content": "Doors at 9"}}}, 200, nil)
	var pv app.PublicPreview
	e.do(nil, "GET", "/v1/previews/"+tok, "", nil, 200, &pv)
	got := map[string]app.PostComment{}
	for _, cm := range pv.Comments {
		got[cm.ID] = cm
	}
	if r := got[root.ID]; r.AnchorStart != 19 || r.AnchorEnd != 31 || r.TextChanged {
		t.Fatalf("moved passage: %+v", r)
	}
	if r := got[onThread.ID]; !r.TextChanged || r.AnchorStart != -1 || r.Quote != "coffee" || r.ResolvedAt == nil {
		t.Fatalf("removed passage: %+v", r)
	}
	if len(pv.Comments) != 4 {
		t.Fatalf("comments: %d", len(pv.Comments))
	}

	// Another workspace can't reply, resolve or read.
	other := newEnv(t)
	other.do(nil, "POST", "/v1/posts/"+post.ID+"/comments", other.key, map[string]any{"body": "hi", "parent_id": root.ID}, 404, nil)
	other.do(nil, "POST", "/v1/posts/"+post.ID+"/comments/"+root.ID+"/resolve", other.key, nil, 404, nil)

	// Removing a thread's first comment removes its replies.
	e.do(nil, "DELETE", "/v1/posts/"+post.ID+"/comments/"+root.ID, e.key, nil, 204, nil)
	var comments struct{ Data []app.PostComment }
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/comments", e.key, nil, 200, &comments)
	if len(comments.Data) != 1 || comments.Data[0].ID != onThread.ID {
		t.Fatalf("after removing the thread: %+v", comments.Data)
	}
}

// Reviewers are limited per client address.
func TestPreviewCommentRateLimit(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Limits")
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "hello"}, 201, &post)
	var link app.PreviewLink
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/previews", e.key, nil, 201, &link)
	limited := 0
	for range 25 {
		res := e.do(nil, "POST", "/v1/previews/"+link.Token+"/comments", "", map[string]any{"name": "Bot", "body": "spam"}, 0, nil)
		if res.StatusCode == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("25 comments from one address were all accepted")
	}
}

// Numbers come from the platform on demand, are cached for a while, refresh on request; your own automation
// sends its own; platforms without numbers say so.
func TestPostMetrics(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Numbers")
	var ch, tok, own app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "fakemetrics", "fields": map[string]string{"token": "good"}}, 201, &ch)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &tok)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": "My n8n"}, 201, &own)
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "numbers",
		"targets": []map[string]any{{"channel_id": ch.ID}, {"channel_id": tok.ID}, {"channel_id": own.ID}}}, 201, &post)

	byChannel := func(out []app.TargetMetrics) map[string]app.TargetMetrics {
		m := map[string]app.TargetMetrics{}
		for _, x := range out {
			m[x.ChannelID] = x
		}
		return m
	}
	var res struct{ Data []app.TargetMetrics }
	// Not published yet: nothing to read.
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/metrics", e.key, nil, 200, &res)
	if fakeMet.reads.Load() != 0 || len(res.Data) != 3 {
		t.Fatalf("unpublished: reads %d, %+v", fakeMet.reads.Load(), res.Data)
	}
	if _, err := e.svc.DB.Exec(`UPDATE post_targets SET status = 'published', external_id = 'ext_m' WHERE post_id = ? AND channel_id = ?`, post.ID, ch.ID); err != nil {
		t.Fatal(err)
	}
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/metrics", e.key, nil, 200, &res)
	got := byChannel(res.Data)
	if m := got[ch.ID]; !m.Available || m.Metrics == nil || *m.Metrics.Impressions != 1200 || *m.Metrics.Likes != 34 || m.Metrics.Reposts != nil || m.UpdatedAt == nil {
		t.Fatalf("read numbers: %+v", got[ch.ID])
	}
	if got[tok.ID].Available || got[tok.ID].Metrics != nil || !got[own.ID].Available {
		t.Fatalf("availability: %+v", got)
	}
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/metrics", e.key, nil, 200, &res)
	if fakeMet.reads.Load() != 1 {
		t.Fatalf("cached numbers read again: %d", fakeMet.reads.Load())
	}
	fakeMet.fail.Store(true)
	defer fakeMet.fail.Store(false)
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/metrics?refresh=true", e.key, nil, 200, &res)
	if m := byChannel(res.Data)[ch.ID]; fakeMet.reads.Load() != 2 || m.Error == "" || m.Metrics == nil || *m.Metrics.Impressions != 1200 {
		t.Fatalf("a failed refresh keeps the last numbers and says why: %+v", m)
	}

	// Your own automation reports its numbers; other platforms can't be written (409, as for a reported result).
	e.do(nil, "PUT", "/v1/posts/"+post.ID+"/targets/"+tok.ID+"/metrics", e.key, map[string]any{"likes": 3}, 409, nil)
	e.do(nil, "PUT", "/v1/posts/"+post.ID+"/targets/"+own.ID+"/metrics", e.key, map[string]any{"likes": -1}, 422, nil)
	var rep app.TargetMetrics
	e.do(nil, "PUT", "/v1/posts/"+post.ID+"/targets/"+own.ID+"/metrics", e.key, map[string]any{"impressions": 50, "link_clicks": 4}, 200, &rep)
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/metrics", e.key, nil, 200, &res)
	if m := byChannel(res.Data)[own.ID]; m.Metrics == nil || *m.Metrics.LinkClicks != 4 || *m.Metrics.Impressions != 50 {
		t.Fatalf("reported numbers: %+v", m)
	}
}

// Duplicating copies the post as a draft: content, per-channel text and settings, tags, thread; not its schedule
// or published state.
func TestDuplicatePost(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Copies")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &ch)
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "original", "tags": []string{"launch"},
		"targets": []map[string]any{{"channel_id": ch.ID, "content": "short"}}, "publish_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), "status": "scheduled"}, 201, &post)
	var cp app.Post
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/duplicate", e.key, nil, 201, &cp)
	if cp.ID == post.ID || cp.Status != "draft" || cp.PublishAt != nil || cp.Content != "original" || len(cp.Tags) != 1 ||
		len(cp.Targets) != 1 || cp.Targets[0].Content == nil || *cp.Targets[0].Content != "short" || cp.Targets[0].Status != "pending" {
		t.Fatalf("duplicate: %+v", cp)
	}
}
