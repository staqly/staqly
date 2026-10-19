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
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"
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
	if err := baselineSquash(ctx, db); err != nil {
		return err
	}
	// Each goose migration runs in its own transaction, so an interrupted
	// startup leaves the schema at the last fully applied version.
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return migrateRiver(ctx, db)
}

// squashedVersion is the last migration before the history was squashed into 00001_init.sql (2026-10-20,
// before the first release): a database at this version has exactly the squashed schema.
const squashedVersion = 47

// baselineSquash lets a database made before the squash run on: at version 47 it already has 00001_init's
// schema, so version 1 is recorded as applied (PostgreSQL databases started at 27 and never had it). One
// stopped part way (2-46) can't be upgraded and is refused; a new database has no goose table yet.
func baselineSquash(ctx context.Context, db *sql.DB) error {
	var max sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version").Scan(&max); err != nil || !max.Valid || max.Int64 <= 1 {
		return nil // no goose table yet (a new database), or only the init
	}
	if max.Int64 < squashedVersion {
		return fmt.Errorf("migrate: this database is at version %d, from before the migrations were squashed; "+
			"only databases at version %d or newer can be opened (start a new one)", max.Int64, squashedVersion)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1").Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := db.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (1, true)")
	return err
}

// migrateRiver applies the job queue's own schema (River: river_job, river_leader, river_queue, ...),
// after the application's migrations, with River's migrator for the database's dialect.
func migrateRiver(ctx context.Context, db *sql.DB) error {
	var err error
	if IsPostgres(db) {
		var m *rivermigrate.Migrator[*sql.Tx]
		if m, err = rivermigrate.New(riverdatabasesql.New(db), nil); err == nil {
			_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
		}
	} else {
		var m *rivermigrate.Migrator[*sql.Tx]
		if m, err = rivermigrate.New(riversqlite.New(db), nil); err == nil {
			_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
		}
	}
	if err != nil {
		return fmt.Errorf("migrate job queue: %w", err)
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
		return errors.New("cloud mode runs on PostgreSQL: back it up with pg_dump (https://docs.staqly.ai/self-hosting/backup-and-restore), e.g. " +
			"pg_dump --format=custom --file=staqly.dump \"$DATABASE_URL\"")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	_, err := db.ExecContext(ctx, "VACUUM INTO ?", dest)
	return err
}
