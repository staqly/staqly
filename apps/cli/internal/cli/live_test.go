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

	// A channel on your own automation: its posts wait for `posts result`.
	var ch Channel
	json.Unmarshal([]byte(must(0, "channels", "connect", "custom", "--name", col, "--json")), &ch)
	if ch.ID == "" {
		t.Fatal("channel not created")
	}
	t.Cleanup(func() { run("channels", "remove", ch.ID) })

	t.Run("draft, schedule, cancel", func(t *testing.T) {
		var p Post
		json.Unmarshal([]byte(must(0, "posts", "create", col, "--content", "live draft", "--channel", ch.ID, "--tag", "live", "--json")), &p)
		if p.Status != "draft" || len(p.Targets) != 1 {
			t.Fatalf("post = %+v", p)
		}
		must(0, "posts", "schedule", p.ID, "+1d")
		out := must(0, "schedule", "--collection", col, "--days", "3")
		mustContain(t, out, p.ID)
		must(0, "posts", "cancel", p.ID)
		json.Unmarshal([]byte(must(0, "posts", "get", p.ID, "--json")), &p)
		if p.Status != "draft" {
			t.Fatalf("after cancel: %s", p.Status)
		}
		must(0, "posts", "delete", p.ID)
	})

	t.Run("publish on your own automation and report", func(t *testing.T) {
		var p Post
		json.Unmarshal([]byte(must(0, "posts", "create", col, "--content", "live publish", "--channel", ch.ID, "--now", "--json")), &p)
		mustContain(t, must(0, "work"), p.ID)
		must(0, "posts", "result", p.ID, ch.ID, "--status", "published", "--url", "https://example.com/p/1")
		json.Unmarshal([]byte(must(0, "posts", "get", p.ID, "--json")), &p)
		if p.Status != "published" || p.Targets[0].ExternalURL != "https://example.com/p/1" {
			t.Fatalf("after result: %+v", p)
		}
		mustContain(t, must(0, "posts", "history", p.ID), "post.published")
	})

	t.Run("failed target exits 2 and can be retried", func(t *testing.T) {
		var p Post
		json.Unmarshal([]byte(must(0, "posts", "create", col, "--content", "live fail", "--channel", ch.ID, "--now", "--json")), &p)
		must(0, "posts", "result", p.ID, ch.ID, "--status", "failed", "--error", "nope")
		_, errOut, code := run("posts", "publish", p.ID, "--wait", "--timeout", "2s")
		if code != ExitTimeout && code != ExitFailed {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	})

	t.Run("events, jobs, usage errors", func(t *testing.T) {
		mustContain(t, must(0, "events", "tail", "--collection", col, "--type", "post.*", "--limit", "50"), "post.created")
		must(0, "jobs", "stats")
		must(0, "posts", "list", "--collection", col)
		must(ExitUsage, "posts", "schedule", "pst_x")
		must(ExitError, "posts", "get", "pst_doesnotexist")
	})
}
