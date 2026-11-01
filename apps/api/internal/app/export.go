package app

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

// ---- activity (event) export ----

// EventExportFilter selects events for GET /v1/events/export.
type EventExportFilter struct {
	Format               string // ndjson (default) | csv
	From, To             string // created_at range: From inclusive, To exclusive
	Type, TypePrefix     string
	ActorType, ActorID   string
	CollectionID, PostID string
	AfterSeq             int64 // only events after this seq (continues a capped MCP export)
}

// EventExport is a validated, ready-to-stream export. Each pages through the
// matching events in seq order, so memory use stays flat for any size.
type EventExport struct {
	Format string
	s      *Service
	params db.ExportEventsPageParams
}

const exportPageSize = 500

// PrepareEventExport checks scope and validates the filter before the caller
// commits to a streaming response.
func (s *Service) PrepareEventExport(ctx context.Context, p Principal, f EventExportFilter) (*EventExport, error) {
	if err := p.require(ScopeEventsRead); err != nil {
		return nil, err
	}
	if err := s.requireDataExport(ctx, p.WorkspaceID); err != nil {
		return nil, err
	}
	switch f.Format {
	case "":
		f.Format = "ndjson"
	case "ndjson", "csv":
	default:
		return nil, invalid("format", "must be ndjson or csv")
	}
	from, err := ParseTimeFilter("from", f.From)
	if err != nil {
		return nil, err
	}
	to, err := ParseTimeFilter("to", f.To)
	if err != nil {
		return nil, err
	}
	if f.ActorType != "" && f.ActorType != PrincipalSession && f.ActorType != PrincipalAPIKey &&
		f.ActorType != PrincipalSystem && f.ActorType != PrincipalLocal {
		return nil, invalid("actor_type", "must be session, api_key, local or system")
	}
	if f.AfterSeq < 0 {
		return nil, invalid("after_seq", "must not be negative")
	}
	for field, v := range map[string]string{"type": f.Type, "type_prefix": f.TypePrefix, "actor_id": f.ActorID,
		"collection_id": f.CollectionID, "post_id": f.PostID} {
		if utf8.RuneCountInString(v) > 200 {
			return nil, invalid(field, "must be at most 200 characters")
		}
	}
	audit := int64(0)
	if p.Has(ScopeAdmin) {
		audit = 1 // like ListEvents: api_key.*, session.*, recovery_key.*, device*.* only for admins
	}
	return &EventExport{Format: f.Format, s: s, params: db.ExportEventsPageParams{
		WorkspaceID: p.WorkspaceID, FromTs: from, ToTs: to, Type: optAny(f.Type), TypePrefix: optAny(f.TypePrefix),
		ActorType: optAny(f.ActorType), ActorID: optAny(f.ActorID), CollectionID: optAny(f.CollectionID),
		PostID: optAny(f.PostID), IncludeAudit: audit, Lim: exportPageSize, AfterSeq: f.AfterSeq,
	}}, nil
}

// Each calls fn for every matching event, oldest first.
func (x *EventExport) Each(ctx context.Context, fn func(Event) error) error {
	params := x.params
	for {
		rows, err := x.s.Q.ExportEventsPage(ctx, params)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := fn(toEvent(r)); err != nil {
				return err
			}
		}
		if len(rows) < int(params.Lim) {
			return nil
		}
		params.AfterSeq = rows[len(rows)-1].Seq
	}
}

// EventCSVHeader is the column order of the CSV export.
var EventCSVHeader = []string{"created_at", "id", "type", "actor_type", "actor_id", "actor_name", "collection_id", "post_id", "data"}

// EventCSVRecord renders an event as a CSV row. Cells are neutralised against
// spreadsheet formula injection (a leading =, +, -, @, tab or CR gets a ').
func EventCSVRecord(e Event) []string {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	rec := []string{e.CreatedAt, e.ID, e.Type, e.Actor.Type, e.Actor.ID, e.Actor.Name,
		deref(e.CollectionID), deref(e.PostID), string(e.Data)}
	for i, v := range rec {
		rec[i] = csvSafe(v)
	}
	return rec
}

func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// NewEventCSVWriter returns a CSV writer with CRLF line endings (RFC 4180).
func NewEventCSVWriter(w io.Writer) *csv.Writer {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	return cw
}

// ---- workspace export (GDPR data portability) ----
//
// Every plan can export its data. With data export in the plan (Plus and above, local
// installs) the ZIP carries the media files too; without it (Starter) the export
// is data only: the same JSON files, media referenced by id, name and type.

const exportTTL = 24 * time.Hour

type WorkspaceExport struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"` // pending | running | ready | failed | expired
	SizeBytes   int64   `json:"size_bytes"`
	Error       string  `json:"error,omitempty"`
	CreatedAt   string  `json:"created_at"`
	StartedAt   *string `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
	ExpiresAt   string  `json:"expires_at"`
	DownloadURL string  `json:"download_url,omitempty"`
	// IncludesMedia is false for a data-only export (Starter): no media/ files.
	IncludesMedia bool `json:"includes_media"`
}

func toWorkspaceExport(x db.WorkspaceExport) WorkspaceExport {
	out := WorkspaceExport{ID: x.ID, Status: x.Status, SizeBytes: x.SizeBytes, Error: x.Error, CreatedAt: x.CreatedAt,
		StartedAt: ptr(x.StartedAt), FinishedAt: ptr(x.FinishedAt), ExpiresAt: x.ExpiresAt, IncludesMedia: x.IncludesMedia != 0}
	if x.Status == "ready" {
		out.DownloadURL = "/v1/workspace/exports/" + x.ID + "/download"
	}
	return out
}

// ExportDir holds local ZIPs and stages cloud ZIPs before uploading them to private storage.
func (s *Service) ExportDir() string {
	return filepath.Join(s.Env("DATA_DIR", "./data"), "exports")
}

// export IDs are server-generated (exp_[a-z2-7]+), so the path is safe.
func (s *Service) exportPath(id string) string { return filepath.Join(s.ExportDir(), id+".zip") }

func (s *Service) exportsOnS3() bool {
	return s.Cfg.Mode == ModeCloud && s.Cfg.Blob.Name() == "s3"
}

func (s *Service) exportStore() blob.Store {
	if s.exportsOnS3() {
		return s.Cfg.Blob
	}
	return blob.NewDisk(s.ExportDir())
}

func (s *Service) exportKey(workspaceID, id string) string {
	if s.exportsOnS3() {
		return workspaceKey(workspaceID) + "backups/" + id + ".zip"
	}
	return id + ".zip"
}

var exportWakers sync.Map // *Service -> chan struct{}

func (s *Service) exportWake() chan struct{} {
	ch, _ := exportWakers.LoadOrStore(s, make(chan struct{}, 1))
	return ch.(chan struct{})
}

// RequestWorkspaceExport queues an export job (admin). Only one export per
// workspace may be pending or running at a time. Plans without data export
// (Starter) get a data-only export: no media files.
func (s *Service) RequestWorkspaceExport(ctx context.Context, p Principal) (WorkspaceExport, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return WorkspaceExport{}, err
	}
	media := int64(1)
	if err := s.requireDataExport(ctx, p.WorkspaceID); err != nil {
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 402 {
			return WorkspaceExport{}, err
		}
		media = 0
	}
	now := s.Now().UTC()
	id := newID("exp")
	err := s.tx(ctx, func(q *db.Queries) error {
		if act, err := q.ActiveWorkspaceExport(ctx, p.WorkspaceID); err == nil {
			e := conflict("an export is already in progress for this workspace")
			e.Details = map[string]any{"export_id": act.ID}
			return e
		} else if !isNoRows(err) {
			return err
		}
		if err := q.CreateWorkspaceExport(ctx, db.CreateWorkspaceExportParams{ID: id, WorkspaceID: p.WorkspaceID,
			ActorType: p.Kind, ActorID: p.ID, ActorName: p.Name, CreatedAt: formatTime(now),
			ExpiresAt: formatTime(now.Add(exportTTL)), IncludesMedia: media}); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return conflict("an export is already in progress for this workspace")
			}
			return err
		}
		_, err := s.emit(ctx, q, p, "workspace.export_requested", "", "", map[string]any{"export_id": id, "includes_media": media == 1})
		return err
	})
	if err != nil {
		return WorkspaceExport{}, err
	}
	select {
	case s.exportWake() <- struct{}{}:
	default:
	}
	return s.GetWorkspaceExport(ctx, p, id)
}

func (s *Service) GetWorkspaceExport(ctx context.Context, p Principal, id string) (WorkspaceExport, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return WorkspaceExport{}, err
	}
	x, err := s.Q.GetWorkspaceExport(ctx, db.GetWorkspaceExportParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return WorkspaceExport{}, notFound("export")
	}
	if err != nil {
		return WorkspaceExport{}, err
	}
	return s.liveExport(x), nil
}

// liveExport reports a ready export past its expiry as expired (the worker deletes the file soon): never
// offer its download.
func (s *Service) liveExport(x db.WorkspaceExport) WorkspaceExport {
	if x.Status == "ready" && x.ExpiresAt <= s.now() {
		x.Status = "expired"
	}
	return toWorkspaceExport(x)
}

// ListWorkspaceExports lists the workspace's latest exports (backups), newest first.
func (s *Service) ListWorkspaceExports(ctx context.Context, p Principal) ([]WorkspaceExport, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWorkspaceExports(ctx, p.WorkspaceID)
	out := make([]WorkspaceExport, 0, len(rows))
	for _, x := range rows {
		out = append(out, s.liveExport(x))
	}
	return out, err
}

// OpenWorkspaceExport returns the finished ZIP for download.
func (s *Service) OpenWorkspaceExport(ctx context.Context, p Principal, id string) (io.ReadSeekCloser, WorkspaceExport, error) {
	x, err := s.GetWorkspaceExport(ctx, p, id)
	if err != nil {
		return nil, x, err
	}
	switch {
	case x.Status == "expired":
		return nil, x, &Error{Code: "gone", Status: 410, Message: "this export has expired; request a new one"}
	case x.Status != "ready":
		return nil, x, conflict("export is " + x.Status)
	}
	f, err := s.exportStore().Open(ctx, s.exportKey(p.WorkspaceID, x.ID)) // the stored id, not the request's
	if s.exportsOnS3() && errors.Is(err, blob.ErrNotExist) {
		// ZIPs made before S3 was enabled remain downloadable until their normal expiry.
		f, err = os.Open(s.exportPath(x.ID))
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, x, &Error{Code: "gone", Status: 410, Message: "this export is no longer available; request a new one"}
	}
	return f, x, err
}

// RunExportWorker builds queued exports and deletes expired ones until ctx ends.
func (s *Service) RunExportWorker(ctx context.Context) {
	requeue := func() error { return s.W.RequeueRunningExports(ctx) }
	if s.pg { // other server processes may be building theirs right now
		requeue = func() error {
			return s.W.RequeueStaleExports(ctx, nullStr(formatTime(s.Now().Add(-time.Hour))))
		}
	}
	if err := requeue(); err != nil { // interrupted by a restart
		s.logger(ctx).Error("export requeue failed", "err", err)
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.ProcessExports(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.exportWake():
		}
	}
}

func init() {
	RegisterWorker(BackgroundWorker{Name: "exports", Run: func(ctx context.Context, s *Service) { s.RunExportWorker(ctx) }})
}

// ProcessExports builds every pending export (one at a time) and removes
// expired ZIPs. Exposed for tests.
func (s *Service) ProcessExports(ctx context.Context) {
	s.expireExports(ctx)
	for ctx.Err() == nil {
		jobs, err := s.Q.PendingWorkspaceExports(ctx, 10)
		if err != nil || len(jobs) == 0 {
			if err != nil && ctx.Err() == nil {
				s.logger(ctx).Error("export poll failed", "err", err)
			}
			return
		}
		for _, j := range jobs {
			n, err := s.W.ClaimWorkspaceExport(ctx, db.ClaimWorkspaceExportParams{StartedAt: nullStr(s.now()), ID: j.ID})
			if err != nil || n == 0 {
				continue
			}
			s.buildExport(ctx, j)
		}
	}
}

func (s *Service) buildExport(ctx context.Context, j db.WorkspaceExport) {
	ctx = logctx.With(ctx, "workspace_id", j.WorkspaceID, "export_id", j.ID)
	p := Principal{WorkspaceID: j.WorkspaceID, Kind: j.ActorType, ID: j.ActorID, Name: j.ActorName}
	final := s.exportPath(j.ID)
	size, err := s.writeExportZip(ctx, j.WorkspaceID, final, j.IncludesMedia != 0)
	if err == nil && s.exportsOnS3() {
		var f *os.File
		f, err = os.Open(final)
		if err == nil {
			err = s.Cfg.Blob.Put(ctx, s.exportKey(j.WorkspaceID, j.ID), f, size, "application/zip")
			f.Close()
		}
		os.Remove(final)
	}
	now := s.Now().UTC()
	status, msg := "ready", ""
	if err != nil {
		status, msg = "failed", "export failed"
		s.logger(ctx).Error("workspace export failed", "err", err)
		os.Remove(final)
	}
	n, ferr := s.W.FinishWorkspaceExport(ctx, db.FinishWorkspaceExportParams{Status: status, SizeBytes: size, Error: msg,
		FinishedAt: nullStr(formatTime(now)), ExpiresAt: formatTime(now.Add(exportTTL)), ID: j.ID})
	if ferr != nil {
		s.logger(ctx).Error("export finish failed", "err", ferr)
		return
	}
	if n == 0 {
		// Expired by a purge or the workspace was deleted mid-build: the ZIP
		// may contain erased data, so it must not survive.
		s.removeExportFile(ctx, j.WorkspaceID, j.ID)
		return
	}
	if err == nil {
		_ = s.tx(ctx, func(q *db.Queries) error {
			_, err := s.emit(ctx, q, p, "workspace.exported", "", "", map[string]any{"export_id": j.ID, "size_bytes": size})
			return err
		})
	}
}

func (s *Service) expireExports(ctx context.Context) {
	ids, err := s.Q.ExpiredWorkspaceExports(ctx, s.now())
	if err != nil {
		return
	}
	for _, row := range ids {
		s.removeExportFile(ctx, row.WorkspaceID, row.ID)
		_ = s.W.MarkWorkspaceExportExpired(ctx, row.ID)
	}
	s.sweepExportFiles(ctx)
}

// exportS3Sweeps: when each service last looked for orphan archives in the bucket. Listing "workspaces/" walks
// every workspace's files (media and thumbnails too), so it happens at most once per exportS3SweepEvery, not
// on every run of the export worker (every minute).
var exportS3Sweeps sync.Map // *Service -> time.Time

const exportS3SweepEvery = time.Hour

// sweepExportFiles removes ZIPs that no ready/running export row accounts
// for (deleted workspace, crash between file write and bookkeeping, ...).
func (s *Service) sweepExportFiles(ctx context.Context) {
	stores := []blob.Store{blob.NewDisk(s.ExportDir())}
	if s.exportsOnS3() {
		last, ok := exportS3Sweeps.Load(s)
		if now := s.Now(); !ok || now.Sub(last.(time.Time)) >= exportS3SweepEvery {
			exportS3Sweeps.Store(s, now)
			stores = append(stores, s.Cfg.Blob)
		}
	}
	for _, store := range stores {
		prefix := "exp_"
		if store.Name() == "s3" {
			prefix = "workspaces/"
		}
		err := store.Walk(ctx, prefix, func(key string, _ int64) error {
			name, wsID := key, ""
			if store.Name() == "s3" {
				parts := strings.Split(key, "/")
				if len(parts) != 4 || parts[0] != "workspaces" || parts[1] == "" || parts[2] != "backups" {
					return nil
				}
				wsID, name = parts[1], parts[3]
			}
			id, ok := strings.CutSuffix(name, ".zip")
			if !ok || !strings.HasPrefix(id, "exp_") || strings.ContainsAny(id, "/\\") {
				return nil
			}
			var status string
			var err error
			if wsID == "" {
				status, err = s.Q.ExportStatusByID(ctx, id)
			} else {
				var row db.WorkspaceExport
				row, err = s.Q.GetWorkspaceExport(ctx, db.GetWorkspaceExportParams{WorkspaceID: wsID, ID: id})
				status = row.Status
			}
			if err == nil && (status == "ready" || status == "running") {
				return nil
			}
			if err != nil && !isNoRows(err) {
				return err // Never delete on database uncertainty.
			}
			if err := store.Delete(ctx, key); err != nil {
				s.logger(ctx).Warn("export cleanup failed", "key", key, "driver", store.Name(), "err", err)
			}
			return nil
		})
		if err != nil {
			s.logger(ctx).Warn("export cleanup failed", "driver", store.Name(), "err", err)
		}
	}
}

func (s *Service) removeExportFile(ctx context.Context, workspaceID, id string) {
	if err := blob.NewDisk(s.ExportDir()).Delete(ctx, id+".zip"); err != nil {
		s.logger(ctx).Warn("export file not removed", "export_id", id, "err", err)
	}
	if s.exportsOnS3() {
		if err := s.Cfg.Blob.Delete(ctx, s.exportKey(workspaceID, id)); err != nil {
			s.logger(ctx).Warn("export object not removed", "export_id", id, "err", err)
		}
	}
}

// exportPost is one line of posts.ndjson.
type exportPost struct {
	Post
	MediaFiles []string `json:"media_files"` // paths inside the ZIP, in media order ("" when a file was missing)
}

type exportMedia struct {
	Media
	File string `json:"file"` // path inside the ZIP, "" if the file was missing on disk
}

// writeExportZip writes the ZIP to dst via a temp file. Data is read page by
// page outside any transaction (a write lock for the whole export would stall
// the workspace); ponytail: rows changed during the export may appear in their
// before or after state — fine for portability, not a point-in-time snapshot
// (use `server backup` for that).
func (s *Service) writeExportZip(ctx context.Context, wsID, dst string, withMedia bool) (size int64, err error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return 0, err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	bw := bufio.NewWriterSize(f, 256<<10)
	zw := zip.NewWriter(bw)
	if err = s.fillExportZip(ctx, zw, wsID, withMedia); err != nil {
		return 0, err
	}
	if err = zw.Close(); err != nil {
		return 0, err
	}
	if err = bw.Flush(); err != nil {
		return 0, err
	}
	if err = f.Sync(); err != nil {
		return 0, err
	}
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if err = f.Close(); err != nil {
		return 0, err
	}
	if err = os.Rename(tmp, dst); err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func (s *Service) fillExportZip(ctx context.Context, zw *zip.Writer, wsID string, withMedia bool) error {
	ts := s.Now().UTC()
	create := func(name string, method uint16) (io.Writer, error) {
		return zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: ts})
	}
	writeJSONFile := func(name string, v any) error {
		w, err := create(name, zip.Deflate)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	ws, err := s.Q.GetWorkspace(ctx, wsID)
	if err != nil {
		return err
	}
	counts := map[string]int{}

	// media/ first, so posts can point at the deduplicated file names.
	mediaFiles := map[string]string{} // media id -> zip path
	var mediaIndex []exportMedia
	used := map[string]bool{}
	after := ""
	for {
		rows, err := s.Q.ExportMediaPage(ctx, db.ExportMediaPageParams{WorkspaceID: wsID, AfterID: after, Lim: exportPageSize})
		if err != nil {
			return err
		}
		for _, m := range rows {
			entry := exportMedia{Media: Media{MediaRef: *s.mediaRef(m), SHA256: m.Sha256, CreatedAt: m.CreatedAt}}
			if !withMedia { // data only: the file is referenced, not copied
				mediaIndex = append(mediaIndex, entry)
				continue
			}
			src, err := s.openMediaFile(ctx, m.WorkspaceID, m.StorageName)
			if err != nil {
				s.logger(ctx).Warn("export: media file missing", "media_id", m.ID)
			} else {
				name := "media/" + uniqueZipName(zipSafeName(m.OriginalName), used)
				w, err := create(name, zip.Store) // media is usually compressed already
				if err == nil {
					_, err = io.Copy(w, src)
				}
				src.Close()
				if err != nil {
					return err
				}
				entry.File, mediaFiles[m.ID] = name, name
				counts["media"]++
			}
			mediaIndex = append(mediaIndex, entry)
		}
		if len(rows) < exportPageSize {
			break
		}
		after = rows[len(rows)-1].ID
	}

	// collections.ndjson
	cols, err := s.Q.ExportAllCollections(ctx, wsID)
	if err != nil {
		return err
	}
	if err := writeNDJSON(create, "collections.ndjson", len(cols), func(i int) any { return toCollection(cols[i]) }); err != nil {
		return err
	}
	counts["collections"] = len(cols)

	// posts.ndjson (with their targets)
	w, err := create("posts.ndjson", zip.Deflate)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	after = ""
	for {
		rows, err := s.Q.ExportPostsPage(ctx, db.ExportPostsPageParams{WorkspaceID: wsID, AfterID: after, Lim: exportPageSize})
		if err != nil {
			return err
		}
		posts, err := s.postsOut(ctx, s.Q, wsID, rows) // a page of posts in a fixed number of reads
		if err != nil {
			return err
		}
		for _, post := range posts {
			line := exportPost{Post: post, MediaFiles: []string{}}
			for _, id := range post.Media {
				line.MediaFiles = append(line.MediaFiles, mediaFiles[id])
			}
			if err := enc.Encode(line); err != nil {
				return err
			}
			counts["posts"]++
		}
		if len(rows) < exportPageSize {
			break
		}
		after = rows[len(rows)-1].ID
	}

	// responses.ndjson, response_insights.ndjson
	resp, err := s.Q.ExportResponses(ctx, wsID)
	if err != nil {
		return err
	}
	if err := writeNDJSON(create, "responses.ndjson", len(resp), func(i int) any { return resp[i] }); err != nil {
		return err
	}
	counts["responses"] = len(resp)
	insights, err := s.Q.ExportResponseInsights(ctx, wsID)
	if err != nil {
		return err
	}
	if err := writeNDJSON(create, "response_insights.ndjson", len(insights), func(i int) any { return insights[i] }); err != nil {
		return err
	}

	// events.ndjson
	if w, err = create("events.ndjson", zip.Deflate); err != nil {
		return err
	}
	enc = json.NewEncoder(w)
	x := &EventExport{s: s, params: db.ExportEventsPageParams{WorkspaceID: wsID, IncludeAudit: 1, Lim: exportPageSize}} // admin-only job
	if err := x.Each(ctx, func(e Event) error {
		e.Data = redactSecrets(e.Data)
		counts["events"]++
		return enc.Encode(e)
	}); err != nil {
		return err
	}

	if err := writeNDJSON(create, "media.ndjson", len(mediaIndex), func(i int) any { return mediaIndex[i] }); err != nil {
		return err
	}

	hooks, err := s.Q.ListWebhooks(ctx, wsID)
	if err != nil {
		return err
	}
	outHooks := make([]Webhook, 0, len(hooks))
	for _, h := range hooks {
		wh := toWebhook(h)
		wh.Secret = "[redacted]"
		outHooks = append(outHooks, wh)
	}
	if err := writeJSONFile("webhooks.json", outHooks); err != nil {
		return err
	}
	counts["webhooks"] = len(outHooks)

	chans, err := s.Q.ExportAllChannels(ctx, wsID)
	if err != nil {
		return err
	}
	if err := writeJSONFile("channels.json", chans); err != nil {
		return err
	}
	counts["channels"] = len(chans)

	schedules, err := s.Q.ExportAllSchedules(ctx, wsID)
	if err != nil {
		return err
	}
	if err := writeJSONFile("schedules.json", schedules); err != nil {
		return err
	}
	counts["schedules"] = len(schedules)

	meta := map[string]any{
		"format": "staqly-export", "format_version": 1, "exported_at": formatTime(ts),
		"workspace": s.toWorkspace(ws), "counts": counts, "includes_media": withMedia,
	}
	if !withMedia {
		meta["note"] = dataOnlyNote
	}
	if err := writeJSONFile("workspace.json", meta); err != nil {
		return err
	}
	rw, err := create("README.txt", zip.Deflate)
	if err != nil {
		return err
	}
	readme := fmt.Sprintf(exportReadme, ws.Name, formatTime(ts))
	if !withMedia {
		readme += "\n" + dataOnlyReadme
	}
	_, err = io.WriteString(rw, readme)
	return err
}

func writeNDJSON(create func(string, uint16) (io.Writer, error), name string, n int, at func(int) any) error {
	w, err := create(name, zip.Deflate)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for i := range n {
		if err := enc.Encode(at(i)); err != nil {
			return err
		}
	}
	return nil
}

// zipSafeName keeps an uploaded file's original name usable as a single ZIP
// path element (no separators, no dot-dot, no leading dot).
func zipSafeName(name string) string {
	name = cleanOriginalName(name)
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(name)
	if strings.HasPrefix(name, ".") {
		name = "_" + name
	}
	return name
}

// uniqueZipName appends " (2)", " (3)", ... before the extension on collisions
// (case-insensitive, for case-insensitive file systems).
func uniqueZipName(name string, used map[string]bool) string {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	cand := name
	for i := 2; used[strings.ToLower(cand)]; i++ {
		cand = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	used[strings.ToLower(cand)] = true
	return cand
}

// redactSecrets masks any "secret" field in event data (e.g. webhook events).
func redactSecrets(raw json.RawMessage) json.RawMessage {
	if !strings.Contains(string(raw), `"secret"`) {
		return raw
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if k == "secret" {
					t[k] = "[redacted]"
				} else {
					t[k] = walk(val)
				}
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		}
		return v
	}
	return json.RawMessage(mustJSON(walk(v)))
}

const exportReadme = `Staqly workspace export
========================

Workspace: %s
Exported:  %s (UTC)

Files
-----
workspace.json      Workspace details, export format version and record counts.
collections.ndjson  One collection per line (including archived ones).
posts.ndjson        One post per line with its targets (channel, status, link) and
                    "media_files", the paths of its files below.
events.ndjson       The activity log, oldest first (one event per line).
media.ndjson        Every uploaded file with its id, type, size, SHA-256 and
                    "file" (path inside this archive).
media/              Uploaded files under their original names; duplicates get
                    " (2)", " (3)", ... appended.
webhooks.json       Webhook endpoints. Signing secrets are redacted.
channels.json       Connected channels (no credentials).
schedules.json      Publishing schedules (no endpoint secrets).

NDJSON = one JSON document per line. Timestamps are ISO-8601 UTC.
API keys, sessions and the recovery key are not included.
`

const dataOnlyNote = "Data-only export: media files are referenced in media.ndjson by id, original_name and mime_type but not included. Exports with the files and backups come with the Plus plan and above."

const dataOnlyReadme = `Data only
---------
This export has no media/ folder: media.ndjson lists every file by id,
original_name and mime_type with an empty "file", and posts have empty
"media_files". Exports with the files and backups come with the Plus plan
and above.
`
