package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/config"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// duplicateKeys walks one JSON value as a token stream (a map would silently
// keep the last of two equal keys) and returns keys repeated in any object.
func duplicateKeys(dec *json.Decoder) ([]string, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	var dups []string
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := k.(string)
			if seen[key] {
				dups = append(dups, key)
			}
			seen[key] = true
			d, err := duplicateKeys(dec)
			if err != nil {
				return nil, err
			}
			dups = append(dups, d...)
		}
		_, err = dec.Token()
	case json.Delim('['):
		for dec.More() {
			d, err := duplicateKeys(dec)
			if err != nil {
				return nil, err
			}
			dups = append(dups, d...)
		}
		_, err = dec.Token()
	}
	return dups, err
}

// The base logger carries service/version/mode; no line may repeat them (or
// any other key): log pipelines disagree on which duplicate wins.
func TestStartupLogsHaveNoDuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("ADDR", "127.0.0.1:0")
	t.Setenv("STAQLY_MODE", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	buf := &syncBuf{}
	log := logctx.Base(logctx.New(buf, "json", slog.LevelDebug), "test", cfg.App.Mode)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, log, "test") }()
	// Generous: CI runs every package at once under -race, and a fresh SQLite runs all migrations first.
	deadline := time.Now().Add(60 * time.Second)
	for !strings.Contains(buf.String(), `"msg":"listening"`) {
		if time.Now().After(deadline) {
			t.Fatalf("no listening line: %s", buf)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	lines := 0
	for sc.Scan() {
		lines++
		dups, err := duplicateKeys(json.NewDecoder(bytes.NewReader(sc.Bytes())))
		if err != nil {
			t.Fatalf("not JSON: %s", sc.Text())
		}
		if len(dups) > 0 {
			t.Errorf("duplicate keys %v in %s", dups, sc.Text())
		}
	}
	if lines < 2 { // listening + shutting down
		t.Fatalf("too few lines: %s", buf)
	}
}
