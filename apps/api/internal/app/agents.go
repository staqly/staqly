package app

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Agent is a client seen talking to the workspace: an MCP client by its clientInfo, a REST client by its
// User-Agent, per credential (API key, or "local" for keyless local requests). Browser sessions are people,
// not agents, and are not recorded.
type Agent struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // mcp | api
	Name          string `json:"name"`
	Version       string `json:"version"`
	PrincipalID   string `json:"principal_id"`
	PrincipalName string `json:"principal_name"`
	FirstSeenAt   string `json:"first_seen_at"`
	LastSeenAt    string `json:"last_seen_at"`
	Requests      int64  `json:"requests"`
	PostsCreated  int64  `json:"posts_created"`
}

// agentFlushEvery: requests are tallied in memory and written at most this often per agent (an item created is
// written right away), so polling agents don't turn every read into a write.
const agentFlushEvery = time.Minute

// maxAgentTallies bounds the in-memory tallies; maxAgentsPerWorkspace bounds the stored list (a client can send
// any number of names).
const (
	maxAgentTallies       = 10_000
	maxAgentsPerWorkspace = 200
)

type agentTally struct {
	requests, items int64
	flushed         time.Time
}

// agentTallies are per Service (one per process in production, one per test), so tests never share them.
type agentTallies struct {
	mu sync.Mutex
	m  map[string]*agentTally
}

// SeenAgent records one request from an agent. Errors are ignored: this is bookkeeping, never a reason to fail
// the agent's request.
func (s *Service) SeenAgent(ctx context.Context, p Principal, kind, name, version string, createdItem bool) {
	if p.WorkspaceID == "" || p.Kind == PrincipalSession {
		return
	}
	name, version = agentName(kind, name), clip(strings.TrimSpace(version), 100)
	key := p.WorkspaceID + "\x00" + kind + "\x00" + name + "\x00" + p.ID
	now := s.Now()
	at := &s.agents
	at.mu.Lock()
	if at.m == nil {
		at.m = map[string]*agentTally{}
	}
	t := at.m[key]
	if t == nil && len(at.m) >= maxAgentTallies {
		// Bounded: names come from clients. Unflushed counts of other agents are lost, which is fine for a tally.
		clear(at.m)
	}
	if t == nil {
		t = &agentTally{}
		at.m[key] = t
	}
	t.requests++
	if createdItem {
		t.items++
	}
	if !createdItem && !t.flushed.IsZero() && now.Sub(t.flushed) < agentFlushEvery {
		at.mu.Unlock()
		return
	}
	requests, items := t.requests, t.items
	t.requests, t.items, t.flushed = 0, 0, now
	at.mu.Unlock()
	_ = s.W.UpsertAgent(context.WithoutCancel(ctx), db.UpsertAgentParams{ID: newID("agt"), WorkspaceID: p.WorkspaceID,
		Kind: kind, Name: name, Version: version, PrincipalID: p.ID, PrincipalName: p.Name, SeenAt: formatTime(now),
		Requests: requests, ItemsCreated: items, MaxAgents: maxAgentsPerWorkspace})
}

// AgentFromUserAgent names a REST client: "staqly-sdk-python/0.3 my-agent/1.2" is my-agent (version
// "1.2 · staqly-sdk-python 0.3"); otherwise the first product, e.g. n8n or curl.
func AgentFromUserAgent(ua string) (name, version string) {
	tokens := strings.Fields(ua)
	if len(tokens) == 0 {
		return "", ""
	}
	split := func(t string) (string, string) {
		n, v, _ := strings.Cut(t, "/")
		return n, v
	}
	n, v := split(tokens[0])
	if strings.HasPrefix(n, "staqly-sdk-") && len(tokens) > 1 {
		an, av := split(tokens[len(tokens)-1])
		return an, strings.TrimSpace(av + " · " + n + " " + v)
	}
	return n, v
}

// ListAgents lists the agents seen in the workspace, most recently active first.
func (s *Service) ListAgents(ctx context.Context, p Principal) ([]Agent, error) {
	// Admin: it names the API keys agents use, like the key list.
	if err := p.require(ScopeAdmin); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListAgents(ctx, p.WorkspaceID)
	out := []Agent{}
	for _, a := range rows {
		out = append(out, Agent{ID: a.ID, Kind: a.Kind, Name: a.Name, Version: a.Version, PrincipalID: a.PrincipalID,
			PrincipalName: a.PrincipalName, FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt, Requests: a.Requests,
			PostsCreated: a.ItemsCreated})
	}
	return out, err
}

// ForgetAgent removes an agent from the list (it comes back if it talks to the workspace again).
func (s *Service) ForgetAgent(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeAdmin); err != nil {
		return err
	}
	n, err := s.W.DeleteAgent(ctx, db.DeleteAgentParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err == nil && n == 0 {
		return notFound("agent")
	}
	return err
}

// agentName is how an agent is listed (and named on its events).
func agentName(kind, name string) string {
	if name = clip(strings.TrimSpace(name), 100); name == "" {
		name = map[string]string{"mcp": "MCP client", "api": "REST client"}[kind]
	}
	return name
}

// clip cuts s to at most n bytes without splitting a UTF-8 character (the result names agents and item
// sources, which must stay valid text).
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
