package store

import (
	"context"
	"slices"
	"testing"
)

// import-sqlite copies the tables in importOrder only: every application table must be there (a missing one
// was dropped from the import, or broke it with a foreign key), after the tables it references.
func TestImportOrderCoversEveryTable(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/x.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		AND name NOT LIKE 'river_%' AND name <> 'goose_db_version' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	for _, n := range tables {
		if !slices.Contains(importOrder, n) {
			t.Errorf("table %s is not in importOrder: import-sqlite would not copy it", n)
		}
	}
	fks, err := db.QueryContext(ctx, `SELECT m.name, p."table" FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) p WHERE m.type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer fks.Close()
	for fks.Next() {
		var from, to string
		if err := fks.Scan(&from, &to); err != nil {
			t.Fatal(err)
		}
		if i, j := slices.Index(importOrder, from), slices.Index(importOrder, to); i >= 0 && j >= 0 && j > i {
			t.Errorf("%s references %s but comes before it in importOrder", from, to)
		}
	}
}
