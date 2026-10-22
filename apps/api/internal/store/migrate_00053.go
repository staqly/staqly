package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/pressly/goose/v3"
)

// 00053 removes accounts: a workspace has people (workspace_members with roles), and the person who owned an
// account becomes an owner of each of its workspaces. It also lets a webhook delivery go with its event
// (ON DELETE CASCADE).
//
// A Go migration because SQLite can't drop a column that is a foreign key: the table is rebuilt, which needs
// foreign keys off, or dropping the old workspaces table would cascade into every table that references it.
// That pragma only works outside a transaction and per connection, so the rebuild runs on one pinned
// connection, in its own transaction, and checks every foreign key before it commits (SQLite's documented
// 12-step procedure). sqlc sees the same change in db/sqlc/00053_remove_accounts.sql.
func init() {
	goose.AddNamedMigrationNoTxContext("00053_remove_accounts.go", removeAccounts, nil)
}

// Every account owner is an owner of the account's workspaces (they always were, through the account).
const ownersFromAccounts = `INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at)
SELECT w.id, a.owner_user_id, 'owner', a.created_at, a.updated_at FROM workspaces w JOIN accounts a ON a.id = w.account_id
WHERE true -- SQLite: an upsert after a join needs a WHERE, or ON CONFLICT reads as the join's ON
ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = 'owner'`

func removeAccounts(ctx context.Context, db *sql.DB) error {
	var accounts int // a database made without accounts (a test's minimal schema) has nothing to remove
	q := `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'accounts'`
	if IsPostgres(db) {
		q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'accounts'`
	}
	if err := db.QueryRowContext(ctx, q).Scan(&accounts); err != nil || accounts == 0 {
		return err
	}
	if IsPostgres(db) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, q := range []string{
			ownersFromAccounts,
			`ALTER TABLE workspaces DROP COLUMN account_id`,
			`DROP TABLE accounts`,
			`ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS webhook_deliveries_event_id_fkey`,
			`ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_event_id_fkey FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE CASCADE`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("00053: %w", err)
			}
		}
		return tx.Commit()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`) }() // before it goes back to the pool
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, ownersFromAccounts); err != nil {
		return fmt.Errorf("00053 owners: %w", err)
	}
	accountColumn := regexp.MustCompile(`(?m)^\s*account_id\s+TEXT REFERENCES accounts \(id\),\n`)
	if err := rebuildSQLiteTable(ctx, tx, "workspaces", func(ddl string) (string, error) {
		if !accountColumn.MatchString(ddl) {
			return "", fmt.Errorf("workspaces has no account_id column as 00001 made it")
		}
		return accountColumn.ReplaceAllString(ddl, ""), nil
	}, "account_id"); err != nil {
		return fmt.Errorf("00053 workspaces: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE accounts`); err != nil {
		return fmt.Errorf("00053 accounts: %w", err)
	}
	const eventFK = "event_id         TEXT NOT NULL REFERENCES events (id),"
	if err := rebuildSQLiteTable(ctx, tx, "webhook_deliveries", func(ddl string) (string, error) {
		if !strings.Contains(ddl, eventFK) {
			return "", fmt.Errorf("webhook_deliveries.event_id is not as 00001 made it")
		}
		return strings.Replace(ddl, eventFK, "event_id         TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,", 1), nil
	}, ""); err != nil {
		return fmt.Errorf("00053 webhook_deliveries: %w", err)
	}
	if err := checkSQLiteForeignKeys(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// rebuildSQLiteTable recreates table from its own CREATE statement as edit changes it, without the column
// drop ("" for none), copying the rows and keeping its indexes (but those on the dropped column) and
// triggers. The caller's connection has foreign keys off.
func rebuildSQLiteTable(ctx context.Context, tx *sql.Tx, table string, edit func(string) (string, error), drop string) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl); err != nil {
		return err
	}
	ddl, err := edit(ddl)
	if err != nil {
		return err
	}
	head := "CREATE TABLE " + table + " ("
	if !strings.HasPrefix(ddl, head) {
		return fmt.Errorf("unexpected CREATE statement for %s", table)
	}
	ddl = "CREATE TABLE " + table + "_new (" + strings.TrimPrefix(ddl, head)

	var cols []string
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return err
		}
		if c != drop {
			cols = append(cols, `"`+c+`"`)
		}
	}
	rows.Close()

	var keep []string // indexes and triggers to make again
	rows, err = tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL`, table)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		if drop == "" || !strings.Contains(s, drop) {
			keep = append(keep, s)
		}
	}
	rows.Close()

	list := strings.Join(cols, ", ")
	for _, q := range append([]string{
		ddl,
		"INSERT INTO " + table + "_new (" + list + ") SELECT " + list + " FROM " + table,
		"DROP TABLE " + table,
		"ALTER TABLE " + table + "_new RENAME TO " + table,
	}, keep...) {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%w (%.80s)", err, q)
		}
	}
	return nil
}

// checkSQLiteForeignKeys fails when any row points at a row that isn't there.
func checkSQLiteForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fk int
		_ = rows.Scan(&table, &rowid, &parent, &fk)
		return fmt.Errorf("00053: %s has a row that points at a missing %s row", table, parent)
	}
	return rows.Err()
}
