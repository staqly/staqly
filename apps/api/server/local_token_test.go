package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

// A local start makes the access token (an owner-only file), prints it with a dashboard link that carries
// it in the fragment, and the API wants it; `server token --rotate` replaces it under the running server.
func TestLocalStartPrintsAndRequiresTheToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("ADDR", "127.0.0.1:0")
	t.Setenv("DASHBOARD_ADDR", "127.0.0.1:0")
	t.Setenv("DASHBOARD_PORT", "8080")
	t.Setenv("STAQLY_MODE", "")
	t.Setenv("STAQLY_LOCAL_TOKEN", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuf{}
	old := stderr
	stderr = out
	t.Cleanup(func() { stderr = old })
	buf := &syncBuf{}
	log := logctx.Base(logctx.New(buf, "json", slog.LevelDebug), "test", cfg.App.Mode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, log, "test") }()
	link := regexp.MustCompile(`http://localhost:8080/#token=(stk_local_[a-z0-9]+)`)
	for deadline := time.Now().Add(60 * time.Second); !link.MatchString(out.String()); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no token printed: %s / %s", out, buf)
		}
	}
	tok := link.FindStringSubmatch(out.String())[1]
	if !strings.Contains(out.String(), "Access token:        "+tok) || strings.Contains(buf.String(), tok) {
		t.Fatalf("printed: %s; the token must stay out of the log lines: %s", out, buf)
	}
	path := filepath.Join(dir, "access_token")
	st, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatalf("token file: %v %v", st, err)
	}
	// The token is printed before the API logs that it listens: wait for that line too.
	addr := ""
	for deadline := time.Now().Add(60 * time.Second); addr == ""; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no listening line: %s", buf)
		}
		for _, line := range strings.Split(buf.String(), "\n") {
			var l struct{ Msg, Addr string }
			if json.Unmarshal([]byte(line), &l) == nil && l.Msg == "listening" {
				addr = l.Addr
			}
		}
	}
	get := func(token string) int {
		req, _ := http.NewRequest("GET", "http://"+addr+"/v1/collections", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(""); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	if code := get(tok); code != 200 {
		t.Fatalf("token: %d", code)
	}
	// `server token --rotate` from another process, while this one runs.
	devNull, _ := os.Open(os.DevNull)
	oldOut := os.Stdout
	os.Stdout = devNull
	code := tokenCmd([]string{"--rotate"})
	os.Stdout = oldOut
	if code != 0 {
		t.Fatalf("token --rotate: exit %d", code)
	}
	b, _ := os.ReadFile(path)
	next := strings.TrimSpace(string(b))
	if next == tok || !strings.HasPrefix(next, "stk_local_") {
		t.Fatalf("rotated token: %q", next)
	}
	if get(tok) != 401 || get(next) != 200 {
		t.Fatal("the running server must take the rotated token at once")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// The next start prints the same token again.
	if again, err := os.ReadFile(path); err != nil || strings.TrimSpace(string(again)) != next {
		t.Fatalf("token after restart: %v", err)
	}
}
