package store_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
)

// Exercise the real backup/restore scripts and archive contents with Docker's process/mount boundary
// simulated locally. No test can address an installed container or volume.
func TestFullBackupRequiresStoppedWritersAndRestoresFiles(t *testing.T) {
	for _, state := range []string{"running", "other_writer", "dirty_wal", "stopped", "absolute_restore"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			data := filepath.Join(root, "data")
			for _, dir := range []string{"scripts", "bin", "data/uploads/workspaces/ws/media", "data/backups"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"backup.sh", "restore.sh"} {
				b, err := os.ReadFile(filepath.Join("../../../../scripts", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "scripts", name), b, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			database, err := store.Open(t.Context(), filepath.Join(data, "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			q := db.New(database)
			if err := q.CreateWorkspace(t.Context(), db.CreateWorkspaceParams{ID: "ws", Name: "Backup fixture", RecoveryVerifier: "fixture", CreatedAt: "2026-11-04", UpdatedAt: "2026-11-04"}); err != nil {
				t.Fatal(err)
			}
			if err := q.CreateMedia(t.Context(), db.CreateMediaParams{ID: "med_fixture", WorkspaceID: "ws", StorageName: "photo.png", OriginalName: "photo.png", MimeType: "image/png", SizeBytes: 7, Sha256: "fixture", CreatedAt: "2026-11-04"}); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			media := filepath.Join(data, "uploads/workspaces/ws/media/photo.png")
			for name, contents := range map[string]string{media: "picture", filepath.Join(data, "secret.key"): "sealed-data-key", filepath.Join(data, "notify_secret.key"): "notify-key", filepath.Join(data, "access_token"): "local-token"} {
				if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if state == "dirty_wal" {
				if err := os.WriteFile(filepath.Join(data, "workspace.db-wal"), []byte("uncheckpointed"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "bin/docker"), []byte("#!/bin/sh\nexec \"$BACKUP_TEST_BINARY\" -test.run='^TestBackupDockerProcess$' -- \"$@\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "BACKUP_TEST_BINARY="+os.Args[0], "BACKUP_TEST_DATA="+data, "BACKUP_TEST_STATE="+state, "STAQLY_CONTAINER=fixture-only", "STAQLY_VOLUME=fixture-only", "FORCE=1", "GORACE="+strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
			run := func(name string, args ...string) ([]byte, error) {
				t.Helper()
				cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", name)}, args...)...)
				cmd.Env = env
				return cmd.CombinedOutput()
			}
			out, err := run("backup.sh")
			if state == "running" || state == "other_writer" || state == "dirty_wal" {
				if err == nil {
					backups, _ := filepath.Glob(filepath.Join(root, "backups/*/workspace.db"))
					if state == "running" && len(backups) == 1 {
						copy, openErr := store.Open(t.Context(), backups[0])
						if openErr != nil {
							t.Fatal(openErr)
						}
						_, rowErr := db.New(copy).GetMedia(t.Context(), db.GetMediaParams{WorkspaceID: "ws", ID: "med_fixture"})
						copy.Close()
						files, _ := exec.Command("tar", "tzf", filepath.Join(filepath.Dir(backups[0]), "uploads.tar.gz")).Output()
						t.Errorf("live backup media row: %v; file included in archive: %v", rowErr, bytes.Contains(files, []byte("photo.png")))
					}
					t.Fatalf("full backup accepted %s writers/storage: %s", state, out)
				}
				want := "stop"
				if state == "dirty_wal" {
					want = "WAL"
				}
				if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(want)) {
					t.Fatalf("unexpected refusal for %s: %s", state, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("backup: %v\n%s", err, out)
			}
			backups, _ := filepath.Glob(filepath.Join(root, "backups/*/workspace.db"))
			if len(backups) != 1 {
				t.Fatalf("backup database missing: %v", backups)
			}
			if err := os.RemoveAll(filepath.Join(data, "uploads")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "workspace.db"), []byte("replace me"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "secret.key"), []byte("wrong-key"), 0o600); err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(root, filepath.Dir(backups[0]))
			if err != nil {
				t.Fatal(err)
			}
			if state == "absolute_restore" {
				relative = filepath.Dir(backups[0])
			}
			if out, err := run("restore.sh", relative); err != nil {
				t.Fatalf("restore: %v\n%s", err, out)
			}
			restored, err := store.Open(t.Context(), filepath.Join(data, "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if _, err := db.New(restored).GetMedia(t.Context(), db.GetMediaParams{WorkspaceID: "ws", ID: "med_fixture"}); err != nil {
				t.Fatalf("restored media row: %v", err)
			}
			for name, want := range map[string]string{media: "picture", filepath.Join(data, "secret.key"): "sealed-data-key", filepath.Join(data, "notify_secret.key"): "notify-key", filepath.Join(data, "access_token"): "local-token"} {
				got, err := os.ReadFile(name)
				if err != nil || string(got) != want {
					t.Errorf("restored %s: %q %v", filepath.Base(name), got, err)
				}
			}
		})
	}
}

func TestBackupDockerProcess(t *testing.T) {
	data := os.Getenv("BACKUP_TEST_DATA")
	if data == "" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	switch args[0] {
	case "inspect":
		fmt.Println(os.Getenv("BACKUP_TEST_STATE") == "running")
	case "ps":
		if os.Getenv("BACKUP_TEST_STATE") == "other_writer" {
			fmt.Println("other-fixture-writer")
		}
	case "stop", "start":
	case "exec":
		if args[2] == "server" {
			database, err := store.Open(context.Background(), filepath.Join(data, "workspace.db"))
			if err != nil {
				fail(err)
			}
			if err := store.Backup(context.Background(), database, strings.Replace(args[4], "/data", data, 1)); err != nil {
				fail(err)
			}
			// The original live recipe lets permanent deletion win before its separate archive step.
			if _, err := database.Exec("DELETE FROM media WHERE id = 'med_fixture'"); err != nil {
				fail(err)
			}
			database.Close()
			_ = os.Remove(filepath.Join(data, "uploads/workspaces/ws/media/photo.png"))
		} else {
			_ = os.Remove(strings.Replace(args[len(args)-1], "/data", data, 1))
		}
	case "cp":
		_, src, _ := strings.Cut(args[1], ":")
		b, err := os.ReadFile(strings.Replace(src, "/data", data, 1))
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(args[2], b, 0o600); err != nil {
			fail(err)
		}
	case "run":
		out := ""
		env := os.Environ()
		for i, arg := range args[:len(args)-1] {
			if arg == "-v" && strings.Contains(args[i+1], ":/backup") {
				out, _, _ = strings.Cut(args[i+1], ":/backup")
			}
			if arg == "-e" {
				env = append(env, args[i+1])
			}
		}
		body := strings.ReplaceAll(args[len(args)-1], "chown -R 10001:10001 /data", ":") // container UID mapping only
		body = strings.ReplaceAll(strings.ReplaceAll(body, "/data", data), "/backup", out)
		cmd := exec.Command("sh", "-ec", body)
		cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("unexpected Docker operation %q", args[0]))
	}
	os.Exit(0)
}
