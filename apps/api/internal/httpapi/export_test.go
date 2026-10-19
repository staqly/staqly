package httpapi_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// raw performs a request and returns status, headers and body.
func (e *env) raw(c *http.Client, method, path, auth string, body any, hdr ...string) (int, http.Header, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func ndjsonEvents(t *testing.T, b []byte) []app.Event {
	t.Helper()
	var out []app.Event
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	for sc.Scan() {
		var ev app.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad ndjson line %q: %v", sc.Text(), err)
		}
		out = append(out, ev)
	}
	return out
}

func withDataDir(e *env) string {
	dir := e.t.TempDir()
	e.svc.Getenv = func(k string) string {
		if k == "DATA_DIR" {
			return dir
		}
		return ""
	}
	return dir
}

func TestEventExportFormatsFiltersAndCSVEscaping(t *testing.T) {
	e := newEnv(t)
	c1 := e.collection("One")
	c2 := e.collection("Two")
	it1 := e.post(c1.ID, map[string]any{"content": "a \"quoted\", comma\nnewline"})
	e.post(c2.ID, map[string]any{"content": "b"})
	e.do(nil, "PATCH", "/v1/posts/"+it1.ID, e.key, map[string]any{"content": "a \"quoted\", comma\nnewline v2"}, 200, nil)
	// A key whose name is a spreadsheet formula.
	e.paid()
	var evil app.APIKey
	e.do(e.browser, "POST", "/v1/api-keys", "", map[string]any{"name": "=HYPERLINK(\"http://x\")", "scopes": []string{app.ScopePostsWrite, app.ScopePostsRead, app.ScopeCollectionsRead}}, 201, &evil, "X-CSRF-Token", e.csrf)
	e.post(c2.ID, map[string]any{"content": "by evil"}) // default key
	var byEvil app.Post
	e.do(nil, "POST", "/v1/posts", evil.Key, map[string]any{"collection": c2.ID, "content": "-1+1"}, 201, &byEvil)

	// NDJSON, everything, oldest first; streamed (no Content-Length).
	resp := e.do(nil, "GET", "/v1/events/export", e.key, nil, 200, nil)
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content type %q", ct)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("disposition %q", resp.Header.Get("Content-Disposition"))
	}
	_, _, body := e.raw(nil, "GET", "/v1/events/export?format=ndjson", e.key, nil)
	all := ndjsonEvents(t, body)
	if len(all) < 6 {
		t.Fatalf("want >= 6 events, got %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq <= all[i-1].Seq {
			t.Fatal("events not in ascending seq order")
		}
	}

	count := func(q string) int {
		t.Helper()
		st, _, b := e.raw(nil, "GET", "/v1/events/export?"+q, e.key, nil)
		if st != 200 {
			t.Fatalf("%s: %d %s", q, st, b)
		}
		return len(ndjsonEvents(t, b))
	}
	if n := count("type=post.created"); n != 4 {
		t.Fatalf("type filter: %d", n)
	}
	if n := count("type_prefix=post."); n != 5 {
		t.Fatalf("type_prefix filter: %d", n)
	}
	if n := count("type_prefix=post.&collection_id=" + c1.ID); n != 2 {
		t.Fatalf("collection filter: %d", n)
	}
	if n := count("post_id=" + it1.ID); n != 2 {
		t.Fatalf("post filter: %d", n)
	}
	// The session's events here are api_key.created audit events: admins only, like GET /v1/events.
	if n := count("actor_type=session"); n != 0 {
		t.Fatalf("non-admin export shows audit events: %d", n)
	}
	if st, _, b := e.raw(nil, "GET", "/v1/events/export?actor_type=session", e.admin, nil); st != 200 || len(ndjsonEvents(t, b)) < 1 {
		t.Fatalf("actor_type filter (admin): %d %s", st, b)
	}
	if n := count("actor_id=" + evil.ID); n != 1 {
		t.Fatalf("actor_id filter: %d", n)
	}
	if n := count("from=2000-01-01&to=2001-01-01"); n != 0 {
		t.Fatalf("range filter: %d", n)
	}
	if n := count("from=" + all[2].CreatedAt); n < len(all)-2 {
		t.Fatalf("from filter: %d", n)
	}
	if n := count("type_prefix=%25"); n != 0 { // no LIKE wildcards
		t.Fatalf("type_prefix must be literal: %d", n)
	}

	// CSV: header, RFC 4180 quoting, formula neutralisation, JSON data column.
	st, hdr, b := e.raw(nil, "GET", "/v1/events/export?format=csv", e.key, nil)
	if st != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv: %d %v", st, hdr)
	}
	if !bytes.Contains(b, []byte("\r\n")) {
		t.Fatal("csv should use CRLF")
	}
	recs, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	if strings.Join(recs[0], ",") != "created_at,id,type,actor_type,actor_id,actor_name,collection_id,post_id,data" {
		t.Fatalf("header %v", recs[0])
	}
	if len(recs) != len(all)+1 {
		t.Fatalf("csv rows %d vs %d events", len(recs)-1, len(all))
	}
	var sawEvil, sawQuoted bool
	for _, r := range recs[1:] {
		if r[4] == evil.ID {
			sawEvil = true
			if r[5] != "'=HYPERLINK(\"http://x\")" {
				t.Fatalf("formula not neutralised: %q", r[5])
			}
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(r[8]), &data); err != nil {
			t.Fatalf("data column is not JSON: %q", r[8])
		}
		if r[7] == it1.ID && r[2] == "post.created" {
			content := data["post"].(map[string]any)["content"]
			sawQuoted = content == "a \"quoted\", comma\nnewline"
		}
	}
	if !sawEvil || !sawQuoted {
		t.Fatalf("csv rows missing: evil=%v quoted=%v", sawEvil, sawQuoted)
	}

	// Validation and scopes.
	e.do(nil, "GET", "/v1/events/export?format=xml", e.key, nil, 422, nil)
	e.do(nil, "GET", "/v1/events/export?from=yesterday", e.key, nil, 422, nil)
	e.do(nil, "GET", "/v1/events/export?actor_type=robot", e.key, nil, 422, nil)
	e.do(nil, "GET", "/v1/events/export", e.keyWith(app.ScopeCollectionsRead), nil, 403, nil)
	e.do(nil, "GET", "/v1/events/export", "", nil, 401, nil)
	e.do(e.browser, "GET", "/v1/events/export?format=csv", "", nil, 200, nil)

	// Isolation: another workspace exports only its own events.
	other := newWorkspace(e, "Other")
	_, _, b = e.raw(nil, "GET", "/v1/events/export", other.key, nil)
	for _, ev := range ndjsonEvents(t, b) {
		if ev.Type != "workspace.created" && ev.Type != "api_key.created" {
			t.Fatalf("foreign event leaked: %s", ev.Type)
		}
	}
}

func TestEventExportStreamsManyPages(t *testing.T) {
	e := newEnv(t)
	ws, _ := e.svc.GetWorkspace(context.Background(), app.Principal{WorkspaceID: workspaceID(e), Kind: app.PrincipalSystem})
	q := db.New(e.svc.DB)
	const n = 1234
	for i := range n {
		if _, err := q.InsertEvent(context.Background(), db.InsertEventParams{ID: fmt.Sprintf("evt_bulk%05d", i), WorkspaceID: ws.ID,
			Type: "bulk.test", ActorType: "system", ActorID: "t", ActorName: "t", Data: `{"i":` + fmt.Sprint(i) + `}`,
			CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")}); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/v1/events/export?type=bulk.test", nil)
	req.Header.Set("Authorization", "Bearer "+e.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ContentLength != -1 || len(resp.TransferEncoding) == 0 || resp.TransferEncoding[0] != "chunked" {
		t.Fatalf("response should be streamed (chunked), got length %d te %v", resp.ContentLength, resp.TransferEncoding)
	}
	b, _ := io.ReadAll(resp.Body)
	evs := ndjsonEvents(t, b)
	if len(evs) != n {
		t.Fatalf("want %d events, got %d", n, len(evs))
	}
	for i, ev := range evs {
		if ev.ID != fmt.Sprintf("evt_bulk%05d", i) {
			t.Fatalf("event %d out of order: %s", i, ev.ID)
		}
	}
}

// newWorkspace signs up a second cloud workspace (with its own browser session).
func newWorkspace(e *env, name string) *env {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	o := &env{t: e.t, svc: e.svc, srv: e.srv, browser: &http.Client{Jar: jar}}
	var res struct {
		APIKey app.APIKey `json:"api_key"`
		CSRF   string     `json:"csrf_token"`
	}
	e.do(o.browser, "POST", "/v1/workspaces", "", map[string]string{"name": name, "setup_token": "tok"}, 201, &res)
	o.key, o.csrf = res.APIKey.Key, res.CSRF
	return o
}

func workspaceID(e *env) string {
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	return ws.ID
}

func readZip(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		if slices.Contains(strings.Split(f.Name, "/"), "..") || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "\\") {
			t.Fatalf("unsafe zip path %q", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	return out
}

func TestWorkspaceExportZipLifecycle(t *testing.T) {
	e := newEnv(t)
	dataDir := withDataDir(e)
	csrf := []string{"X-CSRF-Token", e.csrf}
	c := e.collection("Photos")
	img1 := append(append([]byte{}, pngBytes...), []byte("first")...)
	img2 := append(append([]byte{}, pngBytes...), []byte("second")...)
	var m1, m2, dot app.Media
	e.upload("/v1/media", e.key, nil, "photo.png", img1, 201, &m1)
	e.upload("/v1/media", e.key, nil, "photo.png", img2, 201, &m2)
	it1 := e.post(c.ID, map[string]any{"content": "one", "media": []string{m1.ID}})
	it2 := e.post(c.ID, map[string]any{"content": "two", "media": []string{m2.ID}})
	e.upload("/v1/media", e.key, nil, "..png", pngBytes, 201, &dot)
	e.do(nil, "PATCH", "/v1/posts/"+it1.ID, e.key, map[string]any{"content": "one v2"}, 200, nil)
	ch := e.customChannel("Bot")
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/hook", "events": []string{"post.*"}}, 201, &wh)
	if wh.Secret == "" {
		t.Fatal("webhook secret expected")
	}

	// Scopes: admin only.
	e.do(nil, "POST", "/v1/workspace/export", e.key, nil, 403, nil)
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, csrf...)
	if job.Status != "pending" || job.DownloadURL != "" {
		t.Fatalf("job %+v", job)
	}
	// One export at a time per workspace.
	var conflictBody struct{ Error app.Error }
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 409, &conflictBody, csrf...)
	if conflictBody.Error.Details["export_id"] != job.ID {
		t.Fatalf("conflict details %+v", conflictBody.Error)
	}
	adminKey := e.keyWith(app.ScopeAdmin)
	e.do(nil, "POST", "/v1/workspace/export", adminKey, nil, 409, nil)
	e.do(nil, "GET", "/v1/workspace/exports/"+job.ID+"/download", adminKey, nil, 409, nil) // not ready yet

	// Another workspace can export concurrently, but can't see this job.
	other := newWorkspace(e, "Other")
	e.do(other.browser, "GET", "/v1/workspace/exports/"+job.ID, "", nil, 404, nil)
	e.do(other.browser, "POST", "/v1/workspace/export", "", nil, 202, nil, "X-CSRF-Token", other.csrf)

	e.svc.ProcessExports(context.Background())
	e.do(nil, "GET", "/v1/workspace/exports/"+job.ID, adminKey, nil, 200, &job)
	if job.Status != "ready" || job.SizeBytes == 0 || job.DownloadURL == "" || !job.IncludesMedia {
		t.Fatalf("job after build %+v", job)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "exports", job.ID+".zip")); err != nil {
		t.Fatalf("zip not in DATA_DIR/exports: %v", err)
	}
	// The list (the dashboard's Backups card): this workspace's exports only, newest first; admin only.
	var list struct{ Data []app.WorkspaceExport }
	e.do(nil, "GET", "/v1/workspace/exports", adminKey, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != job.ID || list.Data[0].DownloadURL == "" {
		t.Fatalf("list %+v", list.Data)
	}
	e.do(nil, "GET", "/v1/workspace/exports", e.key, nil, 403, nil)
	e.do(nil, "GET", job.DownloadURL, e.key, nil, 403, nil)
	e.do(other.browser, "GET", job.DownloadURL, "", nil, 404, nil)
	st, hdr, b := e.raw(e.browser, "GET", job.DownloadURL, "", nil)
	if st != 200 || hdr.Get("Content-Type") != "application/zip" || !strings.Contains(hdr.Get("Content-Disposition"), ".zip") {
		t.Fatalf("download %d %v", st, hdr)
	}
	files := readZip(t, b)
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, want := range []string{"README.txt", "workspace.json", "collections.ndjson", "posts.ndjson", "events.ndjson",
		"webhooks.json", "channels.json", "schedules.json", "media/photo.png", "media/photo (2).png", "media/_..png"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	// Media bytes round-trip, and posts point at their deduplicated file.
	var posts []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(files["posts.ndjson"]), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatal(err)
		}
		posts = append(posts, m)
	}
	if len(posts) != 2 {
		t.Fatalf("posts %d", len(posts))
	}
	for _, m := range posts {
		f := m["media_files"].([]any)[0].(string)
		want := img1
		if m["id"] == it2.ID {
			want = img2
		}
		if !bytes.Equal(files[f], want) {
			t.Fatalf("media bytes mismatch for %s (%s)", m["id"], f)
		}
		if m["id"] == it1.ID && m["content"] != "one v2" {
			t.Fatalf("edited post: %v", m)
		}
	}
	var chans []app.Channel
	json.Unmarshal(files["channels.json"], &chans)
	if len(chans) != 1 || chans[0].ID != ch.ID || bytes.Contains(files["channels.json"], []byte("credentials")) {
		t.Fatalf("channels.json %s", files["channels.json"])
	}
	// Secrets are redacted everywhere.
	for n, data := range files {
		if bytes.Contains(data, []byte(wh.Secret)) {
			t.Fatalf("webhook secret leaked in %s", n)
		}
	}
	var hooks []app.Webhook
	json.Unmarshal(files["webhooks.json"], &hooks)
	if len(hooks) != 1 || hooks[0].Secret != "[redacted]" || hooks[0].URL != "https://example.com/hook" {
		t.Fatalf("webhooks.json %+v", hooks)
	}
	var wsj struct {
		Workspace     app.Workspace
		Counts        map[string]int
		IncludesMedia bool `json:"includes_media"`
	}
	json.Unmarshal(files["workspace.json"], &wsj)
	if wsj.Workspace.Name != "Test" || wsj.Counts["posts"] != 2 || wsj.Counts["media"] != 3 || !wsj.IncludesMedia {
		t.Fatalf("workspace.json %s", files["workspace.json"])
	}
	if !bytes.Contains(files["events.ndjson"], []byte(`"post.created"`)) || bytes.Contains(files["events.ndjson"], []byte("Other")) {
		t.Fatal("events.ndjson content")
	}
	_, _, evb := e.raw(nil, "GET", "/v1/events/export?type=workspace.exported", e.key, nil)
	if len(ndjsonEvents(t, evb)) != 1 {
		t.Fatal("workspace.exported event expected")
	}

	// A new export may start once the previous finished.
	var job2 app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job2, csrf...)
	e.svc.ProcessExports(context.Background())

	// Expiry after 24h: file removed, status expired, download 410.
	e.svc.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	defer func() { e.svc.Now = time.Now }()
	e.do(e.browser, "GET", job.DownloadURL, "", nil, 410, nil)
	// Before the worker runs, GET already reports it expired, without a download link (like the list).
	var stale app.WorkspaceExport
	e.do(e.browser, "GET", "/v1/workspace/exports/"+job.ID, "", nil, 200, &stale)
	if stale.Status != "expired" || stale.DownloadURL != "" {
		t.Fatalf("expired export offered: %+v", stale)
	}
	e.svc.ProcessExports(context.Background())
	if _, err := os.Stat(filepath.Join(dataDir, "exports", job.ID+".zip")); !os.IsNotExist(err) {
		t.Fatalf("expired zip should be deleted: %v", err)
	}
	e.do(e.browser, "GET", "/v1/workspace/exports/"+job.ID, "", nil, 200, &job)
	if job.Status != "expired" {
		t.Fatalf("status %s", job.Status)
	}
	e.do(e.browser, "GET", "/v1/workspace/exports/"+job.ID+"/download", "", nil, 410, nil)
}

// Free: the workspace export is data only. The same JSON files (collections, posts, events with the
// history and the audit events, media.ndjson), but no media/ files; admin only.
func TestWorkspaceExportFreeIsDataOnly(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	csrf := []string{"X-CSRF-Token", e.csrf}
	c := e.collection("Photos")
	var m app.Media
	e.upload("/v1/media", e.key, nil, "photo.png", pngBytes, 201, &m)
	it := e.post(c.ID, map[string]any{"content": "one", "media": []string{m.ID}, "tags": []string{"red"}, "metadata": map[string]any{"k": "v"}})
	e.do(nil, "PATCH", "/v1/posts/"+it.ID, e.key, map[string]any{"content": "one v2"}, 200, nil)
	e.free()

	// Admin only, on Free too.
	e.do(nil, "POST", "/v1/workspace/export", e.key, nil, 403, nil)
	e.do(nil, "GET", "/v1/workspace/exports", e.key, nil, 403, nil)
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, csrf...)
	if job.IncludesMedia {
		t.Fatalf("Free export includes media: %+v", job)
	}
	e.svc.ProcessExports(context.Background())
	e.do(e.browser, "GET", "/v1/workspace/exports/"+job.ID, "", nil, 200, &job)
	if job.Status != "ready" || job.IncludesMedia || job.DownloadURL == "" {
		t.Fatalf("job after build %+v", job)
	}
	e.do(nil, "GET", job.DownloadURL, e.key, nil, 403, nil)
	st, _, b := e.raw(e.browser, "GET", job.DownloadURL, "", nil)
	if st != 200 {
		t.Fatalf("download %d", st)
	}
	files := readZip(t, b)
	for n := range files {
		if strings.HasPrefix(n, "media/") {
			t.Fatalf("Free export has a media file: %s", n)
		}
	}
	for _, want := range []string{"README.txt", "workspace.json", "collections.ndjson", "posts.ndjson", "events.ndjson", "media.ndjson"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing %s", want)
		}
	}
	var wsj struct {
		Counts        map[string]int
		IncludesMedia bool   `json:"includes_media"`
		Note          string `json:"note"`
	}
	json.Unmarshal(files["workspace.json"], &wsj)
	if wsj.IncludesMedia || !strings.Contains(wsj.Note, "Pro") || wsj.Counts["posts"] != 1 || wsj.Counts["collections"] < 1 || wsj.Counts["events"] == 0 {
		t.Fatalf("workspace.json %s", files["workspace.json"])
	}
	if !strings.Contains(string(files["README.txt"]), "Data only") {
		t.Fatal("README without the data-only note")
	}
	var med map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(files["media.ndjson"]), &med); err != nil {
		t.Fatalf("media.ndjson %s", files["media.ndjson"])
	}
	if med["id"] != m.ID || med["original_name"] != "photo.png" || med["mime_type"] != "image/png" || med["file"] != "" {
		t.Fatalf("media.ndjson %v", med)
	}
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(files["posts.ndjson"]), &line); err != nil {
		t.Fatal(err)
	}
	if line["content"] != "one v2" || line["status"] != "draft" || fmt.Sprint(line["media_files"]) != "[]" ||
		!strings.Contains(fmt.Sprint(line["tags"]), "red") {
		t.Fatalf("posts.ndjson %v", line)
	}
	ev := string(files["events.ndjson"])
	if !strings.Contains(ev, `"post.updated"`) || !strings.Contains(ev, `"session.`) && !strings.Contains(ev, `"api_key.`) {
		t.Fatalf("events.ndjson without the history or the audit events")
	}
}

func TestWorkspaceExportConcurrentRequests(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	admin := e.keyWith(app.ScopeAdmin)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, _, _ := e.raw(nil, "POST", "/v1/workspace/export", admin, nil)
			codes <- st
		}()
	}
	wg.Wait()
	close(codes)
	got := map[int]int{}
	for c := range codes {
		got[c]++
	}
	if got[202] != 1 || got[409] != 7 {
		t.Fatalf("want exactly one accepted export, got %v", got)
	}
}

func TestExportWorkerBuildsInBackground(t *testing.T) {
	e := newEnv(t)
	withDataDir(e)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunExportWorker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, "X-CSRF-Token", e.csrf)
	waitFor(t, func() bool {
		e.do(e.browser, "GET", "/v1/workspace/exports/"+job.ID, "", nil, 200, &job)
		return job.Status == "ready"
	})
}

func TestRetentionPruning(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Old")
	e.post(c.ID, map[string]any{"content": "old"})
	keep := e.post(c.ID, map[string]any{"content": "stays"})
	wsID := workspaceID(e)
	other := newWorkspace(e, "Other")
	oc := other.collection("Theirs")
	other.post(oc.ID, map[string]any{"content": "theirs"})

	// Seed deliveries: one finished, one pending, on an existing event.
	q := db.New(e.svc.DB)
	var wh app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.admin, map[string]any{"url": "https://example.com/h", "events": []string{"device.connected"}}, 201, &wh)
	evs, _ := e.svc.ListEvents(ctx, app.Principal{WorkspaceID: wsID, Kind: app.PrincipalSystem, Scopes: []string{app.ScopeAdmin}}, app.EventFilter{Limit: 200})
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	for i, st := range []string{"succeeded", "pending"} {
		id := fmt.Sprintf("dlv_seed%d", i)
		if err := q.InsertDelivery(ctx, db.InsertDeliveryParams{ID: id, WorkspaceID: wsID, WebhookID: wh.ID, EventID: evs.Data[i].ID,
			EventType: evs.Data[i].Type, NextAttemptAt: "9999", CreatedAt: ts}); err != nil {
			t.Fatal(err)
		}
		if st != "pending" {
			if err := q.FinishDelivery(ctx, db.FinishDeliveryParams{Status: st, NextAttemptAt: ts, ID: id}); err != nil {
				t.Fatal(err)
			}
		}
	}
	pendingEventID := evs.Data[1].ID

	setEnv := func(m map[string]string) { e.svc.Getenv = func(k string) string { return m[k] } }
	defer func() { e.svc.Now = time.Now }()

	// Defaults: deliveries 30 days, nothing else; nothing is old enough yet.
	setEnv(nil)
	rep, err := e.svc.RunRetention(ctx)
	if err != nil || rep != (app.RetentionReport{}) {
		t.Fatalf("fresh data must not be pruned: %+v %v", rep, err)
	}
	countEvents := func(p app.Principal) int {
		n := 0
		cur := ""
		for {
			page, err := e.svc.ListEvents(ctx, p, app.EventFilter{Limit: 200, Before: cur})
			if err != nil {
				t.Fatal(err)
			}
			n += len(page.Data)
			if page.NextCursor == "" {
				return n
			}
			cur = page.NextCursor
		}
	}
	mine := app.Principal{WorkspaceID: wsID, Kind: app.PrincipalSystem, Scopes: []string{app.ScopeAdmin}}
	before := countEvents(mine)

	// 40 days later, with all retention enabled.
	e.svc.Now = func() time.Time { return time.Now().Add(40 * 24 * time.Hour) }
	setEnv(map[string]string{"RETENTION_EVENTS_DAYS": "35"})
	rep, err = e.svc.RunRetention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deliveries != 1 || rep.Events == 0 {
		t.Fatalf("report %+v", rep)
	}
	if _, err := e.svc.GetPost(ctx, mine, keep.ID); err != nil {
		t.Fatal("posts are never pruned")
	}
	// Remaining events: the one with a pending delivery + the summary event.
	page, _ := e.svc.ListEvents(ctx, mine, app.EventFilter{Limit: 200})
	types := map[string]int{}
	for _, ev := range page.Data {
		types[ev.Type]++
	}
	if types["retention.events_pruned"] != 1 || countEvents(mine) >= before {
		t.Fatalf("after prune: %v (before %d)", types, before)
	}
	var kept bool
	for _, ev := range page.Data {
		kept = kept || ev.ID == pendingEventID
	}
	if !kept {
		t.Fatal("event with a pending delivery must be kept")
	}
	if _, err := q.GetDelivery(ctx, db.GetDeliveryParams{WorkspaceID: wsID, ID: "dlv_seed1"}); err != nil {
		t.Fatal("pending delivery must be kept")
	}
	// The other workspace got its own summary.
	op := app.Principal{WorkspaceID: workspaceIDOf(e, other), Kind: app.PrincipalSystem, Scopes: []string{app.ScopeAdmin}}
	opage, _ := e.svc.ListEvents(ctx, op, app.EventFilter{Type: "retention.events_pruned"})
	if len(opage.Data) != 1 {
		t.Fatal("other workspace summary missing")
	}

	// Events are still immutable.
	if _, err := e.svc.DB.ExecContext(ctx, "UPDATE events SET type = 'x'"); err == nil {
		t.Fatal("events must stay immutable")
	}

	// Idempotent: a second run prunes nothing new.
	rep, err = e.svc.RunRetention(ctx)
	if err != nil || rep.Events != 0 || rep.Deliveries != 0 {
		t.Fatalf("second run %+v %v", rep, err)
	}
}

func workspaceIDOf(e, o *env) string {
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", o.key, nil, 200, &ws)
	return ws.ID
}

func TestDeleteWorkspace(t *testing.T) {
	e := newEnv(t)
	dataDir := withDataDir(e)
	csrf := []string{"X-CSRF-Token", e.csrf}
	c := e.collection("Doomed")
	var m app.Media
	e.upload("/v1/media", e.key, nil, "a.png", pngBytes, 201, &m)
	e.post(c.ID, map[string]any{"content": "pic", "media": []string{m.ID}})
	e.do(nil, "POST", "/v1/webhooks", e.key, map[string]any{"url": "https://example.com/h", "events": []string{"post.*"}}, 201, nil)
	e.post(c.ID, map[string]any{"content": "creates a delivery"})
	var job app.WorkspaceExport
	e.do(e.browser, "POST", "/v1/workspace/export", "", nil, 202, &job, csrf...)
	e.svc.ProcessExports(context.Background())

	other := newWorkspace(e, "Survivor")
	oc := other.collection("Kept")
	var om app.Media
	other.upload("/v1/media", other.key, nil, "b.png", pngBytes, 201, &om)
	oit := other.post(oc.ID, map[string]any{"content": "kept", "media": []string{om.ID}})

	files, _ := filepath.Glob(filepath.Join(e.svc.Cfg.UploadDir, "*", "*"))
	if len(files) != 2 {
		t.Fatalf("expected 2 media files, got %v", files)
	}

	// API keys (even admin) can't; confirmation required.
	e.do(nil, "DELETE", "/v1/workspace", e.keyWith(app.ScopeAdmin), map[string]string{"confirm": "Test"}, 403, nil)
	e.do(e.browser, "DELETE", "/v1/workspace", "", nil, 422, nil, csrf...)
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "test"}, 422, nil, csrf...)
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 403, nil) // no CSRF
	resp := e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, csrf...)
	if !strings.Contains(resp.Header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("session cookie should be cleared: %q", resp.Header.Get("Set-Cookie"))
	}

	// Everything is gone: session, keys, data, files.
	e.do(e.browser, "GET", "/v1/workspace", "", nil, 401, nil)
	e.do(nil, "GET", "/v1/collections", e.key, nil, 401, nil)
	for _, table := range []string{"collections", "posts", "channels", "schedules", "jobs", "events", "media", "webhooks", "webhook_deliveries", "api_keys", "sessions", "workspace_exports"} {
		var m int
		e.svc.DB.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE workspace_id NOT IN (SELECT id FROM workspaces)").Scan(&m)
		if m != 0 {
			t.Fatalf("%s: %d orphaned rows", table, m)
		}
	}
	files, _ = filepath.Glob(filepath.Join(e.svc.Cfg.UploadDir, "*", "*"))
	if len(files) != 1 {
		t.Fatalf("only the other workspace's media should remain: %v", files)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "exports", job.ID+".zip")); !os.IsNotExist(err) {
		t.Fatal("export zip should be deleted")
	}

	// The other workspace is untouched.
	other.do(nil, "GET", "/v1/posts/"+oit.ID, other.key, nil, 200, nil)
	other.do(nil, "GET", "/v1/media/"+om.ID, other.key, nil, 200, nil)
	other.do(other.browser, "GET", "/v1/workspace", "", nil, 200, nil)
}

func TestDeleteWorkspaceLocalModeRefused(t *testing.T) {
	_, srv := newLocal(t)
	st, body := localReq(t, srv, "DELETE", "/v1/workspace", "", "", `{"confirm":"Local workspace"}`)
	if st != 409 || !strings.Contains(body, "volume") {
		t.Fatalf("local delete: %d %s", st, body)
	}
}
