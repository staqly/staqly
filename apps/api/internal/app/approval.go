package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ---- edit-before-approve whitelist and diff ----

func checkEditable(editableJSON string, e itemFields) error {
	var allowed []string
	_ = json.Unmarshal([]byte(editableJSON), &allowed)
	if len(allowed) == 0 {
		return nil
	}
	ok := func(f string) bool {
		for _, a := range allowed {
			if a == f {
				return true
			}
		}
		return false
	}
	deny := func(f string) error {
		return &Error{Code: "not_editable", Status: 422, Message: f + " is not editable for this item (editable: " + strings.Join(allowed, ", ") + ")",
			Fields: map[string]string{f: "not editable"}}
	}
	for f, set := range map[string]bool{"title": e.Title != nil, "content": e.Content != nil, "url": e.URL != nil, "media_id": e.MediaID != nil, "metadata": e.Metadata != nil} {
		if set && !ok(f) {
			return deny(f)
		}
	}
	for k := range e.MetadataPatch {
		if !ok("metadata") && !ok("metadata."+k) {
			return deny("metadata." + k)
		}
	}
	return nil
}

// itemDiff reports changed reviewable fields as {field: {before, after}}.
func itemDiff(before, after db.Item) map[string]any {
	d := map[string]any{}
	add := func(f string, b, a any) { d[f] = map[string]any{"before": b, "after": a} }
	if before.Title != after.Title {
		add("title", before.Title, after.Title)
	}
	if before.Content != after.Content {
		add("content", before.Content, after.Content)
	}
	if before.Url != after.Url {
		add("url", before.Url, after.Url)
	}
	if before.MediaID != after.MediaID {
		add("media_id", before.MediaID.String, after.MediaID.String)
	}
	if before.Metadata != after.Metadata {
		var b, a map[string]any
		_ = json.Unmarshal([]byte(before.Metadata), &b)
		_ = json.Unmarshal([]byte(after.Metadata), &a)
		for k, av := range a {
			if bv, ok := b[k]; !ok || mustJSON(bv) != mustJSON(av) {
				add("metadata."+k, b[k], av)
			}
		}
		for k, bv := range b {
			if _, ok := a[k]; !ok {
				add("metadata."+k, bv, nil)
			}
		}
	}
	return d
}

// ---- waiting for a decision ----

// changeBus wakes waiters after any committed write. Waiters re-check the DB,
// so a spurious wake-up is harmless; a 1 s poll covers writes made by another
// process (mcp-stdio) that can't signal us.
type changeBus struct {
	mu sync.Mutex
	ch chan struct{}
}

func (b *changeBus) wait() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ch == nil {
		b.ch = make(chan struct{})
	}
	return b.ch
}

func (b *changeBus) broadcast() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ch != nil {
		close(b.ch)
		b.ch = nil
	}
}

type Decision struct {
	Decided bool `json:"decided"`
	Item    Item `json:"item"`
}

// WaitForDecision blocks until the item leaves "pending" or timeout passes.
// It always returns the current item; Decided tells which happened.
func (s *Service) WaitForDecision(ctx context.Context, p Principal, id string, timeout time.Duration) (Decision, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		woke := s.changes.wait()
		it, err := s.GetItem(ctx, p, id)
		if err != nil {
			return Decision{}, err
		}
		if it.Status != "pending" {
			return Decision{Decided: true, Item: it}, nil
		}
		select {
		case <-woke:
		case <-poll.C:
		case <-deadline.C:
			return Decision{Item: it}, nil
		case <-ctx.Done():
			return Decision{Item: it}, ctx.Err()
		}
	}
}

// ---- reporting the outcome of an approved item ----

type ResultInput struct {
	Status string          `json:"status"` // completed | failed
	Result json.RawMessage `json:"result"` // any JSON, stored in metadata.result
	Error  string          `json:"error"`  // failure reason, stored in metadata.error
}

// ReportResult records what happened after approval (e.g. the post was
// published, or publishing failed), so the item's lifecycle is complete.
func (s *Service) ReportResult(ctx context.Context, p Principal, id string, in ResultInput) (Item, error) {
	if err := p.require(ScopeItemsWrite); err != nil {
		return Item{}, err
	}
	if in.Status != "completed" && in.Status != "failed" {
		return Item{}, invalid("status", "must be completed or failed")
	}
	if in.Result != nil && !json.Valid(in.Result) {
		return Item{}, invalid("result", "must be valid JSON")
	}
	errText, err := trimmed(in.Error, "error", 5000, false)
	if err != nil {
		return Item{}, err
	}
	var out db.Item
	err = s.tx(ctx, func(q *db.Queries) error {
		var err error
		out, err = s.mutateItem(ctx, q, p, id, nil, func(it *db.Item, _ Collection) (string, map[string]any, error) {
			if it.Status == in.Status {
				return "", nil, alreadyErr("item", it.Status)
			}
			if it.Status != "approved" && it.Status != "processing" {
				return "", nil, conflict("results can be reported for approved or processing items (item is " + it.Status + ")")
			}
			patch := map[string]json.RawMessage{}
			if in.Result != nil {
				patch["result"] = in.Result
			}
			if errText != "" {
				patch["error"] = json.RawMessage(mustJSON(errText))
			}
			if len(patch) > 0 {
				if _, err := s.applyFields(ctx, q, it, itemFields{MetadataPatch: patch}); err != nil {
					return "", nil, err
				}
			}
			it.Status = in.Status
			extra := map[string]any{"error": errText}
			if in.Result != nil {
				extra["result"] = in.Result
			}
			return "item." + in.Status, extra, nil
		})
		return err
	})
	if err != nil {
		return Item{}, err
	}
	return s.toItem(ctx, out), nil
}

// wasPendingBeforeArchive reports whether the item was waiting for review when it was archived: the newest
// version snapshot that is not itself archived is the item right before the archive (changes made while
// archived, e.g. a metadata_patch or a move to another collection, add archived snapshots on top).
func wasPendingBeforeArchive(ctx context.Context, q *db.Queries, id string) bool {
	versions, err := q.ListItemVersions(ctx, id)
	if err != nil {
		return false
	}
	for _, v := range versions { // newest first
		var prev struct {
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(v.Snapshot), &prev) != nil {
			return false
		}
		if prev.Status != "archived" {
			return prev.Status == "pending"
		}
	}
	return false
}

// Unarchive brings an archived item back where it was: pending if it was waiting for review, else its run (Published, Publish failed...) if one ran, else
// its decision, else a draft. No new decision is made, so items:write is enough, like archiving.
func (s *Service) Unarchive(ctx context.Context, p Principal, id string) (Item, error) {
	if err := p.require(ScopeItemsWrite); err != nil {
		return Item{}, err
	}
	var out db.Item
	err := s.tx(ctx, func(q *db.Queries) error {
		var err error
		out, err = s.mutateItem(ctx, q, p, id, nil, func(it *db.Item, _ Collection) (string, map[string]any, error) {
			if it.Status != "archived" {
				return "", nil, conflict("only archived items can be unarchived (item is " + it.Status + ")")
			}
			switch {
			// Archived while waiting for review (which recorded a rejection): back to review. The latest
			// version is the item as it was right before it was archived.
			case wasPendingBeforeArchive(ctx, q, it.ID):
				it.Status = "pending"
				it.DecidedAt = sql.NullString{}
			case it.Run != "" && it.Run != "none":
				it.Status = it.Run
			case it.Decision.Valid:
				it.Status = it.Decision.String
			default:
				it.Status = "draft"
			}
			// Bringing back a decision vouches for what the item holds now (metadata may have changed while
			// archived): a reviewer's call.
			if (it.Status == "approved" || it.Status == "rejected") && !p.Has(ScopeItemsReview) {
				return "", nil, &Error{Code: "forbidden", Status: 403, Message: "unarchiving back to " + it.Status + " requires items:review"}
			}
			return "item.unarchived", map[string]any{"status": it.Status}, nil
		})
		return err
	})
	if err != nil {
		return Item{}, err
	}
	return s.toItem(ctx, out), nil
}
