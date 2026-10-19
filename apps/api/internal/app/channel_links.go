package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Connect links: someone without access to Staqly (a customer, a colleague) opens the link and connects a channel
// to this workspace. They see the workspace's name, the team's note and the platforms the link allows, sign in at
// the platform (or enter its token) and that's all: no session, no access to anything else. Only the token's hash
// is stored; a link is open until it expires, is revoked or its uses are taken.

const (
	channelLinkDefaultHours = 7 * 24
	channelLinkMaxHours     = 30 * 24
	channelLinkMaxUses      = 50
)

type ChannelLink struct {
	ID        string   `json:"id"`
	Note      string   `json:"note"`
	Providers []string `json:"providers"` // empty: any platform the server offers
	GroupID   string   `json:"group_id"`  // the channel group a connected channel joins ("": Default)
	MaxUses   int64    `json:"max_uses"`
	Uses      int64    `json:"uses"`
	Status    string   `json:"status"` // open, used, expired, revoked
	CreatedBy string   `json:"created_by"`
	CreatedAt string   `json:"created_at"`
	ExpiresAt string   `json:"expires_at"`
	RevokedAt string   `json:"revoked_at,omitempty"`
	// Only in the answer that creates the link: the secret and the page to send.
	Token string `json:"token,omitempty"`
	URL   string `json:"url,omitempty"`
}

type ChannelLinkInput struct {
	Note           string   `json:"note"`
	Providers      []string `json:"providers"`
	GroupID        string   `json:"group_id"`
	ExpiresInHours int      `json:"expires_in_hours"` // 0: 7 days; at most 30 days
	MaxUses        int      `json:"max_uses"`         // 0: 1
}

// ChannelLinkPublic is what the link's page may show: nothing about the workspace beyond its name.
type ChannelLinkPublic struct {
	Workspace string         `json:"workspace"`
	Note      string         `json:"note"`
	Providers []ProviderInfo `json:"providers"`
	ExpiresAt string         `json:"expires_at"`
	UsesLeft  int64          `json:"uses_left"`
}

func (s *Service) toChannelLink(l db.ChannelLink) ChannelLink {
	out := ChannelLink{ID: l.ID, Note: l.Note, Providers: []string{}, GroupID: l.GroupID, MaxUses: l.MaxUses, Uses: l.Uses,
		CreatedBy: l.CreatedByName, CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt, RevokedAt: l.RevokedAt.String}
	_ = json.Unmarshal([]byte(l.Providers), &out.Providers)
	out.Status = s.channelLinkStatus(l)
	return out
}

func (s *Service) channelLinkStatus(l db.ChannelLink) string {
	switch {
	case l.RevokedAt.Valid:
		return "revoked"
	case l.ExpiresAt <= s.now():
		return "expired"
	case l.Uses >= l.MaxUses:
		return "used"
	}
	return "open"
}

// linkPlatformOK: a link offers real platforms only (your own automation is the team's to set up).
func (s *Service) linkPlatformOK(id string) bool {
	e, ok := providers.Get(id)
	return ok && id != "custom" && e.Status == providers.StatusAvailable && s.providerEnabled(id)
}

// CreateChannelLink makes a connect link. channels:manage, like connecting a channel yourself: an API key with
// that scope may already connect channels directly, so an agency's automation may send links too.
func (s *Service) CreateChannelLink(ctx context.Context, p Principal, in ChannelLinkInput, origin string) (ChannelLink, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return ChannelLink{}, err
	}
	note, err := trimmed(in.Note, "note", 300, false)
	if err != nil {
		return ChannelLink{}, err
	}
	provs := []string{}
	for _, id := range in.Providers {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(provs, id) {
			continue
		}
		if !s.linkPlatformOK(id) {
			return ChannelLink{}, invalid("providers", id+" can't be connected through a link on this server")
		}
		provs = append(provs, id)
	}
	group := strings.TrimSpace(in.GroupID)
	if group != "" {
		if _, err := s.Q.GetChannelGroup(ctx, db.GetChannelGroupParams{WorkspaceID: p.WorkspaceID, ID: group}); isNoRows(err) {
			return ChannelLink{}, invalid("group_id", "unknown channel group")
		} else if err != nil {
			return ChannelLink{}, err
		}
	}
	hours := in.ExpiresInHours
	if hours == 0 {
		hours = channelLinkDefaultHours
	}
	if hours < 1 || hours > channelLinkMaxHours {
		return ChannelLink{}, invalid("expires_in_hours", "must be 1 to 720 hours (30 days)")
	}
	uses := in.MaxUses
	if uses == 0 {
		uses = 1
	}
	if uses < 1 || uses > channelLinkMaxUses {
		return ChannelLink{}, invalid("max_uses", "must be 1 to 50 channels")
	}
	token := randomString(32)
	now := s.Now().UTC()
	row := db.ChannelLink{ID: newID("chl"), WorkspaceID: p.WorkspaceID, TokenHash: hashSecret(token), Note: note,
		Providers: mustJSON(provs), GroupID: group, MaxUses: int64(uses), CreatedBy: p.Kind + ":" + p.ID,
		CreatedByName: p.Name, CreatedAt: formatTime(now), ExpiresAt: formatTime(now.Add(time.Duration(hours) * time.Hour))}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateChannelLink(ctx, db.CreateChannelLinkParams{ID: row.ID, WorkspaceID: row.WorkspaceID,
			TokenHash: row.TokenHash, Note: row.Note, Providers: row.Providers, GroupID: row.GroupID, MaxUses: row.MaxUses,
			CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "channel_link.created", "", "", map[string]any{"channel_link_id": row.ID,
			"expires_at": row.ExpiresAt, "max_uses": row.MaxUses})
		return err
	})
	if err != nil {
		return ChannelLink{}, err
	}
	out := s.toChannelLink(row)
	out.Token = token
	// The token travels in the fragment: it never reaches server logs, proxies or Referer headers.
	out.URL = s.dashboardBase(origin) + "/add-channel#" + token
	return out, nil
}

func (s *Service) ListChannelLinks(ctx context.Context, p Principal) ([]ChannelLink, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListChannelLinks(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toChannelLink(r))
	}
	return out, nil
}

func (s *Service) RevokeChannelLink(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeChannelsManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.RevokeChannelLink(ctx, db.RevokeChannelLinkParams{Now: nullStr(s.now()), WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := q.GetChannelLink(ctx, db.GetChannelLinkParams{WorkspaceID: p.WorkspaceID, ID: id}); isNoRows(err) {
				return notFound("connect link")
			}
			return nil // already revoked
		}
		_, err = s.emit(ctx, q, p, "channel_link.revoked", "", "", map[string]any{"channel_link_id": id})
		return err
	})
}

// openChannelLink finds an open link by its token; a closed one says why, so the page can too.
func (s *Service) openChannelLink(ctx context.Context, token string) (db.ChannelLink, error) {
	l, err := s.Q.GetChannelLinkByHash(ctx, hashSecret(strings.TrimSpace(token)))
	if isNoRows(err) || strings.TrimSpace(token) == "" {
		return l, &Error{Code: "invalid_link", Status: 404, Message: "this link is not valid; ask for a new one"}
	}
	if err != nil {
		return l, err
	}
	return l, s.channelLinkOpen(l)
}

func (s *Service) channelLinkOpen(l db.ChannelLink) error {
	switch s.channelLinkStatus(l) {
	case "revoked":
		return &Error{Code: "link_revoked", Status: 410, Message: "this link was turned off; ask for a new one"}
	case "expired":
		return &Error{Code: "link_expired", Status: 410, Message: "this link expired; ask for a new one"}
	case "used":
		return &Error{Code: "link_used", Status: 410, Message: "this link was already used; ask for a new one"}
	}
	return nil
}

// linkPrincipal acts for a customer on a connect link: it may connect channels in the link's workspace, nothing else.
func linkPrincipal(l db.ChannelLink) Principal {
	name := "Connected via link"
	if l.Note != "" {
		name += ": " + l.Note
	}
	return Principal{WorkspaceID: l.WorkspaceID, Kind: PrincipalSystem, ID: "channel_link:" + l.ID, Name: name,
		Scopes: []string{ScopeChannelsRead, ScopeChannelsManage}}
}

// linkProviders: the platforms a link allows that this server can connect now.
func (s *Service) linkProviders(ctx context.Context, l db.ChannelLink) ([]ProviderInfo, error) {
	var allowed []string
	_ = json.Unmarshal([]byte(l.Providers), &allowed)
	all, err := s.providerCatalog(ctx, l.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := []ProviderInfo{}
	for _, p := range all {
		if p.Status != providers.StatusAvailable || !s.linkPlatformOK(p.ID) || (len(allowed) > 0 && !slices.Contains(allowed, p.ID)) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// OpenChannelLink is the public page's data: the workspace's name, the note and the platforms to pick from.
func (s *Service) OpenChannelLink(ctx context.Context, token string) (ChannelLinkPublic, error) {
	l, err := s.openChannelLink(ctx, token)
	if err != nil {
		return ChannelLinkPublic{}, err
	}
	ws, err := s.Q.GetWorkspace(ctx, l.WorkspaceID)
	if err != nil {
		return ChannelLinkPublic{}, err
	}
	provs, err := s.linkProviders(ctx, l)
	if err != nil {
		return ChannelLinkPublic{}, err
	}
	return ChannelLinkPublic{Workspace: ws.Name, Note: l.Note, Providers: provs, ExpiresAt: l.ExpiresAt,
		UsesLeft: l.MaxUses - l.Uses}, nil
}

func (s *Service) linkAllows(ctx context.Context, l db.ChannelLink, providerID string) (providers.Entry, error) {
	provs, err := s.linkProviders(ctx, l)
	if err != nil {
		return providers.Entry{}, err
	}
	if !slices.ContainsFunc(provs, func(p ProviderInfo) bool { return p.ID == providerID }) {
		return providers.Entry{}, invalid("provider", "this link doesn't offer that platform")
	}
	return s.availableProvider(providerID)
}

// takeChannelLinkUse claims one use before connecting; release gives it back when connecting fails.
func (s *Service) takeChannelLinkUse(ctx context.Context, l db.ChannelLink) (release func(), err error) {
	n, err := s.Q.UseChannelLink(ctx, db.UseChannelLinkParams{Now: s.now(), WorkspaceID: l.WorkspaceID, ID: l.ID})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		l.Uses = l.MaxUses // someone took the last use, or it closed meanwhile
		if err := s.channelLinkOpen(l); err != nil {
			return nil, err
		}
		return nil, &Error{Code: "link_used", Status: 410, Message: "this link was already used; ask for a new one"}
	}
	return func() {
		_ = s.Q.ReleaseChannelLink(context.WithoutCancel(ctx), db.ReleaseChannelLinkParams{WorkspaceID: l.WorkspaceID, ID: l.ID})
	}, nil
}

// linkGroup is the group a channel connected through the link joins; a group deleted since reads as Default.
func (s *Service) linkGroup(ctx context.Context, l db.ChannelLink) string {
	if l.GroupID == "" {
		return ""
	}
	if _, err := s.Q.GetChannelGroup(ctx, db.GetChannelGroupParams{WorkspaceID: l.WorkspaceID, ID: l.GroupID}); err != nil {
		return ""
	}
	return l.GroupID
}

// ConnectChannelViaLink connects a platform that signs in with a token, entered on the link's page.
func (s *Service) ConnectChannelViaLink(ctx context.Context, token string, in ChannelInput) (Channel, error) {
	l, err := s.openChannelLink(ctx, token)
	if err != nil {
		return Channel{}, err
	}
	e, err := s.linkAllows(ctx, l, in.Provider)
	if err != nil {
		return Channel{}, err
	}
	if e.Auth() == providers.AuthOAuth2 {
		return Channel{}, invalid("provider", "this platform connects through its sign-in page")
	}
	release, err := s.takeChannelLinkUse(ctx, l)
	if err != nil {
		return Channel{}, err
	}
	ch, err := s.ConnectChannel(ctx, linkPrincipal(l), ChannelInput{Provider: in.Provider, Fields: in.Fields, GroupID: s.linkGroup(ctx, l)})
	if err != nil {
		release()
		return Channel{}, err
	}
	return ch, nil
}

// StartOAuthViaLink returns the platform's sign-in URL for a customer on a link. The state carries the link
// (signed), so the callback finishes in the link's workspace without anyone signed in to Staqly.
func (s *Service) StartOAuthViaLink(ctx context.Context, token, providerID, redirect string, extra map[string]string) (string, error) {
	l, err := s.openChannelLink(ctx, token)
	if err != nil {
		return "", err
	}
	e, err := s.linkAllows(ctx, l, providerID)
	if err != nil {
		return "", err
	}
	if e.Auth() != providers.AuthOAuth2 {
		return "", invalid("provider", "this platform connects with a token")
	}
	app, err := s.providerApp(ctx, l.WorkspaceID, providerID)
	if err != nil {
		return "", err
	}
	st := oauthState{Provider: providerID, Workspace: l.WorkspaceID, Nonce: randomString(12),
		Expires: s.Now().Add(10 * time.Minute).Unix(), Link: l.ID}
	state, err := s.signState(st)
	if err != nil {
		return "", err
	}
	return e.AuthURL(app, redirect, state, withVerifier(extra, s.pkceVerifier(st)))
}

// IsLinkState tells the callback which flow a platform's answer belongs to (the signature is checked later).
func (s *Service) IsLinkState(state string) bool {
	st, err := s.decodeState(state)
	return err == nil && st.Link != ""
}

// CompleteOAuthViaLink finishes a customer's sign-in started from a link and saves the channel there.
func (s *Service) CompleteOAuthViaLink(ctx context.Context, providerID, code, state, redirect string, extra map[string]string) (Channel, string, error) {
	st, err := s.decodeState(state)
	if err != nil {
		return Channel{}, "", err
	}
	if st.Link == "" || st.Provider != providerID {
		return Channel{}, "", invalid("state", "belongs to another connection")
	}
	l, err := s.Q.GetChannelLink(ctx, db.GetChannelLinkParams{WorkspaceID: st.Workspace, ID: st.Link})
	if isNoRows(err) {
		return Channel{}, "", &Error{Code: "invalid_link", Status: 404, Message: "this link is not valid; ask for a new one"}
	}
	if err != nil {
		return Channel{}, "", err
	}
	ws, err := s.Q.GetWorkspace(ctx, l.WorkspaceID)
	if err != nil {
		return Channel{}, "", err
	}
	if err := s.channelLinkOpen(l); err != nil {
		return Channel{}, ws.Name, err
	}
	e, err := s.linkAllows(ctx, l, providerID)
	if err != nil {
		return Channel{}, ws.Name, err
	}
	release, err := s.takeChannelLinkUse(ctx, l)
	if err != nil {
		return Channel{}, ws.Name, err
	}
	ch, err := s.completeOAuth(ctx, linkPrincipal(l), e, st, code, redirect, extra, s.linkGroup(ctx, l))
	if err != nil {
		release()
		return Channel{}, ws.Name, err
	}
	return ch, ws.Name, nil
}
