package httpapi_test

import (
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Routing, scope, body decoding and idempotency of POST /v1/posts/{id}/unpublish. The 202 path
// (targets going to "removing") is covered by the service tests and the seam run.
func TestUnpublishRoute(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Unpublish")
	post := e.post(c.ID, map[string]any{"title": "Draft", "content": "x"})
	path := "/v1/posts/" + post.ID + "/unpublish"

	// A draft never went out: 409 not_published.
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	e.do(nil, "POST", path, e.key, map[string]any{"channel_ids": []string{"chn_x"}}, 409, &errBody)
	if errBody.Error.Code != "not_published" {
		t.Fatalf("error body: %+v", errBody)
	}

	// Same key, same body replays the stored answer.
	code, _, _ := rawPost(e, path, "unp-1", `{}`)
	code2, h2, _ := rawPost(e, path, "unp-1", `{ }`)
	if code != 409 || code2 != 409 || h2.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("idempotency: %d %d %v", code, code2, h2)
	}

	// Scope: posts:publish, not posts:write.
	e.do(nil, "POST", path, e.keyWith(app.ScopePostsWrite), nil, 403, nil)
	e.do(nil, "POST", "/v1/posts/pst_missing/unpublish", e.key, nil, 404, nil)

	// channel_ids is a list of strings; unknown fields are refused.
	e.do(nil, "POST", path, e.key, map[string]any{"channel_ids": "chn_x"}, 400, nil)
	e.do(nil, "POST", path, e.key, map[string]any{"bogus": 1}, 400, nil)
	// An empty list is not "all channels".
	e.do(nil, "POST", path, e.key, map[string]any{"channel_ids": []string{}}, 422, nil)
}
