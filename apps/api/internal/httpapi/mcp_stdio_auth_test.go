package httpapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/mcpserver"
)

func stdioClient(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "auth regression"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func stdioCall(t *testing.T, client *mcp.ClientSession, name string, args map[string]any, wantError bool) *mcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != wantError {
		t.Fatalf("%s IsError=%v, want %v: %+v", name, result.IsError, wantError, result)
	}
	return result
}

func TestMCPStdioRevokedKeyStopsOnNextCall(t *testing.T) {
	e := newEnv(t)
	p, err := e.svc.AuthenticateAPIKey(context.Background(), e.key)
	if err != nil {
		t.Fatal(err)
	}
	client := stdioClient(t, mcpserver.NewWithAuth(e.svc, mcpserver.KeyAuth(e.svc, e.key), "test"))
	stdioCall(t, client, "create_collection", map[string]any{"name": "Before revoke"}, false)
	e.do(e.browser, "DELETE", "/v1/api-keys/"+p.ID, "", nil, 204, nil, "X-CSRF-Token", e.csrf)
	stdioCall(t, client, "create_collection", map[string]any{"name": "After revoke"}, true)
	stdioCall(t, client, "list_collections", nil, true)
}

func TestMCPStdioKeyUsesCreatorsCurrentRole(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, _, _ := e.team()
	adminID := e.session(admin).User.ID
	var key app.APIKey
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "Stdio key", "scopes": []string{app.ScopeCollectionsRead, app.ScopeCollectionsWrite}}, 201, &key)
	client := stdioClient(t, mcpserver.NewWithAuth(e.svc, mcpserver.KeyAuth(e.svc, key.Key), "test"))
	stdioCall(t, client, "create_collection", map[string]any{"name": "Before demotion"}, false)
	e.as(owner, "PATCH", "/v1/members/"+adminID, map[string]any{"role": app.RoleViewer}, 200, nil)
	stdioCall(t, client, "list_collections", nil, false)
	result := stdioCall(t, client, "create_collection", map[string]any{"name": "After demotion"}, true)
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, app.ScopeCollectionsWrite) {
		t.Fatalf("missing current-role scope error: %+v", result)
	}
	e.as(owner, "DELETE", "/v1/members/"+adminID, nil, 204, nil)
	stdioCall(t, client, "list_collections", nil, true)
}

func TestMCPStdioLocalModeRemainsKeyless(t *testing.T) {
	svc, _ := newLocal(t)
	p, err := svc.LocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := stdioClient(t, mcpserver.New(svc, p, "test"))
	stdioCall(t, client, "create_collection", map[string]any{"name": "Local stdio"}, false)
	stdioCall(t, client, "list_collections", nil, false)
}
