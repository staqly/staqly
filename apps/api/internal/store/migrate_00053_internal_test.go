package store

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"github.com/pressly/goose/v3"

	dbfiles "github.com/staqly/staqly/apps/api/db"
)

// 00053 on a database with accounts: nothing a workspace holds is lost (the rebuild of workspaces runs with
// foreign keys off, or it would cascade), the account owner is an owner of every account workspace, accounts
// and workspaces.account_id are gone, and a webhook delivery now goes with its event.
func TestMigration00053RemovesAccounts(t *testing.T) {
	ctx := context.Background()
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(ON)", "busy_timeout(10000)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/m.db?"+q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	gooseMu.Lock()
	goose.SetBaseFS(dbfiles.Migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	err = goose.UpToContext(ctx, db, "migrations", 52)
	gooseMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	const now = "2026-10-01T00:00:00Z"
	for _, s := range []string{
		`INSERT INTO users (id, email, created_at, updated_at) VALUES ('usr_owner', 'owner@example.com', '` + now + `', '` + now + `'),
			('usr_editor', 'editor@example.com', '` + now + `', '` + now + `')`,
		`INSERT INTO accounts (id, owner_user_id, name, created_at, updated_at) VALUES ('acc_1', 'usr_owner', 'Acme', '` + now + `', '` + now + `')`,
		`INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at, account_id) VALUES
			('ws_a', 'A', 'v1', '` + now + `', '` + now + `', '` + now + `', 'acc_1'),
			('ws_b', 'B', 'v2', '` + now + `', '` + now + `', '` + now + `', 'acc_1'),
			('ws_old', 'Before accounts', 'v3', '` + now + `', '` + now + `', '` + now + `', NULL)`,
		// The owner was demoted to editor in B by an older version: the account made them owner anyway.
		`INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at) VALUES
			('ws_a', 'usr_owner', 'owner', '` + now + `', '` + now + `'), ('ws_b', 'usr_owner', 'editor', '` + now + `', '` + now + `'),
			('ws_a', 'usr_editor', 'editor', '` + now + `', '` + now + `')`,
		`INSERT INTO collections (id, workspace_id, name, created_at, updated_at) VALUES
			('col_a', 'ws_a', 'News', '` + now + `', '` + now + `'), ('col_b', 'ws_b', 'Blog', '` + now + `', '` + now + `'),
			('col_old', 'ws_old', 'Old', '` + now + `', '` + now + `')`,
		`INSERT INTO events (id, workspace_id, type, actor_type, actor_id, actor_name, data, created_at) VALUES
			('evt_1', 'ws_a', 'post.created', 'system', 's', 'S', '{}', '` + now + `')`,
		`INSERT INTO webhooks (id, workspace_id, url, secret, events, created_at, updated_at) VALUES
			('wh_1', 'ws_a', 'https://example.com/hook', 's', '["post.*"]', '` + now + `', '` + now + `')`,
		`INSERT INTO webhook_deliveries (id, workspace_id, webhook_id, event_id, event_type, next_attempt_at, created_at) VALUES
			('dlv_1', 'ws_a', 'wh_1', 'evt_1', 'post.created', '` + now + `', '` + now + `')`,
	} {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("%v: %.60s", err, s)
		}
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("%v: %s", err, query)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM workspaces`); n != 3 {
		t.Fatalf("workspaces: %d, want 3", n)
	}
	if n := count(`SELECT COUNT(*) FROM collections`); n != 3 {
		t.Fatalf("collections: %d, want 3 (the rebuild cascaded)", n)
	}
	if n := count(`SELECT COUNT(*) FROM webhook_deliveries`); n != 1 {
		t.Fatalf("deliveries: %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM workspace_members WHERE user_id = 'usr_owner' AND role = 'owner'`); n != 2 {
		t.Fatalf("the account owner owns %d workspaces, want 2", n)
	}
	if n := count(`SELECT COUNT(*) FROM workspace_members WHERE user_id = 'usr_editor' AND role = 'editor'`); n != 1 {
		t.Fatalf("other members: %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'accounts'`); n != 0 {
		t.Fatal("accounts is still there")
	}
	if n := count(`SELECT COUNT(*) FROM pragma_table_info('workspaces') WHERE name = 'account_id'`); n != 0 {
		t.Fatal("workspaces.account_id is still there")
	}
	if n := count(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'workspaces_stripe_customer'`); n != 1 {
		t.Fatal("the workspaces index is gone")
	}
	if n := count(`SELECT COUNT(*) FROM pragma_foreign_key_list('webhook_deliveries') WHERE "table" = 'events' AND on_delete = 'CASCADE'`); n != 1 {
		t.Fatal("webhook_deliveries.event_id does not cascade")
	}
	// Foreign keys are on again and enforced.
	if n := count(`PRAGMA foreign_keys`); n != 1 {
		t.Fatal("foreign keys stayed off")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM events WHERE id = 'evt_1'`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM webhook_deliveries`); n != 0 {
		t.Fatal("a delivery outlived its event")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = 'ws_b'`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM collections WHERE workspace_id = 'ws_b'`); n != 0 {
		t.Fatal("deleting a workspace no longer cascades")
	}
}
