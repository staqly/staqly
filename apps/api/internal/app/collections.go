package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ActionDef is a catalog action as a collection enables it. Executing one records an event (and
// optionally moves the item's status); external systems do the work. Everything but the name and,
// for regenerate and publish, allowed_statuses is fixed by the catalog.
type ActionDef struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Event            string   `json:"event"`
	RequiresFeedback bool     `json:"requires_feedback"`
	SetStatus        string   `json:"set_status,omitempty"`
	AllowedStatuses  []string `json:"allowed_statuses,omitempty"`
	Builtin          bool     `json:"builtin,omitempty"`
	// RunLabels name the run an action starts (set_status processing), e.g. Publishing / Published /
	// Publish failed. Empty labels read Processing / Completed / Failed.
	RunLabels *RunLabels `json:"run_labels,omitempty"`
	// AutoRun (publish only) starts the action as soon as an item is approved, in the same transaction.
	AutoRun bool `json:"auto_run,omitempty"`
}

type RunLabels struct {
	Running string `json:"running,omitempty"`
	Done    string `json:"done,omitempty"`
	Failed  string `json:"failed,omitempty"`
}

// allowedFrom reports whether the action may run on an item in this status.
func (d ActionDef) allowedFrom(status string) bool {
	// While an automation works on an item nothing else runs: it reports a result (or a person marks it
	// completed, failed or canceled) first.
	if status == "processing" {
		return false
	}
	// A run starts after a decision: before it, the reviewer approves or rejects (and asks for a
	// regeneration from there), so "pending" and "regenerating" never mix.
	if runStatuses[d.SetStatus] && (status == "pending" || status == "draft") {
		return false
	}
	return len(d.AllowedStatuses) == 0 || slices.Contains(d.AllowedStatuses, status)
}

// Built-in review actions; they need items:review rather than actions:execute.
var builtinActions = []ActionDef{
	{Name: "approve", Label: "Approve", Event: "item.approved", SetStatus: "approved", AllowedStatuses: []string{"pending"}, Builtin: true},
	{Name: "reject", Label: "Reject", Event: "item.rejected", RequiresFeedback: true, SetStatus: "rejected", AllowedStatuses: []string{"pending"}, Builtin: true},
}

// catalogAction is one entry of the fixed action catalog. A collection only chooses which entries are
// enabled and, where Options is set, which of those statuses each may run from.
type catalogAction struct {
	Def ActionDef // fixed fields; AllowedStatuses is the default (empty: any status)
	// Options are the statuses a collection may pick from; nil means AllowedStatuses is fixed.
	Options []string
	// Required actions are always enabled, so every collection keeps the flow approve, publish,
	// done or failed, plus archive.
	Required bool
	// AutoRunnable actions may start on their own right after an approval (auto_run).
	AutoRunnable bool
}

// actionCatalog is the fixed set of collection actions, in display order.
var actionCatalog = []catalogAction{
	{Def: ActionDef{Name: "regenerate", Label: "Regenerate", Event: "item.regenerate_requested", RequiresFeedback: true, SetStatus: "processing",
		AllowedStatuses: []string{"rejected", "failed"}, RunLabels: &RunLabels{Running: "Regenerating", Done: "Regenerated", Failed: "Regeneration failed"}},
		Options: []string{"rejected", "failed", "expired"}},
	{Def: ActionDef{Name: "publish", Label: "Publish", Event: "item.publish_requested", SetStatus: "processing",
		AllowedStatuses: []string{"approved"}, RunLabels: &RunLabels{Running: "Publishing", Done: "Published", Failed: "Publish failed"}},
		Options: []string{"approved", "completed"}, Required: true, AutoRunnable: true},
	{Def: ActionDef{Name: "retry", Label: "Retry", Event: "item.retry_requested", SetStatus: "processing",
		AllowedStatuses: []string{"failed"}, RunLabels: &RunLabels{Running: "Retrying", Done: "Done", Failed: "Retry failed"}}},
	{Def: ActionDef{Name: "archive", Label: "Archive", Event: "item.archived", SetStatus: "archived"}, Required: true},
}

// def returns a copy of the catalog's fixed definition with the default allowed statuses.
func (c catalogAction) def() ActionDef {
	d := c.Def
	d.AllowedStatuses = slices.Clone(c.Def.AllowedStatuses)
	if c.Def.RunLabels != nil {
		l := *c.Def.RunLabels
		d.RunLabels = &l
	}
	return d
}

// options are the statuses an action may be allowed from: its choices, its fixed list, or any status.
func (c catalogAction) options() []string {
	switch {
	case c.Options != nil:
		return c.Options
	case len(c.Def.AllowedStatuses) > 0:
		return c.Def.AllowedStatuses
	default:
		return Statuses
	}
}

// DefaultActions are the whole catalog with default allowed statuses: what new collections get.
func DefaultActions() []ActionDef {
	out := []ActionDef{}
	for _, c := range actionCatalog {
		out = append(out, c.def())
	}
	return out
}

var Statuses = []string{"draft", "pending", "approved", "rejected", "expired", "processing", "completed", "failed", "archived"}

func validStatus(s string) bool {
	return slices.Contains(Statuses, s)
}

// catalogActions builds a collection's actions from what a client sent or the database holds. Catalog
// actions keep their enabled state and allowed statuses; every other field comes from the catalog, and
// required actions (publish, archive) are always present. A list without any catalog action means the
// defaults, so a collection never ends up without a flow. strict (client input) rejects unknown names and
// statuses outside an action's options with 422; otherwise (stored legacy data) they are dropped.
func catalogActions(defs []ActionDef, strict bool) ([]ActionDef, error) {
	byName := map[string]ActionDef{}
	for _, d := range defs {
		name := strings.TrimSpace(d.Name)
		i := slices.IndexFunc(actionCatalog, func(c catalogAction) bool { return c.Def.Name == name })
		if i < 0 {
			if strict {
				return nil, invalid("actions", "unknown action "+name+"; choose from regenerate, publish, retry, archive")
			}

			continue
		}
		if _, dup := byName[name]; dup && strict {
			return nil, invalid("actions", "duplicate action "+name)
		}
		byName[name] = d
	}
	if len(byName) == 0 {
		return DefaultActions(), nil
	}
	out := []ActionDef{}
	for _, c := range actionCatalog {
		in, ok := byName[c.Def.Name]
		if !ok && !c.Required {
			continue
		}
		d := c.def()
		picked := []string{}
		for _, st := range in.AllowedStatuses {
			if !slices.Contains(c.options(), st) {
				if strict {
					return nil, invalid("actions", c.Def.Name+" can run from "+strings.Join(c.options(), ", ")+", not "+st)
				}

				continue
			}
			picked = append(picked, st)
		}
		// Only regenerate and publish keep a choice (in catalog order); the others stay fixed.
		if c.Options != nil && len(picked) > 0 {
			d.AllowedStatuses = slices.DeleteFunc(slices.Clone(c.Options), func(st string) bool { return !slices.Contains(picked, st) })
		}
		if c.AutoRunnable && in.AutoRun {
			// Starting after an approval needs the action to be allowed from approved.
			if d.allowedFrom("approved") {
				d.AutoRun = true
			} else if strict {
				return nil, invalid("actions", c.Def.Name+" can start automatically after approval only when it may run from approved")
			}
		}
		out = append(out, d)
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
	IsDefault    bool           `json:"is_default"`
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
	var stored []ActionDef
	_ = json.Unmarshal([]byte(c.Actions), &stored)
	actions, _ := catalogActions(stored, false) // legacy custom actions and labels normalize on read
	return Collection{
		ID: c.ID, Name: c.Name, Description: c.Description, Icon: c.Icon, Color: c.Color,
		DefaultView: c.DefaultView, Actions: actions, IsDefault: c.IsDefault == 1, Archived: c.ArchivedAt.Valid, ArchivedAt: ptr(c.ArchivedAt),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, StatusCounts: map[string]int{},
	}
}

// AllActions returns built-ins followed by the collection's catalog actions.
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
		acts, err := catalogActions(*in.Actions, true)
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
	c := db.Collection{ID: newID("col"), WorkspaceID: p.WorkspaceID, DefaultView: "table",
		Actions: mustJSON(DefaultActions()), CreatedAt: now, UpdatedAt: now}
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

// ensureDefaultCollection creates the workspace's Default collection unless it has one.
func (s *Service) ensureDefaultCollection(ctx context.Context, q *db.Queries, workspaceID string) error {
	return q.CreateDefaultCollection(ctx, db.CreateDefaultCollectionParams{ID: newID("col"), WorkspaceID: workspaceID,
		Actions: mustJSON(DefaultActions()), Now: s.now()})
}

// EnsureDefaultCollections gives every workspace without one its Default
// collection (workspaces created before it existed). Run on start.
func (s *Service) EnsureDefaultCollections(ctx context.Context) error {
	ids, err := s.Q.WorkspacesWithoutDefaultCollection(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		for _, id := range ids {
			if err := s.ensureDefaultCollection(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
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
		if c.IsDefault == 1 && in.Archived != nil && *in.Archived {
			return conflict("the Default collection cannot be archived")
		}
		if in.Archived != nil && *in.Archived == wasArchived && onlyChange(in, "Archived") {
			if wasArchived {
				return alreadyErr("collection", "archived")
			}
			return conflict("collection is not archived")
		}
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
		if c.IsDefault == 1 {
			return conflict("the Default collection cannot be deleted")
		}
		if confirm != c.Name {
			return invalid("confirm", "must equal the collection name to delete it")
		}
		// Watchers that also add to other collections keep going with those.
		if err := q.RepointWatchers(ctx, db.RepointWatchersParams{WorkspaceID: p.WorkspaceID, CollectionID: id}); err != nil {
			return err
		}
		if _, err := q.DeleteCollection(ctx, db.DeleteCollectionParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "collection.deleted", id, "", map[string]any{"collection": toCollection(c)})
		return err
	})
	return err
}
