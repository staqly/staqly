package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/blob/blobtest"
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

// slowUpload is the archive store whose first upload waits for release (a builder stalled before its Put).
type slowUpload struct {
	blob.Store
	stall   atomic.Int32
	waiting chan struct{}
	release chan struct{}
}

func (b *slowUpload) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if strings.Contains(key, "/backups/") && b.stall.Add(-1) >= 0 {
		b.waiting <- struct{}{}
		<-b.release
	}
	return b.Store.Put(ctx, key, r, size, contentType)
}

// Was a bug: on S3 builder A stalled past its lease before uploading, builder B took the export over and
// finished it; A then uploaded its older archive under the same object key, over B's. Each builder has a key
// of its own now, the export records the winner's, and the loser removes what it uploaded.
func TestStaleExportBuilderOnS3LeavesTheWinnersArchive(t *testing.T) {
	s, p, _ := exportTest(t)
	ctx := t.Context()
	fake := blobtest.NewS3(t, "media")
	st, err := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Bucket: "media", AccessKey: "test", SecretKey: "test-secret", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	b := &slowUpload{Store: st, waiting: make(chan struct{}, 1), release: make(chan struct{})}
	s.Cfg.Blob, s.Cfg.Mode = b, ModeCloud
	s.pg = true // several server processes: nothing is requeued just because this one started
	var ahead atomic.Int64
	start := time.Now()
	s.Now = func() time.Time { return start.Add(time.Duration(ahead.Load())) }
	x, err := s.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	b.stall.Store(1)
	aDone := make(chan struct{})
	go func() { s.ProcessExports(ctx); close(aDone) }() // builder A
	<-b.waiting                                         // A built its ZIP and is stalled before the upload
	if _, err := s.CreateCollection(ctx, p, CollectionInput{Name: ptrTo("Only in the winner's archive")}); err != nil {
		t.Fatal(err)
	}
	ahead.Store(int64(exportLease + time.Minute))
	s.ProcessExports(ctx) // builder B: the lease ran out, it builds the export again
	download := func() []byte {
		t.Helper()
		f, _, err := s.OpenWorkspaceExport(ctx, p, x.ID)
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if st := exportStatus(t, s, x.ID); st != "ready" {
		t.Fatalf("setup: builder B left the export %s", st)
	}
	winner := download()
	close(b.release) // A wakes up and uploads its archive
	<-aDone
	if st := exportStatus(t, s, x.ID); st != "ready" {
		t.Fatalf("after the stale builder ended the export is %s", st)
	}
	if got := download(); !bytes.Equal(got, winner) {
		t.Fatalf("the download is %d bytes, not the winner's %d: the stale builder replaced the archive", len(got), len(winner))
	}
	if keys := fake.Keys("media"); len(keys) != 1 {
		t.Fatalf("objects left in the bucket: %v, want the winner's archive only", keys)
	}
}

// An export finished before each builder had its own key has no object_key: its archive is under the shared
// name, where the download finds it and the hourly sweep leaves it.
func TestExportOnS3WithoutARecordedKeyIsStillServed(t *testing.T) {
	s, p, _ := exportTest(t)
	ctx := t.Context()
	fake := blobtest.NewS3(t, "media")
	st, err := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Bucket: "media", AccessKey: "test", SecretKey: "test-secret", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Cfg.Blob, s.Cfg.Mode = st, ModeCloud
	x, err := s.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	s.ProcessExports(ctx)
	keys := fake.Keys("media")
	if len(keys) != 1 {
		t.Fatalf("setup: objects %v", keys)
	}
	data := fake.Get("media", keys[0])
	old := s.exportKey(p.WorkspaceID, x.ID)
	if err := st.Put(ctx, old, bytes.NewReader(data), int64(len(data)), "application/zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE workspace_exports SET object_key = '' WHERE id = ?`, x.ID); err != nil {
		t.Fatal(err)
	}
	exportS3Sweeps.Delete(s) // the sweep is due
	s.ProcessExports(ctx)
	if keys := fake.Keys("media"); len(keys) != 1 || keys[0] != old {
		t.Fatalf("after the sweep the bucket holds %v, want only %s", keys, old)
	}
	f, _, err := s.OpenWorkspaceExport(ctx, p, x.ID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer f.Close()
	if got, _ := io.ReadAll(f); !bytes.Equal(got, data) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(data))
	}
}
