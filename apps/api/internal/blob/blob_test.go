package blob_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/blob/blobtest"
)

func s3Store(t *testing.T, fake *blobtest.S3, prefix string) *blob.S3 {
	t.Helper()
	s, err := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Region: "us-east-1", Bucket: "media", AccessKey: "k",
		SecretKey: "s", PathStyle: true, Prefix: prefix, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Both drivers behave the same: put, open with seek (HTTP ranges), stat, walk, delete, missing keys.
func TestStores(t *testing.T) {
	ctx := context.Background()
	fake := blobtest.NewS3(t, "media")
	stores := map[string]blob.Store{"disk": blob.NewDisk(t.TempDir()), "s3": s3Store(t, fake, "staqly/")}
	for name, st := range stores {
		t.Run(name, func(t *testing.T) {
			if err := st.Check(ctx, false); err != nil {
				t.Fatal(err)
			}
			body := []byte("hello, media library")
			if err := st.Put(ctx, "ab/abcdef.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
				t.Fatal(err)
			}
			if err := st.Put(ctx, "thumbs/ab/abcdef_160.jpg", strings.NewReader("thumb"), 5, "image/jpeg"); err != nil {
				t.Fatal(err)
			}
			if n, err := st.Stat(ctx, "ab/abcdef.txt"); err != nil || n != int64(len(body)) {
				t.Fatalf("stat: %d %v", n, err)
			}
			f, err := st.Open(ctx, "ab/abcdef.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Seek(7, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(f)
			f.Close()
			if string(got) != "media library" {
				t.Fatalf("seek+read: %q", got)
			}
			var keys []string
			if err := st.Walk(ctx, "", func(k string, _ int64) error { keys = append(keys, k); return nil }); err != nil {
				t.Fatal(err)
			}
			sort.Strings(keys)
			if strings.Join(keys, ",") != "ab/abcdef.txt,thumbs/ab/abcdef_160.jpg" {
				t.Fatalf("walk: %v", keys)
			}
			if err := st.Delete(ctx, "ab/abcdef.txt"); err != nil {
				t.Fatal(err)
			}
			if err := st.Delete(ctx, "ab/abcdef.txt"); err != nil { // missing is fine
				t.Fatal(err)
			}
			if _, err := st.Open(ctx, "ab/abcdef.txt"); !errors.Is(err, blob.ErrNotExist) {
				t.Fatalf("open deleted: %v", err)
			}
			if _, err := st.Stat(ctx, "ab/abcdef.txt"); !errors.Is(err, blob.ErrNotExist) {
				t.Fatalf("stat deleted: %v", err)
			}
			if _, err := st.Open(ctx, "../etc/passwd"); err == nil {
				t.Fatal("a key escaping the root was accepted")
			}
		})
	}
	// The prefix is a folder in the bucket.
	if keys := fake.Keys("media"); strings.Join(keys, ",") != "staqly/thumbs/ab/abcdef_160.jpg" {
		t.Fatalf("bucket keys: %v", keys)
	}
}

// An upload staged in the disk store's TempDir is moved into place, not copied.
func TestDiskPutRenamesStagedFile(t *testing.T) {
	ctx := context.Background()
	d := blob.NewDisk(t.TempDir())
	if err := d.Check(ctx, false); err != nil {
		t.Fatal(err)
	}
	tmp, err := os.CreateTemp(d.TempDir(), ".upload-*")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("staged")
	tmp.Seek(0, io.SeekStart)
	if err := d.Put(ctx, "st/staged.bin", tmp, 6, ""); err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	if _, err := os.Stat(tmp.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staged file still in TempDir")
	}
	b, _ := os.ReadFile(filepath.Join(d.Root, "st", "staged.bin"))
	if string(b) != "staged" {
		t.Fatalf("moved file: %q", b)
	}
	// TempDir is not part of the store's files.
	n := 0
	_ = d.Walk(ctx, "", func(string, int64) error { n++; return nil })
	if n != 1 {
		t.Fatalf("walk saw %d files, want 1", n)
	}
}

func TestS3CheckAndSignedURL(t *testing.T) {
	ctx := context.Background()
	fake := blobtest.NewS3(t) // no buckets yet
	st := s3Store(t, fake, "")
	if err := st.Check(ctx, false); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing bucket: %v", err)
	}
	if err := st.Check(ctx, true); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	u, err := st.SignedURL(ctx, "ab/x.jpg", time.Hour)
	if err != nil || !strings.HasPrefix(u, fake.URL+"/media/ab/x.jpg?") || !strings.Contains(u, "X-Amz-Signature=") {
		t.Fatalf("signed url: %s %v", u, err)
	}
	pub, _ := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Bucket: "media", AccessKey: "k", SecretKey: "s",
		PathStyle: true, PublicURL: "https://cdn.example.com/"})
	if u, _ := pub.SignedURL(ctx, "ab/x y.jpg", time.Hour); u != "https://cdn.example.com/ab/x%20y.jpg" {
		t.Fatalf("public url: %s", u)
	}
	tunnel, _ := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Bucket: "media", AccessKey: "k", SecretKey: "s",
		PathStyle: true, SignEndpoint: "https://media.example.com"})
	if u, _ := tunnel.SignedURL(ctx, "ab/x.jpg", time.Hour); !strings.HasPrefix(u, "https://media.example.com/media/ab/x.jpg?") {
		t.Fatalf("sign endpoint url: %s", u)
	}
	if missing := (blob.S3Config{Endpoint: "x"}).Missing(); len(missing) != 3 {
		t.Fatalf("missing: %v", missing)
	}
}

// Copy is idempotent and resumable: a second run copies nothing; a dry run writes nothing.
func TestCopy(t *testing.T) {
	ctx := context.Background()
	src := blob.NewDisk(t.TempDir())
	for k, v := range map[string]string{"ab/one.jpg": "1111", "cd/two.mp4": "22", "thumbs/ab/one_160.jpg": "t"} {
		if err := src.Put(ctx, k, strings.NewReader(v), int64(len(v)), ""); err != nil {
			t.Fatal(err)
		}
	}
	fake := blobtest.NewS3(t, "media")
	dst := s3Store(t, fake, "")
	rep, err := blob.Copy(ctx, src, dst, true, nil)
	if err != nil || rep.Copied != 3 || len(fake.Keys("media")) != 0 {
		t.Fatalf("dry run: %+v %v %v", rep, err, fake.Keys("media"))
	}
	// One file is there already (a run that stopped half way).
	_ = dst.Put(ctx, "ab/one.jpg", strings.NewReader("1111"), 4, "")
	rep, err = blob.Copy(ctx, src, dst, false, nil)
	if err != nil || rep.Copied != 2 || rep.Skipped != 1 || rep.Bytes != 3 {
		t.Fatalf("copy: %+v %v", rep, err)
	}
	if string(fake.Get("media", "cd/two.mp4")) != "22" {
		t.Fatal("content not copied")
	}
	rep, err = blob.Copy(ctx, src, dst, false, nil)
	if err != nil || rep.Copied != 0 || rep.Skipped != 3 {
		t.Fatalf("second run: %+v %v", rep, err)
	}
}

// An opt-in check against a real S3-compatible server (the dev stack's MaxIOFS):
// STAQLY_TEST_S3=http://localhost:9000 S3_BUCKET=... S3_ACCESS_KEY_ID=... S3_SECRET_ACCESS_KEY=...
func TestRealS3(t *testing.T) {
	endpoint := os.Getenv("STAQLY_TEST_S3")
	if endpoint == "" {
		t.Skip("set STAQLY_TEST_S3 to run against a real S3-compatible server")
	}
	ctx := context.Background()
	st, err := blob.NewS3(blob.S3Config{Endpoint: endpoint, Region: os.Getenv("S3_REGION"), Bucket: os.Getenv("S3_BUCKET"),
		AccessKey: os.Getenv("S3_ACCESS_KEY_ID"), SecretKey: os.Getenv("S3_SECRET_ACCESS_KEY"), PathStyle: true,
		Prefix: "staqly-test/", TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Check(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(ctx, "probe/x.txt", strings.NewReader("probe"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	f, err := st.Open(ctx, "probe/x.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "probe" {
		t.Fatalf("read back %q", b)
	}
	if err := st.Delete(ctx, "probe/x.txt"); err != nil {
		t.Fatal(err)
	}
}
