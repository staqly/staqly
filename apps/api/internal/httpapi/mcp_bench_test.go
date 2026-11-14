package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// benchMCP sends one JSON-RPC request to /mcp through the real router (auth, rate limit, MCP handler)
// without a network round trip, b.N times.
func benchMCP(b *testing.B, body string) {
	b.Setenv("RATE_LIMIT_DISABLED", "true") // measure the handler, not the per-key limit
	e := newEnv(b)
	e.collection("Bench")
	h := e.srv.Config.Handler
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+e.key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"isError":true`) {
			b.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
}

// go test -run '^$' -bench MCP -benchmem ./internal/httpapi
func BenchmarkMCPToolCall(b *testing.B) {
	benchMCP(b, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_collections","arguments":{}}}`)
}

func BenchmarkMCPToolsList(b *testing.B) {
	benchMCP(b, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
}
