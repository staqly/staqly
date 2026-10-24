package app

import (
	"context"
	"errors"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/blob"
)

// layoutMarker records in the store that its files are in workspace folders (MoveLegacyFiles ran).
const layoutMarker = "workspaces/.layout"

// removeWorkspaceFiles deletes a workspace's folder in the store (media, thumbnails, projects, backups) and returns how
// many files went.
func (s *Service) removeWorkspaceFiles(ctx context.Context, workspaceID string) (int, error) {
	var keys []string
	if err := s.Cfg.Blob.Walk(ctx, workspaceKey(workspaceID), func(key string, _ int64) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return 0, err
	}
	for i, k := range keys {
		if err := s.Cfg.Blob.Delete(ctx, k); err != nil {
			return i, err
		}
	}
	return len(keys), nil
}

// MoveLegacyFiles moves files stored before workspace folders ("ab/abcdef.jpg", "projects/ab/abcdef.json") to
// workspaces/<id>/..., once per store, and drops the old thumbnails (a cache, made again on demand). Each file is
// copied before its old copy is deleted, so an interrupted run is repeated safely at the next start.
func (s *Service) MoveLegacyFiles(ctx context.Context) error {
	if _, err := s.Cfg.Blob.Stat(ctx, layoutMarker); err == nil || !errors.Is(err, blob.ErrNotExist) {
		return err
	}
	media, err := s.Q.AllMediaFiles(ctx)
	if err != nil {
		return err
	}
	projects, err := s.Q.AllMediaProjectFiles(ctx)
	if err != nil {
		return err
	}
	moved := 0
	for _, m := range media {
		ok, err := s.moveFile(ctx, m.StorageName[:2]+"/"+m.StorageName, mediaKey(m.WorkspaceID, m.StorageName), m.MimeType)
		if err != nil {
			return err
		}
		if ok {
			moved++
		}
	}
	for _, p := range projects {
		ok, err := s.moveFile(ctx, "projects/"+p.StorageName[:2]+"/"+p.StorageName, projectKey(p.WorkspaceID, p.StorageName), "")
		if err != nil {
			return err
		}
		if ok {
			moved++
		}
	}
	var thumbs []string
	if err := s.Cfg.Blob.Walk(ctx, "thumbs/", func(key string, _ int64) error {
		thumbs = append(thumbs, key)
		return nil
	}); err != nil {
		return err
	}
	for _, k := range thumbs {
		if err := s.Cfg.Blob.Delete(ctx, k); err != nil {
			return err
		}
	}
	if moved > 0 || len(thumbs) > 0 {
		s.Log.Info("media files moved to workspace folders", "files", moved, "thumbnails_dropped", len(thumbs))
	}
	return s.Cfg.Blob.Put(ctx, layoutMarker, strings.NewReader(""), 0, "text/plain")
}

// moveFile copies from to to and deletes from; false when from is not there (moved already, or never stored).
func (s *Service) moveFile(ctx context.Context, from, to, contentType string) (bool, error) {
	size, err := s.Cfg.Blob.Stat(ctx, from)
	if errors.Is(err, blob.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	f, err := s.Cfg.Blob.Open(ctx, from)
	if err != nil {
		return false, err
	}
	err = s.Cfg.Blob.Put(ctx, to, f, size, contentType)
	f.Close()
	if err != nil {
		return false, err
	}
	return true, s.Cfg.Blob.Delete(ctx, from)
}
