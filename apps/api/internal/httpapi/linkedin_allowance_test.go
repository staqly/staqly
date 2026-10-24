package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// withFakeLinkedIn puts fake platforms in place of LinkedIn's profile and Page for one test (comments go through
// Reply, as LinkedIn's do), then restores them.
func withFakeLinkedIn(t *testing.T) *replier {
	t.Helper()
	r := &replier{Base: providers.NewBase("linkedin", "LinkedIn", providers.AuthToken,
		providers.Limits{MaxChars: 3000, MaxMedia: 9, MediaKinds: []string{"image", "gif", "video"}})}
	for _, id := range []string{"linkedin", "linkedin_page"} {
		orig, _ := providers.Get(id)
		providers.Register(&namedReplier{replier: r, id: id}, providers.StatusAvailable)
		t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
	}
	return r
}

// namedReplier is one replier registered under another provider id.
type namedReplier struct {
	*replier
	id string
}

func (n *namedReplier) ID() string { return n.id }

// counts: the comments and the posts published so far.
func (r *replier) counts() (comments, posts int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.parents), r.n
}

// Posts to LinkedIn are counted per workspace and month: every post to a profile or a Page counts once on the
// posts allowance, one with a video once more on the video allowance; comments don't count. Over either, the
// post is refused early (402) and a target that gets past that fails before LinkedIn is called.
func TestLinkedInAllowances(t *testing.T) {
	fake := withFakeLinkedIn(t)
	e := newEnv(t)
	ctx := context.Background()
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if err := e.svc.SetPlan(ctx, ws.ID, "starter", time.Now().Add(20*24*time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	c := e.collection("LinkedIn")
	var profile, page app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "linkedin", "name": "Jane Doe"}, 201, &profile)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "linkedin_page", "name": "Example Inc"}, 201, &page)
	both := []map[string]any{{"channel_id": profile.ID}, {"channel_id": page.ID}}
	period := time.Now().UTC().Format("2006-01")
	seed := func(meter string, n int) {
		t.Helper()
		if _, err := e.svc.DB.Exec(`DELETE FROM usage_events WHERE workspace_id = ? AND provider = ?`, ws.ID, meter); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
		if _, err := e.svc.DB.Exec(`INSERT INTO usage_events (workspace_id, provider, ref, period, units, included, credits, overage, created_at)
			VALUES (?, ?, 'seed', ?, ?, ?, 0, 0, ?)`, ws.ID, meter, period, n, n, time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")); err != nil {
			t.Fatal(err)
		}
	}
	usage := func() (posts, videos int64, u struct {
		Plan  app.Plan
		Usage map[string]int64
	}) {
		t.Helper()
		e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
		return u.Usage["linkedin_posts_per_period"], u.Usage["linkedin_videos_per_period"], u
	}
	if _, _, u := usage(); u.Plan.Limits.LinkedInPostsPerPeriod != 300 || u.Plan.Limits.LinkedInVideosPerPeriod != 30 {
		t.Fatalf("starter limits: %+v", u.Plan.Limits)
	}

	// Under: 298 of 300 used, a post with a comment to the profile and the Page takes the last two. The comments
	// are published and not counted; running the jobs again counts nothing twice.
	seed(app.MeterLinkedIn, 298)
	p := e.post(c.ID, map[string]any{"content": "hello", "targets": both, "thread": []map[string]any{{"content": "and more"}}})
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &p)
	if comments, _ := fake.counts(); p.Status != "published" || comments != 2 {
		t.Fatalf("published with comments: %s, %d comments", p.Status, comments)
	}
	if posts, videos, _ := usage(); posts != 300 || videos != 0 {
		t.Fatalf("at the allowance: %d posts, %d videos", posts, videos)
	}

	// At the allowance: refused before anything is queued, also when scheduled this month; next month is fine.
	p2 := e.post(c.ID, map[string]any{"content": "one more", "targets": []map[string]any{{"channel_id": page.ID}}})
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/publish", e.key, nil, 402, &errBody)
	if errBody.Error.Code != "linkedin_posts_exhausted" ||
		!strings.HasPrefix(errBody.Error.Message, "Your LinkedIn posts for this period are used up. Wait until ") {
		t.Fatalf("exhausted: %+v", errBody.Error)
	}
	soon := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/schedule", e.key, map[string]any{"publish_at": soon}, 402, nil)
	nextMonth := time.Date(time.Now().Year(), time.Now().Month()+1, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/schedule", e.key, map[string]any{"publish_at": nextMonth}, 200, nil)

	// Over at publish time (the room went while it was queued): the target fails with the reason, LinkedIn isn't called.
	seed(app.MeterLinkedIn, 0)
	p3 := e.post(c.ID, map[string]any{"content": "queued", "targets": []map[string]any{{"channel_id": profile.ID}}})
	e.do(nil, "POST", "/v1/posts/"+p3.ID+"/publish", e.key, nil, 202, nil)
	seed(app.MeterLinkedIn, 300)
	_, before := fake.counts()
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+p3.ID, e.key, nil, 200, &p3)
	if _, after := fake.counts(); p3.Status != "failed" || !strings.Contains(p3.Targets[0].Error, "LinkedIn posts for this period are used up") || after != before {
		t.Fatalf("at publish time: %+v (%d calls)", p3, after-before)
	}

	// A video counts against both: 29 of 30 videos used, one post with a video fits, the next one doesn't, a post
	// without a video still does.
	seed(app.MeterLinkedIn, 0)
	seed(app.MeterLinkedInVideo, 29)
	var clip app.Media
	e.upload("/v1/media", e.key, nil, "clip.mp4", append([]byte("\x00\x00\x00\x18ftypmp42"), make([]byte, 64)...), 201, &clip)
	v := e.post(c.ID, map[string]any{"content": "watch", "media": []string{clip.ID}, "targets": []map[string]any{{"channel_id": profile.ID}}})
	e.do(nil, "POST", "/v1/posts/"+v.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if posts, videos, _ := usage(); posts != 1 || videos != 30 {
		t.Fatalf("a video counts on both: %d posts, %d videos", posts, videos)
	}
	v2 := e.post(c.ID, map[string]any{"content": "again", "media": []string{clip.ID}, "targets": []map[string]any{{"channel_id": page.ID}}})
	e.do(nil, "POST", "/v1/posts/"+v2.ID+"/publish", e.key, nil, 402, &errBody)
	if errBody.Error.Code != "linkedin_videos_exhausted" ||
		!strings.HasPrefix(errBody.Error.Message, "Your LinkedIn videos for this period are used up. Post without a video or wait until ") {
		t.Fatalf("videos exhausted: %+v", errBody.Error)
	}
	text := e.post(c.ID, map[string]any{"content": "words only", "targets": []map[string]any{{"channel_id": page.ID}}})
	e.do(nil, "POST", "/v1/posts/"+text.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if posts, videos, _ := usage(); posts != 2 || videos != 30 {
		t.Fatalf("a post without a video: %d posts, %d videos", posts, videos)
	}

	// An entitlement raises the allowance (staqly_limit_linkedin_videos_per_period), and the check uses it.
	stripe := httptest.NewServer(http.NotFoundHandler()) // prices fall back to the built-in ones
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_linkedin", WebhookSecret: testWhsec, APIBase: stripe.URL}
	if _, err := e.svc.DB.Exec(`UPDATE workspaces SET entitlements = ? WHERE id = ?`,
		`[{"lookup_key":"linkedin_videos_extra","name":"LinkedIn videos","metadata":{"staqly_limit_linkedin_videos_per_period":"+10"}}]`, ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, u := usage(); u.Plan.Limits.LinkedInVideosPerPeriod != 40 {
		t.Fatalf("with the entitlement: %+v", u.Plan.Limits)
	}
	e.do(nil, "POST", "/v1/posts/"+v2.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if posts, videos, _ := usage(); posts != 3 || videos != 31 {
		t.Fatalf("over the plan's allowance with the entitlement: %d posts, %d videos", posts, videos)
	}

	// X's meter is its own.
	var x int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE workspace_id = ? AND provider = 'x'`, ws.ID).Scan(&x)
	if x != 0 {
		t.Fatalf("x usage rows: %d", x)
	}
}

// A local install has no LinkedIn allowance: nothing is refused or counted.
func TestLinkedInUnlimitedLocally(t *testing.T) {
	withFakeLinkedIn(t)
	svc, srv := newLocal(t)
	p := mustLocal(t, svc)
	ctx := context.Background()
	ch, err := svc.ConnectChannel(ctx, p, app.ChannelInput{Provider: "linkedin", Name: "Jane Doe"})
	if err != nil {
		t.Fatal(err)
	}
	col, err := svc.CreateCollection(ctx, p, app.CollectionInput{Name: ptrTo("LinkedIn")})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := svc.CreatePost(ctx, p, app.PostInput{Collection: col.ID, Content: "hello", PublishNow: true,
			Targets: []app.PostTargetInput{{ChannelID: ch.ID}}}); err != nil {
			t.Fatal(err)
		}
	}
	svc.RunDueJobsOnce(ctx)
	var rows int
	svc.DB.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&rows)
	code, body := localReq(t, srv, "GET", "/v1/workspace/usage", "", "", "")
	if rows != 0 || code != 200 || !strings.Contains(body, `"linkedin_posts_per_period":-1`) || !strings.Contains(body, `"linkedin_videos_per_period":-1`) {
		t.Fatalf("local: %d usage rows, %d %s", rows, code, body)
	}
}
