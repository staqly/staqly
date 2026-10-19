package app

import (
	"context"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Onboarded reports whether the welcome wizard (/welcome) is done for this principal: per person in an
// account; on a local install per workspace, where one that already has channels or posts counts as done.
// Sessions without a person in cloud mode (development API key sign-in) never see it.
func (s *Service) Onboarded(ctx context.Context, p Principal) bool {
	if p.UserID != "" {
		u, err := s.Q.GetUser(ctx, p.UserID)
		return err != nil || u.OnboardedAt.Valid
	}
	if s.Cfg.Mode != ModeLocal {
		return true
	}
	ws, err := s.Q.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil || ws.OnboardedAt.Valid {
		return true
	}
	n, err := s.Q.WorkspaceContentCount(ctx, p.WorkspaceID)
	return err != nil || n > 0
}

// FinishOnboarding marks the welcome wizard done (finished or skipped). name, when given, is how the person
// wants to be called: their account's name when they own one (the sidebar shows it), else their own.
func (s *Service) FinishOnboarding(ctx context.Context, p Principal, name string) error {
	if p.Kind != PrincipalSession && p.Kind != PrincipalLocal {
		return forbidden("the welcome is finished from the dashboard")
	}
	name, err := trimmed(name, "name", 100, false)
	if err != nil {
		return err
	}
	now := s.now()
	return s.tx(ctx, func(q *db.Queries) error {
		if p.UserID == "" {
			return q.SetWorkspaceOnboarded(ctx, db.SetWorkspaceOnboardedParams{OnboardedAt: nullStr(now), UpdatedAt: now, ID: p.WorkspaceID})
		}
		if name != "" {
			if acc, err := q.GetAccountByOwner(ctx, p.UserID); err == nil {
				if err := q.UpdateAccountName(ctx, db.UpdateAccountNameParams{Name: name, UpdatedAt: now, ID: acc.ID}); err != nil {
					return err
				}
			} else if !isNoRows(err) {
				return err
			}
			if err := q.SetUserName(ctx, db.SetUserNameParams{Name: name, UpdatedAt: now, ID: p.UserID}); err != nil {
				return err
			}
		}
		return q.SetUserOnboarded(ctx, db.SetUserOnboardedParams{OnboardedAt: nullStr(now), UpdatedAt: now, ID: p.UserID})
	})
}
