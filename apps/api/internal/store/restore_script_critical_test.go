package store_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// restoreFixture runs the real scripts/restore.sh with a stub `docker` on throwaway folders (the stub of
// e2e/cross-client/06-scripts.test.ts): /data and /backup become folders under root, chown is dropped.
// STUB_KILL_AFTER=<name> kills the helper (SIGKILL: no trap runs) right after that file was swapped in.
func restoreFixture(t *testing.T) (root string, run func(env ...string) (string, error)) {
	t.Helper()
	root = t.TempDir()
	for _, dir := range []string{"bin", "data/uploads", "backup", "archive/uploads"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stub := `#!/bin/sh
echo "$1" >> "$STUB_ROOT/calls.log"
case "$1" in
  stop|start) exit 0 ;;
  run)
    for last; do :; done
    script=$(printf '%s' "$last" | sed "s#/data#$STUB_ROOT/data#g; s#/backup#$STUB_ROOT/backup#g; s#chown -R 10001:10001#: #")
    if [ -n "${STUB_KILL_AFTER:-}" ]; then
      script=$(printf '%s' "$script" | sed "s#installed=\"\$installed \$name\"#&; [ \"\$name\" != \"$STUB_KILL_AFTER\" ] || kill -9 \$\$#")
    fi
    exec sh -ec "$script" ;;
  *) echo "stub: unexpected docker $*" >&2; exit 97 ;;
esac
`
	files := map[string]string{"bin/docker": stub, "data/workspace.db-wal": "current wal", "data/uploads/current.png": "current picture",
		"data/secret.key": "current key", "archive/uploads/old.png": "old picture", "archive/secret.key": "backup key"}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("tar", "czf", filepath.Join(root, "backup/uploads.tar.gz"), "-C", filepath.Join(root, "archive"), "uploads", "secret.key").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	run = func(env ...string) (string, error) {
		cmd := exec.Command("bash", "../../../../scripts/restore.sh", filepath.Join(root, "backup"))
		cmd.Env = append(append(os.Environ(), "STUB_ROOT="+root, "FORCE=1",
			"PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH")), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return root, run
}

// Audit 2026-11-13, finding 2: a backup whose workspace.db is not a readable SQLite database must be
// refused before the current data is replaced.
func TestRestoreRefusesCorruptDatabase(t *testing.T) {
	root, run := restoreFixture(t)
	current := filepath.Join(root, "data/workspace.db")
	database, err := store.Open(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	_ = os.Remove(current + "-wal")
	_ = os.Remove(current + "-shm")
	healthy, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	// The backup: the same database cut in half (an interrupted copy).
	if err := os.WriteFile(filepath.Join(root, "backup/workspace.db"), healthy[:len(healthy)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := run()
	now, _ := os.ReadFile(current)
	if err == nil || !bytes.Equal(now, healthy) {
		_, openErr := store.Open(t.Context(), current)
		t.Fatalf("restore accepted a truncated database (exit error: %v; healthy database still in place: %v; "+
			"previous copy kept in .restore-old: %v; the restored database opens: %v)\n%s",
			err, bytes.Equal(now, healthy), exists(filepath.Join(root, "data/.restore-old")), openErr == nil, out)
	}
}

// Audit 2026-11-13, finding 3: a helper killed in the middle of the swap (no trap runs) leaves a mix of new
// and previous files; the container must stay stopped.
func TestRestoreKilledDuringSwapLeavesContainerStopped(t *testing.T) {
	root, run := restoreFixture(t)
	for name, body := range map[string]string{"data/workspace.db": "CURRENT DATABASE", "backup/workspace.db": "BACKUP DATABASE"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := run("STUB_KILL_AFTER=workspace.db")
	if err == nil {
		t.Fatalf("the restore reported success: %s", out)
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name))
		return string(b)
	}
	// The fixture did what the finding describes: new database, previous uploads and key, old data aside.
	if read("data/workspace.db") != "BACKUP DATABASE" || read("data/secret.key") != "current key" ||
		read("data/.restore-old/workspace.db") != "CURRENT DATABASE" {
		t.Fatalf("fixture: the swap was not interrupted half way: db=%q key=%q old=%q\n%s",
			read("data/workspace.db"), read("data/secret.key"), read("data/.restore-old/workspace.db"), out)
	}
	if calls := strings.Fields(read("calls.log")); len(calls) != 2 {
		t.Fatalf("docker calls %v: the container was started on half-swapped data (backup database, previous "+
			"uploads and secret.key, previous database only in .restore-old)\n%s", calls, out)
	}
}

func exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
