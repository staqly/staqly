package httpapi_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// sizedPNG is a file of exactly n bytes that reads as a PNG, different for every tag.
func sizedPNG(n int, tag string) []byte {
	b := append(append([]byte{}, pngBytes...), make([]byte, n-len(pngBytes))...)
	copy(b[len(pngBytes):], tag)
	return b
}

func storedMediaFiles(t *testing.T, e *env) int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(e.svc.Cfg.UploadDir, "workspaces", "*", "media", "*"))
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func storageUsed(e *env) int64 {
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	return u.Usage[app.LimitStorage]
}

// Deleting several files for good takes their rows, their stored files and their share of the storage, needs
// posts:delete, and reports ids it does not know, another workspace's among them, without touching those.
func TestBulkPermanentDeleteTakesRowsFilesAndStorage(t *testing.T) {
	e := newEnv(t)
	var a, b, c, theirs app.Media
	e.upload("/v1/media", e.key, nil, "a.png", sizedPNG(500, "a"), 201, &a)
	e.upload("/v1/media", e.key, nil, "b.png", sizedPNG(600, "b"), 201, &b)
	e.upload("/v1/media", e.key, nil, "c.png", sizedPNG(700, "c"), 201, &c)
	other := newWorkspace(e, "Other")
	other.upload("/v1/media", other.key, nil, "theirs.png", sizedPNG(800, "t"), 201, &theirs)
	if used, files := storageUsed(e), storedMediaFiles(t, e); used != 1800 || files != 4 {
		t.Fatalf("setup: %d bytes used and %d files stored, want 1800 and 4", used, files)
	}
	body := map[string]any{"ids": []string{a.ID, b.ID, theirs.ID, "med_nope"}, "permanent": true}

	e.do(nil, "POST", "/v1/media/delete", e.key, body, 403, nil) // media:write is not posts:delete
	if files := storedMediaFiles(t, e); files != 4 {
		t.Fatalf("a refused delete left %d files, want 4", files)
	}
	var res struct{ Deleted, Missing []string }
	e.do(nil, "POST", "/v1/media/delete", e.keyWith("media:read", "posts:delete"), body, 200, &res)
	slices.Sort(res.Deleted)
	slices.Sort(res.Missing)
	wantDeleted, wantMissing := []string{a.ID, b.ID}, []string{theirs.ID, "med_nope"}
	slices.Sort(wantDeleted)
	slices.Sort(wantMissing)
	if !slices.Equal(res.Deleted, wantDeleted) || !slices.Equal(res.Missing, wantMissing) {
		t.Fatalf("deleted %v missing %v, want %v and %v", res.Deleted, res.Missing, wantDeleted, wantMissing)
	}
	e.do(nil, "GET", "/v1/media/"+a.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/media/"+b.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/media/"+c.ID, e.key, nil, 200, nil)
	other.do(nil, "GET", "/v1/media/"+theirs.ID, other.key, nil, 200, nil)
	var trash app.MediaPage
	e.do(nil, "GET", "/v1/media?deleted=true", e.key, nil, 200, &trash)
	if len(trash.Data) != 0 {
		t.Fatalf("deleted for good, yet %d files wait in Deleted", len(trash.Data))
	}
	if used, files := storageUsed(e), storedMediaFiles(t, e); used != 700 || files != 2 {
		t.Fatalf("after the delete %d bytes are used and %d files stored, want 700 (c.png) and 2 (c.png and the other workspace's)", used, files)
	}
	// The same request again: nothing is left to delete, nothing breaks.
	e.do(nil, "POST", "/v1/media/delete", e.keyWith("media:read", "posts:delete"), body, 200, &res)
	if len(res.Deleted) != 0 || len(res.Missing) != 4 {
		t.Fatalf("the second time: deleted %v missing %v, want none and all four", res.Deleted, res.Missing)
	}
}

// Uploads and permanent deletes at the same time: whatever order they land in, the storage counted is the
// size of the files that are there, the stored files are exactly the library's, the limit is never passed, and
// room that was freed can be used.
func TestStorageAccountingUnderConcurrentUploadsAndDeletes(t *testing.T) {
	e := newEnv(t)
	deleter := e.keyWith("media:read", "posts:delete") // before the plan: keyWith moves the workspace to a paid one
	e.tinyStorage()                                    // room for three 1000-byte files
	var first [3]app.Media
	for i := range first {
		e.upload("/v1/media", e.key, nil, fmt.Sprintf("first%d.png", i), sizedPNG(1000, fmt.Sprint("first", i)), 201, &first[i])
	}
	e.upload("/v1/media", e.key, nil, "full.png", sizedPNG(1000, "full"), 402, nil)

	var wg sync.WaitGroup
	var stored atomic.Int32
	for i := range 8 {
		wg.Go(func() {
			body, ct := multipartBody(t, nil, fmt.Sprintf("new%d.png", i), sizedPNG(1000, fmt.Sprint("new", i)))
			req, _ := http.NewRequest("POST", e.srv.URL+"/v1/media", body)
			req.Header.Set("Content-Type", ct)
			req.Header.Set("Authorization", "Bearer "+e.key)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			switch resp.StatusCode {
			case 201:
				stored.Add(1)
			case 402:
			default:
				t.Errorf("upload: %d", resp.StatusCode)
			}
		})
	}
	for _, m := range first {
		wg.Go(func() {
			req, _ := http.NewRequest("DELETE", e.srv.URL+"/v1/media/"+m.ID+"?permanent=true", nil)
			req.Header.Set("Authorization", "Bearer "+deleter)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 204 {
				t.Errorf("delete: %d", resp.StatusCode)
			}
		})
	}
	wg.Wait()

	var page app.MediaPage
	e.do(nil, "GET", "/v1/media", e.key, nil, 200, &page)
	n := len(page.Data)
	if n != int(stored.Load()) || n > 3 {
		t.Fatalf("the library has %d files after %d accepted uploads (all three old ones deleted); the plan has room for 3", n, stored.Load())
	}
	if used := storageUsed(e); used != int64(n)*1000 {
		t.Fatalf("%d bytes counted for %d files of 1000 bytes", used, n)
	}
	if files := storedMediaFiles(t, e); files != n {
		t.Fatalf("%d files stored for a library of %d: a refused upload or a deleted file left one behind, or one is missing", files, n)
	}
	// The room the deletes freed is real: uploads fit until the plan is full again, then the next is refused.
	for i := n; i < 3; i++ {
		e.upload("/v1/media", e.key, nil, fmt.Sprintf("fill%d.png", i), sizedPNG(1000, fmt.Sprint("fill", i)), 201, nil)
	}
	e.upload("/v1/media", e.key, nil, "over.png", sizedPNG(1000, "over"), 402, nil)
	if used, files := storageUsed(e), storedMediaFiles(t, e); used != 3000 || files != 3 {
		t.Fatalf("full again: %d bytes counted and %d files stored, want 3000 and 3", used, files)
	}
}
