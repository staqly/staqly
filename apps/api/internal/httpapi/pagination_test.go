package httpapi_test

import (
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Delivery history and a post's comments page with next_cursor: every row once, in order, the last page
// without a cursor; a bad cursor is a 422.
func TestDeliveriesAndCommentsPage(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Pages")
	e.do(nil, "POST", "/v1/webhooks", e.admin, map[string]any{"url": "http://127.0.0.1:9/hook", "events": []string{"post.created"}}, 201, nil)
	post := e.post(c.ID, map[string]any{"content": "first"})
	for i := 0; i < 4; i++ {
		e.post(c.ID, map[string]any{"content": "more"})
	}
	for _, body := range []string{"one", "two", "three", "four", "five"} {
		e.do(nil, "POST", "/v1/posts/"+post.ID+"/comments", e.key, map[string]any{"body": body}, 201, nil)
	}

	type page[T any] struct {
		Data       []T    `json:"data"`
		NextCursor string `json:"next_cursor"`
	}
	var deliveries []app.Delivery
	cursor := ""
	for pages := 0; ; pages++ {
		var p page[app.Delivery]
		e.do(nil, "GET", "/v1/webhook-deliveries?limit=2&cursor="+cursor, e.admin, nil, 200, &p)
		deliveries = append(deliveries, p.Data...)
		if p.NextCursor == "" {
			break
		}
		if pages > 5 {
			t.Fatal("deliveries never end")
		}
		cursor = p.NextCursor
	}
	seen := map[string]bool{}
	for i, d := range deliveries {
		if seen[d.ID] {
			t.Fatalf("delivery %s twice", d.ID)
		}
		seen[d.ID] = true
		if i > 0 && d.CreatedAt > deliveries[i-1].CreatedAt {
			t.Fatalf("deliveries not newest first: %v", deliveries)
		}
	}
	if len(deliveries) != 5 {
		t.Fatalf("deliveries: %d, want 5", len(deliveries))
	}

	var bodies []string
	cursor = ""
	for {
		var p page[app.PostComment]
		e.do(nil, "GET", "/v1/posts/"+post.ID+"/comments?limit=2&cursor="+cursor, e.key, nil, 200, &p)
		for _, c := range p.Data {
			bodies = append(bodies, c.Body)
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if want := "one two three four five"; strings.Join(bodies, " ") != want {
		t.Fatalf("comments in order: %q, want %q", strings.Join(bodies, " "), want)
	}
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/comments?cursor=nonsense", e.key, nil, 422, nil)
	e.do(nil, "GET", "/v1/webhook-deliveries?cursor=nonsense", e.admin, nil, 422, nil)
}
