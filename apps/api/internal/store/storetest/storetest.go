// Package storetest opens a database for tests: SQLite in the test's temp dir, or, when
// STAQLY_TEST_DATABASE_URL points at a PostgreSQL server, a fresh schema there (migrated, dropped
// at cleanup). The whole Go suite runs on either: `STAQLY_TEST_DATABASE_URL=postgres://... go test ./...`.
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// Open returns a migrated database; path is the SQLite file used when no PostgreSQL URL is set.
func Open(t testing.TB, path string) (*sql.DB, error) {
	t.Helper()
	base := os.Getenv("STAQLY_TEST_DATABASE_URL")
	if base == "" {
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

// Postgres reports whether the tests run on PostgreSQL.
func Postgres() bool { return os.Getenv("STAQLY_TEST_DATABASE_URL") != "" }
