package app

import (
	"context"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// AttachWorkspace (operator command `server attach-workspace`) hands a workspace without people (a cloud
// workspace from before accounts, or one moved here with `server import-sqlite`) to a person: it issues an
// owner invitation for their address and returns it with its link (emailed when mail is configured). Signing
// in as that address (Google or an email code) and accepting it makes that person its first owner. The
// sign-in proves the address, as for every invitation. An open invitation of that address is replaced (the old link stops working).
func (s *Service) AttachWorkspace(ctx context.Context, workspaceID, email string) (Invitation, error) {
	if !s.AccountsEnabled() {
		return Invitation{}, conflict("accounts are off on this server (local mode, or cloud without a way to sign in): nothing to attach to")
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return Invitation{}, err
	}
	ws, err := s.Q.GetWorkspace(ctx, strings.TrimSpace(workspaceID))
	if isNoRows(err) {
		return Invitation{}, notFound("workspace")
	}
	if err != nil {
		return Invitation{}, err
	}
	if s.workspaceHasPeople(ctx, s.Q, ws.ID) {
		return Invitation{}, conflict("this workspace has people already: its owners invite others from Settings > Members")
	}
	token := invitePrefix + randomString(32)
	now := s.now()
	inv := db.WorkspaceInvitation{ID: newID("inv"), WorkspaceID: ws.ID, Email: email, Role: RoleOwner, InvitedBy: "Server operator",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: formatTime(s.Now().Add(invitationTTL))}
	p := Principal{WorkspaceID: ws.ID, Kind: PrincipalSystem, ID: "cli", Name: "Server operator"}
	err = s.tx(ctx, func(q *db.Queries) error {
		open, err := q.ListOpenInvitations(ctx, ws.ID)
		if err != nil {
			return err
		}
		for _, o := range open {
			if o.Email == email {
				if _, err := q.RevokeInvitation(ctx, db.RevokeInvitationParams{Now: nullStr(now), WorkspaceID: ws.ID, ID: o.ID}); err != nil {
					return err
				}
			}
		}
		if err := q.CreateInvitation(ctx, db.CreateInvitationParams{ID: inv.ID, WorkspaceID: ws.ID, Email: email, Role: RoleOwner,
			TokenHash: hashSecret(token), InvitedBy: inv.InvitedBy, CreatedAt: now, UpdatedAt: now, ExpiresAt: inv.ExpiresAt}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "invitation.created", "", "", map[string]any{"invitation_id": inv.ID, "email": email,
			"role": RoleOwner, "via": "attach-workspace"})
		return err
	})
	if err != nil {
		return Invitation{}, err
	}
	return s.deliverInvitation(ctx, p, inv, token), nil
}

// firstOwnerJoined (in the accepting transaction): a workspace from before accounts got its first owner. What
// signed in without a person ends, so from now on only people with a role open it: browser sessions and unused
// connect links without a user are revoked; API keys without a creator keep working (agents keep running)
// but now belong to the owner, so they never do more than the owner may. The recovery key stops working by
// itself (a workspace with people refuses it).
func (s *Service) firstOwnerJoined(ctx context.Context, q *db.Queries, workspaceID string, owner db.User) error {
	now := s.now()
	sessions, err := q.RevokeAnonymousSessions(ctx, db.RevokeAnonymousSessionsParams{Now: nullStr(now), WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	keys, err := q.AdoptAPIKeys(ctx, db.AdoptAPIKeysParams{UserID: nullStr(owner.ID), WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	links, err := q.ExpireAnonymousDeviceLinks(ctx, db.ExpireAnonymousDeviceLinksParams{Now: now, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	p := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "invitation", Name: displayName(owner), UserID: owner.ID}
	_, err = s.emit(ctx, q, p, "workspace.account_attached", "", "", map[string]any{"owner_user_id": owner.ID,
		"sessions_revoked": sessions, "api_keys_adopted": keys, "connect_links_expired": links})
	return err
}
