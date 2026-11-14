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

// oldChainVersion is the last migration of the history that was squashed into 00001_init.sql (2026-11-01).
const oldChainVersion = 76

// baselineSquash lets a database made by the old chain (00001, 00048-00076) run on: at version 76 it has
// exactly 00001_init's schema, so its goose rows are replaced by version 1 and goose goes on with 00002.
//
// Telling an old database from a new one: the old chain skipped numbers (never 67 and 68; a fresh one went
// 1, 48, ...), while a database made by the squashed chain has every version from 1 to its latest. So the
// versions above 0 are counted: a gap means the old chain. A gap below 76 is refused (upgrade to v0.13.0, which
// ends the old chain, first). Once rewritten the rows are 0 and 1 and later migrations number on from 2
// without a gap, so a database is never rewritten twice. Limit: an old database stopped at 2-46 or at 47-65
// with no gap looks new; goose then fails on its missing migrations instead of changing anything.
func baselineSquash(ctx context.Context, db *sql.DB) error {
	var max, n sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id), COUNT(DISTINCT version_id) FROM goose_db_version WHERE version_id > 0").Scan(&max, &n); err != nil || !max.Valid || max.Int64 <= 1 {
		return nil // no goose table yet (a new database), or only the init
	}
	if n.Int64 == max.Int64 {
		return nil // every version from 1: the squashed chain
	}
	if max.Int64 < oldChainVersion {
		return fmt.Errorf("migrate: this database is at version %d, from before the migrations were squashed; "+
			"upgrade to staqly v0.13.0 first (it takes the database to version %d), then to this version", max.Int64, oldChainVersion)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Keep goose's own initial row (version 0).
	if _, err := tx.ExecContext(ctx, "DELETE FROM goose_db_version WHERE version_id <> 0"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (1, true)"); err != nil {
		return err
	}
	return tx.Commit()
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
	// The copy holds webhook secrets: created owner-only (and exclusively) before VACUUM INTO fills it, which
	// accepts an empty file. A chmod after the copy would leave it readable under the umask until then.
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", dest)
	}
	if err != nil {
		return err
	}
	f.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		os.Remove(dest)
		return err
	}
	return nil
}
