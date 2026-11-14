package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Admin panel sign-in (separate from workspace accounts): an address in the admins table gets a 6-digit code
// by mail and trades it for a 12 h session. Only the code's and the token's hashes are stored.

const (
	adminCodeCooldown   = 30 * time.Second
	adminCodesPerHour   = 10
	adminSessionTTL     = 12 * time.Hour
	adminTouchEvery     = time.Minute
	adminDevCodePrefix  = "admin:"
	adminCodeSubject    = "Your staqly admin sign-in code"
	adminCodeMaxAttempt = emailCodeMaxAttempts
)

// Admin is the signed-in operator of one request.
type Admin struct {
	Email     string `json:"email"`
	SessionID string `json:"-"`
	CSRF      string `json:"-"`
}

var errAdminCode = &Error{Code: "invalid_code", Status: 401, Message: "that code is not right or has expired: ask for a new one"}

// AdminEnabled: the admin listener is configured (ADMIN_ADDR), in local and cloud mode alike.
func (s *Service) AdminEnabled() bool { return s.Cfg.AdminEnabled }

// isAdmin: the address is a row of the admins table (read on every request, so a removal is immediate).
func (s *Service) isAdmin(ctx context.Context, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false, nil
	}
	_, err := s.Q.GetAdminByEmail(ctx, email)
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

func adminCodeHash(id, code string) string { return hashSecret("admin:" + id + ":" + code) }

// errAdminQuiet: a cooldown or the hourly cap; no code is sent, and the caller says nothing either.
var errAdminQuiet = errors.New("admin code not sent")

// StartAdminSignIn mails a code when the address is an admin; for anyone else it does nothing and says nothing.
// The answer is the same for both (no cooldown error, mail sent in the background), so it can't tell who is an admin.
func (s *Service) StartAdminSignIn(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !s.AdminEnabled() {
		return nil
	}
	if ok, err := s.isAdmin(ctx, email); err != nil || !ok {
		return err
	}
	now := s.Now().UTC()
	code, id := sixDigits(), newID("adc")
	err := s.tx(ctx, func(q *db.Queries) error {
		latest, err := q.LatestAdminCode(ctx, email)
		if err != nil && !isNoRows(err) {
			return err
		}
		if err == nil {
			if created, perr := time.Parse(timeFormat, latest.CreatedAt); perr == nil && now.Before(created.Add(adminCodeCooldown)) {
				return errAdminQuiet
			}
		}
		n, err := q.CountAdminCodesSince(ctx, db.CountAdminCodesSinceParams{Email: email, CreatedAt: formatTime(now.Add(-time.Hour))})
		if err != nil {
			return err
		}
		if n >= adminCodesPerHour {
			return errAdminQuiet
		}
		if err := q.PurgeAdminCodes(ctx, formatTime(now.Add(-24*time.Hour))); err != nil {
			return err
		}
		return q.CreateAdminCode(ctx, db.CreateAdminCodeParams{ID: id, Email: email, CodeHash: adminCodeHash(id, code),
			CreatedAt: formatTime(now), ExpiresAt: formatTime(now.Add(emailCodeTTL))})
	})
	if errors.Is(err, errAdminQuiet) {
		return nil
	}
	if err != nil {
		return err
	}
	// After the commit: no network call inside a transaction.
	if s.Cfg.DevSignIn {
		s.devCodes.put(adminDevCodePrefix+email, code)
	}
	if s.mailConfigured() {
		text := fmt.Sprintf("Your code to sign in to the staqly admin panel:\n\n%s\n\nIt works **once** and expires in **10 minutes**. **Never share it.**\n", code)
		go func(ctx context.Context) {
			if err := s.sendSystemEmail(ctx, email, adminCodeSubject, text); err != nil {
				s.logger(ctx).Warn("admin sign-in code email not sent", "err", err)
			}
		}(context.WithoutCancel(ctx))
	}
	return nil
}

// DevAdminCode: the last admin code sent to the address, only with AUTH_DEV_SIGNIN.
func (s *Service) DevAdminCode(email string) (string, bool) {
	if !s.Cfg.DevSignIn {
		return "", false
	}
	s.devCodes.mu.Lock()
	defer s.devCodes.mu.Unlock()
	c, ok := s.devCodes.codes[adminDevCodePrefix+strings.ToLower(strings.TrimSpace(email))]
	return c, ok
}

// VerifyAdminSignIn trades a valid code for a session. Every failure is the same generic 401.
func (s *Service) VerifyAdminSignIn(ctx context.Context, email, code, userAgent string) (Admin, string, time.Time, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	code = strings.Join(strings.Fields(code), "")
	if !s.AdminEnabled() {
		return Admin{}, "", time.Time{}, errAdminCode
	}
	// Read before the admins check, so an address off the list costs the same lookup.
	row, err := s.Q.LatestAdminCode(ctx, email)
	if err != nil && !isNoRows(err) {
		return Admin{}, "", time.Time{}, err
	}
	ok, aerr := s.isAdmin(ctx, email)
	if aerr != nil {
		return Admin{}, "", time.Time{}, aerr
	}
	if isNoRows(err) || !ok {
		return Admin{}, "", time.Time{}, errAdminCode
	}
	now := s.Now().UTC()
	exp, perr := time.Parse(timeFormat, row.ExpiresAt)
	if row.UsedAt.Valid || perr != nil || !now.Before(exp) || row.Attempts >= adminCodeMaxAttempt {
		return Admin{}, "", time.Time{}, errAdminCode
	}
	// The try is counted before the comparison and only while tries are left (atomically: parallel guesses
	// cannot exceed the limit), so a wrong code always costs one.
	n, err := s.W.SpendAdminCodeAttempt(ctx, db.SpendAdminCodeAttemptParams{ID: row.ID, MaxAttempts: adminCodeMaxAttempt, Now: formatTime(now)})
	if err != nil {
		return Admin{}, "", time.Time{}, err
	}
	if n != 1 {
		return Admin{}, "", time.Time{}, errAdminCode
	}
	if subtle.ConstantTimeCompare([]byte(adminCodeHash(row.ID, code)), []byte(row.CodeHash)) != 1 {
		return Admin{}, "", time.Time{}, errAdminCode
	}
	a, token, expires := newAdminSession(email, now)
	err = s.tx(ctx, func(q *db.Queries) error {
		// Single use: re-read inside the transaction so a concurrent use of the same code loses.
		cur, err := q.LatestAdminCode(ctx, email)
		if err != nil {
			return err
		}
		if cur.ID != row.ID || cur.UsedAt.Valid {
			return errAdminCode
		}
		if err := q.UseAdminCode(ctx, db.UseAdminCodeParams{UsedAt: nullStr(formatTime(now)), ID: row.ID}); err != nil {
			return err
		}
		return s.storeAdminSession(ctx, q, a, token, userAgent, now, expires)
	})
	if err != nil {
		return Admin{}, "", time.Time{}, err
	}
	return a, token, expires, nil
}

func newAdminSession(email string, now time.Time) (Admin, string, time.Time) {
	return Admin{Email: email, SessionID: newID("ads"), CSRF: randomString(13)[:20]}, randomString(32), now.Add(adminSessionTTL)
}

// storeAdminSession saves the session and stamps the admin's last sign-in.
func (s *Service) storeAdminSession(ctx context.Context, q *db.Queries, a Admin, token, userAgent string, now, expires time.Time) error {
	if err := q.TouchAdminLogin(ctx, db.TouchAdminLoginParams{LastLoginAt: nullStr(formatTime(now)), Email: a.Email}); err != nil {
		return err
	}
	return q.CreateAdminSession(ctx, db.CreateAdminSessionParams{ID: a.SessionID, Email: a.Email, TokenHash: hashSecret(token),
		CsrfToken: a.CSRF, UserAgent: userAgent, CreatedAt: formatTime(now), LastSeenAt: formatTime(now), ExpiresAt: formatTime(expires)})
}

// AuthenticateAdmin: the session behind a token, while it is live and its address is still an admin.
func (s *Service) AuthenticateAdmin(ctx context.Context, token string) (Admin, error) {
	if token == "" || !s.AdminEnabled() {
		return Admin{}, ErrUnauthorized
	}
	row, err := s.Q.GetAdminSessionByHash(ctx, hashSecret(token))
	if isNoRows(err) {
		return Admin{}, ErrUnauthorized
	}
	if err != nil {
		return Admin{}, err
	}
	now := s.Now().UTC()
	exp, perr := time.Parse(timeFormat, row.ExpiresAt)
	if row.RevokedAt.Valid || perr != nil || !now.Before(exp) {
		return Admin{}, ErrUnauthorized
	}
	if ok, err := s.isAdmin(ctx, row.Email); err != nil {
		return Admin{}, err
	} else if !ok {
		return Admin{}, ErrUnauthorized
	}
	if seen, perr := time.Parse(timeFormat, row.LastSeenAt); perr != nil || now.Sub(seen) >= adminTouchEvery {
		_ = s.W.TouchAdminSession(ctx, db.TouchAdminSessionParams{LastSeenAt: formatTime(now), ID: row.ID})
	}
	return Admin{Email: row.Email, SessionID: row.ID, CSRF: row.CsrfToken}, nil
}

func (s *Service) SignOutAdmin(ctx context.Context, a Admin) error {
	return s.W.RevokeAdminSession(ctx, db.RevokeAdminSessionParams{RevokedAt: nullStr(s.now()), ID: a.SessionID})
}

// AdminAudit records one admin action (details: no secrets).
func (s *Service) AdminAudit(ctx context.Context, a Admin, action, workspaceID, target string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return s.W.InsertAdminAudit(ctx, db.InsertAdminAuditParams{ID: newID("ada"), Email: a.Email, Action: action,
		WorkspaceID: workspaceID, Target: target, Details: string(b), CreatedAt: s.now()})
}
