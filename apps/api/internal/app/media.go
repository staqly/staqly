package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// Allowed upload types, keyed by sniffed MIME type: the library holds what posts carry, pictures, videos and
// audio. SVG and HTML are excluded on purpose: served inline they can run script. Files of types accepted
// before (documents, archives) stay readable; new ones are refused.
var allowedMedia = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/avif": ".avif",
	"video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov",
	"audio/mpeg": ".mp3", "audio/wave": ".wav", "audio/ogg": ".ogg", "audio/mp4": ".m4a",
}

// acceptedMedia is the upload error's list of what is accepted.
const acceptedMedia = "a picture (PNG, JPEG, GIF, WebP, AVIF), a video (MP4, WebM, MOV) or audio (MP3, WAV, OGG, M4A)"

// sniff detects the type from content; declared types/extensions are only
// trusted to refine generic text or containers the stdlib can't tell apart.
func sniff(head []byte, filename string) string {
	ct, _, _ := strings.Cut(http.DetectContentType(head), ";")
	ext := strings.ToLower(filepath.Ext(filename))
	switch {
	case ct == "application/ogg":
		return "audio/ogg"
	// MP3 without an ID3 tag starts with an MPEG audio frame sync (11 set bits); only trusted with the extension.
	case ct == "application/octet-stream" && ext == ".mp3" && len(head) > 1 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "audio/mpeg"
	case ct == "application/octet-stream" && len(head) > 12 && string(head[4:8]) == "ftyp":
		switch {
		case string(head[8:10]) == "qt":
			return "video/quicktime"
		case string(head[8:12]) == "avif":
			return "image/avif"
		case string(head[8:12]) == "M4A ":
			return "audio/mp4"
		}
		return "video/mp4"
	}
	return ct
}

type MediaRef struct {
	ID           string `json:"id"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	OriginalName string `json:"original_name"`
	URL          string `json:"url"`
}

type Media struct {
	MediaRef
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"created_at"`
	// Alt text describes a picture for people who can't see it; platforms that take it receive it.
	AltText string `json:"alt_text"`
	// Pixels and, for a video, its length; 0 when unknown.
	Width      int64 `json:"width"`
	Height     int64 `json:"height"`
	DurationMs int64 `json:"duration_ms"`
	// The library folder the file sits in ("" = the top level) and its tags.
	FolderID string   `json:"folder_id"`
	Tags     []string `json:"tags"`
	// A file in Deleted: when it went there and when it is deleted for good.
	DeletedAt *string `json:"deleted_at,omitempty"`
	PurgeAt   *string `json:"purge_at,omitempty"`
	// DuplicateOf is the oldest other file with the same content (in the library listing, upload and details).
	DuplicateOf string `json:"duplicate_of,omitempty"`
}

func (s *Service) toMedia(m db.Medium) Media {
	out := Media{MediaRef: *s.mediaRef(m), SHA256: m.Sha256, CreatedAt: m.CreatedAt, AltText: m.AltText,
		Width: m.Width, Height: m.Height, DurationMs: m.DurationMs, FolderID: m.FolderID, Tags: parseMediaTags(m.Tags)}
	if m.DeletedAt.Valid {
		out.DeletedAt = &m.DeletedAt.String
		if t, err := time.Parse(timeFormat, m.DeletedAt.String); err == nil {
			purge := formatTime(t.Add(MediaTrashDays * 24 * time.Hour))
			out.PurgeAt = &purge
		}
	}
	return out
}

// withTwin adds the oldest identical file, if any.
func (s *Service) withTwin(ctx context.Context, m db.Medium, out Media) Media {
	if id, err := s.Q.OldestTwin(ctx, db.OldestTwinParams{WorkspaceID: m.WorkspaceID, Sha256: m.Sha256, ID: m.ID}); err == nil {
		out.DuplicateOf = id
	}
	return out
}

func (s *Service) mediaRef(m db.Medium) *MediaRef {
	return &MediaRef{ID: m.ID, MimeType: m.MimeType, SizeBytes: m.SizeBytes, OriginalName: m.OriginalName,
		URL: strings.TrimRight(s.Cfg.PublicURL, "/") + "/v1/media/" + m.ID}
}

// workspaceKey is the folder of everything a workspace keeps in the store (disk or bucket): deleting or measuring
// a workspace's files is one prefix.
func workspaceKey(workspaceID string) string { return "workspaces/" + workspaceID + "/" }

// mediaKey is where an upload lives in the store: "workspaces/<id>/media/abcdef.jpg". Both parts are
// server-generated (storage names are [a-z2-7]+.ext), so no traversal is possible.
func mediaKey(workspaceID, storageName string) string {
	return workspaceKey(workspaceID) + "media/" + storageName
}

// signedMediaTTL is how long a platform may fetch a file from S3 after the publish job hands it the link.
const signedMediaTTL = 2 * time.Hour

// openMediaFile opens an upload from the store; a missing file is ErrNotExist.
func (s *Service) openMediaFile(ctx context.Context, workspaceID, storageName string) (io.ReadSeekCloser, error) {
	return s.Cfg.Blob.Open(ctx, mediaKey(workspaceID, storageName))
}

func cleanOriginalName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == "" {
		name = "upload"
	}
	if len(name) > 200 {
		// Cut on a character boundary and keep the extension: a byte cut split é in two and lost ".png".
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		base := strings.TrimSuffix(name, ext)
		for len(base)+len(ext) > 200 {
			_, size := utf8.DecodeLastRuneInString(base)
			base = base[:len(base)-size]
		}
		name = base + ext
	}
	return name
}

// UploadMedia validates size and type, then keeps the file in the store (disk or S3).
func (s *Service) UploadMedia(ctx context.Context, p Principal, filename string, r io.Reader) (Media, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return Media{}, err
	}
	br := bufio.NewReaderSize(r, 512)
	head, _ := br.Peek(512)
	if len(head) == 0 {
		return Media{}, invalid("file", "is empty")
	}
	mime := sniff(head, filename)
	ext, ok := allowedMedia[mime]
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".svg", ".svgz", ".html", ".htm", ".xhtml", ".xml", ".js", ".mjs":
		ok = false // active content even if it sniffs as text
	}
	if !ok {
		return Media{}, &Error{Code: "unsupported_media_type", Status: 415, Message: "file type " + mime + " is not allowed: upload " + acceptedMedia}
	}
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Media{}, err
	}
	maxBytes := s.Cfg.MaxUploadBytes
	if l := plan.Limits.MaxUploadBytes; l != Unlimited && l < maxBytes {
		maxBytes = l
	}
	used, err := s.Q.StorageUsed(ctx, p.WorkspaceID)
	if err != nil {
		return Media{}, err
	}
	id := newID("med")
	storage := randomString(16) + ext
	// Staged on local disk first: size, hash and limits are known before anything reaches the store.
	if err := os.MkdirAll(s.Cfg.Blob.TempDir(), 0o750); err != nil {
		return Media{}, err
	}
	f, err := os.CreateTemp(s.Cfg.Blob.TempDir(), ".upload-*")
	if err != nil {
		return Media{}, err
	}
	path := f.Name()
	defer os.Remove(path) // no-op once the store moved it into place
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(br, maxBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
	case n > maxBytes && maxBytes < s.Cfg.MaxUploadBytes:
		err = limitErr(plan, LimitMaxUpload, maxBytes, n)
	case n > maxBytes:
		err = &Error{Code: "too_large", Status: 413, Message: "file exceeds upload limit"}
	case plan.Limits.StorageBytes != Unlimited && used+n > plan.Limits.StorageBytes:
		// Refused before the store sees the file; checked again with the insert below (concurrent uploads).
		err = limitErr(plan, LimitStorage, plan.Limits.StorageBytes, used)
	}
	if err != nil {
		return Media{}, err
	}
	width, height := imageSize(path, mime)
	staged, err := os.Open(path)
	if err != nil {
		return Media{}, err
	}
	err = s.Cfg.Blob.Put(ctx, mediaKey(p.WorkspaceID, storage), staged, n, mime)
	staged.Close()
	if err != nil {
		return Media{}, fmt.Errorf("store media file: %w", err)
	}
	row := db.Medium{ID: id, WorkspaceID: p.WorkspaceID, StorageName: storage, OriginalName: cleanOriginalName(filename),
		MimeType: mime, SizeBytes: n, Sha256: hex.EncodeToString(h.Sum(nil)), CreatedAt: s.now(), Width: width, Height: height}
	if err := s.tx(ctx, func(q *db.Queries) error {
		if plan.Limits.StorageBytes != Unlimited {
			used, err := q.StorageUsed(ctx, p.WorkspaceID)
			if err != nil {
				return err
			}
			if used+n > plan.Limits.StorageBytes {
				return limitErr(plan, LimitStorage, plan.Limits.StorageBytes, used)
			}
		}
		return q.CreateMedia(ctx, db.CreateMediaParams{ID: row.ID, WorkspaceID: row.WorkspaceID, StorageName: storage,
			OriginalName: row.OriginalName, MimeType: mime, SizeBytes: n, Sha256: row.Sha256, CreatedAt: row.CreatedAt,
			Width: width, Height: height})
	}); err != nil {
		_ = s.Cfg.Blob.Delete(ctx, mediaKey(p.WorkspaceID, storage))
		return Media{}, err
	}
	return s.withTwin(ctx, row, s.toMedia(row)), nil
}

// OpenMedia returns the file for an authorized reader (looked up in the workspace first, never by key).
func (s *Service) OpenMedia(ctx context.Context, p Principal, id string) (io.ReadSeekCloser, db.Medium, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return nil, db.Medium{}, err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return nil, m, notFound("media")
	}
	f, err := s.openMediaFile(ctx, m.WorkspaceID, m.StorageName)
	if errors.Is(err, blob.ErrNotExist) {
		return nil, m, notFound("media file")
	}
	return f, m, err
}

// DiscardUpload removes media the caller just uploaded for a post that was
// then not created (multipart create failed), so failed requests don't leak
// storage quota. It needs only posts:write, like the upload itself, and
// deletes nothing a post references: deleting media in use is DeleteMedia
// (posts:delete).
func (s *Service) DiscardUpload(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeMediaWrite); err != nil {
		return err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return notFound("media")
	}
	n, err := s.W.DeleteUnreferencedMedia(ctx, db.DeleteUnreferencedMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil || n == 0 {
		return err
	}
	if err := s.removeMediaFiles(ctx, m.WorkspaceID, m.StorageName); err != nil {
		s.logger(ctx).Warn("media file not removed", "media_id", id, "err", err)
	}
	return nil
}

// DeleteMedia removes the record (posts referencing it lose that file) and the file.
func (s *Service) DeleteMedia(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopePostsDelete); err != nil {
		return err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return notFound("media")
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		// As for the trash: a scheduled or publishing post keeps its files, or it would go out without them.
		if n, err := upcomingUses(ctx, q, p.WorkspaceID, id); err != nil {
			return err
		} else if n > 0 {
			return conflict(fmt.Sprintf("%s is used by %d scheduled %s; remove it from %s first", m.OriginalName, n,
				plural(n, "post", "posts"), plural(n, "that post", "those posts")))
		}
		if _, err := q.DeleteMedia(ctx, db.DeleteMediaParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		return s.stripMediaFromPosts(ctx, q, p.WorkspaceID, id)
	})
	if err != nil {
		return err
	}
	if err := s.removeMediaFiles(ctx, m.WorkspaceID, m.StorageName); err != nil {
		s.logger(ctx).Warn("media file not removed", "media_id", id, "err", err)
	}
	return nil
}

// stripMediaFromPosts takes a deleted file out of every post: its files and the parts of its thread (a part
// keeps its text). Runs in the caller's transaction.
func (s *Service) stripMediaFromPosts(ctx context.Context, q *db.Queries, ws, id string) error {
	now := s.now()
	if err := q.StripMediaFromPosts(ctx, db.StripMediaFromPostsParams{MediaID: id, UpdatedAt: now, WorkspaceID: ws}); err != nil {
		return err
	}
	rows, err := q.PostThreadsWithMedia(ctx, db.PostThreadsWithMediaParams{WorkspaceID: ws, Quoted: `"` + id + `"`})
	if err != nil {
		return err
	}
	for _, r := range rows {
		parts := parseThread(r.Thread)
		for i := range parts {
			parts[i].Media = slices.DeleteFunc(parts[i].Media, func(m string) bool { return m == id })
		}
		if err := q.SetPostThreadMedia(ctx, db.SetPostThreadMediaParams{Thread: mustJSON(parts), UpdatedAt: now,
			WorkspaceID: ws, ID: r.ID}); err != nil {
			return err
		}
	}
	return nil
}

// imageSize reads a picture's pixel size from its header (no full decode); 0, 0 for anything else.
func imageSize(path, mime string) (int64, int64) {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return 0, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(bufio.NewReader(f))
	if err != nil {
		return 0, 0
	}
	return int64(cfg.Width), int64(cfg.Height)
}

// ---- the media library ----

// LibraryItem is a library entry with the number of posts that use it.
type LibraryItem struct {
	Media
	Uses int64 `json:"uses"`
}

type MediaPage struct {
	Data []LibraryItem `json:"data"`
	// NextOffset is where the next page starts; absent on the last page.
	NextOffset *int `json:"next_offset,omitempty"`
}

type MediaFilter struct {
	Query, Kind, Used, Sort string
	// Folder: a folder id, "root" for the top level, "" for every folder. Tag: files carrying it.
	Folder, Tag string
	// Deleted lists the files in Deleted; Duplicates only files with an identical twin, grouped by content.
	Deleted, Duplicates bool
	Offset, Limit       int
}

// ListMedia is the media library: filter by name, type and use, sorted, a page at a time.
func (s *Service) ListMedia(ctx context.Context, p Principal, f MediaFilter) (MediaPage, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return MediaPage{}, err
	}
	if f.Sort != "" && !db.ValidMediaSort(f.Sort) {
		return MediaPage{}, invalid("sort", "must be created_desc, created_asc, size_desc or name_asc")
	}
	if f.Kind != "" && !slices.Contains(db.MediaKinds, f.Kind) {
		return MediaPage{}, invalid("kind", "must be image, video or other")
	}
	if f.Used != "" && f.Used != "used" && f.Used != "unused" {
		return MediaPage{}, invalid("used", "must be used or unused")
	}
	if f.Limit < 0 || f.Limit > 200 {
		return MediaPage{}, invalid("limit", "must be 1 to 200")
	}
	if f.Limit == 0 {
		f.Limit = 60
	}
	if f.Tag != "" {
		tags, err := normalizeMediaTags([]string{f.Tag})
		if err != nil || len(tags) != 1 {
			return MediaPage{}, invalid("tag", "not a valid tag")
		}
		f.Tag = tags[0]
	}
	rows, more, err := s.Q.SearchMedia(ctx, db.MediaSearch{WorkspaceID: p.WorkspaceID, Query: f.Query, Kind: f.Kind,
		Used: f.Used, Folder: f.Folder, Tag: f.Tag, Deleted: f.Deleted, Duplicates: f.Duplicates, Sort: f.Sort,
		Offset: f.Offset, Limit: f.Limit})
	if err != nil {
		return MediaPage{}, err
	}
	out := MediaPage{Data: make([]LibraryItem, 0, len(rows))}
	for _, r := range rows {
		m := s.toMedia(r.Medium)
		m.DuplicateOf = r.DuplicateOf.String
		out.Data = append(out.Data, LibraryItem{Media: m, Uses: r.Uses})
	}
	if more {
		next := max(f.Offset, 0) + len(rows)
		out.NextOffset = &next
	}
	return out, nil
}

// GetMediaInfo is one library entry (the file itself is GET /media/{id}).
func (s *Service) GetMediaInfo(ctx context.Context, p Principal, id string) (Media, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return Media{}, err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return Media{}, notFound("media")
	}
	return s.withTwin(ctx, m, s.toMedia(m)), nil
}

// MediaPatch changes a file's name, alt text, or (from the browser that measured it) a video's size and length.
type MediaPatch struct {
	Name       *string `json:"name"`
	AltText    *string `json:"alt_text"`
	Width      *int64  `json:"width"`
	Height     *int64  `json:"height"`
	DurationMs *int64  `json:"duration_ms"`
	// FolderID moves the file ("" = the top level); Tags replaces its tags.
	FolderID *string  `json:"folder_id"`
	Tags     []string `json:"tags"`
}

func (s *Service) UpdateMedia(ctx context.Context, p Principal, id string, in MediaPatch) (Media, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return Media{}, err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return Media{}, notFound("media")
	}
	name, alt := m.OriginalName, m.AltText
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return Media{}, invalid("name", "is required")
		}
		name = cleanOriginalName(strings.TrimSpace(*in.Name))
	}
	if in.AltText != nil {
		alt = strings.TrimSpace(*in.AltText)
		if len([]rune(alt)) > 1500 {
			return Media{}, invalid("alt_text", "at most 1500 characters")
		}
	}
	var tags []string
	if in.Tags != nil {
		if tags, err = normalizeMediaTags(in.Tags); err != nil {
			return Media{}, err
		}
	}
	w, h, d := m.Width, m.Height, m.DurationMs
	for _, v := range []struct {
		field string
		in    *int64
		out   *int64
		max   int64
	}{{"width", in.Width, &w, 100_000}, {"height", in.Height, &h, 100_000}, {"duration_ms", in.DurationMs, &d, 86_400_000}} {
		if v.in == nil {
			continue
		}
		if *v.in < 0 || *v.in > v.max {
			return Media{}, invalid(v.field, fmt.Sprintf("must be 0 to %d", v.max))
		}
		*v.out = *v.in
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.UpdateMediaInfo(ctx, db.UpdateMediaInfoParams{OriginalName: name, AltText: alt,
			WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if _, err := q.SetMediaSize(ctx, db.SetMediaSizeParams{Width: w, Height: h, DurationMs: d, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if in.FolderID != nil {
			if err := s.checkMediaFolder(ctx, q, p.WorkspaceID, *in.FolderID); err != nil {
				return err
			}
			if _, err := q.SetMediaFolder(ctx, db.SetMediaFolderParams{FolderID: *in.FolderID, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
				return err
			}
			m.FolderID = *in.FolderID
		}
		if tags != nil {
			if _, err := q.SetMediaTags(ctx, db.SetMediaTagsParams{Tags: mustJSON(tags), WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
				return err
			}
			m.Tags = mustJSON(tags)
		}
		return nil
	})
	if err != nil {
		return Media{}, err
	}
	m.OriginalName, m.AltText, m.Width, m.Height, m.DurationMs = name, alt, w, h, d
	return s.toMedia(m), nil
}

// MediaUse is a post that carries the file (in its files or in a comment).
type MediaUse struct {
	PostID       string  `json:"post_id"`
	CollectionID string  `json:"collection_id"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	PublishAt    *string `json:"publish_at,omitempty"`
}

// MediaUsage lists up to 50 posts that use the file, newest first.
func (s *Service) MediaUsage(ctx context.Context, p Principal, id string) ([]MediaUse, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return nil, err
	}
	if !p.Has(ScopePostsRead) {
		return []MediaUse{}, nil // a media-only key sees files, not posts
	}
	if _, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
		return nil, notFound("media")
	}
	rows, err := s.Q.MediaUsage(ctx, db.MediaUsageParams{WorkspaceID: p.WorkspaceID, Quoted: `"` + id + `"`})
	if err != nil {
		return nil, err
	}
	out := make([]MediaUse, 0, len(rows))
	for _, r := range rows {
		u := MediaUse{PostID: r.ID, CollectionID: r.CollectionID, Title: mediaUseTitle(r.Content), Status: r.Status}
		if r.PublishAt.Valid {
			u.PublishAt = &r.PublishAt.String
		}
		out = append(out, u)
	}
	return out, nil
}

// mediaUseTitle is a post's first line, short.
func mediaUseTitle(content string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	if r := []rune(line); len(r) > 80 {
		return string(r[:79]) + "..."
	}
	return line
}

// DeleteMediaMany moves several files to Deleted, or with permanent deletes them for good (posts:delete, like
// one); unknown ids are reported, not fatal. Moving to Deleted checks every file first: one used by a scheduled
// post refuses the whole request (409) before anything moves.
func (s *Service) DeleteMediaMany(ctx context.Context, p Principal, ids []string, permanent bool) (deleted, missing []string, err error) {
	if err := p.require(ScopePostsDelete); err != nil {
		return nil, nil, err
	}
	ids = uniqueIDs(ids)
	if len(ids) == 0 || len(ids) > 200 {
		return nil, nil, invalid("ids", "1 to 200 media ids")
	}
	deleted, missing = []string{}, []string{}
	del := s.TrashMedia
	if permanent {
		del = s.DeleteMedia
	} else {
		for _, id := range ids {
			m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
			if err != nil || m.DeletedAt.Valid {
				continue
			}
			if n, err := upcomingUses(ctx, s.Q, p.WorkspaceID, id); err != nil {
				return nil, nil, err
			} else if n > 0 {
				return nil, nil, conflict(fmt.Sprintf("%s is used by %d scheduled %s; remove it from %s first",
					m.OriginalName, n, plural(n, "post", "posts"), plural(n, "that post", "those posts")))
			}
		}
	}
	for _, id := range ids {
		switch err := del(ctx, p, id); {
		case err == nil:
			deleted = append(deleted, id)
		case isNotFound(err):
			missing = append(missing, id)
		default:
			return deleted, missing, err
		}
	}
	return deleted, missing, nil
}
