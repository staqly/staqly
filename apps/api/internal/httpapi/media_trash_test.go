package httpapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Deleted files: delete moves a file to Deleted (restorable, listed with deleted_at and purge_at, out of the
// library and its counts), a file a scheduled post uses is refused, restore brings it back, and the retention
// sweep deletes it for good after 30 days. Duplicates: identical uploads point at the oldest one, list together,
// and keep re-points posts to the kept file and moves the copies to Deleted.
func TestMediaTrashAndDuplicates(t *testing.T) {
	e := newEnv(t)
	e.paid()
	ctx := context.Background()
	deleter := e.keyWith(app.ScopePostsDelete, app.ScopePostsWrite, app.ScopeMediaRead)
	c := e.collection("Trash")
	ch := e.customChannel("Own")

	var a, b, other app.Media
	png := realPNG(t, 10, 10)
	e.upload("/v1/media", e.key, nil, "a.png", png, 201, &a)
	e.upload("/v1/media", e.key, nil, "b.png", png, 201, &b) // same bytes as a
	e.upload("/v1/media", e.key, nil, "other.png", realPNG(t, 12, 12), 201, &other)
	if a.DuplicateOf != "" || b.DuplicateOf != a.ID || other.DuplicateOf != "" {
		t.Fatalf("duplicate_of: a=%q b=%q other=%q", a.DuplicateOf, b.DuplicateOf, other.DuplicateOf)
	}

	list := func(query string) []app.LibraryItem {
		t.Helper()
		var p app.MediaPage
		e.do(nil, "GET", "/v1/media"+query, e.key, nil, 200, &p)
		return p.Data
	}
	if l := list("?duplicates=true"); len(l) != 2 || l[0].ID != a.ID || l[1].ID != b.ID || l[1].DuplicateOf != a.ID {
		t.Fatalf("duplicates: %+v", l)
	}

	// A scheduled post uses b: moving b to Deleted is refused, alone and in bulk.
	post := e.post(c.ID, map[string]any{"media": []string{b.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	e.do(nil, "DELETE", "/v1/media/"+b.ID, deleter, nil, 409, nil)
	e.do(nil, "POST", "/v1/media/delete", deleter, map[string]any{"ids": []string{other.ID, b.ID}}, 409, nil)
	if l := list(""); len(l) != 3 {
		t.Fatalf("a refused bulk delete moves nothing: %+v", l)
	}

	// Keep a: the post now uses a, b goes to Deleted.
	e.do(nil, "POST", "/v1/media/"+a.ID+"/keep", e.key, nil, 403, nil) // posts:delete needed
	var kept struct{ Deleted []string }
	e.do(nil, "POST", "/v1/media/"+a.ID+"/keep", deleter, nil, 200, &kept)
	if len(kept.Deleted) != 1 || kept.Deleted[0] != b.ID {
		t.Fatalf("keep: %+v", kept)
	}
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &after)
	if len(after.Media) != 1 || after.Media[0] != a.ID {
		t.Fatalf("post re-pointed to the kept file: %v", after.Media)
	}
	if l := list("?duplicates=true"); len(l) != 0 {
		t.Fatalf("no duplicates left: %+v", l)
	}

	// Deleted: other goes there too; the library and its counts leave both out.
	e.do(nil, "DELETE", "/v1/media/"+other.ID, deleter, nil, 204, nil)
	trash := list("?deleted=true")
	if len(trash) != 2 || trash[0].DeletedAt == nil || trash[0].PurgeAt == nil {
		t.Fatalf("deleted list: %+v", trash)
	}
	if l := list(""); len(l) != 1 || l[0].ID != a.ID {
		t.Fatalf("library without deleted files: %+v", l)
	}
	var tree app.MediaFolders
	e.do(nil, "GET", "/v1/media-folders", e.key, nil, 200, &tree)
	if tree.Total != 1 || tree.Deleted != 2 || tree.Duplicates != 0 {
		t.Fatalf("counts: %+v", tree)
	}

	// Restore other; another workspace can neither restore nor keep these files.
	var restored app.Media
	e.do(nil, "POST", "/v1/media/"+other.ID+"/restore", deleter, nil, 200, &restored)
	if restored.DeletedAt != nil {
		t.Fatalf("restored: %+v", restored)
	}
	ws := app.Principal{WorkspaceID: "ws_trash_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	if _, err := e.svc.RestoreMedia(ctx, ws, b.ID); err == nil {
		t.Fatal("another workspace restored a file")
	}
	if _, err := e.svc.KeepMedia(ctx, ws, a.ID); err == nil {
		t.Fatal("another workspace kept a file")
	}

	// The sweep deletes b for good once it has been in Deleted for 30 days; other (restored) stays.
	defer func() { e.svc.Now = time.Now }()
	e.svc.Now = func() time.Time { return time.Now().Add(29 * 24 * time.Hour) }
	if rep, err := e.svc.RunRetention(ctx); err != nil || rep.Media != 0 {
		t.Fatalf("too early: %+v %v", rep, err)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	if rep, err := e.svc.RunRetention(ctx); err != nil || rep.Media != 1 {
		t.Fatalf("purge: %+v %v", rep, err)
	}
	e.do(nil, "GET", "/v1/media/"+b.ID+"/info", e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/media/"+other.ID+"/info", e.key, nil, 200, nil)
}
