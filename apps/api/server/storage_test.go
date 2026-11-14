package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/blob/blobtest"
)

// `server storage migrate` copies UPLOAD_DIR into the S3 bucket; a dry run writes nothing; a second run copies
// nothing; with STORAGE_DRIVER=disk it refuses (the folder is the storage already).
func TestStorageMigrate(t *testing.T) {
	data := t.TempDir()
	uploads := filepath.Join(data, "uploads")
	for k, v := range map[string]string{"ab/abcd.png": "png", "cd/cdef.mp4": "video", "thumbs/ab/abcd_160.jpg": "t"} {
		p := filepath.Join(uploads, filepath.FromSlash(k))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DATA_DIR", data)
	t.Setenv("UPLOAD_DIR", uploads)
	if code := Main([]string{"storage", "migrate"}, "test"); code != 2 {
		t.Fatalf("disk storage: exit %d, want 2", code)
	}

	fake := blobtest.NewS3(t, "media")
	t.Setenv("STORAGE_DRIVER", "s3")
	t.Setenv("S3_ENDPOINT", fake.URL)
	t.Setenv("S3_BUCKET", "media")
	t.Setenv("S3_ACCESS_KEY_ID", "k")
	t.Setenv("S3_SECRET_ACCESS_KEY", "s")
	t.Setenv("S3_FORCE_PATH_STYLE", "true")
	if code := Main([]string{"storage", "migrate", "--dry-run"}, "test"); code != 0 || len(fake.Keys("media")) != 0 {
		t.Fatalf("dry run: exit %d, bucket %v", code, fake.Keys("media"))
	}
	if code := Main([]string{"storage", "migrate"}, "test"); code != 0 || len(fake.Keys("media")) != 3 {
		t.Fatalf("migrate: exit %d, bucket %v", code, fake.Keys("media"))
	}
	if string(fake.Get("media", "cd/cdef.mp4")) != "video" {
		t.Fatal("content not copied")
	}
	if code := Main([]string{"storage", "migrate", "--from", uploads}, "test"); code != 0 {
		t.Fatalf("second run: exit %d", code)
	}
	if code := Main([]string{"storage", "copy"}, "test"); code != 2 {
		t.Fatalf("unknown subcommand: exit %d", code)
	}

	// Incomplete S3 settings refuse to load.
	t.Setenv("S3_BUCKET", "")
	if code := Main([]string{"storage", "migrate"}, "test"); code != 2 {
		t.Fatalf("missing S3_BUCKET: exit %d, want 2", code)
	}
}
