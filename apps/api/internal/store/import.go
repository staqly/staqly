package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// importOrder lists every application table in foreign-key order (referenced tables first);
// TestImportOrderCoversEveryTable fails when a migration adds a table that isn't here.
var importOrder = []string{
	"installation", "users", "workspaces", "user_identities", "passkeys", "passkey_challenges",
	"email_codes", "workspace_members", "workspace_invitations", "api_keys", "sessions", "collections",
	"media_folders", "media", "media_projects", "events", "webhooks", "webhook_deliveries", "idempotency_keys",
	"usage_counters", "usage_events", "credit_grants", "device_links", "notification_channels",
	"notification_outbox", "workspace_exports", "stripe_events", "agents", "channel_groups",
	"channels", "provider_apps", "posts", "post_targets", "preview_links", "post_comments", "schedules", "jobs",
	"job_requeues", "channel_links", "pending_connections", "ai_jobs", "assistant_chats", "telegram_connect_codes", "oauth_clients",
	"trial_fingerprints", "workspace_trials",
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
	// The job queue's queued and running jobs (River's table, per dialect) travel as rows of the old queue's
	// table, which the server carries into River when it starts.
	carried, err := carryQueuedJobs(ctx, src, tx)
	if err != nil {
		return nil, fmt.Errorf("queued jobs: %w", err)
	}
	report["jobs"] += carried
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
	// A database made before the squash says 47 where a new one says 1: the same schema.
	if a == squashedVersion {
		a = 1
	}
	if b == squashedVersion {
		b = 1
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

// carryQueuedJobs copies the source's live River jobs into the target's jobs table (the server's startup
// moves them into its own River queue).
func carryQueuedJobs(ctx context.Context, src *sql.DB, tx *sql.Tx) (int64, error) {
	rows, err := src.QueryContext(ctx, `SELECT id, kind, json(args), max_attempts, scheduled_at, created_at FROM river_job
		WHERE state IN ('available', 'pending', 'retryable', 'scheduled', 'running')`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var n int64
	for rows.Next() {
		var id, maxAttempts int64
		var kind, args, scheduled, created string
		if err := rows.Scan(&id, &kind, &args, &maxAttempts, &scheduled, &created); err != nil {
			return n, err
		}
		var a struct {
			Key         string          `json:"key"`
			WorkspaceID string          `json:"workspace_id"`
			Payload     json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(args), &a); err != nil || a.Key == "" {
			continue // not one of ours
		}
		if len(a.Payload) == 0 {
			a.Payload = json.RawMessage("{}")
		}
		runAt, created := riverTime(scheduled), riverTime(created)
		if _, err := tx.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, kind, key, payload, status, run_at, attempts, max_attempts, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 'queued', ?, 0, ?, ?, ?)`, fmt.Sprintf("job_river_%d", id), a.WorkspaceID, kind, a.Key, string(a.Payload),
			runAt, maxAttempts, created, created); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// riverTime turns River's SQLite timestamp (RFC 3339, or "2006-01-02 15:04:05.000" from SQL defaults; UTC)
// into the application's format.
func riverTime(v string) string {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05.000000Z")
		}
	}
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
}
