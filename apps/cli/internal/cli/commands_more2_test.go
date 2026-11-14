package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginEdgeCases(t *testing.T) {
	f := statusFake(t)
	cfg := filepath.Join(t.TempDir(), "config.json")
	o := runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_API_URL": ""}}

	// Bad URL scheme: usage error, nothing contacted.
	r := f.runWith(o, "login", "--url", "localhost:3333").mustCode(t, ExitUsage)
	mustContain(t, r.err, "must start with http://")

	// Key from STAQLY_API_KEY, odd prefix warns, trailing slash trimmed, JSON output masks the key.
	o2 := runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_API_URL": "", "STAQLY_API_KEY": "abc-legacy-key"}}
	r = f.runWith(o2, "login", "--url", f.srv.URL+"/", "--json").mustCode(t, 0)
	mustContain(t, r.err, "normally start with stk_")
	m := mustJSON(t, r.out)
	if m["profile"] != "default" || m["url"] != f.srv.URL || m["key"] != "abc-lega..." || m["mode"] != "cloud" || m["config"] != cfg {
		t.Fatalf("login json = %v", m)
	}
	if got := f.last("GET", "/v1/session").Header.Get("Authorization"); got != "Bearer abc-legacy-key" {
		t.Fatalf("Authorization = %q", got)
	}
	if strings.Contains(r.out, "abc-legacy-key") {
		t.Fatal("login --json leaked the key")
	}

	// --no-verify saves without contacting the server (unreachable URL).
	n := len(f.find("GET", "/v1/session"))
	r = f.runWith(o, "login", "--url", "http://127.0.0.1:1", "--key", "stk_offline123", "--profile", "offline", "--no-verify").mustCode(t, 0)
	mustContain(t, r.out, `Saved profile "offline"`)
	if strings.Contains(r.out, "Connected to") {
		t.Fatalf("--no-verify must not claim a connection: %s", r.out)
	}
	if len(f.find("GET", "/v1/session")) != n {
		t.Fatal("--no-verify contacted the server")
	}
	loaded, _ := loadConfig(cfg)
	if loaded.Current != "offline" || loaded.Profiles["offline"].Key != "stk_offline123" || loaded.Profiles["default"] == nil {
		t.Fatalf("config = %+v", loaded)
	}

	// Rejected key: the API error is explained and nothing is saved.
	f.on("GET /v1/session", 401, apiErr(401, "unauthorized", "invalid or revoked API key"))
	r = f.runWith(o, "login", "--url", f.srv.URL, "--key", "stk_badbadbad", "--profile", "bad").mustCode(t, ExitError)
	mustContain(t, r.err, "authentication failed", "Nothing was saved")
	if loaded, _ = loadConfig(cfg); loaded.Profiles["bad"] != nil || loaded.Current != "offline" {
		t.Fatalf("failed login changed config: %+v", loaded)
	}

	// Broken config file stops login before anything else.
	bad := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(bad, []byte("{"), 0o600)
	f.runWith(runOpts{cfgPath: bad}, "login", "--no-verify").mustCode(t, ExitError)
}

func TestProfilesJSONAndEmpty(t *testing.T) {
	f := newFake(t)
	cfg := filepath.Join(t.TempDir(), "config.json")
	o := runOpts{cfgPath: cfg}

	mustContain(t, f.runWith(o, "profiles").mustCode(t, 0).err, "No profiles yet")
	m := mustJSON(t, f.runWith(o, "profiles", "list", "--json").mustCode(t, 0).out)
	if m["config"] != cfg || len(m["profiles"].([]any)) != 0 {
		t.Fatalf("empty profiles json = %v", m)
	}

	(&Config{Current: "b", Profiles: map[string]*Profile{"a": {URL: "http://a"}, "b": {URL: "https://b", Key: "stk_bkey12345"}}}).save(cfg)
	m = mustJSON(t, f.runWith(o, "profiles", "--json").mustCode(t, 0).out)
	ps := m["profiles"].([]any)
	a, b := ps[0].(map[string]any), ps[1].(map[string]any)
	if a["name"] != "a" || a["current"] != false || a["key"] != "" || b["name"] != "b" || b["current"] != true || b["key"] != "stk_bkey..." {
		t.Fatalf("profiles json = %v", ps)
	}
	// STAQLY_PROFILE changes which one is marked current without saving.
	m = mustJSON(t, f.runWith(runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_PROFILE": "a"}}, "profiles", "--json").mustCode(t, 0).out)
	if m["profiles"].([]any)[0].(map[string]any)["current"] != true {
		t.Fatalf("STAQLY_PROFILE not honoured: %v", m)
	}
	mustContain(t, f.runWith(o, "profiles", "use", "a").mustCode(t, 0).out, `Now using profile "a" (http://a)`)
	if loaded, _ := loadConfig(cfg); loaded.Current != "a" {
		t.Fatalf("current = %q", loaded.Current)
	}
	m = mustJSON(t, f.runWith(o, "logout", "--profile", "b", "--json").mustCode(t, 0).out)
	if m["removed"] != "b" {
		t.Fatalf("logout json = %v", m)
	}
	if loaded, _ := loadConfig(cfg); loaded.Current != "a" || loaded.Profiles["b"] != nil {
		t.Fatalf("logout of a non-current profile changed current: %+v", loaded)
	}
}

func TestWorkspaceExportWaitEdges(t *testing.T) {
	exp := func(status string) map[string]any {
		return map[string]any{"id": "wex_1", "status": status, "created_at": "2026-10-13T10:00:00Z", "error": ""}
	}

	// Transient errors while polling are retried; --interval is clamped to 500ms.
	f := newFake(t)
	f.on("POST /v1/workspace/export", 202, exp("pending"))
	f.seq("GET /v1/workspace/exports/wex_1", respond(502, nil), respond(200, exp("ready")))
	r := f.run("workspace", "export", "start", "--wait", "--interval", "1ms", "--json").mustCode(t, 0)
	if m := mustJSON(t, r.out); m["status"] != "ready" {
		t.Fatalf("json = %v", m)
	}
	if len(r.sleeps) != 2 || r.sleeps[0] != 500*time.Millisecond {
		t.Fatalf("sleeps = %v", r.sleeps)
	}
	if strings.Contains(r.err, "Started export") {
		t.Fatal("--json must keep stderr quiet")
	}

	// Still running at the deadline: ExitTimeout with a hint.
	f = newFake(t)
	f.on("POST /v1/workspace/export", 202, exp("pending"))
	f.on("GET /v1/workspace/exports/wex_1", 200, exp("running"))
	r = f.run("workspace", "export", "start", "--wait", "--timeout", "5s", "--interval", "2s").mustCode(t, ExitTimeout)
	mustContain(t, r.err, "still running after 5s", "export status wex_1")

	// A non-transient error while polling stops the wait.
	f = newFake(t)
	f.on("POST /v1/workspace/export", 202, exp("pending"))
	f.on("GET /v1/workspace/exports/wex_1", 403, apiErr(403, "forbidden", "admin only"))
	mustContain(t, f.run("workspace", "export", "start", "--wait").mustCode(t, ExitError).err, "permission denied")

	// Expired export: error exit.
	f = newFake(t)
	f.on("POST /v1/workspace/export", 202, exp("expired"))
	mustContain(t, f.run("workspace", "export", "start", "--wait").mustCode(t, ExitError).err, "export wex_1 expired")

	// Start itself failing.
	f = newFake(t)
	f.handle("POST /v1/workspace/export", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 409, apiErr(409, "export_running", "an export is already running"))
	})
	mustContain(t, f.run("workspace", "export", "start").mustCode(t, ExitError).err, "already running")
}
