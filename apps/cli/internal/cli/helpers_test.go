package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// call is one request the fake API received.
type call struct {
	Method, Path string
	Query        map[string][]string
	Header       http.Header
	Body         []byte
}

func (c call) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("request body is not JSON: %q", c.Body)
	}
	return m
}

// fakeAPI is an httptest server with per-route handlers that records calls.
type fakeAPI struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	calls  []call
	routes map[string]http.HandlerFunc
}

func newFake(t *testing.T) *fakeAPI {
	f := &fakeAPI{t: t, routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body})
		h := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if h == nil {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found", "message": "no fake route " + r.Method + " " + r.URL.Path}})
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// on registers a static JSON response.
func (f *fakeAPI) on(route string, status int, body any) {
	f.handle(route, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, status, body) })
}

func (f *fakeAPI) handle(route string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[route] = h
}

// seq serves the given responses in order, repeating the last one.
func (f *fakeAPI) seq(route string, resps ...func(w http.ResponseWriter)) {
	i := 0
	var mu sync.Mutex
	f.handle(route, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		r := resps[min(i, len(resps)-1)]
		i++
		mu.Unlock()
		r(w)
	})
}

func (f *fakeAPI) find(method, path string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeAPI) last(method, path string) call {
	f.t.Helper()
	cs := f.find(method, path)
	if len(cs) == 0 {
		f.t.Fatalf("no %s %s request was made", method, path)
	}
	return cs[len(cs)-1]
}

func apiErr(status int, code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": msg}}
}

func target(channel, status string) map[string]any {
	return map[string]any{"channel_id": channel, "channel_name": "Brand on Mastodon", "provider": "mastodon", "avatar_url": "",
		"content": nil, "settings": map[string]any{}, "status": status, "external_id": "", "external_url": "", "error": "",
		"attempts": 0, "published_at": nil}
}

func post(id, status string) map[string]any {
	return map[string]any{"id": id, "collection_id": "col_1", "collection_name": "Posts", "content": "Launch day " + id,
		"media": []any{}, "media_items": []any{}, "status": status, "publish_at": nil, "published_at": nil, "tags": []any{},
		"metadata": map[string]any{}, "source": "cli", "external_ref": "", "version": 1,
		"targets": []any{target("ch_1", "pending")}, "created_at": "2026-10-13T10:00:00.000Z", "updated_at": "2026-10-13T10:00:00.000Z"}
}

func channel(id, provider, name string) map[string]any {
	return map[string]any{"id": id, "provider": provider, "name": name, "handle": "@" + strings.ToLower(name), "avatar_url": "",
		"profile_url": "", "status": "connected", "settings": map[string]any{}, "last_error": "", "group_id": "grp_a",
		"created_at": "2026-10-13T10:00:00.000Z", "updated_at": "2026-10-13T10:00:00.000Z",
		"activity": map[string]any{"last_published_at": "2026-10-14T09:00:00.000Z", "last_post_id": "post_1", "next_publish_at": nil, "published_30d": 3}}
}

func collection(id, name string) map[string]any {
	return map[string]any{"id": id, "name": name, "description": "", "icon": "", "color": "", "processing_timeout_seconds": nil,
		"is_default": false, "archived": false, "archived_at": nil, "created_at": "2026-10-13T10:00:00.000Z",
		"updated_at": "2026-10-13T10:00:00.000Z", "post_count": 3, "status_counts": map[string]int{"draft": 2, "scheduled": 1}}
}

func respond(status int, body any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { writeJSON(w, status, body) }
}

// runOpts tweak one CLI invocation.
type runOpts struct {
	stdin     string
	stdinTTY  bool
	stdoutTTY bool
	env       map[string]string
	cfgPath   string
	ctx       context.Context
	sleep     func(ctx context.Context, d time.Duration) error
	// clock receives a function that advances the fake clock (e.g. to
	// simulate a long-poll holding the connection).
	clock func(advance func(time.Duration))
}

type result struct {
	out, err string
	code     int
	sleeps   []time.Duration
}

func (f *fakeAPI) run(args ...string) result { return f.runWith(runOpts{}, args...) }

func (f *fakeAPI) runWith(o runOpts, args ...string) result {
	f.t.Helper()
	return runCLI(f.t, f.srv.URL, o, args...)
}

func runCLI(t *testing.T, baseURL string, o runOpts, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	env := map[string]string{"STAQLY_API_URL": baseURL}
	if o.cfgPath == "" {
		o.cfgPath = filepath.Join(t.TempDir(), "config.json")
	}
	env["STAQLY_CONFIG"] = o.cfgPath
	env["HOME"] = t.TempDir() // never this computer's data folder (its local access token)
	for k, v := range o.env {
		env[k] = v
	}
	clock := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	var res result
	e := &Env{
		Stdin: strings.NewReader(o.stdin), Stdout: &out, Stderr: &errb,
		Getenv:   func(k string) string { return env[k] },
		StdinTTY: o.stdinTTY, StdoutTTY: o.stdoutTTY, StdinFd: -1,
		HTTP:  &http.Client{Timeout: 10 * time.Second},
		Build: BuildInfo{Version: "1.2.3", Commit: "abc123", Date: "2026-10-13"},
		Now:   func() time.Time { return clock },
	}
	e.Sleep = func(ctx context.Context, d time.Duration) error {
		res.sleeps = append(res.sleeps, d)
		if o.sleep != nil {
			if err := o.sleep(ctx, d); err != nil {
				return err
			}
		}
		clock = clock.Add(d)
		return ctx.Err()
	}
	if o.clock != nil {
		var mu sync.Mutex
		o.clock(func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() })
	}
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	res.code = Run(ctx, e, args)
	res.out, res.err = out.String(), errb.String()
	return res
}

func (r result) mustCode(t *testing.T, code int) result {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, code, r.out, r.err)
	}
	return r
}

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("output does not contain %q:\n%s", sub, s)
		}
	}
}

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, s)
	}
	return m
}
