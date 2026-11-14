package store

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	dbfiles "github.com/staqly/staqly/apps/api/db"
)

func TestProductFeedbackAttachmentsMigration(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "feedback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gooseMu.Lock()
	goose.SetBaseFS(dbfiles.Migrations)
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, db, "migrations", 10)
	}
	gooseMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at) VALUES ('ws', 'Demo', 'hash', 'now', 'now', 'now')`,
		`INSERT INTO product_feedback (id, workspace_id, reporter_id, kind, description, page_path, attachment_key, attachment_type, created_at) VALUES ('fb', 'ws', 'usr', 'problem', 'Description', '/', 'private/old.png', 'image/png', 'now')`,
		`INSERT INTO product_feedback (id, workspace_id, reporter_id, kind, description, page_path, created_at) VALUES ('empty', 'ws', 'usr', 'feature', 'Idea', '/', 'now')`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var id, key, mime string
	var position, count int
	if err := db.QueryRowContext(ctx, `SELECT feedback_id, position, storage_key, mime_type FROM product_feedback_attachments`).Scan(&id, &position, &key, &mime); err != nil || id != "fb" || position != 0 || key != "private/old.png" || mime != "image/png" {
		t.Fatalf("lost attachment: %s %d %s %s %v", id, position, key, mime, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_feedback_attachments`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count %d %v", count, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = 'ws'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_feedback_attachments`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade: %d %v", count, err)
	}
}
