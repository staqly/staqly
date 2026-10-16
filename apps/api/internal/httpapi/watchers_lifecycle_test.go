package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func (e *env) watcherEvents() []string {
	e.t.Helper()
	var page app.EventPage
	e.do(e.browser, "GET", "/v1/events?type_prefix=watcher.&limit=100", "", nil, 200, &page)
	var out []string
	for _, ev := range page.Data {
		out = append(out, ev.Type)
	}
	return out
}

// svcErr asserts a service error status. Error paths the OpenAPI document
// does not list per operation are exercised through the service (the one
// authorization path REST and MCP share) to keep the contract check clean.
func svcErr(t *testing.T, err error, status int) {
	t.Helper()
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Status != status {
		t.Fatalf("got %v, want status %d", err, status)
	}
}

func sp(s string) *string { return &s }

func TestWatcherCRUDAndScopes(t *testing.T) {
	e := newEnv(t)
	feed := &feedServer{}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	c := e.collection("Feeds")
	other := e.collection("Other")
	ctx := context.Background()
	noScope := principalOf(t, e, e.keyWith(app.ScopeItemsRead))
	_, err := e.svc.ListWatchers(ctx, noScope)
	svcErr(t, err, 403)
	_, err = e.svc.CreateWatcher(ctx, noScope, app.WatcherInput{Name: sp("x"), Kind: sp("rss"), URL: sp(srv.URL), Collection: sp(c.ID)})
	svcErr(t, err, 403)

	bad := []map[string]any{
		{"name": "x", "kind": "atom", "url": srv.URL, "collection": c.ID},
		{"name": "x", "kind": "rss", "url": "https://user:pw@example.com/feed", "collection": c.ID},
		{"name": "x", "kind": "rss", "url": "https:///nohost", "collection": c.ID},
		{"name": "x", "kind": "rss", "url": srv.URL, "collection": "no such collection"},
		{"name": "x", "kind": "rss", "url": srv.URL, "collection": c.ID, "every": "soon"},
		{"name": "x", "kind": "rss", "url": srv.URL, "collection": c.ID, "every": "200h"},
		{"name": "x", "kind": "rss", "url": srv.URL, "collection": c.ID, "keywords": []string{strings.Repeat("k", 101)}},
		{"name": "x", "kind": "rss", "url": srv.URL, "collection": c.ID, "status": "paused"},
		{"name": " ", "kind": "rss", "url": srv.URL, "collection": c.ID},
		{"kind": "rss", "url": srv.URL, "collection": c.ID},
	}
	for _, b := range bad {
		e.do(nil, "POST", "/v1/watchers", e.key, b, 422, nil)
	}

	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "News", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.Name,
		"keywords": []string{" ai ", ""}, "exclude_keywords": []string{"crypto"}, "every": "2h"}, 201, &w)
	if w.CollectionID != c.ID || w.EverySeconds != 7200 || w.Every != "2h0m0s" || len(w.Keywords) != 1 || w.Keywords[0] != "ai" ||
		len(w.ExcludeKeywords) != 1 || w.Status != "active" {
		t.Fatalf("created: %+v", w)
	}
	var list struct{ Data []app.Watcher }
	e.do(nil, "GET", "/v1/watchers", e.key, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != w.ID {
		t.Fatalf("list: %+v", list.Data)
	}
	owner := principalOf(t, e, e.key)
	_, err = e.svc.UpdateWatcher(ctx, owner, w.ID, app.WatcherInput{Kind: sp("url_diff")})
	svcErr(t, err, 422)
	_, err = e.svc.UpdateWatcher(ctx, owner, w.ID, app.WatcherInput{Status: sp("broken")})
	svcErr(t, err, 422)
	var got app.Watcher
	e.do(nil, "PATCH", "/v1/watchers/"+w.ID, e.key, map[string]any{"name": "Renamed", "collection": other.ID, "status": "paused"}, 200, &got)
	if got.Name != "Renamed" || got.CollectionID != other.ID || got.Status != "paused" {
		t.Fatalf("updated: %+v", got)
	}

	// Another workspace can't see, change or delete it.
	o := principalOf(t, e, newWorkspace(e, "Other WS").key)
	_, err = e.svc.GetWatcher(ctx, o, w.ID)
	svcErr(t, err, 404)
	_, err = e.svc.UpdateWatcher(ctx, o, w.ID, app.WatcherInput{Name: sp("pwned")})
	svcErr(t, err, 404)
	svcErr(t, e.svc.DeleteWatcher(ctx, o, w.ID), 404)
	_, err = e.svc.RunWatcherNow(ctx, o, w.ID)
	svcErr(t, err, 404)

	svcErr(t, e.svc.DeleteWatcher(ctx, noScope, w.ID), 403)
	e.do(nil, "DELETE", "/v1/watchers/"+w.ID, e.key, nil, 204, nil)
	svcErr(t, e.svc.DeleteWatcher(ctx, owner, w.ID), 404)
	_, err = e.svc.GetWatcher(ctx, owner, w.ID)
	svcErr(t, err, 404)
	e.do(nil, "GET", "/v1/watchers", e.key, nil, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("after delete: %+v", list.Data)
	}
	if got := strings.Join(e.watcherEvents(), ","); got != "watcher.deleted,watcher.updated,watcher.created" {
		t.Fatalf("events: %s", got)
	}
}

// Resuming a paused watcher counts against the plan's active-watcher limit.
func TestWatcherResumeRespectsPlanLimit(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(&feedServer{})
	defer srv.Close()
	e.free()
	c := e.collection("Feeds")
	var ws []app.Watcher
	for i := range 3 {
		var w app.Watcher
		e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "w" + string(rune('a'+i)), "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 201, &w)
		ws = append(ws, w)
	}
	e.do(nil, "PATCH", "/v1/watchers/"+ws[0].ID, e.key, map[string]any{"status": "paused"}, 200, nil)
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "4th", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 201, nil)
	owner := principalOf(t, e, e.key)
	_, err := e.svc.UpdateWatcher(context.Background(), owner, ws[0].ID, app.WatcherInput{Status: sp("active")})
	svcErr(t, err, 402)
	_, err = e.svc.UpdateWatcher(context.Background(), owner, ws[0].ID, app.WatcherInput{Every: sp("5m")}) // Free: at least 15m
	svcErr(t, err, 402)
	e.do(nil, "PATCH", "/v1/watchers/"+ws[0].ID, e.key, map[string]any{"name": "still paused"}, 200, nil)
}

// Failing sources degrade after 5 runs and pause after a day degraded; a
// manual check never resumes a paused watcher; a success recovers a degraded one.
func TestWatcherDegradesPausesAndRecovers(t *testing.T) {
	e := newEnv(t)
	e.svc.Fetcher = http.DefaultClient
	feed := &feedServer{items: []string{rssItem("1", "AI news", "x")}}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	c := e.collection("Feeds")
	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "w", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID,
		"keywords": []string{"ai"}, "exclude_keywords": []string{"crypto"}}, 201, &w)
	run := func() app.WatcherRun {
		var r app.WatcherRun
		e.do(nil, "POST", "/v1/watchers/"+w.ID+"/run", e.key, nil, 200, &r)
		return r
	}
	get := func() app.Watcher {
		var g app.Watcher
		e.do(nil, "GET", "/v1/watchers/"+w.ID, e.key, nil, 200, &g)
		return g
	}
	run() // baseline
	feed.mu.Lock()
	feed.items = append([]string{rssItem("2", "AI and crypto", "x"), rssItem("3", "AI agents", "x"), rssItem("4", "Gardening", "x")}, feed.items...)
	feed.mu.Unlock()
	if r := run(); r.NewItems != 1 || r.Seen != 4 {
		t.Fatalf("include/exclude keywords: %+v", r)
	}

	// Recovery: degraded, then the source works again.
	feed.mu.Lock()
	feed.failing = true
	feed.mu.Unlock()
	for range 5 {
		if r := run(); r.Status != "error" || r.Error == "" {
			t.Fatalf("failing run: %+v", r)
		}
	}
	if g := get(); g.Status != "degraded" {
		t.Fatalf("degraded: %+v", g)
	}
	feed.mu.Lock()
	feed.failing = false
	feed.mu.Unlock()
	if r := run(); r.Status != "ok" {
		t.Fatalf("recovery run: %+v", r)
	}
	if g := get(); g.Status != "active" || g.FailCount != 0 || g.LastError != "" {
		t.Fatalf("recovered: %+v", g)
	}

	// Pause: degraded for more than a day.
	feed.mu.Lock()
	feed.failing = true
	feed.mu.Unlock()
	for range 5 {
		run()
	}
	if g := get(); g.Status != "degraded" {
		t.Fatalf("degraded again: %+v", g)
	}
	run() // still within the day
	if g := get(); g.Status != "degraded" || g.FailCount != 6 {
		t.Fatalf("degraded < 24h: %+v", g)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	defer func() { e.svc.Now = time.Now }()
	run()
	if g := get(); g.Status != "paused" {
		t.Fatalf("paused after a day degraded: %+v", g)
	}
	// A manual check of a paused watcher, even a successful one, keeps it paused.
	feed.mu.Lock()
	feed.failing = false
	feed.mu.Unlock()
	if r := run(); r.Status != "ok" {
		t.Fatalf("manual run while paused: %+v", r)
	}
	if g := get(); g.Status != "paused" {
		t.Fatalf("manual run resumed a paused watcher: %+v", g)
	}
	want := "watcher.paused,watcher.degraded,watcher.recovered,watcher.degraded,watcher.created"
	if got := strings.Join(e.watcherEvents(), ","); got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
}

// The scheduler runs due watchers and skips suspended workspaces (leasing
// them for later instead of creating items).
func TestWatcherSchedulerRunsDueAndSkipsSuspended(t *testing.T) {
	e := newEnv(t)
	e.svc.Fetcher = http.DefaultClient
	feed := &feedServer{items: []string{rssItem("1", "Hello", "x")}}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	c := e.collection("Feeds")
	var w app.Watcher
	e.do(nil, "POST", "/v1/watchers", e.key, map[string]any{"name": "w", "kind": "rss", "url": srv.URL + "/feed.xml", "collection": c.ID}, 201, &w)
	get := func() app.Watcher {
		var g app.Watcher
		e.do(nil, "GET", "/v1/watchers/"+w.ID, e.key, nil, 200, &g)
		return g
	}
	start := func() func() {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { e.svc.RunWatchers(ctx); close(done) }()
		return func() { cancel(); <-done }
	}

	// Suspended: leased (next run pushed ~10 minutes out) but not run.
	if err := e.svc.SetPlan(context.Background(), workspaceID(e), "pro", "2020-01-01"); err != nil {
		t.Fatal(err)
	}
	stop := start()
	waitFor(t, func() bool { return get().NextRunAt != w.NextRunAt })
	stop()
	g := get()
	if g.LastRunAt != nil {
		t.Fatalf("suspended workspace watcher ran: %+v", g)
	}
	if next, _ := time.Parse(time.RFC3339, g.NextRunAt); time.Until(next) < 9*time.Minute {
		t.Fatalf("lease: next run %s", g.NextRunAt)
	}

	// Paid again and due: the scheduler runs the baseline.
	e.paid()
	e.svc.Now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	stop = start()
	waitFor(t, func() bool { return get().LastRunAt != nil })
	stop()
	if g := get(); g.Status != "active" || g.LastError != "" {
		t.Fatalf("scheduled run: %+v", g)
	}
}
