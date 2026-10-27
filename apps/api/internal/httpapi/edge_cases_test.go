package httpapi_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func TestTagFilterWithSpecialCharacters(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Tags")
	for _, tag := range []string{"R&D", "<3", `a\b`, "plain"} {
		e.post(c.ID, map[string]any{"tags": []string{tag}})
		var page app.PostPage
		e.do(nil, "GET", "/v1/posts?tag="+url.QueryEscape(tag), e.key, nil, 200, &page)
		if len(page.Data) != 1 {
			t.Errorf("tag %q: filter found %d posts, want 1", tag, len(page.Data))
		}
	}
	var tags struct{ Data []app.Tag }
	e.do(nil, "GET", "/v1/tags", e.key, nil, 200, &tags)
	t.Logf("tags listed: %+v", tags.Data)
}

func TestKeepMediaLeavesOneCopyInAPost(t *testing.T) {
	e := newEnv(t)
	e.paid()
	deleter := e.keyWith(app.ScopePostsDelete, app.ScopePostsWrite, app.ScopeMediaRead)
	c := e.collection("Keep")
	var a, b app.Media
	png := realPNG(t, 10, 10)
	e.upload("/v1/media", e.key, nil, "a.png", png, 201, &a)
	e.upload("/v1/media", e.key, nil, "b.png", png, 201, &b)
	post := e.post(c.ID, map[string]any{"media": []string{a.ID, b.ID}})
	e.do(nil, "POST", "/v1/media/"+a.ID+"/keep", deleter, nil, 200, nil)
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &after)
	t.Logf("media after keep: %v (version %d -> %d)", after.Media, post.Version, after.Version)
	if len(after.Media) == 2 && after.Media[0] == after.Media[1] {
		t.Errorf("post carries the same file twice: %v", after.Media)
	}
	st, _, body := e.raw(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"media": after.Media})
	t.Logf("PATCH same media back: %d %s", st, body)
}

func TestPermanentDeleteKeepsScheduledPostsFiles(t *testing.T) {
	e := newEnv(t)
	e.paid()
	deleter := e.keyWith(app.ScopePostsDelete, app.ScopePostsWrite, app.ScopeMediaRead)
	c := e.collection("Perm")
	ch := e.customChannel("Own")
	var a app.Media
	e.upload("/v1/media", e.key, nil, "a.png", realPNG(t, 10, 10), 201, &a)
	post := e.post(c.ID, map[string]any{"media": []string{a.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	e.do(nil, "DELETE", "/v1/media/"+a.ID, deleter, nil, 409, nil)
	st, _, body := e.raw(nil, "DELETE", "/v1/media/"+a.ID+"?permanent=true", deleter, nil)
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &after)
	t.Logf("permanent delete: %d %s; post status %s media %v version %d->%d", st, body, after.Status, after.Media, post.Version, after.Version)
	if st == 204 && after.Status == "scheduled" {
		t.Errorf("a scheduled post silently lost its file to a permanent delete")
	}
}

func TestMCPClearsPublishAtWithEmptyString(t *testing.T) {
	e := newEnv(t)
	e.paid()
	c := e.collection("MCPNull")
	ch := e.customChannel("Own")
	post := e.post(c.ID, map[string]any{"targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	// null is what some clients send for every argument they leave out: it changes nothing. "" clears.
	if out := mcpResult(e, e.key, "update_post", map[string]any{"post_id": post.ID, "publish_at": nil}); out["status"] != "scheduled" {
		t.Errorf("an unset (null) publish_at unscheduled the post: %v", out["status"])
	}
	if out := mcpResult(e, e.key, "update_post", map[string]any{"post_id": post.ID, "publish_at": ""}); out["status"] != "draft" {
		t.Errorf("publish_at \"\" over MCP: %v, want draft", out["status"])
	}
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	// REST twin
	var p app.Post
	e.do(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"publish_at": nil}, 200, &p)
	t.Logf("REST PATCH publish_at=null -> status %s publish_at %v", p.Status, p.PublishAt)
}

func TestIdempotencyDoesNotReplay429(t *testing.T) {
	e := newEnv(t)
	e.paid()
	ch := e.customChannel("Own")
	path := "/v1/channels/" + ch.ID + "/test"
	for i := 0; i < 5; i++ {
		st, _, b := e.raw(nil, "POST", path, e.admin, nil)
		if st != 200 {
			t.Fatalf("warmup %d: %d %s", i, st, b)
		}
	}
	st, h, b := e.raw(nil, "POST", path, e.admin, nil, "Idempotency-Key", "K")
	t.Logf("limited: %d retry-after=%q %s", st, h.Get("Retry-After"), b)
	time.Sleep(7 * time.Second) // one token back (10/min)
	st, h, b = e.raw(nil, "POST", path, e.admin, nil, "Idempotency-Key", "K")
	t.Logf("retry with same key after Retry-After: %d replayed=%q retry-after=%q %s", st, h.Get("Idempotent-Replayed"), h.Get("Retry-After"), b)
	if st == 429 {
		t.Errorf("a 429 (not processed) was stored and replayed for the same Idempotency-Key")
	}
}

func TestUploadCutsLongNamesOnACharacter(t *testing.T) {
	e := newEnv(t)
	name := strings.Repeat("a", 199) + "é.png"
	var m app.Media
	e.upload("/v1/media", e.key, nil, name, realPNG(t, 4, 4), 201, &m)
	if !utf8.ValidString(m.OriginalName) || !strings.HasSuffix(m.OriginalName, ".png") || len(m.OriginalName) > 200 {
		t.Fatalf("a long name is cut on a character and keeps its extension: %q", m.OriginalName)
	}
}

func TestCollectionNameExactMatchFirst(t *testing.T) {
	e := newEnv(t)
	e.paid()
	e.collection("Żółw")
	// SQLite's NOCASE folds ASCII only, so both names can exist there; PostgreSQL's lower() folds Unicode and
	// refuses the second one (nothing to tell apart then).
	var lower app.Collection
	if st, _, _ := e.raw(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "żółw"}); st == 201 {
		e.do(nil, "GET", "/v1/collections/"+url.PathEscape("żółw"), e.key, nil, 200, &lower)
		if p := e.post("żółw", nil); p.CollectionID != lower.ID {
			t.Errorf("post addressed to collection %q landed in %q", "żółw", "Żółw")
		}
	}
	st, _, b := e.raw(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "col_news"})
	t.Logf("create col_news: %d %s", st, b)
	if st == 201 {
		if st, _, b = e.raw(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": "col_news", "content": "x"}); st != 201 {
			t.Errorf("post to the collection named col_news: %d %s", st, b)
		}
	}
}

func TestMediaTagFilterWithAmpersand(t *testing.T) {
	e := newEnv(t)
	e.paid()
	var m app.Media
	e.upload("/v1/media", e.key, nil, "a.png", realPNG(t, 4, 4), 201, &m)
	e.do(nil, "POST", "/v1/media/bulk", e.key, map[string]any{"ids": []string{m.ID}, "add_tags": []string{"r&d"}}, 200, nil)
	var page app.MediaPage
	e.do(nil, "GET", "/v1/media?tag="+url.QueryEscape("r&d"), e.key, nil, 200, &page)
	if len(page.Data) != 1 {
		t.Errorf("media tag r&d: %d files, want 1", len(page.Data))
	}
}

func TestBulkDeleteWithARepeatedID(t *testing.T) {
	e := newEnv(t)
	e.paid()
	deleter := e.keyWith(app.ScopePostsDelete, app.ScopePostsWrite, app.ScopePostsRead)
	c := e.collection("Bulk")
	p := e.post(c.ID, nil)
	var res struct{ Deleted, Missing []string }
	e.do(nil, "POST", "/v1/posts/delete", deleter, map[string]any{"ids": []string{p.ID, p.ID}}, 200, &res)
	t.Logf("posts bulk delete with dup ids: %+v", res)
	if len(res.Deleted) == 1 && len(res.Missing) == 1 {
		t.Errorf("same id reported both deleted and missing: %+v", res)
	}
}

// rawSend sends body as it is (not JSON-encoded) with the given Content-Type.
func (e *env) rawSend(method, path, auth, ctype, body string, hdr ...string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("Authorization", "Bearer "+auth)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// What a client may send that is not a clean JSON object: each answer is a 4xx with the JSON error envelope, and
// nothing is created.
func TestJSONBodyEdgeCases(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name, ctype, body string
		want              int
	}{
		{"two values", "application/json", `{"name":"a"} {"name":"b"}`, 400},
		{"trailing garbage", "application/json", `{"name":"a"}x`, 400},
		{"unknown field", "application/json", `{"name":"a","nmae":"b"}`, 400},
		{"empty", "application/json", ``, 400},
		{"cut short", "application/json", `{"name":`, 400},
		{"not an object", "application/json", `["a"]`, 400},
		{"wrong type", "application/json", `{"name":7}`, 400},
		{"over 2 MiB", "application/json", `{"name":"` + strings.Repeat("a", 2<<20) + `"}`, 413},
		{"not JSON", "text/plain", `name=a`, 415},
		{"blank name", "application/json", `{"name":"   "}`, 422},
		{"null name", "application/json", `{"name":null}`, 422},
	} {
		st, body := e.rawSend("POST", "/v1/collections", e.key, c.ctype, c.body)
		if st != c.want || !strings.Contains(body, `"error"`) {
			t.Errorf("%s: %d %.200s, want %d", c.name, st, body, c.want)
		}
	}
	var page struct{ Data []app.Collection }
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &page)
	if len(page.Data) != 1 || !page.Data[0].IsDefault {
		t.Fatalf("a refused body created %+v", page.Data)
	}

	// Text outside ASCII comes back exactly as sent: accents, emoji (4-byte UTF-8), right-to-left, combining marks.
	name := "Zażółć 🚀 שלום é"
	var col app.Collection
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": name}, 201, &col)
	post := e.post(col.ID, map[string]any{"content": name + "\n\n" + name, "tags": []string{"żółw"}})
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &got)
	if col.Name != name || got.Content != name+"\n\n"+name || len(got.Tags) != 1 || got.Tags[0] != "żółw" {
		t.Fatalf("round trip: %q %q %v", col.Name, got.Content, got.Tags)
	}
}

// Cursors and limits from clients: a cursor that is not one of ours is a 422, limits out of range are clamped,
// and walking every page returns each post exactly once.
func TestPaginationEdgeCases(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Pages")
	want := map[string]bool{}
	for range 5 {
		want[e.post(c.ID, map[string]any{"content": "x"}).ID] = true
	}
	for _, path := range []string{
		"/v1/posts?cursor=garbage",
		"/v1/posts?cursor=" + url.QueryEscape("%%%"),
		"/v1/webhook-deliveries?cursor=AA",
		"/v1/webhook-deliveries?cursor=" + url.QueryEscape("not base64!"),
		"/v1/events?cursor=abc",
	} {
		st, _, body := e.raw(nil, "GET", path, e.admin, nil)
		if st != 422 {
			t.Errorf("%s: %d %s, want 422", path, st, body)
		}
	}
	for _, lim := range []string{"-1", "0", "100000", "abc"} {
		var page app.PostPage
		e.do(nil, "GET", "/v1/posts?limit="+lim, e.key, nil, 200, &page)
		if len(page.Data) != 5 {
			t.Errorf("limit=%s: %d posts", lim, len(page.Data))
		}
	}
	seen := map[string]int{}
	cursor := ""
	for i := 0; i < 10; i++ {
		var page app.PostPage
		e.do(nil, "GET", "/v1/posts?limit=2&cursor="+url.QueryEscape(cursor), e.key, nil, 200, &page)
		for _, p := range page.Data {
			seen[p.ID]++
		}
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("pages returned %d posts, want %d: %v", len(seen), len(want), seen)
	}
	for id, n := range seen {
		if !want[id] || n != 1 {
			t.Fatalf("post %s seen %d times", id, n)
		}
	}
}

// An Idempotency-Key belongs to one request: the same key on another path (or with another body) is refused, not
// replayed, and an overlong key is refused before anything runs.
func TestIdempotencyKeyEdgeCases(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Idem")
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "once"}, 201, nil, "Idempotency-Key", "k-path")
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "Other"}, 422, nil, "Idempotency-Key", "k-path")
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "Long"}, 422, nil, "Idempotency-Key", strings.Repeat("k", 201))
	var page struct{ Data []app.Collection }
	e.do(nil, "GET", "/v1/collections", e.key, nil, 200, &page)
	if len(page.Data) != 2 { // Default and Idem
		t.Fatalf("collections %+v", page.Data)
	}
}
