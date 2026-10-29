package app

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ChannelGroup is the one structural bucket a channel sits in ("Client A", "Personal"). Every workspace
// has a Default group, which can't be deleted; a deleted group's channels move to Default.
type ChannelGroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	// Icon is one of channelGroupIcons, '' for none.
	Icon     string `json:"icon"`
	Position int64  `json:"position"`
	Default  bool   `json:"default"`
	// Channels in the group (list only).
	Channels  int64  `json:"channels"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ChannelGroupInput creates a group or (PATCH, nil fields untouched) changes one.
type ChannelGroupInput struct {
	Name     *string `json:"name"`
	Color    *string `json:"color"`
	Icon     *string `json:"icon"`
	Position *int64  `json:"position"`
}

const maxChannelGroups = 100

var reHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// channelGroupIcons are the icons a group can have; the dashboard draws each (components/channels/group-icon.tsx).
var channelGroupIcons = []string{"briefcase", "building", "store", "house", "user", "users", "heart", "star",
	"rocket", "sparkles", "zap", "flame", "trophy", "globe", "megaphone", "camera", "music", "gamepad", "coffee",
	"shopping-bag", "graduation-cap", "palette", "code", "leaf", "car", "plane", "bike", "utensils", "pizza", "wine", "dumbbell", "book", "film", "tv", "mic", "headphones", "gift", "shirt", "gem", "crown", "paw-print", "flower", "tree", "mountain", "sun", "moon", "lightbulb", "target", "flag", "bell", "mail", "newspaper", "landmark", "banknote", "shield", "bot"}

func toChannelGroup(g db.ChannelGroup, channels int64) ChannelGroup {
	return ChannelGroup{ID: g.ID, Name: g.Name, Color: g.Color, Icon: g.Icon, Position: g.Position, Default: g.IsDefault == 1,
		Channels: channels, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

// defaultChannelGroup returns the workspace's Default group, creating it on first use (workspaces made after the
// migration get theirs here).
func (s *Service) defaultChannelGroup(ctx context.Context, q *db.Queries, workspaceID string) (db.ChannelGroup, error) {
	g, err := q.GetDefaultChannelGroup(ctx, workspaceID)
	if err == nil || !isNoRows(err) {
		return g, err
	}
	now := s.now()
	if err := q.InsertDefaultChannelGroup(ctx, db.InsertDefaultChannelGroupParams{ID: newID("grp"), WorkspaceID: workspaceID,
		CreatedAt: now, UpdatedAt: now}); err != nil {
		return g, err
	}
	return q.GetDefaultChannelGroup(ctx, workspaceID)
}

// groupResolver maps a channel's stored group_id to the group it is in: ” or a group that is gone reads as Default.
func (s *Service) groupResolver(ctx context.Context, q *db.Queries, workspaceID string) (func(string) string, error) {
	def, err := s.defaultChannelGroup(ctx, q, workspaceID)
	if err != nil {
		return nil, err
	}
	groups, err := q.ListChannelGroups(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(groups))
	for _, g := range groups {
		known[g.ID] = true
	}
	return func(id string) string {
		if known[id] {
			return id
		}
		return def.ID
	}, nil
}

// channelGroupFor checks a group id given by a person (empty = Default) and returns the group to store.
func (s *Service) channelGroupFor(ctx context.Context, q *db.Queries, workspaceID, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		def, err := s.defaultChannelGroup(ctx, q, workspaceID)
		return def.ID, err
	}
	g, err := q.GetChannelGroup(ctx, db.GetChannelGroupParams{WorkspaceID: workspaceID, ID: id})
	if isNoRows(err) {
		return "", invalid("group_id", "unknown channel group")
	}
	return g.ID, err
}

func (s *Service) ListChannelGroups(ctx context.Context, p Principal) ([]ChannelGroup, error) {
	if err := p.require(ScopeChannelsRead); err != nil {
		return nil, err
	}
	var out []ChannelGroup
	err := s.tx(ctx, func(q *db.Queries) error {
		resolve, err := s.groupResolver(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		counts, err := q.CountChannelsByGroup(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		n := map[string]int64{}
		for _, c := range counts {
			n[resolve(c.GroupID)] += c.Channels
		}
		groups, err := q.ListChannelGroups(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		out = make([]ChannelGroup, 0, len(groups))
		for _, g := range groups {
			out = append(out, toChannelGroup(g, n[g.ID]))
		}
		return nil
	})
	return out, err
}

// checkGroupFields validates a name (unique in the workspace, case-insensitive), a colour and an icon.
func checkGroupFields(groups []db.ChannelGroup, selfID string, name, color, icon *string) error {
	if name != nil {
		n, err := trimmed(*name, "name", 60, true)
		if err != nil {
			return err
		}
		for _, g := range groups {
			if g.ID != selfID && strings.EqualFold(g.Name, n) {
				return &Error{Code: "conflict", Status: 409, Message: "a channel group with this name already exists",
					Fields: map[string]string{"name": "A group with this name already exists"}}
			}
		}
		*name = n
	}
	if color != nil && *color != "" && !reHexColor.MatchString(*color) {
		return invalid("color", "a colour like #22c55e, or empty")
	}
	if icon != nil && *icon != "" && !slices.Contains(channelGroupIcons, *icon) {
		return invalid("icon", "one of "+strings.Join(channelGroupIcons, ", ")+", or empty")
	}
	return nil
}

func (s *Service) CreateChannelGroup(ctx context.Context, p Principal, in ChannelGroupInput) (ChannelGroup, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return ChannelGroup{}, err
	}
	if in.Name == nil {
		return ChannelGroup{}, invalid("name", "name is required")
	}
	var out db.ChannelGroup
	err := s.tx(ctx, func(q *db.Queries) error {
		if _, err := s.defaultChannelGroup(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		groups, err := q.ListChannelGroups(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if len(groups) >= maxChannelGroups {
			return invalid("name", "at most 100 channel groups")
		}
		if err := checkGroupFields(groups, "", in.Name, in.Color, in.Icon); err != nil {
			return err
		}
		pos := int64(len(groups))
		if in.Position != nil {
			pos = *in.Position
		}
		now := s.now()
		out = db.ChannelGroup{ID: newID("grp"), WorkspaceID: p.WorkspaceID, Name: *in.Name, Color: deref(in.Color), Icon: deref(in.Icon), Position: pos,
			CreatedAt: now, UpdatedAt: now}
		return q.CreateChannelGroup(ctx, db.CreateChannelGroupParams{ID: out.ID, WorkspaceID: out.WorkspaceID, Name: out.Name,
			Color: out.Color, Icon: out.Icon, Position: out.Position, CreatedAt: now, UpdatedAt: now})
	})
	return toChannelGroup(out, 0), err
}

func (s *Service) UpdateChannelGroup(ctx context.Context, p Principal, id string, in ChannelGroupInput) (ChannelGroup, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return ChannelGroup{}, err
	}
	var out db.ChannelGroup
	err := s.tx(ctx, func(q *db.Queries) error {
		g, err := q.GetChannelGroup(ctx, db.GetChannelGroupParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("channel group")
		}
		if err != nil {
			return err
		}
		groups, err := q.ListChannelGroups(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if err := checkGroupFields(groups, g.ID, in.Name, in.Color, in.Icon); err != nil {
			return err
		}
		if in.Name != nil {
			g.Name = *in.Name
		}
		if in.Color != nil {
			g.Color = *in.Color
		}
		if in.Icon != nil {
			g.Icon = *in.Icon
		}
		if in.Position != nil {
			g.Position = *in.Position
		}
		g.UpdatedAt = s.now()
		out = g
		return q.UpdateChannelGroup(ctx, db.UpdateChannelGroupParams{Name: g.Name, Color: g.Color, Icon: g.Icon, Position: g.Position,
			UpdatedAt: g.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: g.ID})
	})
	return toChannelGroup(out, 0), err
}

// DeleteChannelGroup removes a group; its channels move to the Default group, which itself can't be deleted.
func (s *Service) DeleteChannelGroup(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeChannelsManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		g, err := q.GetChannelGroup(ctx, db.GetChannelGroupParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("channel group")
		}
		if err != nil {
			return err
		}
		if g.IsDefault == 1 {
			return &Error{Code: "default_group", Status: 409, Message: "the Default group can't be deleted"}
		}
		def, err := s.defaultChannelGroup(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		if err := q.MoveGroupChannels(ctx, db.MoveGroupChannelsParams{ToGroup: def.ID, UpdatedAt: s.now(),
			WorkspaceID: p.WorkspaceID, FromGroup: g.ID}); err != nil {
			return err
		}
		_, err = q.DeleteChannelGroup(ctx, db.DeleteChannelGroupParams{WorkspaceID: p.WorkspaceID, ID: g.ID})
		return err
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
