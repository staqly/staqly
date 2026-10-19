package cli

import (
	"net/http"
	"strings"
	"testing"
)

func provider(id, auth, status string, app bool) map[string]any {
	p := providerWithFields(id, auth, status, app)
	if auth != "token" {
		p["credential_fields"] = []any{}
	}
	return p
}

func providerWithFields(id, auth, status string, app bool) map[string]any {
	return map[string]any{"id": id, "name": strings.ToUpper(id[:1]) + id[1:], "auth": auth, "status": status, "app_configured": app,
		"limits": map[string]any{"max_chars": 500, "max_media": 4, "media_kinds": []any{"image"}, "needs_title": false, "needs_media": false, "needs_link": false},
		"credential_fields": []any{map[string]any{"key": "bot_token", "label": "Bot token", "kind": "secret", "required": true},
			map[string]any{"key": "chat_id", "label": "Chat", "kind": "text", "required": true}},
		"settings_fields": []any{map[string]any{"key": "visibility", "label": "Visibility", "kind": "select", "required": false, "default": "public",
			"options": []any{map[string]any{"value": "public", "label": "Public"}}}},
		"docs_url": "https://docs.staqly.ai/channels/" + id}
}

func TestChannels(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/providers", 200, map[string]any{"data": []any{
		provider("telegram", "token", "available", false), provider("mastodon", "oauth2", "available", true),
		provider("custom", "none", "available", false), provider("linkedin", "oauth2", "soon", false)}})
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "mastodon", "Brand")}})
	f.on("GET /v1/channel-groups", 200, map[string]any{"data": []any{
		map[string]any{"id": "grp_def", "name": "Default", "color": "", "position": 0, "default": true, "channels": 0},
		map[string]any{"id": "grp_a", "name": "Client A", "color": "", "position": 1, "default": false, "channels": 1}}})
	f.on("POST /v1/channel-groups", 201, map[string]any{"id": "grp_b", "name": "Client B", "color": "", "position": 2, "default": false, "channels": 0})
	f.on("PATCH /v1/channel-groups/grp_a", 200, map[string]any{"id": "grp_a", "name": "Client Alpha", "color": "", "position": 1, "default": false, "channels": 1})
	f.handle("DELETE /v1/channel-groups/grp_a", noContent)
	f.on("GET /v1/channels/ch_1", 200, channel("ch_1", "mastodon", "Brand"))
	f.on("POST /v1/channels", 201, channel("ch_2", "telegram", "News"))
	f.on("PATCH /v1/channels/ch_1", 200, channel("ch_1", "mastodon", "Renamed"))
	f.handle("DELETE /v1/channels/ch_1", noContent)
	f.seq("POST /v1/channels/ch_1/test",
		respond(200, map[string]any{"ok": true, "profile": map[string]any{"handle": "@brand"}}),
		respond(200, map[string]any{"ok": false, "error": "401 from Mastodon"}))
	f.on("GET /v1/channels/oauth/mastodon/start", 200, map[string]any{"url": "https://mastodon.social/oauth/authorize?x=1"})
	f.handle("PUT /v1/providers/mastodon/app", noContent)
	f.handle("DELETE /v1/providers/mastodon/app", noContent)

	r := f.run("channels", "providers").mustCode(t, 0)
	mustContain(t, r.out, "telegram", "token", "mastodon", "oauth2", "configured", "missing", "soon", "500")
	r = f.run("channels", "providers", "telegram").mustCode(t, 0)
	mustContain(t, r.out, "bot_token", "Bot token", "secret", "visibility", "public")
	f.run("channels", "providers", "nope").mustCode(t, ExitError)

	mustContain(t, f.run("channels", "list").mustCode(t, 0).out, "ch_1", "Brand", "mastodon", "@brand", "connected", "Client A", "2026-10-14")
	f.run("channels", "list", "--group", "client a").mustCode(t, 0)
	if q := f.last("GET", "/v1/channels").Query["group"]; len(q) != 1 || q[0] != "grp_a" {
		t.Fatalf("group filter sent as %q", q)
	}
	f.run("channels", "list", "--group", "Nope").mustCode(t, ExitError)
	mustContain(t, f.run("channels", "get", "Brand").mustCode(t, 0).out, "ch_1", "Brand", "connected", "grp_a", "Posts, 30 days")

	// Channel groups.
	mustContain(t, f.run("channels", "groups", "list").mustCode(t, 0).out, "grp_def", "Default", "yes", "Client A")
	mustContain(t, f.run("channels", "groups", "create", "Client B", "--color", "#22c55e").mustCode(t, 0).out, "Client B", "grp_b")
	if b := f.last("POST", "/v1/channel-groups").json(t); b["name"] != "Client B" || b["color"] != "#22c55e" {
		t.Fatalf("create group: %v", b)
	}
	mustContain(t, f.run("channels", "groups", "rename", "Client A", "Client Alpha").mustCode(t, 0).out, "Client Alpha")
	f.run("channels", "groups", "remove", "grp_a").mustCode(t, 0)
	if len(f.find("DELETE", "/v1/channel-groups/grp_a")) != 1 {
		t.Fatal("group not removed")
	}

	// Token platform: fields from flags and stdin; required fields are checked locally.
	r = f.runWith(runOpts{stdin: `{"bot_token":"123:abc"}`}, "channels", "connect", "telegram", "--name", "News",
		"--fields-json", "-", "--field", "chat_id=@news", "--setting", "silent=true").mustCode(t, 0)
	mustContain(t, r.out, "Connected telegram News")
	b := f.last("POST", "/v1/channels").json(t)
	fields := b["fields"].(map[string]any)
	if b["provider"] != "telegram" || b["name"] != "News" || fields["bot_token"] != "123:abc" || fields["chat_id"] != "@news" ||
		b["settings"].(map[string]any)["silent"] != true {
		t.Fatalf("body = %v", b)
	}
	f.run("channels", "connect", "telegram", "--field", "chat_id=@news").mustCode(t, ExitUsage)
	f.run("channels", "connect", "linkedin").mustCode(t, ExitError)
	f.run("channels", "connect", "nope").mustCode(t, ExitError)

	// Your own automation takes no fields.
	f.run("channels", "connect", "custom", "--name", "Pipeline").mustCode(t, 0)
	if b := f.last("POST", "/v1/channels").json(t); b["provider"] != "custom" || b["fields"] != nil {
		t.Fatalf("custom body = %v", b)
	}

	// OAuth: a sign-in link on stdout.
	r = f.run("channels", "connect", "mastodon", "--instance", "mastodon.social").mustCode(t, 0)
	if strings.TrimSpace(r.out) != "https://mastodon.social/oauth/authorize?x=1" {
		t.Fatalf("stdout = %q", r.out)
	}
	if q := f.last("GET", "/v1/channels/oauth/mastodon/start").Query; q["instance"][0] != "mastodon.social" {
		t.Fatalf("query = %v", q)
	}

	f.run("channels", "update", "ch_1", "--name", "Renamed", "--setting", "visibility=unlisted", "--disable").mustCode(t, 0)
	b = f.last("PATCH", "/v1/channels/ch_1").json(t)
	if b["name"] != "Renamed" || b["status"] != "disabled" || b["settings"].(map[string]any)["visibility"] != "unlisted" {
		t.Fatalf("body = %v", b)
	}
	f.run("channels", "update", "Brand", "--enable", "--field", "bot_token=new").mustCode(t, 0)
	b = f.last("PATCH", "/v1/channels/ch_1").json(t)
	if b["status"] != "connected" || b["fields"].(map[string]any)["bot_token"] != "new" {
		t.Fatalf("body = %v", b)
	}
	f.run("channels", "update", "ch_1", "--group", "Client A").mustCode(t, 0)
	if b := f.last("PATCH", "/v1/channels/ch_1").json(t); b["group_id"] != "grp_a" {
		t.Fatalf("move to group: %v", b)
	}
	f.run("channels", "update", "ch_1").mustCode(t, ExitUsage)
	f.run("channels", "update", "ch_1", "--enable", "--disable").mustCode(t, ExitUsage)

	mustContain(t, f.run("channels", "test", "ch_1").mustCode(t, 0).out, "Connected as @brand")
	mustContain(t, f.run("channels", "test", "ch_1").mustCode(t, ExitError).err, "401 from Mastodon")
	f.run("channels", "remove", "Brand").mustCode(t, 0)
	if len(f.find("DELETE", "/v1/channels/ch_1")) != 1 {
		t.Fatal("remove must DELETE")
	}

	f.runWith(runOpts{stdin: "s3cret\n"}, "channels", "app", "mastodon", "--client-id", "abc", "--client-secret", "-").mustCode(t, 0)
	if b := f.last("PUT", "/v1/providers/mastodon/app").json(t); b["client_id"] != "abc" || b["client_secret"] != "s3cret" {
		t.Fatalf("app body = %v", b)
	}
	f.run("channels", "app", "mastodon", "--remove").mustCode(t, 0)
	f.run("channels", "app", "mastodon").mustCode(t, ExitUsage)
}

func TestSchedules(t *testing.T) {
	f := newFake(t)
	sched := map[string]any{"id": "sch_1", "collection_id": "col_1", "collection_name": "Posts", "name": "Mornings", "channels": []any{"ch_1"},
		"channel_names": []any{"Brand"}, "kind": "recurring", "run_at": nil, "rule": map[string]any{"times": []any{"09:00"}, "weekdays": []any{1, 2, 3, 4, 5}, "tz": "Europe/Warsaw"},
		"pick": "next", "post_ids": []any{}, "source": "agent", "endpoint_url": "", "enabled": true, "next_run_at": "2026-10-14T07:00:00Z",
		"last_run_at": nil, "last_result": "published pst_1", "created_at": "2026-10-13T10:00:00Z", "updated_at": "2026-10-13T10:00:00Z"}
	f.on("GET /v1/schedules", 200, map[string]any{"data": []any{sched}})
	f.on("GET /v1/schedules/sch_1", 200, sched)
	f.on("POST /v1/schedules", 201, sched)
	f.on("PATCH /v1/schedules/sch_1", 200, sched)
	f.handle("DELETE /v1/schedules/sch_1", noContent)
	f.on("POST /v1/schedules/sch_1/run", 202, sched)
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "mastodon", "Brand")}})

	r := f.run("schedules", "list", "--collection", "Posts").mustCode(t, 0)
	mustContain(t, r.out, "sch_1", "Mornings", "09:00 on mon,tue,wed,thu,fri Europe/Warsaw", "next", "agent", "published pst_1")
	if q := f.last("GET", "/v1/schedules").Query; q["collection_id"][0] != "Posts" {
		t.Fatalf("query = %v", q)
	}
	mustContain(t, f.run("schedules", "get", "sch_1").mustCode(t, 0).out, "Mornings", "Brand", "2026-10-14")

	f.run("schedules", "create", "Posts", "--name", "Mornings", "--times", "09:00, 17:30", "--weekdays", "mon,tue", "--tz", "Europe/Warsaw",
		"--channel", "Brand", "--pick", "all", "--source", "agent").mustCode(t, 0)
	b := f.last("POST", "/v1/schedules").json(t)
	rule := b["rule"].(map[string]any)
	if b["collection"] != "Posts" || b["kind"] != "recurring" || len(rule["times"].([]any)) != 2 || rule["weekdays"].([]any)[1] != float64(2) ||
		rule["tz"] != "Europe/Warsaw" || b["channels"].([]any)[0] != "ch_1" || b["pick"] != "all" || b["source"] != "agent" {
		t.Fatalf("body = %v", b)
	}
	f.run("schedules", "create", "Posts", "--name", "Hourly", "--every", "6h", "--endpoint-url", "https://my.app/posts", "--disabled").mustCode(t, 0)
	b = f.last("POST", "/v1/schedules").json(t)
	if b["rule"].(map[string]any)["every_minutes"] != float64(360) || b["source"] != "endpoint" || b["endpoint_url"] != "https://my.app/posts" || b["enabled"] != false {
		t.Fatalf("every body = %v", b)
	}
	f.run("schedules", "create", "Posts", "--name", "Once", "--at", "2026-11-03T09:00:00Z", "--post", "pst_1,pst_2").mustCode(t, 0)
	b = f.last("POST", "/v1/schedules").json(t)
	if b["kind"] != "once" || b["run_at"] != "2026-11-03T09:00:00Z" || b["pick"] != "selected" || len(b["post_ids"].([]any)) != 2 {
		t.Fatalf("once body = %v", b)
	}
	f.run("schedules", "create", "Posts", "--every", "1h").mustCode(t, ExitUsage)                               // no name
	f.run("schedules", "create", "Posts", "--name", "x").mustCode(t, ExitUsage)                                 // no time
	f.run("schedules", "create", "Posts", "--name", "x", "--at", "+1h", "--every", "1h").mustCode(t, ExitUsage) // once and recurring
	f.run("schedules", "create", "Posts", "--name", "x", "--every", "1m").mustCode(t, ExitUsage)                // too often
	f.run("schedules", "create", "Posts", "--name", "x", "--times", "09:00", "--weekdays", "funday").mustCode(t, ExitUsage)
	f.run("schedules", "create", "Posts", "--name", "x", "--every", "1h", "--tz", "UTC").mustCode(t, ExitUsage) // tz needs times

	f.run("schedules", "update", "sch_1", "--disable", "--times", "08:00").mustCode(t, 0)
	b = f.last("PATCH", "/v1/schedules/sch_1").json(t)
	if b["enabled"] != false || b["rule"].(map[string]any)["times"].([]any)[0] != "08:00" || b["collection"] != nil {
		t.Fatalf("update body = %v", b)
	}
	f.run("schedules", "update", "sch_1").mustCode(t, ExitUsage)
	mustContain(t, f.run("schedules", "run", "sch_1").mustCode(t, 0).out, "Queued a run of Mornings")
	f.run("schedules", "delete", "sch_1").mustCode(t, 0)
	if len(f.find("DELETE", "/v1/schedules/sch_1")) != 1 {
		t.Fatal("delete must DELETE")
	}
}

func TestJobsAndWork(t *testing.T) {
	f := newFake(t)
	job := func(id, status string) map[string]any {
		return map[string]any{"id": id, "kind": "publish_post", "key": "publish_post:pst_1", "status": status, "run_at": "2026-10-13T10:00:00Z",
			"attempts": 1, "max_attempts": 3, "lease_until": nil, "leased_by": "", "last_error": "token expired",
			"payload": map[string]any{"post_id": "pst_1"}, "created_at": "2026-10-13T10:00:00Z", "updated_at": "2026-10-13T10:00:00Z", "done_at": nil}
	}
	f.on("GET /v1/jobs", 200, map[string]any{"data": []any{job("job_1", "failed")}, "next_cursor": "n2"})
	f.on("GET /v1/jobs/stats", 200, map[string]any{"queued": 2, "running": 1, "failed": 1, "done_24h": 9,
		"by_kind": map[string]any{"publish_post": map[string]any{"queued": 2, "running": 1, "failed": 1}}})
	f.on("GET /v1/jobs/job_1", 200, job("job_1", "failed"))
	f.on("POST /v1/jobs/job_1/retry", 202, job("job_1", "queued"))
	f.on("POST /v1/jobs/job_1/cancel", 200, job("job_1", "cancelled"))
	f.on("GET /v1/work", 200, map[string]any{"data": []any{
		map[string]any{"kind": "target", "post": post("pst_1", "publishing"), "channel": channel("ch_9", "custom", "Pipeline"), "collection": collection("col_1", "Posts"), "requested_at": "2026-10-13T10:00:00Z"},
		map[string]any{"kind": "posts", "schedule": map[string]any{"id": "sch_1", "name": "Mornings"}, "collection": collection("col_1", "Posts"), "requested_at": "2026-10-13T10:00:00Z"}}})

	r := f.run("jobs", "list", "--status", "failed", "--kind", "publish_post", "--limit", "5").mustCode(t, 0)
	mustContain(t, r.out, "job_1", "publish_post", "failed", "1/3", "pst_1", "token expired")
	mustContain(t, r.err, "--cursor n2")
	if q := f.last("GET", "/v1/jobs").Query; q["status"][0] != "failed" || q["kind"][0] != "publish_post" || q["limit"][0] != "5" {
		t.Fatalf("query = %v", q)
	}
	mustContain(t, f.run("jobs", "stats").mustCode(t, 0).out, "Queued", "2", "Done (24 h)", "9", "publish_post")
	mustContain(t, f.run("jobs", "get", "job_1").mustCode(t, 0).out, "publish_post:pst_1", "1 of 3")
	mustContain(t, f.run("jobs", "retry", "job_1").mustCode(t, 0).out, "Queued job_1 again")
	mustContain(t, f.run("jobs", "cancel", "job_1").mustCode(t, 0).out, "Cancelled job_1")
	r = f.run("work").mustCode(t, 0)
	mustContain(t, r.out, "publish", "pst_1 on ch_9", "staqly posts result pst_1 ch_9", "new posts", "Mornings (sch_1)", `staqly posts create "Posts"`)
	f.on("GET /v1/work", 200, map[string]any{"data": []any{}})
	mustContain(t, f.run("work").mustCode(t, 0).err, "Nothing is waiting for you.")
}

var _ = http.StatusOK
