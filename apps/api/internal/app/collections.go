package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ActionDef is a collection-configured action. Executing one records an event
// (and optionally moves the item's status); external systems do the work.
type ActionDef struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Event            string   `json:"event"`
	RequiresFeedback bool     `json:"requires_feedback"`
	SetStatus        string   `json:"set_status,omitempty"`
	AllowedStatuses  []string `json:"allowed_statuses,omitempty"`
	Builtin          bool     `json:"builtin,omitempty"`
}

// Built-in review actions; they need items:review rather than actions:execute.
var builtinActions = []ActionDef{
	{Name: "approve", Label: "Approve", Event: "item.approved", SetStatus: "approved", AllowedStatuses: []string{"pending"}, Builtin: true},
	{Name: "reject", Label: "Reject", Event: "item.rejected", SetStatus: "rejected", AllowedStatuses: []string{"pending"}, Builtin: true},
}

// DefaultActions are attached to new collections when none are supplied.
var DefaultActions = []ActionDef{
	{Name: "regenerate", Label: "Regenerate", Event: "item.regenerate_requested", RequiresFeedback: true, SetStatus: "processing", AllowedStatuses: []string{"pending", "rejected", "failed"}},
	{Name: "publish", Label: "Publish", Event: "item.publish_requested", SetStatus: "processing", AllowedStatuses: []string{"approved"}},
	{Name: "retry", Label: "Retry", Event: "item.retry_requested", SetStatus: "processing", AllowedStatuses: []string{"failed"}},
	{Name: "archive", Label: "Archive", Event: "item.archived", SetStatus: "archived"},
}

var Statuses = []string{"draft", "pending", "approved", "rejected", "expired", "processing", "completed", "failed", "archived"}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

var (
	actionNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	actionEventRe = regexp.MustCompile(`^item\.[a-z0-9_.]{1,60}$`)
)

func validateActions(defs []ActionDef) ([]ActionDef, error) {
	seen := map[string]bool{"approve": true, "reject": true}
	out := []ActionDef{}
	for _, d := range defs {
		d.Name = strings.TrimSpace(d.Name)
		d.Builtin = false
		if !actionNameRe.MatchString(d.Name) {
			return nil, invalid("actions", "name must match ^[a-z][a-z0-9_]{0,39}$")
		}
		if seen[d.Name] {
			return nil, invalid("actions", "duplicate or reserved action name "+d.Name)
		}
		seen[d.Name] = true
		if d.Label = strings.TrimSpace(d.Label); d.Label == "" {
			d.Label = strings.ToUpper(d.Name[:1]) + strings.ReplaceAll(d.Name[1:], "_", " ")
		}
		if d.Event == "" {
			d.Event = "item." + d.Name + "_requested"
		}
		if !actionEventRe.MatchString(d.Event) {
			return nil, invalid("actions", "event must look like item.something")
		}
		if d.SetStatus != "" && !validStatus(d.SetStatus) {
			return nil, invalid("actions", "unknown set_status "+d.SetStatus)
		}
		// Review outcomes must go through approve/reject (items:review scope).
		if d.SetStatus == "approved" || d.SetStatus == "rejected" {
			return nil, invalid("actions", "custom actions cannot set approved/rejected; use the built-in approve/reject")
		}
		for _, st := range d.AllowedStatuses {
			if !validStatus(st) {
				return nil, invalid("actions", "unknown allowed status "+st)
			}
		}
		out = append(out, d)
	}
	if len(out) > 20 {
		return nil, invalid("actions", "at most 20 actions")
	}
	return out, nil
}

type Collection struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Icon         string         `json:"icon"`
	Color        string         `json:"color"`
	DefaultView  string         `json:"default_view"`
	Actions      []ActionDef    `json:"actions"`
	Archived     bool           `json:"archived"`
	ArchivedAt   *string        `json:"archived_at"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	ItemCount    int64          `json:"item_count"`
	StatusCounts map[string]int `json:"status_counts"`
	Cover        *Cover         `json:"cover,omitempty"`
}

type Cover struct {
	MediaID string `json:"media_id,omitempty"`
	URL     string `json:"url,omitempty"`
}

func toCollection(c db.Collection) Collection {
	var actions []ActionDef
	_ = json.Unmarshal([]byte(c.Actions), &actions)
	return Collection{
		ID: c.ID, Name: c.Name, Description: c.Description, Icon: c.Icon, Color: c.Color,
		DefaultView: c.DefaultView, Actions: actions, Archived: c.ArchivedAt.Valid, ArchivedAt: ptr(c.ArchivedAt),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, StatusCounts: map[string]int{},
	}
}

// AllActions returns built-ins followed by the collection's custom actions.
func (c Collection) AllActions() []ActionDef {
	return append(append([]ActionDef{}, builtinActions...), c.Actions...)
}

type CollectionInput struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	Icon        *string      `json:"icon"`
	Color       *string      `json:"color"`
	DefaultView *string      `json:"default_view"`
	Actions     *[]ActionDef `json:"actions"`
	Archived    *bool        `json:"archived"`
}

func (s *Service) applyCollectionInput(c *db.Collection, in CollectionInput) error {
	var err error
	if in.Name != nil {
		if c.Name, err = trimmed(*in.Name, "name", 100, true); err != nil {
			return err
		}
	}
	if in.Description != nil {
		if c.Description, err = trimmed(*in.Description, "description", 2000, false); err != nil {
			return err
		}
	}
	if in.Icon != nil {
		if c.Icon, err = trimmed(*in.Icon, "icon", 40, false); err != nil {
			return err
		}
	}
	if in.Color != nil {
		if c.Color, err = trimmed(*in.Color, "color", 20, false); err != nil {
			return err
		}
	}
	if in.DefaultView != nil {
		switch *in.DefaultView {
		case "gallery", "table", "list":
			c.DefaultView = *in.DefaultView
		default:
			return invalid("default_view", "must be gallery, table or list")
		}
	}
	if in.Actions != nil {
		acts, err := validateActions(*in.Actions)
		if err != nil {
			return err
		}
		c.Actions = mustJSON(acts)
	}
	if in.Archived != nil {
		switch {
		case *in.Archived && !c.ArchivedAt.Valid:
			c.ArchivedAt = nullStr(s.now())
		case !*in.Archived:
			c.ArchivedAt = nullStr("")
		}
	}
	return nil
}

func (s *Service) CreateCollection(ctx context.Context, p Principal, in CollectionInput) (Collection, error) {
	if err := p.require(ScopeCollectionsWrite); err != nil {
		return Collection{}, err
	}
	if in.Name == nil {
		return Collection{}, invalid("name", "is required")
	}
	now := s.now()
	c := db.Collection{ID: newID("col"), WorkspaceID: p.WorkspaceID, DefaultView: "gallery",
		Actions: mustJSON(DefaultActions), CreatedAt: now, UpdatedAt: now}
	if err := s.applyCollectionInput(&c, in); err != nil {
		return Collection{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateCollection(ctx, db.CreateCollectionParams{ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name,
			Description: c.Description, Icon: c.Icon, Color: c.Color, DefaultView: c.DefaultView, Actions: c.Actions,
			CreatedAt: now, UpdatedAt: now}); err != nil {
			return uniqueName(err)
		}
		_, err := s.emit(ctx, q, p, "collection.created", c.ID, "", map[string]any{"collection": toCollection(c)})
		return err
	})
	if err != nil {
		return Collection{}, err
	}
	return toCollection(c), nil
}

func uniqueName(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed: collections.workspace_id, collections.name") {
		return conflict("a collection with this name already exists")
	}
	return err
}

func (s *Service) GetCollection(ctx context.Context, p Principal, id string) (Collection, error) {
	if err := p.require(ScopeCollectionsRead); err != nil {
		return Collection{}, err
	}
	c, err := s.Q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return Collection{}, notFound("collection")
	}
	if err != nil {
		return Collection{}, err
	}
	out := []Collection{toCollection(c)}
	if err := s.decorateCollections(ctx, p.WorkspaceID, out); err != nil {
		return Collection{}, err
	}
	return out[0], nil
}

// FindCollection resolves an ID or (case-insensitive) name, which is what
// agents naturally have ("add this to Instagram Avatar").
func (s *Service) FindCollection(ctx context.Context, p Principal, idOrName string) (Collection, error) {
	if strings.HasPrefix(idOrName, "col_") {
		return s.GetCollection(ctx, p, idOrName)
	}
	all, err := s.ListCollections(ctx, p, true)
	if err != nil {
		return Collection{}, err
	}
	for _, c := range all {
		if strings.EqualFold(c.Name, strings.TrimSpace(idOrName)) {
			return c, nil
		}
	}
	return Collection{}, notFound("collection")
}

func (s *Service) ListCollections(ctx context.Context, p Principal, includeArchived bool) ([]Collection, error) {
	if err := p.require(ScopeCollectionsRead); err != nil {
		return nil, err
	}
	var inc int64
	if includeArchived {
		inc = 1
	}
	rows, err := s.Q.ListCollections(ctx, db.ListCollectionsParams{WorkspaceID: p.WorkspaceID, IncludeArchived: inc})
	if err != nil {
		return nil, err
	}
	out := make([]Collection, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCollection(r))
	}
	return out, s.decorateCollections(ctx, p.WorkspaceID, out)
}

func (s *Service) decorateCollections(ctx context.Context, ws string, cols []Collection) error {
	idx := map[string]*Collection{}
	for i := range cols {
		idx[cols[i].ID] = &cols[i]
	}
	counts, err := s.Q.CollectionStatusCounts(ctx, ws)
	if err != nil {
		return err
	}
	for _, r := range counts {
		if c := idx[r.CollectionID]; c != nil {
			c.StatusCounts[r.Status] = int(r.N)
			c.ItemCount += r.N
		}
	}
	covers, err := s.Q.CollectionCovers(ctx, ws)
	if err != nil {
		return err
	}
	for _, r := range covers {
		if c := idx[r.CollectionID]; c != nil && c.Cover == nil {
			c.Cover = &Cover{MediaID: r.MediaID.String, URL: r.Url}
		}
	}
	return nil
}

func (s *Service) UpdateCollection(ctx context.Context, p Principal, id string, in CollectionInput) (Collection, error) {
	if err := p.require(ScopeCollectionsWrite); err != nil {
		return Collection{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		c, err := q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("collection")
		}
		if err != nil {
			return err
		}
		wasArchived := c.ArchivedAt.Valid
		if err := s.applyCollectionInput(&c, in); err != nil {
			return err
		}
		c.UpdatedAt = s.now()
		if err := q.UpdateCollection(ctx, db.UpdateCollectionParams{Name: c.Name, Description: c.Description, Icon: c.Icon,
			Color: c.Color, DefaultView: c.DefaultView, Actions: c.Actions, ArchivedAt: c.ArchivedAt, UpdatedAt: c.UpdatedAt,
			WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return uniqueName(err)
		}
		typ := "collection.updated"
		if !wasArchived && c.ArchivedAt.Valid {
			typ = "collection.archived"
		}
		_, err = s.emit(ctx, q, p, typ, id, "", map[string]any{"collection": toCollection(c)})
		return err
	})
	if err != nil {
		return Collection{}, err
	}
	return s.GetCollection(ctx, p, id)
}

// DeleteCollection permanently removes a collection and its items. confirm
// must equal the collection name, so a stray call cannot wipe data.
func (s *Service) DeleteCollection(ctx context.Context, p Principal, id, confirm string) error {
	if err := p.require(ScopeCollectionsWrite); err != nil {
		return err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		c, err := q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("collection")
		}
		if err != nil {
			return err
		}
		if confirm != c.Name {
			return invalid("confirm", "must equal the collection name to delete it")
		}
		if _, err := q.DeleteCollection(ctx, db.DeleteCollectionParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "collection.deleted", id, "", map[string]any{"collection": toCollection(c)})
		return err
	})
	return err
}
