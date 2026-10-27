package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
)

// Collection groups posts (a brand, a client, a campaign) and owns the
// publishing schedules that take posts from it.
type Collection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	// ProcessingTimeoutSeconds: how long a custom target of this collection waits for your automation's
	// result before it fails (null = the server's PROCESSING_TIMEOUT).
	ProcessingTimeoutSeconds *int64         `json:"processing_timeout_seconds"`
	IsDefault                bool           `json:"is_default"`
	Archived                 bool           `json:"archived"`
	ArchivedAt               *string        `json:"archived_at"`
	CreatedAt                string         `json:"created_at"`
	UpdatedAt                string         `json:"updated_at"`
	PostCount                int64          `json:"post_count"`
	StatusCounts             map[string]int `json:"status_counts"`
}

// nullInt is a nullable integer column as a JSON number or null.
func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func toCollection(c db.Collection) Collection {
	return Collection{
		ID: c.ID, Name: c.Name, Description: c.Description, Icon: c.Icon, Color: c.Color,
		ProcessingTimeoutSeconds: nullInt(c.ProcessingTimeoutSeconds), IsDefault: c.IsDefault == 1,
		Archived: c.ArchivedAt.Valid, ArchivedAt: ptr(c.ArchivedAt),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, StatusCounts: map[string]int{},
	}
}

type CollectionInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Color       *string `json:"color"`
	// ProcessingTimeoutSeconds: 60 to 86400; null goes back to the server's PROCESSING_TIMEOUT.
	ProcessingTimeoutSeconds json.RawMessage `json:"processing_timeout_seconds"`
	Archived                 *bool           `json:"archived"`
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
	if len(in.ProcessingTimeoutSeconds) > 0 {
		if string(in.ProcessingTimeoutSeconds) == "null" {
			c.ProcessingTimeoutSeconds = sql.NullInt64{}
		} else {
			var secs int64
			if err := json.Unmarshal(in.ProcessingTimeoutSeconds, &secs); err != nil || secs < 60 || secs > 86400 {
				return invalid("processing_timeout_seconds", "must be between 60 and 86400 (1 minute to 24 hours), or null")
			}
			c.ProcessingTimeoutSeconds = sql.NullInt64{Int64: secs, Valid: true}
		}
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
	c := db.Collection{ID: newID("col"), WorkspaceID: p.WorkspaceID, DefaultView: "table", Actions: "[]", CreatedAt: now, UpdatedAt: now}
	if err := s.applyCollectionInput(&c, in); err != nil {
		return Collection{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateCollection(ctx, db.CreateCollectionParams{ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name,
			Description: c.Description, Icon: c.Icon, Color: c.Color, DefaultView: c.DefaultView,
			ProcessingTimeoutSeconds: c.ProcessingTimeoutSeconds, CreatedAt: now, UpdatedAt: now}); err != nil {
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
	return q.CreateDefaultCollection(ctx, db.CreateDefaultCollectionParams{ID: newID("col"), WorkspaceID: workspaceID, Now: s.now()})
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
	if store.UniqueViolation(err, "collections_name", "collections.workspace_id, collections.name") {
		return conflict("a collection with this name already exists")
	}
	return err
}

func (s *Service) GetCollection(ctx context.Context, p Principal, id string) (Collection, error) {
	if err := p.require(ScopeCollectionsRead); err != nil {
		return Collection{}, err
	}
	return s.getCollection(ctx, p, id)
}

// getCollection reads a collection of p's workspace without a scope check:
// for operations whose own scope already covers naming the collection they
// act on (creating or listing its posts).
func (s *Service) getCollection(ctx context.Context, p Principal, id string) (Collection, error) {
	if p.WorkspaceID == "" {
		return Collection{}, ErrUnauthorized
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
// agents naturally have ("post this to Instagram Avatar").
func (s *Service) FindCollection(ctx context.Context, p Principal, idOrName string) (Collection, error) {
	if err := p.require(ScopeCollectionsRead); err != nil {
		return Collection{}, err
	}
	return s.findCollection(ctx, p, idOrName)
}

// PostsCollection resolves the collection a post listing is narrowed to:
// reading posts (posts:read) covers naming their collection.
func (s *Service) PostsCollection(ctx context.Context, p Principal, idOrName string) (Collection, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return Collection{}, err
	}
	return s.findCollection(ctx, p, idOrName)
}

// findCollection is FindCollection without the scope check (see getCollection).
func (s *Service) findCollection(ctx context.Context, p Principal, idOrName string) (Collection, error) {
	if strings.HasPrefix(idOrName, "col_") {
		c, err := s.getCollection(ctx, p, idOrName)
		var e *Error
		if !errors.As(err, &e) || e.Status != 404 {
			return c, err
		}
		// Not an id after all: a collection may be named col_news.
	}
	if p.WorkspaceID == "" {
		return Collection{}, ErrUnauthorized
	}
	rows, err := s.Q.ListCollections(ctx, db.ListCollectionsParams{WorkspaceID: p.WorkspaceID, IncludeArchived: 1})
	if err != nil {
		return Collection{}, err
	}
	// The exact name first: names differing only in non-ASCII case (Żółw, żółw) can both exist, and the
	// case-insensitive match is only for a name that has no exact one.
	name := strings.TrimSpace(idOrName)
	match := slices.IndexFunc(rows, func(r db.Collection) bool { return r.Name == name })
	if match < 0 {
		match = slices.IndexFunc(rows, func(r db.Collection) bool { return strings.EqualFold(r.Name, name) })
	}
	if match < 0 {
		return Collection{}, notFound("collection")
	}
	out := []Collection{toCollection(rows[match])}
	if err := s.decorateCollections(ctx, p.WorkspaceID, out); err != nil {
		return Collection{}, err
	}
	return out[0], nil
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
	counts, err := s.Q.CollectionPostCounts(ctx, ws)
	if err != nil {
		return err
	}
	for _, r := range counts {
		if c := idx[r.CollectionID]; c != nil {
			c.StatusCounts[r.Status] = int(r.N)
			c.PostCount += r.N
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
		if in.Archived != nil && *in.Archived == wasArchived && in.Name == nil && in.Description == nil && in.Icon == nil &&
			in.Color == nil && len(in.ProcessingTimeoutSeconds) == 0 {
			if wasArchived {
				return conflict("collection is already archived")
			}
			return conflict("collection is not archived")
		}
		if err := s.applyCollectionInput(&c, in); err != nil {
			return err
		}
		c.UpdatedAt = s.now()
		if err := q.UpdateCollection(ctx, db.UpdateCollectionParams{Name: c.Name, Description: c.Description, Icon: c.Icon,
			Color: c.Color, DefaultView: c.DefaultView, ProcessingTimeoutSeconds: c.ProcessingTimeoutSeconds, ArchivedAt: c.ArchivedAt,
			UpdatedAt: c.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
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

// DeleteCollection permanently removes a collection and its schedules. Its posts are kept: every one moves to the
// workspace's Default collection as it is (drafts stay drafts, a scheduled post still goes out at its time, posts
// that went out keep their status and history), each with a post.updated event. confirm must equal the collection
// name, so a stray call cannot remove it.
func (s *Service) DeleteCollection(ctx context.Context, p Principal, id, confirm string) error {
	if err := p.require(ScopeCollectionsDelete); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
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
		def, err := q.GetDefaultCollection(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		// The posts first: the collection's foreign key would otherwise take them with it.
		postIDs, err := q.CollectionPostIDs(ctx, db.CollectionPostIDsParams{WorkspaceID: p.WorkspaceID, CollectionID: id})
		if err != nil {
			return err
		}
		if _, err := q.MoveCollectionPosts(ctx, db.MoveCollectionPostsParams{ToCollectionID: def.ID, UpdatedAt: s.now(),
			WorkspaceID: p.WorkspaceID, FromCollectionID: id}); err != nil {
			return err
		}
		// Its schedules go with it (foreign key): their queued runs are dropped too.
		schedules, err := q.ListSchedules(ctx, db.ListSchedulesParams{WorkspaceID: p.WorkspaceID, CollectionID: id})
		if err != nil {
			return err
		}
		for _, r := range schedules {
			if err := s.cancelJobs(ctx, q, "run_schedule:"+r.ID); err != nil {
				return err
			}
		}
		if _, err := q.DeleteCollection(ctx, db.DeleteCollectionParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		for _, r := range schedules {
			if _, err := s.emitRef(ctx, q, p, "schedule.deleted", Ref{Collection: id, Schedule: r.ID},
				map[string]any{"schedule": s.toSchedule(ctx, r)}); err != nil {
				return err
			}
		}
		moved := map[string]any{"id": c.ID, "name": c.Name}
		for _, postID := range postIDs {
			if _, err := s.emit(ctx, q, p, "post.updated", def.ID, postID,
				s.postEventData(ctx, q, p.WorkspaceID, postID, map[string]any{"moved_from": moved})); err != nil {
				return err
			}
		}
		_, err = s.emit(ctx, q, p, "collection.deleted", id, "", map[string]any{"collection": toCollection(c),
			"moved_posts": len(postIDs), "moved_to": map[string]any{"id": def.ID, "name": def.Name}})
		return err
	})
}

// processingTimeout is how long a custom target of this collection waits for
// a result: the collection's own value, else the server's (0 = never).
func (s *Service) processingTimeout(c db.Collection) time.Duration {
	if c.ProcessingTimeoutSeconds.Valid {
		return time.Duration(c.ProcessingTimeoutSeconds.Int64) * time.Second
	}
	if s.Cfg.ProcessingTimeoutNever {
		return 0
	}
	return s.Cfg.ProcessingTimeout
}

// ServerProcessingTimeoutSeconds is the server default as clients see it (0 = never).
func (s *Service) ServerProcessingTimeoutSeconds() int64 {
	return int64(s.Cfg.ProcessingTimeout / time.Second)
}
