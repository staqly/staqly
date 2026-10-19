package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// importOrder lists the tables in foreign-key order (referenced tables first).
var importOrder = []string{
	"installation", "workspaces", "api_keys", "sessions", "collections", "media", "events", "webhooks",
	"webhook_deliveries", "idempotency_keys", "usage_counters", "device_links", "notification_channels",
	"notification_outbox", "workspace_exports", "stripe_events", "agents", "channels", "provider_apps",
	"posts", "post_targets", "schedules", "jobs",
}

// ImportReport is how many rows each table received; NULReplaced counts text values whose NUL characters
// (which SQLite stores and PostgreSQL text cannot) became U+FFFD.
type ImportReport map[string]int64

// NULReplaced is the ImportReport key for text values with NUL characters replaced.
const NULReplaced = "nul_replaced"

// ImportSQLite copies a SQLite database (an earlier cloud deployment, or a local one moving to cloud)
// into an EMPTY PostgreSQL database, in one transaction, and checks every table's row count before
// committing. It refuses a target that already has data. The SQLite file is opened (and migrated to the
// current version first, as the server would) but not changed otherwise.
func ImportSQLite(ctx context.Context, pg *sql.DB, sqlitePath string) (ImportReport, error) {
	if !IsPostgres(pg) {
		return nil, errors.New("the target must be the PostgreSQL database (DATABASE_URL)")
	}
	src, err := Open(ctx, sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", sqlitePath, err)
	}
	defer src.Close()
	if err := sameVersion(ctx, src, pg); err != nil {
		return nil, err
	}
	for _, t := range importOrder {
		var n int64
		if err := pg.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("the PostgreSQL database is not empty (%s has %d rows): import only into a fresh database", t, n)
		}
	}
	tx, err := pg.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	report := ImportReport{}
	for _, t := range importOrder {
		n, nuls, err := copyTable(ctx, src, tx, t)
		report[NULReplaced] += nuls
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		var got int64
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&got); err != nil {
			return nil, err
		}
		if got != n {
			return nil, fmt.Errorf("%s: copied %d rows but the target has %d", t, n, got)
		}
		report[t] = n
	}
	// events.seq is an identity column: continue after the imported values.
	if _, err := tx.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('events', 'seq'), GREATEST((SELECT MAX(seq) FROM events), 1))`); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return report, nil
}

func sameVersion(ctx context.Context, src, pg *sql.DB) error {
	var a, b int64
	if err := src.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&a); err != nil {
		return err
	}
	if err := pg.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&b); err != nil {
		return err
	}
	if a != b {
		return fmt.Errorf("schema versions differ (SQLite %d, PostgreSQL %d): run the same server version on both", a, b)
	}
	return nil
}

func copyTable(ctx context.Context, src *sql.DB, tx *sql.Tx, table string) (int64, int64, error) {
	rows, err := src.QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, 0, err
	}
	insert := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") OVERRIDING SYSTEM VALUE VALUES (" +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var n, nuls int64
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, nuls, err
		}
		for i, v := range vals {
			if s, ok := v.(string); ok && strings.IndexByte(s, 0) >= 0 {
				vals[i] = strings.ReplaceAll(s, "\x00", "\uFFFD")
				nuls++
			}
		}
		if _, err := tx.ExecContext(ctx, insert, vals...); err != nil {
			return n, nuls, err
		}
		n++
	}
	return n, nuls, rows.Err()
}
