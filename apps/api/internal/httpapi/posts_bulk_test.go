package httpapi_test

import (
	"context"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
)

func TestDeletePosts(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Bulk")
	a, b, keep := e.post(c.ID, nil), e.post(c.ID, nil), e.post(c.ID, nil)

	// A list row says who created the post and how many comment threads are open.
	var page struct{ Data []app.Post }
	e.do(nil, "GET", "/v1/posts?collection_id="+c.ID, e.key, nil, 200, &page)
	if len(page.Data) != 3 || page.Data[0].CreatedBy == "" || page.Data[0].OpenComments != 0 {
		t.Fatalf("list extras: %+v", page.Data)
	}

	// Deleting needs posts:delete; posts:write is not enough.
	e.do(nil, "POST", "/v1/posts/delete", e.keyWith("posts:read", "posts:write"), map[string]any{"ids": []string{a.ID}}, 403, nil)
	deleter := e.keyWith("posts:read", "posts:delete")
	e.do(nil, "POST", "/v1/posts/delete", deleter, map[string]any{"ids": []string{}}, 422, nil)

	// A post of another workspace is reported as missing, never touched.
	ctx := context.Background()
	if err := db.New(e.svc.DB).CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_bulk_other", Name: "Other",
		RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := app.Principal{WorkspaceID: "ws_bulk_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	oc, err := e.svc.CreateCollection(ctx, other, app.CollectionInput{Name: ptr("Theirs")})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := e.svc.CreatePost(ctx, other, app.PostInput{Collection: oc.ID, Content: "theirs", Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}

	var res struct{ Deleted, Missing []string }
	e.do(nil, "POST", "/v1/posts/delete", deleter, map[string]any{"ids": []string{a.ID, b.ID, theirs.ID, "pst_nope"}}, 200, &res)
	if len(res.Deleted) != 2 || len(res.Missing) != 2 {
		t.Fatalf("bulk delete: %+v", res)
	}
	e.do(nil, "GET", "/v1/posts/"+a.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/posts/"+keep.ID, e.key, nil, 200, nil)
	if _, err := e.svc.GetPost(ctx, other, theirs.ID); err != nil {
		t.Fatalf("other workspace's post: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
