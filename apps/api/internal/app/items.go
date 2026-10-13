package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

var ItemTypes = []string{"image", "video", "text", "link", "json", "file"}

type MediaRef struct {
	ID           string `json:"id"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	OriginalName string `json:"original_name"`
	URL          string `json:"url"`
}

type Item struct {
	ID           string          `json:"id"`
	CollectionID string          `json:"collection_id"`
	Title        string          `json:"title"`
	Type         string          `json:"type"`
	Content      string          `json:"content"`
	URL          string          `json:"url"`
	Media        *MediaRef       `json:"media"`
	Metadata     json.RawMessage `json:"metadata"`
	Status       string          `json:"status"`
	Source       string          `json:"source"`
	ExternalRef  string          `json:"external_ref"`
	Feedback     string          `json:"feedback"`
	Version      int64           `json:"version"`
	DecidedAt    *string         `json:"decided_at"`
	ExpiresAt    *string         `json:"expires_at"`
	Editable     []string        `json:"editable"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

func (s *Service) toItem(ctx context.Context, i db.Item) Item {
	it := Item{
		ID: i.ID, CollectionID: i.CollectionID, Title: i.Title, Type: i.Type, Content: i.Content, URL: i.Url,
		Metadata: json.RawMessage(i.Metadata), Status: i.Status, Source: i.Source, ExternalRef: i.ExternalRef,
		Feedback: i.Feedback, Version: i.Version, DecidedAt: ptr(i.DecidedAt), ExpiresAt: ptr(i.ExpiresAt),
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, Editable: []string{},
	}
	_ = json.Unmarshal([]byte(i.Editable), &it.Editable)
	if i.MediaID.Valid {
		// ponytail: one lookup per item; join in SQL if list pages get slow.
		if m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: i.WorkspaceID, ID: i.MediaID.String}); err == nil {
			it.Media = s.mediaRef(m)
		}
	}
	return it
}

// itemFields is the editable subset of an item shared by create, update and
// "edit before approval".
type itemFields struct {
	Title         *string                    `json:"title"`
	Content       json.RawMessage            `json:"content"`
	URL           *string                    `json:"url"`
	MediaID       *string                    `json:"media_id"`
	Metadata      json.RawMessage            `json:"metadata"`
	MetadataPatch map[string]json.RawMessage `json:"metadata_patch"`
}

type CreateItemInput struct {
	itemFields
	Type        string `json:"type"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	ExternalRef string `json:"external_ref"`
	// ExpiresIn: a pending item not decided within this time becomes
	// "expired" (seconds as a number, or a duration string like "24h").
	ExpiresIn json.RawMessage `json:"expires_in"`
	// Editable limits what a reviewer may change before approving:
	// "title", "content", "url", "media_id", "metadata" or "metadata.<key>".
	// Empty = everything.
	Editable []string `json:"editable"`
}

var editableFields = []string{"title", "content", "url", "media_id", "metadata"}

func parseExpiresIn(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var secs float64
	if err := json.Unmarshal(raw, &secs); err == nil {
		d := time.Duration(secs) * time.Second
		if d < time.Minute || d > 90*24*time.Hour {
			return 0, invalid("expires_in", "must be between 60 seconds and 90 days")
		}
		return d, nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if d, err := time.ParseDuration(str); err == nil && d >= time.Minute && d <= 90*24*time.Hour {
			return d, nil
		}
	}
	return 0, invalid("expires_in", `must be seconds or a duration like "24h" (1 minute to 90 days)`)
}

func validateEditable(fields []string) error {
	if len(fields) > 50 {
		return invalid("editable", "at most 50 fields")
	}
	for _, f := range fields {
		if slices.Contains(editableFields, f) || (strings.HasPrefix(f, "metadata.") && len(f) > 9 && len(f) < 110) {
			continue
		}
		return invalid("editable", "unknown field "+f+" (use title, content, url, media_id, metadata or metadata.<key>)")
	}
	return nil
}

type UpdateItemInput struct {
	itemFields
	Status          *string `json:"status"`
	Source          *string `json:"source"`
	ExternalRef     *string `json:"external_ref"`
	CollectionID    *string `json:"collection_id"`
	ExpectedVersion *int64  `json:"expected_version"`
}

// contentEdit reports whether the update replaces reviewed content (anything
// but metadata_patch).
func (f itemFields) contentEdit() bool {
	return f.Title != nil || f.Content != nil || f.URL != nil || f.MediaID != nil || f.Metadata != nil
}

// writerTransitions are the status changes items:write may make on its own:
// drafting, the post-approval lifecycle (processing → completed/failed),
// retries, resubmitting rejected/expired work and archiving. Everything else
// (e.g. pending → completed, approved → pending) needs items:review.
var writerTransitions = map[string][]string{
	"draft":      {"pending"},
	"pending":    {"draft"},
	"approved":   {"processing", "completed", "failed"},
	"processing": {"completed", "failed"},
	"failed":     {"processing", "pending"},
	"rejected":   {"pending"},
	"expired":    {"pending"},
}

func writerTransition(from, to string) bool {
	return from == to || to == "archived" || slices.Contains(writerTransitions[from], to)
}

// contentString accepts either a JSON string or any JSON value (stored compacted).
func contentString(raw json.RawMessage) (string, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	if raw[0] == '"' {
		var s string
		err := json.Unmarshal(raw, &s)
		return s, false, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return "", false, err
	}
	return buf.String(), true, nil
}

func (s *Service) applyFields(ctx context.Context, q *db.Queries, it *db.Item, f itemFields) (isJSON bool, err error) {
	if f.Title != nil {
		if it.Title, err = trimmed(*f.Title, "title", 500, false); err != nil {
			return false, err
		}
	}
	if f.Content != nil {
		c, j, err := contentString(f.Content)
		if err != nil {
			return false, invalid("content", "must be a string or valid JSON")
		}
		if len(c) > 1<<20 {
			return false, invalid("content", "must be at most 1 MiB")
		}
		it.Content, isJSON = c, j
	}
	if f.URL != nil {
		u := strings.TrimSpace(*f.URL)
		if u != "" {
			pu, err := url.Parse(u)
			if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" || len(u) > 4000 {
				return false, invalid("url", "must be an absolute http(s) URL")
			}
		}
		it.Url = u
	}
	if f.MediaID != nil {
		if *f.MediaID == "" {
			it.MediaID = nullStr("")
		} else {
			if _, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: it.WorkspaceID, ID: *f.MediaID}); err != nil {
				return false, invalid("media_id", "unknown media")
			}
			it.MediaID = nullStr(*f.MediaID)
		}
	}
	if f.Metadata != nil {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(f.Metadata, &m); err != nil || m == nil {
			return false, invalid("metadata", "must be a JSON object")
		}
		it.Metadata = mustJSON(m)
	}
	if f.MetadataPatch != nil {
		var m map[string]json.RawMessage
		_ = json.Unmarshal([]byte(it.Metadata), &m)
		if m == nil {
			m = map[string]json.RawMessage{}
		}
		for k, v := range f.MetadataPatch {
			if string(bytes.TrimSpace(v)) == "null" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		it.Metadata = mustJSON(m)
	}
	if len(it.Metadata) > 256<<10 {
		return false, invalid("metadata", "must be at most 256 KiB")
	}
	return isJSON, nil
}

func (s *Service) CreateItem(ctx context.Context, p Principal, collection string, in CreateItemInput) (Item, error) {
	return s.createItem(ctx, p, collection, in, nil)
}

// errSkipItem is returned by createItem when its pre hook declined the write.
var errSkipItem = errors.New("item creation skipped")

// createItem creates an item; pre (optional) runs first inside the same
// transaction and may veto the creation (false → errSkipItem), so a watcher's
// "seen" mark and the item it produces commit or roll back together.
func (s *Service) createItem(ctx context.Context, p Principal, collection string, in CreateItemInput, pre func(q *db.Queries) (bool, error)) (Item, error) {
	if err := p.require(ScopeItemsWrite); err != nil {
		return Item{}, err
	}
	col, err := s.FindCollection(ctx, p, collection)
	if err != nil {
		return Item{}, err
	}
	if col.Archived {
		return Item{}, conflict("collection is archived")
	}
	now := s.now()
	it := db.Item{ID: newID("itm"), WorkspaceID: p.WorkspaceID, CollectionID: col.ID, Metadata: "{}", Status: "pending",
		CreatedAt: now, UpdatedAt: now, Version: 1}
	if in.Status != "" {
		if !validStatus(in.Status) {
			return Item{}, invalid("status", "must be one of "+strings.Join(Statuses, ", "))
		}
		if (in.Status == "approved" || in.Status == "rejected") && !p.Has(ScopeItemsReview) {
			return Item{}, &Error{Code: "forbidden", Status: 403, Message: "creating a decided item requires items:review"}
		}
		it.Status = in.Status
	}
	if it.Source, err = trimmed(in.Source, "source", 200, false); err != nil {
		return Item{}, err
	}
	if it.ExternalRef, err = trimmed(in.ExternalRef, "external_ref", 200, false); err != nil {
		return Item{}, err
	}
	expiresIn, err := parseExpiresIn(in.ExpiresIn)
	if err != nil {
		return Item{}, err
	}
	if err := validateEditable(in.Editable); err != nil {
		return Item{}, err
	}
	if in.Editable == nil {
		in.Editable = []string{}
	}
	it.Editable = mustJSON(in.Editable)
	var out db.Item
	err = s.tx(ctx, func(q *db.Queries) error {
		if pre != nil {
			ok, err := pre(q)
			if err != nil {
				return err
			}
			if !ok {
				return errSkipItem
			}
		}
		isJSON, err := s.applyFields(ctx, q, &it, in.itemFields)
		if err != nil {
			return err
		}
		it.Type, err = s.inferType(ctx, q, it, in.Type, isJSON)
		if err != nil {
			return err
		}
		if it.Title == "" {
			it.Title = defaultTitle(it)
		}
		if it.Status == "approved" || it.Status == "rejected" {
			it.DecidedAt = nullStr(now)
		}
		if err := s.useItemQuota(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		if err := q.CreateItem(ctx, db.CreateItemParams{ID: it.ID, WorkspaceID: it.WorkspaceID, CollectionID: it.CollectionID,
			Title: it.Title, Type: it.Type, Content: it.Content, Url: it.Url, MediaID: it.MediaID, Metadata: it.Metadata,
			Status: it.Status, Source: it.Source, ExternalRef: it.ExternalRef, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if len(in.Editable) > 0 {
			if err := q.SetItemEditable(ctx, db.SetItemEditableParams{Editable: it.Editable, ID: it.ID}); err != nil {
				return err
			}
		}
		if expiresIn > 0 && it.Status == "pending" {
			it.ExpiresAt = nullStr(formatTime(s.Now().Add(expiresIn)))
			if err := q.SetItemExpiry(ctx, db.SetItemExpiryParams{ExpiresAt: it.ExpiresAt, ID: it.ID}); err != nil {
				return err
			}
		}
		if _, err = s.emit(ctx, q, p, "item.created", col.ID, it.ID, s.itemEventData(ctx, it, col, nil)); err != nil {
			return err
		}
		out, err = s.applyRules(ctx, q, p, it, col)
		return err
	})
	if err != nil {
		return Item{}, err
	}
	return s.toItem(ctx, out), nil
}

func (s *Service) inferType(ctx context.Context, q *db.Queries, it db.Item, given string, isJSON bool) (string, error) {
	if given != "" {
		if !slices.Contains(ItemTypes, given) {
			return "", invalid("type", "must be one of "+strings.Join(ItemTypes, ", "))
		}
		if given == "json" && it.Content != "" && !json.Valid([]byte(it.Content)) {
			return "", invalid("content", "must be valid JSON for json items")
		}
		return given, nil
	}
	if it.MediaID.Valid {
		m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: it.WorkspaceID, ID: it.MediaID.String})
		if err != nil {
			return "", err
		}
		switch {
		case strings.HasPrefix(m.MimeType, "image/"):
			return "image", nil
		case strings.HasPrefix(m.MimeType, "video/"):
			return "video", nil
		}
		return "file", nil
	}
	if isJSON {
		return "json", nil
	}
	if it.Url != "" && it.Content == "" {
		return "link", nil
	}
	return "text", nil
}

func defaultTitle(it db.Item) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(it.Content), "\n"); line != "" && it.Type == "text" {
		if r := []rune(line); len(r) > 80 {
			return string(r[:80]) + "…"
		}
		return line
	}
	if it.Url != "" {
		return it.Url
	}
	return "Untitled " + it.Type
}

// itemEventData is the snapshot carried by item events and webhook payloads.
func (s *Service) itemEventData(ctx context.Context, it db.Item, col Collection, extra map[string]any) map[string]any {
	d := map[string]any{
		"item":       s.toItem(ctx, it),
		"collection": map[string]string{"id": col.ID, "name": col.Name},
	}
	for k, v := range extra {
		d[k] = v
	}
	return d
}

func (s *Service) GetItem(ctx context.Context, p Principal, id string) (Item, error) {
	if err := p.require(ScopeItemsRead); err != nil {
		return Item{}, err
	}
	it, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return Item{}, notFound("item")
	}
	if err != nil {
		return Item{}, err
	}
	return s.toItem(ctx, it), nil
}

// mutateItem loads an item inside a transaction, lets fn modify it, then
// writes it with an optimistic version check and a version snapshot.
func (s *Service) mutateItem(ctx context.Context, q *db.Queries, p Principal, id string, expected *int64,
	fn func(it *db.Item, col Collection) (eventType string, extra map[string]any, err error)) (db.Item, error) {
	it, err := q.GetItem(ctx, db.GetItemParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return it, notFound("item")
	}
	if err != nil {
		return it, err
	}
	if expected != nil && *expected != it.Version {
		return it, conflict(fmt.Sprintf("item was modified (version %d, expected %d)", it.Version, *expected))
	}
	dc, err := q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: it.CollectionID})
	if err != nil {
		return it, err
	}
	before := it
	typ, extra, err := fn(&it, toCollection(dc))
	if err != nil {
		return it, err
	}
	if err := q.InsertItemVersion(ctx, db.InsertItemVersionParams{ItemID: before.ID, Version: before.Version,
		Snapshot: mustJSON(s.toItem(ctx, before)), Actor: p.Kind + ":" + p.ID, CreatedAt: s.now()}); err != nil {
		return it, err
	}
	it.UpdatedAt = s.now()
	n, err := q.UpdateItem(ctx, db.UpdateItemParams{Title: it.Title, Content: it.Content, Url: it.Url, MediaID: it.MediaID,
		Metadata: it.Metadata, Status: it.Status, Source: it.Source, ExternalRef: it.ExternalRef, Feedback: it.Feedback,
		DecidedAt: it.DecidedAt, CollectionID: it.CollectionID, UpdatedAt: it.UpdatedAt,
		WorkspaceID: p.WorkspaceID, ID: id, Version: before.Version})
	if err != nil {
		return it, err
	}
	if n == 0 {
		return it, conflict("item was modified concurrently")
	}
	it.Version++
	col := toCollection(dc)
	if it.CollectionID != dc.ID {
		nc, err := q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: it.CollectionID})
		if err != nil {
			return it, err
		}
		col = toCollection(nc)
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["previous_status"] = before.Status
	_, err = s.emit(ctx, q, p, typ, it.CollectionID, it.ID, s.itemEventData(ctx, it, col, extra))
	return it, err
}

func (s *Service) UpdateItem(ctx context.Context, p Principal, id string, in UpdateItemInput) (Item, error) {
	if err := p.require(ScopeItemsWrite); err != nil {
		return Item{}, err
	}
	var out db.Item
	err := s.tx(ctx, func(q *db.Queries) error {
		var err error
		out, err = s.mutateItem(ctx, q, p, id, in.ExpectedVersion, func(it *db.Item, col Collection) (string, map[string]any, error) {
			// After a decision the reviewed content is frozen for writers:
			// changing what was approved/rejected needs items:review. Agents
			// may still record results via metadata_patch.
			if (it.Status == "approved" || it.Status == "rejected") && in.contentEdit() && !p.Has(ScopeItemsReview) {
				return "", nil, &Error{Code: "forbidden", Status: 403,
					Message: "editing the content of an " + it.Status + " item requires items:review (metadata_patch is allowed)"}
			}
			if _, err := s.applyFields(ctx, q, it, in.itemFields); err != nil {
				return "", nil, err
			}
			if in.Source != nil {
				it.Source = strings.TrimSpace(*in.Source)
			}
			if in.ExternalRef != nil {
				it.ExternalRef = strings.TrimSpace(*in.ExternalRef)
			}
			if in.CollectionID != nil && *in.CollectionID != it.CollectionID {
				if _, err := q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: *in.CollectionID}); err != nil {
					return "", nil, invalid("collection_id", "unknown collection")
				}
				it.CollectionID = *in.CollectionID
			}
			typ := "item.updated"
			if in.Status != nil && *in.Status != it.Status {
				st := *in.Status
				if !validStatus(st) {
					return "", nil, invalid("status", "must be one of "+strings.Join(Statuses, ", "))
				}
				if st == "approved" || st == "rejected" {
					return "", nil, invalid("status", "use the approve/reject actions to decide an item")
				}
				if !writerTransition(it.Status, st) && !p.Has(ScopeItemsReview) {
					return "", nil, &Error{Code: "forbidden", Status: 403,
						Message: "changing status from " + it.Status + " to " + st + " requires items:review"}
				}
				it.Status = st
				typ = "item.status_changed"
			}
			return typ, nil, nil
		})
		return err
	})
	if err != nil {
		return Item{}, err
	}
	return s.toItem(ctx, out), nil
}

func (s *Service) DeleteItem(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeItemsWrite); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		it, err := q.GetItem(ctx, db.GetItemParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("item")
		}
		if err != nil {
			return err
		}
		dc, err := q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: it.CollectionID})
		if err != nil {
			return err
		}
		data := s.itemEventData(ctx, it, toCollection(dc), nil)
		if _, err := q.DeleteItem(ctx, db.DeleteItemParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "item.deleted", it.CollectionID, id, data)
		return err
	})
}

type ActionInput struct {
	Feedback        string          `json:"feedback"`
	Edits           *itemFields     `json:"edits"`
	Payload         json.RawMessage `json:"payload"`
	ExpectedVersion *int64          `json:"expected_version"`
}

// ItemEdits is the public name for the "edit before approval" fields.
type ItemEdits = itemFields

// RunAction executes approve, reject or a collection-defined action. The
// whole decision (state check, update, event, webhook outbox) is one
// transaction, and the status/version guard makes concurrent decisions
// resolve to exactly one winner; the loser gets 409.
func (s *Service) RunAction(ctx context.Context, p Principal, itemID, name string, in ActionInput) (Item, error) {
	if p.WorkspaceID == "" {
		return Item{}, ErrUnauthorized
	}
	feedback, err := trimmed(in.Feedback, "feedback", 5000, false)
	if err != nil {
		return Item{}, err
	}
	if in.Payload != nil && !json.Valid(in.Payload) {
		return Item{}, invalid("payload", "must be valid JSON")
	}
	var out db.Item
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		out, err = s.mutateItem(ctx, q, p, itemID, in.ExpectedVersion, func(it *db.Item, col Collection) (string, map[string]any, error) {
			var def *ActionDef
			for _, d := range col.AllActions() {
				if d.Name == name {
					def = &d
					break
				}
			}
			if def == nil {
				return "", nil, notFound("action " + name)
			}
			if !def.Builtin && reservedEvents[def.Event] {
				// Stored before reserved events were enforced: refuse to run.
				return "", nil, conflict("action " + name + " emits the reserved event " + def.Event + "; change its event in the collection settings")
			}
			scope := ScopeActionsExecute
			if def.Builtin {
				scope = ScopeItemsReview
			}
			if err := p.require(scope); err != nil {
				return "", nil, err
			}
			if len(def.AllowedStatuses) > 0 && !slices.Contains(def.AllowedStatuses, it.Status) {
				return "", nil, conflict(fmt.Sprintf("cannot %s an item that is %s (allowed from: %s)",
					name, it.Status, strings.Join(def.AllowedStatuses, ", ")))
			}
			if def.RequiresFeedback && feedback == "" {
				return "", nil, invalid("feedback", "is required for "+name)
			}
			var diff map[string]any
			if in.Edits != nil && !def.Builtin && !p.Has(ScopeItemsWrite) {
				return "", nil, &Error{Code: "forbidden", Status: 403, Message: "editing an item while running an action requires items:write"}
			}
			if in.Edits != nil {
				if err := checkEditable(it.Editable, *in.Edits); err != nil {
					return "", nil, err
				}
				before := *it
				if _, err := s.applyFields(ctx, q, it, *in.Edits); err != nil {
					return "", nil, err
				}
				diff = itemDiff(before, *it)
			}
			if def.SetStatus != "" {
				it.Status = def.SetStatus
			}
			if def.Builtin {
				it.DecidedAt = nullStr(s.now())
				it.Feedback = feedback
			} else if feedback != "" {
				it.Feedback = feedback
			}
			extra := map[string]any{"action": def.Name, "feedback": feedback, "edited": len(diff) > 0}
			if len(diff) > 0 {
				extra["diff"] = diff
			}
			if in.Payload != nil {
				extra["payload"] = in.Payload
			}
			return def.Event, extra, nil
		})
		return err
	})
	if err != nil {
		return Item{}, err
	}
	return s.toItem(ctx, out), nil
}

type BulkResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error *Error `json:"error,omitempty"`
	Item  *Item  `json:"item,omitempty"`
}

func (s *Service) BulkAction(ctx context.Context, p Principal, ids []string, name string, in ActionInput) ([]BulkResult, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, invalid("ids", "must contain 1-100 item ids")
	}
	in.ExpectedVersion = nil
	out := make([]BulkResult, 0, len(ids))
	for _, id := range ids {
		it, err := s.RunAction(ctx, p, id, name, in)
		r := BulkResult{ID: id, OK: err == nil}
		if err != nil {
			var de *Error
			if !asError(err, &de) {
				return nil, err
			}
			r.Error = de
		} else {
			r.Item = &it
		}
		out = append(out, r)
	}
	return out, nil
}

type ItemFilter struct {
	CollectionID, Status, Type, Source, ExternalRef, Q       string
	CreatedAfter, CreatedBefore, DecidedAfter, DecidedBefore string
	Sort                                                     string
	Limit                                                    int
	Cursor                                                   string
}

type ItemPage struct {
	Data       []Item `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func (s *Service) ListItems(ctx context.Context, p Principal, f ItemFilter) (ItemPage, error) {
	if err := p.require(ScopeItemsRead); err != nil {
		return ItemPage{}, err
	}
	if f.Status != "" && !validStatus(f.Status) {
		return ItemPage{}, invalid("status", "must be one of "+strings.Join(Statuses, ", "))
	}
	if f.Type != "" && !slices.Contains(ItemTypes, f.Type) {
		return ItemPage{}, invalid("type", "must be one of "+strings.Join(ItemTypes, ", "))
	}
	if f.Sort == "" {
		f.Sort = "created_desc"
		if db.FTSQuery(f.Q) != "" {
			f.Sort = "relevance"
		}
	}
	if !db.ValidItemSort(f.Sort) {
		return ItemPage{}, invalid("sort", "unknown sort")
	}
	search := db.ItemSearch{WorkspaceID: p.WorkspaceID, CollectionID: f.CollectionID, Status: f.Status, Type: f.Type,
		Source: f.Source, ExternalRef: f.ExternalRef, Query: f.Q, Sort: f.Sort, Cursor: f.Cursor, Limit: clampLimit(f.Limit, 50, 200)}
	for _, t := range []struct {
		field, v string
		dst      *string
	}{{"created_after", f.CreatedAfter, &search.CreatedAfter}, {"created_before", f.CreatedBefore, &search.CreatedBefore},
		{"decided_after", f.DecidedAfter, &search.DecidedAfter}, {"decided_before", f.DecidedBefore, &search.DecidedBefore}} {
		v, err := ParseTimeFilter(t.field, t.v)
		if err != nil {
			return ItemPage{}, err
		}
		if v != nil {
			*t.dst = v.(string)
		}
	}
	// A bounded query: pathological searches (or a huge workspace) must not
	// hold the database for long. Full-text searches that time out get 422
	// (refine the query); plain listings 503 (transient, retry).
	timeout := s.SearchTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	qctx, cancel := context.WithTimeout(ctx, timeout)
	rows, next, err := s.Q.SearchItems(qctx, search)
	timedOut := err != nil && qctx.Err() == context.DeadlineExceeded && ctx.Err() == nil
	cancel()
	if errors.Is(err, db.ErrBadCursor) {
		return ItemPage{}, invalid("cursor", "is invalid")
	}
	if timedOut {
		if db.FTSQuery(f.Q) != "" {
			return ItemPage{}, &Error{Code: "search_too_broad", Status: 422,
				Message: "search too broad: it took too long; add more specific words or filters", Fields: map[string]string{"q": "too broad"}}
		}
		return ItemPage{}, &Error{Code: "timeout", Status: 503, Message: "listing items took too long; try again or add filters"}
	}
	if err != nil {
		return ItemPage{}, err
	}
	page := ItemPage{Data: []Item{}, NextCursor: next}
	for _, r := range rows {
		page.Data = append(page.Data, s.toItem(ctx, r))
	}
	return page, nil
}

type ItemVersion struct {
	Version   int64           `json:"version"`
	Snapshot  json.RawMessage `json:"snapshot"`
	Actor     string          `json:"actor"`
	CreatedAt string          `json:"created_at"`
}

func (s *Service) ListItemVersions(ctx context.Context, p Principal, id string) ([]ItemVersion, error) {
	if _, err := s.GetItem(ctx, p, id); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListItemVersions(ctx, id)
	out := []ItemVersion{}
	for _, r := range rows {
		out = append(out, ItemVersion{Version: r.Version, Snapshot: json.RawMessage(r.Snapshot), Actor: r.Actor, CreatedAt: r.CreatedAt})
	}
	return out, err
}

// AvailableActions lists the actions the principal may run on the item now.
func (s *Service) AvailableActions(ctx context.Context, p Principal, itemID string) ([]ActionDef, error) {
	it, err := s.GetItem(ctx, p, itemID)
	if err != nil {
		return nil, err
	}
	col, err := s.GetCollection(ctx, p, it.CollectionID)
	if err != nil {
		return nil, err
	}
	out := []ActionDef{}
	for _, d := range col.AllActions() {
		scope := ScopeActionsExecute
		if d.Builtin {
			scope = ScopeItemsReview
		}
		if p.Has(scope) && (len(d.AllowedStatuses) == 0 || slices.Contains(d.AllowedStatuses, it.Status)) {
			out = append(out, d)
		}
	}
	return out, nil
}
