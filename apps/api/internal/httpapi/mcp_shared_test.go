package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The /mcp server is built once and shared by every request; each tool call must still run as its own
// request's key. Keys with different scopes and workspaces call tools concurrently: a read-only key is
// always refused a write, a writer always succeeds, and each workspace only ever sees its own collections.
func TestMCPSharedServerKeepsPrincipalsApart(t *testing.T) {
	e := newEnv(t)
	e.collection("Only in A")
	reader := e.keyWith(app.ScopeCollectionsRead)
	writer := e.keyWith(app.ScopeCollectionsRead, app.ScopeCollectionsWrite)
	var other struct {
		APIKey app.APIKey `json:"api_key"`
	}
	e.do(nil, "POST", "/v1/workspaces", "", map[string]string{"name": "B", "setup_token": "tok"}, 201, &other)
	keyB := other.APIKey.Key
	mcpResult(e, keyB, "create_collection", map[string]any{"name": "Only in B"})

	call := func(key, tool string, args map[string]any) (map[string]any, error) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args}})
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("status %d: %w", resp.StatusCode, err)
		}

		return out, nil
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			out, err := call(reader, "create_collection", map[string]any{"name": fmt.Sprintf("reader %d", i)})
			if err != nil || !isToolError(out) || !strings.Contains(fmt.Sprint(out), "collections:write") {
				t.Errorf("read-only key created a collection: %v %v", out, err)
			}
		}()
		go func() {
			defer wg.Done()
			out, err := call(writer, "create_collection", map[string]any{"name": fmt.Sprintf("writer %d", i)})
			if err != nil || isToolError(out) {
				t.Errorf("writer refused: %v %v", out, err)
			}
		}()
		go func() {
			defer wg.Done()
			out, err := call(keyB, "list_collections", nil)
			if err != nil || isToolError(out) {
				t.Errorf("workspace B list: %v %v", out, err)

				return
			}
			s := fmt.Sprint(out)
			if !strings.Contains(s, "Only in B") || strings.Contains(s, "Only in A") || strings.Contains(s, "writer ") {
				t.Errorf("workspace B saw another workspace: %s", s)
			}
		}()
	}
	wg.Wait()

	// Workspace A got exactly the writer's collections, none from the reader.
	s := fmt.Sprint(mcpResult(e, e.key, "list_collections", nil))
	if strings.Contains(s, "reader ") || strings.Contains(s, "Only in B") || strings.Count(s, "writer ") != 8 {
		t.Fatalf("workspace A collections: %s", s)
	}
}
