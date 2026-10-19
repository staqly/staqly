package mcpserver

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
)

var update = flag.Bool("update", false, "rewrite testdata/tools.golden.json")

// listTools returns tools/list of srv as a client sees it (names, descriptions, annotations, schemas).
func listTools(t *testing.T, srv *mcp.Server) []byte {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.MarshalIndent(res.Tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return append(out, '\n')
}

// The tool list (names, descriptions, annotations, input and output schemas) is pinned: the shared HTTP
// server and the STDIO servers must publish exactly this list. After an intended tool change run
// go test ./internal/mcpserver -run TestToolListGolden -update
func TestToolListGolden(t *testing.T) {
	got := listTools(t, New(nil, app.Principal{}, "test"))
	const path = "testdata/tools.golden.json"
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("tool list differs from %s (run with -update after an intended change)", path)
	}
	// The shared HTTP server and the re-validating stdio server publish the same list.
	shared := listTools(t, newServer(&tools{principalOf: httpapi.PrincipalFrom}, "test"))
	stdio := listTools(t, NewWithAuth(nil, func(context.Context) app.Principal { return app.Principal{} }, "test"))
	if string(shared) != string(want) || string(stdio) != string(want) {
		t.Fatal("HTTP or stdio server publishes a different tool list")
	}
}
