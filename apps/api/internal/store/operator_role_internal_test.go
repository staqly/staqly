package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	dbfiles "github.com/staqly/staqly/apps/api/db"
)

func TestOperatorRoleMigration(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Start with the released schema, including its original role constraints.
	gooseMu.Lock()
	goose.SetBaseFS(dbfiles.Migrations)
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, db, "migrations", 1)
	}
	gooseMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at)
		VALUES ('ws', 'Demo', 'hash', 'now', 'now', 'now')`)
	exec(`INSERT INTO users (id, email, created_at, updated_at) VALUES ('usr', 'demo@example.com', 'now', 'now')`)
	exec(`INSERT INTO workspace_members VALUES ('ws', 'usr', 'owner', 'created', 'updated')`)
	exec(`INSERT INTO workspace_invitations (id, workspace_id, email, role, token_hash, created_at, updated_at, expires_at)
		VALUES ('inv', 'ws', 'invite@example.com', 'editor', 'invite-hash', 'created', 'updated', 'later')`)
	for _, role := range []string{"", "editor", "viewer"} {
		exec(`INSERT INTO device_links (id, workspace_id, token_hash, created_by, created_at, expires_at, user_id, guest_role)
			VALUES (?, 'ws', ?, 'cli', 'created', 'later', 'usr', ?)`, "link-"+role, "link-hash-"+role, role)
		exec(`INSERT INTO sessions (id, workspace_id, token_hash, csrf_token, created_at, last_seen_at, expires_at, user_id, guest_role)
			VALUES (?, 'ws', ?, 'csrf', 'created', 'seen', 'later', 'usr', ?)`, "session-"+role, "session-hash-"+role, role)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ query, want string }{
		{`SELECT role || ':' || created_at || ':' || updated_at FROM workspace_members WHERE user_id = 'usr'`, "owner:created:updated"},
		{`SELECT role || ':' || token_hash || ':' || expires_at FROM workspace_invitations WHERE id = 'inv'`, "editor:invite-hash:later"},
	} {
		var got string
		if err := db.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("migration lost member/invitation data: got %q, want %q, err %v", got, check.want, err)
		}
	}
	for _, table := range []string{"device_links", "sessions"} {
		for _, role := range []string{"", "editor", "viewer"} {
			want := role
			if role == "editor" {
				want = "operator"
			}
			var got, expires string
			if err := db.QueryRowContext(ctx, "SELECT guest_role, expires_at FROM "+table+" WHERE token_hash LIKE ?", "%-hash-"+role).Scan(&got, &expires); err != nil || got != want || expires != "later" {
				t.Fatalf("%s role %q: got %q until %q, err %v", table, role, got, expires, err)
			}
		}
	}
	// Both rebuilt tables accept the new role and still enforce their foreign keys.
	exec(`UPDATE workspace_members SET role = 'operator' WHERE user_id = 'usr'`)
	exec(`UPDATE workspace_invitations SET role = 'operator' WHERE id = 'inv'`)
	exec(`DELETE FROM workspaces WHERE id = 'ws'`)
	for _, table := range []string{"workspace_members", "workspace_invitations", "device_links", "sessions"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s cascade: count %d, err %v", table, count, err)
		}
	}
}
