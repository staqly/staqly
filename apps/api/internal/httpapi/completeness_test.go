package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// workspaceTables lists every table that holds rows of a workspace (a workspace_id column), outside the job
// queue's own tables.
func workspaceTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	q := `SELECT m.name FROM sqlite_master m JOIN pragma_table_info(m.name) c ON c.name = 'workspace_id'
		WHERE m.type = 'table' AND m.name NOT LIKE 'river_%' ORDER BY m.name`
	if storetest.Postgres() {
		q = `SELECT table_name FROM information_schema.columns WHERE table_schema = current_schema()
			AND column_name = 'workspace_id' AND table_name NOT LIKE 'river_%' ORDER BY table_name`
	}
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if len(out) < 40 {
		t.Fatalf("found %d tables with a workspace_id, the schema has over 40: %v", len(out), out)
	}
	return out
}

// declaredFor checks a table of decisions against the schema: every table with a workspace_id has one, and
// every decision names a table that exists.
func declaredFor(t *testing.T, what string, tables []string, decisions map[string]string) {
	t.Helper()
	for _, tb := range tables {
		if decisions[tb] == "" {
			t.Errorf("table %s holds a workspace's rows and %s does not say what happens to them: add it to the table in this test", tb, what)
		}
	}
	for tb := range decisions {
		if !slices.Contains(tables, tb) {
			t.Errorf("%s names table %s, which does not exist (or has no workspace_id) any more", what, tb)
		}
	}
}

// exportPlan: for every table that holds a workspace's rows, the file of the workspace export its rows are in,
// or why they are left out. A new table needs a line here; the test fails until it has one.
var exportPlan = map[string]string{
	"collections":            "collections.ndjson",
	"posts":                  "posts.ndjson",
	"post_responses":         "responses.ndjson",
	"post_response_insights": "response_insights.ndjson",
	"events":                 "events.ndjson",
	"media":                  "media.ndjson",
	"webhooks":               "webhooks.json",
	"channels":               "channels.json",
	"schedules":              "schedules.json",
	"post_comments":          "post_comments.ndjson",
	"media_folders":          "media_folders.ndjson",
	"channel_groups":         "channel_groups.ndjson",

	// Left out by the owner's decision (2026-11-13).
	"media_projects":        "left out: the editors' working files, in their own format; what they produce is media",
	"channel_links":         "left out: invitation links to connect a channel are access, not content",
	"notification_channels": "left out: alert destinations hold sealed secrets (webhook URLs, bot tokens)",
	"assistant_chats":       "left out: conversations with the assistant are scratch work, not the workspace's content",
	"agents":                "left out: connected agents are access (they act with keys), not content",
	"preview_links":         "left out: share links are access, not content",

	// Left out by design: secrets, people, billing and bookkeeping.
	"api_keys":                          "left out: credentials",
	"sessions":                          "left out: credentials",
	"device_links":                      "left out: credentials (one-time sign-in links)",
	"provider_apps":                     "left out: developer app secrets",
	"pending_connections":               "left out: sign-ins in progress, with their tokens",
	"telegram_connect_codes":            "left out: one-time codes",
	"workspace_members":                 "left out: people, not the workspace's content",
	"workspace_invitations":             "left out: people, not the workspace's content",
	"webhook_deliveries":                "left out: delivery attempts; the events they carried are exported",
	"notification_outbox":               "left out: alerts on their way; the events they tell about are exported",
	"idempotency_keys":                  "left out: request bookkeeping",
	"usage_counters":                    "left out: metering",
	"usage_events":                      "left out: metering",
	"credit_grants":                     "left out: billing",
	"workspace_trials":                  "left out: billing",
	"trial_fingerprints":                "left out: abuse prevention",
	"limit_workspaces":                  "left out: the operator's limits for the workspace",
	"publishing_allowance_reservations": "left out: quota held by sends in progress",
	"ai_jobs":                           "left out: AI requests; their results are media files",
	"channel_activity":                  "left out: numbers read from the platforms, read again any time",
	"product_feedback":                  "left out: feedback sent to staqly",
	"workspace_exports":                 "left out: the exports themselves",
	"jobs":                              "left out: the queue before River, empty after every start",
	"job_requeues":                      "left out: the job queue",
	"installation":                      "left out: the install's own row",
	"admin_audit":                       "left out: the operator's audit log",
}

// Every table with a workspace's rows is in the export or says why not, and every file the plan names is in a
// real archive, described in its README.
func TestWorkspaceExportCoversEveryTable(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	declaredFor(t, "the workspace export", workspaceTables(t, e.svc.DB), exportPlan)

	c := e.collection("Everything")
	post := e.post(c.ID, map[string]any{"content": "one"})
	var comment app.PostComment
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/comments", e.key, map[string]any{"body": "Shorter, please"}, 201, &comment)
	var folder app.MediaFolder
	e.do(nil, "POST", "/v1/media-folders", e.key, map[string]any{"name": "Logos"}, 201, &folder)
	var m app.Media
	e.upload("/v1/media", e.key, nil, "logo.png", pngBytes, 201, &m)
	e.do(nil, "POST", "/v1/media/bulk", e.key, map[string]any{"ids": []string{m.ID}, "folder_id": folder.ID}, 200, nil)
	var group app.ChannelGroup
	e.do(nil, "POST", "/v1/channel-groups", e.admin, map[string]any{"name": "Client A", "color": "#22c55e"}, 201, &group)
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": "Bot", "group_id": group.ID}, 201, &ch)
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, "X-CSRF-Token", e.csrf)
	e.svc.ProcessExports(context.Background())
	st, _, body := e.raw(nil, "GET", "/v1/workspace/exports/"+job.ID+"/download", e.keyWith(app.ScopeAdmin), nil)
	if st != 200 {
		t.Fatalf("download: %d %s", st, body)
	}
	files := readZip(t, body)
	readme := string(files["README.txt"])
	for tb, where := range exportPlan {
		if strings.HasPrefix(where, "left out: ") {
			continue
		}
		if _, ok := files[where]; !ok {
			t.Errorf("table %s is exported to %s, which is not in the archive", tb, where)
		}
	}
	for name := range files {
		if name != "README.txt" && !strings.HasPrefix(name, "media/") && !strings.Contains(readme, name) {
			t.Errorf("the archive has %s, its README does not mention it", name)
		}
	}

	// What the new files carry: the comment on its post, the folder and the file in it, the group and its channel.
	lines := func(name string) []map[string]any {
		t.Helper()
		var out []map[string]any
		for _, l := range strings.Split(strings.TrimSpace(string(files[name])), "\n") {
			if l == "" {
				continue
			}
			var v map[string]any
			if err := json.Unmarshal([]byte(l), &v); err != nil {
				t.Fatalf("%s: %v in %q", name, err, l)
			}
			out = append(out, v)
		}
		return out
	}
	if got := lines("post_comments.ndjson"); len(got) != 1 || got[0]["id"] != comment.ID || got[0]["post_id"] != post.ID || got[0]["body"] != "Shorter, please" {
		t.Errorf("post_comments.ndjson: %v, want the one comment on its post", got)
	}
	if got := lines("media_folders.ndjson"); len(got) != 1 || got[0]["id"] != folder.ID || got[0]["name"] != "Logos" || got[0]["parent_id"] != "" {
		t.Errorf("media_folders.ndjson: %v, want the Logos folder at the top level", got)
	}
	inFolder := false
	for _, l := range lines("media.ndjson") {
		inFolder = inFolder || (l["id"] == m.ID && l["folder_id"] == folder.ID)
	}
	if !inFolder {
		t.Errorf("media.ndjson does not put %s in folder %s: %s", m.ID, folder.ID, files["media.ndjson"])
	}
	groups := map[string]map[string]any{}
	for _, g := range lines("channel_groups.ndjson") {
		groups[g["id"].(string)] = g
	}
	if g := groups[group.ID]; g == nil || g["name"] != "Client A" || g["color"] != "#22c55e" {
		t.Errorf("channel_groups.ndjson: %v, want Client A with its color", groups)
	}
	var chans []map[string]any
	if err := json.Unmarshal(files["channels.json"], &chans); err != nil {
		t.Fatal(err)
	}
	grouped := false
	for _, c := range chans {
		grouped = grouped || (c["id"] == ch.ID && c["group_id"] == group.ID)
		if _, ok := c["credentials"]; ok {
			t.Errorf("channels.json carries credentials: %v", c)
		}
	}
	if !grouped {
		t.Errorf("channels.json does not put %s in group %s: %s", ch.ID, group.ID, files["channels.json"])
	}
	var meta struct{ Counts map[string]int }
	if err := json.Unmarshal(files["workspace.json"], &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Counts["post_comments"] != 1 || meta.Counts["media_folders"] != 1 || meta.Counts["channel_groups"] != len(groups) {
		t.Errorf("workspace.json counts %v", meta.Counts)
	}
}

// deletionPlan: how the rows of every table leave when their workspace is deleted.
//
//	cascade:  the table's workspace_id references the workspace ON DELETE CASCADE (checked in the schema)
//	explicit: deleteWorkspace deletes them itself, or they go with the post they belong to (checked with a row)
//	kept:     they stay, on purpose (checked with a row)
var deletionPlan = map[string]string{
	"installation":           "explicit",
	"job_requeues":           "explicit",
	"post_responses":         "explicit",
	"post_response_insights": "explicit",
	"jobs":                   "kept: the queue before River; nothing writes to it and every start empties it",
	"trial_fingerprints":     "kept: a deleted workspace's card and address still count against a second free trial",
	"admin_audit":            "kept: the operator's record of what was done to the workspace",
}

func cascadesFromWorkspace(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	q := `SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE "table" = 'workspaces' AND "from" = 'workspace_id' AND on_delete = 'CASCADE'`
	if storetest.Postgres() {
		q = `SELECT COUNT(*) FROM information_schema.referential_constraints rc
			JOIN information_schema.key_column_usage k ON k.constraint_schema = rc.constraint_schema AND k.constraint_name = rc.constraint_name
			JOIN information_schema.constraint_column_usage u ON u.constraint_schema = rc.constraint_schema AND u.constraint_name = rc.constraint_name
			WHERE rc.constraint_schema = current_schema() AND k.table_name = ? AND k.column_name = 'workspace_id'
			AND u.table_name = 'workspaces' AND rc.delete_rule = 'CASCADE'`
	}
	var n int
	if err := db.QueryRow(q, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// Deleting a workspace empties every table that holds its rows: by the schema's cascade, or by a declared
// exception that is checked with a row. A new table without a cascade and without a line above fails here.
func TestWorkspaceDeletionEmptiesEveryTable(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	db := e.svc.DB
	tables := workspaceTables(t, db)
	for _, tb := range tables {
		plan, cascades := deletionPlan[tb], cascadesFromWorkspace(t, db, tb)
		switch {
		case plan == "" && !cascades:
			t.Errorf("table %s holds a workspace's rows without ON DELETE CASCADE to workspaces: deleting the workspace leaves them. Add the cascade, or delete them in deleteWorkspace and say so in this test", tb)
		case plan != "" && cascades:
			t.Errorf("table %s cascades from the workspace; its line in this test (%q) is out of date", tb, plan)
		}
	}
	for tb := range deletionPlan {
		if !slices.Contains(tables, tb) {
			t.Errorf("the deletion plan names table %s, which does not exist (or has no workspace_id) any more", tb)
		}
	}

	// A workspace with a row in every table that does not cascade, and the usual rows in those that do.
	ws := workspaceID(e)
	ctx := context.Background()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	c := e.collection("Doomed")
	ch := e.customChannel("Bot")
	var m app.Media
	e.upload("/v1/media", e.key, nil, "a.png", pngBytes, 201, &m)
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h", "events": []string{"post.*"}}, 201, nil)
	post := e.post(c.ID, map[string]any{"content": "pic", "media": []string{m.ID}, "targets": []map[string]any{{"channel_id": ch.ID}},
		"status": "scheduled", "publish_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err := e.svc.QueueJob(ctx, ws, "check_target", "check_target:"+post.ID+":"+ch.ID, map[string]any{"post_id": post.ID, "channel_id": ch.ID}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO installation (id, workspace_id, created_at) VALUES (1, ?, ?)`, []any{ws, now}},
		{`INSERT INTO job_requeues (key, workspace_id, kind, run_at, created_at) VALUES ('check_target:x', ?, 'check_target', ?, ?)`, []any{ws, now, now}},
		{`INSERT INTO post_responses (workspace_id, post_id, channel_id, id, created_at, fetched_at) VALUES (?, ?, ?, 'r1', ?, ?)`, []any{ws, post.ID, ch.ID, now, now}},
		{`INSERT INTO post_response_insights (workspace_id, post_id, channel_id, analyzed_at) VALUES (?, ?, ?, ?)`, []any{ws, post.ID, ch.ID, now}},
		{`INSERT INTO jobs (id, workspace_id, kind, key, run_at, created_at, updated_at) VALUES ('job_old', ?, 'publish_post', 'publish_post:old', ?, ?, ?)`, []any{ws, now, now, now}},
		{`INSERT INTO trial_fingerprints (kind, fingerprint, workspace_id, created_at, updated_at) VALUES ('card', 'fp', ?, ?, ?)`, []any{ws, now, now}},
		{`INSERT INTO admin_audit (id, email, action, workspace_id, created_at) VALUES ('aud_1', 'ops@example.com', 'set_plan', ?, ?)`, []any{ws, now}},
	} {
		if _, err := db.Exec(stmt.sql, stmt.args...); err != nil {
			t.Fatalf("setup: %s: %v", stmt.sql, err)
		}
	}
	count := func(tb string) (n int) {
		t.Helper()
		if err := db.QueryRow(`SELECT COUNT(*) FROM `+tb+` WHERE workspace_id = ?`, ws).Scan(&n); err != nil {
			t.Fatalf("%s: %v", tb, err)
		}
		return n
	}
	queued := func() (n int) {
		t.Helper()
		q := `SELECT COUNT(*) FROM river_job WHERE json_extract(args, '$.workspace_id') = ?`
		if storetest.Postgres() {
			q = `SELECT COUNT(*) FROM river_job WHERE args->>'workspace_id' = $1`
		}
		if err := db.QueryRow(q, ws).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for tb := range deletionPlan {
		if count(tb) == 0 {
			t.Fatalf("setup: no row in %s for the workspace", tb)
		}
	}
	seeded := 0
	for _, tb := range tables {
		if count(tb) > 0 {
			seeded++
		}
	}
	if seeded < 15 || queued() < 2 {
		t.Fatalf("setup: rows in %d tables and %d queued jobs: too little to tell a deletion that works from one that does not", seeded, queued())
	}

	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, "X-CSRF-Token", e.csrf)

	for _, tb := range tables {
		n, kept := count(tb), strings.HasPrefix(deletionPlan[tb], "kept: ")
		if kept && n == 0 {
			t.Errorf("%s: the deleted workspace's row is gone; the plan says it is %s", tb, deletionPlan[tb])
		}
		if !kept && n != 0 {
			t.Errorf("%s: %d rows of the deleted workspace are left", tb, n)
		}
	}
	if n := queued(); n != 0 {
		t.Errorf("%d jobs of the deleted workspace are left in the queue", n)
	}
}
