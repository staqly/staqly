package httpapi_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// inviteToken pulls the token out of an invitation link (/invite?token=...).
func inviteToken(t testing.TB, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Query().Get("token") == "" {
		t.Fatalf("invitation link: %q", link)
	}
	return u.Query().Get("token")
}

// attachAndClaim: the operator gives the workspace to an address, that person signs in with an email code
// through the invitation and lands in it as its account owner.
func (e *acctEnv) attachAndClaim(wsID, email string) *person {
	e.t.Helper()
	inv, err := e.svc.AttachWorkspace(context.Background(), wsID, email)
	if err != nil {
		e.t.Fatal(err)
	}
	if inv.Role != app.RoleOwner || inv.Email != email {
		e.t.Fatalf("invitation: %+v", inv)
	}
	owner := e.emailSignIn(email, map[string]any{"invite": inviteToken(e.t, inv.Link)})
	if s := e.session(owner); s.Workspace.ID != wsID || s.Role != app.RoleOwner || !s.AccountOwner || s.User.Email != email {
		e.t.Fatalf("after accepting: %+v", s)
	}
	return owner
}

// A cloud workspace from before accounts: attach-workspace hands it to a person; what signed in without a
// person ends, its API keys keep working for that owner, and it refuses the old ways in afterwards.
func TestAttachWorkspace(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	ctx := context.Background()
	old, err := e.svc.Setup(ctx, "Old workspace", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	ws := old.Workspace.ID
	link, err := e.svc.CreateDeviceLink(ctx, app.Principal{WorkspaceID: ws, Kind: app.PrincipalSystem, ID: "cli"}, "")
	if err != nil {
		t.Fatal(err)
	}
	legacy := []string{"Cookie", "staqly_session=" + old.Session.Token}
	e.do(nil, "GET", "/v1/session", "", nil, 200, nil, legacy...)

	// Refusals: an unknown workspace, a bad address.
	if _, err := e.svc.AttachWorkspace(ctx, "ws_nope", "ana@example.com"); err == nil {
		t.Fatal("unknown workspace attached")
	}
	if _, err := e.svc.AttachWorkspace(ctx, ws, "not an address"); err == nil {
		t.Fatal("bad address accepted")
	}
	// A second run replaces the first link.
	first, err := e.svc.AttachWorkspace(ctx, ws, "ana@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Someone else signing in through the link is refused: the invitation is for ana's address.
	e.sendCode("mallory@example.com", 202)
	var mismatch apiErr
	e.verifyCode("mallory@example.com", e.devCode("mallory@example.com"), map[string]any{"invite": inviteToken(t, first.Link)}, 403, &mismatch)
	if mismatch.Error.Code != "invitation_email_mismatch" {
		t.Fatalf("other address: %+v", mismatch)
	}

	owner := e.attachAndClaim(ws, "ana@example.com")
	clock.add(time.Minute) // one code per address every 30 seconds
	e.sendCode("ana@example.com", 202)
	var dead apiErr
	e.verifyCode("ana@example.com", e.devCode("ana@example.com"), map[string]any{"invite": inviteToken(t, first.Link)}, 410, &dead)

	// The old ways in are closed; the admin API key keeps working and now belongs to the owner.
	e.do(nil, "GET", "/v1/session", "", nil, 401, nil, legacy...)
	e.do(nil, "POST", "/v1/device-links/redeem", "", map[string]any{"token": link.URL[strings.Index(link.URL, "#")+1:]}, 410, nil) // expired by the attach
	var refused apiErr
	e.do(nil, "POST", "/v1/sessions/api-key", "", map[string]any{"api_key": old.APIKey.Key}, 403, &refused)
	if refused.Error.Code != "sign_in_with_account" {
		t.Fatalf("api key sign-in: %+v", refused)
	}
	e.do(nil, "POST", "/v1/sessions/recover", "", map[string]any{"recovery_key": old.RecoveryKey}, 403, &refused)
	if refused.Error.Code != "sign_in_with_account" {
		t.Fatalf("recovery: %+v", refused)
	}
	e.do(nil, "GET", "/v1/collections", old.APIKey.Key, nil, 200, nil)
	var creator string
	if err := e.svc.DB.QueryRow(`SELECT created_by_user_id FROM api_keys WHERE id = '` + old.APIKey.ID + `'`).Scan(&creator); err != nil ||
		creator != e.session(owner).User.ID {
		t.Fatalf("api key owner: %q %v", creator, err)
	}
	var events struct {
		Data []struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	e.as(owner, "GET", "/v1/events?type=workspace.account_attached", nil, 200, &events)
	if len(events.Data) != 1 || events.Data[0].Data["sessions_revoked"] != float64(1) || events.Data[0].Data["api_keys_adopted"] != float64(1) ||
		events.Data[0].Data["connect_links_expired"] != float64(1) {
		t.Fatalf("event: %+v", events.Data)
	}

	// Attached once: a second attach is refused.
	if _, err := e.svc.AttachWorkspace(ctx, ws, "bob@example.com"); err == nil || !strings.Contains(err.Error(), "already belongs to an account") {
		t.Fatalf("attach twice: %v", err)
	}
}

// Local mode has no accounts: nothing to attach to.
func TestAttachWorkspaceLocalRefused(t *testing.T) {
	database, err := storetest.Open(t, t.TempDir()+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	svc := app.New(database, nil, app.Config{Mode: app.ModeLocal})
	ctx := context.Background()
	if err := svc.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := svc.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachWorkspace(ctx, p.WorkspaceID, "ana@example.com"); err == nil || !strings.Contains(err.Error(), "accounts are off") {
		t.Fatalf("local attach: %v", err)
	}
}

// From a local install to a cloud server: import-sqlite, attach-workspace, sign in with an email code, and
// the local posts are there.
func TestImportThenAttach(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("needs STAQLY_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	path := t.TempDir() + "/local.db"
	lite, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	local := app.New(lite, nil, app.Config{Mode: app.ModeLocal})
	if err := local.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	if err := local.EnsureDefaultCollections(ctx); err != nil {
		t.Fatal(err)
	}
	lp, err := local.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := local.CreatePost(ctx, lp, app.PostInput{Collection: "Default", Content: fmt.Sprintf("local post %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	lite.Close()

	pg, err := storetest.Open(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportSQLite(ctx, pg, path); err != nil {
		t.Fatal(err)
	}
	e := newAccountsEnvOn(t, "", pg)
	e.svc.Cfg.EmailSignIn, e.svc.Cfg.DevSignIn = true, true
	owner := e.attachAndClaim(lp.WorkspaceID, "ana@example.com")
	var posts struct{ Data []app.Post }
	e.as(owner, "GET", "/v1/posts?limit=50", nil, 200, &posts)
	if len(posts.Data) != 3 {
		t.Fatalf("imported posts after attach: %d", len(posts.Data))
	}
}
