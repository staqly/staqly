package app

import (
	"context"
	"fmt"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Changing the address a person signs in with by email code (cloud accounts). Two codes gate it: one to
// the current address (the person still owns it, so a stolen session alone cannot move the account), then
// one to the new address (it is theirs). Only then is the identity ("email", <current>) re-pointed to the
// new address: the same person, sessions and other sign-in methods stay as they are. Codes carry their
// own purposes, so a sign-in code never changes an address and a change code never signs anyone in.

// emailIdentity: the person's email sign-in address; with want, only that address ("" when they have none).
func (s *Service) emailIdentity(ctx context.Context, userID, want string) (string, error) {
	rows, err := s.Q.ListUserIdentities(ctx, userID)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Provider == ProviderEmail && (want == "" || r.Subject == want) {
			return r.Subject, nil
		}
	}
	return "", nil
}

// changeSubject: the signed-in person and their email sign-in address `current`.
func (s *Service) changeSubject(ctx context.Context, p Principal, current string) (string, error) {
	if err := accountsOnly(s); err != nil {
		return "", err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return "", forbidden("only a signed-in person changes their sign-in address")
	}
	current, err := normalizeEmail(current)
	if err != nil {
		return "", err
	}
	got, err := s.emailIdentity(ctx, p.UserID, current)
	if err != nil {
		return "", err
	}
	if got == "" {
		return "", notFound("sign-in method")
	}
	return current, nil
}

// StartEmailChange mails a code to the address the person signs in with now.
func (s *Service) StartEmailChange(ctx context.Context, p Principal, current string) (EmailCodeSent, error) {
	current, err := s.changeSubject(ctx, p, current)
	if err != nil {
		return EmailCodeSent{}, err
	}
	return s.sendEmailCode(ctx, current, purposeChangeCurrent, p.UserID)
}

// VerifyEmailChangeCurrent proves the current address; the proof stays open for emailCodeTTL while the
// person asks for and enters the code to the new address.
func (s *Service) VerifyEmailChangeCurrent(ctx context.Context, p Principal, current, code string) error {
	current, err := s.changeSubject(ctx, p, current)
	if err != nil {
		return err
	}
	row, _, err := s.checkEmailCode(ctx, current, code, purposeChangeCurrent, p.UserID)
	if err != nil {
		return err
	}
	now := s.Now().UTC()
	n, err := s.W.MarkEmailCodeVerified(ctx, db.MarkEmailCodeVerifiedParams{Now: nullStr(formatTime(now)),
		ExpiresAt: formatTime(now.Add(emailCodeTTL)), ID: row.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errCodeExpired
	}
	return nil
}

var errChangeNotProved = &Error{Code: "change_not_verified", Status: 409,
	Message: "confirm your current address first: ask for a code to it and enter it (it works for 10 minutes)"}

// provedCurrent: the open, verified code to the current address (step 1 done and still fresh).
func (s *Service) provedCurrent(ctx context.Context, q *db.Queries, userID, current string) (db.EmailCode, error) {
	row, err := q.LatestEmailCode(ctx, db.LatestEmailCodeParams{Email: current, Purpose: purposeChangeCurrent, UserID: userID})
	if isNoRows(err) {
		return db.EmailCode{}, errChangeNotProved
	}
	if err != nil {
		return db.EmailCode{}, err
	}
	if !row.VerifiedAt.Valid || row.UsedAt.Valid || row.ExpiresAt <= s.now() {
		return db.EmailCode{}, errChangeNotProved
	}
	return row, nil
}

// StartEmailChangeNew mails a code to the new address once the current one is proved. Whether the new
// address already belongs to someone shows only after its code proves it is the person's.
func (s *Service) StartEmailChangeNew(ctx context.Context, p Principal, current, next string) (EmailCodeSent, error) {
	current, err := s.changeSubject(ctx, p, current)
	if err != nil {
		return EmailCodeSent{}, err
	}
	next, err = normalizeEmail(next)
	if err != nil {
		return EmailCodeSent{}, err
	}
	if next == current {
		return EmailCodeSent{}, invalid("new_email", "that is the address you sign in with now: enter a different one")
	}
	if _, err := s.provedCurrent(ctx, s.Q, p.UserID, current); err != nil {
		return EmailCodeSent{}, err
	}
	return s.sendEmailCode(ctx, next, purposeChangeNew, p.UserID)
}

// VerifyEmailChangeNew makes the change: the identity signs in with the new address from now on. The old
// address gets a notice (after the commit).
func (s *Service) VerifyEmailChangeNew(ctx context.Context, p Principal, current, next, code string) ([]SignInMethod, error) {
	current, err := s.changeSubject(ctx, p, current)
	if err != nil {
		return nil, err
	}
	row, next, err := s.checkEmailCode(ctx, next, code, purposeChangeNew, p.UserID)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		proved, err := s.provedCurrent(ctx, q, p.UserID, current)
		if err != nil {
			return err
		}
		if err := useEmailCode(ctx, q, s.now(), proved.ID); err != nil {
			return err
		}
		if err := useEmailCode(ctx, q, s.now(), row.ID); err != nil {
			return err
		}
		if _, err := q.GetIdentity(ctx, db.GetIdentityParams{Provider: ProviderEmail, Subject: next}); err == nil {
			return &Error{Code: "identity_taken", Status: 409,
				Message: next + " already signs in to another Staqly person: sign in with it and remove it there first"}
		} else if !isNoRows(err) {
			return err
		}
		n, err := q.ChangeIdentitySubject(ctx, db.ChangeIdentitySubjectParams{NewSubject: next, Email: next,
			Provider: ProviderEmail, Subject: current, UserID: p.UserID})
		if err != nil {
			return err
		}
		if n == 0 {
			return notFound("sign-in method")
		}
		if err := q.ChangeUserEmail(ctx, db.ChangeUserEmailParams{Email: next, Now: s.now(), ID: p.UserID, OldEmail: current}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "sign_in_method.changed", "", "", map[string]any{"provider": ProviderEmail, "from": current, "to": next})
		return err
	})
	if err != nil {
		return nil, err
	}
	// After the commit: no network call inside a transaction. The change stands even if the notice fails.
	if s.mailConfigured() {
		text := fmt.Sprintf("The address you sign in to Staqly with was changed from %s to %s on %s.\n\n"+
			"If you did not make this change, sign in and check Settings, Account, or ask your workspace owner for help right away.\n",
			current, next, s.Now().UTC().Format(time.RFC1123))
		if err := s.sendSystemEmail(ctx, current, "Your Staqly sign-in address was changed", text); err != nil {
			s.logger(ctx).Warn("email change notice not sent", "err", err)
		}
	}
	return s.ListSignInMethods(ctx, p)
}
