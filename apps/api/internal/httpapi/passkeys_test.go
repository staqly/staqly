package httpapi_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/cookiejar"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// softKey is a software authenticator for one passkey: a P-256 key, "none" attestation, and the RP and
// origin of the test server's dashboard (PUBLIC_DASHBOARD_URL https://staqly.example.com).
type softKey struct {
	t      *testing.T
	key    *ecdsa.PrivateKey
	id     []byte
	handle []byte
}

const (
	testRPID   = "staqly.example.com"
	testOrigin = "https://staqly.example.com"
)

func newSoftKey(t *testing.T) *softKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &softKey{t: t, key: key, id: id}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (k *softKey) authData(flags protocol.AuthenticatorFlags, count uint32, attested []byte) []byte {
	h := sha256.Sum256([]byte(testRPID))
	d := append([]byte{}, h[:]...)
	d = append(d, byte(flags))
	d = binary.BigEndian.AppendUint32(d, count)
	return append(d, attested...)
}

func clientData(t *testing.T, typ, challenge string) []byte {
	b, err := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": testOrigin, "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// create answers navigator.credentials.create for the options the server sent.
func (k *softKey) create(options map[string]any) map[string]any {
	k.t.Helper()
	user := options["user"].(map[string]any)
	handle, err := base64.RawURLEncoding.DecodeString(user["id"].(string))
	if err != nil {
		k.t.Fatal(err)
	}
	k.handle = handle
	pub, err := webauthncbor.Marshal(map[int64]any{
		1: int64(webauthncose.EllipticKey), 3: int64(webauthncose.AlgES256), -1: int64(webauthncose.P256),
		-2: k.key.X.FillBytes(make([]byte, 32)), -3: k.key.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		k.t.Fatal(err)
	}
	attested := make([]byte, 16) // AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(k.id)))
	attested = append(append(attested, k.id...), pub...)
	auth := k.authData(protocol.FlagUserPresent|protocol.FlagUserVerified|protocol.FlagAttestedCredentialData, 0, attested)
	att, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": auth})
	if err != nil {
		k.t.Fatal(err)
	}
	return map[string]any{"id": b64(k.id), "rawId": b64(k.id), "type": "public-key", "response": map[string]any{
		"attestationObject": b64(att), "clientDataJSON": b64(clientData(k.t, "webauthn.create", options["challenge"].(string))),
	}}
}

// get answers navigator.credentials.get with the sign counter `count` (signer: the key that signs).
func (k *softKey) get(options map[string]any, count uint32, signer *ecdsa.PrivateKey) map[string]any {
	k.t.Helper()
	auth := k.authData(protocol.FlagUserPresent|protocol.FlagUserVerified, count, nil)
	cd := clientData(k.t, "webauthn.get", options["challenge"].(string))
	ch := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, auth...), ch[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, signer, digest[:])
	if err != nil {
		k.t.Fatal(err)
	}
	return map[string]any{"id": b64(k.id), "rawId": b64(k.id), "type": "public-key", "response": map[string]any{
		"authenticatorData": b64(auth), "clientDataJSON": b64(cd), "signature": b64(sig), "userHandle": b64(k.handle),
	}}
}

type challengeOut struct {
	ID      string         `json:"challenge_id"`
	Options map[string]any `json:"options"`
}

// passkeySignIn runs a sign-in in a new browser with the authenticator's answer.
func (e *acctEnv) passkeySignIn(k *softKey, count uint32, signer *ecdsa.PrivateKey, want int, out any) *person {
	e.t.Helper()
	var ch challengeOut
	e.do(nil, "POST", "/v1/auth/passkey/start", "", nil, 200, &ch)
	jar, _ := cookiejar.New(nil)
	c := noRedirects(jar)
	e.do(c, "POST", "/v1/auth/passkey/finish", "", map[string]any{"challenge_id": ch.ID, "response": k.get(ch.Options, count, signer)}, want, out)
	if want != 201 {
		return nil
	}
	p := &person{c: c}
	var s struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(c, "GET", "/v1/session", "", nil, 200, &s)
	p.csrf = s.CSRF
	return p
}

func TestPasskeyAddSignInRenameRemove(t *testing.T) {
	e, _ := newEmailEnv(t, "")
	var st map[string]any
	e.do(nil, "GET", "/v1/setup", "", nil, 200, &st)
	if st["passkey_sign_in"] != true {
		t.Fatalf("setup: %v", st)
	}
	zoe := e.emailSignIn("zoe@example.com", nil)
	me := e.session(zoe).User.ID
	k := newSoftKey(t)

	var ch challengeOut
	e.as(zoe, "POST", "/v1/account/sign-in-methods/passkey/start", nil, 200, &ch)
	if ch.Options["rp"].(map[string]any)["id"] != testRPID {
		t.Fatalf("options: %+v", ch.Options)
	}
	if sel := ch.Options["authenticatorSelection"].(map[string]any); sel["residentKey"] != "required" {
		t.Fatalf("not discoverable: %+v", sel)
	}
	answer := k.create(ch.Options)
	var list struct {
		Data []app.SignInMethod `json:"data"`
	}
	e.as(zoe, "POST", "/v1/account/sign-in-methods/passkey/finish", map[string]any{"challenge_id": ch.ID, "name": " MacBook ", "response": answer}, 201, &list)
	if len(list.Data) != 2 || list.Data[1].Provider != app.ProviderPasskey || list.Data[1].Name != "MacBook" || list.Data[1].LastLoginAt != nil {
		t.Fatalf("after adding: %+v", list)
	}
	pk := list.Data[1].Subject
	// The challenge works once.
	var errOut apiErr
	e.as(zoe, "POST", "/v1/account/sign-in-methods/passkey/finish", map[string]any{"challenge_id": ch.ID, "response": answer}, 410, &errOut)
	if errOut.Error.Code != "passkey_expired" {
		t.Fatalf("replay: %+v", errOut)
	}
	// The same device again: excluded in the options, refused if answered anyway.
	e.as(zoe, "POST", "/v1/account/sign-in-methods/passkey/start", nil, 200, &ch)
	if ex, _ := ch.Options["excludeCredentials"].([]any); len(ex) != 1 {
		t.Fatalf("exclusions: %+v", ch.Options["excludeCredentials"])
	}
	e.as(zoe, "POST", "/v1/account/sign-in-methods/passkey/finish", map[string]any{"challenge_id": ch.ID, "response": k.create(ch.Options)}, 409, nil)

	// Sign in with it, no address typed.
	p := e.passkeySignIn(k, 1, k.key, 201, nil)
	if s := e.session(p); s.User.ID != me {
		t.Fatalf("passkey signed in %+v", s.User)
	}
	// Another key's signature, and a counter that went backwards (a copied authenticator), are refused.
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	e.passkeySignIn(k, 2, other, 401, &errOut)
	if errOut.Error.Code != "passkey_invalid" {
		t.Fatalf("wrong key: %+v", errOut)
	}
	e.passkeySignIn(k, 1, k.key, 401, &errOut)
	if errOut.Error.Code != "passkey_cloned" {
		t.Fatalf("clone: %+v", errOut)
	}
	// A refused answer uses its challenge up: a second try with the right key needs a new one.
	var sch challengeOut
	e.do(nil, "POST", "/v1/auth/passkey/start", "", nil, 200, &sch)
	e.do(nil, "POST", "/v1/auth/passkey/finish", "", map[string]any{"challenge_id": sch.ID, "response": k.get(sch.Options, 3, other)}, 401, nil)
	e.do(nil, "POST", "/v1/auth/passkey/finish", "", map[string]any{"challenge_id": sch.ID, "response": k.get(sch.Options, 3, k.key)}, 410, nil)
	// An answer for a challenge of another kind (registration) does not sign in.
	e.as(zoe, "POST", "/v1/account/sign-in-methods/passkey/start", nil, 200, &ch)
	e.do(nil, "POST", "/v1/auth/passkey/finish", "", map[string]any{"challenge_id": ch.ID, "response": k.get(ch.Options, 5, k.key)}, 410, nil)

	e.as(zoe, "PATCH", "/v1/account/sign-in-methods/passkey/"+pk, map[string]any{"name": "Work laptop"}, 200, &list)
	if list.Data[1].Name != "Work laptop" || list.Data[1].LastLoginAt == nil {
		t.Fatalf("after rename: %+v", list)
	}
	e.as(zoe, "PATCH", "/v1/account/sign-in-methods/passkey/"+pk, map[string]any{"name": " "}, 422, nil)
	// Someone else cannot rename or remove it.
	bob := e.emailSignIn("bob@example.com", nil)
	e.as(bob, "PATCH", "/v1/account/sign-in-methods/passkey/"+pk, map[string]any{"name": "Mine"}, 404, nil)

	// The passkey keeps Zoe signed in once her address is gone; the last method stays.
	e.as(zoe, "DELETE", "/v1/account/sign-in-methods/email/zoe@example.com", nil, 200, &list)
	e.as(zoe, "DELETE", "/v1/account/sign-in-methods/passkey/"+pk, nil, 409, &errOut)
	if errOut.Error.Code != "last_sign_in_method" {
		t.Fatalf("last: %+v", errOut)
	}
	var events struct {
		Data []struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	e.as(zoe, "GET", "/v1/events?type=sign_in_method.added", nil, 200, &events)
	if len(events.Data) != 1 {
		t.Fatalf("events: %+v", events)
	}
}
