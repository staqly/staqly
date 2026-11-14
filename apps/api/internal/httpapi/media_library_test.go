package httpapi_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
)

func realPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The media library: list with filters and sorts, picture size measured on upload, rename and alt text, a
// video's size from the browser, which posts use a file, bulk delete with its own scope, and isolation.
func TestMediaLibrary(t *testing.T) {
	e := newEnv(t)
	e.paid()

	var pic, wide, clip, notes app.Media
	e.upload("/v1/media", e.key, nil, "Sunset.png", realPNG(t, 1080, 1350), 201, &pic)
	e.upload("/v1/media", e.key, nil, "banner.png", realPNG(t, 1600, 900), 201, &wide)
	e.upload("/v1/media", e.key, nil, "clip.mp4", append([]byte("\x00\x00\x00\x18ftypmp42"), make([]byte, 64)...), 201, &clip)
	e.upload("/v1/media", e.key, nil, "voice.ogg", append([]byte("OggS\x00\x02"), make([]byte, 64)...), 201, &notes)
	// Documents are not media: refused, with what is accepted.
	e.upload("/v1/media", e.key, nil, "notes.txt", []byte("hello world, plain text"), 415, nil)
	if pic.Width != 1080 || pic.Height != 1350 || clip.Width != 0 {
		t.Fatalf("sizes on upload: %+v %+v", pic, clip)
	}

	c := e.collection("Media")
	e.post(c.ID, map[string]any{"content": "With the sunset\nmore text", "media": []string{pic.ID}})

	list := func(query string) app.MediaPage {
		t.Helper()
		var p app.MediaPage
		e.do(nil, "GET", "/v1/media"+query, e.key, nil, 200, &p)
		return p
	}
	if p := list(""); len(p.Data) != 4 || p.Data[0].ID != notes.ID || p.NextOffset != nil {
		t.Fatalf("newest first: %+v", p)
	}
	if p := list("?kind=image"); len(p.Data) != 2 {
		t.Fatalf("images: %+v", p.Data)
	}
	if p := list("?kind=video"); len(p.Data) != 1 || p.Data[0].ID != clip.ID {
		t.Fatalf("videos: %+v", p.Data)
	}
	if p := list("?kind=audio"); len(p.Data) != 1 || p.Data[0].ID != notes.ID || p.Data[0].MimeType != "audio/ogg" {
		t.Fatalf("audio: %+v", p.Data)
	}
	if p := list("?used=used"); len(p.Data) != 1 || p.Data[0].ID != pic.ID || p.Data[0].Uses != 1 {
		t.Fatalf("used: %+v", p.Data)
	}
	if p := list("?used=unused"); len(p.Data) != 3 {
		t.Fatalf("unused: %+v", p.Data)
	}
	if p := list("?q=SUN"); len(p.Data) != 1 || p.Data[0].ID != pic.ID {
		t.Fatalf("search: %+v", p.Data)
	}
	if p := list("?sort=name_asc"); p.Data[0].ID != wide.ID {
		t.Fatalf("by name: %+v", p.Data)
	}
	if p := list("?sort=size_desc"); p.Data[0].SizeBytes < p.Data[1].SizeBytes {
		t.Fatalf("by size: %+v", p.Data)
	}
	if p := list("?limit=3"); len(p.Data) != 3 || p.NextOffset == nil || *p.NextOffset != 3 {
		t.Fatalf("page 1: %+v", p)
	}
	if p := list("?limit=3&offset=3"); len(p.Data) != 1 || p.NextOffset != nil {
		t.Fatalf("page 2: %+v", p)
	}
	e.do(nil, "GET", "/v1/media?sort=nope", e.key, nil, 422, nil)
	e.do(nil, "GET", "/v1/media?kind=other", e.key, nil, 422, nil)

	// Rename, alt text, a video's size and length; bad values are refused.
	var got app.Media
	e.do(nil, "PATCH", "/v1/media/"+pic.ID, e.key, map[string]any{"name": "Golden hour.png", "alt_text": "Sun over the city"}, 200, &got)
	if got.OriginalName != "Golden hour.png" || got.AltText != "Sun over the city" || got.Width != 1080 {
		t.Fatalf("rename: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/media/"+clip.ID, e.key, map[string]any{"width": 1080, "height": 1920, "duration_ms": 15000}, 200, &got)
	e.do(nil, "GET", "/v1/media/"+clip.ID+"/info", e.key, nil, 200, &got)
	if got.Width != 1080 || got.Height != 1920 || got.DurationMs != 15000 {
		t.Fatalf("video size: %+v", got)
	}
	e.do(nil, "PATCH", "/v1/media/"+clip.ID, e.key, map[string]any{"name": " "}, 422, nil)
	e.do(nil, "PATCH", "/v1/media/"+clip.ID, e.key, map[string]any{"duration_ms": -1}, 422, nil)

	// Which posts use a file.
	var usage struct{ Data []app.MediaUse }
	e.do(nil, "GET", "/v1/media/"+pic.ID+"/usage", e.key, nil, 200, &usage)
	if len(usage.Data) != 1 || usage.Data[0].Title != "With the sunset" || usage.Data[0].Status != "draft" {
		t.Fatalf("usage: %+v", usage)
	}

	// Bulk delete needs posts:delete; unknown ids are reported.
	e.do(nil, "POST", "/v1/media/delete", e.key, map[string]any{"ids": []string{notes.ID}}, 403, nil)
	deleter := e.keyWith("media:read", "media:write", "posts:delete")
	var res struct{ Deleted, Missing []string }
	e.do(nil, "POST", "/v1/media/delete", deleter, map[string]any{"ids": []string{notes.ID, wide.ID, "med_nope"}}, 200, &res)
	if len(res.Deleted) != 2 || len(res.Missing) != 1 {
		t.Fatalf("bulk delete: %+v", res)
	}
	if p := list(""); len(p.Data) != 2 {
		t.Fatalf("after delete: %+v", p.Data)
	}

	// Another workspace sees none of it.
	ctx := context.Background()
	if err := db.New(e.svc.DB).CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_media_other", Name: "Other",
		RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := app.Principal{WorkspaceID: "ws_media_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	if p, err := e.svc.ListMedia(ctx, other, app.MediaFilter{}); err != nil || len(p.Data) != 0 {
		t.Fatalf("other workspace list: %+v %v", p, err)
	}
	if _, err := e.svc.UpdateMedia(ctx, other, pic.ID, app.MediaPatch{AltText: new(string)}); err == nil {
		t.Fatal("other workspace rename")
	}
	if u, err := e.svc.MediaUsage(ctx, other, pic.ID); err == nil {
		t.Fatalf("other workspace usage: %+v", u)
	}
}
