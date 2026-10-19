package db

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// The media library listing has optional filters and several sort orders, so like post search it is written by
// hand. Only whitelisted SQL fragments are concatenated; all values are bound parameters.

// mediaUses counts the posts that carry the media id in their files or in a comment of their thread.
const mediaUses = `(SELECT COUNT(*) FROM posts p WHERE p.workspace_id = m.workspace_id
	AND (instr(p.media, '"' || m.id || '"') > 0 OR instr(p.thread, '"' || m.id || '"') > 0))`

// mediaTwin is the oldest other file (not deleted) with the same content, or NULL.
const mediaTwin = `(SELECT d.id FROM media d WHERE d.workspace_id = m.workspace_id AND d.sha256 = m.sha256
	AND d.id <> m.id AND d.deleted_at IS NULL ORDER BY d.created_at, d.id LIMIT 1)`

var mediaSorts = map[string]string{
	"created_desc": "m.created_at DESC, m.id DESC",
	"created_asc":  "m.created_at ASC, m.id ASC",
	"size_desc":    "m.size_bytes DESC, m.id DESC",
	"name_asc":     "m.original_name COLLATE NOCASE ASC, m.id ASC",
}

func ValidMediaSort(s string) bool { _, ok := mediaSorts[s]; return ok }

// MediaKinds are the type filters: picture, video, anything else.
var MediaKinds = []string{"image", "video", "audio"}

type MediaSearch struct {
	WorkspaceID string
	Query       string // substring of the file name, case-insensitive
	Kind        string // image | video | other | ""
	Used        string // "used" | "unused" | ""
	Folder      string // a folder id, "root" for the top level, "" for every folder
	Tag         string // files carrying this tag; "" for any
	Deleted     bool   // the files in Deleted instead of the library
	Duplicates  bool   // only files with an identical twin, grouped by content (oldest first)
	Sort        string
	Offset      int
	Limit       int
}

// MediaRow is one library entry with the number of posts that use it and its oldest identical twin.
type MediaRow struct {
	Medium
	Uses        int64
	DuplicateOf sql.NullString
}

// SearchMedia returns one page and whether there is a next one.
func (q *Queries) SearchMedia(ctx context.Context, s MediaSearch) ([]MediaRow, bool, error) {
	order, ok := mediaSorts[s.Sort]
	if !ok {
		order = mediaSorts["created_desc"]
	}
	var where []string
	var args []any
	add := func(cond string, vals ...any) { where = append(where, cond); args = append(args, vals...) }
	add("m.workspace_id = ?", s.WorkspaceID)
	if s.Deleted {
		add("m.deleted_at IS NOT NULL")
	} else {
		add("m.deleted_at IS NULL")
	}
	if s.Duplicates {
		add(mediaTwin + " IS NOT NULL")
		order = "m.sha256, m.created_at, m.id"
	}
	switch s.Kind {
	case "image":
		add("m.mime_type LIKE 'image/%'")
	case "video":
		add("m.mime_type LIKE 'video/%'")
	case "audio":
		add("m.mime_type LIKE 'audio/%'")
	}
	switch s.Used {
	case "used":
		add(mediaUses + " > 0")
	case "unused":
		add(mediaUses + " = 0")
	}
	switch s.Folder {
	case "":
	case "root":
		add("m.folder_id = ''")
	default:
		add("m.folder_id = ?", s.Folder)
	}
	if s.Tag != "" {
		add("instr(m.tags, ?) > 0", `"`+strings.ReplaceAll(s.Tag, `"`, `\"`)+`"`)
	}
	if qs := strings.TrimSpace(s.Query); qs != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(qs)
		add(`m.original_name LIKE ? ESCAPE '\' COLLATE NOCASE`, "%"+esc+"%")
	}
	limit := s.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	query := `SELECT m.id, m.workspace_id, m.storage_name, m.original_name, m.mime_type, m.size_bytes, m.sha256,
		m.created_at, m.alt_text, m.width, m.height, m.duration_ms, m.folder_id, m.tags, m.deleted_at, ` + mediaUses + ` AS uses, ` + mediaTwin + ` AS duplicate_of
		FROM media m WHERE ` + strings.Join(where, " AND ") + " ORDER BY " + order +
		" LIMIT " + strconv.Itoa(limit+1) + " OFFSET " + strconv.Itoa(max(s.Offset, 0))
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []MediaRow
	for rows.Next() {
		var r MediaRow
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.StorageName, &r.OriginalName, &r.MimeType, &r.SizeBytes, &r.Sha256,
			&r.CreatedAt, &r.AltText, &r.Width, &r.Height, &r.DurationMs, &r.FolderID, &r.Tags, &r.DeletedAt, &r.Uses, &r.DuplicateOf); err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}
