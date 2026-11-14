package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Passkeys (WebAuthn) for cloud accounts: a signed-in person adds one from Settings, then signs in with it
// without typing an address (a discoverable credential names the person). The relying party is the
// dashboard (PUBLIC_DASHBOARD_URL): its host is the RP ID, its origin the only one allowed. Each ceremony's
// challenge is kept server-side (passkey_challenges), single use, for passkeyChallengeTTL. A passkey is a
// sign-in method like Google or an email address: listed, renamed, removed (keeping at least one method).

const (
	ProviderPasskey     = "passkey"
	passkeyChallengeTTL = 5 * time.Minute

	challengeRegister = "register"
	challengeSignIn   = "sign_in"
)

var (
	errPasskeyOff     = &Error{Code: "passkeys_disabled", Status: 409, Message: "passkeys need this server's public address, which is not set up yet"}
	errPasskeyExpired = &Error{Code: "passkey_expired", Status: 410, Message: "this passkey prompt expired: try again"}
	errPasskeyInvalid = &Error{Code: "passkey_invalid", Status: 401, Message: "this passkey could not be checked: try again or sign in another way"}
)

// PasskeySignIn: passkeys work in cloud mode when the dashboard's public address is known.
func (s *Service) PasskeySignIn() bool {
	_, err := s.webAuthn()
	return err == nil
}

func (s *Service) webAuthn() (*webauthn.WebAuthn, error) {
	if s.Cfg.Mode != ModeCloud || s.Cfg.PublicDashboardURL == "" {
		return nil, errPasskeyOff
	}
	u, err := url.Parse(s.Cfg.PublicDashboardURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errPasskeyOff
	}
	wa, err := webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "Staqly", RPOrigins: []string{u.Scheme + "://" + u.Host}})
	if err != nil {
		return nil, errPasskeyOff
	}
	return wa, nil
}

// passkeyUser adapts a person to the library. The user handle is the person's id: random, never an address.
type passkeyUser struct {
	user  db.User
	creds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u passkeyUser) WebAuthnName() string                       { return u.user.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return displayName(u.user) }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Service) loadPasskeyUser(ctx context.Context, q *db.Queries, userID string) (passkeyUser, []db.Passkey, error) {
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return passkeyUser{}, nil, err
	}
	rows, err := q.ListUserPasskeys(ctx, userID)
	if err != nil {
		return passkeyUser{}, nil, err
	}
	u := passkeyUser{user: user}
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(r.Credential), &c); err != nil {
			return passkeyUser{}, nil, err
		}
		u.creds = append(u.creds, c)
	}
	return u, rows, nil
}

// PasskeyChallenge is one ceremony: its id (sent back with the browser's answer) and the options the
// browser passes to navigator.credentials (WebAuthn JSON, binary fields base64url).
type PasskeyChallenge struct {
	ID      string `json:"challenge_id"`
	Options any    `json:"options"`
}

func (s *Service) saveChallenge(ctx context.Context, purpose, userID string, session *webauthn.SessionData, options any) (PasskeyChallenge, error) {
	now := s.Now().UTC()
	data, err := json.Marshal(session)
	if err != nil {
		return PasskeyChallenge{}, err
	}
	id := newID("pkc")
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.PurgePasskeyChallenges(ctx, formatTime(now.Add(-time.Hour))); err != nil {
			return err
		}
		return q.CreatePasskeyChallenge(ctx, db.CreatePasskeyChallengeParams{ID: id, Purpose: purpose, UserID: userID,
			Session: string(data), CreatedAt: formatTime(now), ExpiresAt: formatTime(now.Add(passkeyChallengeTTL))})
	})
	return PasskeyChallenge{ID: id, Options: options}, err
}

// takeChallenge: the ceremony's session, deleted as it is read (a replayed answer finds nothing). Callers take it
// outside their transaction, so an answer that is refused uses the challenge up too.
func (s *Service) takeChallenge(ctx context.Context, q *db.Queries, id, purpose, userID string) (webauthn.SessionData, error) {
	row, err := q.TakePasskeyChallenge(ctx, db.TakePasskeyChallengeParams{ID: id, Purpose: purpose, UserID: userID, Now: s.now()})
	if isNoRows(err) {
		return webauthn.SessionData{}, errPasskeyExpired
	}
	if err != nil {
		return webauthn.SessionData{}, err
	}
	var sd webauthn.SessionData
	return sd, json.Unmarshal([]byte(row.Session), &sd)
}

func passkeyOnly(s *Service, p Principal) (*webauthn.WebAuthn, error) {
	if err := notGuest(p); err != nil {
		return nil, err
	}
	if err := accountsOnly(s); err != nil {
		return nil, err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return nil, forbidden("only a signed-in person adds a passkey")
	}
	return s.webAuthn()
}

// BeginPasskeyRegistration: options for navigator.credentials.create. The passkey must be discoverable
// (it names the person at sign-in); passkeys the person has are excluded so a device is not added twice.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, p Principal) (PasskeyChallenge, error) {
	wa, err := passkeyOnly(s, p)
	if err != nil {
		return PasskeyChallenge{}, err
	}
	u, _, err := s.loadPasskeyUser(ctx, s.Q, p.UserID)
	if err != nil {
		return PasskeyChallenge{}, err
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(u.creds))
	for _, c := range u.creds {
		exclude = append(exclude, c.Descriptor())
	}
	creation, session, err := wa.BeginRegistration(u,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementRequired, RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return PasskeyChallenge{}, err
	}
	return s.saveChallenge(ctx, challengeRegister, p.UserID, session, creation.Response)
}

// FinishPasskeyRegistration checks the browser's answer and adds the passkey under `name`.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, p Principal, challengeID, name string, response json.RawMessage) ([]SignInMethod, error) {
	wa, err := passkeyOnly(s, p)
	if err != nil {
		return nil, err
	}
	name = truncateRunes(strings.TrimSpace(name), 100)
	if name == "" {
		name = "Passkey"
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, invalid("response", "the browser's passkey answer could not be read: try again")
	}
	// Taken outside the transaction: a refused answer still uses the challenge up.
	session, err := s.takeChallenge(ctx, s.W, challengeID, challengeRegister, p.UserID)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		u, _, err := s.loadPasskeyUser(ctx, q, p.UserID)
		if err != nil {
			return err
		}
		cred, err := wa.CreateCredential(u, session, parsed)
		if err != nil {
			s.logger(ctx).Info("passkey registration refused", "err", err)
			return errPasskeyInvalid
		}
		credID := base64.RawURLEncoding.EncodeToString(cred.ID)
		if _, err := q.GetPasskeyByCredential(ctx, credID); err == nil {
			return &Error{Code: "passkey_exists", Status: 409, Message: "this passkey is already added"}
		} else if !isNoRows(err) {
			return err
		}
		data, err := json.Marshal(cred)
		if err != nil {
			return err
		}
		id := newID("pk")
		if err := q.CreatePasskey(ctx, db.CreatePasskeyParams{ID: id, UserID: p.UserID, CredentialID: credID, Name: name,
			Credential: string(data), CreatedAt: s.now()}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "sign_in_method.added", "", "", map[string]any{"provider": ProviderPasskey, "passkey_id": id, "name": name})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.ListSignInMethods(ctx, p)
}

// BeginPasskeySignIn: options for navigator.credentials.get, with no address (the passkey picks the person).
func (s *Service) BeginPasskeySignIn(ctx context.Context) (PasskeyChallenge, error) {
	if err := accountsOnly(s); err != nil {
		return PasskeyChallenge{}, err
	}
	wa, err := s.webAuthn()
	if err != nil {
		return PasskeyChallenge{}, err
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return PasskeyChallenge{}, err
	}
	return s.saveChallenge(ctx, challengeSignIn, "", session, assertion.Response)
}

// FinishPasskeySignIn checks the answer and signs the passkey's person in with a fresh session. A sign
// counter that went backwards (a cloned authenticator) is refused.
func (s *Service) FinishPasskeySignIn(ctx context.Context, challengeID string, response json.RawMessage, next, userAgent string) (_ LoginResult, err error) {
	defer func() { s.trackSignInFailed("passkey", err) }()
	if err := accountsOnly(s); err != nil {
		return LoginResult{}, err
	}
	wa, err := s.webAuthn()
	if err != nil {
		return LoginResult{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return LoginResult{}, invalid("response", "the browser's passkey answer could not be read: try again")
	}
	session, err := s.takeChallenge(ctx, s.W, challengeID, challengeSignIn, "")
	if err != nil {
		return LoginResult{}, err
	}
	var res LoginResult
	err = s.tx(ctx, func(q *db.Queries) error {
		var row db.Passkey
		handler := func(rawID, userHandle []byte) (webauthn.User, error) {
			var err error
			row, err = q.GetPasskeyByCredential(ctx, base64.RawURLEncoding.EncodeToString(rawID))
			if err != nil {
				return nil, err
			}
			if row.UserID != string(userHandle) {
				return nil, errors.New("user handle does not match the passkey")
			}
			u, _, err := s.loadPasskeyUser(ctx, q, row.UserID)
			return u, err
		}
		u, cred, err := wa.ValidatePasskeyLogin(handler, session, parsed)
		if err != nil {
			s.logger(ctx).Info("passkey sign-in refused", "err", err)
			return errPasskeyInvalid
		}
		if cred.Authenticator.CloneWarning {
			return &Error{Code: "passkey_cloned", Status: 401,
				Message: "this passkey looks copied to another device: sign in another way and remove it in Settings, Account"}
		}
		data, err := json.Marshal(cred)
		if err != nil {
			return err
		}
		now := s.now()
		if err := q.UpdatePasskeyCredential(ctx, db.UpdatePasskeyCredentialParams{Credential: string(data), Now: nullStr(now), ID: row.ID}); err != nil {
			return err
		}
		user := u.(passkeyUser).user
		if err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{Email: user.Email, Name: user.Name, AvatarUrl: user.AvatarUrl,
			UpdatedAt: now, LastLoginAt: nullStr(now), ID: user.ID}); err != nil {
			return err
		}
		res, err = s.signInUserTx(ctx, q, user, loginState{Next: SafeNext(next)}, userAgent, ProviderPasskey)
		return err
	})
	return res, err
}

// RenamePasskey changes the name a person gave one of their passkeys.
func (s *Service) RenamePasskey(ctx context.Context, p Principal, id, name string) ([]SignInMethod, error) {
	if err := notGuest(p); err != nil {
		return nil, err
	}
	if err := accountsOnly(s); err != nil {
		return nil, err
	}
	if p.Kind != PrincipalSession || p.UserID == "" {
		return nil, forbidden("only a signed-in person renames a passkey")
	}
	name = truncateRunes(strings.TrimSpace(name), 100)
	if name == "" {
		return nil, invalid("name", "give the passkey a name, such as MacBook")
	}
	n, err := s.W.RenamePasskey(ctx, db.RenamePasskeyParams{Name: name, ID: id, UserID: p.UserID})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, notFound("passkey")
	}
	return s.ListSignInMethods(ctx, p)
}
