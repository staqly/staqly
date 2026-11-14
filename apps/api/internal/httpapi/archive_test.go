package httpapi_test

import (
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// POST /v1/posts/{id}/archive and /unarchive, and include_archived on the list.
func TestArchiveRoutes(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Archive")
	post := e.post(c.ID, map[string]any{"title": "Old", "content": "x"})
	count := func(query string) int {
		var page struct{ Data []app.Post }
		e.do(nil, "GET", "/v1/posts?collection_id="+c.ID+query, e.key, nil, 200, &page)
		return len(page.Data)
	}

	var got app.Post
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/archive", e.key, nil, 200, &got)
	if got.ArchivedAt == nil {
		t.Fatalf("archived_at %v", got.ArchivedAt)
	}
	if count("") != 0 || count("&include_archived=true") != 1 {
		t.Fatal("the list leaves an archived post out unless include_archived")
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/unarchive", e.key, nil, 200, &got)
	if got.ArchivedAt != nil || count("") != 1 {
		t.Fatalf("unarchived %v", got.ArchivedAt)
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/archive", e.keyWith(app.ScopePostsRead), nil, 403, nil)
	e.do(nil, "POST", "/v1/posts/pst_missing/unarchive", e.key, nil, 404, nil)
}
