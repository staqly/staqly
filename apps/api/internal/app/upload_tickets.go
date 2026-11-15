package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Files too big for the dashboard's proxy (MaxProxyBodyBytes, below a Cloudflare proxy's 100 MB) go straight to
// the API's own address (PUBLIC_API_URL) with an upload ticket: a short-lived, signed copy of the principal that
// asked for it, good only for POST /v1/media/direct, which runs the same UploadMedia (type, plan and storage
// limits, reuse) as POST /v1/media.

// UploadTicketTTL: how long a ticket may start an upload.
const UploadTicketTTL = 10 * time.Minute

// uploadTicketPurpose keeps a ticket's signature from matching any other value signed with the secret key.
const uploadTicketPurpose = "media-upload\x00"

type UploadTicket struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
	MaxBytes  int64  `json:"max_bytes"`
}

type uploadTicketBody struct {
	P   Principal `json:"p"`
	Exp int64     `json:"exp"`
}

// MediaUploadTicket gives the caller a URL to upload one large file to directly.
func (s *Service) MediaUploadTicket(ctx context.Context, p Principal) (UploadTicket, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return UploadTicket{}, err
	}
	if s.Cfg.PublicURL == "" || len(s.Cfg.SecretKey) == 0 {
		return UploadTicket{}, &Error{Code: "conflict", Status: 409, Message: "this server has no public API address for direct uploads (PUBLIC_API_URL)"}
	}
	_, maxBytes, err := s.uploadMax(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return UploadTicket{}, err
	}
	exp := s.Now().Add(UploadTicketTTL)
	body := base64.RawURLEncoding.EncodeToString([]byte(mustJSON(uploadTicketBody{P: p, Exp: exp.Unix()})))
	return UploadTicket{URL: strings.TrimRight(s.Cfg.PublicURL, "/") + "/v1/media/direct?ticket=" + body + "." + s.ticketMAC(body),
		ExpiresAt: formatTime(exp), MaxBytes: maxBytes}, nil
}

// UploadTicketPrincipal checks a ticket's signature and expiry and answers whom it was made for.
func (s *Service) UploadTicketPrincipal(raw string) (Principal, error) {
	bad := &Error{Code: "unauthorized", Status: 401, Message: "the upload ticket is invalid or expired; ask for a new one"}
	body, sig, ok := strings.Cut(raw, ".")
	want, err := hex.DecodeString(sig)
	if !ok || err != nil || len(s.Cfg.SecretKey) == 0 {
		return Principal{}, bad
	}
	got, _ := hex.DecodeString(s.ticketMAC(body))
	if !hmac.Equal(want, got) {
		return Principal{}, bad
	}
	var t uploadTicketBody
	b, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(b, &t) != nil || s.Now().Unix() > t.Exp || t.P.WorkspaceID == "" {
		return Principal{}, bad
	}
	return t.P, nil
}

func (s *Service) ticketMAC(body string) string {
	mac := hmac.New(sha256.New, s.Cfg.SecretKey)
	mac.Write([]byte(uploadTicketPurpose + body))
	return hex.EncodeToString(mac.Sum(nil))
}
