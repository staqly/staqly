package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Sign-in with an email code (cloud accounts, accounts.go). A 6-digit code mailed to an address proves
// it: only its hash is stored, it works once, for emailCodeTTL, and dies after emailCodeMaxAttempts wrong
// tries. The answer to "send a code" never says whether the address has an account. A verified code
// signs in as the identity ("email", <address>) through the same signInIdentity as Google, so a Google
// identity and an email identity with the same address stay two people unless the person links them.

const (
	emailCodeTTL         = 10 * time.Minute
	emailCodeCooldown    = 30 * time.Second
	emailCodeMaxAttempts = 5
	// emailCodesPerHour: codes one address can receive per hour (per client IP: httpapi).
	emailCodesPerHour = 5
	// emailCodesPerDay: and per rolling 24 hours, so guessing stays bounded over days (15 codes x 5 tries:
	// 75 guesses a day out of a million). Every purpose counts, like the hourly cap.
	emailCodesPerDay = 15

	purposeSignIn = "sign_in"
	purposeLink   = "link"
	// Changing an email sign-in address (email_change.go): a code to the current address, then one to the new.
	purposeChangeCurrent = "change_current"
	purposeChangeNew     = "change_new"
)

// EmailCodeSent: when the next code may be asked for and how long this one works (seconds).
type EmailCodeSent struct {
	ResendIn  int `json:"resend_in"`
	ExpiresIn int `json:"expires_in"`
}

var (
	errCodeExpired  = &Error{Code: "code_expired", Status: 410, Message: "this code expired or was already used: send a new one"}
	errCodeTooMany  = &Error{Code: "code_expired", Status: 410, Message: "too many wrong tries for this code: send a new one"}
	errEmailSignOff = &Error{Code: "email_sign_in_disabled", Status: 409, Message: "sign-in with an email code is not set up on this server (MAIL_SMTP_HOST, MAIL_FROM)"}
)

// sixDigits: a uniformly random code from 000000 to 999999.
func sixDigits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func emailCodeHash(id, code string) string { return hashSecret(id + ":" + code) }

// StartEmailSignIn mails a sign-in code to the address, whether or not it has an account.
func (s *Service) StartEmailSignIn(ctx context.Context, email string) (EmailCodeSent, error) {
	return s.sendEmailCode(ctx, email, purposeSignIn, "")
}

// VerifyEmailSignIn checks the code and signs in (a new person gets an account and a first workspace,
// an invitation is accepted), with a fresh session.
func (s *Service) VerifyEmailSignIn(ctx context.Context, email, code, next, invite, setupToken, userAgent string) (LoginResult, error) {
	row, email, err := s.checkEmailCode(ctx, email, code, purposeSignIn, "")
	if err != nil {
		return LoginResult{}, err
	}
	st := loginState{Next: SafeNext(next), Invite: strings.TrimSpace(invite), SetupToken: setupToken}
	var res LoginResult
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := useEmailCode(ctx, q, s.now(), row.ID); err != nil {
			return err
		}
		var err error
		res, err = s.signInIdentityTx(ctx, q, Identity{Provider: ProviderEmail, Subject: email, Email: email}, st, userAgent)
		return err
	})
	return res, err
}

// StartEmailLink mails a code to an address the signed-in person wants to sign in with too. Whether the
// address already belongs to someone else shows only after the code proves it is theirs.
func (s *Service) StartEmailLink(ctx context.Context, p Principal, email string) (EmailCodeSent, error) {
	if p.Kind != PrincipalSession || p.UserID == "" {
		return EmailCodeSent{}, forbidden("only a signed-in person adds a sign-in method")
	}
	// One email address per person: someone who signs in with one changes it instead (email_change.go).
	if cur, err := s.emailIdentity(ctx, p.UserID, ""); err != nil {
		return EmailCodeSent{}, err
	} else if cur != "" {
		return EmailCodeSent{}, &Error{Code: "email_exists", Status: 409,
			Message: "you already sign in with " + cur + ": change that address instead of adding another"}
	}
	return s.sendEmailCode(ctx, email, purposeLink, p.UserID)
}

// VerifyEmailLink adds the address as a sign-in method of the signed-in person.
func (s *Service) VerifyEmailLink(ctx context.Context, p Principal, email, code string) ([]SignInMethod, error) {
	if p.Kind != PrincipalSession || p.UserID == "" {
		return nil, forbidden("only a signed-in person adds a sign-in method")
	}
	row, email, err := s.checkEmailCode(ctx, email, code, purposeLink, p.UserID)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := useEmailCode(ctx, q, s.now(), row.ID); err != nil {
			return err
		}
		if err := s.linkIdentityTx(ctx, q, p.UserID, Identity{Provider: ProviderEmail, Subject: email, Email: email}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "sign_in_method.added", "", "", map[string]any{"provider": ProviderEmail, "email": email})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.ListSignInMethods(ctx, p)
}

func (s *Service) sendEmailCode(ctx context.Context, email, purpose, userID string) (EmailCodeSent, error) {
	if !s.EmailSignIn() {
		return EmailCodeSent{}, errEmailSignOff
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return EmailCodeSent{}, err
	}
	now := s.Now().UTC()
	code, id := sixDigits(), newID("emc")
	err = s.tx(ctx, func(q *db.Queries) error {
		latest, err := q.LatestEmailCode(ctx, db.LatestEmailCodeParams{Email: email, Purpose: purpose, UserID: userID})
		if err != nil && !isNoRows(err) {
			return err
		}
		if err == nil {
			if created, perr := time.Parse(timeFormat, latest.CreatedAt); perr == nil && now.Before(created.Add(emailCodeCooldown)) {
				wait := int(created.Add(emailCodeCooldown).Sub(now).Seconds()) + 1
				return &Error{Code: "code_cooldown", Status: 429, Message: fmt.Sprintf("a code was just sent: wait %d seconds before asking for another", wait),
					Details: map[string]any{"resend_in": wait}}
			}
		}
		n, err := q.CountEmailCodesSince(ctx, db.CountEmailCodesSinceParams{Email: email, CreatedAt: formatTime(now.Add(-time.Hour))})
		if err != nil {
			return err
		}
		if n >= emailCodesPerHour {
			return &Error{Code: "rate_limited", Status: 429, Message: "too many codes for this address: wait an hour and try again"}
		}
		// Purged after 24 h (below), so the day's codes are all still here.
		n, err = q.CountEmailCodesSince(ctx, db.CountEmailCodesSinceParams{Email: email, CreatedAt: formatTime(now.Add(-24 * time.Hour))})
		if err != nil {
			return err
		}
		if n >= emailCodesPerDay {
			return &Error{Code: "rate_limited", Status: 429, Message: "too many codes for this address today: try again tomorrow"}
		}
		if err := q.PurgeEmailCodes(ctx, formatTime(now.Add(-24*time.Hour))); err != nil {
			return err
		}
		if err := q.CloseEmailCodes(ctx, db.CloseEmailCodesParams{Now: nullStr(formatTime(now)), Email: email, Purpose: purpose, UserID: userID}); err != nil {
			return err
		}
		return q.CreateEmailCode(ctx, db.CreateEmailCodeParams{ID: id, Email: email, Purpose: purpose, UserID: userID,
			CodeHash: emailCodeHash(id, code), CreatedAt: formatTime(now), ExpiresAt: formatTime(now.Add(emailCodeTTL))})
	})
	if err != nil {
		return EmailCodeSent{}, err
	}
	// After the commit: no network call inside a transaction.
	if s.Cfg.DevSignIn {
		s.devCodes.put(email, code)
	}
	if s.mailConfigured() {
		subject, text := emailCodeMessage(purpose, code)
		if err := s.sendSystemEmail(ctx, email, subject, text); err != nil {
			s.logger(ctx).Warn("sign-in code email not sent", "err", err)
			return EmailCodeSent{}, &Error{Code: "mail_failed", Status: 503, Message: "the code could not be sent: try again in a minute"}
		}
	} else if !s.Cfg.DevSignIn {
		return EmailCodeSent{}, errEmailSignOff
	}
	return EmailCodeSent{ResendIn: int(emailCodeCooldown.Seconds()), ExpiresIn: int(emailCodeTTL.Seconds())}, nil
}

func emailCodeMessage(purpose, code string) (string, string) {
	switch purpose {
	case purposeChangeCurrent:
		return "Your Staqly code: " + code, fmt.Sprintf("Your code to change the address you sign in to Staqly with is %s.\n\n"+
			"Enter it in Settings, Account. It works once, for 10 minutes.\n\n"+
			"If you did not ask for it, someone may be signed in to your account: sign out other sessions in Settings.\n", code)
	case purposeChangeNew:
		return "Your Staqly code: " + code, fmt.Sprintf("Your code to sign in to Staqly with this address from now on is %s.\n\n"+
			"Enter it in Settings, Account. It works once, for 10 minutes.\n\n"+
			"If you did not ask for it, ignore this email.\n", code)
	}
	if purpose == purposeLink {
		return "Your Staqly code: " + code, fmt.Sprintf("Your code to sign in to Staqly with this address is %s.\n\n"+
			"Enter it in Settings, Account. It works once, for 10 minutes.\n\n"+
			"If you did not ask for it, ignore this email.\n", code)
	}
	return "Your Staqly sign-in code: " + code, fmt.Sprintf("Your sign-in code is %s.\n\n"+
		"Enter it on the sign-in page. It works once, for 10 minutes.\n\n"+
		"If you did not try to sign in to Staqly, ignore this email: nobody can sign in without the code.\n", code)
}

// checkEmailCode spends one try on the open code for the address and returns it when the code matches.
// The try is counted before the comparison (and outside the caller's transaction), so a wrong code always
// costs a try and concurrent guesses never exceed the limit.
func (s *Service) checkEmailCode(ctx context.Context, email, code, purpose, userID string) (db.EmailCode, string, error) {
	if !s.EmailSignIn() {
		return db.EmailCode{}, "", errEmailSignOff
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return db.EmailCode{}, "", err
	}
	code = strings.Join(strings.Fields(code), "")
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return db.EmailCode{}, "", invalid("code", "enter the 6-digit code from the email")
	}
	row, err := s.Q.LatestEmailCode(ctx, db.LatestEmailCodeParams{Email: email, Purpose: purpose, UserID: userID})
	if isNoRows(err) {
		return db.EmailCode{}, "", errCodeExpired
	}
	if err != nil {
		return db.EmailCode{}, "", err
	}
	n, err := s.W.SpendEmailCodeAttempt(ctx, db.SpendEmailCodeAttemptParams{ID: row.ID, MaxAttempts: emailCodeMaxAttempts, Now: s.now()})
	if err != nil {
		return db.EmailCode{}, "", err
	}
	if n == 0 {
		if row.Attempts >= emailCodeMaxAttempts {
			return db.EmailCode{}, "", errCodeTooMany
		}
		return db.EmailCode{}, "", errCodeExpired
	}
	if subtle.ConstantTimeCompare([]byte(emailCodeHash(row.ID, code)), []byte(row.CodeHash)) != 1 {
		left := emailCodeMaxAttempts - int(row.Attempts) - 1
		if left <= 0 {
			return db.EmailCode{}, "", errCodeTooMany
		}
		return db.EmailCode{}, "", &Error{Code: "code_invalid", Status: 401, Message: fmt.Sprintf("that code is not right: %d tries left", left),
			Details: map[string]any{"attempts_left": left}}
	}
	return row, email, nil
}

// useEmailCode: single use, in the caller's transaction (a concurrent use of the same code gets 0 rows).
func useEmailCode(ctx context.Context, q *db.Queries, now, id string) error {
	n, err := q.UseEmailCode(ctx, db.UseEmailCodeParams{Now: nullStr(now), ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return errCodeExpired
	}
	return nil
}

// ---- sign-in methods ----

// SignInMethod is one way the person signs in: Google (an account), an email address or a passkey
// (subject: the passkey's id; name: what the person called it).
type SignInMethod struct {
	Provider    string  `json:"provider"`
	Subject     string  `json:"subject"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt *string `json:"last_login_at"`
}

func (s *Service) ListSignInMethods(ctx context.Context, p Principal) ([]SignInMethod, error) {
	if err := accountsOnly(s); err != nil {
		return nil, err
	}
	if p.Kind != PrincipalSession || p.UserID == "" { // an API key carries its creator's id, not their sign-in
		return nil, forbidden("only a signed-in person has sign-in methods")
	}
	rows, err := s.Q.ListUserIdentities(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := []SignInMethod{}
	for _, r := range rows {
		m := SignInMethod{Provider: r.Provider, Subject: r.Subject, Email: r.Email, CreatedAt: r.CreatedAt}
		if r.LastLoginAt.Valid {
			m.LastLoginAt = &r.LastLoginAt.String
		}
		out = append(out, m)
	}
	keys, err := s.Q.ListUserPasskeys(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		m := SignInMethod{Provider: ProviderPasskey, Subject: k.ID, Name: k.Name, CreatedAt: k.CreatedAt}
		if k.LastUsedAt.Valid {
			m.LastLoginAt = &k.LastUsedAt.String
		}
		out = append(out, m)
	}
	return out, nil
}

// RemoveSignInMethod: the person keeps at least one way to sign in.
func (s *Service) RemoveSignInMethod(ctx context.Context, p Principal, provider, subject string) ([]SignInMethod, error) {
	if err := accountsOnly(s); err != nil {
		return nil, err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return nil, forbidden("only a signed-in person removes a sign-in method")
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		rows, err := q.ListUserIdentities(ctx, p.UserID)
		if err != nil {
			return err
		}
		keys, err := q.ListUserPasskeys(ctx, p.UserID)
		if err != nil {
			return err
		}
		if len(rows)+len(keys) <= 1 {
			return &Error{Code: "last_sign_in_method", Status: 409, Message: "keep at least one way to sign in: add another one first"}
		}
		var n int64
		if provider == ProviderPasskey {
			n, err = q.DeletePasskey(ctx, db.DeletePasskeyParams{ID: subject, UserID: p.UserID})
		} else {
			n, err = q.DeleteIdentity(ctx, db.DeleteIdentityParams{Provider: provider, Subject: subject, UserID: p.UserID})
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return notFound("sign-in method")
		}
		_, err = s.emit(ctx, q, p, "sign_in_method.removed", "", "", map[string]any{"provider": provider, "subject": subject})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.ListSignInMethods(ctx, p)
}

func (s *Service) linkIdentity(ctx context.Context, userID string, id Identity) error {
	return s.tx(ctx, func(q *db.Queries) error { return s.linkIdentityTx(ctx, q, userID, id) })
}

// linkIdentityTx adds a verified identity to a person; one that already signs in someone else is refused
// (never moved: that would take over the other person).
func (s *Service) linkIdentityTx(ctx context.Context, q *db.Queries, userID string, id Identity) error {
	cur, err := q.GetIdentity(ctx, db.GetIdentityParams{Provider: id.Provider, Subject: id.Subject})
	switch {
	case err == nil && cur.UserID == userID:
		return nil
	case err == nil:
		what := "this Google account"
		if id.Provider == ProviderEmail {
			what = id.Email
		}
		return &Error{Code: "identity_taken", Status: 409,
			Message: what + " already signs in to another Staqly person: sign in with it and remove it there first"}
	case !isNoRows(err):
		return err
	}
	return q.CreateIdentity(ctx, db.CreateIdentityParams{Provider: id.Provider, Subject: id.Subject, UserID: userID,
		Email: strings.ToLower(strings.TrimSpace(id.Email)), CreatedAt: s.now()})
}

// ---- development ----

// devCodeBox keeps the last code per address in memory under AUTH_DEV_SIGNIN, so tests and the dev
// stack can sign in without reading mail. Never filled otherwise.
type devCodeBox struct {
	mu    sync.Mutex
	codes map[string]string
}

func (b *devCodeBox) put(email, code string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.codes == nil {
		b.codes = map[string]string{}
	}
	if len(b.codes) > 10_000 {
		clear(b.codes)
	}
	b.codes[email] = code
}

// DevEmailCode: the last code sent to the address, only with AUTH_DEV_SIGNIN.
func (s *Service) DevEmailCode(email string) (string, bool) {
	if !s.Cfg.DevSignIn {
		return "", false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	s.devCodes.mu.Lock()
	defer s.devCodes.mu.Unlock()
	c, ok := s.devCodes.codes[email]
	return c, ok
}
