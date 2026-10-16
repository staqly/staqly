package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/store"
)

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// logLines decodes the JSON log lines written so far.
func logLines(t *testing.T, buf *lockedBuf) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not JSON: %s", sc.Text())
		}
		if k := duplicateKey(sc.Bytes()); k != "" {
			t.Errorf("duplicate key %q in log line %s", k, sc.Text())
		}
		out = append(out, m)
	}
	return out
}

// duplicateKey returns the first key repeated in the line's top-level object
// (slog happily writes a base attr twice; a map would hide it).
func duplicateKey(line []byte) string {
	dec := json.NewDecoder(strings.NewReader(string(line)))
	if _, err := dec.Token(); err != nil {
		return ""
	}
	seen := map[string]bool{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return ""
		}
		key, _ := k.(string)
		if seen[key] {
			return key
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return ""
		}
	}
	return ""
}

func findLog(lines []map[string]any, msg, path string) map[string]any {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i]["msg"] == msg && (path == "" || lines[i]["path"] == path) {
			return lines[i]
		}
	}
	return nil
}

func TestStructuredRequestLogs(t *testing.T) {
	buf := &lockedBuf{}
	log := logctx.Base(logctx.New(buf, "json", slog.LevelInfo), "9.9.9", app.ModeCloud)
	database, err := store.Open(context.Background(), t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	svc := app.New(database, log, app.Config{Mode: app.ModeCloud, UploadDir: t.TempDir()})
	srv := httptest.NewServer(httpapi.New(svc, log, httpapi.Config{}))
	defer srv.Close()
	res, err := svc.Setup(context.Background(), "w", "", "")
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, hdr ...string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+res.APIKey.Key)
		req.Header.Set("User-Agent", "agent/"+strings.Repeat("x", 300))
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		b, _ := io.ReadAll(resp.Body)
		json.Unmarshal(b, &body)
		return resp.StatusCode, body
	}

	// traceparent + CF-Ray → correlated access line.
	if c, _ := call("/v1/items?q=supersecretquery", "traceparent", testTraceparent, "CF-Ray", "8f2c1a2b3c4d5e6f-AMS"); c != 200 {
		t.Fatalf("status %d", c)
	}
	line := findLog(logLines(t, buf), "http_request", "/v1/items")
	if line == nil {
		t.Fatalf("no access line: %s", buf.String())
	}
	want := map[string]any{"service": "staqly-api", "version": "9.9.9", "mode": "cloud", "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id": "00f067aa0ba902b7", "cf_ray": "8f2c1a2b3c4d5e6f-AMS", "method": "GET", "status": float64(200),
		"workspace_id": res.Workspace.ID, "principal": "api_key:" + res.APIKey.ID, "ip": "127.0.0.1", "level": "INFO"}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	for _, k := range []string{"request_id", "duration_ms", "bytes", "user_agent"} {
		if _, ok := line[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if ua, _ := line["user_agent"].(string); len(ua) != 200 {
		t.Errorf("user_agent not truncated to 200: %d", len(ua))
	}
	if strings.Contains(buf.String(), "supersecretquery") || strings.Contains(buf.String(), res.APIKey.Key) {
		t.Fatal("query string or key leaked into logs")
	}

	// An invalid traceparent / CF-Ray is ignored.
	call("/v1/workspace", "traceparent", "00-xyz-bad-01", "CF-Ray", "bad ray <x>")
	line = findLog(logLines(t, buf), "http_request", "/v1/workspace")
	if line == nil || line["trace_id"] != nil || line["cf_ray"] != nil {
		t.Fatalf("invalid headers logged: %v", line)
	}

	// A 500 logs the error with request context; the body carries trace_id.
	if _, err := database.Exec(`DROP TABLE webhook_deliveries`); err != nil {
		t.Fatal(err)
	}
	c, body := call("/v1/webhook-deliveries", "traceparent", testTraceparent)
	if c != 500 || body["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || body["request_id"] == nil {
		t.Fatalf("500 body: %d %v", c, body)
	}
	lines := logLines(t, buf)
	errLine := findLog(lines, "internal error", "")
	if errLine == nil || errLine["level"] != "ERROR" || errLine["request_id"] != body["request_id"] ||
		errLine["workspace_id"] != res.Workspace.ID || errLine["trace_id"] == nil || errLine["path"] != "/v1/webhook-deliveries" || errLine["err"] == nil {
		t.Fatalf("500 log line: %v", errLine)
	}
	if acc := findLog(lines, "http_request", "/v1/webhook-deliveries"); acc == nil || acc["level"] != "ERROR" {
		t.Fatalf("5xx access line must be error level: %v", acc)
	}

	// Probes are not logged at info level.
	resp, _ := http.Get(srv.URL + "/livez")
	resp.Body.Close()
	resp, _ = http.Get(srv.URL + "/health")
	resp.Body.Close()
	if findLog(logLines(t, buf), "http_request", "/livez") != nil || findLog(logLines(t, buf), "http_request", "/health") != nil {
		t.Fatal("probe requests logged")
	}
}
