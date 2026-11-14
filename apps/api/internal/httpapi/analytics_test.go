package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The analytics endpoint aggregates the workspace's own targets (SQL on SQLite, and on PostgreSQL with
// STAQLY_TEST_DATABASE_URL): outcomes, delay, sources, time zone buckets, cached numbers, failures, filters.
func TestAnalytics(t *testing.T) {
	e := newEnv(t)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	e.svc.Now = func() time.Time { return now }
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"timezone": "Europe/Warsaw"}, 200, nil)
	c := e.collection("Numbers")
	other := e.collection("Other")
	var met, tok, own app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "fakemetrics", "fields": map[string]string{"token": "good"}}, 201, &met)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &tok)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": "My n8n"}, 201, &own)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.svc.DB.Exec(q, args...); err != nil {
			t.Fatal(q, err)
		}
	}
	post := func(source, status, publishAt string, channels ...string) string {
		t.Helper()
		targets := []map[string]any{}
		for _, ch := range channels {
			targets = append(targets, map[string]any{"channel_id": ch})
		}
		var p app.Post
		e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"title": "Post", "collection": c.ID, "content": "Hi\n  there", "targets": targets}, 201, &p)
		exec(`UPDATE posts SET source = ?, status = ?, publish_at = ?, created_at = ? WHERE id = ?`, source, status, nullable(publishAt), "2026-10-02T10:00:00.000000Z", p.ID)
		return p.ID
	}
	target := func(postID, channelID, status, at string, attempts int, errMsg, metrics string) {
		t.Helper()
		published := any(nil)
		if status == "published" {
			published = at
		}
		exec(`UPDATE post_targets SET status = ?, published_at = ?, updated_at = ?, attempts = ?, error = ?, metrics = ?, external_url = ?
			WHERE post_id = ? AND channel_id = ?`, status, published, at, attempts, errMsg, metrics, "https://example.com/"+postID, postID, channelID)
	}
	exec(`INSERT INTO agents (id, workspace_id, kind, name, principal_id, first_seen_at, last_seen_at) VALUES ('agt_cc', ?, 'mcp', 'Claude Code', 'k', ?, ?)`,
		workspaceID(e), "2026-10-01T00:00:00.000000Z", "2026-10-01T00:00:00.000000Z")

	// Tuesday 2026-10-20 08:00 UTC is 10:00 in Warsaw (summer time).
	p1 := post("dashboard", "published", "2026-10-20T08:00:00.000000Z", met.ID, own.ID)
	target(p1, met.ID, "published", "2026-10-20T08:00:30.000000Z", 1, "", `{"impressions":1000,"likes":10,"replies":2}`)
	target(p1, own.ID, "published", "2026-10-20T08:02:00.000000Z", 2, "", `{"likes":5}`)
	p2 := post("Claude Code", "failed", "2026-10-21T10:00:00.000000Z", met.ID, tok.ID)
	target(p2, met.ID, "failed", "2026-10-21T10:00:00.000000Z", 3, "rate limit hit, retry after 30 seconds", "")
	target(p2, tok.ID, "published", "2026-10-21T10:00:05.000000Z", 1, "", "")
	// 23:30 UTC on 10-22 is 01:30 on 10-23 in Warsaw: the failure counts on the 23rd.
	p3 := post("schedule:Daily", "failed", "2026-10-22T23:30:00.000000Z", tok.ID)
	target(p3, tok.ID, "failed", "2026-10-22T23:30:00.000000Z", 1, "rate limit hit, retry after 12 seconds", "")
	// The previous period (2026-09-04 to 2026-09-30) and long before.
	p4 := post("dashboard", "published", "2026-09-20T08:00:00.000000Z", met.ID)
	target(p4, met.ID, "published", "2026-09-20T08:00:00.000000Z", 1, "", "")
	p5 := post("dashboard", "published", "2026-01-01T08:00:00.000000Z", met.ID)
	target(p5, met.ID, "published", "2026-01-01T08:00:00.000000Z", 1, "", "")
	// What waits.
	post("dashboard", "draft", "", met.ID)
	post("dashboard", "scheduled", "2026-11-01T09:00:00.000000Z", met.ID)

	// Feedback: a thread opened and resolved today.
	var root app.PostComment
	e.do(nil, "POST", "/v1/posts/"+p1+"/comments", e.key, map[string]any{"body": "Looks good"}, 201, &root)
	e.do(nil, "POST", "/v1/posts/"+p1+"/comments/"+root.ID+"/resolve", e.key, nil, 200, nil)

	var r app.AnalyticsReport
	e.do(nil, "GET", "/v1/analytics?from=2026-10-01&to=2026-10-27", e.key, nil, 200, &r)
	if r.Range.Days != 27 || r.Range.Timezone != "Europe/Warsaw" || r.Range.PreviousFrom != "2026-09-04" || r.Range.PreviousTo != "2026-09-30" ||
		r.Range.Start != "2026-09-30T22:00:00.000000Z" {
		t.Fatalf("range: %+v", r.Range)
	}
	tot := r.Totals
	if tot.TargetsPublished != 3 || tot.TargetsFailed != 2 || tot.PostsPublished != 2 || tot.Retried != 2 || *tot.SuccessRate != 0.6 {
		t.Fatalf("totals: %+v", tot)
	}
	if *tot.DelayAvgSeconds != 51.7 || *tot.DelayP90Seconds != 120 {
		t.Fatalf("delay: %v %v", *tot.DelayAvgSeconds, *tot.DelayP90Seconds)
	}
	if m := tot.Metrics; m == nil || *m.Impressions != 1000 || m.Engagements != 17 || *m.Likes != 15 || m.Reposts != nil {
		t.Fatalf("metrics: %+v", tot.Metrics)
	}
	if r.Previous.TargetsPublished != 1 || r.Previous.TargetsFailed != 0 || r.Previous.Metrics != nil || r.Previous.PostsCreated != 0 {
		t.Fatalf("previous: %+v", r.Previous)
	}
	if r.Queue.Scheduled != 1 || r.Queue.Drafts != 1 || r.Queue.NextPublishAt == nil || *r.Queue.NextPublishAt != "2026-11-01T09:00:00.000000Z" {
		t.Fatalf("queue: %+v", r.Queue)
	}
	if len(r.Daily) != 27 || r.Daily[0].Date != "2026-10-01" || r.Daily[26].Date != "2026-10-27" {
		t.Fatalf("daily: %d days", len(r.Daily))
	}
	day := func(d string) app.AnalyticsDay {
		for _, x := range r.Daily {
			if x.Date == d {
				return x
			}
		}
		return app.AnalyticsDay{}
	}
	if day("2026-10-20").Published != 2 || day("2026-10-21").Failed != 1 || day("2026-10-22").Failed != 0 || day("2026-10-23").Failed != 1 {
		t.Fatalf("daily buckets in the workspace's zone: %+v", r.Daily)
	}

	chans := map[string]app.ChannelStats{}
	for _, ch := range r.Channels {
		chans[ch.ChannelID] = ch
	}
	if m := chans[met.ID]; m.Published != 1 || m.Failed != 1 || *m.SuccessRate != 0.5 || m.TopFailure == nil ||
		!strings.HasPrefix(*m.TopFailure, "rate limit hit, retry after") || m.MetricsCoverage.Support != "platform" ||
		m.MetricsCoverage.WithData != 1 || m.MetricsCoverage.Published != 1 || *m.AvgEngagements != 12 {
		t.Fatalf("metrics channel: %+v", m)
	}
	if o := chans[own.ID]; o.MetricsCoverage.Support != "automation" || o.Metrics == nil || o.Metrics.Engagements != 5 {
		t.Fatalf("own automation: %+v", o)
	}
	if k := chans[tok.ID]; k.MetricsCoverage.Support != "none" || k.Metrics != nil || k.Published != 1 || k.Failed != 1 {
		t.Fatalf("no numbers: %+v", k)
	}

	sources := map[string]app.SourceStats{}
	for _, s := range r.Sources {
		sources[s.Source] = s
	}
	if s := sources["dashboard"]; s.Kind != "dashboard" || s.TargetsPublished != 2 || s.PostsPublished != 1 || s.PostsCreated != 5 {
		t.Fatalf("dashboard source: %+v", s)
	}
	if s := sources["Claude Code"]; s.Kind != "mcp" || *s.SuccessRate != 0.5 {
		t.Fatalf("agent source: %+v", s)
	}
	if s := sources["schedule:Daily"]; s.Kind != "schedule" || s.Label != "Daily" || s.TargetsFailed != 1 || *s.SuccessRate != 0 {
		t.Fatalf("schedule source: %+v", s)
	}

	if len(r.Failures) != 1 || r.Failures[0].Count != 2 || len(r.Failures[0].Channels) != 2 || r.Failures[0].SamplePostID != p3 {
		t.Fatalf("failures grouped by reason: %+v", r.Failures)
	}
	var slot *app.TimeSlot
	for i, c := range r.Times.Cells {
		if c.Weekday == 1 && c.Hour == 10 {
			slot = &r.Times.Cells[i]
		}
	}
	if slot == nil || slot.Published != 2 || slot.Samples != 2 || *slot.AvgEngagements != 8.5 || !slot.LowConfidence {
		t.Fatalf("posting time Tuesday 10:00: %+v", r.Times.Cells)
	}
	if len(r.Times.Best) != 1 || r.Times.Best[0].Hour != 10 {
		t.Fatalf("best times: %+v", r.Times.Best)
	}
	if r.Top.By != "engagements" || len(r.Top.Posts) != 1 || r.Top.Posts[0].PostID != p1 || len(r.Top.Posts[0].Channels) != 2 ||
		r.Top.Posts[0].Excerpt != "Hi there" {
		t.Fatalf("top posts: %+v", r.Top)
	}
	if r.Feedback.Opened != 1 || r.Feedback.Resolved != 1 || r.Feedback.Open != 0 {
		t.Fatalf("feedback: %+v", r.Feedback)
	}

	// Filters: a channel, a source (sources stay unfiltered), another collection.
	e.do(nil, "GET", "/v1/analytics?from=2026-10-01&to=2026-10-27&channel="+tok.ID, e.key, nil, 200, &r)
	if r.Totals.TargetsPublished != 1 || r.Totals.TargetsFailed != 1 || len(r.Channels) != 1 || r.Top.By != "recent" || r.Queue.Drafts != 0 {
		t.Fatalf("channel filter: %+v %d", r.Totals, len(r.Channels))
	}
	e.do(nil, "GET", "/v1/analytics?from=2026-10-01&to=2026-10-27&source=dashboard", e.key, nil, 200, &r)
	if r.Totals.TargetsPublished != 2 || r.Totals.TargetsFailed != 0 || len(r.Sources) != 3 || len(r.Failures) != 0 || r.Totals.PostsCreated != 5 {
		t.Fatalf("source filter: %+v %+v", r.Totals, r.Sources)
	}
	// Several channels or sources add up; source= alone is the posts with none recorded, not every source.
	total := func(q string) (int64, int64) {
		var x app.AnalyticsReport
		e.do(nil, "GET", "/v1/analytics?from=2026-10-01&to=2026-10-27&"+q, e.key, nil, 200, &x)
		return x.Totals.TargetsPublished, x.Totals.TargetsFailed
	}
	n1, m1 := total("channel=" + tok.ID)
	n2, m2 := total("channel=" + met.ID)
	if p, f := total("channel=" + tok.ID + "&channel=" + met.ID); p != n1+n2 || f != m1+m2 {
		t.Fatalf("two channels: %d %d, want %d %d", p, f, n1+n2, m1+m2)
	}
	n1, m1 = total("source=dashboard")
	n2, m2 = total("source=Claude+Code")
	if p, f := total("source=dashboard&source=Claude+Code"); p != n1+n2 || f != m1+m2 {
		t.Fatalf("two sources: %d %d, want %d %d", p, f, n1+n2, m1+m2)
	}
	if p, f := total("source="); p != 0 || f != 0 {
		t.Fatalf("no source recorded: %d %d", p, f)
	}
	e.do(nil, "GET", "/v1/analytics?from=2026-10-01&to=2026-10-27&collection="+other.ID, e.key, nil, 200, &r)
	if r.Totals.TargetsPublished != 0 || r.Totals.SuccessRate != nil || r.Totals.DelayAvgSeconds != nil || len(r.Top.Posts) != 0 || r.Queue.Scheduled != 0 {
		t.Fatalf("collection filter: %+v", r.Totals)
	}

	// Defaults: the last 30 days up to today in the workspace's zone.
	e.do(nil, "GET", "/v1/analytics", e.key, nil, 200, &r)
	if r.Range.From != "2026-09-28" || r.Range.To != "2026-10-27" || r.Range.Days != 30 {
		t.Fatalf("default range: %+v", r.Range)
	}
	e.do(nil, "GET", "/v1/analytics?days=7", e.key, nil, 200, &r)
	if r.Range.From != "2026-10-21" || r.Range.To != "2026-10-27" || r.Range.PreviousFrom != "2026-10-14" {
		t.Fatalf("last 7 days: %+v", r.Range)
	}

	// Validation.
	for _, q := range []string{"from=yesterday", "from=2026-10-10&to=2026-10-01", "from=2025-01-01&to=2026-10-01",
		"channel=chn_nope", "collection=col_nope", "source=" + strings.Repeat("x", 201), "days=0x", "days=367", "days=7&from=2026-10-01"} {
		e.do(nil, "GET", "/v1/analytics?"+q, e.key, nil, 422, nil)
	}

	// Scope and workspace: posts:read is needed; another workspace sees nothing of this one.
	e.do(nil, "GET", "/v1/analytics", e.keyWith(app.ScopeMediaRead), nil, 403, nil)
	o := newWorkspace(e, "Other")
	e.do(nil, "GET", "/v1/analytics?from=2026-10-01&to=2026-10-27", o.key, nil, 200, &r)
	if r.Totals.TargetsPublished != 0 || len(r.Channels) != 0 || len(r.Sources) != 0 || r.Feedback.Opened != 0 || r.Queue.Drafts != 0 {
		t.Fatalf("other workspace sees this one: %+v", r)
	}
	e.do(nil, "GET", "/v1/analytics?channel="+met.ID, o.key, nil, 422, nil)
}

// The MCP tool calls the same service.
func TestAnalyticsMCP(t *testing.T) {
	e := newEnv(t)
	out := mcpResult(e, e.key, "get_analytics", map[string]any{"from": "2026-10-01", "to": "2026-10-07"})
	raw, _ := json.Marshal(out)
	var r app.AnalyticsReport
	if err := json.Unmarshal(raw, &r); err != nil || r.Range.Days != 7 || len(r.Daily) != 7 {
		t.Fatalf("mcp analytics: %v %s", err, raw)
	}
}

// Local mode: the workspace's own analytics with the access token.
func TestAnalyticsLocal(t *testing.T) {
	_, srv := newLocal(t)
	code, body := localReq(t, srv, http.MethodGet, "/v1/analytics", "", "", "")
	var r app.AnalyticsReport
	if code != 200 || json.Unmarshal([]byte(body), &r) != nil || r.Range.Days != 30 {
		t.Fatalf("local analytics: %d %s", code, body)
	}
	if code, _ := localReqWith(t, srv, http.MethodGet, "/v1/analytics", "", "", "", ""); code != 401 {
		t.Fatalf("without the token: %d", code)
	}
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
