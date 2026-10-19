package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// A backup holds webhook secrets and hashed credentials: it is owner-only from the moment it exists (not chmod'ed
// after VACUUM INTO wrote it with the umask's mode), opens as a database, and never overwrites a file.
func TestBackupIsOwnerOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, dir+"/live.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dest := dir + "/backup.db"
	if err := store.Backup(ctx, db, dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode: %v %v", fi.Mode(), err)
	}
	copy, err := store.Open(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := copy.QueryRowContext(ctx, "SELECT COUNT(*) FROM workspaces").Scan(&n); err != nil {
		t.Fatalf("backup is not a database: %v", err)
	}
	copy.Close()
	if err := store.Backup(ctx, db, dest); err == nil {
		t.Fatal("a second backup to the same file must fail")
	}
}
