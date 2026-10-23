package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/blob/blobtest"
)

func useBackupS3(t *testing.T, e *env) *blobtest.S3 {
	t.Helper()
	fake := blobtest.NewS3(t, "backups", "media")
	st, err := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Bucket: "backups", AccessKey: "test", SecretKey: "test-secret", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	e.svc.Cfg.BackupBlob = st
	return fake
}

func TestWorkspaceExportS3Lifecycle(t *testing.T) {
	for _, cleanup := range []string{"expiry", "purge", "delete workspace"} {
		t.Run(cleanup, func(t *testing.T) {
			e := newEnv(t)
			dir := withDataDir(e)
			fake := useBackupS3(t, e)
			ctx := context.Background()
			col := e.collection("Photos")
			var media app.Media
			e.upload("/v1/media", e.key, nil, "photo.png", pngBytes, 201, &media)
			post := e.post(col.ID, map[string]any{"content": "private post", "media": []string{media.ID}})
			key := e.keyWith(app.ScopeAdmin)
			var job app.WorkspaceExport
			e.do(nil, "POST", "/v1/workspace/export", key, nil, 202, &job)
			e.svc.ProcessExports(ctx)
			e.do(nil, "GET", "/v1/workspace/exports/"+job.ID, key, nil, 200, &job)
			data := fake.Get("backups", job.ID+".zip")
			if job.Status != "ready" || int64(len(data)) != job.SizeBytes || len(data) == 0 {
				t.Fatalf("export not stored in backup bucket: %+v, bytes=%d", job, len(data))
			}
			if len(fake.Keys("media")) != 0 {
				t.Fatal("backup leaked into media bucket")
			}
			if _, err := os.Stat(filepath.Join(dir, "exports", job.ID+".zip")); !os.IsNotExist(err) {
				t.Fatalf("S3 export left on disk: %v", err)
			}
			e.do(nil, "GET", job.DownloadURL, e.key, nil, 403, nil)
			other := newWorkspace(e, "Other")
			e.do(other.browser, "GET", job.DownloadURL, "", nil, 404, nil)
			status, headers, downloaded := e.raw(nil, "GET", job.DownloadURL, key, nil)
			if status != 200 || headers.Get("Content-Type") != "application/zip" || !bytes.Equal(downloaded, data) {
				t.Fatalf("download: %d, %v", status, headers)
			}
			files := readZip(t, downloaded)
			if !bytes.Contains(files["posts.ndjson"], []byte("private post")) || !bytes.Equal(files["media/photo.png"], pngBytes) {
				t.Fatal("S3 export lost JSON or media")
			}
			status, _, partial := e.raw(nil, "GET", job.DownloadURL, key, nil, "Range", "bytes=0-9")
			if status != 206 || !bytes.Equal(partial, data[:10]) {
				t.Fatalf("range download: %d", status)
			}
			switch cleanup {
			case "expiry":
				e.svc.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
				e.do(nil, "GET", job.DownloadURL, key, nil, 410, nil)
				e.svc.ProcessExports(ctx)
			case "purge":
				e.do(nil, "POST", "/v1/posts/"+post.ID+"/purge", key, nil, 200, nil)
				e.do(nil, "GET", job.DownloadURL, key, nil, 410, nil)
			case "delete workspace":
				e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, "X-CSRF-Token", e.csrf)
			}
			if len(fake.Keys("backups")) != 0 {
				t.Fatalf("backup survived %s", cleanup)
			}
		})
	}
}

func TestWorkspaceExportS3LegacyDownloadAndOrphanCleanup(t *testing.T) {
	e := newEnv(t)
	dir := withDataDir(e)
	key := e.keyWith(app.ScopeAdmin)
	var job app.WorkspaceExport
	e.do(nil, "POST", "/v1/workspace/export", key, nil, 202, &job)
	e.svc.ProcessExports(context.Background())
	e.do(nil, "GET", "/v1/workspace/exports/"+job.ID, key, nil, 200, &job)
	fake := useBackupS3(t, e)
	e.do(nil, "GET", job.DownloadURL, key, nil, 200, nil)
	if err := e.svc.Cfg.BackupBlob.Put(context.Background(), "exp_orphan.zip", bytes.NewReader([]byte("orphan")), 6, "application/zip"); err != nil {
		t.Fatal(err)
	}
	e.svc.ProcessExports(context.Background())
	if len(fake.Keys("backups")) != 0 {
		t.Fatal("orphan archive was not deleted")
	}
	e.svc.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	e.svc.ProcessExports(context.Background())
	if _, err := os.Stat(filepath.Join(dir, "exports", job.ID+".zip")); !os.IsNotExist(err) {
		t.Fatalf("legacy archive survived expiry: %v", err)
	}
}

type failingBackupUpload struct{ blob.Store }

func (f failingBackupUpload) Put(context.Context, string, io.Reader, int64, string) error {
	return errors.New("upload unavailable")
}

func TestWorkspaceExportS3UploadFailure(t *testing.T) {
	e := newEnv(t)
	dir := withDataDir(e)
	fake := useBackupS3(t, e)
	e.svc.Cfg.BackupBlob = failingBackupUpload{e.svc.Cfg.BackupBlob}
	key := e.keyWith(app.ScopeAdmin)
	var job app.WorkspaceExport
	e.do(nil, "POST", "/v1/workspace/export", key, nil, 202, &job)
	e.svc.ProcessExports(context.Background())
	e.do(nil, "GET", "/v1/workspace/exports/"+job.ID, key, nil, 200, &job)
	if job.Status != "failed" || job.DownloadURL != "" {
		t.Fatalf("failed upload offered for download: %+v", job)
	}
	if len(fake.Keys("backups")) != 0 {
		t.Fatal("failed upload left an object")
	}
	if _, err := os.Stat(filepath.Join(dir, "exports", job.ID+".zip")); !os.IsNotExist(err) {
		t.Fatalf("failed upload left a ZIP: %v", err)
	}
}

// The worker must retry failed object deletions even after the row is expired.
type failingBackupDelete struct {
	blob.Store
	unavailable  bool
	beforeDelete func(context.Context)
}

func (f *failingBackupDelete) Delete(ctx context.Context, key string) error {
	if f.beforeDelete != nil {
		f.beforeDelete(ctx)
	}
	if f.unavailable {
		return errors.New("delete unavailable")
	}
	return f.Store.Delete(ctx, key)
}

func TestWorkspaceExportS3RetriesExpiredDeletion(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	fake := useBackupS3(t, e)
	store := &failingBackupDelete{Store: e.svc.Cfg.BackupBlob, unavailable: true}
	e.svc.Cfg.BackupBlob = store
	key := e.keyWith(app.ScopeAdmin)
	var job app.WorkspaceExport
	e.do(nil, "POST", "/v1/workspace/export", key, nil, 202, &job)
	e.svc.ProcessExports(context.Background())
	e.svc.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	e.svc.ProcessExports(context.Background())
	e.do(nil, "GET", "/v1/workspace/exports/"+job.ID+"/download", key, nil, 410, nil)
	if len(fake.Keys("backups")) != 1 {
		t.Fatal("expected the failed deletion to leave the object")
	}
	store.unavailable = false
	e.svc.ProcessExports(context.Background())
	if len(fake.Keys("backups")) != 0 {
		t.Fatal("expired object deletion was not retried")
	}
}

func TestWorkspaceExportS3PurgeRevokesBeforeDeletion(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	fake := useBackupS3(t, e)
	post := e.post(e.collection("Private").ID, map[string]any{"content": "erase me"})
	key := e.keyWith(app.ScopeAdmin)
	var job app.WorkspaceExport
	e.do(nil, "POST", "/v1/workspace/export", key, nil, 202, &job)
	e.svc.ProcessExports(context.Background())
	var statusAtDelete string
	store := &failingBackupDelete{Store: e.svc.Cfg.BackupBlob, unavailable: true,
		beforeDelete: func(ctx context.Context) {
			statusAtDelete, _ = e.svc.Q.ExportStatusByID(ctx, job.ID)
		}}
	e.svc.Cfg.BackupBlob = store
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/purge", key, nil, 200, nil)
	if statusAtDelete != "expired" {
		t.Fatalf("backup still accessible during remote deletion: %s", statusAtDelete)
	}
	e.do(nil, "GET", "/v1/workspace/exports/"+job.ID+"/download", key, nil, 410, nil)
	if len(fake.Keys("backups")) != 1 {
		t.Fatal("expected failed deletion to leave an inaccessible object")
	}
	store.unavailable = false
	e.svc.ProcessExports(context.Background())
	if len(fake.Keys("backups")) != 0 {
		t.Fatal("purged export deletion was not retried")
	}
}
