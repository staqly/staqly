package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Allowed upload types, keyed by sniffed MIME type. SVG and HTML are excluded
// on purpose: served inline they can run script.
var allowedMedia = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/avif": ".avif",
	"video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov",
	"application/pdf": ".pdf", "text/plain": ".txt", "text/csv": ".csv", "text/markdown": ".md",
	"application/json": ".json", "application/zip": ".zip",
	"audio/mpeg": ".mp3", "audio/wave": ".wav", "audio/ogg": ".ogg",
}

// sniff detects the type from content; declared types/extensions are only
// trusted to refine generic text or containers the stdlib can't tell apart.
func sniff(head []byte, filename string) string {
	ct, _, _ := strings.Cut(http.DetectContentType(head), ";")
	ext := strings.ToLower(filepath.Ext(filename))
	switch {
	case ct == "text/plain" && ext == ".csv":
		return "text/csv"
	case ct == "text/plain" && ext == ".md":
		return "text/markdown"
	case ct == "text/plain" && ext == ".json":
		return "application/json"
	case ct == "application/octet-stream" && len(head) > 12 && string(head[4:8]) == "ftyp":
		if string(head[8:10]) == "qt" {
			return "video/quicktime"
		}
		if string(head[8:12]) == "avif" {
			return "image/avif"
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
}

func (s *Service) toMedia(m db.Medium) Media {
	return Media{MediaRef: *s.mediaRef(m), SHA256: m.Sha256, CreatedAt: m.CreatedAt}
}

func (s *Service) mediaRef(m db.Medium) *MediaRef {
	return &MediaRef{ID: m.ID, MimeType: m.MimeType, SizeBytes: m.SizeBytes, OriginalName: m.OriginalName,
		URL: strings.TrimRight(s.Cfg.PublicURL, "/") + "/v1/media/" + m.ID}
}

func (s *Service) mediaPath(storageName string) string {
	// storageName is server-generated ([a-z2-7]+.ext), so no traversal is possible.
	return filepath.Join(s.Cfg.UploadDir, storageName[:2], storageName)
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
		name = name[:200]
	}
	return name
}

// UploadMedia stores r on local disk after validating size and type.
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
		return Media{}, &Error{Code: "unsupported_media_type", Status: 415, Message: "file type " + mime + " is not allowed"}
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
	path := s.mediaPath(storage)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return Media{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return Media{}, err
	}
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
		// ponytail: check-then-insert; concurrent uploads can overshoot the quota by one file.
		err = limitErr(plan, LimitStorage, plan.Limits.StorageBytes, used)
	}
	if err != nil {
		os.Remove(path)
		return Media{}, err
	}
	row := db.Medium{ID: id, WorkspaceID: p.WorkspaceID, StorageName: storage, OriginalName: cleanOriginalName(filename),
		MimeType: mime, SizeBytes: n, Sha256: hex.EncodeToString(h.Sum(nil)), CreatedAt: s.now()}
	if err := s.W.CreateMedia(ctx, db.CreateMediaParams{ID: row.ID, WorkspaceID: row.WorkspaceID, StorageName: storage,
		OriginalName: row.OriginalName, MimeType: mime, SizeBytes: n, Sha256: row.Sha256, CreatedAt: row.CreatedAt}); err != nil {
		os.Remove(path)
		return Media{}, err
	}
	return Media{MediaRef: *s.mediaRef(row), SHA256: row.Sha256, CreatedAt: row.CreatedAt}, nil
}

// OpenMedia returns the file for an authorized reader.
func (s *Service) OpenMedia(ctx context.Context, p Principal, id string) (*os.File, db.Medium, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return nil, db.Medium{}, err
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return nil, m, notFound("media")
	}
	f, err := os.Open(s.mediaPath(m.StorageName))
	if errors.Is(err, os.ErrNotExist) {
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
	if err := s.removeMediaFiles(m.StorageName); err != nil {
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
		if _, err := q.DeleteMedia(ctx, db.DeleteMediaParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		return q.StripMediaFromPosts(ctx, db.StripMediaFromPostsParams{MediaID: id, UpdatedAt: s.now(), WorkspaceID: p.WorkspaceID})
	})
	if err != nil {
		return err
	}
	if err := s.removeMediaFiles(m.StorageName); err != nil {
		s.logger(ctx).Warn("media file not removed", "media_id", id, "err", err)
	}
	return nil
}
