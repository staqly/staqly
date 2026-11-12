package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// ReplaceMedia is the design editor's "Replace the original". A media id never changes content (thumbnails and
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
	edited, err := s.UploadMedia(ctx, p, filename, r, false)
	if err != nil {
		return Media{}, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		// Uploading can take minutes: metadata or the original itself may have changed meanwhile.
		old, err = q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) || err == nil && old.DeletedAt.Valid {
			return notFound("media")
		}
		if err != nil {
			return err
		}
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
	return s.getMediaInfo(ctx, p, edited.ID)
}

// MediaProject is an editable picture of the design editor: its design (JSON: page, texts, pictures, shapes) kept
// in the store, the library files it was made from and the file it last saved. Projects saved before 2026-10-25
// hold the former photo editor's .gpex files; they stay downloadable.
type MediaProject struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	SourceIDs []string `json:"source_ids"`
	OutputID  string   `json:"output_id"`
	// PageIDs are the files its pages saved as, in order (OutputID is the first); one for a single page.
	PageIDs   []string `json:"page_ids"`
	SizeBytes int64    `json:"size_bytes"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// MediaProjectInput is what the editor sends with a project file.
type MediaProjectInput struct {
	Name      string
	SourceIDs []string
	OutputID  string
	PageIDs   []string
}

// designFile: the part of an editor file the server checks (the editors read the rest). A design has pages; a
// timeline of the video and sound editor has a kind ("video": a frame size, picture clips, captions, sound clips;
// "audio": sound clips).
type designFile struct {
	Version, Width, Height float64
	Kind                   string
	Pages, Clips, Sounds   int // how many
}

func (d designFile) valid() bool {
	switch {
	case d.Version != 1:
		return false
	case d.Kind == "video":
		return d.Width > 0 && d.Height > 0 && d.Clips+d.Sounds > 0
	case d.Kind == "audio":
		return d.Sounds > 0
	default:
		return d.Kind == "" && d.Width > 0 && d.Height > 0 && d.Pages > 0
	}
}

// readDesignFile reads an editor file (the JSON object at its start) as it streams, keeping only what valid
// checks. It holds one small buffer however large the file or a text in it is: decoding a file whole took
// several times its size in memory, and files go up to the upload limit.
func readDesignFile(r io.Reader) (designFile, error) {
	s := designScan{bufio.NewReaderSize(r, 64<<10)}
	var d designFile
	if c, err := s.next(); err != nil || c != '{' {
		return d, errDesign
	}
	_, err := s.each('{', func(key string, c byte) error {
		var err error
		count := func(n *int) error {
			if c != '[' {
				return s.skip(c, 1)
			}
			*n, err = s.each('[', func(_ string, c byte) error { return s.skip(c, 2) })
			return err
		}
		number := func(v *float64) error {
			raw, err := s.scalar(c)
			if err != nil || raw == "null" {
				return err
			}
			*v, err = strconv.ParseFloat(raw, 64)
			return err
		}
		switch strings.ToLower(key) {
		case "version":
			return number(&d.Version)
		case "width":
			return number(&d.Width)
		case "height":
			return number(&d.Height)
		case "kind":
			if c != '"' { // null is no kind; anything else is no editor file
				if raw, err := s.scalar(c); err != nil || raw != "null" {
					return errDesign
				}
				return nil
			}
			d.Kind, err = s.str(16)
			return err
		case "pages":
			return count(&d.Pages)
		case "clips":
			return count(&d.Clips)
		case "sounds":
			return count(&d.Sounds)
		}
		return s.skip(c, 1)
	})
	return d, err
}

var errDesign = errors.New("not an editor project")

// designScan walks JSON byte by byte, checking its syntax.
type designScan struct{ r *bufio.Reader }

// next is the next byte that is not white space.
func (s designScan) next() (byte, error) {
	for {
		c, err := s.r.ReadByte()
		if err != nil || (c != ' ' && c != '\t' && c != '\n' && c != '\r') {
			return c, err
		}
	}
}

// str reads a string whose opening quote was read and answers its first keep bytes as written (an escape as a
// backslash: such a name is none the server knows).
func (s designScan) str(keep int) (string, error) {
	var out []byte
	for {
		c, err := s.r.ReadByte()
		switch {
		case err != nil:
			return "", err
		case c == '"':
			return string(out), nil
		case c < 0x20:
			return "", errDesign
		case c == '\\':
			e, err := s.r.ReadByte()
			if err != nil {
				return "", err
			}
			if e == 'u' {
				hex, err := s.r.Peek(4)
				if _, perr := strconv.ParseUint(string(hex), 16, 16); err != nil || perr != nil {
					return "", errDesign
				}
			} else if !strings.ContainsRune(`"\/bfnrt`, rune(e)) {
				return "", errDesign
			}
		}
		if len(out) < keep {
			out = append(out, c)
		}
	}
}

// scalar reads a number, true, false or null that starts with c, as written.
func (s designScan) scalar(c byte) (string, error) {
	out := []byte{c}
	for {
		b, err := s.r.Peek(1)
		if err != nil || !strings.ContainsRune("+-.0123456789eEaflnrstu", rune(b[0])) {
			break
		}
		out = append(out, b[0])
		_, _ = s.r.Discard(1)
		if len(out) > 64 {
			return "", errDesign
		}
	}
	if !json.Valid(out) {
		return "", errDesign
	}
	return string(out), nil
}

// each walks an object's members or an array's elements (open, { or [, was read): fn gets a member's name ("" in
// an array) and the first byte of its value, and reads that value. It answers how many there were.
func (s designScan) each(open byte, fn func(key string, c byte) error) (int, error) {
	end := open + 2 // } for {, ] for [
	n := 0
	c, err := s.next()
	if err != nil || c == end {
		return 0, err
	}
	for {
		key := ""
		if open == '{' {
			if c != '"' {
				return n, errDesign
			}
			if key, err = s.str(16); err != nil {
				return n, err
			}
			if c, err = s.next(); err != nil || c != ':' {
				return n, errDesign
			}
			if c, err = s.next(); err != nil {
				return n, err
			}
		}
		if err := fn(key, c); err != nil {
			return n, err
		}
		n++
		if c, err = s.next(); err != nil || c == end {
			return n, err
		}
		if c != ',' {
			return n, errDesign
		}
		if c, err = s.next(); err != nil {
			return n, err
		}
	}
}

// skip reads past the value that starts with c.
func (s designScan) skip(c byte, depth int) error {
	var err error
	switch c {
	case '"':
		_, err = s.str(0)
	case '{', '[':
		if depth > 500 { // far deeper than any editor nests
			return errDesign
		}
		_, err = s.each(c, func(_ string, c byte) error { return s.skip(c, depth+1) })
	default:
		_, err = s.scalar(c)
	}
	return err
}

// projectKey is where a design editor project lives in the store: "workspaces/<id>/projects/abcdef.json".
func projectKey(workspaceID, storageName string) string {
	return workspaceKey(workspaceID) + "projects/" + storageName
}

func toMediaProject(m db.MediaProject) MediaProject {
	out := MediaProject{ID: m.ID, Name: m.Name, OutputID: m.OutputID, SizeBytes: m.SizeBytes, CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt, SourceIDs: []string{}, PageIDs: []string{}}
	_ = json.Unmarshal([]byte(m.SourceIds), &out.SourceIDs)
	_ = json.Unmarshal([]byte(m.PageIds), &out.PageIDs)
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
	if len(in.PageIDs) > 20 {
		return invalid("page_ids", "at most 20 pages")
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
	for _, mid := range append(append(append([]string{}, in.SourceIDs...), in.PageIDs...), in.OutputID) {
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
	if plan.Limits.StorageBytes != Unlimited { // before the file is read again to check it
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
	if d, err := readDesignFile(f); err != nil || !d.valid() {
		f.Close()
		return MediaProject{}, &Error{Code: "unsupported_media_type", Status: 415, Message: "not an editor project (design JSON with pages, or a video or sound timeline, version 1)"}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return MediaProject{}, err
	}
	storage := randomString(16) + ".json"
	err = s.Cfg.Blob.Put(ctx, projectKey(p.WorkspaceID, storage), f, n, "application/json")
	f.Close()
	if err != nil {
		s.removeStoredFile(ctx, projectKey(p.WorkspaceID, storage))
		return MediaProject{}, fmt.Errorf("store project file: %w", err)
	}
	sources, _ := json.Marshal(nonNil(in.SourceIDs))
	pages, _ := json.Marshal(nonNil(in.PageIDs))
	now := s.now()
	replaced := "" // the file this version takes the place of, read in the transaction (a concurrent save may have moved it)
	if id == "" {
		id = newID("prj")
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		// The subscription may change while the file is being uploaded.
		plan, err := s.planFor(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		if limit := plan.Limits.MaxUploadBytes; limit != Unlimited && n > limit {
			return &Error{Code: "too_large", Status: 413, Message: "file exceeds the current plan's upload limit"}
		}
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
				SourceIds: string(sources), OutputID: in.OutputID, PageIds: string(pages), StorageName: storage, SizeBytes: n, CreatedBy: p.UserID,
				CreatedAt: now, UpdatedAt: now})
		}
		rows, err := q.UpdateMediaProject(ctx, db.UpdateMediaProjectParams{Name: in.Name, SourceIds: string(sources),
			OutputID: in.OutputID, PageIds: string(pages), StorageName: storage, SizeBytes: n, UpdatedAt: now, WorkspaceID: p.WorkspaceID, ID: id})
		if err == nil && rows == 0 {
			err = notFound("project")
		}
		return err
	})
	if err != nil {
		s.removeStoredFile(ctx, projectKey(p.WorkspaceID, storage))
		return MediaProject{}, err
	}
	if replaced != "" {
		s.removeStoredFile(ctx, projectKey(p.WorkspaceID, replaced))
	}
	s.trackAs(nil, p, "media_edited", "kind", "project")
	return s.getMediaProject(ctx, p, id)
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
	return s.getMediaProject(ctx, p, id)
}

// getMediaProject also returns a save's result under media:write.
func (s *Service) getMediaProject(ctx context.Context, p Principal, id string) (MediaProject, error) {
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
	var m db.MediaProject
	if err := s.tx(ctx, func(q *db.Queries) error {
		var err error
		m, err = q.GetMediaProject(ctx, db.GetMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("project")
		}
		if err != nil {
			return err
		}
		_, err = q.DeleteMediaProject(ctx, db.DeleteMediaProjectParams{WorkspaceID: p.WorkspaceID, ID: id})
		return err
	}); err != nil {
		return err
	}
	s.removeStoredFile(ctx, projectKey(m.WorkspaceID, m.StorageName))
	return nil
}
