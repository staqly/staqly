package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
)

// Interleave a request cancellation or another write immediately after storage accepts the file.
type afterPutStore struct {
	blob.Store
	after func()
	err   error
}

func (s afterPutStore) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if err := s.Store.Put(ctx, key, r, size, mime); err != nil {
		return err
	}
	s.after()
	return s.err
}

func TestCanceledUploadRemovesStoredObject(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		putErr error
	}{
		{"media", nil}, {"new project", nil}, {"updated project", nil},
		{"media", context.Canceled}, {"new project", context.Canceled}, {"updated project", context.Canceled},
	} {
		name := tc.kind
		if tc.putErr != nil {
			name += "/lost storage response"
		}
		t.Run(name, func(t *testing.T) {
			kind := tc.kind
			e := newEnv(t)
			fake := withS3(t, e)
			p, err := e.svc.AuthenticateAPIKey(context.Background(), e.admin)
			if err != nil {
				t.Fatal(err)
			}
			id := ""
			if kind == "updated project" {
				saved, err := e.svc.SaveMediaProject(context.Background(), p, "", app.MediaProjectInput{Name: "Original"}, bytes.NewReader(designFile("original")))
				if err != nil {
					t.Fatal(err)
				}
				id = saved.ID
			}
			before := fake.Keys("media")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e.svc.Cfg.Blob = afterPutStore{Store: e.svc.Cfg.Blob, after: cancel, err: tc.putErr}
			if kind == "media" {
				_, err = e.svc.UploadMedia(ctx, p, "photo.png", bytes.NewReader(realPNG(t, 2, 2)))
			} else {
				_, err = e.svc.SaveMediaProject(ctx, p, id, app.MediaProjectInput{Name: "Changed"}, bytes.NewReader(designFile("changed")))
			}
			if err == nil {
				t.Fatal("canceled write succeeded")
			}
			if keys := fake.Keys("media"); len(keys) != len(before) {
				t.Fatalf("canceled upload leaked objects: before=%v after=%v", before, keys)
			}
			if id != "" {
				f, project, err := e.svc.OpenMediaProject(context.Background(), p, id)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				data, err := io.ReadAll(f)
				if err != nil || project.Name != "Original" || !bytes.Equal(data, designFile("original")) {
					t.Fatalf("original project lost: %+v %s %v", project, data, err)
				}
			}
		})
	}
}

func TestDiscardUploadAfterCancellation(t *testing.T) {
	e := newEnv(t)
	fake := withS3(t, e)
	p, err := e.svc.AuthenticateAPIKey(context.Background(), e.admin)
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.UploadMedia(context.Background(), p, "photo.png", bytes.NewReader(realPNG(t, 2, 2)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.svc.DiscardUpload(ctx, p, m.ID); err != nil {
		t.Fatal(err)
	}
	if keys := fake.Keys("media"); len(keys) != 0 {
		t.Fatalf("discard leaked objects: %v", keys)
	}
	if _, err := e.svc.GetMediaInfo(context.Background(), p, m.ID); err == nil {
		t.Fatal("discard left a media row")
	}
}

func TestConcurrentProjectSaveAndDeleteRemovesObjects(t *testing.T) {
	e := newEnv(t)
	fake := withS3(t, e)
	ctx := context.Background()
	p, err := e.svc.AuthenticateAPIKey(ctx, e.admin)
	if err != nil {
		t.Fatal(err)
	}
	store := e.svc.Cfg.Blob
	for range 30 {
		e.svc.Cfg.Blob = store
		project, err := e.svc.SaveMediaProject(ctx, p, "", app.MediaProjectInput{Name: "Original"}, bytes.NewReader(designFile("original")))
		if err != nil {
			t.Fatal(err)
		}
		stored := make(chan struct{})
		start := make(chan struct{})
		e.svc.Cfg.Blob = afterPutStore{Store: store, after: func() {
			close(stored)
			<-start
		}}
		var wg sync.WaitGroup
		wg.Go(func() {
			_, err := e.svc.SaveMediaProject(ctx, p, project.ID, app.MediaProjectInput{Name: "Changed"}, bytes.NewReader(designFile("changed")))
			var apiErr *app.Error
			if err != nil && (!errors.As(err, &apiErr) || apiErr.Status != 404) {
				t.Errorf("save: %v", err)
			}
		})
		wg.Go(func() {
			<-stored
			close(start)
			if err := e.svc.DeleteMediaProject(ctx, p, project.ID); err != nil {
				t.Errorf("delete: %v", err)
			}
		})
		wg.Wait()
		if keys := fake.Keys("media"); len(keys) != 0 {
			t.Fatalf("save/delete leaked objects: %v", keys)
		}
	}
}

func TestCanceledAIRequestRemovesInputs(t *testing.T) {
	e := newEnv(t)
	fake := withS3(t, e)
	e.aiSetup(newFakeFal(t), nil)
	p, err := e.svc.AuthenticateAPIKey(context.Background(), e.admin)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.svc.Cfg.Blob = afterPutStore{Store: e.svc.Cfg.Blob, after: cancel}
	_, _, err = e.svc.CreateAIJob(ctx, p, app.AIJobInput{Operation: "remove_background", Image: realPNG(t, 32, 32)})
	if err == nil {
		t.Fatal("canceled job was queued")
	}
	if keys := fake.Keys("media"); len(keys) != 0 {
		t.Fatalf("canceled AI request leaked inputs: %v", keys)
	}
}

func TestUploadRechecksPlanAfterStorageWrite(t *testing.T) {
	for _, kind := range []string{"media", "project"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			fake := withS3(t, e)
			e.svc.Cfg.MaxUploadBytes = 10 << 20
			ctx := context.Background()
			p, err := e.svc.AuthenticateAPIKey(ctx, e.admin)
			if err != nil {
				t.Fatal(err)
			}
			e.svc.Cfg.Blob = afterPutStore{Store: e.svc.Cfg.Blob, after: func() {
				if err := e.svc.SetPlan(ctx, p.WorkspaceID, limitedPlan, ""); err != nil {
					t.Fatal(err)
				}
			}}
			// The new plan allows 5 MiB, but the upload began on Pro's 100 MiB allowance.
			if kind == "media" {
				data := append(realPNG(t, 2, 2), make([]byte, 6<<20)...)
				_, err = e.svc.UploadMedia(ctx, p, "photo.png", bytes.NewReader(data))
			} else {
				data := append(designFile("large"), bytes.Repeat([]byte(" "), 6<<20)...)
				_, err = e.svc.SaveMediaProject(ctx, p, "", app.MediaProjectInput{Name: "Large"}, bytes.NewReader(data))
			}
			var apiErr *app.Error
			if !errors.As(err, &apiErr) || apiErr.Status != 413 {
				t.Fatalf("upload exceeded the current plan: %v", err)
			}
			if keys := fake.Keys("media"); len(keys) != 0 {
				t.Fatalf("rejected upload leaked objects: %v", keys)
			}
		})
	}
}
