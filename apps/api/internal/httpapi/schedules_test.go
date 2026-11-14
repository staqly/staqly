package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Schedules over REST: validation, CRUD, a run that publishes the oldest waiting draft, a one-time schedule
// that runs once, asking an agent when nothing waits, and fetching posts from an endpoint.
func TestSchedulesREST(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Daily")
	ch := e.customChannel("Bot")

	// Validation.
	var errBody struct{ Error app.Error }
	for body, field := range map[string]string{
		`{"name":"x","kind":"recurring","rule":{"times":["09:00"]}}`:                                                         "collection",
		`{"collection":"` + c.ID + `","name":"x","kind":"recurring"}`:                                                        "rule",
		`{"collection":"` + c.ID + `","name":"x","kind":"recurring","rule":{"times":["25:00"]}}`:                             "rule.times",
		`{"collection":"` + c.ID + `","name":"x","kind":"recurring","rule":{"times":["09:00"],"tz":"Mars"}}`:                 "rule.tz",
		`{"collection":"` + c.ID + `","name":"x","kind":"once"}`:                                                             "run_at",
		`{"collection":"` + c.ID + `","name":"x","kind":"sometimes"}`:                                                        "kind",
		`{"collection":"` + c.ID + `","name":"x","kind":"once","run_at":"` + future(time.Hour) + `","channels":["ch_nope"]}`: "channels",
		`{"collection":"` + c.ID + `","name":"x","kind":"once","run_at":"` + future(time.Hour) + `","source":"endpoint"}`:    "endpoint_url",
	} {
		var in map[string]any
		json.Unmarshal([]byte(body), &in)
		e.do(nil, "POST", "/v1/schedules", e.key, in, 422, &errBody)
		if errBody.Error.Fields[field] == "" {
			t.Fatalf("%s: want a %s error, got %+v", body, field, errBody.Error)
		}
	}
	e.do(nil, "POST", "/v1/schedules", e.keyWith(app.ScopePostsRead), map[string]any{"collection": c.ID, "name": "x", "kind": "once", "run_at": future(time.Hour)}, 403, nil)

	// A recurring schedule: next run is the next 09:00 or 17:30 on a weekday in Warsaw.
	var sc app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": "Daily", "name": "Weekdays", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"17:30", "09:00", "09:00"}, "weekdays": []int{1, 2, 3, 4, 5}, "tz": "Europe/Warsaw"}}, 201, &sc)
	if sc.Kind != "recurring" || sc.NextRunAt == nil || sc.Pick != "next" || sc.Source != "none" || !sc.Enabled || sc.CollectionName != "Daily" ||
		len(sc.ChannelNames) != 1 || sc.ChannelNames[0] != "Bot" || strings.Join(sc.Rule.Times, ",") != "09:00,17:30" || sc.Rule.TZ != "Europe/Warsaw" {
		t.Fatalf("recurring: %+v", sc)
	}
	next, _ := time.Parse(time.RFC3339Nano, *sc.NextRunAt)
	loc, _ := time.LoadLocation("Europe/Warsaw")
	if l := next.In(loc); (l.Hour() != 9 && l.Hour() != 17) || l.Weekday() == time.Saturday || l.Weekday() == time.Sunday || !next.After(time.Now()) {
		t.Fatalf("next run %s", l)
	}
	var jobs app.JobPage
	e.do(nil, "GET", "/v1/jobs?kind=run_schedule&status=queued", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 || jobs.Data[0].Payload.ScheduleID != sc.ID || !strings.HasPrefix(jobs.Data[0].RunAt, (*sc.NextRunAt)[:19]) {
		t.Fatalf("run job: %+v", jobs.Data)
	}
	var list struct{ Data []app.Schedule }
	e.do(nil, "GET", "/v1/schedules?collection_id="+c.ID, e.key, nil, 200, &list)
	e.do(nil, "GET", "/v1/schedules/"+sc.ID, e.key, nil, 200, &sc)
	if len(list.Data) != 1 {
		t.Fatalf("list: %+v", list.Data)
	}
	scID := sc.ID
	sc = app.Schedule{} // decode into a fresh value: absent (omitempty) rule fields must not linger
	e.do(nil, "PATCH", "/v1/schedules/"+scID, e.key, map[string]any{"name": "Working days", "rule": map[string]any{"every_minutes": 30}}, 200, &sc)
	if sc.Name != "Working days" || sc.Rule.EveryMinutes != 30 || len(sc.Rule.Times) != 0 {
		t.Fatalf("patched: %+v", sc)
	}

	// Run now with a draft waiting: it is published to the schedule's channel; the next run is unchanged.
	draft := e.post(c.ID, map[string]any{"content": "oldest draft"})
	e.post(c.ID, map[string]any{"content": "newer draft"})
	before := *sc.NextRunAt
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if sc.LastResult != "published 1" || sc.LastRunAt == nil || *sc.NextRunAt != before {
		t.Fatalf("after run: %+v", sc)
	}
	var post app.Post
	e.do(nil, "GET", "/v1/posts/"+draft.ID, e.key, nil, 200, &post)
	if post.Status != "publishing" || len(post.Targets) != 1 || post.Targets[0].ChannelID != ch.ID {
		t.Fatalf("picked post: %+v", post)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?schedule_id="+sc.ID+"&type=schedule.ran", e.key, nil, 200, &evs)
	if len(evs.Data) != 1 || evs.Data[0].ScheduleID == nil || *evs.Data[0].ScheduleID != sc.ID {
		t.Fatalf("schedule.ran: %+v", evs.Data)
	}
	// pick all publishes everything waiting.
	e.do(nil, "PATCH", "/v1/schedules/"+sc.ID, e.key, map[string]any{"pick": "all"}, 200, &sc)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if sc.LastResult != "published 1" {
		t.Fatalf("pick all: %+v", sc)
	}
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if sc.LastResult != "nothing to publish" {
		t.Fatalf("nothing waiting: %+v", sc)
	}
	// pick selected publishes exactly those posts, wherever they are.
	chosen := e.post(c.ID, map[string]any{"content": "chosen"})
	e.post(c.ID, map[string]any{"content": "not chosen"})
	e.do(nil, "PATCH", "/v1/schedules/"+sc.ID, e.key, map[string]any{"pick": "selected"}, 422, nil)
	e.do(nil, "PATCH", "/v1/schedules/"+sc.ID, e.key, map[string]any{"pick": "selected", "post_ids": []string{chosen.ID}}, 200, &sc)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	e.do(nil, "GET", "/v1/posts/"+chosen.ID, e.key, nil, 200, &post)
	if sc.LastResult != "published 1" || post.Status != "publishing" {
		t.Fatalf("pick selected: %+v %+v", sc, post)
	}
	// The scheduled run itself: when its time comes the job runs it and queues the next one.
	slot := *sc.NextRunAt
	e.svc.Now = func() time.Time { return time.Now().Add(31 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/schedules/"+sc.ID, e.key, nil, 200, &sc)
	e.do(nil, "GET", "/v1/jobs?kind=run_schedule&status=queued", e.key, nil, 200, &jobs)
	if sc.NextRunAt == nil || *sc.NextRunAt <= slot || sc.LastRunAt == nil || len(jobs.Data) != 1 || jobs.Data[0].Payload.ScheduleID != sc.ID {
		t.Fatalf("after the scheduled run (slot %s): %+v / %+v", slot, sc, jobs.Data)
	}
	e.svc.Now = time.Now

	// A one-time schedule runs once and switches itself off.
	var once app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Launch day", "kind": "once", "run_at": future(time.Hour), "channels": []string{ch.ID}}, 201, &once)
	if once.NextRunAt == nil || once.RunAt == nil || *once.NextRunAt != *once.RunAt {
		t.Fatalf("once: %+v", once)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	e.svc.RunDueJobsOnce(ctx)
	e.svc.Now = time.Now
	e.do(nil, "GET", "/v1/schedules/"+once.ID, e.key, nil, 200, &once)
	if once.Enabled || once.NextRunAt != nil || once.LastRunAt == nil {
		t.Fatalf("once after its run: %+v", once)
	}
	e.do(nil, "DELETE", "/v1/schedules/"+once.ID, e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/schedules/"+once.ID, e.key, nil, 404, nil)
}

// When nothing waits a schedule asks an agent (an event plus an entry on /work) or an endpoint (a signed
// POST that answers with posts).
func TestScheduleSources(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Fed")
	ch := e.customChannel("Bot")

	// agent
	var sc app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Ask", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"every_minutes": 60}, "source": "agent"}, 201, &sc)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if sc.LastResult != "asked for posts" {
		t.Fatalf("agent run: %+v", sc)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?type=schedule.posts_requested", e.key, nil, 200, &evs)
	if len(evs.Data) != 1 || evs.Data[0].ScheduleID == nil || *evs.Data[0].ScheduleID != sc.ID {
		t.Fatalf("posts_requested: %+v", evs.Data)
	}
	var work struct{ Data []app.WorkItem }
	e.do(nil, "GET", "/v1/work", e.key, nil, 200, &work)
	if len(work.Data) != 1 || work.Data[0].Kind != "posts" || work.Data[0].Schedule.ID != sc.ID || work.Data[0].Collection.ID != c.ID {
		t.Fatalf("work: %+v", work.Data)
	}
	// Once the agent wrote a draft the request is satisfied.
	e.post(c.ID, map[string]any{"content": "from the agent"})
	e.do(nil, "GET", "/v1/work", e.key, nil, 200, &work)
	if len(work.Data) != 0 {
		t.Fatalf("work after a draft: %+v", work.Data)
	}
	e.do(nil, "DELETE", "/v1/schedules/"+sc.ID, e.key, nil, 204, nil)

	// endpoint
	empty := e.collection("Empty")
	var calls atomic.Int32
	var fail atomic.Bool
	var untitled atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Staqly-Signature") == "" || r.Header.Get("X-Staqly-Timestamp") == "" || !strings.Contains(string(body), `"collection_id":"`+empty.ID+`"`) {
			w.WriteHeader(400)
			return
		}
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if untitled.Load() {
			w.Write([]byte(`{"posts":[{"content":"no title"}]}`))
			return
		}
		w.Write([]byte(`{"posts":[{"title":"One","content":"fetched one","tags":["feed"]},{"title":"Two","content":"fetched two"}]}`))
	}))
	defer endpoint.Close()
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": empty.ID, "name": "Feed", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"08:00"}}, "source": "endpoint", "endpoint_url": endpoint.URL + "/posts"}, 201, &sc)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if sc.LastResult != "published 2" || calls.Load() != 1 {
		t.Fatalf("endpoint run: %+v (%d calls)", sc, calls.Load())
	}
	var page app.PostPage
	e.do(nil, "GET", "/v1/collections/"+empty.ID+"/posts", e.key, nil, 200, &page)
	if len(page.Data) != 2 {
		t.Fatalf("fetched posts: %+v", page.Data)
	}
	for _, p := range page.Data {
		if p.Status != "publishing" || p.Source != "schedule:Feed" || len(p.Targets) != 1 || p.Targets[0].ChannelID != ch.ID {
			t.Fatalf("fetched post: %+v", p)
		}
	}
	// A title is required here as for every post: an untitled one fails the run.
	untitled.Store(true)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if !strings.Contains(sc.LastResult, "title") {
		t.Fatalf("untitled endpoint post: %+v", sc)
	}
	untitled.Store(false)
	fail.Store(true)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, &sc)
	if !strings.HasPrefix(sc.LastResult, "endpoint failed") {
		t.Fatalf("failing endpoint: %+v", sc)
	}
	e.do(nil, "GET", "/v1/events?type=schedule.failed&schedule_id="+sc.ID, e.key, nil, 200, &evs)
	if len(evs.Data) != 2 { // the untitled post's run and this one
		t.Fatalf("schedule.failed: %+v", evs.Data)
	}
}

// An endpoint schedule's secret, generated when left empty, is returned in full by the create (and by
// an update that sets a new one) so the receiver can verify the signature; everywhere else it is masked.
func TestScheduleEndpointSecret(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Signed")
	ch := e.customChannel("Bot")
	var secret atomic.Value
	var verified atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var ts int64
		_, _ = fmt.Sscan(r.Header.Get("X-Staqly-Timestamp"), &ts)
		verified.Store(r.Header.Get("X-Staqly-Signature") == app.Sign(secret.Load().(string), ts, body))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"posts":[]}`))
	}))
	defer endpoint.Close()

	var sc app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Feed", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"08:00"}}, "source": "endpoint", "endpoint_url": endpoint.URL}, 201, &sc)
	if len(sc.EndpointSecret) < 20 || strings.Contains(sc.EndpointSecret, "*") {
		t.Fatalf("create returns the generated secret in full: %q", sc.EndpointSecret)
	}
	// A secret the caller chose is never echoed back, not even by the create.
	var chosen app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Chosen", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"09:00"}}, "source": "endpoint", "endpoint_url": endpoint.URL, "endpoint_secret": "my_own_secret_1234"}, 201, &chosen)
	if chosen.EndpointSecret != "****1234" {
		t.Fatalf("a chosen secret comes back masked: %q", chosen.EndpointSecret)
	}
	e.do(nil, "DELETE", "/v1/schedules/"+chosen.ID, e.key, nil, 204, nil)
	secret.Store(sc.EndpointSecret)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, nil)
	if !verified.Load() {
		t.Fatal("the request is not signed with the returned secret")
	}

	masked := "****" + sc.EndpointSecret[len(sc.EndpointSecret)-4:]
	var got app.Schedule // each response decodes into a fresh value: endpoint_secret is omitted when empty
	e.do(nil, "GET", "/v1/schedules/"+sc.ID, e.key, nil, 200, &got)
	if got.EndpointSecret != masked {
		t.Fatalf("get masks the secret: %q", got.EndpointSecret)
	}
	var list struct{ Data []app.Schedule }
	e.do(nil, "GET", "/v1/schedules", e.key, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].EndpointSecret != masked {
		t.Fatalf("list masks the secret: %+v", list.Data)
	}
	got = app.Schedule{}
	e.do(nil, "PATCH", "/v1/schedules/"+sc.ID, e.key, map[string]any{"name": "Feed 2"}, 200, &got)
	if got.EndpointSecret != masked {
		t.Fatalf("an update that keeps the secret masks it: %q", got.EndpointSecret)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?schedule_id="+sc.ID, e.key, nil, 200, &evs)
	raw, _ := json.Marshal(evs)
	if strings.Contains(string(raw), sc.EndpointSecret) {
		t.Fatalf("an event carries the secret: %s", raw)
	}

	// An empty secret generates a new one, returned once.
	got = app.Schedule{}
	e.do(nil, "PATCH", "/v1/schedules/"+sc.ID, e.key, map[string]any{"endpoint_secret": ""}, 200, &got)
	if got.EndpointSecret == sc.EndpointSecret || strings.Contains(got.EndpointSecret, "*") || len(got.EndpointSecret) < 20 {
		t.Fatalf("a new secret in full: %q (was %q)", got.EndpointSecret, sc.EndpointSecret)
	}
	secret.Store(got.EndpointSecret)
	verified.Store(false)
	e.do(nil, "POST", "/v1/schedules/"+sc.ID+"/run", e.key, nil, 202, nil)
	if !verified.Load() {
		t.Fatal("the request is not signed with the new secret")
	}

	// MCP create_schedule hands the generated secret to the agent the same way.
	out := mcpResult(e, e.key, "create_schedule", map[string]any{"collection": c.ID, "name": "Agent feed", "kind": "recurring",
		"rule": map[string]any{"times": []string{"09:00"}}, "source": "endpoint", "endpoint_url": endpoint.URL})
	if v, _ := out["endpoint_secret"].(string); len(v) < 20 || strings.Contains(v, "*") {
		t.Fatalf("create_schedule returns the secret in full: %v", out)
	}
	if v := mcpResult(e, e.key, "list_schedules", nil); strings.Contains(fmt.Sprint(v), fmt.Sprint(out["endpoint_secret"])) {
		t.Fatalf("list_schedules shows the secret: %v", v)
	}

	// Schedules without an endpoint carry no secret at all.
	var plain app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Plain", "kind": "recurring",
		"rule": map[string]any{"times": []string{"08:00"}}}, 201, &plain)
	if plain.EndpointSecret != "" {
		t.Fatalf("no endpoint, no secret: %q", plain.EndpointSecret)
	}
}
