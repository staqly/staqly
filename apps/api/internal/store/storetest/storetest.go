// Package storetest opens a database for tests: SQLite in the test's temp dir, or, when
// STAQLY_TEST_DATABASE_URL points at a PostgreSQL server, a fresh schema there (migrated, dropped
// at cleanup). The whole Go suite runs on either: `STAQLY_TEST_DATABASE_URL=postgres://... go test ./...`.
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// Open returns a migrated database; path is the SQLite file used when no PostgreSQL URL is set.
func Open(t testing.TB, path string) (*sql.DB, error) {
	t.Helper()
	base := os.Getenv("STAQLY_TEST_DATABASE_URL")
	if base == "" {
		if err := seedSQLite(path); err != nil {
			return nil, err
		}
		return store.Open(context.Background(), path)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	admin, err := store.OpenPostgresRaw(context.Background(), base)
	if err != nil {
		return nil, err
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		return nil, err
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		admin.Close()
	})
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return store.OpenPostgres(context.Background(), u.String())
}

// template is a migrated SQLite database, made once per test binary. Migrating a new file costs ~1.5 s under
// -race (goose and River's migrations on the transpiled SQLite); paid by each of the ~230 httpapi tests it
// pushed that package past go test's 10 minute timeout on CI. A test opening a new path gets a copy instead,
// and store.Open still runs Migrate on it (a no-op: every version is recorded), so nothing is skipped.
var template struct {
	once sync.Once
	data []byte
	err  error
}

// seedSQLite writes the migrated template to path unless a database is already there (a test reopening its
// file keeps what it wrote).
func seedSQLite(path string) error {
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	template.once.Do(func() { template.data, template.err = migratedTemplate() })
	if template.err != nil {
		return template.err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, template.data, 0o600)
}

func migratedTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "staqly-storetest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	db, err := store.Open(context.Background(), filepath.Join(dir, "migrated.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	snapshot := filepath.Join(dir, "template.db")
	if err := store.Backup(context.Background(), db, snapshot); err != nil {
		return nil, err
	}
	return os.ReadFile(snapshot)
}

// Postgres reports whether the tests run on PostgreSQL.
func Postgres() bool { return os.Getenv("STAQLY_TEST_DATABASE_URL") != "" }
