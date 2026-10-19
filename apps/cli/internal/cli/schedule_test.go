package cli

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	got, err := parseWhen("2026-11-03T09:00:00+01:00")
	if err != nil || got != "2026-11-03T08:00:00Z" {
		t.Fatalf("rfc3339: %q %v", got, err)
	}
	day, err := parseWhen("2026-11-03")
	if err != nil || !strings.HasSuffix(day, "Z") {
		t.Fatalf("day: %q %v", day, err)
	}
	if local, _ := time.ParseInLocation("2006-01-02", "2026-11-03", time.Local); local.UTC().Format(time.RFC3339) != day {
		t.Fatalf("a day is local midnight: %s", day)
	}
	rel, err := parseWhen("+2h")
	if err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(time.RFC3339, rel)
	if d := time.Until(at); d < 119*time.Minute || d > 121*time.Minute {
		t.Fatalf("+2h: %s", d)
	}
	for _, bad := range []string{"tomorrow", "+2", "+x3d", "+0d", "2026-13-40"} {
		if _, err := parseWhen(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestScheduleCommand(t *testing.T) {
	f := newFake(t)
	a := post("pst_1", "scheduled")
	a["publish_at"] = "2026-11-03T09:00:00.000Z"
	b := post("pst_2", "published")
	b["publish_at"] = "2026-10-01T09:00:00.000Z"
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{a, b}})
	r := f.run("schedule", "--collection", "Posts", "--days", "30").mustCode(t, 0)
	q := url.Values(f.last("GET", "/v1/posts").Query)
	if q.Get("sort") != "publish_asc" || q.Get("status") != "scheduled" || q.Get("collection_id") != "Posts" || q.Get("publish_before") == "" {
		t.Fatalf("query: %v", q)
	}
	mustContain(t, r.out, "PUBLISH AT", "pst_1", "2026-11-03", "Brand on Mastodon", "Posts")
	// --all: every post with a time, no status filter.
	f.run("schedule", "--all").mustCode(t, 0)
	q = url.Values(f.last("GET", "/v1/posts").Query)
	if q.Get("status") != "" || q.Get("publish_before") != "" || q.Get("publish_after") == "" {
		t.Fatalf("--all query: %v", q)
	}
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{}})
	mustContain(t, f.run("schedule").mustCode(t, 0).err, "Nothing scheduled in the next 7 days")
	f.run("schedule", "--days", "0").mustCode(t, ExitUsage)
}
