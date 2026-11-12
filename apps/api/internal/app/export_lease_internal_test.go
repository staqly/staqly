package app

import (
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

func exportStatus(t *testing.T, s *Service, id string) string {
	t.Helper()
	st, err := s.Q.ExportStatusByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// A server process dies while building an export and another one (or the same, restarted) looks a few
// minutes later: the export left `running` is built again once its lease ran out, instead of blocking every
// new export of the workspace. One whose builder still renews its lease is left alone.
func TestExportLeftRunningIsRecovered(t *testing.T) {
	s, p, _ := publishTest(t)
	p.Scopes = AllScopes
	dir := t.TempDir()
	s.Getenv = func(k string) string { // the ZIP goes to DATA_DIR/exports
		if k == "DATA_DIR" {
			return dir
		}
		return ""
	}
	s.pg = true // several server processes: nothing is requeued just because this one started
	now := time.Now()
	s.Now = func() time.Time { return now }
	ctx := t.Context()
	x, err := s.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	claimed := formatTime(now)
	if n, err := s.W.ClaimWorkspaceExport(ctx, db.ClaimWorkspaceExportParams{StartedAt: nullStr(claimed), ID: x.ID}); err != nil || n != 1 {
		t.Fatal(n, err)
	}

	// The builder is alive: it renews its lease, and four minutes after the last beat nobody takes the export.
	now = now.Add(4 * time.Minute)
	lease, ok, err := s.renewExportLease(ctx, x.ID, claimed)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	now = now.Add(4 * time.Minute)
	s.ProcessExports(ctx)
	if st := exportStatus(t, s, x.ID); st != "running" {
		t.Fatalf("a live export was taken from its builder: %s", st)
	}

	// The builder died: once the lease ran out the export is built again.
	now = now.Add(2 * time.Minute)
	s.ProcessExports(ctx)
	if st := exportStatus(t, s, x.ID); st != "ready" {
		_, rerr := s.RequestWorkspaceExport(ctx, p)
		t.Fatalf("the abandoned export is %q after its lease ran out; a new export answers: %v", st, rerr)
	}

	// The old builder was only stalled: it no longer owns the export and cannot renew or finish it.
	if _, ok, err := s.renewExportLease(ctx, x.ID, lease); err != nil || ok {
		t.Fatalf("a lost lease was renewed: %v %v", ok, err)
	}
	n, err := s.W.FinishWorkspaceExport(ctx, db.FinishWorkspaceExportParams{Status: "failed", Error: "export failed",
		FinishedAt: nullStr(s.now()), ExpiresAt: s.now(), StartedAt: nullStr(claimed), ID: x.ID, Lease: nullStr(lease)})
	if err != nil || n != 0 || exportStatus(t, s, x.ID) != "ready" {
		t.Fatalf("the old builder finished an export it lost: %d %v", n, err)
	}
	got, err := s.GetWorkspaceExport(ctx, p, x.ID)
	if err != nil || got.StartedAt == nil || *got.StartedAt != formatTime(now) {
		t.Fatalf("started_at %v, want when the finished build began (%s): %v", got.StartedAt, formatTime(now), err)
	}
}
