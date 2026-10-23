package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// A sign-in that may publish as several accounts (Facebook Pages, LinkedIn Pages, YouTube channels, Google Business
// locations...) is kept here, sealed, while the person picks which accounts become channels. Nothing is picked for
// them: the only account of a sign-in is connected directly, several wait for a choice.

// pendingTTL is how long a sign-in waits for the choice.
const pendingTTL = 10 * time.Minute

// PendingConnection is a sign-in waiting for the person to pick its accounts.
type PendingConnection struct {
	ID        string              `json:"id"`
	Provider  string              `json:"provider"`
	Accounts  []providers.Account `json:"accounts"`
	ExpiresAt string              `json:"expires_at"`
}

// OAuthResult is what finishing a platform's sign-in gave: a channel, or accounts to pick from.
type OAuthResult struct {
	Channel *Channel
	Pending *PendingConnection
}

// connectSignIn saves what a platform's sign-in gave: the account itself, the only account it may publish as, or a
// pending connection when there are several to pick from.
func (s *Service) connectSignIn(ctx context.Context, p Principal, e providers.Entry, creds providers.Credentials,
	profile providers.Profile, linkID, groupID string) (OAuthResult, error) {
	lister, ok := e.Provider.(providers.AccountLister)
	if !ok {
		ch, err := s.saveOrReconnect(ctx, p, e, creds, profile, groupID)
		return OAuthResult{Channel: &ch}, err
	}
	cctx, cancel := context.WithTimeout(providers.WithHTTPClient(ctx, s.safeClient()), connectTimeout)
	defer cancel()
	accounts, err := lister.Accounts(cctx, creds)
	if err != nil {
		return OAuthResult{}, connectError(err)
	}
	switch len(accounts) {
	case 0:
		return OAuthResult{}, &Error{Code: "connect_failed", Status: 422,
			Message: "this sign-in has no account " + e.Name() + " lets staqly publish as; check the account you signed in with"}
	case 1:
		ch, err := s.useAccount(ctx, p, e, lister, creds, accounts[0].ID, groupID)
		return OAuthResult{Channel: &ch}, err
	}
	sealed, err := s.seal(creds)
	if err != nil {
		return OAuthResult{}, err
	}
	pc := PendingConnection{ID: newID("pc"), Provider: e.ID(), Accounts: accounts,
		ExpiresAt: formatTime(s.Now().Add(pendingTTL))}
	err = s.W.DeleteExpiredPendingConnections(ctx, s.now())
	if err == nil {
		err = s.W.CreatePendingConnection(ctx, db.CreatePendingConnectionParams{ID: pc.ID, WorkspaceID: p.WorkspaceID,
			Provider: pc.Provider, Credentials: sealed, Accounts: mustJSON(accounts), LinkID: linkID, GroupID: groupID,
			CreatedAt: s.now(), ExpiresAt: pc.ExpiresAt})
	}
	if err != nil {
		return OAuthResult{}, err
	}
	return OAuthResult{Pending: &pc}, nil
}

// saveOrReconnect saves the account as a channel; an existing channel of the same provider and handle is
// reconnected instead of duplicated.
func (s *Service) saveOrReconnect(ctx context.Context, p Principal, e providers.Entry, creds providers.Credentials,
	profile providers.Profile, groupID string) (Channel, error) {
	if profile.Handle != "" {
		rows, err := s.Q.ListChannels(ctx, p.WorkspaceID)
		if err != nil {
			return Channel{}, err
		}
		for _, row := range rows {
			if row.Provider == e.ID() && row.Handle == profile.Handle {
				return s.reconnectChannel(ctx, p, row, creds, profile)
			}
		}
	}
	return s.saveChannel(ctx, p, e, firstNonEmpty(profile.Name, profile.Handle, e.Name()), creds, profile, nil, groupID)
}

// useAccount narrows a sign-in to one of its accounts and saves that as a channel.
func (s *Service) useAccount(ctx context.Context, p Principal, e providers.Entry, lister providers.AccountLister,
	creds providers.Credentials, accountID, groupID string) (Channel, error) {
	cctx, cancel := context.WithTimeout(providers.WithHTTPClient(ctx, s.safeClient()), connectTimeout)
	defer cancel()
	c, profile, err := lister.UseAccount(cctx, creds, accountID)
	if err != nil {
		return Channel{}, connectError(err)
	}
	return s.saveOrReconnect(ctx, p, e, c, profile, groupID)
}

// pendingRow reads a pending connection that has not run out.
func (s *Service) pendingRow(ctx context.Context, workspaceID, id string) (db.PendingConnection, error) {
	row, err := s.Q.GetPendingConnection(ctx, db.GetPendingConnectionParams{WorkspaceID: workspaceID, ID: strings.TrimSpace(id), Now: s.now()})
	if isNoRows(err) {
		return row, &Error{Code: "not_found", Status: 404, Message: "this sign-in is not waiting any more (it is done or took too long); connect again"}
	}
	return row, err
}

func toPending(row db.PendingConnection) PendingConnection {
	pc := PendingConnection{ID: row.ID, Provider: row.Provider, ExpiresAt: row.ExpiresAt, Accounts: []providers.Account{}}
	_ = json.Unmarshal([]byte(row.Accounts), &pc.Accounts)
	return pc
}

// GetPendingConnection shows the accounts a sign-in may publish as, for the person to pick from.
func (s *Service) GetPendingConnection(ctx context.Context, p Principal, id string) (PendingConnection, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return PendingConnection{}, err
	}
	row, err := s.pendingRow(ctx, p.WorkspaceID, id)
	if err != nil {
		return PendingConnection{}, err
	}
	if row.LinkID != "" {
		return PendingConnection{}, notFound("pending connection")
	}
	return toPending(row), nil
}

// FinishPendingConnection connects the accounts the person picked, each as its own channel (an account already
// connected is reconnected). The sign-in is used once: picking again means signing in again.
func (s *Service) FinishPendingConnection(ctx context.Context, p Principal, id string, accountIDs []string) ([]Channel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return nil, err
	}
	row, err := s.pendingRow(ctx, p.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if row.LinkID != "" {
		return nil, notFound("pending connection")
	}
	return s.finishPending(ctx, p, row, accountIDs, nil)
}

// finishPending checks the choice, claims the sign-in and connects every picked account. take (connect links)
// claims one use of the link per account and returns its release.
func (s *Service) finishPending(ctx context.Context, p Principal, row db.PendingConnection, accountIDs []string,
	take func() (func(), error)) ([]Channel, error) {
	e, err := s.availableProvider(row.Provider)
	if err != nil {
		return nil, err
	}
	lister, ok := e.Provider.(providers.AccountLister)
	if !ok {
		return nil, invalid("provider", "this platform has no accounts to pick")
	}
	pc := toPending(row)
	seen := map[string]bool{}
	var ids []string
	for _, id := range accountIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		known := false
		for _, a := range pc.Accounts {
			known = known || a.ID == id
		}
		if !known {
			return nil, invalid("account_ids", "pick accounts from the list")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, invalid("account_ids", "pick at least one account")
	}
	var creds providers.Credentials
	if err := s.unseal(row.Credentials, &creds); err != nil {
		return nil, err
	}
	// Claimed once: a second submit (a double click, another tab) finds nothing to finish.
	n, err := s.W.DeletePendingConnection(ctx, db.DeletePendingConnectionParams{WorkspaceID: row.WorkspaceID, ID: row.ID})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, &Error{Code: "not_found", Status: 404, Message: "this sign-in is not waiting any more (it is done or took too long); connect again"}
	}
	out := []Channel{}
	for _, id := range ids {
		release := func() {}
		if take != nil {
			if release, err = take(); err != nil {
				return out, err
			}
		}
		ch, err := s.useAccount(ctx, p, e, lister, creds, id, row.GroupID)
		if err != nil {
			release()
			return out, err
		}
		out = append(out, ch)
	}
	return out, nil
}

// GetPendingConnectionViaLink is the picker of a sign-in started from a connect link.
func (s *Service) GetPendingConnectionViaLink(ctx context.Context, token, id string) (PendingConnection, error) {
	l, err := s.openChannelLink(ctx, token)
	if err != nil {
		return PendingConnection{}, err
	}
	row, err := s.pendingRow(ctx, l.WorkspaceID, id)
	if err != nil {
		return PendingConnection{}, err
	}
	if row.LinkID != l.ID {
		return PendingConnection{}, notFound("pending connection")
	}
	return toPending(row), nil
}

// FinishPendingConnectionViaLink connects the accounts a customer picked; every account takes one use of the link.
func (s *Service) FinishPendingConnectionViaLink(ctx context.Context, token, id string, accountIDs []string) ([]Channel, error) {
	l, err := s.openChannelLink(ctx, token)
	if err != nil {
		return nil, err
	}
	row, err := s.pendingRow(ctx, l.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if row.LinkID != l.ID {
		return nil, notFound("pending connection")
	}
	if _, err := s.linkAllows(ctx, l, row.Provider); err != nil {
		return nil, err
	}
	return s.finishPending(ctx, linkPrincipal(l), row, accountIDs, func() (func(), error) { return s.takeChannelLinkUse(ctx, l) })
}
