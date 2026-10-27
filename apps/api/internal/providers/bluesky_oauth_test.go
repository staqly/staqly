package providers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// blueskyVerify checks a compact ES256 JWT against a public key and returns its header and claims.
func blueskyVerify(t *testing.T, jwt string, pub *ecdsa.PublicKey) (header, claims map[string]any) {
	t.Helper()
	p := strings.Split(jwt, ".")
	if len(p) != 3 {
		t.Fatalf("not a JWT: %q", jwt)
	}
	h, _ := base64.RawURLEncoding.DecodeString(p[0])
	c, _ := base64.RawURLEncoding.DecodeString(p[1])
	_ = json.Unmarshal(h, &header)
	_ = json.Unmarshal(c, &claims)
	if pub == nil { // a DPoP proof carries its own key
		jwk := header["jwk"].(map[string]any)
		x, _ := base64.RawURLEncoding.DecodeString(jwk["x"].(string))
		y, _ := base64.RawURLEncoding.DecodeString(jwk["y"].(string))
		pub = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
	}
	sig, _ := base64.RawURLEncoding.DecodeString(p[2])
	sum := sha256.Sum256([]byte(p[0] + "." + p[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatalf("bad signature on %v", claims)
	}
	return header, claims
}

// A sign-in end to end against a fake bsky.social, directory and PDS: PAR and token with the client's key and a
// DPoP nonce, the account's server checked, a post with an OAuth token, then a refresh.
func TestBlueskyOAuth(t *testing.T) {
	clientKey := blueskyDPoPKey("client")
	raw, _ := clientKey.Bytes()
	var srv *httptest.Server
	app := App{ClientID: "https://staqly.example.com/api/v1/oauth/bluesky/client-metadata.json", ClientSecret: hex.EncodeToString(raw)}
	var dpopJKT string
	refreshes, posts := 0, 0
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": srv.URL, "pushed_authorization_request_endpoint": srv.URL + "/oauth/par",
			"authorization_endpoint": srv.URL + "/oauth/authorize", "token_endpoint": srv.URL + "/oauth/token"})
	})
	tokenEndpoint := func(w http.ResponseWriter, r *http.Request, answer map[string]any) {
		_ = r.ParseForm()
		_, claims := blueskyVerify(t, r.Form.Get("client_assertion"), &clientKey.PublicKey)
		if claims["iss"] != app.ClientID || claims["aud"] != srv.URL || r.Form.Get("client_id") != app.ClientID {
			t.Errorf("client assertion %v", claims)
		}
		header, proof := blueskyVerify(t, r.Header.Get("DPoP"), nil)
		jwk, _ := json.Marshal(header["jwk"])
		if dpopJKT == "" {
			dpopJKT = string(jwk)
		} else if dpopJKT != string(jwk) {
			t.Errorf("another DPoP key: %s", jwk)
		}
		if proof["nonce"] != "n1" {
			w.Header().Set("DPoP-Nonce", "n1")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"use_dpop_nonce"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(answer)
	}
	mux.HandleFunc("/oauth/par", func(w http.ResponseWriter, r *http.Request) {
		tokenEndpoint(w, r, map[string]any{"request_uri": "urn:ietf:params:oauth:request_uri:abc"})
		if r.Form.Get("code_challenge_method") != "S256" || r.Form.Get("scope") != blueskyScopes {
			t.Errorf("PAR form %v", r.Form)
		}
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshes++
			if r.Form.Get("refresh_token") != "r1" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		}
		tokenEndpoint(w, r, map[string]any{"access_token": "a1", "refresh_token": "r1", "expires_in": 3600, "sub": "did:plc:jane", "token_type": "DPoP"})
	})
	mux.HandleFunc("/did:plc:jane", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"alsoKnownAs": []string{"at://jane.bsky.social"},
			"service": []map[string]string{{"id": "#atproto_pds", "type": "AtprotoPersonalDataServer", "serviceEndpoint": srv.URL}}})
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{srv.URL}})
	})
	mux.HandleFunc("/xrpc/", func(w http.ResponseWriter, r *http.Request) {
		_, proof := blueskyVerify(t, r.Header.Get("DPoP"), nil)
		if r.Header.Get("Authorization") != "DPoP a1" || proof["ath"] == nil {
			t.Errorf("xrpc auth %q %v", r.Header.Get("Authorization"), proof)
		}
		if proof["nonce"] != "p1" {
			w.Header().Set("DPoP-Nonce", "p1")
			w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/xrpc/app.bsky.actor.getProfile":
			_, _ = w.Write([]byte(`{"handle":"jane.bsky.social","displayName":"Jane Doe"}`))
		case "/xrpc/com.atproto.repo.getRecord":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"RecordNotFound"}`))
		case "/xrpc/com.atproto.repo.createRecord":
			posts++
			_, _ = w.Write([]byte(`{"uri":"at://did:plc:jane/app.bsky.feed.post/x","cid":"c1"}`))
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	defer func(d, dir string) { blueskyDefault, blueskyDirectory = d, dir }(blueskyDefault, blueskyDirectory)
	blueskyDefault, blueskyDirectory = srv.URL, srv.URL

	e, _ := Get("bluesky")
	b := e.Provider.(Bluesky)
	ctx := context.Background()
	extra := map[string]string{"code_verifier": "verifier-0123456789-0123456789-0123456789"}
	u, err := b.AuthURLContext(ctx, app, "https://staqly.example.com/channels/callback/bluesky", "st", extra)
	if err != nil {
		t.Fatal(err)
	}
	if q, _ := url.Parse(u); q.Query().Get("request_uri") == "" || q.Query().Get("state") != "st" || q.Query().Get("client_id") != app.ClientID {
		t.Fatalf("auth URL %s", u)
	}
	extra["iss"] = srv.URL
	creds, prof, err := b.Exchange(ctx, app, "https://staqly.example.com/channels/callback/bluesky", "code", extra)
	if err != nil {
		t.Fatal(err)
	}
	if creds["service"] != srv.URL || creds["did"] != "did:plc:jane" || prof.Handle != "@jane.bsky.social" || prof.Name != "Jane Doe" {
		t.Fatalf("creds %v profile %+v", creds, prof)
	}
	if _, err := b.Publish(ctx, creds, PostContent{PostID: "pst_1", ChannelID: "ch_1", Text: "Hello"}, nil); err != nil || posts != 1 {
		t.Fatalf("publish: %v, %d posts", err, posts)
	}
	fresh, err := b.Refresh(ctx, app, creds)
	if err != nil || fresh["access_token"] != "a1" || fresh["dpop_key"] != creds["dpop_key"] || refreshes == 0 {
		t.Fatalf("refresh %v %v", fresh, err)
	}
	creds["refresh_token"] = "used"
	if _, err := b.Refresh(ctx, app, creds); !errors.Is(err, ErrReauth) {
		t.Fatalf("a refused refresh: %v", err)
	}
	// Coming back from another sign-in server is refused.
	extra["iss"] = "https://evil.example.com"
	if _, _, err := b.Exchange(ctx, app, "https://staqly.example.com/channels/callback/bluesky", "code", extra); err == nil {
		t.Fatal("another issuer was accepted")
	}
	// The metadata names the key the assertions are signed with.
	m, err := BlueskyClientMetadata(app, "https://staqly.example.com", "https://staqly.example.com/channels/callback/bluesky")
	if err != nil || m["client_id"] != app.ClientID || m["token_endpoint_auth_method"] != "private_key_jwt" {
		t.Fatalf("metadata %v %v", m, err)
	}
}
