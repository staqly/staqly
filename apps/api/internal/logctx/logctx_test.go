package logctx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseTraceparent(t *testing.T) {
	tid, sid, ok := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok || tid != "4bf92f3577b34da6a3ce929d0e0e4736" || sid != "00f067aa0ba902b7" {
		t.Fatalf("valid header: %q %q %v", tid, sid, ok)
	}
	if _, _, ok := ParseTraceparent("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-future"); !ok {
		t.Fatal("future versions may carry extra fields")
	}
	for _, bad := range []string{
		"",
		"garbage",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",   // uppercase
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",   // zero trace id
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",   // zero span id
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",   // invalid version
		"00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01",    // short trace id
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-x", // v00 with extra field
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902bz-01",   // non-hex
	} {
		if _, _, ok := ParseTraceparent(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestValidCFRay(t *testing.T) {
	for v, want := range map[string]bool{"8f2c1a2b3c4d5e6f-AMS": true, "8f2c1a2b3c4d5e6f": true, "": false,
		"bad ray": false, "x\ny": false, "<script>": false, strings.Repeat("a", 65): false} {
		if ValidCFRay(v) != want {
			t.Errorf("ValidCFRay(%q) != %v", v, want)
		}
	}
}

func TestParseLevelAndFormat(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "INFO": slog.LevelInfo, "debug": slog.LevelDebug,
		" Warn ": slog.LevelWarn, "warning": slog.LevelWarn, "ERROR": slog.LevelError} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Fatal("invalid level accepted")
	}
	for in, want := range map[string]string{"": "json", "JSON": "json", "text": "text"} {
		if got, err := ParseFormat(in, "json"); err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseFormat("logfmt", "json"); err == nil {
		t.Fatal("invalid format accepted")
	}
}

func TestContextLoggerAndBase(t *testing.T) {
	var buf bytes.Buffer
	l := Base(New(&buf, "json", slog.LevelInfo), "1.2.3", "cloud")
	ctx := With(NewContext(context.Background(), l), "workspace_id", "ws_1")
	FromContext(ctx).Info("hello")
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["service"] != "staqly-api" || m["version"] != "1.2.3" || m["mode"] != "cloud" || m["workspace_id"] != "ws_1" {
		t.Fatalf("line: %v", m)
	}
	if FromContext(context.Background()) != slog.Default() {
		t.Fatal("fallback should be slog.Default")
	}
}
