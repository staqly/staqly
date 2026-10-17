package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }

// ---- notification channels ----

func channel(id string) map[string]any {
	return map[string]any{"id": id, "name": "Phone", "type": "ntfy", "enabled": true,
		"config": map[string]any{"topic": "staqly-alerts", "access_token": "••••abcd", "priority": 4},
		"events": []string{"item.created", "item.escalated"}, "collection_ids": []string{}, "digest_window_seconds": 300,
		"fail_count": 0, "last_error": "", "last_sent_at": nil, "interactive": false, "created_at": "2026-10-13T10:00:00Z"}
}

func TestChannels(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/notification-channels", 200, map[string]any{"data": []any{channel("nch_1")}})
	f.on("GET /v1/notification-channels/nch_1", 200, channel("nch_1"))
	f.on("POST /v1/notification-channels", 201, channel("nch_1"))
	f.on("PATCH /v1/notification-channels/nch_1", 200, channel("nch_1"))
	f.handle("DELETE /v1/notification-channels/nch_1", noContent)
	f.seq("POST /v1/notification-channels/nch_1/test",
		respond(200, map[string]any{"ok": true}),
		respond(200, map[string]any{"ok": false, "error": "HTTP 401 from ntfy"}))
	f.on("GET /v1/notification-channels/nch_1/deliveries", 200, map[string]any{"data": []any{map[string]any{"id": "ntd_1",
		"event_type": "item.created", "item_id": "itm_1", "status": "failed", "attempts": 3, "last_error": "timeout", "created_at": "2026-10-13T10:00:00Z"}}})
	f.on("GET /v1/collections/Blog", 200, collection("col_1", "Blog"))

	mustContain(t, f.run("channels", "list").mustCode(t, 0).out, "nch_1", "Phone", "ntfy", "item.created,item.escalated")
	r := f.run("channels", "get", "nch_1").mustCode(t, 0)
	mustContain(t, r.out, "staqly-alerts", "••••abcd", "5m0s", "one-tap links")

	f.run("channels", "create", "--type", "ntfy", "--name", "Phone", "--config", "topic=staqly-alerts", "--config", "priority=4",
		"--events", "item.*", "--collection", "Blog", "--digest", "5m").mustCode(t, 0)
	b := f.last("POST", "/v1/notification-channels").json(t)
	cfg := b["config"].(map[string]any)
	if b["type"] != "ntfy" || b["name"] != "Phone" || cfg["topic"] != "staqly-alerts" || cfg["priority"] != float64(4) ||
		b["digest_window_seconds"] != float64(300) || b["collection_ids"].([]any)[0] != "col_1" || b["events"].([]any)[0] != "item.*" {
		t.Fatalf("body = %v", b)
	}
	// Secrets from stdin; chat IDs stay strings; lists split on commas.
	f.runWith(runOpts{stdin: `{"bot_token":"123:abc"}`}, "channels", "create", "--type", "telegram", "--name", "T",
		"--config-json", "-", "--config", "chat_id=-1001234", "--config", "approver_user_ids=1,2").mustCode(t, 0)
	cfg = f.last("POST", "/v1/notification-channels").json(t)["config"].(map[string]any)
	if cfg["bot_token"] != "123:abc" || cfg["chat_id"] != "-1001234" || len(cfg["approver_user_ids"].([]any)) != 2 {
		t.Fatalf("config = %v", cfg)
	}
	f.run("channels", "create", "--name", "x", "--config", "topic=t").mustCode(t, ExitUsage)
	f.run("channels", "create", "--type", "ntfy", "--name", "x").mustCode(t, ExitUsage)

	f.run("channels", "update", "nch_1", "--disable", "--config", "access_token=null", "--all-collections").mustCode(t, 0)
	b = f.last("PATCH", "/v1/notification-channels/nch_1").json(t)
	if b["enabled"] != false || len(b["collection_ids"].([]any)) != 0 {
		t.Fatalf("body = %v", b)
	}
	if v, ok := b["config"].(map[string]any)["access_token"]; !ok || v != nil {
		t.Fatalf("access_token=null must send null: %v", b)
	}
	f.run("channels", "update", "nch_1").mustCode(t, ExitUsage)
	f.run("channels", "update", "nch_1", "--enable", "--disable").mustCode(t, ExitUsage)

	mustContain(t, f.run("channels", "test", "nch_1").mustCode(t, 0).out, "Sent a test message")
	mustContain(t, f.run("channels", "test", "nch_1").mustCode(t, ExitError).err, "HTTP 401 from ntfy")
	r = f.run("channels", "deliveries", "nch_1", "--limit", "5").mustCode(t, 0)
	mustContain(t, r.out, "ntd_1", "failed", "timeout")
	if q := f.last("GET", "/v1/notification-channels/nch_1/deliveries").Query; q["limit"][0] != "5" {
		t.Fatalf("query = %v", q)
	}
	f.run("channels", "delete", "nch_1").mustCode(t, 0)
	mustJSON(t, f.run("notifications", "get", "nch_1", "--json").mustCode(t, 0).out)
}

// ---- keys rotate ----

func TestKeysRotate(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/api-keys/key_1/rotate", 201, map[string]any{"id": "key_2", "name": "CI", "prefix": "stk_rot",
		"scopes": []string{"items:read"}, "key": "stk_rotatedsecretvalue"})
	r := f.run("keys", "rotate", "key_1").mustCode(t, 0)
	mustContain(t, r.out, "key_1", "key_2", "stk_rotatedsecretvalue", "not shown again")
	if m := mustJSON(t, f.run("keys", "rotate", "key_1", "--json").mustCode(t, 0).out); m["key"] != "stk_rotatedsecretvalue" {
		t.Fatal(m)
	}
}

// ---- workspace, plans, sessions, connect ----

func usageBody(plan string) map[string]any {
	return map[string]any{"plan": map[string]any{"id": plan, "name": strings.ToUpper(plan[:1]) + plan[1:],
		"limits": map[string]any{"items_per_month": 500, "storage_bytes": 1 << 30}}, "period": "2026-10",
		"usage": map[string]any{"items_per_month": 12, "storage_bytes": 2048}, "plan_expires_at": nil, "suspended": plan == "pro"}
}

func TestWorkspacePlansSessions(t *testing.T) {
	f := newFake(t)
	ws := map[string]any{"id": "ws_1", "name": "Acme", "plan": "free", "recovery_rotated_at": "2026-10-01T10:00:00Z", "created_at": "2026-09-01T10:00:00Z"}
	f.on("GET /v1/workspace", 200, ws)
	f.on("PATCH /v1/workspace", 200, map[string]any{"id": "ws_1", "name": "Acme content", "plan": "free"})
	f.on("GET /v1/workspace/usage", 200, usageBody("pro"))
	f.on("POST /v1/workspace/plan", 200, usageBody("free"))
	f.on("GET /v1/plans", 200, map[string]any{"data": []any{
		map[string]any{"id": "free", "name": "Free", "price_usd_month": 0, "limits": map[string]any{"items_per_month": 500, "storage_bytes": 1 << 30, "webhooks": 2}},
		map[string]any{"id": "pro", "name": "Pro", "price_usd_month": 19, "limits": map[string]any{"items_per_month": -1}}}})
	f.on("GET /v1/sessions", 200, map[string]any{"data": []any{map[string]any{"id": "ses_1", "user_agent": "Mozilla/5.0 (iPhone)",
		"created_at": "2026-10-13T10:00:00Z", "last_seen_at": "2026-10-13T11:00:00Z", "expires_at": "2026-11-13T10:00:00Z", "current": false}}})
	f.handle("DELETE /v1/sessions/ses_1", noContent)
	f.on("POST /v1/device-links", 201, map[string]any{"url": "http://localhost:8080/connect#tok", "expires_at": "2026-10-13T10:05:00Z",
		"reachable": false, "hint": "Set PUBLIC_DASHBOARD_URL."})

	mustContain(t, f.run("workspace", "get").mustCode(t, 0).out, "ws_1", "Acme", "free")
	f.run("workspace", "rename", "Acme content").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/workspace").json(t); b["name"] != "Acme content" {
		t.Fatalf("body = %v", b)
	}
	mustContain(t, f.run("workspace", "usage").mustCode(t, 0).out, "Pro (pro)", "suspended", "items_per_month", "1.0 GiB")

	r := f.run("plans", "list").mustCode(t, 0)
	mustContain(t, r.out, "free", "Pro", "19", "unlimited", "1.0 GiB")
	f.run("plans", "free").mustCode(t, ExitUsage)
	f.runWith(runOpts{stdinTTY: true, stdin: "n\n"}, "plans", "free").mustCode(t, ExitError)
	if len(f.find("POST", "/v1/workspace/plan")) != 0 {
		t.Fatal("must not switch the plan without confirmation")
	}
	mustContain(t, f.run("plan", "free", "--yes").mustCode(t, 0).out, "Switched to the Free plan")
	if b := f.last("POST", "/v1/workspace/plan").json(t); b["plan"] != "free" {
		t.Fatalf("body = %v", b)
	}
	f.runWith(runOpts{stdinTTY: true, stdin: "y\n"}, "plans", "free").mustCode(t, 0)

	mustContain(t, f.run("sessions", "list").mustCode(t, 0).out, "ses_1", "iPhone")
	f.run("sessions", "revoke", "ses_1").mustCode(t, 0)
	if len(f.find("DELETE", "/v1/sessions/ses_1")) != 1 {
		t.Fatal("revoke must DELETE")
	}

	r = f.run("connect").mustCode(t, 0)
	if strings.TrimSpace(r.out) != "http://localhost:8080/connect#tok" {
		t.Fatalf("connect stdout = %q", r.out)
	}
	mustContain(t, r.err, "One-time", "localhost", "PUBLIC_DASHBOARD_URL")
}

// ---- items delete / purge / bulk / versions ----

func TestItemsDeletePurge(t *testing.T) {
	f := newFake(t)
	f.handle("DELETE /v1/items/itm_1", noContent)
	f.on("DELETE /v1/items/itm_2", 404, apiErr(404, "not_found", "item not found"))
	f.on("POST /v1/items/itm_1/purge", 200, map[string]any{"item_id": "itm_1", "events_deleted": 7, "media_deleted": true})

	mustContain(t, f.run("items", "delete", "itm_1").mustCode(t, 0).out, "Deleted item itm_1")
	r := f.run("items", "delete", "itm_1", "itm_2").mustCode(t, ExitError)
	mustContain(t, r.out, "itm_1")
	mustContain(t, r.err, "itm_2", "1 of 2 failed")
	r = f.run("items", "delete", "itm_1", "itm_2", "--json").mustCode(t, ExitError)
	if !strings.Contains(r.out, `"ok": false`) {
		t.Fatalf("json = %s", r.out)
	}

	f.run("items", "purge", "itm_1").mustCode(t, ExitUsage)
	f.runWith(runOpts{stdinTTY: true, stdin: "itm_9\n"}, "items", "purge", "itm_1").mustCode(t, ExitError)
	if len(f.find("POST", "/v1/items/itm_1/purge")) != 0 {
		t.Fatal("must not purge without confirmation")
	}
	mustContain(t, f.runWith(runOpts{stdinTTY: true, stdin: "itm_1\n"}, "items", "purge", "itm_1").mustCode(t, 0).out, "7 events deleted", "media deleted")
	f.run("items", "purge", "itm_1", "--yes").mustCode(t, 0)
}

func TestItemsBulk(t *testing.T) {
	f := newFake(t)
	f.handle("POST /v1/items/bulk-actions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
		}
		_ = decodeBody(r, &in)
		var data []any
		for _, id := range in.IDs {
			if id == "itm_bad" {
				data = append(data, map[string]any{"id": id, "ok": false, "error": map[string]any{"code": "conflict", "message": "already decided"}})
				continue
			}
			data = append(data, map[string]any{"id": id, "ok": true, "item": item(id, "approved")})
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})

	r := f.run("items", "bulk", "approve", "itm_1", "itm_2", "-m", "ship it").mustCode(t, 0)
	mustContain(t, r.out, "Approved itm_1", "Approved itm_2")
	b := f.last("POST", "/v1/items/bulk-actions").json(t)
	if b["action"] != "approve" || b["feedback"] != "ship it" || len(b["ids"].([]any)) != 2 {
		t.Fatalf("body = %v", b)
	}

	// 150 IDs from stdin → two requests of 100 and 50.
	var ids []string
	for i := range 150 {
		ids = append(ids, fmt.Sprintf("itm_%d", i))
	}
	r = f.runWith(runOpts{stdin: strings.Join(ids, "\n")}, "items", "bulk", "approve", "--stdin", "--json").mustCode(t, 0)
	if n := len(mustJSON(t, r.out)["data"].([]any)); n != 150 {
		t.Fatalf("results = %d", n)
	}
	calls := f.find("POST", "/v1/items/bulk-actions")
	if len(calls) != 3 || len(calls[1].json(t)["ids"].([]any)) != 100 || len(calls[2].json(t)["ids"].([]any)) != 50 {
		t.Fatalf("expected chunks of 100 and 50, got %d calls", len(calls))
	}

	r = f.run("items", "bulk", "regenerate", "itm_1", "itm_bad", "--payload", `{"n":2}`).mustCode(t, ExitError)
	mustContain(t, r.err, "itm_bad", "already decided", "1 of 2 failed")
	if p := f.last("POST", "/v1/items/bulk-actions").json(t)["payload"].(map[string]any); p["n"] != float64(2) {
		t.Fatalf("payload = %v", p)
	}
	f.run("items", "bulk", "approve").mustCode(t, ExitUsage)
	f.run("items", "bulk", "approve", "itm_1", "--payload", "{").mustCode(t, ExitUsage)
}

func TestItemsVersionsAndMediaID(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/items/itm_1/versions", 200, map[string]any{"data": []any{
		map[string]any{"version": 1, "snapshot": item("itm_1", "pending"), "actor": "api_key:key_1", "created_at": "2026-10-13T10:00:00Z"}}})
	f.on("PATCH /v1/items/itm_1", 200, item("itm_1", "pending"))
	mustContain(t, f.run("items", "versions", "itm_1").mustCode(t, 0).out, "VERSION", "Title itm_1", "api_key:key_1")
	f.run("items", "update", "itm_1", "--media-id", "med_1").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/items/itm_1").json(t); b["media_id"] != "med_1" {
		t.Fatalf("body = %v", b)
	}
	f.run("items", "update", "itm_1", "--media-id", "").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/items/itm_1").json(t); b["media_id"] != "" {
		t.Fatalf("--media-id \"\" must clear: %v", b)
	}
}

// ---- media ----

func TestMedia(t *testing.T) {
	f := newFake(t)
	var uploaded string
	f.handle("POST /v1/media", func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		part, err := mr.NextPart()
		if err != nil || part.FormName() != "file" {
			writeJSON(w, 422, apiErr(422, "validation_failed", "file is required"))
			return
		}
		uploaded = part.FileName() + ":" + readAllString(part)
		writeJSON(w, 201, map[string]any{"id": "med_1", "mime_type": "image/png", "size_bytes": 4, "original_name": "cover.png", "url": "/v1/media/med_1"})
	})
	f.handle("GET /v1/media/med_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `inline; filename="../cover.png"`)
		_, _ = w.Write([]byte("\x89PNG" + r.URL.Query().Get("w")))
	})
	f.handle("DELETE /v1/media/med_1", noContent)

	dir := t.TempDir()
	src := filepath.Join(dir, "cover.png")
	os.WriteFile(src, []byte("\x89PNG"), 0o600)
	r := f.run("media", "upload", src).mustCode(t, 0)
	mustContain(t, r.out, "med_1", "image/png", "--media-id med_1")
	if uploaded != "cover.png:\x89PNG" {
		t.Fatalf("uploaded = %q", uploaded)
	}

	out := filepath.Join(dir, "thumb.png")
	f.run("media", "get", "med_1", "--width", "320", "-o", out).mustCode(t, 0)
	if b, _ := os.ReadFile(out); string(b) != "\x89PNG320" {
		t.Fatalf("file = %q", b)
	}
	if f.last("GET", "/v1/media/med_1").Query["w"][0] != "320" {
		t.Fatal("--width must send w")
	}
	// Piped stdout gets the bytes; a terminal is refused unless saving to a file.
	if r := f.run("media", "get", "med_1").mustCode(t, 0); r.out != "\x89PNG" {
		t.Fatalf("stdout = %q", r.out)
	}
	f.runWith(runOpts{stdoutTTY: true}, "media", "get", "med_1", "-o", "-").mustCode(t, ExitUsage)
	m := mustJSON(t, f.run("media", "get", "med_1", "-o", out, "--json").mustCode(t, 0).out)
	if m["path"] != out || m["bytes"] != float64(4) {
		t.Fatalf("json = %v", m)
	}
	f.run("media", "delete", "med_1").mustCode(t, 0)
}

func TestSuggestedName(t *testing.T) {
	cases := map[string]string{
		`attachment; filename="staqly-export-20261013-1000.zip"`: "staqly-export-20261013-1000.zip",
		`inline; filename="../../etc/passwd"`:                    "passwd",
		`inline; filename="..\\evil.png"`:                        "evil.png",
		`inline; filename=".bashrc"`:                             "fallback.bin",
		``:                                                       "fallback.bin",
	}
	for in, want := range cases {
		if got := suggestedName(in, "fallback.bin"); got != want {
			t.Errorf("suggestedName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- events export ----

func TestEventsExport(t *testing.T) {
	f := newFake(t)
	f.handle("GET /v1/events/export", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") == "csv" {
			w.Header().Set("Content-Type", "text/csv")
			fmt.Fprint(w, "created_at,id,type\n2026-10-13T10:00:00Z,evt_1,item.created\x1b[2J\n")
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"id":"evt_1","type":"item.created"}`+"\n"+`{"id":"evt_2","type":"item.approved"}`+"\n")
	})
	f.on("GET /v1/collections/Blog", 200, collection("col_1", "Blog"))

	r := f.run("events", "export", "--type", "item.*", "--collection", "Blog", "--from", "2026-10-01", "--actor-type", "api_key").mustCode(t, 0)
	if strings.Count(r.out, "\n") != 2 || !strings.Contains(r.out, "evt_2") {
		t.Fatalf("stdout = %q", r.out)
	}
	q := f.last("GET", "/v1/events/export").Query
	if q["type_prefix"][0] != "item." || q["collection_id"][0] != "col_1" || q["from"][0] != "2026-10-01" || q["format"][0] != "ndjson" || q["actor_type"][0] != "api_key" {
		t.Fatalf("query = %v", q)
	}
	if _, ok := q["type"]; ok {
		t.Fatal("a glob must become type_prefix")
	}
	f.run("events", "export", "--type", "item.approved").mustCode(t, 0)
	if q := f.last("GET", "/v1/events/export").Query; q["type"][0] != "item.approved" {
		t.Fatalf("query = %v", q)
	}
	f.run("events", "export", "--type", "item.*.x").mustCode(t, ExitUsage)
	f.run("events", "export", "--format", "xml").mustCode(t, ExitUsage)

	// CSV to a terminal is sanitized; to a file it is verbatim.
	if r := f.runWith(runOpts{stdoutTTY: true}, "events", "export", "--format", "csv").mustCode(t, 0); strings.Contains(r.out, "\x1b") {
		t.Fatalf("terminal output must be sanitized: %q", r.out)
	}
	out := filepath.Join(t.TempDir(), "events.csv")
	r = f.run("events", "export", "--format", "csv", "--output", out).mustCode(t, 0)
	mustContain(t, r.err, "Saved", out)
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "evt_1,item.created\x1b[2J") {
		t.Fatalf("file = %q", b)
	}
}

// ---- workspace export ----

func TestWorkspaceExport(t *testing.T) {
	f := newFake(t)
	exp := func(status string) map[string]any {
		return map[string]any{"id": "wex_1", "status": status, "size_bytes": 2048, "created_at": "2026-10-13T10:00:00Z",
			"started_at": nil, "finished_at": nil, "expires_at": "2026-10-14T10:00:00Z"}
	}
	f.on("POST /v1/workspace/export", 202, exp("pending"))
	f.seq("GET /v1/workspace/exports/wex_1", respond(200, exp("running")), respond(200, exp("ready")))
	f.handle("GET /v1/workspace/exports/wex_1/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="staqly-export-20261013-1000.zip"`)
		fmt.Fprint(w, "PK\x03\x04zip")
	})

	r := f.run("workspace", "export", "start").mustCode(t, 0)
	mustContain(t, r.out, "wex_1", "pending", "export status wex_1")
	if len(f.find("GET", "/v1/workspace/exports/wex_1")) != 0 {
		t.Fatal("start without --wait must not poll")
	}

	out := filepath.Join(t.TempDir(), "export.zip")
	r = f.run("workspace", "export", "start", "--output", out).mustCode(t, 0)
	if b, _ := os.ReadFile(out); string(b) != "PK\x03\x04zip" {
		t.Fatalf("zip = %q", b)
	}
	if len(r.sleeps) != 2 || len(f.find("GET", "/v1/workspace/exports/wex_1")) != 2 {
		t.Fatalf("expected two polls, slept %v", r.sleeps)
	}

	mustContain(t, f.run("workspace", "export", "status", "wex_1").mustCode(t, 0).out, "ready", "2.0 KiB", "export download wex_1")

	// Default file name comes from Content-Disposition when stdout is a terminal.
	dir := t.TempDir()
	t.Chdir(dir)
	f.runWith(runOpts{stdoutTTY: true}, "workspace", "export", "download", "wex_1").mustCode(t, 0)
	if _, err := os.Stat(filepath.Join(dir, "staqly-export-20261013-1000.zip")); err != nil {
		t.Fatal(err)
	}
	// Never overwrites a file it named itself.
	f.runWith(runOpts{stdoutTTY: true}, "workspace", "export", "download", "wex_1").mustCode(t, ExitError)
	if r := f.run("workspace", "export", "download", "wex_1"); r.out != "PK\x03\x04zip" {
		t.Fatalf("piped stdout = %q", r.out)
	}
}

func TestWorkspaceExportDataOnly(t *testing.T) {
	f := newFake(t)
	job := map[string]any{"id": "wex_1", "status": "pending", "size_bytes": 0, "created_at": "2026-10-13T10:00:00Z",
		"started_at": nil, "finished_at": nil, "expires_at": "2026-10-14T10:00:00Z", "includes_media": false}
	f.on("POST /v1/workspace/export", 202, job)
	f.on("GET /v1/workspace/exports/wex_1", 200, job)
	mustContain(t, f.run("workspace", "export", "start").mustCode(t, 0).out, "Data only", "come with Pro")
	mustContain(t, f.run("workspace", "export", "status", "wex_1").mustCode(t, 0).out, "Contents:", "data only")
}

func TestWorkspaceExportFailed(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/workspace/export", 202, map[string]any{"id": "wex_1", "status": "failed", "error": "disk full"})
	mustContain(t, f.run("workspace", "export", "start", "--wait").mustCode(t, ExitError).err, "disk full")
}

// ---- watchers / webhooks get & update ----

func TestWatchersGet(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/watchers/wch_1", 200, map[string]any{"id": "wch_1", "name": "Go releases", "kind": "rss", "status": "paused",
		"url": "https://github.com/golang/go/releases.atom", "every": "1h0m0s", "next_run_at": "2026-10-13T11:00:00Z", "items_created": 5})
	mustContain(t, f.run("watchers", "get", "wch_1").mustCode(t, 0).out, "Go releases", "paused", "releases.atom")
}

func TestWebhooksGetUpdate(t *testing.T) {
	f := newFake(t)
	masked := map[string]any{"id": "whk_1", "url": "https://example.com/h", "secret": "whsec_****mnop", "events": []string{"*"},
		"collection_ids": []string{}, "active": true, "timeout_ms": 10000, "max_attempts": 8}
	rotated := map[string]any{"id": "whk_1", "url": "https://example.com/h", "secret": "whsec_newsecretvalue1234", "events": []string{"*"},
		"collection_ids": []string{}, "active": true}
	f.on("GET /v1/webhooks/whk_1", 200, masked)
	f.handle("PATCH /v1/webhooks/whk_1", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = decodeBody(r, &in)
		if in["rotate_secret"] == true {
			writeJSON(w, 200, rotated)
			return
		}
		writeJSON(w, 200, masked)
	})
	f.on("GET /v1/collections/Blog", 200, collection("col_1", "Blog"))

	mustContain(t, f.run("webhooks", "get", "whk_1").mustCode(t, 0).out, "https://example.com/h", "10000 ms", "all")
	r := f.run("webhooks", "update", "whk_1", "--disable", "--events", "item.*", "--collection", "Blog", "--description", "").mustCode(t, 0)
	if strings.Contains(r.out, "secret") {
		t.Fatalf("no secret without --rotate-secret: %s", r.out)
	}
	b := f.last("PATCH", "/v1/webhooks/whk_1").json(t)
	if b["active"] != false || b["events"].([]any)[0] != "item.*" || b["collection_ids"].([]any)[0] != "col_1" || b["description"] != "" {
		t.Fatalf("body = %v", b)
	}
	if _, ok := b["rotate_secret"]; ok {
		t.Fatal("rotate_secret only when asked")
	}
	r = f.run("webhooks", "update", "whk_1", "--rotate-secret").mustCode(t, 0)
	mustContain(t, r.out, "whsec_newsecretvalue1234", "not shown again")
	f.run("webhooks", "update", "whk_1").mustCode(t, ExitUsage)
	f.run("webhooks", "update", "whk_1", "--collection", "Blog", "--all-collections").mustCode(t, ExitUsage)
}

func TestNewCommandsInHelpAndCompletion(t *testing.T) {
	f := newFake(t)
	r := f.run("--help").mustCode(t, 0)
	mustContain(t, r.out, "channels", "media", "workspace", "plans", "sessions", "connect")
	b := f.run("completion", "bash").mustCode(t, 0).out
	mustContain(t, b, "bulk", "purge", "versions", "rotate", "deliveries")
	if len(f.calls) != 0 {
		t.Fatal("help must not call the API")
	}
}

func decodeBody(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

func readAllString(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestBillingCommands(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/billing", 200, map[string]any{"enabled": true, "plan": "pro", "subscription_status": "active",
		"plan_expires_at": "2026-11-16T09:00:00.000000Z", "suspended": false, "has_customer": true})
	f.on("POST /v1/billing/checkout", 200, map[string]string{"url": "https://checkout.stripe.com/c/pay/cs_test_a1b2c3"})
	mustContain(t, f.run("billing", "status").mustCode(t, 0).out, "active")
	mustContain(t, f.run("billing", "checkout").mustCode(t, 0).out, "https://checkout.stripe.com/c/pay/cs_test_a1b2c3")
}
