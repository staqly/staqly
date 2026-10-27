package providers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"  // aspect ratios of GIFs
	_ "image/jpeg" // ... JPEGs
	_ "image/png"  // ... PNGs
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // ... and WebP pictures
)

// Bluesky (or any AT Protocol server), registered twice: "bluesky" signs in with OAuth (bluesky_oauth.go), and
// "bluesky_app_password" with an app password, which staqly keeps sealed and opens a session with for every publish
// (createSession), so there is no token to renew. Both publish the same way. Posts get rich-text facets for
// links, mentions and hashtags, up to 4 pictures, and the post's thread as replies.
//
// createRecord has no idempotency key, but the record key is ours to choose: every post (and reply) gets a record
// key derived from the post and channel, and a retry first asks for that record, so it finds the post it already made
// instead of posting twice.
type Bluesky struct{ Base }

var (
	blueskyDefault = "https://bsky.social"
	// blueskyApp is where a published post is shown.
	blueskyApp = "https://bsky.app"
)

const blueskyPost = "app.bsky.feed.post"

func init() {
	limits := Limits{MaxChars: 300, MaxMedia: 4, MediaKinds: []string{"image"}, Image: imageRules["bluesky"]}
	Register(Bluesky{NewBase("bluesky", "Bluesky", AuthOAuth2, limits)}, StatusAvailable)
	pw := NewBase("bluesky_app_password", "Bluesky (app password)", AuthToken, limits)
	pw.docs = "https://docs.staqly.ai/channels/bluesky#with-an-app-password"
	Register(Bluesky{pw}, StatusAvailable)
}

// Needs: the server is its own OAuth client, so it needs an https address and no keys.
func (b Bluesky) Needs() Needs {
	return Needs{HTTPS: b.Auth() == AuthOAuth2, OwnClient: b.Auth() == AuthOAuth2}
}

func (b Bluesky) CredentialFields() []Field {
	if b.Auth() == AuthOAuth2 {
		return nil
	}
	return []Field{
		{Key: "identifier", Label: "Handle", Kind: "text", Required: true, Placeholder: "janedoe.bsky.social",
			Help: "Your handle without the @, or your DID."},
		{Key: "app_password", Label: "App password", Kind: "secret", Required: true, Placeholder: "abcd-efgh-ijkl-mnop",
			Help: "Settings > Privacy and security > App passwords > Add App Password. Not your account password."},
		{Key: "service", Label: "Server", Kind: "url", Default: blueskyDefault, Placeholder: blueskyDefault,
			Help: "Leave as is unless your account lives on its own server (PDS)."},
	}
}

func (b Bluesky) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(b.Limits(), p, s); err != nil {
		return err
	}
	rules := b.Limits().Image
	for _, m := range p.Media {
		if !strings.HasPrefix(m.MimeType, "image/") {
			return invalid("media", "Bluesky takes pictures only")
		}
		if rules != nil && m.Size > rules.MaxBytes {
			return invalid("media", m.Name+" is larger than Bluesky's 1 MB for a picture; make it smaller")
		}
	}
	return CheckFields(b.SettingsFields(), s)
}

// blueskySession is an open session on the account's server.
type blueskySession struct {
	service, did, handle, access string
	// dpop: an OAuth sign-in's key; every request then carries a DPoP proof (with the server's latest nonce).
	dpop  *ecdsa.PrivateKey
	nonce string
}

// blueskyOpen opens a session with what the channel stores: an OAuth sign-in or an app password.
func blueskyOpen(ctx context.Context, c Credentials) (*blueskySession, error) {
	if c["access_token"] != "" {
		return blueskyOAuthSession(c)
	}
	return blueskyLogin(ctx, c)
}

// profile is the account as Bluesky shows it, from the app view (the server proxies it); without it the handle is
// enough.
func (s *blueskySession) profile(ctx context.Context) Profile {
	var prof struct {
		Handle      string `json:"handle"`
		DisplayName string `json:"displayName"`
		Avatar      string `json:"avatar"`
	}
	_ = s.call(ctx, http.MethodGet, "app.bsky.actor.getProfile?actor="+url.QueryEscape(s.did), nil, "", &prof)
	handle := fediFirst(prof.Handle, s.handle, s.did)
	return Profile{Handle: "@" + handle, Name: fediFirst(prof.DisplayName, handle), AvatarURL: prof.Avatar,
		ProfileURL: blueskyApp + "/profile/" + handle}
}

func (Bluesky) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	service, err := fediInstance(f["service"], blueskyDefault, "service")
	if err != nil {
		return nil, Profile{}, err
	}
	id := strings.TrimPrefix(strings.TrimSpace(f["identifier"]), "@")
	pass := strings.TrimSpace(f["app_password"])
	if id == "" {
		return nil, Profile{}, invalid("identifier", "enter your handle, for example janedoe.bsky.social")
	}
	if pass == "" {
		return nil, Profile{}, invalid("app_password", "paste an app password")
	}
	creds := Credentials{"identifier": id, "app_password": pass, "service": service}
	sess, err := blueskyLogin(ctx, creds)
	if err != nil {
		return nil, Profile{}, err
	}
	creds["did"] = sess.did
	return creds, sess.profile(ctx), nil
}

func fediFirst(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// blueskyLogin opens a session with the stored app password.
func blueskyLogin(ctx context.Context, c Credentials) (*blueskySession, error) {
	if c["identifier"] == "" || c["app_password"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Bluesky: no account stored; reconnect the account"}
	}
	// Checked again on every use: the stored address only ever goes back to a plain https server.
	service, err := fediInstance(c["service"], blueskyDefault, "service")
	if err != nil {
		return nil, &Failure{Kind: ErrReauth, Msg: "Bluesky: the stored server address is not valid; reconnect the account"}
	}
	s := &blueskySession{service: service}
	body, _ := json.Marshal(map[string]string{"identifier": c["identifier"], "password": c["app_password"]})
	var out struct {
		AccessJwt string `json:"accessJwt"`
		DID       string `json:"did"`
		Handle    string `json:"handle"`
	}
	if err := s.call(ctx, http.MethodPost, "com.atproto.server.createSession", bytes.NewReader(body), "application/json", &out); err != nil {
		return nil, err
	}
	if out.AccessJwt == "" || out.DID == "" {
		return nil, &Failure{Kind: ErrRejected, Msg: "this server did not answer like Bluesky; check the server's address"}
	}
	// The session's scope tells an app password (com.atproto.appPass...) from the account password
	// (com.atproto.access), which staqly should never hold.
	if blueskyScope(out.AccessJwt) == "com.atproto.access" {
		return nil, &Failure{Kind: ErrReauth, Msg: "that is the account password: make an app password (Settings > Privacy and security > App passwords) and use it instead"}
	}
	s.did, s.handle, s.access = out.DID, out.Handle, out.AccessJwt
	return s, nil
}

// blueskyScope reads the scope claim of a session token (read only, the server checks it).
func blueskyScope(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.Scope
}

// Bluesky publishes a post's thread as replies under it.
var _ Replier = Bluesky{}

func (b Bluesky) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	return b.post(ctx, c, "", p)
}

func (b Bluesky) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Bluesky: nothing to reply to (the post has no id)"}
	}
	return b.post(ctx, c, parentExternalID, p)
}

// blueskyRef points at one record: what a reply names as its root and parent.
type blueskyRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// An ExternalID is "<uri>|<cid>" for a post and "<uri>|<cid>|<root uri>|<root cid>" for a reply, so the next reply
// knows both the post it answers and the thread's first post.
func (r blueskyRef) id(root blueskyRef) string {
	if root.URI == "" || root.URI == r.URI {
		return r.URI + "|" + r.CID
	}
	return r.URI + "|" + r.CID + "|" + root.URI + "|" + root.CID
}

func blueskyParent(externalID string) (parent, root blueskyRef, ok bool) {
	f := strings.Split(externalID, "|")
	switch len(f) {
	case 2:
		parent = blueskyRef{f[0], f[1]}
		root = parent
	case 4:
		parent, root = blueskyRef{f[0], f[1]}, blueskyRef{f[2], f[3]}
	default:
		return parent, root, false
	}
	return parent, root, strings.HasPrefix(parent.URI, "at://") && parent.CID != "" && root.CID != ""
}

func (Bluesky) post(ctx context.Context, c Credentials, parentID string, p PostContent) (Result, error) {
	var parent, root blueskyRef
	if parentID != "" {
		var ok bool
		if parent, root, ok = blueskyParent(parentID); !ok {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Bluesky: the post replied to has an id staqly can't read (" + parentID + ")"}
		}
	}
	sess, err := blueskyOpen(ctx, c)
	if err != nil {
		return Result{}, err
	}
	rkey := blueskyRecordKey(fediKey("bluesky", p.PostID, p.ChannelID, parentID))

	// A retry after an answer that never came: the record may be there already.
	var have struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	q := url.Values{"repo": {sess.did}, "collection": {blueskyPost}, "rkey": {rkey}}
	switch err := sess.call(ctx, http.MethodGet, "com.atproto.repo.getRecord?"+q.Encode(), nil, "", &have); {
	case err == nil && have.CID != "":
		return sess.result(blueskyRef{have.URI, have.CID}, root, rkey), nil
	case err != nil && !blueskyNotFound(err):
		return Result{}, err
	}

	record := map[string]any{
		"$type":     blueskyPost,
		"text":      p.Text,
		"createdAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	if facets := blueskyFacets(ctx, sess, p.Text); len(facets) > 0 {
		record["facets"] = facets
	}
	if len(p.Media) > 0 {
		var images []map[string]any
		for _, m := range p.Media {
			img, err := sess.upload(ctx, m)
			if err != nil {
				return Result{}, err
			}
			images = append(images, img)
		}
		record["embed"] = map[string]any{"$type": "app.bsky.embed.images", "images": images}
	}
	if parentID != "" {
		record["reply"] = map[string]any{"root": root, "parent": parent}
	}
	body, _ := json.Marshal(map[string]any{"repo": sess.did, "collection": blueskyPost, "rkey": rkey, "record": record})
	var out blueskyRef
	if err := sess.call(ctx, http.MethodPost, "com.atproto.repo.createRecord", bytes.NewReader(body), "application/json", &out); err != nil {
		return Result{}, err
	}
	if out.CID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Bluesky published the post but did not say its id; check the profile"}
	}
	return sess.result(out, root, rkey), nil
}

func (s *blueskySession) result(r, root blueskyRef, rkey string) Result {
	who := s.handle
	if who == "" || who == "handle.invalid" {
		who = s.did
	}
	return Result{ExternalID: r.id(root), URL: blueskyApp + "/profile/" + who + "/post/" + rkey}
}

// blueskyRecordKey makes a record key (a TID: 13 characters of sortable base32 over a 64-bit number, 53 bits of
// microseconds and 10 of clock id) from a stable key. Its time falls in 2023-2024, so it never sorts as a future
// record; apps order posts by createdAt, not by record key.
func blueskyRecordKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	n := binary.BigEndian.Uint64(sum[:8])
	start := uint64(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro())
	span := uint64(2 * 365 * 24 * time.Hour / time.Microsecond)
	v := (start+n%span)<<10 | uint64(binary.BigEndian.Uint16(sum[8:10])&0x3ff)
	const alphabet = "234567abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 13)
	for i := 12; i >= 0; i-- {
		out[i] = alphabet[v&31]
		v >>= 5
	}
	return string(out)
}

// upload sends a picture as a blob and returns its entry for app.bsky.embed.images (alt text empty).
func (s *blueskySession) upload(ctx context.Context, m MediaFile) (map[string]any, error) {
	r, err := m.Reader(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", m.Name, err)
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, 1_000_001))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", m.Name, err)
	}
	if len(raw) > 1_000_000 {
		return nil, &Failure{Kind: ErrRejected, Msg: m.Name + " is larger than Bluesky's 1 MB for a picture; make it smaller"}
	}
	var out struct {
		Blob json.RawMessage `json:"blob"`
	}
	if err := s.call(ctx, http.MethodPost, "com.atproto.repo.uploadBlob", bytes.NewReader(raw), m.MimeType, &out); err != nil {
		return nil, err
	}
	img := map[string]any{"alt": "", "image": out.Blob}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
		img["aspectRatio"] = map[string]int{"width": cfg.Width, "height": cfg.Height}
	}
	return img, nil
}

var (
	// A mention: @handle.domain after the start or a space (not an email address).
	blueskyMentionRe = regexp.MustCompile(`(?:^|[\s(])(@([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)`)
	// A hashtag: # after the start or a space, letters, digits and _, not only digits.
	blueskyTagRe = regexp.MustCompile(`(?:^|\s)(#[\p{L}\p{N}_]*[\p{L}_][\p{L}\p{N}_]*)`)
)

// blueskyFacets marks links, mentions and hashtags in text with their UTF-8 byte ranges, which Bluesky needs to
// make them clickable. A mention whose handle does not resolve stays plain text.
func blueskyFacets(ctx context.Context, s *blueskySession, text string) []map[string]any {
	var facets []map[string]any
	type span struct{ start, end int }
	var links []span
	add := func(start, end int, feature map[string]any) {
		facets = append(facets, map[string]any{
			"index":    map[string]int{"byteStart": start, "byteEnd": end},
			"features": []map[string]any{feature},
		})
	}
	for _, loc := range linkRe.FindAllStringIndex(text, -1) {
		u := text[loc[0]:loc[1]]
		if !isWebLink(u) || (loc[0] > 0 && text[loc[0]-1] == '@') {
			continue // not a link, or the handle of a mention
		}
		if !schemeRe.MatchString(u) {
			u = "https://" + u
		}
		links = append(links, span{loc[0], loc[1]})
		add(loc[0], loc[1], map[string]any{"$type": "app.bsky.richtext.facet#link", "uri": u})
	}
	inLink := func(i int) bool {
		for _, l := range links {
			if i >= l.start && i < l.end {
				return true
			}
		}
		return false
	}
	for _, m := range blueskyMentionRe.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2], m[3]
		if inLink(start) {
			continue
		}
		var out struct {
			DID string `json:"did"`
		}
		handle := text[start+1 : end]
		if err := s.call(ctx, http.MethodGet, "com.atproto.identity.resolveHandle?handle="+url.QueryEscape(handle), nil, "", &out); err != nil || out.DID == "" {
			continue
		}
		add(start, end, map[string]any{"$type": "app.bsky.richtext.facet#mention", "did": out.DID})
	}
	for _, m := range blueskyTagRe.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2], m[3]
		if inLink(start) || end-start-1 > 64 {
			continue
		}
		add(start, end, map[string]any{"$type": "app.bsky.richtext.facet#tag", "tag": text[start+1 : end]})
	}
	return facets
}

// call sends one XRPC request (method "nsid?query") and decodes a 2xx reply into out.
func (s *blueskySession) call(ctx context.Context, method, nsid string, body io.Reader, contentType string, out any) error {
	var payload []byte
	if body != nil {
		payload, _ = io.ReadAll(body) // sent twice when the server asks for a DPoP nonce
	}
	resp, err := s.send(ctx, method, nsid, payload, contentType)
	if err == nil && s.dpop != nil {
		if n, again := dpopRetry(resp); again {
			resp.Body.Close()
			s.nonce = n
			resp, err = s.send(ctx, method, nsid, payload, contentType)
		} else if n != "" {
			s.nonce = n
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Retrying is safe: the record key makes the next attempt find a post this one may have made.
		return &Failure{Msg: "Bluesky server " + strings.TrimPrefix(s.service, "https://") + " could not be reached; staqly tries again in a few minutes"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: "Bluesky server " + resp.Request.URL.Host + " did not answer like Bluesky; check the server's address"}
			}
		}
		return nil
	}
	return blueskyError(resp.StatusCode, raw)
}

// send makes one XRPC request: with the app password's session token, or the OAuth token and a DPoP proof.
func (s *blueskySession) send(ctx context.Context, method, nsid string, payload []byte, contentType string) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.service+"/xrpc/"+nsid, body)
	if err != nil {
		return nil, err
	}
	switch {
	case s.dpop != nil:
		proof, err := blueskyProof(s.dpop, method, req.URL.String(), s.nonce, s.access)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "DPoP "+s.access)
		req.Header.Set("DPoP", proof)
	case s.access != "":
		req.Header.Set("Authorization", "Bearer "+s.access)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return httpClient(ctx).Do(req)
}

// blueskyFailure keeps the XRPC error name, so callers can tell "no such record" from a refusal.
type blueskyFailure struct {
	Failure
	name string
}

func (f *blueskyFailure) Unwrap() error { return &f.Failure }

func blueskyNotFound(err error) bool {
	bf, ok := err.(*blueskyFailure)
	return ok && bf.name == "RecordNotFound"
}

// blueskyError maps XRPC errors ({"error": "Name", "message": "..."}) to what staqly does next.
func blueskyError(status int, raw []byte) error {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := fediFirst(e.Message, e.Error, fediSnippet(raw))
	fail := func(kind error, msg string) error {
		return &blueskyFailure{Failure: Failure{Kind: kind, Msg: msg}, name: e.Error}
	}
	switch {
	case e.Error == "RecordNotFound":
		return fail(ErrRejected, "Bluesky could not find the record ("+detail+")")
	case e.Error == "AuthFactorTokenRequired":
		return fail(ErrReauth, "Bluesky asks for an email code for this password: use an app password instead and reconnect")
	case e.Error == "AccountTakedown", e.Error == "AccountDeactivated":
		return fail(ErrReauth, "Bluesky has suspended or deactivated this account ("+detail+")")
	case status == http.StatusUnauthorized, e.Error == "AuthenticationRequired", e.Error == "InvalidToken", e.Error == "ExpiredToken":
		return fail(ErrReauth, "Bluesky no longer accepts this sign-in ("+detail+"); reconnect the account (with an app password: make a new one)")
	case status == http.StatusTooManyRequests, e.Error == "RateLimitExceeded":
		return fail(nil, "Bluesky rate limit reached; staqly tries again in a few minutes")
	case status >= 500:
		return fail(nil, fmt.Sprintf("Bluesky is unavailable (%d); staqly tries again in a few minutes", status))
	case e.Error == "BlobTooLarge", status == http.StatusRequestEntityTooLarge:
		return fail(ErrRejected, "the picture is larger than Bluesky takes ("+detail+")")
	}
	return fail(ErrRejected, "Bluesky refused it ("+detail+")")
}
