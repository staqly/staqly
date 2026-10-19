package httpapi_test

import (
	"context"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// Library folders and tags: nested folders with unique names per parent, moves that can't create a cycle,
// deleting a folder moves its files and subfolders up, file tags (normalised) with counts and a filter that
// combines with the folder, bulk move and tag, scopes, and isolation between workspaces.
func TestMediaFolders(t *testing.T) {
	e := newEnv(t)
	e.paid()

	var a, b, c app.Media
	e.upload("/v1/media", e.key, nil, "a.png", realPNG(t, 10, 10), 201, &a)
	e.upload("/v1/media", e.key, nil, "b.png", realPNG(t, 10, 10), 201, &b)
	e.upload("/v1/media", e.key, nil, "c.png", realPNG(t, 10, 10), 201, &c)
	if a.FolderID != "" || len(a.Tags) != 0 {
		t.Fatalf("a new file is at the top level without tags: %+v", a)
	}

	folder := func(name, parent string, status int) app.MediaFolder {
		t.Helper()
		var f app.MediaFolder
		e.do(nil, "POST", "/v1/media-folders", e.key, map[string]any{"name": name, "parent_id": parent}, status, &f)
		return f
	}
	clients := folder("Clients", "", 201)
	acme := folder("  ACME   launch ", clients.ID, 201)
	if acme.Name != "ACME launch" || acme.ParentID != clients.ID {
		t.Fatalf("nested folder: %+v", acme)
	}
	folder("clients", "", 409)      // same name at the same level, any case
	folder("Clients", acme.ID, 201) // the same name elsewhere is fine
	folder("", "", 422)             // a name is required
	folder("a/b", "", 422)          // no slashes
	folder("Lost", "mfd_nope", 422) // unknown parent
	read := e.keyWith("media:read")
	e.do(nil, "POST", "/v1/media-folders", read, map[string]any{"name": "No"}, 403, nil)

	// No cycles: a folder can't move under itself or one of its own folders.
	e.do(nil, "PATCH", "/v1/media-folders/"+clients.ID, e.key, map[string]any{"parent_id": acme.ID}, 422, nil)
	e.do(nil, "PATCH", "/v1/media-folders/"+clients.ID, e.key, map[string]any{"parent_id": clients.ID}, 422, nil)
	var renamed app.MediaFolder
	e.do(nil, "PATCH", "/v1/media-folders/"+acme.ID, e.key, map[string]any{"name": "ACME"}, 200, &renamed)
	if renamed.Name != "ACME" || renamed.ParentID != clients.ID {
		t.Fatalf("rename: %+v", renamed)
	}

	// A file moves into a folder and gets tags, normalised.
	var got app.Media
	e.do(nil, "PATCH", "/v1/media/"+a.ID, e.key, map[string]any{"folder_id": acme.ID, "tags": []string{"#Summer Sale", "summer-sale", " Hero "}}, 200, &got)
	if got.FolderID != acme.ID || len(got.Tags) != 2 || got.Tags[0] != "summer-sale" || got.Tags[1] != "hero" {
		t.Fatalf("move and tag: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/media/"+a.ID, e.key, map[string]any{"folder_id": "mfd_nope"}, 422, nil)

	// Bulk: move b and c into Clients, tag both, untag one.
	var bulk app.MediaBulkResult
	e.do(nil, "POST", "/v1/media/bulk", e.key, map[string]any{"ids": []string{b.ID, c.ID, "med_nope"}, "folder_id": clients.ID,
		"add_tags": []string{"Hero"}}, 200, &bulk)
	if len(bulk.Updated) != 2 || len(bulk.Missing) != 1 {
		t.Fatalf("bulk: %+v", bulk)
	}
	e.do(nil, "POST", "/v1/media/bulk", e.key, map[string]any{"ids": []string{c.ID}, "remove_tags": []string{"hero"}}, 200, nil)
	e.do(nil, "POST", "/v1/media/bulk", e.key, map[string]any{"ids": []string{c.ID}}, 422, nil) // nothing to do
	e.do(nil, "POST", "/v1/media/bulk", read, map[string]any{"ids": []string{c.ID}, "folder_id": ""}, 403, nil)

	list := func(query string) []app.LibraryItem {
		t.Helper()
		var p app.MediaPage
		e.do(nil, "GET", "/v1/media"+query, e.key, nil, 200, &p)
		return p.Data
	}
	if l := list("?folder=" + clients.ID); len(l) != 2 {
		t.Fatalf("files directly in Clients: %+v", l)
	}
	if l := list("?folder=root"); len(l) != 0 {
		t.Fatalf("top level: %+v", l)
	}
	if l := list("?tag=hero"); len(l) != 2 {
		t.Fatalf("tag hero: %+v", l)
	}
	if l := list("?tag=hero&folder=" + clients.ID); len(l) != 1 || l[0].ID != b.ID {
		t.Fatalf("tag and folder: %+v", l)
	}
	if l := list("?tag=%23Hero"); len(l) != 2 {
		t.Fatalf("tag filter is normalised: %+v", l)
	}

	var tree app.MediaFolders
	e.do(nil, "GET", "/v1/media-folders", read, nil, 200, &tree)
	count := map[string]int64{}
	for _, f := range tree.Data {
		count[f.ID] = f.Files
	}
	if len(tree.Data) != 3 || count[clients.ID] != 2 || count[acme.ID] != 1 || tree.RootFile != 0 || tree.Total != 3 {
		t.Fatalf("tree: %+v", tree)
	}
	if len(tree.Tags) != 2 || tree.Tags[0].Tag != "hero" || tree.Tags[0].Files != 2 {
		t.Fatalf("tag counts: %+v", tree.Tags)
	}

	// Deleting Clients moves its files to the top level and ACME up a level; no file is deleted.
	e.do(nil, "DELETE", "/v1/media-folders/"+clients.ID, e.key, nil, 204, nil)
	e.do(nil, "DELETE", "/v1/media-folders/"+clients.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/media-folders", e.key, nil, 200, &tree)
	parent := map[string]string{}
	for _, f := range tree.Data {
		parent[f.ID] = f.ParentID
	}
	if len(tree.Data) != 2 || parent[acme.ID] != "" || tree.RootFile != 2 || tree.Total != 3 {
		t.Fatalf("after delete: %+v", tree)
	}
	if l := list(""); len(l) != 3 {
		t.Fatalf("no file deleted: %+v", l)
	}

	// Another workspace sees none of it and can't use these folders.
	ctx := context.Background()
	if err := db.New(e.svc.DB).CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_folders_other", Name: "Other",
		RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := app.Principal{WorkspaceID: "ws_folders_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	if tr, err := e.svc.ListMediaFolders(ctx, other); err != nil || len(tr.Data) != 0 || tr.Total != 0 || len(tr.Tags) != 0 {
		t.Fatalf("other workspace tree: %+v %v", tr, err)
	}
	if _, err := e.svc.UpdateMediaFolder(ctx, other, acme.ID, app.MediaFolderInput{Name: new(string)}); err == nil {
		t.Fatal("other workspace renamed a folder")
	}
	if err := e.svc.DeleteMediaFolder(ctx, other, acme.ID); err == nil {
		t.Fatal("other workspace deleted a folder")
	}
	if _, err := e.svc.BulkUpdateMedia(ctx, other, app.MediaBulk{IDs: []string{a.ID}, AddTags: []string{"x"}}); err != nil {
		t.Fatal(err)
	} else if p, _ := e.svc.ListMedia(ctx, other, app.MediaFilter{}); len(p.Data) != 0 {
		t.Fatalf("other workspace listed files: %+v", p.Data)
	}
	if _, err := e.svc.CreateMediaFolder(ctx, other, app.MediaFolderInput{Name: ptrTo("X"), ParentID: &acme.ID}); err == nil {
		t.Fatal("other workspace nested a folder under this workspace's folder")
	}
}
