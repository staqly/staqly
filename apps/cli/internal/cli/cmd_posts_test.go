package cli

import "testing"

func TestPostsUnpublish(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/channels", 200, map[string]any{"data": []any{channel("ch_1", "custom", "Pipeline")}})
	f.on("POST /v1/posts/pst_1/unpublish", 202, map[string]any{"id": "pst_1", "status": "published", "targets": []any{
		map[string]any{"channel_id": "ch_1", "channel_name": "Pipeline", "removal": "removing"}}})

	r := f.run("posts", "unpublish", "pst_1", "--channel", "Pipeline").mustCode(t, 0)
	mustContain(t, r.out, "Unpublishing pst_1", "Pipeline: removing")
	if ids, _ := f.last("POST", "/v1/posts/pst_1/unpublish").json(t)["channel_ids"].([]any); len(ids) != 1 || ids[0] != "ch_1" {
		t.Fatalf("channel_ids = %v", ids)
	}
	f.run("posts", "unpublish", "pst_1").mustCode(t, 0)
	if b := f.last("POST", "/v1/posts/pst_1/unpublish").Body; len(b) != 0 {
		t.Fatalf("body sent without --channel: %q", b)
	}
}
