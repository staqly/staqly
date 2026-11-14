package store

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"

	dbfiles "github.com/staqly/staqly/apps/api/db"
)

// oldChainRows makes the goose table of a database the old chain took to version top.
func oldChainRows(top int) []int {
	rows := []int{0, 1}
	for v := 48; v <= top; v++ {
		if v != 67 && v != 68 {
			rows = append(rows, v)
		}
	}
	return rows
}

func TestBaselineSquash(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "staqly.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	versions := func() string {
		rows, err := db.QueryContext(ctx, "SELECT version_id FROM goose_db_version ORDER BY version_id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var v int
			_ = rows.Scan(&v)
			out = append(out, fmt.Sprint(v))
		}
		return strings.Join(out, ",")
	}
	setRows := func(rows []int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, "DELETE FROM goose_db_version"); err != nil {
			t.Fatal(err)
		}
		for _, v := range rows {
			if _, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO goose_db_version (version_id, is_applied) VALUES (%d, true)", v)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO installation (id, workspace_id, created_at) VALUES (1, 'ws', 'now')"); err != nil {
		t.Fatal(err)
	}

	// 74: from before v0.13.0, refused and left alone.
	setRows(oldChainRows(74))
	before := versions()
	if err := baselineSquash(ctx, db); err == nil || !strings.Contains(err.Error(), "v0.13.0") {
		t.Fatalf("version 74: err = %v", err)
	}
	if versions() != before {
		t.Fatal("a refused database was changed")
	}

	// 76: rebaselined to 0,1 with its data; again is a no-op.
	setRows(oldChainRows(76))
	for range 2 {
		if err := baselineSquash(ctx, db); err != nil {
			t.Fatal(err)
		}
		if got := versions(); got != "0,1" {
			t.Fatalf("versions = %s, want 0,1", got)
		}
	}
	var ws string
	if err := db.QueryRowContext(ctx, "SELECT workspace_id FROM installation").Scan(&ws); err != nil || ws != "ws" {
		t.Fatalf("data lost: %q %v", ws, err)
	}

	// goose goes on with 00002, and the database is not rewritten again, even once it passes 76 migrations.
	init, err := fs.ReadFile(dbfiles.Migrations, "migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{
		"migrations/00001_init.sql":  {Data: init},
		"migrations/00002_extra.sql": {Data: []byte("-- +goose Up\nCREATE TABLE extra (id INTEGER);\n")},
	}
	gooseMu.Lock()
	defer gooseMu.Unlock()
	goose.SetBaseFS(files)
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		t.Fatal(err)
	}
	if got := versions(); got != "0,1,2" {
		t.Fatalf("versions = %s, want 0,1,2", got)
	}
	var rows []int
	for v := 0; v <= 80; v++ {
		rows = append(rows, v)
	}
	setRows(rows)
	if err := baselineSquash(ctx, db); err != nil {
		t.Fatal(err)
	}
	if got := versions(); !strings.HasSuffix(got, ",79,80") {
		t.Fatalf("a squashed chain was rewritten: %s", got)
	}
}
