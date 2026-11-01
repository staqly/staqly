package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

func adminReadTest(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{Mode: ModeCloud, UploadDir: dir + "/u"})
}

func adminExec(t *testing.T, s *Service, q string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

// seedWS: a workspace created at the given time with an owner user "<id>@x.io".
func seedWS(t *testing.T, s *Service, id string, created time.Time) {
	t.Helper()
	at := formatTime(created)
	adminExec(t, s, `INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at, plan)
		VALUES (?, ?, '', ?, ?, ?, 'starter')`, id, "WS "+id, at, at, at)
	adminExec(t, s, `INSERT INTO users (id, email, created_at, updated_at, last_login_at) VALUES (?, ?, ?, ?, ?)`,
		"u_"+id, id+"@Acme.io", at, at, at)
	adminExec(t, s, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at) VALUES (?, ?, 'owner', ?, ?)`,
		id, "u_"+id, at, at)
}

func TestAdminListWorkspacesCursor(t *testing.T) {
	s := adminReadTest(t)
	now := s.Now()
	for i, id := range []string{"w1", "w2", "w3"} {
		seedWS(t, s, id, now.Add(-time.Duration(3-i)*time.Hour))
	}
	ctx := context.Background()
	p1, err := s.AdminListWorkspaces(ctx, AdminWorkspaceFilter{Limit: 2})
	if err != nil || len(p1.Items) != 2 || p1.NextCursor == "" || p1.Items[0].ID != "w3" {
		t.Fatalf("page 1: %+v %v", p1, err)
	}
	p2, err := s.AdminListWorkspaces(ctx, AdminWorkspaceFilter{Limit: 2, Cursor: p1.NextCursor})
	if err != nil || len(p2.Items) != 1 || p2.NextCursor != "" || p2.Items[0].ID != "w1" {
		t.Fatalf("page 2: %+v %v", p2, err)
	}
	if _, err := s.AdminListWorkspaces(ctx, AdminWorkspaceFilter{Cursor: "!!"}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestAdminListWorkspacesOwnerEmailSearch(t *testing.T) {
	s := adminReadTest(t)
	seedWS(t, s, "w1", s.Now())
	seedWS(t, s, "w2", s.Now().Add(-time.Hour))
	pg, err := s.AdminListWorkspaces(context.Background(), AdminWorkspaceFilter{Q: "W2@ACME"})
	if err != nil || len(pg.Items) != 1 || pg.Items[0].ID != "w2" || pg.Items[0].OwnerEmail != "w2@Acme.io" || pg.Items[0].Members != 1 {
		t.Fatalf("%+v %v", pg, err)
	}
}

func TestAdminOverviewDaily30(t *testing.T) {
	s := adminReadTest(t)
	seedWS(t, s, "w1", s.Now().Add(-48*time.Hour))
	seedWS(t, s, "w2", s.Now().Add(-48*time.Hour))
	adminExec(t, s, `INSERT INTO channels (id, workspace_id, provider, name, created_at, updated_at) VALUES ('c1','w1','custom','A',?,?)`, s.now(), s.now())
	adminExec(t, s, `INSERT INTO channels (id, workspace_id, provider, name, created_at, updated_at, deleted_at) VALUES ('c2','w1','custom','B',?,?,?)`, s.now(), s.now(), s.now())
	o, err := s.AdminOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(o.SignupsDaily) != 30 || len(o.PublishedDaily) != 30 {
		t.Fatalf("days %d %d", len(o.SignupsDaily), len(o.PublishedDaily))
	}
	last := o.SignupsDaily[29]
	if last.Day != s.Now().UTC().Format("2006-01-02") || last.Count != 0 {
		t.Fatalf("last %+v", last)
	}
	if d := o.SignupsDaily[27]; d.Count != 2 || d.Day != s.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02") {
		t.Fatalf("day -2: %+v", d)
	}
	if o.Workspaces != 2 || o.Users != 2 || o.ActiveUsers7d != 2 || o.Channels.Total != 1 || o.Channels.ByProvider["custom"] != 1 {
		t.Fatalf("%+v", o)
	}
}

func TestAdminWorkspaceUsage(t *testing.T) {
	s := adminReadTest(t)
	seedWS(t, s, "w1", s.Now())
	adminExec(t, s, `INSERT INTO usage_counters (workspace_id, period, metric, count) VALUES ('w1', ?, 'posts_per_month', 4)`, s.period())
	d, err := s.AdminWorkspace(context.Background(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.GetUsage(context.Background(), Principal{WorkspaceID: "w1", Kind: PrincipalSystem, ID: "admin"})
	if err != nil || !reflect.DeepEqual(d.Usage, want) {
		t.Fatalf("usage differs: %v\n%+v\n%+v", err, d.Usage, want)
	}
	if d.Workspace.PostsThisPeriod != 4 || len(d.Members) != 1 || d.Channels == nil {
		t.Fatalf("%+v", d)
	}
	u, err := s.AdminUser(context.Background(), "u_w1")
	if err != nil || len(u.Workspaces) != 1 || u.Workspaces[0].Role != "owner" {
		t.Fatalf("%+v %v", u, err)
	}
	pg, err := s.AdminListUsers(context.Background(), "acme", "", 0)
	if err != nil || len(pg.Items) != 1 || pg.Items[0].Workspaces != 1 {
		t.Fatalf("%+v %v", pg, err)
	}
}

func TestAdminWorkspaceNotFound(t *testing.T) {
	s := adminReadTest(t)
	_, err := s.AdminWorkspace(context.Background(), "missing")
	if e, ok := err.(*Error); !ok || e.Code != "not_found" || e.Status != 404 {
		t.Fatalf("%v", fmt.Sprint(err))
	}
}
