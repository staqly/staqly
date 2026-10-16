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
	CollectionID, ItemID string
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
	if err := p.require(ScopeItemsRead); err != nil {
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
	for field, v := range map[string]string{"type": f.Type, "type_prefix": f.TypePrefix, "actor_id": f.ActorID,
		"collection_id": f.CollectionID, "item_id": f.ItemID} {
		if utf8.RuneCountInString(v) > 200 {
			return nil, invalid(field, "must be at most 200 characters")
		}
	}
	return &EventExport{Format: f.Format, s: s, params: db.ExportEventsPageParams{
		WorkspaceID: p.WorkspaceID, FromTs: from, ToTs: to, Type: optAny(f.Type), TypePrefix: optAny(f.TypePrefix),
		ActorType: optAny(f.ActorType), ActorID: optAny(f.ActorID), CollectionID: optAny(f.CollectionID),
		ItemID: optAny(f.ItemID), Lim: exportPageSize,
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
var EventCSVHeader = []string{"created_at", "id", "type", "actor_type", "actor_id", "actor_name", "collection_id", "item_id", "data"}

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
		deref(e.CollectionID), deref(e.ItemID), string(e.Data)}
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
}

func toWorkspaceExport(x db.WorkspaceExport) WorkspaceExport {
	out := WorkspaceExport{ID: x.ID, Status: x.Status, SizeBytes: x.SizeBytes, Error: x.Error, CreatedAt: x.CreatedAt,
		StartedAt: ptr(x.StartedAt), FinishedAt: ptr(x.FinishedAt), ExpiresAt: x.ExpiresAt}
	if x.Status == "ready" {
		out.DownloadURL = "/v1/workspace/exports/" + x.ID + "/download"
	}
	return out
}

// ExportDir is where finished ZIPs live (DATA_DIR/exports).
func (s *Service) ExportDir() string {
	return filepath.Join(s.Env("DATA_DIR", "./data"), "exports")
}

// export IDs are server-generated (exp_[a-z2-7]+), so the path is safe.
func (s *Service) exportPath(id string) string { return filepath.Join(s.ExportDir(), id+".zip") }

var exportWakers sync.Map // *Service -> chan struct{}

func (s *Service) exportWake() chan struct{} {
	ch, _ := exportWakers.LoadOrStore(s, make(chan struct{}, 1))
	return ch.(chan struct{})
}

// RequestWorkspaceExport queues an export job (admin). Only one export per
// workspace may be pending or running at a time.
func (s *Service) RequestWorkspaceExport(ctx context.Context, p Principal) (WorkspaceExport, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return WorkspaceExport{}, err
	}
	if err := s.requireDataExport(ctx, p.WorkspaceID); err != nil {
		return WorkspaceExport{}, err
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
			ExpiresAt: formatTime(now.Add(exportTTL))}); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return conflict("an export is already in progress for this workspace")
			}
			return err
		}
		_, err := s.emit(ctx, q, p, "workspace.export_requested", "", "", map[string]any{"export_id": id})
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
func (s *Service) OpenWorkspaceExport(ctx context.Context, p Principal, id string) (*os.File, WorkspaceExport, error) {
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
	f, err := os.Open(s.exportPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, x, &Error{Code: "gone", Status: 410, Message: "this export is no longer available; request a new one"}
	}
	return f, x, err
}

// RunExportWorker builds queued exports and deletes expired ones until ctx ends.
func (s *Service) RunExportWorker(ctx context.Context) {
	if err := s.W.RequeueRunningExports(ctx); err != nil { // interrupted by a restart
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
	size, err := s.writeExportZip(ctx, j.WorkspaceID, final)
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
		s.removeExportFile(j.ID)
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
	for _, id := range ids {
		s.removeExportFile(id)
		_ = s.W.MarkWorkspaceExportExpired(ctx, id)
	}
	s.sweepExportFiles(ctx)
}

// sweepExportFiles removes ZIPs that no ready/running export row accounts
// for (deleted workspace, crash between file write and bookkeeping, ...).
func (s *Service) sweepExportFiles(ctx context.Context) {
	entries, err := os.ReadDir(s.ExportDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".zip")
		if e.IsDir() || !ok || !strings.HasPrefix(id, "exp_") {
			continue
		}
		st, err := s.Q.ExportStatusByID(ctx, id)
		if err == nil && (st == "ready" || st == "running") {
			continue
		}
		if err != nil && !isNoRows(err) {
			continue // database trouble: never delete on uncertainty
		}
		s.removeExportFile(id)
	}
}

func (s *Service) removeExportFile(id string) {
	if err := os.Remove(s.exportPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.Log.Warn("export file not removed", "export_id", id, "err", err)
	}
}

// exportItem is one line of items.ndjson.
type exportItem struct {
	Item
	MediaFile string        `json:"media_file,omitempty"`
	Versions  []ItemVersion `json:"versions"`
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
func (s *Service) writeExportZip(ctx context.Context, wsID, dst string) (size int64, err error) {
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
	if err = s.fillExportZip(ctx, zw, wsID); err != nil {
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

func (s *Service) fillExportZip(ctx context.Context, zw *zip.Writer, wsID string) error {
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

	// media/ first, so items can point at the deduplicated file names.
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
			src, err := os.Open(s.mediaPath(m.StorageName))
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

	// items.ndjson (with full version history)
	w, err := create("items.ndjson", zip.Deflate)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	after = ""
	for {
		rows, err := s.Q.ExportItemsPage(ctx, db.ExportItemsPageParams{WorkspaceID: wsID, AfterID: after, Lim: exportPageSize})
		if err != nil {
			return err
		}
		for _, r := range rows {
			line := exportItem{Item: s.toItem(ctx, r), Versions: []ItemVersion{}}
			if r.MediaID.Valid {
				line.MediaFile = mediaFiles[r.MediaID.String]
			}
			vs, err := s.Q.ExportItemVersions(ctx, r.ID)
			if err != nil {
				return err
			}
			for _, v := range vs {
				line.Versions = append(line.Versions, ItemVersion{Version: v.Version, Snapshot: json.RawMessage(v.Snapshot),
					Actor: v.Actor, CreatedAt: v.CreatedAt})
			}
			if err := enc.Encode(line); err != nil {
				return err
			}
			counts["items"]++
		}
		if len(rows) < exportPageSize {
			break
		}
		after = rows[len(rows)-1].ID
	}

	// events.ndjson
	if w, err = create("events.ndjson", zip.Deflate); err != nil {
		return err
	}
	enc = json.NewEncoder(w)
	x := &EventExport{s: s, params: db.ExportEventsPageParams{WorkspaceID: wsID, Lim: exportPageSize}}
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

	watchers, err := s.Q.ListWatchers(ctx, wsID)
	if err != nil {
		return err
	}
	outWatchers := make([]Watcher, 0, len(watchers))
	for _, wt := range watchers {
		cols, err := s.watcherCollections(ctx, s.Q, wt)
		if err != nil {
			return err
		}
		outWatchers = append(outWatchers, toWatcher(wt, cols))
	}
	if err := writeJSONFile("watchers.json", outWatchers); err != nil {
		return err
	}
	counts["watchers"] = len(outWatchers)

	if err := writeJSONFile("workspace.json", map[string]any{
		"format": "staqly-export", "format_version": 1, "exported_at": formatTime(ts),
		"workspace": s.toWorkspace(ws), "counts": counts,
	}); err != nil {
		return err
	}
	rw, err := create("README.txt", zip.Deflate)
	if err != nil {
		return err
	}
	_, err = io.WriteString(rw, fmt.Sprintf(exportReadme, ws.Name, formatTime(ts)))
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

const exportReadme = `staqly workspace export
========================

Workspace: %s
Exported:  %s (UTC)

Files
-----
workspace.json      Workspace details, export format version and record counts.
collections.ndjson  One collection per line (including archived ones).
items.ndjson        One item per line with its full version history ("versions",
                    oldest first) and "media_file", the path of its file below.
events.ndjson       The activity log, oldest first (one event per line).
media.ndjson        Every uploaded file with its id, type, size, SHA-256 and
                    "file" (path inside this archive).
media/              Uploaded files under their original names; duplicates get
                    " (2)", " (3)", ... appended.
webhooks.json       Webhook endpoints. Signing secrets are redacted.
watchers.json       Watchers (RSS / page-change sources).

NDJSON = one JSON document per line. Timestamps are ISO-8601 UTC.
API keys, sessions and the recovery key are not included.
`
