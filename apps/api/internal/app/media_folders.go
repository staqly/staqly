package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ---- media library folders and tags ----
//
// Folders are virtual: a file sits in one folder ("" = the top level) and folders nest. They only organize, so
// deleting a folder moves its files and subfolders up to its parent and never deletes a file. Tags are free
// labels on files, normalised like post tags in the dashboard: lowercase, no leading #, spaces become dashes.

const (
	maxMediaFolderName = 80
	maxMediaFolders    = 500
	maxMediaFolderPath = 10 // nesting depth
	maxMediaBulk       = 200
)

// MediaFolder is one folder with the number of files directly in it.
type MediaFolder struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id"` // "" = the top level
	Name      string `json:"name"`
	Files     int64  `json:"files"`
	CreatedAt string `json:"created_at"`
}

// MediaTagCount is a tag and how many files carry it.
type MediaTagCount struct {
	Tag   string `json:"tag"`
	Files int64  `json:"files"`
}

// MediaFolders is the library's tree in one answer: every folder, the top level's file count, and the tags.
type MediaFolders struct {
	Data     []MediaFolder   `json:"data"`
	RootFile int64           `json:"root_files"`
	Total    int64           `json:"total_files"`
	Tags     []MediaTagCount `json:"tags"`
	// Files in Deleted, and files that have an identical twin (both outside every count above).
	Deleted    int64 `json:"deleted_files"`
	Duplicates int64 `json:"duplicate_files"`
}

func toMediaFolder(f db.MediaFolder, files int64) MediaFolder {
	return MediaFolder{ID: f.ID, ParentID: f.ParentID, Name: f.Name, Files: files, CreatedAt: f.CreatedAt}
}

// normalizeMediaTags lowercases, drops a leading #, turns spaces into dashes, dedupes and caps the list.
func normalizeMediaTags(in []string) ([]string, error) {
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		t = strings.TrimLeft(t, "#")
		t = strings.Join(strings.Fields(t), "-")
		if t == "" {
			continue
		}
		if len([]rune(t)) > maxTagLen {
			return nil, invalid("tags", fmt.Sprintf("each tag is at most %d characters", maxTagLen))
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	if len(out) > maxTags {
		return nil, invalid("tags", fmt.Sprintf("at most %d tags", maxTags))
	}
	return out, nil
}

func parseMediaTags(raw string) []string {
	var tags []string
	if json.Unmarshal([]byte(raw), &tags) != nil || tags == nil {
		return []string{}
	}
	return tags
}

// ListMediaFolders returns the whole tree (folders are few), file counts and tag counts.
func (s *Service) ListMediaFolders(ctx context.Context, p Principal) (MediaFolders, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return MediaFolders{}, err
	}
	folders, err := s.Q.ListMediaFolders(ctx, p.WorkspaceID)
	if err != nil {
		return MediaFolders{}, err
	}
	counts, err := s.Q.MediaFolderCounts(ctx, p.WorkspaceID)
	if err != nil {
		return MediaFolders{}, err
	}
	byFolder := map[string]int64{}
	out := MediaFolders{Data: make([]MediaFolder, 0, len(folders)), Tags: []MediaTagCount{}}
	known := map[string]bool{}
	for _, f := range folders {
		known[f.ID] = true
	}
	for _, c := range counts {
		out.Total += c.Files
		// Files in a folder that no longer exists count as the top level, where they are listed.
		if c.FolderID == "" || !known[c.FolderID] {
			out.RootFile += c.Files
			continue
		}
		byFolder[c.FolderID] = c.Files
	}
	for _, f := range folders {
		out.Data = append(out.Data, toMediaFolder(f, byFolder[f.ID]))
	}
	raws, err := s.Q.MediaTagsAll(ctx, p.WorkspaceID)
	if err != nil {
		return MediaFolders{}, err
	}
	tagCount := map[string]int64{}
	for _, raw := range raws {
		for _, t := range parseMediaTags(raw) {
			tagCount[t]++
		}
	}
	for t, n := range tagCount {
		out.Tags = append(out.Tags, MediaTagCount{Tag: t, Files: n})
	}
	if out.Deleted, err = s.Q.TrashedMediaCount(ctx, p.WorkspaceID); err != nil {
		return MediaFolders{}, err
	}
	if out.Duplicates, err = s.Q.DuplicateMediaCount(ctx, p.WorkspaceID); err != nil {
		return MediaFolders{}, err
	}
	sort.Slice(out.Tags, func(i, j int) bool {
		if out.Tags[i].Files != out.Tags[j].Files {
			return out.Tags[i].Files > out.Tags[j].Files
		}
		return out.Tags[i].Tag < out.Tags[j].Tag
	})
	return out, nil
}

// MediaFolderInput creates or changes a folder. ParentID "" = the top level; on update, nil keeps the parent.
type MediaFolderInput struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

func cleanFolderName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", invalid("name", "is required")
	}
	if strings.ContainsAny(name, "/\\") {
		return "", invalid("name", "can't contain / or \\")
	}
	if len([]rune(name)) > maxMediaFolderName {
		return "", invalid("name", fmt.Sprintf("at most %d characters", maxMediaFolderName))
	}
	return name, nil
}

// checkFolderPlace validates a folder's name under a parent: the parent exists, the name is free there
// (ignoring case) and, for a move, the folder doesn't go under itself or its own subfolders.
func checkFolderPlace(folders []db.MediaFolder, id, parentID, name string) error {
	byID := map[string]db.MediaFolder{}
	for _, f := range folders {
		byID[f.ID] = f
	}
	if parentID != "" {
		if _, ok := byID[parentID]; !ok {
			return invalid("parent_id", "unknown folder")
		}
		depth := 1
		for at := parentID; at != ""; at = byID[at].ParentID {
			if at == id {
				return invalid("parent_id", "a folder can't go inside itself or one of its own folders")
			}
			depth++
			if depth > maxMediaFolderPath+1 {
				return invalid("parent_id", fmt.Sprintf("folders nest at most %d deep", maxMediaFolderPath))
			}
		}
	}
	for _, f := range folders {
		if f.ID != id && f.ParentID == parentID && strings.EqualFold(f.Name, name) {
			return &Error{Code: "conflict", Status: 409, Message: "a folder with this name already exists here",
				Fields: map[string]string{"name": "a folder with this name already exists here"}}
		}
	}
	return nil
}

func (s *Service) CreateMediaFolder(ctx context.Context, p Principal, in MediaFolderInput) (MediaFolder, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return MediaFolder{}, err
	}
	name, err := cleanFolderName(deref(in.Name))
	if err != nil {
		return MediaFolder{}, err
	}
	parent := deref(in.ParentID)
	var out MediaFolder
	err = s.tx(ctx, func(q *db.Queries) error {
		folders, err := q.ListMediaFolders(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if len(folders) >= maxMediaFolders {
			return invalid("name", fmt.Sprintf("at most %d folders", maxMediaFolders))
		}
		if err := checkFolderPlace(folders, "", parent, name); err != nil {
			return err
		}
		now := s.now()
		f := db.MediaFolder{ID: newID("mfd"), WorkspaceID: p.WorkspaceID, ParentID: parent, Name: name,
			Position: int64(len(folders)), CreatedAt: now, UpdatedAt: now}
		if err := q.CreateMediaFolder(ctx, db.CreateMediaFolderParams{ID: f.ID, WorkspaceID: f.WorkspaceID,
			ParentID: f.ParentID, Name: f.Name, Position: f.Position, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		out = toMediaFolder(f, 0)
		return nil
	})
	return out, err
}

// UpdateMediaFolder renames a folder and/or moves it under another one.
func (s *Service) UpdateMediaFolder(ctx context.Context, p Principal, id string, in MediaFolderInput) (MediaFolder, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return MediaFolder{}, err
	}
	var out MediaFolder
	err := s.tx(ctx, func(q *db.Queries) error {
		f, err := q.GetMediaFolder(ctx, db.GetMediaFolderParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return notFound("folder")
		}
		name, parent := f.Name, f.ParentID
		if in.Name != nil {
			if name, err = cleanFolderName(*in.Name); err != nil {
				return err
			}
		}
		if in.ParentID != nil {
			parent = *in.ParentID
		}
		folders, err := q.ListMediaFolders(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if err := checkFolderPlace(folders, id, parent, name); err != nil {
			return err
		}
		if _, err := q.UpdateMediaFolder(ctx, db.UpdateMediaFolderParams{ParentID: parent, Name: name, UpdatedAt: s.now(),
			WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		f.Name, f.ParentID = name, parent
		out = toMediaFolder(f, 0)
		return nil
	})
	return out, err
}

// DeleteMediaFolder removes a folder; its files and subfolders move up to its parent. No file is deleted.
func (s *Service) DeleteMediaFolder(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeMediaWrite); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		f, err := q.GetMediaFolder(ctx, db.GetMediaFolderParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return notFound("folder")
		}
		// Subfolders moving up could clash with a sibling's name; they keep their names (names are a hint,
		// the uniqueness check runs on create, rename and move).
		if err := q.ReparentMediaFolders(ctx, db.ReparentMediaFoldersParams{ToParent: f.ParentID, UpdatedAt: s.now(),
			WorkspaceID: p.WorkspaceID, FromParent: id}); err != nil {
			return err
		}
		if err := q.MoveFolderMedia(ctx, db.MoveFolderMediaParams{ToFolder: f.ParentID, WorkspaceID: p.WorkspaceID,
			FromFolder: id}); err != nil {
			return err
		}
		_, err = q.DeleteMediaFolder(ctx, db.DeleteMediaFolderParams{WorkspaceID: p.WorkspaceID, ID: id})
		return err
	})
}

// checkMediaFolder: "" (the top level) or a folder of this workspace.
func (s *Service) checkMediaFolder(ctx context.Context, q *db.Queries, workspaceID, folderID string) error {
	if folderID == "" {
		return nil
	}
	if _, err := q.GetMediaFolder(ctx, db.GetMediaFolderParams{WorkspaceID: workspaceID, ID: folderID}); err != nil {
		return invalid("folder_id", "unknown folder")
	}
	return nil
}

// MediaBulk moves files to a folder and/or adds and removes tags, up to 200 files at a time.
type MediaBulk struct {
	IDs        []string `json:"ids"`
	FolderID   *string  `json:"folder_id"` // "" = the top level; nil leaves the folder alone
	AddTags    []string `json:"add_tags"`
	RemoveTags []string `json:"remove_tags"`
}

type MediaBulkResult struct {
	Updated []string `json:"updated"`
	Missing []string `json:"missing"`
}

func (s *Service) BulkUpdateMedia(ctx context.Context, p Principal, in MediaBulk) (MediaBulkResult, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return MediaBulkResult{}, err
	}
	if len(in.IDs) == 0 || len(in.IDs) > maxMediaBulk {
		return MediaBulkResult{}, invalid("ids", fmt.Sprintf("1 to %d media ids", maxMediaBulk))
	}
	add, err := normalizeMediaTags(in.AddTags)
	if err != nil {
		return MediaBulkResult{}, invalid("add_tags", err.(*Error).Message)
	}
	remove, err := normalizeMediaTags(in.RemoveTags)
	if err != nil {
		return MediaBulkResult{}, invalid("remove_tags", err.(*Error).Message)
	}
	if in.FolderID == nil && len(add) == 0 && len(remove) == 0 {
		return MediaBulkResult{}, invalid("folder_id", "give a folder, tags to add or tags to remove")
	}
	out := MediaBulkResult{Updated: []string{}, Missing: []string{}}
	err = s.tx(ctx, func(q *db.Queries) error {
		if in.FolderID != nil {
			if err := s.checkMediaFolder(ctx, q, p.WorkspaceID, *in.FolderID); err != nil {
				return err
			}
		}
		for _, id := range in.IDs {
			m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
			if err != nil {
				out.Missing = append(out.Missing, id)
				continue
			}
			if in.FolderID != nil {
				if _, err := q.SetMediaFolder(ctx, db.SetMediaFolderParams{FolderID: *in.FolderID,
					WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
					return err
				}
			}
			if len(add) > 0 || len(remove) > 0 {
				tags := parseMediaTags(m.Tags)
				tags = slices.DeleteFunc(tags, func(t string) bool { return slices.Contains(remove, t) })
				for _, t := range add {
					if !slices.Contains(tags, t) {
						tags = append(tags, t)
					}
				}
				if len(tags) > maxTags {
					return invalid("add_tags", fmt.Sprintf("a file has at most %d tags", maxTags))
				}
				if _, err := q.SetMediaTags(ctx, db.SetMediaTagsParams{Tags: mustJSON(tags),
					WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
					return err
				}
			}
			out.Updated = append(out.Updated, id)
		}
		return nil
	})
	return out, err
}
