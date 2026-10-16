package app

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

const deviceLinkTTL = 5 * time.Minute

type DeviceLink struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
	Reachable bool   `json:"reachable"`
	Hint      string `json:"hint,omitempty"`
}

// CreateDeviceLink returns a one-time URL (rendered as a QR code) that signs
// another device into this workspace. The token travels in the URL fragment,
// so it never reaches server logs, proxies or Referer headers; it is stored
// hashed, expires after 5 minutes and works once. Only the dashboard can
// create links (an API key must not be able to mint browser sessions).
func (s *Service) CreateDeviceLink(ctx context.Context, p Principal, origin string) (DeviceLink, error) {
	if p.Kind != PrincipalSession && p.Kind != PrincipalLocal && p.Kind != PrincipalSystem {
		return DeviceLink{}, &Error{Code: "forbidden", Status: 403, Message: "device links can only be created from the dashboard"}
	}
	base := s.Cfg.PublicDashboardURL
	if base == "" {
		base = origin
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		host := "localhost:" + s.Cfg.DashboardPort
		if s.Cfg.DashboardPort == "" {
			host = "localhost:8080"
		}
		base, u = "http://"+host, &url.URL{Scheme: "http", Host: host}
	}
	token := randomString(32)
	now := s.Now().UTC()
	exp := formatTime(now.Add(deviceLinkTTL))
	err = s.tx(ctx, func(q *db.Queries) error {
		_ = q.PurgeDeviceLinks(ctx, formatTime(now.Add(-time.Hour)))
		if err := q.CreateDeviceLink(ctx, db.CreateDeviceLinkParams{ID: newID("dvl"), WorkspaceID: p.WorkspaceID,
			TokenHash: hashSecret(token), CreatedBy: p.Kind + ":" + p.ID, CreatedAt: formatTime(now), ExpiresAt: exp}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "device_link.created", "", "", map[string]any{"expires_at": exp})
		return err
	})
	if err != nil {
		return DeviceLink{}, err
	}
	link := DeviceLink{URL: strings.TrimRight(base, "/") + "/connect#" + token, ExpiresAt: exp, Reachable: !isLoopbackHost(u.Hostname())}
	if !link.Reachable && p.Kind != PrincipalSystem {
		link.Hint = "This address only works on this computer. To connect a phone on your network, add " +
			"PUBLIC_DASHBOARD_URL=http://<this computer's LAN IP>:8080 to .env and run `docker compose up -d`."
	}
	return link, nil
}

func isLoopbackHost(h string) bool {
	h = strings.ToLower(h)
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "::1" || strings.HasPrefix(h, "127.")
}

// RedeemDeviceLink exchanges a device-link token for a new browser session.
// RedeemDeviceLink signs a browser in with a one-time link. currentToken is the session cookie the browser
// already sends, if any: a session of the same workspace is revoked and replaced, so signing the same browser
// in again (every `make dev` opens a link) doesn't pile up entries under Devices.
func (s *Service) RedeemDeviceLink(ctx context.Context, token, userAgent, currentToken string) (NewSession, error) {
	link, err := s.Q.GetDeviceLinkByHash(ctx, hashSecret(strings.TrimSpace(token)))
	if err != nil {
		return NewSession{}, &Error{Code: "invalid_link", Status: 401, Message: "this connect link is not valid"}
	}
	var ns NewSession
	err = s.tx(ctx, func(q *db.Queries) error {
		n, err := q.UseDeviceLink(ctx, db.UseDeviceLinkParams{Now: nullStr(s.now()), ID: link.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			if link.UsedAt.Valid {
				return &Error{Code: "link_used", Status: 410, Message: "this connect link was already used; generate a new QR code"}
			}
			return &Error{Code: "link_expired", Status: 410, Message: "this connect link expired; generate a new QR code"}
		}
		if ns, err = s.createSession(ctx, q, link.WorkspaceID, userAgent); err != nil {
			return err
		}
		data := map[string]any{"user_agent": userAgent}
		if currentToken != "" {
			old, err := q.GetSessionByHash(ctx, hashSecret(currentToken))
			if err == nil && old.WorkspaceID == link.WorkspaceID && !old.RevokedAt.Valid {
				if _, err := q.RevokeSession(ctx, db.RevokeSessionParams{RevokedAt: nullStr(s.now()), WorkspaceID: old.WorkspaceID, ID: old.ID}); err != nil {
					return err
				}
				data["replaced_session_id"] = old.ID
			}
		}
		sys := Principal{WorkspaceID: link.WorkspaceID, Kind: PrincipalSystem, ID: "device-link", Name: "Connect a device"}
		_, err = s.emit(ctx, q, sys, "device.connected", "", "", data)
		return err
	})
	return ns, err
}
