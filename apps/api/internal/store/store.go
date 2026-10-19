// Package store opens the database and runs its migrations: SQLite in local mode (this file),
// PostgreSQL in cloud mode (postgres.go).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

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
	// workspace through one index instead of using the collection one.
	// 0x10002 = also analyze tables never analyzed (fast with the default analysis_limit).
	if _, err := db.ExecContext(ctx, "PRAGMA analysis_limit=400; PRAGMA optimize=0x10002"); err != nil {
		db.Close()
		return nil, fmt.Errorf("optimize database: %w", err)
	}
	return db, nil
}

// gooseMu guards goose's package-level settings (base FS, logger, dialect): opening two databases at once
// (parallel tests, tools) raced on them.
var gooseMu sync.Mutex

// Migrate applies the migrations of the database's dialect.
func Migrate(ctx context.Context, db *sql.DB) error {
	if IsPostgres(db) {
		return MigratePostgres(ctx, db)
	}
	gooseMu.Lock()
	defer gooseMu.Unlock()
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

// OpenFor opens PostgreSQL when url is set (cloud mode), else the SQLite file at path (local mode).
func OpenFor(ctx context.Context, url, path string) (*sql.DB, error) {
	if url != "" {
		return OpenPostgres(ctx, url)
	}
	return Open(ctx, path)
}

// Backup writes a consistent snapshot of the live database to dest using
// VACUUM INTO, which is safe under WAL with concurrent writers. PostgreSQL is backed up with its own
// tools (pg_dump), never by this server.
func Backup(ctx context.Context, db *sql.DB, dest string) error {
	if IsPostgres(db) {
		return errors.New("cloud mode runs on PostgreSQL: back it up with pg_dump (docs/backup.md), e.g. " +
			"pg_dump --format=custom --file=staqly.dump \"$DATABASE_URL\"")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	_, err := db.ExecContext(ctx, "VACUUM INTO ?", dest)
	return err
}
