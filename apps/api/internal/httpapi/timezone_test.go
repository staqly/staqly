package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The workspace time zone: UTC by default, changed with PATCH /v1/workspace (admin, a known IANA name), and
// the zone of a schedule rule sent without one.
func TestWorkspaceTimezone(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.admin, nil, 200, &ws)
	if ws.Timezone != "UTC" {
		t.Fatalf("default timezone %q", ws.Timezone)
	}
	e.do(nil, "PATCH", "/v1/workspace", e.key, map[string]any{"timezone": "Europe/Warsaw"}, 403, nil)
	for _, bad := range []string{"", "Local", "Mars/Olympus", "../etc/passwd"} {
		var errBody struct{ Error app.Error }
		e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"timezone": bad}, 422, &errBody)
		if errBody.Error.Fields["timezone"] == "" {
			t.Fatalf("%q: %+v", bad, errBody.Error)
		}
	}
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"timezone": "Europe/Warsaw"}, 200, &ws)
	if ws.Timezone != "Europe/Warsaw" || ws.Name == "" {
		t.Fatalf("patched: %+v", ws)
	}
	// The name alone leaves the zone as it is.
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"name": "Renamed"}, 200, &ws)
	if ws.Timezone != "Europe/Warsaw" || ws.Name != "Renamed" {
		t.Fatalf("renamed: %+v", ws)
	}
	var evs struct{ Data []app.Event }
	e.do(nil, "GET", "/v1/events?type=workspace.updated", e.admin, nil, 200, &evs)
	found := false
	for _, ev := range evs.Data {
		b, _ := json.Marshal(ev.Data)
		found = found || strings.Contains(string(b), `"timezone":"Europe/Warsaw"`)
	}
	if !found {
		t.Fatalf("no workspace.updated event with the timezone: %+v", evs.Data)
	}

	c := e.collection("Daily")
	ch := e.customChannel("Bot")
	var sc app.Schedule
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Mornings", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"09:00"}}}, 201, &sc)
	if sc.Rule.TZ != "Europe/Warsaw" {
		t.Fatalf("rule without tz: %+v", sc.Rule)
	}
	e.do(nil, "POST", "/v1/schedules", e.key, map[string]any{"collection": c.ID, "name": "Evenings", "kind": "recurring", "channels": []string{ch.ID},
		"rule": map[string]any{"times": []string{"18:00"}, "tz": "America/New_York"}}, 201, &sc)
	if sc.Rule.TZ != "America/New_York" {
		t.Fatalf("rule with tz: %+v", sc.Rule)
	}
}

// A local workspace starts in the machine's zone (TZ here), UTC when that is not an IANA name.
func TestLocalWorkspaceTimezone(t *testing.T) {
	for tz, want := range map[string]string{"America/New_York": "America/New_York", ":Asia/Tokyo": "Asia/Tokyo", "": "UTC", "EST5EDT,M3.2.0,M11.1.0": "UTC"} {
		t.Setenv("TZ", tz)
		_, srv := newLocal(t)
		code, body := localReq(t, srv, "GET", "/v1/workspace", "", "", "")
		var ws app.Workspace
		if err := json.Unmarshal([]byte(body), &ws); code != 200 || err != nil || ws.Timezone != want {
			t.Fatalf("TZ=%q: %d %s, want %s", tz, code, body, want)
		}
	}
}
