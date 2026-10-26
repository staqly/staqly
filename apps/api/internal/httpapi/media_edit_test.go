package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// designFile is a minimal design editor project; body tells versions apart.
func designFile(body string) []byte {
	return []byte(`{"version":1,"name":"` + body + `","width":1080,"height":1080,"pages":[{"id":"p1","background":{"kind":"color","color":"#fff"},"elements":[]}]}`)
}

// sendFile is upload with any method (PUT for a new version of a project).
func (e *env) sendFile(method, path, key string, fields map[string]string, content []byte, want int, out any) {
	e.t.Helper()
	body, ct := multipartBody(e.t, fields, "file.bin", content)
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("decode: %v: %s", err, raw)
		}
	}
}

// Replace the original: the edit becomes a new file with the original's name, alt text, folder and tags,
// drafts that used the original use the edit, and the original goes to Deleted. A file that a scheduled (or
// published) post uses is refused, so those posts keep what they had.
func TestMediaReplace(t *testing.T) {
	e := newEnv(t)
	e.paid()
	c := e.collection("Edits")
	ch := e.customChannel("Own")

	var a, b app.Media
	e.upload("/v1/media", e.key, nil, "photo.png", realPNG(t, 20, 10), 201, &a)
	e.upload("/v1/media", e.key, nil, "other.png", realPNG(t, 30, 10), 201, &b)
	e.do(nil, "PATCH", "/v1/media/"+a.ID, e.key, map[string]any{"alt_text": "A runner"}, 200, nil)
	draft := e.post(c.ID, map[string]any{"media": []string{a.ID}})
	scheduled := e.post(c.ID, map[string]any{"media": []string{b.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+scheduled.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)

	// The default agent key may not delete: replacing trashes the original, so it needs posts:delete too.
	e.upload("/v1/media/"+a.ID+"/replace", e.key, nil, "edit.png", realPNG(t, 40, 40), 403, nil)
	var edited app.Media
	e.upload("/v1/media/"+a.ID+"/replace", e.admin, nil, "edit.png", realPNG(t, 40, 40), 201, &edited)
	if edited.ID == a.ID || edited.OriginalName != "photo.png" || edited.AltText != "A runner" || edited.Width != 40 {
		t.Fatalf("edited: %+v", edited)
	}
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+draft.ID, e.key, nil, 200, &after)
	if len(after.Media) != 1 || after.Media[0] != edited.ID {
		t.Fatalf("the draft uses the edit: %v", after.Media)
	}
	var old app.Media
	e.do(nil, "GET", "/v1/media/"+a.ID+"/info", e.key, nil, 200, &old)
	if old.DeletedAt == nil {
		t.Fatalf("the original is in Deleted: %+v", old)
	}

	e.upload("/v1/media/"+b.ID+"/replace", e.admin, nil, "edit.png", realPNG(t, 40, 40), 409, nil)
	e.do(nil, "GET", "/v1/posts/"+scheduled.ID, e.key, nil, 200, &after)
	if after.Media[0] != b.ID {
		t.Fatalf("the scheduled post keeps its file: %v", after.Media)
	}
}

// Projects: an editable design per picture, listed by the files it was made from, saved again in place,
// readable back byte for byte, deleted with posts:delete, invisible to another workspace.
func TestMediaProjects(t *testing.T) {
	e := newEnv(t)
	e.paid()
	ctx := context.Background()
	var a app.Media
	e.upload("/v1/media", e.key, nil, "photo.png", realPNG(t, 20, 10), 201, &a)

	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Poster"}, []byte("not a project"), 415, nil)
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Old"}, []byte("GPEX\x01\x00\x14\x00x"), 415, nil)
	// The video and sound editor's timelines are projects too; an empty one is not.
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Reel"}, []byte(`{"version":1,"kind":"video","width":1080,"height":1920,"clips":[{"id":"c1"}],"captions":[],"sounds":[]}`), 201, nil)
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Jingle"}, []byte(`{"version":1,"kind":"audio","width":0,"height":0,"clips":[],"sounds":[{"id":"s1"}]}`), 201, nil)
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Silence"}, []byte(`{"version":1,"kind":"audio","sounds":[]}`), 415, nil)
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Odd"}, []byte(`{"version":1,"kind":"poster","width":1,"height":1,"pages":[{}]}`), 415, nil)
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"source_ids": "med_nope"}, designFile("x"), 422, nil)

	var p app.MediaProject
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Poster", "source_ids": a.ID}, designFile("v1"), 201, &p)
	if p.Name != "Poster" || len(p.SourceIDs) != 1 || p.SourceIDs[0] != a.ID {
		t.Fatalf("project: %+v", p)
	}
	var list struct{ Data []app.MediaProject }
	e.do(nil, "GET", "/v1/media-projects?media="+a.ID, e.key, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != p.ID {
		t.Fatalf("by media: %+v", list)
	}
	e.do(nil, "GET", "/v1/media-projects?media=med_other", e.key, nil, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("other media: %+v", list)
	}

	e.sendFile("PUT", "/v1/media-projects/"+p.ID, e.key, map[string]string{"name": "Poster 2", "source_ids": a.ID, "output_id": a.ID, "page_ids": a.ID + ", " + a.ID}, designFile("v2"), 200, &p)
	if p.Name != "Poster 2" || p.OutputID != a.ID || len(p.PageIDs) != 2 {
		t.Fatalf("saved again: %+v", p)
	}
	// Its saved file in Deleted: the project is left out with it until the file comes back.
	e.do(nil, "DELETE", "/v1/media/"+a.ID, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/media-projects", e.key, nil, 200, &list)
	if slices.ContainsFunc(list.Data, func(x app.MediaProject) bool { return x.ID == p.ID }) {
		t.Fatalf("listed with its file deleted: %+v", list)
	}
	e.do(nil, "POST", "/v1/media/"+a.ID+"/restore", e.admin, nil, 200, nil)
	e.do(nil, "GET", "/v1/media-projects", e.key, nil, 200, &list)
	if !slices.ContainsFunc(list.Data, func(x app.MediaProject) bool { return x.ID == p.ID }) {
		t.Fatalf("not listed after the restore: %+v", list)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/v1/media-projects/"+p.ID+"/file", nil)
	req.Header.Set("Authorization", "Bearer "+e.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(body, designFile("v2")) {
		t.Fatalf("file: %d %q", res.StatusCode, body)
	}

	other := app.Principal{WorkspaceID: "ws_projects_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	if _, err := e.svc.GetMediaProject(ctx, other, p.ID); err == nil {
		t.Fatal("another workspace read a project")
	}
	if err := e.svc.DeleteMediaProject(ctx, other, p.ID); err == nil {
		t.Fatal("another workspace deleted a project")
	}

	e.do(nil, "DELETE", "/v1/media-projects/"+p.ID, e.key, nil, 403, nil) // posts:delete
	e.do(nil, "DELETE", "/v1/media-projects/"+p.ID, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/media-projects/"+p.ID, e.key, nil, 404, nil)
}

// The brand kit: empty at first, shared by the workspace, colours and logos checked, read with media:read and
// written with media:write.
func TestDesignBrand(t *testing.T) {
	e := newEnv(t)
	var b app.DesignBrand
	e.do(nil, "GET", "/v1/design/brand", e.key, nil, 200, &b)
	if len(b.Colors)+len(b.Fonts)+len(b.Logos) != 0 || b.Colors == nil {
		t.Fatalf("empty kit: %+v", b)
	}
	var m app.Media
	e.upload("/v1/media", e.key, nil, "logo.png", realPNG(t, 8, 8), 201, &m)
	pic := m.ID
	e.do(nil, "PUT", "/v1/design/brand", e.key, map[string]any{"colors": []string{"#1F8A4C"}, "fonts": []string{"Montserrat"}, "logos": []string{pic}}, 200, &b)
	if b.Colors[0] != "#1f8a4c" || b.Fonts[0] != "Montserrat" || b.Logos[0] != pic {
		t.Fatalf("kit: %+v", b)
	}
	e.do(nil, "PUT", "/v1/design/brand", e.key, map[string]any{"colors": []string{"red"}, "fonts": []string{}, "logos": []string{}}, 422, nil)
	e.do(nil, "PUT", "/v1/design/brand", e.key, map[string]any{"colors": []string{}, "fonts": []string{}, "logos": []string{"med_nope"}}, 422, nil)
	// Its logo in Deleted: the kit leaves it out, and changing the kit still works.
	e.do(nil, "DELETE", "/v1/media/"+pic, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/design/brand", e.key, nil, 200, &b)
	if len(b.Logos) != 0 {
		t.Fatalf("deleted logo kept: %+v", b)
	}
	e.do(nil, "PUT", "/v1/design/brand", e.key, map[string]any{"colors": []string{"#1f8a4c", "#7c3aed"}, "fonts": b.Fonts, "logos": b.Logos}, 200, &b)
	viewer := e.keyWith(app.ScopeMediaRead)
	e.do(nil, "GET", "/v1/design/brand", viewer, nil, 200, &b)
	e.do(nil, "PUT", "/v1/design/brand", viewer, map[string]any{"colors": []string{}, "fonts": []string{}, "logos": []string{}}, 403, nil)
}

func TestStockPhotos(t *testing.T) {
	e := newEnv(t)
	var pinged atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Client-ID test-key" {
			w.WriteHeader(401)
			return
		}
		photo := `{"id":"p1","width":4000,"height":3000,"alt_description":"a lake","urls":{"small":"https://images.example/s","raw":"https://images.example/r?ixid=1"},"links":{"html":"https://unsplash.example/p1"},"user":{"name":"Jane Doe","links":{"html":"https://unsplash.example/jane"}}}`
		switch r.URL.Path {
		case "/search/photos":
			fmt.Fprintf(w, `{"results":[%s]}`, photo)
		case "/photos":
			fmt.Fprintf(w, `[%s]`, photo)
		case "/photos/p1/download":
			pinged.Add(1)
			fmt.Fprint(w, `{"url":"x"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(fake.Close)

	var page app.StockPage
	e.do(nil, "GET", "/v1/stock/photos?query=lake", e.key, nil, 200, &page)
	if page.Configured || len(page.Data) != 0 {
		t.Fatalf("off without a key: %+v", page)
	}
	e.do(nil, "POST", "/v1/stock/photos/p1/use", e.key, nil, 409, nil)

	e.svc.Cfg.Stock = app.StockConfig{UnsplashKey: "test-key", UnsplashBaseURL: fake.URL}
	for _, q := range []string{"?query=lake", ""} {
		e.do(nil, "GET", "/v1/stock/photos"+q, e.key, nil, 200, &page)
		if !page.Configured || len(page.Data) != 1 {
			t.Fatalf("%q: %+v", q, page)
		}
		p := page.Data[0]
		if p.URL != "https://images.example/r?ixid=1&w=2000&fm=jpg&q=85" || p.Author != "Jane Doe" || !strings.Contains(p.AuthorURL, "utm_source=staqly") {
			t.Fatalf("%q: %+v", q, p)
		}
	}
	e.do(nil, "POST", "/v1/stock/photos/p1/use", e.key, nil, 204, nil)
	if pinged.Load() != 1 {
		t.Fatalf("download ping: %d", pinged.Load())
	}
	e.do(nil, "POST", "/v1/stock/photos/p1/use", e.keyWith(app.ScopeMediaRead), nil, 403, nil)
}
