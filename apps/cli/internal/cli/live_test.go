package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLive runs the real binary against a running local-mode API server:
//
//	cd apps/api && DATA_DIR=/tmp/cli-data ADDR=127.0.0.1:18790 go run ./cmd/server
//	cd apps/cli && STAQLY_LIVE=1 go test -run TestLive -v ./internal/cli
//
// STAQLY_LIVE_URL overrides the server address (default http://127.0.0.1:18790).
func TestLive(t *testing.T) {
	if os.Getenv("STAQLY_LIVE") != "1" {
		t.Skip("set STAQLY_LIVE=1 (and start a local API server) to run the live smoke test")
	}
	base := firstNonEmpty(os.Getenv("STAQLY_LIVE_URL"), "http://127.0.0.1:18790")
	dir := t.TempDir()
	bin := filepath.Join(dir, "staqly")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/staqly").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	env := append(os.Environ(), "STAQLY_API_URL="+base, "STAQLY_CONFIG="+filepath.Join(dir, "config.json"), "STAQLY_API_KEY=", "NO_COLOR=1")
	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errb.String(), code
	}
	must := func(want int, args ...string) string {
		t.Helper()
		out, errOut, code := run(args...)
		if code != want {
			t.Fatalf("staqly %s: exit %d, want %d\n%s\n%s", strings.Join(args, " "), code, want, out, errOut)
		}
		return out
	}

	out := must(0, "status", "--json")
	var st map[string]any
	json.Unmarshal([]byte(out), &st)
	if st["session"].(map[string]any)["mode"] != "local" {
		t.Fatalf("expected a local-mode server: %s", out)
	}
	col := fmt.Sprintf("cli-live-%d", time.Now().UnixNano())
	must(0, "collections", "create", col)
	t.Cleanup(func() { run("collections", "delete", col, "--yes") })

	// pendingID waits until the pushed item shows up in the inbox.
	pendingID := func(title string) string {
		t.Helper()
		for range 50 {
			out := must(0, "items", "list", "--collection", col, "--status", "pending", "--json")
			var page struct{ Data []Item }
			json.Unmarshal([]byte(out), &page)
			for _, it := range page.Data {
				if it.Title == title {
					return it.ID
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("item %q never became pending", title)
		return ""
	}

	push := func(title string, extra ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
		args := append([]string{"push", col, "--title", title, "--content", "live test", "--timeout", "60s"}, extra...)
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, &out, &errb
	}
	exitCode := func(cmd *exec.Cmd) int {
		err := cmd.Wait()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	}

	t.Run("approve path exits 0", func(t *testing.T) {
		cmd, out, errb := push("approve me")
		id := pendingID("approve me")
		must(0, "items", "approve", id, "--feedback", "ship it")
		if code := exitCode(cmd); code != ExitOK {
			t.Fatalf("push exit %d, want 0\n%s\n%s", code, out, errb)
		}
		mustContain(t, out.String(), "Approved: "+id, "Feedback: ship it")
		must(0, "items", "result", id, "--status", "completed", "--result", `{"url":"https://example.com/p/1"}`)
		var it struct {
			Status   string         `json:"status"`
			Metadata map[string]any `json:"metadata"`
		}
		json.Unmarshal([]byte(must(0, "items", "get", id, "--json")), &it)
		if it.Status != "completed" || it.Metadata["result"] == nil {
			t.Fatalf("result not recorded: %+v", it)
		}
	})

	t.Run("expires-in and editable are accepted", func(t *testing.T) {
		out := must(0, "items", "create", col, "--title", "expiring", "--expires-in", "1h", "--editable", "title,content", "--json")
		var it Item
		json.Unmarshal([]byte(out), &it)
		if it.ExpiresAt == nil || len(it.Editable) != 2 {
			t.Fatalf("expires_at/editable missing: %s", out)
		}
		must(0, "items", "reject", it.ID, "-m", "cleanup")
	})

	t.Run("reject path exits 2", func(t *testing.T) {
		cmd, out, errb := push("reject me", "--json")
		id := pendingID("reject me")
		must(0, "items", "reject", id, "-m", "not today")
		if code := exitCode(cmd); code != ExitRejected {
			t.Fatalf("push exit %d, want 2\n%s\n%s", code, out, errb)
		}
		var it Item
		if err := json.Unmarshal(out.Bytes(), &it); err != nil || it.Status != "rejected" || it.Feedback != "not today" {
			t.Fatalf("json output = %s (%v)", out, err)
		}
	})

	t.Run("timeout exits 3", func(t *testing.T) {
		_, errOut, code := run("push", col, "--title", "nobody decides", "--timeout", "2s")
		if code != ExitTimeout {
			t.Fatalf("exit %d, want 3: %s", code, errOut)
		}
	})

	t.Run("double decision is a conflict", func(t *testing.T) {
		id := pendingID("nobody decides")
		must(0, "items", "approve", id)
		_, errOut, code := run("items", "reject", id)
		if code != ExitError || !strings.Contains(errOut, "conflict") {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	})

	t.Run("history, events, inbox", func(t *testing.T) {
		out := must(0, "events", "tail", "--collection", col, "--type", "item.*", "--limit", "50")
		mustContain(t, out, "item.approved", "item.rejected")
		out = must(0, "inbox", "--collection", col, "--json")
		var page struct{ Data []Item }
		json.Unmarshal([]byte(out), &page)
		if len(page.Data) != 0 {
			t.Fatalf("inbox should be empty: %s", out)
		}
		must(0, "items", "list", "--collection", col)
	})

	t.Run("usage errors", func(t *testing.T) {
		must(ExitUsage, "items", "approve")
		must(ExitError, "items", "get", "itm_doesnotexist")
	})
}
