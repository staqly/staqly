package storetest_test

import (
	"context"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// A new path gets the migrated template (every migration recorded, River's tables there); a path that already
// holds a database is opened as it is, never overwritten by the template.
func TestOpenTemplateAndReopen(t *testing.T) {
	if storetest.Postgres() {
		t.Skip("SQLite template only")
	}
	ctx := context.Background()
	path := t.TempDir() + "/w.db"
	first, err := storetest.Open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := first.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version").Scan(&version); err != nil || version < 55 {
		t.Fatalf("goose version %d: %v", version, err)
	}
	if _, err := first.ExecContext(ctx, "SELECT COUNT(*) FROM river_job"); err != nil {
		t.Fatalf("river tables: %v", err)
	}
	if _, err := first.ExecContext(ctx, "CREATE TABLE kept (x INTEGER)"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	again, err := storetest.Open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if _, err := again.ExecContext(ctx, "SELECT COUNT(*) FROM kept"); err != nil {
		t.Fatalf("reopened database lost its table: %v", err)
	}

	other, err := storetest.Open(t, t.TempDir()+"/other.db")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.ExecContext(ctx, "SELECT COUNT(*) FROM kept"); err == nil {
		t.Fatal("a new database shares another test's table")
	}
}
