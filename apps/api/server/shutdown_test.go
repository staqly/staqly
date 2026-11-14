package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

// A request still running when the drain ends (a slow download, a stalled upload) must not turn a
// SIGTERM into "fatal" and exit 1: the remaining connections are closed and the stop is clean.
func TestShutdownWithStuckRequestIsClean(t *testing.T) {
	old := shutdownTimeout
	shutdownTimeout = 300 * time.Millisecond
	t.Cleanup(func() { shutdownTimeout = old })
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("ADDR", "127.0.0.1:0")
	t.Setenv("STAQLY_MODE", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	buf := &syncBuf{}
	log := logctx.Base(logctx.New(buf, "json", slog.LevelDebug), "test", cfg.App.Mode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, log, "test") }()
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
	// A body that never arrives: the handler waits on it, so the connection never goes idle.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /v1/collections HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"na")
	time.Sleep(200 * time.Millisecond) // let the handler start reading
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown with a stuck request returned %v; logs: %s", err, buf)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("shutdown hung")
	}
}
