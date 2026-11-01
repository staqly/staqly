package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The admins table: who may sign in to the admin panel. Seeded from ADMIN_EMAILS at every start, a local
// install gets a default admin, `server admin add` and the panel add and remove the rest.

// DefaultLocalAdmin is the local install's admin when ADMIN_EMAIL is not set.
const DefaultLocalAdmin = "admin@localhost"

// AdminAccount is one row of the admins table.
type AdminAccount struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	CreatedAt   string  `json:"created_at"`
	CreatedBy   string  `json:"created_by"`
	LastLoginAt *string `json:"last_login_at"`
}

func toAdminAccount(r db.Admin) AdminAccount {
	a := AdminAccount{ID: r.ID, Email: r.Email, Name: r.Name, CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy}
	if r.LastLoginAt.Valid {
		a.LastLoginAt = &r.LastLoginAt.String
	}
	return a
}

// EnsureAdmins runs at start: ADMIN_EMAILS are added when missing (never removed), then a local install with
// no admin gets the default one.
func (s *Service) EnsureAdmins(ctx context.Context) error {
	for _, e := range s.Cfg.AdminEmails {
		if _, err := s.W.CreateAdmin(ctx, db.CreateAdminParams{ID: newID("adm"), Email: strings.ToLower(strings.TrimSpace(e)), CreatedAt: s.now(), CreatedBy: "ADMIN_EMAILS"}); err != nil {
			return err
		}
	}
	n, err := s.Q.CountAdmins(ctx)
	if err != nil || n > 0 {
		return err
	}
	if s.Cfg.Mode != ModeLocal {
		if s.Cfg.AdminEnabled {
			s.logger(ctx).Warn("admin panel: no admins yet: run `server admin add <email>`")
		} else {
			s.logger(ctx).Info("admin panel: no admins yet (off: ADMIN_ADDR is empty)")
		}
		return nil
	}
	email := s.Cfg.AdminEmail
	if email == "" {
		email = DefaultLocalAdmin
	}
	if _, err := s.W.CreateAdmin(ctx, db.CreateAdminParams{ID: newID("adm"), Email: email, CreatedAt: s.now(), CreatedBy: "install"}); err != nil {
		return err
	}
	s.logger(ctx).Info("admin panel: default admin " + email)
	return nil
}

func (s *Service) AdminListAdmins(ctx context.Context) ([]AdminAccount, error) {
	rows, err := s.Q.ListAdmins(ctx)
	out := make([]AdminAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAdminAccount(r))
	}
	return out, err
}

// AdminAddAdmin adds an admin; a is the signed-in admin (zero value: the CLI, not audited).
func (s *Service) AdminAddAdmin(ctx context.Context, a Admin, email, name string) (AdminAccount, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return AdminAccount{}, err
	}
	by := a.Email
	if by == "" {
		by = "cli"
	}
	n, err := s.W.CreateAdmin(ctx, db.CreateAdminParams{ID: newID("adm"), Email: email, Name: strings.TrimSpace(name), CreatedAt: s.now(), CreatedBy: by})
	if err != nil {
		return AdminAccount{}, err
	}
	if n == 0 {
		return AdminAccount{}, &Error{Code: "admin_exists", Status: 409, Message: email + " is already an admin"}
	}
	row, err := s.Q.GetAdminByEmail(ctx, email)
	if err != nil {
		return AdminAccount{}, err
	}
	if a.Email != "" {
		err = s.AdminAudit(ctx, a, "add_admin", "", email, nil)
	}
	return toAdminAccount(row), err
}

// AdminRemoveAdmin removes an admin and signs them out at once; never yourself, never the last one.
func (s *Service) AdminRemoveAdmin(ctx context.Context, a Admin, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == a.Email && email != "" {
		return &Error{Code: "cannot_remove_self", Status: 409, Message: "you cannot remove yourself: ask another admin"}
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetAdminByEmail(ctx, email); err != nil {
			if isNoRows(err) {
				return &Error{Code: "not_found", Status: 404, Message: "no such admin"}
			}
			return err
		}
		if n, err := q.CountAdmins(ctx); err != nil {
			return err
		} else if n <= 1 {
			return &Error{Code: "last_admin", Status: 409, Message: "this is the last admin: add another first"}
		}
		if _, err := q.DeleteAdmin(ctx, email); err != nil {
			return err
		}
		return q.RevokeAdminSessionsByEmail(ctx, db.RevokeAdminSessionsByEmailParams{RevokedAt: nullStr(s.now()), Email: email})
	})
	if err != nil || a.Email == "" {
		return err
	}
	return s.AdminAudit(ctx, a, "remove_admin", "", email, nil)
}

// AdminAuthMethods: what the sign-in page can offer.
func (s *Service) AdminAuthMethods() (emailCode, accessToken bool) {
	return s.Cfg.DevSignIn || (s.Cfg.Mode == ModeCloud && s.mailConfigured()), s.Cfg.Mode == ModeLocal
}

// VerifyAdminLocalToken signs the oldest admin in with the install's access token (local mode only).
func (s *Service) VerifyAdminLocalToken(ctx context.Context, token, userAgent string) (Admin, string, time.Time, error) {
	if !s.AdminEnabled() || s.Cfg.Mode != ModeLocal {
		return Admin{}, "", time.Time{}, &Error{Code: "not_found", Status: 404, Message: "no such route"}
	}
	want, err := s.localTokenHash()
	if err != nil {
		return Admin{}, "", time.Time{}, err
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(token)))
	if want == nil || subtle.ConstantTimeCompare(got[:], want) != 1 {
		return Admin{}, "", time.Time{}, &Error{Code: "invalid_token", Status: 401, Message: "this is not the access token of this computer"}
	}
	rows, err := s.Q.ListAdmins(ctx)
	if err != nil {
		return Admin{}, "", time.Time{}, err
	}
	if len(rows) == 0 {
		return Admin{}, "", time.Time{}, &Error{Code: "invalid_token", Status: 401, Message: "no admin yet: restart the server or run `server admin add <email>`"}
	}
	now := s.Now().UTC()
	a, tok, expires := newAdminSession(rows[0].Email, now)
	if err := s.tx(ctx, func(q *db.Queries) error { return s.storeAdminSession(ctx, q, a, tok, userAgent, now, expires) }); err != nil {
		return Admin{}, "", time.Time{}, err
	}
	return a, tok, expires, nil
}
