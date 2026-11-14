package providers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Bluesky through AT Protocol OAuth: the person signs in on Bluesky and allows staqly; no app password. The server is
// its own OAuth client: its client id is the address of its client metadata (BlueskyClientMetadata, served by the
// API), it signs in with a key derived from the server's secret key (private_key_jwt) and binds every token to a key
// of its own per connection (DPoP). The sign-in starts at bsky.social; accounts on a server of their own connect with
// an app password (bluesky_app_password).
//
// The app the server hands the provider: ClientID is the client metadata's address, ClientSecret the client key
// (hex, the raw P-256 scalar).

const blueskyScopes = "atproto transition:generic"

// blueskyDirectory resolves did:plc identities.
var blueskyDirectory = "https://plc.directory"

// BlueskyClientMetadata is the document behind the client id (the AT Protocol's client metadata).
func BlueskyClientMetadata(app App, web, redirect string) (map[string]any, error) {
	key, err := blueskyClientKey(app)
	if err != nil {
		return nil, err
	}
	jwk := blueskyJWK(&key.PublicKey)
	jwk["kid"], jwk["use"], jwk["alg"] = blueskyKID(key), "sig", "ES256"
	return map[string]any{
		"client_id": app.ClientID, "application_type": "web", "client_name": "staqly", "client_uri": web,
		"redirect_uris": []string{redirect}, "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "scope": blueskyScopes, "dpop_bound_access_tokens": true,
		"token_endpoint_auth_method": "private_key_jwt", "token_endpoint_auth_signing_alg": "ES256",
		"jwks": map[string]any{"keys": []any{jwk}},
	}, nil
}

// blueskyServer is an authorization server's metadata, as much as staqly uses.
type blueskyServer struct {
	Issuer        string `json:"issuer"`
	PAR           string `json:"pushed_authorization_request_endpoint"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
}

func blueskyAuthServer(ctx context.Context, issuer string) (blueskyServer, error) {
	var m blueskyServer
	if err := blueskyGetJSON(ctx, issuer+"/.well-known/oauth-authorization-server", &m); err != nil {
		return m, err
	}
	if m.Issuer != issuer || m.PAR == "" || m.Authorization == "" || m.Token == "" {
		return m, &Failure{Kind: ErrRejected, Msg: "Bluesky's sign-in server did not describe itself as expected"}
	}
	return m, nil
}

// AuthURLContext pushes the sign-in request (PAR) and returns where the browser goes: Bluesky's sign-in page.
func (b Bluesky) AuthURLContext(ctx context.Context, app App, redirect, state string, extra map[string]string) (string, error) {
	srv, err := blueskyAuthServer(ctx, blueskyDefault)
	if err != nil {
		return "", err
	}
	verifier := extra["code_verifier"]
	sum := sha256.Sum256([]byte(verifier))
	form := url.Values{"response_type": {"code"}, "redirect_uri": {redirect}, "scope": {blueskyScopes}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	if h := strings.TrimPrefix(strings.TrimSpace(extra["handle"]), "@"); h != "" {
		form.Set("login_hint", h)
	}
	// No prompt=select_account: Bluesky refuses it (account_selection_required) when the browser has no Bluesky
	// session; without a hint its sign-in page still shows the accounts to pick from instead of choosing one.
	var out struct {
		RequestURI string `json:"request_uri"`
	}
	if err := blueskyTokenCall(ctx, app, srv, srv.PAR, form, blueskyDPoPKey(verifier), &out); err != nil {
		return "", err
	}
	// state rides along for the browser's cookie; the sign-in itself reads only the pushed request.
	q := url.Values{"client_id": {app.ClientID}, "request_uri": {out.RequestURI}, "state": {state}}
	return srv.Authorization + "?" + q.Encode(), nil
}

// AuthURL is never used: the sign-in needs the network (AuthURLContext).
func (b Bluesky) AuthURL(app App, redirect, state string, extra map[string]string) (string, error) {
	return b.AuthURLContext(context.Background(), app, redirect, state, extra)
}

func (b Bluesky) Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	srv, err := blueskyAuthServer(ctx, blueskyDefault)
	if err != nil {
		return nil, Profile{}, err
	}
	if iss := extra["iss"]; iss != "" && iss != srv.Issuer {
		return nil, Profile{}, invalid("iss", "the sign-in came back from another server")
	}
	key := blueskyDPoPKey(extra["code_verifier"])
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"code_verifier": {extra["code_verifier"]}}
	tok, err := blueskyToken(ctx, app, srv, form, key)
	if err != nil {
		return nil, Profile{}, err
	}
	// The account's own server (PDS), which must name this sign-in server as its own.
	pds, handle, err := blueskyResolve(ctx, tok.Sub)
	if err != nil {
		return nil, Profile{}, err
	}
	var res struct {
		Servers []string `json:"authorization_servers"`
	}
	if err := blueskyGetJSON(ctx, pds+"/.well-known/oauth-protected-resource", &res); err != nil {
		return nil, Profile{}, err
	}
	if len(res.Servers) == 0 || res.Servers[0] != srv.Issuer {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "this account's server signs in elsewhere; connect it with an app password"}
	}
	raw, _ := key.Bytes()
	creds := tok.creds(Credentials{"service": pds, "did": tok.Sub, "issuer": srv.Issuer, "dpop_key": hex.EncodeToString(raw)}, time.Now())
	sess, err := blueskyOpen(ctx, creds)
	if err != nil {
		return nil, Profile{}, err
	}
	sess.handle = handle
	return creds, sess.profile(ctx), nil
}

func (b Bluesky) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" || c["issuer"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Bluesky: no sign-in stored; reconnect the account"}
	}
	srv, err := blueskyAuthServer(ctx, c["issuer"])
	if err != nil {
		return nil, err
	}
	key, err := blueskyStoredKey(c)
	if err != nil {
		return nil, err
	}
	tok, err := blueskyToken(ctx, app, srv, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}}, key)
	if err != nil {
		return nil, err
	}
	if tok.Sub != c["did"] {
		return nil, &Failure{Kind: ErrReauth, Msg: "Bluesky renewed the sign-in for another account; reconnect it"}
	}
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	return tok.creds(out, time.Now()), nil
}

type blueskyTokens struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int64  `json:"expires_in"`
	Sub       string `json:"sub"`
}

func (t blueskyTokens) creds(c Credentials, now time.Time) Credentials {
	c["access_token"], c["refresh_token"] = t.Access, t.Refresh
	c["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	return c
}

func blueskyToken(ctx context.Context, app App, srv blueskyServer, form url.Values, key *ecdsa.PrivateKey) (blueskyTokens, error) {
	var t blueskyTokens
	if err := blueskyTokenCall(ctx, app, srv, srv.Token, form, key, &t); err != nil {
		return t, err
	}
	if t.Access == "" || t.Refresh == "" || !strings.HasPrefix(t.Sub, "did:") {
		return t, &Failure{Kind: ErrRejected, Msg: "Bluesky's sign-in server answered without a token"}
	}
	return t, nil
}

// blueskyTokenCall posts to the sign-in server as the client (private_key_jwt) with a DPoP proof, once more with the
// nonce the server asks for.
func blueskyTokenCall(ctx context.Context, app App, srv blueskyServer, endpoint string, form url.Values, key *ecdsa.PrivateKey, out any) error {
	ck, err := blueskyClientKey(app)
	if err != nil {
		return err
	}
	now := blueskyNow().Unix()
	assertion, err := blueskyJWT(ck, map[string]any{"alg": "ES256", "kid": blueskyKID(ck)},
		map[string]any{"iss": app.ClientID, "sub": app.ClientID, "aud": srv.Issuer, "jti": randomID(), "iat": now, "exp": now + 60})
	if err != nil {
		return err
	}
	form.Set("client_id", app.ClientID)
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	nonce := ""
	for attempt := 0; ; attempt++ {
		proof, err := blueskyProof(key, http.MethodPost, endpoint, nonce, "")
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", proof)
		resp, err := httpClient(ctx).Do(req)
		if err != nil {
			return &Failure{Msg: "Bluesky's sign-in server could not be reached; try again"}
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		clockOff := blueskyLearnClock(resp)
		var e struct {
			Error string `json:"error"`
			Desc  string `json:"error_description"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "use_dpop_nonce" && attempt == 0 {
			nonce = resp.Header.Get("DPoP-Nonce")
			continue
		}
		if e.Error == "invalid_dpop_proof" && clockOff && attempt < 2 {
			continue // signed again on the server's time
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return json.Unmarshal(raw, out)
		}
		if e.Error == "invalid_grant" {
			return &Failure{Kind: ErrReauth, Msg: "Bluesky no longer accepts this sign-in (" + fediFirst(e.Desc, e.Error) + "); reconnect the account"}
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return &Failure{Msg: fmt.Sprintf("Bluesky's sign-in server is unavailable (%d); try again", resp.StatusCode)}
		}
		if e.Error == "invalid_dpop_proof" {
			// Our signature, not the account: a clock off (a machine waking from sleep) passes, so try again later.
			return &Failure{Msg: "Bluesky did not accept staqly's request signature (" + fediFirst(e.Desc, e.Error) + "); staqly tries again in a few minutes"}
		}
		return &Failure{Kind: ErrRejected, Msg: "Bluesky refused the sign-in (" + fediFirst(e.Desc, e.Error, fediSnippet(raw)) + ")"}
	}
}

// blueskyResolve finds a DID's server (PDS) and handle in its DID document.
func blueskyResolve(ctx context.Context, did string) (pds, handle string, err error) {
	var doc struct {
		AKA     []string `json:"alsoKnownAs"`
		Service []struct {
			ID       string `json:"id"`
			Endpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	var at string
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		at = blueskyDirectory + "/" + did
	case strings.HasPrefix(did, "did:web:"):
		at = "https://" + strings.TrimPrefix(did, "did:web:") + "/.well-known/did.json"
	default:
		return "", "", &Failure{Kind: ErrRejected, Msg: "Bluesky: unknown kind of account id " + did}
	}
	if err := blueskyGetJSON(ctx, at, &doc); err != nil {
		return "", "", err
	}
	for _, s := range doc.Service {
		if strings.HasSuffix(s.ID, "#atproto_pds") {
			pds = strings.TrimRight(s.Endpoint, "/")
		}
	}
	for _, a := range doc.AKA {
		if h, ok := strings.CutPrefix(a, "at://"); ok {
			handle = h
			break
		}
	}
	if _, err := fediInstance(pds, "", "service"); err != nil || pds == "" {
		return "", "", &Failure{Kind: ErrRejected, Msg: "Bluesky: the account's server address is not valid"}
	}
	return pds, handle, nil
}

func blueskyGetJSON(ctx context.Context, at string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, at, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return &Failure{Msg: "Bluesky: " + req.URL.Host + " could not be reached; try again"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, out) != nil {
		return &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("Bluesky: %s answered %d", req.URL.Host, resp.StatusCode)}
	}
	return nil
}

// blueskyClientKey is the server's client key, from the app the server hands over.
func blueskyClientKey(app App) (*ecdsa.PrivateKey, error) {
	raw, err := hex.DecodeString(app.ClientSecret)
	if err != nil || len(raw) != 32 {
		return nil, &Failure{Kind: ErrRejected, Msg: "Bluesky: this server has no client key"}
	}
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
}

// blueskyDPoPKey is one sign-in's DPoP key, derived from its PKCE verifier: the callback recomputes the key the
// pushed request was bound to, and nothing is stored in between.
func blueskyDPoPKey(verifier string) *ecdsa.PrivateKey {
	sum := sha256.Sum256([]byte("staqly-bluesky-dpop:" + verifier))
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), sum[:])
	if err != nil { // ponytail: a sum at or past the curve's order (about 2^-32), so a random key; that sign-in fails
		k, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	return k
}

func blueskyStoredKey(c Credentials) (*ecdsa.PrivateKey, error) {
	raw, err := hex.DecodeString(c["dpop_key"])
	if err == nil {
		if k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw); err == nil {
			return k, nil
		}
	}
	return nil, &Failure{Kind: ErrReauth, Msg: "Bluesky: the stored sign-in key is not valid; reconnect the account"}
}

func blueskyJWK(pub *ecdsa.PublicKey) map[string]any {
	raw, _ := pub.Bytes() // 0x04 || X || Y
	return map[string]any{"kty": "EC", "crv": "P-256",
		"x": base64.RawURLEncoding.EncodeToString(raw[1:33]), "y": base64.RawURLEncoding.EncodeToString(raw[33:])}
}

// blueskyKID names the client key by its public key, so a new server key is a new kid.
func blueskyKID(k *ecdsa.PrivateKey) string {
	raw, _ := k.PublicKey.Bytes()
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// blueskyProof is a DPoP proof (RFC 9449) for one request; ath binds it to an access token.
func blueskyProof(key *ecdsa.PrivateKey, method, target, nonce, access string) (string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	u.RawQuery, u.Fragment = "", ""
	claims := map[string]any{"jti": randomID(), "htm": method, "htu": u.String(), "iat": blueskyNow().Unix()}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if access != "" {
		sum := sha256.Sum256([]byte(access))
		claims["ath"] = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return blueskyJWT(key, map[string]any{"typ": "dpop+jwt", "alg": "ES256", "jwk": blueskyJWK(&key.PublicKey)}, claims)
}

// blueskyJWT signs header.claims with ES256 (r || s, 32 bytes each).
func blueskyJWT(key *ecdsa.PrivateKey, header, claims map[string]any) (string, error) {
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// blueskyOAuthSession opens a session from a stored OAuth sign-in (no request: the token is used as it is).
func blueskyOAuthSession(c Credentials) (*blueskySession, error) {
	service, err := fediInstance(c["service"], "", "service")
	if err != nil || c["did"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Bluesky: the stored sign-in is not valid; reconnect the account"}
	}
	key, err := blueskyStoredKey(c)
	if err != nil {
		return nil, err
	}
	return &blueskySession{service: service, did: c["did"], access: c["access_token"], dpop: key}, nil
}

// blueskySkew is how far Bluesky's clock is from ours, in seconds, learnt from its Date header: proofs signed on a
// clock that is off (a laptop's Docker VM after sleep) are refused as too old or too new.
// ponytail: one offset for every Bluesky server, per-server if one ever runs on a clock of its own.
var blueskySkew atomic.Int64

func blueskyNow() time.Time { return time.Now().Add(time.Duration(blueskySkew.Load()) * time.Second) }

// blueskyLearnClock keeps the server's time from its Date header; it reports whether the offset changed by more
// than a few seconds (a proof refused for its time is then worth signing again).
func blueskyLearnClock(resp *http.Response) bool {
	d, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return false
	}
	off := int64(time.Until(d).Round(time.Second) / time.Second)
	if off > -3 && off < 3 {
		off = 0
	}
	return blueskySkew.Swap(off) != off
}

// blueskyDPoPRefused: the server refused the DPoP proof itself (its time, its signature), not the token.
func blueskyDPoPRefused(raw []byte) bool {
	return bytes.Contains(bytes.ToLower(raw), []byte("dpop proof"))
}

// dpopRetry: a server that answers 401 with use_dpop_nonce wants the request again with its nonce.
func dpopRetry(resp *http.Response) (string, bool) {
	n := resp.Header.Get("DPoP-Nonce")
	return n, resp.StatusCode == http.StatusUnauthorized && n != "" &&
		strings.Contains(resp.Header.Get("WWW-Authenticate"), "use_dpop_nonce")
}
