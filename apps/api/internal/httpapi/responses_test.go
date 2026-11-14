package httpapi_test

import (
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Routing, scopes and 404 of a post's responses; the 200 shape is checked by the contract test once the service reads real data.
func TestResponsesRoutes(t *testing.T) {
	e := newEnv(t)
	col := e.collection("Resp")
	post := e.post(col.ID, nil)

	e.do(nil, "GET", "/v1/posts/post_nope/responses", e.key, nil, 404, nil)
	e.do(nil, "POST", "/v1/posts/post_nope/responses/refresh", e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/responses", e.keyWith(app.ScopeMediaRead), nil, 403, nil)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/responses/refresh", e.keyWith(app.ScopePostsRead), nil, 403, nil)
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/responses", "", nil, 401, nil)
}
