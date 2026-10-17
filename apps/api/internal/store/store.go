// Package store opens the SQLite database and runs migrations.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	dbfiles "github.com/staqly/staqly/apps/api/db"
	_ "modernc.org/sqlite"
)

// Open opens (creating if needed) the database at path and applies migrations.
// Pragmas go in the DSN so every pooled connection gets them, not just the first.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(ON)", "busy_timeout(10000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	// BEGIN IMMEDIATE takes the write lock up front, so concurrent writers
	// queue on busy_timeout instead of failing with SQLITE_BUSY on upgrade.
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// ponytail: SQLite has one writer anyway; a small pool keeps reads concurrent under WAL.
	db.SetMaxOpenConns(8)
	db.SetConnMaxIdleTime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	// Without statistics the planner guesses: a collection filter walked the whole
	// workspace through items_workspace_created instead of using items_collection.
	// 0x10002 = also analyze tables never analyzed (fast with the default analysis_limit).
	if _, err := db.ExecContext(ctx, "PRAGMA analysis_limit=400; PRAGMA optimize=0x10002"); err != nil {
		db.Close()
		return nil, fmt.Errorf("optimize database: %w", err)
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(dbfiles.Migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	// Each goose migration runs in its own transaction, so an interrupted
	// startup leaves the schema at the last fully applied version.
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Backup writes a consistent snapshot of the live database to dest using
// VACUUM INTO, which is safe under WAL with concurrent writers. VACUUM may
// renumber rowids, so the copy's full-text index is rebuilt afterwards.
func Backup(ctx context.Context, db *sql.DB, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return err
	}
	cp, err := sql.Open("sqlite", "file:"+dest)
	if err != nil {
		return err
	}
	defer cp.Close()
	_, err = cp.ExecContext(ctx, "INSERT INTO items_fts (items_fts) VALUES ('rebuild')")
	return err
}
