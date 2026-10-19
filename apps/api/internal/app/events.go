package app

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Agent names the MCP client or REST client (as listed under Agents) that acted, if any.
	Agent string `json:"agent,omitempty"`
}

type agentCtxKey struct{}

// WithAgent tags ctx with the agent making this request; events emitted with it name that agent.
func WithAgent(ctx context.Context, kind, name string) context.Context {
	return context.WithValue(ctx, agentCtxKey{}, agentName(kind, name))
}

func agentFrom(ctx context.Context) string {
	name, _ := ctx.Value(agentCtxKey{}).(string)
	return name
}

type Event struct {
	ID           string          `json:"id"`
	Seq          int64           `json:"seq"`
	Type         string          `json:"type"`
	CollectionID *string         `json:"collection_id"`
	PostID       *string         `json:"post_id"`
	ChannelID    *string         `json:"channel_id,omitempty"`
	ScheduleID   *string         `json:"schedule_id,omitempty"`
	Actor        Actor           `json:"actor"`
	Data         json.RawMessage `json:"data"`
	CreatedAt    string          `json:"created_at"`
}

func toEvent(e db.Event) Event {
	ev := Event{
		ID: e.ID, Seq: e.Seq, Type: e.Type,
		Actor:     Actor{Type: e.ActorType, ID: e.ActorID, Name: e.ActorName, Agent: e.Agent},
		Data:      json.RawMessage(e.Data),
		CreatedAt: e.CreatedAt,
	}
	if e.CollectionID.Valid {
		ev.CollectionID = &e.CollectionID.String
	}
	ev.PostID, ev.ChannelID, ev.ScheduleID = ptr(e.ItemID), ptr(e.ChannelID), ptr(e.ScheduleID)
	return ev
}

// Ref names what an event is about; every part is optional.
type Ref struct {
	Collection, Post, Channel, Schedule string
}

// emit records an immutable event and, in the same transaction, enqueues a
// delivery for every matching webhook (transactional outbox).
func (s *Service) emit(ctx context.Context, q *db.Queries, p Principal, typ, collectionID, postID string, data any) (db.Event, error) {
	return s.emitRef(ctx, q, p, typ, Ref{Collection: collectionID, Post: postID}, data)
}

func (s *Service) emitRef(ctx context.Context, q *db.Queries, p Principal, typ string, ref Ref, data any) (db.Event, error) {
	ev, err := q.InsertEvent(ctx, db.InsertEventParams{
		ID: newID("evt"), WorkspaceID: p.WorkspaceID, Type: typ,
		CollectionID: nullStr(ref.Collection), ItemID: nullStr(ref.Post), ChannelID: nullStr(ref.Channel), ScheduleID: nullStr(ref.Schedule),
		ActorType: p.Kind, ActorID: p.ID, ActorName: p.Name, Agent: agentFrom(ctx),
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
	if strings.HasPrefix(typ, "webhook.") {
		// Audit only: delivering changes of webhooks to webhooks would loop
		// back to the receiver being configured.
		return ev, nil
	}
	hooks, err := q.ListActiveWebhooks(ctx, p.WorkspaceID)
	if err != nil {
		return ev, err
	}
	for _, h := range hooks {
		if !webhookMatches(h, typ, ref.Collection) {
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

// webhookMatches supports exact names, "*" and glob patterns like "item.*" (see eventMatches).
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
		if eventMatches(pat, typ) {
			return true
		}
	}
	return false
}

type EventFilter struct {
	PostID, CollectionID, ChannelID, ScheduleID, Type, ActorType string
	ActorID, TypePrefix                                          string
	Agents                                                       []string // any of these agent names
	From, To                                                     string   // RFC 3339 or YYYY-MM-DD; from inclusive, to exclusive
	Before                                                       string   // seq cursor
	Limit                                                        int
}

type EventPage struct {
	Data       []Event `json:"data"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

func (s *Service) ListEvents(ctx context.Context, p Principal, f EventFilter) (EventPage, error) {
	if err := p.require(ScopeEventsRead); err != nil {
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
	from, err := ParseTimeFilter("from", f.From)
	if err != nil {
		return EventPage{}, err
	}
	to, err := ParseTimeFilter("to", f.To)
	if err != nil {
		return EventPage{}, err
	}
	var rows []db.Event
	onlyPost := f.CollectionID == "" && f.ChannelID == "" && f.ScheduleID == "" && f.Type == "" && f.ActorType == "" && f.ActorID == "" &&
		f.TypePrefix == "" && from == nil && to == nil && len(f.Agents) == 0
	var agents any
	if len(f.Agents) > 0 {
		agents = mustJSON(f.Agents)
	}
	if f.PostID != "" && onlyPost {
		// Post history: a dedicated query that walks events_item directly.
		bs := int64(math.MaxInt64)
		if n, ok := before.(int64); ok {
			bs = n
		}
		rows, err = s.Q.ListPostEvents(ctx, db.ListPostEventsParams{WorkspaceID: p.WorkspaceID,
			PostID: nullStr(f.PostID), BeforeSeq: bs, Lim: int64(lim + 1)})
	} else {
		audit := 0
		if p.Has(ScopeAdmin) {
			audit = 1 // api_key.*, session.*, recovery_key.*, device*.* events
		}
		rows, err = s.Q.ListEvents(ctx, db.ListEventsParams{
			WorkspaceID: p.WorkspaceID, PostID: optAny(f.PostID), CollectionID: optAny(f.CollectionID),
			ChannelID: optAny(f.ChannelID), ScheduleID: optAny(f.ScheduleID), Type: optAny(f.Type), ActorType: optAny(f.ActorType), ActorID: optAny(f.ActorID), Agents: agents, TypePrefix: optAny(f.TypePrefix),
			FromTs: from, ToTs: to, BeforeSeq: before, IncludeAudit: int64(audit), Lim: int64(lim + 1),
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
