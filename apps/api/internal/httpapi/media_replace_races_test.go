package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Interleave another client's write while the edited file is still being uploaded.
type beforeRead struct {
	io.Reader
	once   sync.Once
	before func()
}

func (r *beforeRead) Read(p []byte) (int, error) {
	r.once.Do(r.before)
	return r.Reader.Read(p)
}

func TestReplaceMediaRechecksOriginalAfterUpload(t *testing.T) {
	for _, action := range []string{"metadata", "trash", "delete"} {
		t.Run(action, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			p, err := e.svc.AuthenticateAPIKey(ctx, e.admin)
			if err != nil {
				t.Fatal(err)
			}
			var old app.Media
			e.upload("/v1/media", e.key, nil, "old.png", realPNG(t, 2, 2), 201, &old)
			reader := &beforeRead{Reader: bytes.NewReader(realPNG(t, 4, 4)), before: func() {
				switch action {
				case "metadata":
					e.do(nil, "PATCH", "/v1/media/"+old.ID, e.key, map[string]any{"name": "latest.png", "alt_text": "Latest description", "tags": []string{"latest"}}, 200, nil)
				case "trash":
					e.do(nil, "DELETE", "/v1/media/"+old.ID, e.admin, nil, 204, nil)
				case "delete":
					e.do(nil, "DELETE", "/v1/media/"+old.ID+"?permanent=true", e.admin, nil, 204, nil)
				}
			}}
			got, err := e.svc.ReplaceMedia(ctx, p, old.ID, "edit.png", reader)
			if action == "metadata" {
				if err != nil {
					t.Fatal(err)
				}
				if got.OriginalName != "latest.png" || got.AltText != "Latest description" || len(got.Tags) != 1 || got.Tags[0] != "latest" {
					t.Fatalf("replacement used stale metadata: %+v", got)
				}
			} else {
				var apiErr *app.Error
				if !errors.As(err, &apiErr) || apiErr.Status != 404 {
					t.Fatalf("replaced a removed original: media=%+v err=%v", got, err)
				}
				var list struct{ Data []app.Media }
				e.do(nil, "GET", "/v1/media", e.key, nil, 200, &list)
				if len(list.Data) != 0 {
					t.Fatalf("abandoned upload left behind: %+v", list.Data)
				}
			}
		})
	}
}

func TestReplaceMediaInvalidatesDraftVersion(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Edits")
	var old, edited app.Media
	e.upload("/v1/media", e.key, nil, "old.png", realPNG(t, 2, 2), 201, &old)
	draft := e.post(c.ID, map[string]any{"media": []string{old.ID}})
	e.upload("/v1/media/"+old.ID+"/replace", e.admin, nil, "edited.png", realPNG(t, 4, 4), 201, &edited)
	// An editor opened before the replacement removed the picture locally, without seeing the new edit.
	e.do(nil, "PATCH", "/v1/posts/"+draft.ID, e.key, map[string]any{"expected_version": draft.Version, "media": []string{}}, 409, nil)
	var got app.Post
	e.do(nil, "GET", "/v1/posts/"+draft.ID, e.key, nil, 200, &got)
	if got.Version <= draft.Version || len(got.Media) != 1 || got.Media[0] != edited.ID {
		t.Fatalf("replacement lost to a stale editor: %+v", got)
	}
}

func TestConcurrentKeepMediaPreservesOneCopy(t *testing.T) {
	e := newEnv(t)
	for round := range 10 {
		var a, b app.Media
		png := realPNG(t, round+2, round+2)
		e.upload("/v1/media", e.key, nil, "a.png", png, 201, &a)
		e.upload("/v1/media", e.key, nil, "b.png", png, 201, &b)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, id := range []string{a.ID, b.ID} {
			wg.Go(func() {
				<-start
				resp := e.do(nil, "POST", "/v1/media/"+id+"/keep", e.admin, nil, 0, nil)
				if resp.StatusCode != 200 && resp.StatusCode != 404 {
					t.Errorf("keep returned %d", resp.StatusCode)
				}
			})
		}
		close(start)
		wg.Wait()
		e.do(nil, "GET", "/v1/media/"+a.ID+"/info", e.key, nil, 200, &a)
		e.do(nil, "GET", "/v1/media/"+b.ID+"/info", e.key, nil, 200, &b)
		if (a.DeletedAt == nil) == (b.DeletedAt == nil) {
			t.Fatalf("two clients keeping opposite copies must leave one active file: a=%v b=%v", a.DeletedAt, b.DeletedAt)
		}
	}
}

func TestPermanentDeleteInvalidatesDraftVersion(t *testing.T) {
	for _, where := range []string{"post", "thread"} {
		t.Run(where, func(t *testing.T) {
			e := newEnv(t)
			c := e.collection("Delete")
			var m app.Media
			e.upload("/v1/media", e.key, nil, "old.png", realPNG(t, 2, 2), 201, &m)
			input := map[string]any{"media": []string{m.ID}}
			if where == "thread" {
				input = map[string]any{"thread": []map[string]any{{"content": "Reply", "media": []string{m.ID}}}}
			}
			p := e.post(c.ID, input)
			e.do(nil, "DELETE", "/v1/media/"+m.ID+"?permanent=true", e.admin, nil, 204, nil)
			e.do(nil, "PATCH", "/v1/posts/"+p.ID, e.key, map[string]any{"expected_version": p.Version, "title": "Stale edit"}, 409, nil)
			var after app.Post
			e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &after)
			if after.Version <= p.Version || len(after.Media) != 0 || (where == "thread" && len(after.Thread[0].Media) != 0) {
				t.Fatalf("media deletion not versioned: %+v", after)
			}
		})
	}
}
