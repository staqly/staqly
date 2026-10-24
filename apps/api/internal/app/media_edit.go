package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// ReplaceMedia is the media editor's "Replace the original". A media id never changes content (thumbnails and
// links are cached for good), so the edited file is a new file that takes the original's place: its name, alt
// text, folder and tags, and the drafts that used the original. The original goes to Deleted (restorable).
// A file that a scheduled, publishing, published or failed post uses is refused: those posts keep what they had.
func (s *Service) ReplaceMedia(ctx context.Context, p Principal, id, filename string, r io.Reader) (Media, error) {
	for _, scope := range []string{ScopeMediaWrite, ScopePostsWrite, ScopePostsDelete} {
		if err := p.require(scope); err != nil {
			return Media{}, err
		}
	}
	old, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil || old.DeletedAt.Valid {
		return Media{}, notFound("media")
	}
	quoted := `"` + id + `"`
	usedOutsideDrafts := func(q *db.Queries) error {
		if n, err := q.MediaUsedOutsideDrafts(ctx, db.MediaUsedOutsideDraftsParams{WorkspaceID: p.WorkspaceID, Quoted: quoted}); err != nil {
			return err
		} else if n > 0 {
			return conflict(fmt.Sprintf("%s is used by %d scheduled or published %s, which keep the original; save the edit as a new file instead",
				old.OriginalName, n, plural(int(n), "post", "posts")))
		}
		return nil
	}
	if err := usedOutsideDrafts(s.Q); err != nil { // before the upload; again in the transaction below
		return Media{}, err
	}
	if filename == "" {
		filename = old.OriginalName
	}
	edited, err := s.UploadMedia(ctx, p, filename, r)
	if err != nil {
		return Media{}, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := usedOutsideDrafts(q); err != nil {
			return err
		}
		now := s.now()
		if _, err := q.UpdateMediaInfo(ctx, db.UpdateMediaInfoParams{OriginalName: old.OriginalName, AltText: old.AltText,
			WorkspaceID: p.WorkspaceID, ID: edited.ID}); err != nil {
			return err
		}
		if _, err := q.SetMediaFolder(ctx, db.SetMediaFolderParams{FolderID: old.FolderID, WorkspaceID: p.WorkspaceID, ID: edited.ID}); err != nil {
			return err
		}
		if _, err := q.SetMediaTags(ctx, db.SetMediaTagsParams{Tags: old.Tags, WorkspaceID: p.WorkspaceID, ID: edited.ID}); err != nil {
			return err
		}
		if err := q.RepointDraftMedia(ctx, db.RepointDraftMediaParams{FromQuoted: quoted, ToQuoted: `"` + edited.ID + `"`,
			UpdatedAt: now, WorkspaceID: p.WorkspaceID}); err != nil {
			return err
		}
		_, err := q.TrashMedia(ctx, db.TrashMediaParams{DeletedAt: nullStr(now), WorkspaceID: p.WorkspaceID, ID: id})
		return err
	})
	if err != nil {
		_ = s.DiscardUpload(ctx, p, edited.ID)
		return Media{}, err
	}
	s.trackAs(nil, p, "media_edited", "kind", "file")
	return s.GetMediaInfo(ctx, p, edited.ID)
}

// MediaProject is an editable picture of the media editor: OpenGPEX's .gpex file (layers, text, masks) kept in
// the store, the library files it was made from and the file it last saved.
type MediaProject struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	SourceIDs []string `json:"source_ids"`
	OutputID  string   `json:"output_id"`
	SizeBytes int64    `json:"size_bytes"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// MediaProjectInput is what the editor sends with a project file.
type MediaProjectInput struct {
	Name      string
	SourceIDs []string
	OutputID  string
}

// mediaProjectMagic: a .gpex file starts with these bytes (OpenGPEX's container header).
const mediaProjectMagic = "GPEX"

// projectKey is where a media editor project lives in the store: "workspaces/<id>/projects/abcdef.json".
func projectKey(workspaceID, storageName string) string {
	return workspaceKey(workspaceID) + "projects/" + storageName
}

func toMediaProject(m db.MediaProject) MediaProject {
	out := MediaProject{ID: m.ID, Name: m.Name, OutputID: m.OutputID, SizeBytes: m.SizeBytes, CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt, SourceIDs: []string{}}
	_ = json.Unmarshal([]byte(m.SourceIds), &out.SourceIDs)
	return out
}

func (in *MediaProjectInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = "Untitled"
	}
	if len([]rune(in.Name)) > 200 {
		return invalid("name", "must be at most 200 characters")
	}
	if len(in.SourceIDs) > 20 {
		return invalid("source_ids", "at most 20 files")
	}
	return nil
}

// SaveMediaProject stores a project file: a new project when id is "", otherwise a new version of that one.
// Projects count toward the workspace's storage; the files they name must be in the workspace.
func (s *Service) SaveMediaProject(ctx context.Context, p Principal, id string, in MediaProjectInput, r io.Reader) (MediaProject, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return MediaProject{}, err
	}
	if err := in.clean(); err != nil {
		return MediaProject{}, err
	}
	for _, mid := range append(append([]string{}, in.SourceIDs...), in.OutputID) {
		if mid == "" {
			continue
		}
		if _, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: mid}); err != nil {
			return MediaProject{}, invalid("source_ids", "names a file that is not in this workspace: "+mid)
		}
	}
	var prev db.MediaProject
	if id != "" {
		var err error
		if prev, err = s.Q.GetMediaProject(ctx, db.GetMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return MediaProject{}, notFound("project")
		}
	}
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return MediaProject{}, err
	}
	maxBytes := s.Cfg.MaxUploadBytes
	if l := plan.Limits.MaxUploadBytes; l != Unlimited && l < maxBytes {
		maxBytes = l
	}
	if err := os.MkdirAll(s.Cfg.Blob.TempDir(), 0o750); err != nil {
		return MediaProject{}, err
	}
	f, err := os.CreateTemp(s.Cfg.Blob.TempDir(), ".project-*")
	if err != nil {
		return MediaProject{}, err
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err != nil {
		f.Close()
		return MediaProject{}, err
	}
	if n > maxBytes {
		f.Close()
		return MediaProject{}, &Error{Code: "too_large", Status: 413, Message: "project file exceeds the upload limit"}
	}
	head := make([]byte, len(mediaProjectMagic))
	if _, err := f.ReadAt(head, 0); err != nil || string(head) != mediaProjectMagic {
		f.Close()
		return MediaProject{}, &Error{Code: "unsupported_media_type", Status: 415, Message: "not a media editor project (.gpex)"}
	}
	if plan.Limits.StorageBytes != Unlimited {
		used, err := s.Q.StorageUsed(ctx, p.WorkspaceID) // files and projects
		if err != nil {
			f.Close()
			return MediaProject{}, err
		}
		if used-prev.SizeBytes+n > plan.Limits.StorageBytes {
			f.Close()
			return MediaProject{}, limitErr(plan, LimitStorage, plan.Limits.StorageBytes, used)
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return MediaProject{}, err
	}
	storage := randomString(16) + ".gpex"
	err = s.Cfg.Blob.Put(ctx, projectKey(p.WorkspaceID, storage), f, n, "application/octet-stream")
	f.Close()
	if err != nil {
		return MediaProject{}, fmt.Errorf("store project file: %w", err)
	}
	sources, _ := json.Marshal(nonNil(in.SourceIDs))
	now := s.now()
	replaced := "" // the file this version takes the place of, read in the transaction (a concurrent save may have moved it)
	if id == "" {
		id = newID("prj")
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		replaced = ""
		var prevBytes int64
		if prev.ID != "" {
			cur, err := q.GetMediaProject(ctx, db.GetMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id})
			if err != nil {
				return notFound("project")
			}
			replaced, prevBytes = cur.StorageName, cur.SizeBytes
		}
		if plan.Limits.StorageBytes != Unlimited { // again with the write: concurrent saves and uploads
			used, err := q.StorageUsed(ctx, p.WorkspaceID) // files and projects
			if err != nil {
				return err
			}
			if used-prevBytes+n > plan.Limits.StorageBytes {
				return limitErr(plan, LimitStorage, plan.Limits.StorageBytes, used)
			}
		}
		if prev.ID == "" {
			return q.CreateMediaProject(ctx, db.CreateMediaProjectParams{ID: id, WorkspaceID: p.WorkspaceID, Name: in.Name,
				SourceIds: string(sources), OutputID: in.OutputID, StorageName: storage, SizeBytes: n, CreatedBy: p.UserID,
				CreatedAt: now, UpdatedAt: now})
		}
		rows, err := q.UpdateMediaProject(ctx, db.UpdateMediaProjectParams{Name: in.Name, SourceIds: string(sources),
			OutputID: in.OutputID, StorageName: storage, SizeBytes: n, UpdatedAt: now, WorkspaceID: p.WorkspaceID, ID: id})
		if err == nil && rows == 0 {
			err = notFound("project")
		}
		return err
	})
	if err != nil {
		_ = s.Cfg.Blob.Delete(ctx, projectKey(p.WorkspaceID, storage))
		return MediaProject{}, err
	}
	if replaced != "" {
		if err := s.Cfg.Blob.Delete(ctx, projectKey(p.WorkspaceID, replaced)); err != nil {
			s.logger(ctx).Warn("old project file not removed", "project_id", id, "err", err)
		}
	}
	s.trackAs(nil, p, "media_edited", "kind", "project")
	return s.GetMediaProject(ctx, p, id)
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// GetMediaProject returns a project's facts (not its file).
func (s *Service) GetMediaProject(ctx context.Context, p Principal, id string) (MediaProject, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return MediaProject{}, err
	}
	m, err := s.Q.GetMediaProject(ctx, db.GetMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return MediaProject{}, notFound("project")
	}
	return toMediaProject(m), nil
}

// ListMediaProjects lists the workspace's projects, newest first; with mediaID, the ones made from or saved as it.
func (s *Service) ListMediaProjects(ctx context.Context, p Principal, mediaID string) ([]MediaProject, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListMediaProjects(ctx, db.ListMediaProjectsParams{WorkspaceID: p.WorkspaceID, Media: mediaID})
	if err != nil {
		return nil, err
	}
	out := make([]MediaProject, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMediaProject(r))
	}
	return out, nil
}

// OpenMediaProject returns a project's file for an authorized reader (looked up in the workspace, never by key).
func (s *Service) OpenMediaProject(ctx context.Context, p Principal, id string) (io.ReadSeekCloser, MediaProject, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return nil, MediaProject{}, err
	}
	m, err := s.Q.GetMediaProject(ctx, db.GetMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return nil, MediaProject{}, notFound("project")
	}
	f, err := s.Cfg.Blob.Open(ctx, projectKey(m.WorkspaceID, m.StorageName))
	if errors.Is(err, blob.ErrNotExist) {
		return nil, MediaProject{}, notFound("project file")
	}
	return f, toMediaProject(m), err
}

// DeleteMediaProject removes a project and its file (the files it was made from and saved stay).
func (s *Service) DeleteMediaProject(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopePostsDelete); err != nil {
		return err
	}
	m, err := s.Q.GetMediaProject(ctx, db.GetMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return notFound("project")
	}
	if _, err := s.W.DeleteMediaProject(ctx, db.DeleteMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
		return err
	}
	if err := s.Cfg.Blob.Delete(ctx, projectKey(m.WorkspaceID, m.StorageName)); err != nil {
		s.logger(ctx).Warn("project file not removed", "project_id", id, "err", err)
	}
	return nil
}
