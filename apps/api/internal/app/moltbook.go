package app

import (
	"context"
	"errors"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// MoltbookAgentInput registers a new Moltbook agent and connects it, for someone who has no agent's key yet.
type MoltbookAgentInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// GroupID puts the channel in a channel group (empty: Default).
	GroupID string `json:"group_id"`
}

// MoltbookAgent is the agent's new channel and the link its owner opens to claim it (until then it cannot post).
type MoltbookAgent struct {
	Channel  Channel `json:"channel"`
	ClaimURL string  `json:"claim_url"`
}

// RegisterMoltbookAgent registers an agent with Moltbook and connects it at once: Moltbook shows the agent's key
// only in its answer, so the key is sealed in the channel straight away instead of passing through the browser.
func (s *Service) RegisterMoltbookAgent(ctx context.Context, p Principal, in MoltbookAgentInput) (MoltbookAgent, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return MoltbookAgent{}, err
	}
	e, err := s.availableProvider("moltbook")
	if err != nil {
		return MoltbookAgent{}, err
	}
	name, err := trimmed(in.Name, "name", 100, true)
	if err != nil {
		return MoltbookAgent{}, err
	}
	desc, err := trimmed(in.Description, "description", 500, false)
	if err != nil {
		return MoltbookAgent{}, err
	}
	// An agent registered over the plan's channel limit would have nowhere to keep its key.
	if err := s.useChannelQuota(ctx, s.Q, p.WorkspaceID); err != nil {
		return MoltbookAgent{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	creds, profile, claim, err := providers.MoltbookRegister(s.providerCtx(cctx), name, desc)
	if err != nil {
		var ve *providers.ValidationError
		if errors.As(err, &ve) {
			return MoltbookAgent{}, invalid(ve.Field, ve.Msg)
		}
		return MoltbookAgent{}, connectError(err)
	}
	c, err := s.saveChannel(ctx, p, e, firstNonEmpty(profile.Name, name), creds, profile, nil, strings.TrimSpace(in.GroupID))
	if err != nil {
		return MoltbookAgent{}, err
	}
	return MoltbookAgent{Channel: c, ClaimURL: claim}, nil
}
