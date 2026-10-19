package httpapi_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/blob/blobtest"
)

// withS3 points the env's media storage at an in-memory S3 bucket.
func withS3(t *testing.T, e *env) *blobtest.S3 {
	t.Helper()
	fake := blobtest.NewS3(t, "media")
	st, err := blob.NewS3(blob.S3Config{Endpoint: fake.URL, Region: "us-east-1", Bucket: "media", AccessKey: "k",
		SecretKey: "s", PathStyle: true, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	e.svc.Cfg.Blob = st
	return fake
}

func widePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 800, 20))
	for x := 0; x < 800; x++ {
		img.Set(x, 0, color.RGBA{R: uint8(x), A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// With STORAGE_DRIVER=s3 every media path goes through the bucket: upload, serving (with ranges),
// thumbnails, the export ZIP, deleting a file and deleting the workspace. Nothing lands in UPLOAD_DIR.
func TestMediaOnS3(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	fake := withS3(t, e)
	csrf := []string{"X-CSRF-Token", e.csrf}
	pic := widePNG(t)

	var m app.Media
	e.upload("/v1/media", e.key, nil, "wide.png", pic, 201, &m)
	if m.Width != 800 || m.Height != 20 {
		t.Fatalf("size read from the staged upload: %+v", m)
	}
	keys := fake.Keys("media")
	if len(keys) != 1 || !strings.HasSuffix(keys[0], ".png") || !bytes.Equal(fake.Get("media", keys[0]), pic) {
		t.Fatalf("bucket after upload: %v", keys)
	}

	// Served from the bucket, with Range support for video seeking.
	st, _, b := e.raw(nil, "GET", "/v1/media/"+m.ID, e.key, nil)
	if st != 200 || !bytes.Equal(b, pic) {
		t.Fatalf("download %d (%d bytes)", st, len(b))
	}
	st, hdr, b := e.raw(nil, "GET", "/v1/media/"+m.ID, e.key, nil, "Range", "bytes=0-7")
	if st != 206 || len(b) != 8 || hdr.Get("Content-Range") == "" {
		t.Fatalf("range %d %v %d", st, hdr, len(b))
	}

	// A thumbnail is made from the bucket's file and cached in the bucket.
	st, hdr, _ = e.raw(nil, "GET", "/v1/media/"+m.ID+"?w=320", e.key, nil)
	if st != 200 || hdr.Get("Content-Type") != "image/png" && hdr.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb %d %v", st, hdr)
	}
	var thumbs int
	for _, k := range fake.Keys("media") {
		if strings.HasPrefix(k, "thumbs/") {
			thumbs++
		}
	}
	if thumbs != 1 {
		t.Fatalf("thumbnail not cached in the bucket: %v", fake.Keys("media"))
	}

	// The export ZIP carries the file read from the bucket.
	c := e.collection("S3")
	e.post(c.ID, map[string]any{"content": "with a picture", "media": []string{m.ID}})
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, csrf...)
	e.svc.ProcessExports(context.Background())
	e.do(e.browser, "GET", "/v1/workspace/exports/"+job.ID, "", nil, 200, &job)
	_, _, zb := e.raw(e.browser, "GET", job.DownloadURL, "", nil)
	if f := readZip(t, zb)["media/wide.png"]; !bytes.Equal(f, pic) {
		t.Fatalf("export media/wide.png: %d bytes", len(f))
	}

	// Deleting the media removes the file and its thumbnail from the bucket.
	e.do(nil, "DELETE", "/v1/media/"+m.ID+"?permanent=true", e.keyWith(app.ScopePostsDelete), nil, 204, nil)
	if keys := fake.Keys("media"); len(keys) != 0 {
		t.Fatalf("bucket after delete: %v", keys)
	}

	// Deleting the workspace removes its remaining files.
	var m2 app.Media
	e.upload("/v1/media", e.key, nil, "again.png", pic, 201, &m2)
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, csrf...)
	if keys := fake.Keys("media"); len(keys) != 0 {
		t.Fatalf("bucket after workspace deletion: %v", keys)
	}
}

// Another workspace can't reach a file by guessing: the media row is looked up in the caller's workspace first.
func TestMediaOnS3Isolation(t *testing.T) {
	e := newEnv(t)
	withS3(t, e)
	var m app.Media
	e.upload("/v1/media", e.key, nil, "mine.png", widePNG(t), 201, &m)
	other := newWorkspace(e, "Other")
	other.do(nil, "GET", "/v1/media/"+m.ID, other.key, nil, 404, nil)
}
