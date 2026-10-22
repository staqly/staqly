package app

import (
	"context"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Onboarded reports whether the welcome wizard (/welcome) is done for this principal: per person in an
// account. A local install never sees it (its empty pages guide instead), nor do sessions without a person
// in cloud mode (development API key sign-in).
func (s *Service) Onboarded(ctx context.Context, p Principal) bool {
	if p.UserID == "" {
		return true
	}
	u, err := s.Q.GetUser(ctx, p.UserID)
	return err != nil || u.OnboardedAt.Valid
}

// FinishOnboarding marks the welcome wizard done (finished or skipped). name, when given, is how the person
// wants to be called.
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
			if err := q.SetUserName(ctx, db.SetUserNameParams{Name: name, UpdatedAt: now, ID: p.UserID}); err != nil {
				return err
			}
		}
		return q.SetUserOnboarded(ctx, db.SetUserOnboardedParams{OnboardedAt: nullStr(now), UpdatedAt: now, ID: p.UserID})
	})
}
