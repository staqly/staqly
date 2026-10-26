package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// DesignBrand is the workspace's brand kit in the design editor: its colours, fonts (names from the editor's
// font list) and logos (pictures in the media library). Everyone who edits media shares it.
type DesignBrand struct {
	Colors []string `json:"colors"`
	Fonts  []string `json:"fonts"`
	Logos  []string `json:"logos"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// GetDesignBrand answers the brand kit (empty lists when none was set).
func (s *Service) GetDesignBrand(ctx context.Context, p Principal) (DesignBrand, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return DesignBrand{}, err
	}
	raw, err := s.Q.GetWorkspaceDesignBrand(ctx, p.WorkspaceID)
	if err != nil {
		return DesignBrand{}, err
	}
	var b DesignBrand
	_ = json.Unmarshal([]byte(raw), &b)
	if b.Colors == nil {
		b.Colors = []string{}
	}
	if b.Fonts == nil {
		b.Fonts = []string{}
	}
	if b.Logos == nil {
		b.Logos = []string{}
	}
	return b, nil
}

// SetDesignBrand replaces the brand kit: up to 24 colours (#rrggbb), 6 fonts and 12 logos of this workspace.
func (s *Service) SetDesignBrand(ctx context.Context, p Principal, in DesignBrand) (DesignBrand, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return DesignBrand{}, err
	}
	switch {
	case len(in.Colors) > 24:
		return DesignBrand{}, invalid("colors", "at most 24")
	case len(in.Fonts) > 6:
		return DesignBrand{}, invalid("fonts", "at most 6")
	case len(in.Logos) > 12:
		return DesignBrand{}, invalid("logos", "at most 12")
	}
	for i, c := range in.Colors {
		if !hexColor.MatchString(c) {
			return DesignBrand{}, invalid("colors", "each must look like #1f8a4c")
		}
		in.Colors[i] = strings.ToLower(c)
	}
	for _, f := range in.Fonts {
		if f = strings.TrimSpace(f); f == "" || len(f) > 60 {
			return DesignBrand{}, invalid("fonts", "each must be a font name of at most 60 characters")
		}
	}
	for _, id := range in.Logos {
		m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil || !strings.HasPrefix(m.MimeType, "image/") {
			return DesignBrand{}, invalid("logos", "each must be a picture in this workspace's media library")
		}
	}
	raw, _ := json.Marshal(in)
	if err := s.W.UpdateWorkspaceDesignBrand(ctx, db.UpdateWorkspaceDesignBrandParams{DesignBrand: string(raw), UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
		return DesignBrand{}, err
	}
	return s.GetDesignBrand(ctx, p)
}
