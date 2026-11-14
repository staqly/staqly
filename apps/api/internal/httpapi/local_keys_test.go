package httpapi_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The Community Edition (local mode) has no API keys and no recovery key: nothing mints a key there, not
// even a recovery key left over from data that was served in cloud mode, `server recovery-key` refuses, and a
// key created in cloud mode on the same data is refused (401) once the server runs in local mode.
func TestLocalModeMintsNoKeys(t *testing.T) {
	svc, srv := newLocal(t)
	ctx := context.Background()
	local, err := svc.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// A recovery key and an API key from a time the same data ran in cloud mode.
	svc.Cfg.Mode = app.ModeCloud
	recovery, _, err := svc.ResetRecoveryKey(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPlan(ctx, local.WorkspaceID, "pro", ""); err != nil {
		t.Fatal(err)
	}
	old, err := svc.CreateAPIKey(ctx, local, app.CreateAPIKeyInput{Name: "from cloud", Scopes: []string{app.ScopeAdmin}})
	if err != nil {
		t.Fatal(err)
	}
	svc.Cfg.Mode = app.ModeLocal

	// The key from cloud mode stops working: the Community Edition takes only its access token.
	for _, path := range []string{"/v1/workspace", "/v1/collections"} {
		code, body := localReqWith(t, srv, "GET", path, "", "", "", old.Key)
		if code != 401 || !strings.Contains(body, "staqly serve token") {
			t.Fatalf("GET %s with an API key in local mode: %d %s", path, code, body)
		}
	}
	if code, body := localReqWith(t, srv, "POST", "/mcp", "", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, old.Key); code != 401 {
		t.Fatalf("MCP with an API key in local mode: %d %s", code, body)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, old.Key); !errors.Is(err, app.ErrLocalAPIKey) {
		t.Fatalf("AuthenticateAPIKey in local mode (MCP stdio, key re-checks): %v", err)
	}
	if code, _ := localReq(t, srv, "GET", "/v1/workspace", "", "", ""); code != 200 {
		t.Fatalf("the access token still works: %d", code)
	}

	if code, body := localReqWith(t, srv, "POST", "/v1/sessions/recover", "", "", `{"recovery_key":"`+recovery+`"}`, ""); code != 409 ||
		strings.Contains(body, "stk_") {
		t.Fatalf("recover in local mode: %d %s", code, body)
	}
	if code, body := localReq(t, srv, "POST", "/v1/api-keys", "", "", `{"name":"x","scopes":["admin"]}`); code != 409 {
		t.Fatalf("create key: %d %s", code, body)
	}
	if code, body := localReq(t, srv, "POST", "/v1/api-keys/"+old.ID+"/rotate", "", "", ""); code != 409 || strings.Contains(body, "stk_") {
		t.Fatalf("rotate key in local mode: %d %s", code, body)
	}
	if _, err := svc.RotateRecoveryKey(ctx, app.Principal{WorkspaceID: local.WorkspaceID, Kind: app.PrincipalSession, ID: "ses_x"}); err == nil ||
		!strings.Contains(err.Error(), "no recovery key") {
		t.Fatalf("rotate the recovery key in local mode: %v", err)
	}
	if _, _, err := svc.ResetRecoveryKey(ctx, ""); err == nil || !strings.Contains(err.Error(), "server token --rotate") {
		t.Fatalf("server recovery-key in local mode: %v", err)
	}
}
