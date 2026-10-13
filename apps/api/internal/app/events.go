package app

import (
	"context"
	"encoding/json"
	"math"
	"path"
	"strconv"

	"github.com/staqly/staqly/apps/api/internal/db"
)

type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Event struct {
	ID           string          `json:"id"`
	Seq          int64           `json:"seq"`
	Type         string          `json:"type"`
	CollectionID *string         `json:"collection_id"`
	ItemID       *string         `json:"item_id"`
	Actor        Actor           `json:"actor"`
	Data         json.RawMessage `json:"data"`
	CreatedAt    string          `json:"created_at"`
}

func toEvent(e db.Event) Event {
	ev := Event{
		ID: e.ID, Seq: e.Seq, Type: e.Type,
		Actor:     Actor{Type: e.ActorType, ID: e.ActorID, Name: e.ActorName},
		Data:      json.RawMessage(e.Data),
		CreatedAt: e.CreatedAt,
	}
	if e.CollectionID.Valid {
		ev.CollectionID = &e.CollectionID.String
	}
	if e.ItemID.Valid {
		ev.ItemID = &e.ItemID.String
	}
	return ev
}

// emit records an immutable event and, in the same transaction, enqueues a
// delivery for every matching webhook (transactional outbox).
func (s *Service) emit(ctx context.Context, q *db.Queries, p Principal, typ, collectionID, itemID string, data any) (db.Event, error) {
	ev, err := q.InsertEvent(ctx, db.InsertEventParams{
		ID: newID("evt"), WorkspaceID: p.WorkspaceID, Type: typ,
		CollectionID: nullStr(collectionID), ItemID: nullStr(itemID),
		ActorType: p.Kind, ActorID: p.ID, ActorName: p.Name,
		Data: mustJSON(data), CreatedAt: s.now(),
	})
	if err != nil {
		return ev, err
	}
	for _, h := range emitHooks {
		if err := h(ctx, s, q, ev); err != nil {
			return ev, err
		}
	}
	hooks, err := q.ListActiveWebhooks(ctx, p.WorkspaceID)
	if err != nil {
		return ev, err
	}
	for _, h := range hooks {
		if !webhookMatches(h, typ, collectionID) {
			continue
		}
		if err := q.InsertDelivery(ctx, db.InsertDeliveryParams{
			ID: newID("dlv"), WorkspaceID: p.WorkspaceID, WebhookID: h.ID, EventID: ev.ID,
			EventType: typ, NextAttemptAt: ev.CreatedAt, CreatedAt: ev.CreatedAt,
		}); err != nil {
			return ev, err
		}
	}
	return ev, nil
}

// webhookMatches supports exact names, "*" and glob patterns like "item.*".
func webhookMatches(h db.Webhook, typ, collectionID string) bool {
	var cols []string
	_ = json.Unmarshal([]byte(h.CollectionIds), &cols)
	if len(cols) > 0 {
		found := false
		for _, c := range cols {
			found = found || c == collectionID
		}
		if !found {
			return false
		}
	}
	var pats []string
	_ = json.Unmarshal([]byte(h.Events), &pats)
	for _, pat := range pats {
		if ok, _ := path.Match(pat, typ); ok || pat == "*" {
			return true
		}
	}
	return false
}

type EventFilter struct {
	ItemID, CollectionID, Type, ActorType string
	Before                                string // seq cursor
	Limit                                 int
}

type EventPage struct {
	Data       []Event `json:"data"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

func (s *Service) ListEvents(ctx context.Context, p Principal, f EventFilter) (EventPage, error) {
	if err := p.require(ScopeItemsRead); err != nil {
		return EventPage{}, err
	}
	lim := clampLimit(f.Limit, 50, 200)
	var before any
	if f.Before != "" {
		n, err := strconv.ParseInt(f.Before, 10, 64)
		if err != nil {
			return EventPage{}, invalid("cursor", "is invalid")
		}
		before = n
	}
	var rows []db.Event
	var err error
	if f.ItemID != "" && f.CollectionID == "" && f.Type == "" && f.ActorType == "" {
		// Item history: a dedicated query that walks events_item directly.
		bs := int64(math.MaxInt64)
		if n, ok := before.(int64); ok {
			bs = n
		}
		rows, err = s.Q.ListItemEvents(ctx, db.ListItemEventsParams{WorkspaceID: p.WorkspaceID,
			ItemID: nullStr(f.ItemID), BeforeSeq: bs, Lim: int64(lim + 1)})
	} else {
		audit := 0
		if p.Has(ScopeAdmin) {
			audit = 1 // api_key.*, session.*, recovery_key.*, device*.* events
		}
		rows, err = s.Q.ListEvents(ctx, db.ListEventsParams{
			WorkspaceID: p.WorkspaceID, ItemID: optAny(f.ItemID), CollectionID: optAny(f.CollectionID),
			Type: optAny(f.Type), ActorType: optAny(f.ActorType), BeforeSeq: before, IncludeAudit: int64(audit), Lim: int64(lim + 1),
		})
	}
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Data: []Event{}}
	for i, r := range rows {
		if i == lim {
			page.NextCursor = strconv.FormatInt(rows[i-1].Seq, 10)
			break
		}
		page.Data = append(page.Data, toEvent(r))
	}
	return page, nil
}

func clampLimit(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
