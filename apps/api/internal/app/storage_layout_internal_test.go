package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
)

func storedKeys(t *testing.T, st blob.Store) []string {
	t.Helper()
	var keys []string
	if err := st.Walk(context.Background(), "", func(k string, _ int64) error {
		keys = append(keys, k)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	return keys
}

// Files live in the workspace's folder; files stored before that move there once, at startup.
func TestFilesLiveInWorkspaceFolders(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 400, 300))); err != nil {
		t.Fatal(err)
	}
	m, err := s.UploadMedia(ctx, p, "a.png", bytes.NewReader(pic.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	th, err := s.MediaThumb(ctx, p, m.ID, 320)
	if err != nil {
		t.Fatal(err)
	}
	th.File.Close()
	row, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	name := row.StorageName
	got := storedKeys(t, s.Cfg.Blob)
	want := []string{mediaKey(p.WorkspaceID, name), thumbKey(p.WorkspaceID, name, 320, ".png")}
	if strings.Join(got, ",") != strings.Join(want, ",") || !strings.HasPrefix(want[1], "workspaces/"+p.WorkspaceID+"/thumbs/") {
		t.Fatalf("stored %v, want %v", got, want)
	}

	// The layout before workspace folders: move the upload back there, with an old thumbnail next to it.
	legacy := name[:2] + "/" + name
	if _, err := s.moveFile(ctx, mediaKey(p.WorkspaceID, name), legacy, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := s.Cfg.Blob.Put(ctx, "thumbs/"+name[:2]+"/"+name+"_320.jpg", strings.NewReader("old"), 3, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.removeWorkspaceFiles(ctx, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveLegacyFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if got := storedKeys(t, s.Cfg.Blob); len(got) != 1 || got[0] != mediaKey(p.WorkspaceID, name) {
		t.Fatalf("after the move: %v", got)
	}
	f, err := s.openMediaFile(ctx, p.WorkspaceID, name)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(b, pic.Bytes()) {
		t.Fatal("moved file differs")
	}
	// Once only: a file put back in the old place stays there.
	if err := s.Cfg.Blob.Put(ctx, legacy, strings.NewReader("x"), 1, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveLegacyFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cfg.Blob.Stat(ctx, legacy); err != nil {
		t.Fatalf("second run moved files again: %v", err)
	}

	// Deleting the workspace's files is its folder.
	n, err := s.removeWorkspaceFiles(ctx, p.WorkspaceID)
	if err != nil || n != 1 {
		t.Fatalf("removed %d, %v", n, err)
	}
	if got := storedKeys(t, s.Cfg.Blob); len(got) != 1 || got[0] != legacy {
		t.Fatalf("left: %v", got)
	}
}
