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

// sqliteFile writes a real database to name (checkpointed, no WAL files) and returns its bytes.
func sqliteFile(t *testing.T, name string) []byte {
	t.Helper()
	database, err := store.Open(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	_ = os.Remove(name + "-wal")
	_ = os.Remove(name + "-shm")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Was a bug: a backup whose workspace.db is not a readable SQLite database (cut off by an interrupted copy,
// or another file under that name) replaced the working one, and the previous data was deleted.
func TestRestoreRefusesADamagedDatabase(t *testing.T) {
	for name, damage := range map[string]func([]byte) []byte{
		"cut off":    func(b []byte) []byte { return b[:len(b)/2] },
		"not sqlite": func([]byte) []byte { return []byte("BACKUP DATABASE") },
	} {
		t.Run(name, func(t *testing.T) {
			root, run := restoreFixture(t)
			current := filepath.Join(root, "data/workspace.db")
			healthy := sqliteFile(t, current)
			if err := os.WriteFile(filepath.Join(root, "backup/workspace.db"), damage(healthy), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := run()
			now, _ := os.ReadFile(current)
			calls, _ := os.ReadFile(filepath.Join(root, "calls.log"))
			if err == nil || !bytes.Equal(now, healthy) || len(calls) != 0 || !strings.Contains(out, "nothing was changed") {
				t.Fatalf("restore of a damaged database: exit error %v, working database still in place %v, docker calls %q\n%s",
					err, bytes.Equal(now, healthy), calls, out)
			}
		})
	}
}

// Was a bug: a helper killed in the middle of the swap (no trap runs) leaves a mix of new and previous files,
// and the container was started on it.
func TestRestoreKilledDuringSwapLeavesContainerStopped(t *testing.T) {
	root, run := restoreFixture(t)
	backup := string(sqliteFile(t, filepath.Join(root, "backup/workspace.db")))
	if err := os.WriteFile(filepath.Join(root, "data/workspace.db"), []byte("CURRENT DATABASE"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run("STUB_KILL_AFTER=workspace.db")
	if err == nil {
		t.Fatalf("the restore reported success: %s", out)
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name))
		return string(b)
	}
	// The fixture: new database, previous uploads and key, the previous database only aside.
	if read("data/workspace.db") != backup || read("data/secret.key") != "current key" ||
		read("data/.restore-old/workspace.db") != "CURRENT DATABASE" {
		t.Fatalf("fixture: the swap was not interrupted half way: key=%q old=%q\n%s", read("data/secret.key"), read("data/.restore-old/workspace.db"), out)
	}
	// stop, the helper, the look for .restore-old; never start.
	if calls := strings.Fields(read("calls.log")); strings.Join(calls, " ") != "stop run run" || !strings.Contains(out, "left STOPPED") {
		t.Fatalf("docker calls %v: the container was started on half-swapped data\n%s", calls, out)
	}
}
