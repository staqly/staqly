package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// Was a bug: sign_in_method.* and access_token.* were audit families here and missing from the SQL of the
// events list and the export, so viewers read them. Every family in auditFamilies stays out of both for
// a caller without admin; a new family fails here until the two queries name it.
func TestAuditFamiliesAreHiddenWithoutAdmin(t *testing.T) {
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: dir + "/uploads", LocalTokenPath: dir + "/access_token"})
	if err := s.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.tx(ctx, func(q *db.Queries) error {
		for fam := range auditFamilies {
			if _, err := s.emit(ctx, q, p, fam+".changed", "", "", nil); err != nil {
				return err
			}
		}
		_, err := s.emit(ctx, q, p, "post.created", "", "", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for audit, want := range map[int64]int{0: 1, 1: len(auditFamilies) + 1} {
		listed, err := s.Q.ListEvents(ctx, db.ListEventsParams{WorkspaceID: p.WorkspaceID, IncludeAudit: audit, Lim: 100})
		if err != nil {
			t.Fatal(err)
		}
		exported, err := s.Q.ExportEventsPage(ctx, db.ExportEventsPageParams{WorkspaceID: p.WorkspaceID, IncludeAudit: audit, Lim: 100})
		if err != nil {
			t.Fatal(err)
		}
		var types []string
		for _, e := range listed {
			if e.WorkspaceID == p.WorkspaceID && (e.Type == "post.created" || isAuditEvent(e.Type)) {
				types = append(types, e.Type)
			}
		}
		n := 0
		for _, e := range exported {
			if e.Type == "post.created" || isAuditEvent(e.Type) {
				n++
			}
		}
		if len(types) != want || n != want {
			t.Errorf("include_audit=%d: the list has %d (%v) and the export %d of the test's events, want %d", audit, len(types), types, n, want)
		}
	}
}
