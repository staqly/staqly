package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---- status / errors / global behaviour ----

func statusFake(t *testing.T) *fakeAPI {
	f := newFake(t)
	f.on("GET /health", 200, map[string]any{"status": "ok", "version": "0.9.0"})
	f.on("GET /v1/session", 200, map[string]any{"mode": "cloud", "workspace": map[string]any{"id": "ws_1", "name": "Acme", "plan": "free"},
		"principal": map[string]any{"type": "api_key", "id": "key_1", "name": "CI", "scopes": []string{"posts:read", "posts:write"}}, "csrf_token": ""})
	f.on("GET /v1/workspace/usage", 200, map[string]any{"plan": map[string]any{"id": "free", "name": "Free",
		"limits": map[string]any{"posts_per_month": 500, "storage_bytes": 1 << 30, "webhooks": -1, "data_export": false}}, "period": "2026-10",
		"usage": map[string]any{"posts_per_month": 12, "storage_bytes": 2048, "webhooks": 1}})
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{post("pst_1", "scheduled")}})
	return f
}

func TestStatus(t *testing.T) {
	f := statusFake(t)
	r := f.runWith(runOpts{env: map[string]string{"STAQLY_API_KEY": "stk_secretsecretsecret"}}, "status").mustCode(t, 0)
	mustContain(t, r.out, "cloud", "Acme", "Free (2026-10)", "posts_per_month", "500", "1.0 GiB", "unlimited", "stk_secr...", "posts:read posts:write", "Scheduled posts")
	if strings.Contains(r.out, "stk_secretsecretsecret") {
		t.Fatal("status must not print the full key")
	}
	if got := f.last("GET", "/v1/session").Header.Get("Authorization"); got != "Bearer stk_secretsecretsecret" {
		t.Fatalf("Authorization = %q", got)
	}
	r = f.run("status", "--json").mustCode(t, 0)
	m := mustJSON(t, r.out)
	if m["scheduled"] != float64(1) || m["session"].(map[string]any)["mode"] != "cloud" {
		t.Fatalf("json = %v", m)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status  int
		body    map[string]any
		header  map[string]string
		want    []string
		jsonKey string
	}{
		{401, apiErr(401, "unauthorized", "invalid or revoked API key"), nil, []string{"authentication failed", "staqly login"}, "unauthorized"},
		{403, apiErr(403, "not_local", "requests must come from this machine"), nil, []string{"only accepts keyless requests"}, "not_local"},
		{403, apiErr(403, "forbidden", "missing scope items:write"), nil, []string{"permission denied", "missing scope"}, "forbidden"},
		{402, map[string]any{"error": map[string]any{"code": "plan_limit_exceeded", "message": "The Free plan allows 500 items per month",
			"details": map[string]any{"limit": "items_per_month", "max": 500, "used": 500, "plan": "free"}}}, nil,
			[]string{"plan limit reached", "limit: items_per_month", "used: 500", "max: 500", "plan: free"}, "plan_limit_exceeded"},
		{409, apiErr(409, "conflict", "a collection with this name already exists"), nil, []string{"conflict:", "already exists"}, "conflict"},
		{422, map[string]any{"error": map[string]any{"code": "validation_failed", "message": "invalid input", "fields": map[string]any{"name": "is required"}}}, nil,
			[]string{"invalid request", "name: is required"}, "validation_failed"},
		{429, apiErr(429, "rate_limited", "too many requests"), map[string]string{"Retry-After": "30"}, []string{"rate limited", "Retry after 30s"}, "rate_limited"},
		{500, apiErr(500, "internal", "internal server error"), nil, []string{"server error"}, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.jsonKey, func(t *testing.T) {
			f := newFake(t)
			f.handle("POST /v1/collections", func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				writeJSON(w, tc.status, tc.body)
			})
			r := f.run("collections", "create", "X").mustCode(t, ExitError)
			mustContain(t, r.err, tc.want...)
			r = f.run("collections", "create", "X", "--json").mustCode(t, ExitError)
			var m struct {
				Error map[string]any `json:"error"`
			}
			if err := json.Unmarshal([]byte(r.err), &m); err != nil || m.Error["code"] != tc.jsonKey || m.Error["status"] != float64(tc.status) {
				t.Fatalf("json error = %s", r.err)
			}
			if tc.status == 429 && m.Error["retry_after_seconds"] != float64(30) {
				t.Fatalf("missing retry_after_seconds: %s", r.err)
			}
		})
	}
}

func TestUnreachableServer(t *testing.T) {
	r := runCLI(t, "http://127.0.0.1:1", runOpts{}, "collections", "list").mustCode(t, ExitError)
	mustContain(t, r.err, "cannot reach the staqly API", "STAQLY_API_URL")
	r = runCLI(t, "ftp://x", runOpts{}, "collections", "list").mustCode(t, ExitUsage)
	mustContain(t, r.err, "http://")
}

func TestHelpAndUsage(t *testing.T) {
	f := newFake(t)
	r := f.run().mustCode(t, 0)
	mustContain(t, r.out, "Commands:", "posts", "schedule", "Examples:")
	r = f.run("--help").mustCode(t, 0)
	mustContain(t, r.out, "Global flags:", "--api-url")
	r = f.run("posts", "--help").mustCode(t, 0)
	mustContain(t, r.out, "publish", "history")
	r = f.run("posts").mustCode(t, 0)
	mustContain(t, r.out, "Commands:")
	r = f.run("posts", "publish", "-h").mustCode(t, 0)
	mustContain(t, r.out, "--timeout", "--wait", "exit 0 published")
	f.run("nope").mustCode(t, ExitUsage)
	f.run("posts", "nope").mustCode(t, ExitUsage)
	f.run("posts", "list", "--bogus").mustCode(t, ExitUsage)
	f.run("posts", "get").mustCode(t, ExitUsage)
	f.run("posts", "get", "a", "b").mustCode(t, ExitUsage)
	if len(f.calls) != 0 {
		t.Fatal("help and usage errors must not call the API")
	}
}

func TestVersion(t *testing.T) {
	f := newFake(t)
	mustContain(t, f.run("version").mustCode(t, 0).out, "staqly 1.2.3 (abc123, 2026-10-13)")
	mustContain(t, f.run("--version").mustCode(t, 0).out, "1.2.3")
	if m := mustJSON(t, f.run("version", "--json").out); m["version"] != "1.2.3" {
		t.Fatal(m)
	}
}

func TestColorOnlyOnTTY(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{post("pst_1", "scheduled")}})
	if r := f.run("posts", "list"); strings.Contains(r.out, "\x1b[") {
		t.Fatal("no colors when stdout is not a TTY")
	}
	if r := f.runWith(runOpts{stdoutTTY: true}, "posts", "list"); !strings.Contains(r.out, "\x1b[33mscheduled") {
		t.Fatalf("expected colored status on a TTY: %q", r.out)
	}
	if r := f.runWith(runOpts{stdoutTTY: true, env: map[string]string{"NO_COLOR": "1"}}, "posts", "list"); strings.Contains(r.out, "\x1b[") {
		t.Fatal("NO_COLOR must disable colors")
	}
	if r := f.runWith(runOpts{stdoutTTY: true}, "posts", "list", "--no-color"); strings.Contains(r.out, "\x1b[") {
		t.Fatal("--no-color must disable colors")
	}
}

func TestParseArgsInterspersed(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/posts/pst_1", 200, post("pst_1", "draft"))
	// Global flag before the command, local flag after the positional.
	r := f.run("--json", "posts", "get", "pst_1").mustCode(t, 0)
	mustJSON(t, r.out)
	r = f.run("posts", "get", "--json", "--", "pst_1").mustCode(t, 0)
	mustJSON(t, r.out)
}

// ---- collections ----

func TestCollections(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/collections", 200, map[string]any{"data": []any{collection("col_1", "Blog posts")}})
	f.on("POST /v1/collections", 201, collection("col_2", "New"))
	f.on("GET /v1/collections/Blog posts", 200, collection("col_1", "Blog posts"))
	f.on("PATCH /v1/collections/col_1", 200, collection("col_1", "Renamed"))
	f.handle("DELETE /v1/collections/col_1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })

	r := f.run("collections", "list", "--archived").mustCode(t, 0)
	mustContain(t, r.out, "col_1", "Blog posts", "3", "2")
	if f.last("GET", "/v1/collections").Query["include_archived"][0] != "true" {
		t.Fatal("--archived must send include_archived")
	}
	f.run("collections", "create", "New", "--description", "d", "--processing-timeout", "2h").mustCode(t, 0)
	if b := f.last("POST", "/v1/collections").json(t); b["name"] != "New" || b["description"] != "d" || b["processing_timeout_seconds"] != float64(7200) {
		t.Fatalf("body = %v", b)
	}
	mustContain(t, f.run("collections", "get", "Blog posts").mustCode(t, 0).out, "draft 2", "scheduled 1")
	f.run("collections", "rename", "Blog posts", "Renamed").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/collections/col_1").json(t); b["name"] != "Renamed" {
		t.Fatalf("body = %v", b)
	}
	f.run("collections", "timeout", "Blog posts", "30m").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/collections/col_1").json(t); b["processing_timeout_seconds"] != float64(1800) {
		t.Fatalf("timeout body = %v", b)
	}
	f.run("collections", "timeout", "Blog posts", "default").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/collections/col_1").json(t); b["processing_timeout_seconds"] != nil {
		t.Fatalf("default body = %v", b)
	} else if _, ok := b["processing_timeout_seconds"]; !ok {
		t.Fatalf("default body lacks the key: %v", b)
	}
	f.run("collections", "archive", "Blog posts").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/collections/col_1").json(t); b["archived"] != true {
		t.Fatalf("body = %v", b)
	}
	f.run("collections", "archive", "Blog posts", "--undo").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/collections/col_1").json(t); b["archived"] != false {
		t.Fatalf("body = %v", b)
	}

	// delete: refuses without --yes off a terminal, confirms by name on one.
	f.run("collections", "delete", "Blog posts").mustCode(t, ExitUsage)
	f.runWith(runOpts{stdinTTY: true, stdin: "wrong\n"}, "collections", "delete", "Blog posts").mustCode(t, ExitError)
	if len(f.find("DELETE", "/v1/collections/col_1")) != 0 {
		t.Fatal("must not delete without confirmation")
	}
	f.runWith(runOpts{stdinTTY: true, stdin: "Blog posts\n"}, "collections", "delete", "Blog posts").mustCode(t, 0)
	f.run("collections", "delete", "Blog posts", "--yes").mustCode(t, 0)
	if c := f.last("DELETE", "/v1/collections/col_1"); c.Query["confirm"][0] != "Blog posts" {
		t.Fatalf("confirm = %v", c.Query)
	}
}

// ---- events ----

func ev(seq int, typ string) map[string]any {
	return map[string]any{"id": "evt", "seq": seq, "type": typ, "collection_id": "col_1", "post_id": "pst_1",
		"actor": map[string]any{"type": "local", "id": "local", "name": "Local"}, "data": map[string]any{}, "created_at": "2026-10-13T10:00:00Z"}
}

func TestEventsTailFollow(t *testing.T) {
	f := newFake(t)
	polls := 0
	f.handle("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch {
		case polls == 1:
			writeJSON(w, 200, map[string]any{"data": []any{ev(2, "post.created"), ev(1, "collection.created")}})
		case polls == 2 && r.URL.Query().Get("cursor") == "":
			// Two new events, newest first, with a further page.
			writeJSON(w, 200, map[string]any{"data": []any{ev(4, "post.published"), ev(3, "post.scheduled")}, "next_cursor": "3"})
		default:
			writeJSON(w, 200, map[string]any{"data": []any{ev(2, "post.created")}})
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sleeps := 0
	r := f.runWith(runOpts{ctx: ctx, sleep: func(context.Context, time.Duration) error {
		sleeps++
		if sleeps == 2 {
			cancel()
		}
		return nil
	}}, "events", "tail", "--follow", "--json").mustCode(t, 0)
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	var seqs []float64
	for _, l := range lines {
		seqs = append(seqs, mustJSON(t, l)["seq"].(float64))
	}
	if len(seqs) != 4 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 || seqs[3] != 4 {
		t.Fatalf("events printed out of order or duplicated: %v", seqs)
	}
}

func TestEventsTailTypeFilter(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/events", 200, map[string]any{"data": []any{ev(2, "post.published"), ev(1, "collection.created")}})
	r := f.run("events", "tail", "--type", "post.*").mustCode(t, 0)
	if strings.Contains(r.out, "collection.created") || !strings.Contains(r.out, "post.published") {
		t.Fatalf("glob filter failed:\n%s", r.out)
	}
	if _, ok := f.last("GET", "/v1/events").Query["type"]; ok {
		t.Fatal("globs are filtered client-side")
	}
	f.run("events", "tail", "--type", "post.published", "--limit", "5", "--post", "pst_1", "--channel", "ch_1").mustCode(t, 0)
	q := f.last("GET", "/v1/events").Query
	if q["type"][0] != "post.published" || q["limit"][0] != "5" || q["post_id"][0] != "pst_1" || q["channel_id"][0] != "ch_1" {
		t.Fatalf("query = %v", q)
	}
}

func TestWebhooks(t *testing.T) {
	f := newFake(t)
	hook := map[string]any{"id": "whk_1", "url": "https://example.com/h", "secret": "whsec_abcdefghijklmnop", "events": []string{"post.*"},
		"collection_ids": []string{}, "active": true}
	f.on("GET /v1/webhooks", 200, map[string]any{"data": []any{hook}})
	f.on("POST /v1/webhooks", 201, hook)
	f.on("GET /v1/collections/Blog", 200, collection("col_1", "Blog"))
	f.handle("DELETE /v1/webhooks/whk_1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	f.on("POST /v1/webhooks/whk_1/test", 202, map[string]any{"id": "dlv_1", "status": "pending"})
	f.on("GET /v1/webhook-deliveries", 200, map[string]any{"data": []any{map[string]any{"id": "dlv_1", "webhook_id": "whk_1",
		"event_type": "webhook.test", "status": "failed", "attempts": 2, "last_status_code": 500, "last_error": "HTTP 500", "created_at": "2026-10-13T10:00:00Z"}}})
	f.on("POST /v1/webhook-deliveries/dlv_1/retry", 202, map[string]any{"id": "dlv_1", "status": "pending"})

	r := f.run("webhooks", "list").mustCode(t, 0)
	if strings.Contains(r.out, "whsec_abcdefghijklmnop") {
		t.Fatal("list must mask the secret")
	}
	mustContain(t, r.out, "whk_1", "whsec_ab...")
	r = f.run("webhooks", "create", "--url", "https://example.com/h", "--events", "post.published,post.failed", "--collection", "Blog", "--max-attempts", "5").mustCode(t, 0)
	mustContain(t, r.out, "whsec_abcdefghijklmnop")
	b := f.last("POST", "/v1/webhooks").json(t)
	if b["url"] != "https://example.com/h" || len(b["events"].([]any)) != 2 || b["collection_ids"].([]any)[0] != "col_1" || b["max_attempts"] != float64(5) {
		t.Fatalf("body = %v", b)
	}
	f.run("webhooks", "create").mustCode(t, ExitUsage)
	f.run("webhooks", "delete", "whk_1").mustCode(t, 0)
	mustContain(t, f.run("webhooks", "test", "whk_1").mustCode(t, 0).out, "dlv_1")
	r = f.run("webhooks", "deliveries", "--webhook", "whk_1", "--status", "failed").mustCode(t, 0)
	mustContain(t, r.out, "dlv_1", "500", "HTTP 500")
	if q := f.last("GET", "/v1/webhook-deliveries").Query; q["webhook_id"][0] != "whk_1" || q["status"][0] != "failed" {
		t.Fatalf("query = %v", q)
	}
	f.run("webhooks", "retry", "dlv_1").mustCode(t, 0)
}

func TestKeys(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/api-keys", 200, map[string]any{"data": []any{map[string]any{"id": "key_1", "name": "CI", "prefix": "stk_abcd",
		"scopes": []string{"items:read"}, "created_at": "2026-10-13T10:00:00Z", "last_used_at": nil, "expires_at": nil, "revoked_at": nil}},
		"available_scopes": []string{"items:read", "admin"}})
	f.on("POST /v1/api-keys", 201, map[string]any{"id": "key_2", "name": "Gate", "prefix": "stk_new", "scopes": []string{"items:read", "items:write"}, "key": "stk_newsecretvalue"})
	f.handle("DELETE /v1/api-keys/key_1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })

	mustContain(t, f.run("keys", "list").mustCode(t, 0).out, "key_1", "stk_abcd", "active")
	r := f.run("keys", "create", "--name", "Gate", "--scope", "items:read,items:write", "--expires", "2027-01-01").mustCode(t, 0)
	mustContain(t, r.out, "stk_newsecretvalue", "not shown again")
	b := f.last("POST", "/v1/api-keys").json(t)
	if b["name"] != "Gate" || len(b["scopes"].([]any)) != 2 || b["expires_at"] != "2027-01-01" {
		t.Fatalf("body = %v", b)
	}
	f.run("keys", "create", "--name", "x").mustCode(t, ExitUsage)
	f.run("keys", "revoke", "key_1").mustCode(t, 0)
	if len(f.find("DELETE", "/v1/api-keys/key_1")) != 1 {
		t.Fatal("revoke must DELETE")
	}
}

// ---- login / profiles ----

func TestLoginProfilesLogout(t *testing.T) {
	f := statusFake(t)
	cfg := filepath.Join(t.TempDir(), "staqly", "config.json")
	o := runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_API_URL": ""}}

	r := f.runWith(o, "login", "--url", f.srv.URL, "--key", "stk_workkey12345", "--profile", "work").mustCode(t, 0)
	mustContain(t, r.out, "Acme", `Saved profile "work"`)
	if got := f.last("GET", "/v1/session").Header.Get("Authorization"); got != "Bearer stk_workkey12345" {
		t.Fatalf("login must verify with the key, got %q", got)
	}
	st, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", st.Mode().Perm())
	}
	loaded, _ := loadConfig(cfg)
	if loaded.Current != "work" || loaded.Profiles["work"].Key != "stk_workkey12345" || loaded.Profiles["work"].URL != f.srv.URL {
		t.Fatalf("config = %+v", loaded)
	}

	// The stored profile is used when no flag/env is given.
	f.runWith(o, "posts", "list").mustCode(t, 0)
	if got := f.last("GET", "/v1/posts").Header.Get("Authorization"); got != "Bearer stk_workkey12345" {
		t.Fatalf("profile key not used: %q", got)
	}
	// Env overrides the profile; the flag overrides env.
	o2 := runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_API_URL": "", "STAQLY_API_KEY": "stk_fromenv"}}
	f.runWith(o2, "posts", "list").mustCode(t, 0)
	if got := f.last("GET", "/v1/posts").Header.Get("Authorization"); got != "Bearer stk_fromenv" {
		t.Fatalf("env key not used: %q", got)
	}
	f.runWith(o2, "posts", "list", "--key", "stk_fromflag").mustCode(t, 0)
	if got := f.last("GET", "/v1/posts").Header.Get("Authorization"); got != "Bearer stk_fromflag" {
		t.Fatalf("flag key not used: %q", got)
	}

	// Key from stdin; failed verification saves nothing.
	f.runWith(runOpts{cfgPath: cfg, stdin: "stk_stdinkey999\n", env: o.env}, "login", "--url", f.srv.URL, "--key", "-", "--profile", "ci").mustCode(t, 0)
	loaded, _ = loadConfig(cfg)
	if loaded.Profiles["ci"].Key != "stk_stdinkey999" {
		t.Fatalf("stdin key not stored: %+v", loaded.Profiles["ci"])
	}
	r = runCLI(t, "", runOpts{cfgPath: cfg}, "login", "--url", "http://127.0.0.1:1", "--profile", "broken").mustCode(t, ExitError)
	mustContain(t, r.err, "Nothing was saved")
	if loaded, _ = loadConfig(cfg); loaded.Profiles["broken"] != nil {
		t.Fatal("failed login must not save")
	}

	r = f.runWith(o, "profiles").mustCode(t, 0)
	mustContain(t, r.out, "* ", "ci", "work", "stk_work...")
	if strings.Contains(r.out, "stk_workkey12345") {
		t.Fatal("profiles must mask keys")
	}
	f.runWith(o, "profiles", "use", "work").mustCode(t, 0)
	f.runWith(o, "profiles", "use", "nope").mustCode(t, ExitError)
	f.runWith(o, "posts", "list", "--profile", "nope").mustCode(t, ExitUsage)
	f.runWith(o, "logout", "--profile", "ci").mustCode(t, 0)
	f.runWith(o, "logout").mustCode(t, 0) // current = work
	if loaded, _ = loadConfig(cfg); len(loaded.Profiles) != 0 || loaded.Current != "" {
		t.Fatalf("config after logout = %+v", loaded)
	}
	f.runWith(o, "logout").mustCode(t, ExitError)
}

// ---- mcp / completion ----

func TestMCPConfig(t *testing.T) {
	f := newFake(t)
	r := f.run("mcp", "config", "claude").mustCode(t, 0)
	mustContain(t, r.out, "claude mcp add --transport http staqly "+f.srv.URL+"/mcp", `"type": "http"`)
	if strings.Contains(r.out, "Authorization") {
		t.Fatal("local config needs no auth header")
	}
	withKey := runOpts{env: map[string]string{"STAQLY_API_KEY": "stk_supersecret"}}
	for _, client := range []string{"claude", "codex", "opencode", "cursor", "windsurf"} {
		for _, stdio := range []bool{false, true} {
			args := []string{"mcp", "config", client}
			if stdio {
				args = append(args, "--stdio")
			}
			r := f.runWith(withKey, args...).mustCode(t, 0)
			if strings.Contains(r.out, "stk_supersecret") {
				t.Fatalf("%s: the key must never be printed:\n%s", client, r.out)
			}
			if !strings.Contains(r.out, "STAQLY_API_KEY") {
				t.Fatalf("%s: cloud config must reference STAQLY_API_KEY:\n%s", client, r.out)
			}
			s := f.runWith(withKey, append(args, "--json")...).mustCode(t, 0)
			snip := mustJSON(t, s.out)
			if snip["format"] == "json" {
				var v map[string]any
				if err := json.Unmarshal([]byte(snip["snippet"].(string)), &v); err != nil {
					t.Fatalf("%s snippet is not JSON: %v", client, err)
				}
			}
		}
	}
	r = f.run("mcp", "config", "codex").mustCode(t, 0)
	mustContain(t, r.out, "codex mcp add staqly --url "+f.srv.URL+"/mcp", "[mcp_servers.staqly]")
	r = f.run("mcp", "config", "opencode", "--stdio", "--snippet").mustCode(t, 0)
	mustContain(t, r.out, `"servers"`, `"type": "local"`, `"docker"`, `"staqly"`, `"mcp-stdio"`, `"codemode": false`)
	f.run("mcp", "config", "vim").mustCode(t, ExitUsage)
	if len(f.calls) != 0 {
		t.Fatal("mcp config must work offline")
	}
}

func TestCompletion(t *testing.T) {
	f := newFake(t)
	b := f.run("completion", "bash").mustCode(t, 0).out
	mustContain(t, b, "complete -F _staqly staqly", "posts) COMPREPLY", "publish", `"posts create") flags=`, "--wait")
	mustContain(t, f.run("completion", "zsh").mustCode(t, 0).out, "#compdef staqly", "bashcompinit")
	mustContain(t, f.run("completion", "fish").mustCode(t, 0).out, "complete -c staqly", "-a posts", "-l timeout")
	f.run("completion", "tcsh").mustCode(t, ExitUsage)
}

func TestMCPCodexAndCursorFormats(t *testing.T) {
	f := newFake(t)
	withKey := runOpts{env: map[string]string{"STAQLY_API_KEY": "stk_supersecret"}}
	// Local STDIO: no key, no -e.
	r := f.run("mcp", "config", "codex", "--stdio").mustCode(t, 0)
	mustContain(t, r.out, "codex mcp add staqly -- docker exec -i staqly mcp-stdio", `command = "docker"`, `args = ["exec", "-i", "staqly", "mcp-stdio"]`)
	// STDIO with a key: only the command, never env_vars or a TOML with the key.
	r = f.runWith(withKey, "mcp", "config", "codex", "--stdio").mustCode(t, 0)
	mustContain(t, r.out, "codex mcp add staqly --env STAQLY_API_KEY=$STAQLY_API_KEY -- docker exec -i -e STAQLY_API_KEY staqly mcp-stdio")
	if strings.Contains(r.out, "env_vars") || strings.Contains(r.out, "command =") {
		t.Fatalf("no TOML for stdio with a key:\n%s", r.out)
	}
	f.runWith(withKey, "mcp", "config", "codex", "--stdio", "--snippet").mustCode(t, ExitError)
	r = f.runWith(withKey, "mcp", "config", "codex").mustCode(t, 0)
	mustContain(t, r.out, `bearer_token_env_var = "STAQLY_API_KEY"`)

	// Cursor (https://cursor.com/docs/context/mcp): url + headers; stdio needs type.
	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	json.Unmarshal([]byte(f.run("mcp", "config", "cursor", "--snippet").mustCode(t, 0).out), &cfg)
	if cfg.MCPServers["staqly"]["url"] != f.srv.URL+"/mcp" || cfg.MCPServers["staqly"]["headers"] != nil {
		t.Fatalf("cursor local = %v", cfg)
	}
	json.Unmarshal([]byte(f.runWith(withKey, "mcp", "config", "cursor", "--snippet").mustCode(t, 0).out), &cfg)
	if h := cfg.MCPServers["staqly"]["headers"].(map[string]any); h["Authorization"] != "Bearer ${env:STAQLY_API_KEY}" {
		t.Fatalf("cursor headers = %v", h)
	}
	json.Unmarshal([]byte(f.run("mcp", "config", "cursor", "--stdio", "--snippet").mustCode(t, 0).out), &cfg)
	if cfg.MCPServers["staqly"]["type"] != "stdio" || cfg.MCPServers["staqly"]["command"] != "docker" {
		t.Fatalf("cursor stdio = %v", cfg)
	}

	// Windsurf: serverUrl + headers with ${env:...} interpolation.
	cfg.MCPServers = nil
	json.Unmarshal([]byte(f.runWith(withKey, "mcp", "config", "windsurf", "--snippet").mustCode(t, 0).out), &cfg)
	if w := cfg.MCPServers["staqly"]; w["serverUrl"] != f.srv.URL+"/mcp" ||
		w["headers"].(map[string]any)["Authorization"] != "Bearer ${env:STAQLY_API_KEY}" {
		t.Fatalf("windsurf = %v", cfg)
	}
}
