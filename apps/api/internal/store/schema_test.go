package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// TestSchemasMatch checks that db/migrations gives PostgreSQL (through postgresDDL) the schema it gives
// SQLite: the same tables, columns (in the same order, as SELECT * is read by position), types,
// nullability and indexes, and the same migration version. Runs when STAQLY_TEST_DATABASE_URL is set (CI's PostgreSQL job).
func TestSchemasMatch(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("needs STAQLY_TEST_DATABASE_URL")
	}
	lite, err := store.Open(context.Background(), t.TempDir()+"/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	pg, err := storetest.Open(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()

	want, got := sqliteSchema(t, lite), postgresSchema(t, pg)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Fatalf("schemas differ (- SQLite, + PostgreSQL):\n%s", diff(want, got))
	}
}

// The job queue's tables (river_*) are River's own schema, different per dialect by design: not compared.
func sqliteSchema(t *testing.T, db *sql.DB) []string {
	var out []string
	out = append(out, version(t, db))
	tables := names(t, db, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'river_%' AND name <> 'goose_db_version' ORDER BY name`)
	for _, tb := range tables {
		// A primary key column is NOT NULL on PostgreSQL; SQLite reports TEXT PRIMARY KEY as nullable.
		rows, err := db.Query(`SELECT name, type, "notnull" OR pk > 0 FROM pragma_table_info(?) ORDER BY cid`, tb)
		if err != nil {
			t.Fatal(err)
		}
		i := 0
		for rows.Next() {
			var name, typ string
			var notnull int
			_ = rows.Scan(&name, &typ, &notnull)
			i++
			out = append(out, fmt.Sprintf("col %s %d %s %s %v", tb, i, name, kind(typ), notnull == 1))
		}
		rows.Close()
	}
	for _, ix := range names(t, db, `SELECT name || ' ' || tbl_name || ' ' || (CASE WHEN sql LIKE 'CREATE UNIQUE%' THEN 'unique' ELSE 'index' END)
		FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL AND tbl_name NOT LIKE 'river_%' ORDER BY name`) {
		out = append(out, "idx "+ix)
	}
	return out
}

func postgresSchema(t *testing.T, db *sql.DB) []string {
	var out []string
	out = append(out, version(t, db))
	// The position among the table's columns, not ordinal_position: PostgreSQL keeps a dropped column's number
	// (a dropped column), SQLite renumbers.
	rows, err := db.Query(`SELECT table_name, ROW_NUMBER() OVER (PARTITION BY table_name ORDER BY ordinal_position), column_name, data_type, is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name <> 'goose_db_version' AND table_name NOT LIKE 'river_%' ORDER BY table_name, ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tb, name, typ, nullable string
		var pos int
		_ = rows.Scan(&tb, &pos, &name, &typ, &nullable)
		out = append(out, fmt.Sprintf("col %s %d %s %s %v", tb, pos, name, kind(typ), nullable == "NO"))
	}
	rows.Close()
	// Indexes created by CREATE INDEX (not the ones behind PRIMARY KEY / UNIQUE constraints, which
	// SQLite names sqlite_autoindex_* and the comparison skips on both sides).
	for _, ix := range names(t, db, `SELECT i.indexname || ' ' || i.tablename || ' ' || (CASE WHEN i.indexdef LIKE 'CREATE UNIQUE%' THEN 'unique' ELSE 'index' END)
		FROM pg_indexes i WHERE i.schemaname = current_schema() AND i.tablename NOT LIKE 'river_%'
		AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conname = i.indexname) ORDER BY i.indexname`) {
		out = append(out, "idx "+ix)
	}
	return out
}

// kind normalises column types: what the application can tell apart.
func kind(typ string) string {
	switch strings.ToLower(typ) {
	case "integer", "bigint":
		return "int"
	case "blob", "bytea":
		return "bytes"
	}
	return "text"
}

func version(t *testing.T, db *sql.DB) string {
	var v int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("version %d", v)
}

func names(t *testing.T, db *sql.DB, q string) []string {
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func diff(a, b []string) string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, "- "+s)
		}
	}
	for _, s := range b {
		if !slices.Contains(a, s) {
			out = append(out, "+ "+s)
		}
	}
	return strings.Join(out, "\n")
}

// TestImportSQLite moves a populated SQLite database into an empty PostgreSQL one: every row arrives,
// the application works on the copy (including new events after the imported ones), and a second import
// into the now non-empty database is refused without writing anything.
func TestImportSQLite(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("needs STAQLY_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	path := t.TempDir() + "/old.db"
	lite, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	svc := app.New(lite, nil, app.Config{Mode: app.ModeLocal})
	if err := svc.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultCollections(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := svc.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var first app.Post
	for i := range 5 {
		post, err := svc.CreatePost(ctx, p, app.PostInput{Title: "Post", Collection: "Default", Content: fmt.Sprintf("post %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = post
		}
	}
	// A post's queued job (River, in SQLite) has to arrive in the PostgreSQL queue.
	if err := svc.QueueJob(ctx, p.WorkspaceID, "publish_post", "publish_post:"+first.ID, map[string]any{"post_id": first.ID},
		time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// SQLite keeps a NUL character; PostgreSQL text cannot, so the import replaces it.
	if _, err := svc.CreatePost(ctx, p, app.PostInput{Title: "Post", Collection: "Default", Content: "nul\x00byte"}); err != nil {
		t.Fatal(err)
	}
	lite.Close()

	pg, err := storetest.Open(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	report, err := store.ImportSQLite(ctx, pg, path)
	if err != nil {
		t.Fatal(err)
	}
	if report["posts"] != 6 || report["workspaces"] != 1 || report["events"] == 0 || report[store.NULReplaced] == 0 || report["jobs"] != 1 {
		t.Fatalf("report: %v", report)
	}
	moved := app.New(pg, nil, app.Config{Mode: app.ModeLocal})
	mp, err := moved.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := moved.ListPosts(ctx, mp, app.PostFilter{Limit: 50})
	if err != nil || len(page.Data) != 6 {
		t.Fatalf("posts on PostgreSQL: %d %v", len(page.Data), err)
	}
	// The server's queue takes the scheduled post's job over when it starts.
	qctx, stop := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() { moved.RunJobs(qctx); close(stopped) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		jobs, err := moved.ListJobs(ctx, mp, app.JobFilter{Status: "queued"})
		if err == nil && len(jobs.Data) == 1 && jobs.Data[0].Kind == "publish_post" && jobs.Data[0].Payload.PostID == first.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled job not carried into the queue: %+v %v", jobs, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	<-stopped
	if _, err := moved.CreatePost(ctx, mp, app.PostInput{Title: "Post", Collection: "Default", Content: "after the move"}); err != nil {
		t.Fatalf("a new post (and its event, after the imported sequence): %v", err)
	}
	if _, err := store.ImportSQLite(ctx, pg, path); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("a second import must be refused: %v", err)
	}
}
