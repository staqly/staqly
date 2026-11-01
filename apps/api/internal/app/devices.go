package app

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
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
	if err := notGuest(p); err != nil {
		return DeviceLink{}, err
	}
	return s.createDeviceLink(ctx, p, origin, GuestLink{For: deviceLinkTTL, Uses: 1})
}

// GuestLink: how long a link works, how many times it signs a browser in (0: any number), and with which role
// (RoleEditor or RoleViewer; "" for an ordinary connect link). A guest's sessions end with the link.
type GuestLink struct {
	For  time.Duration
	Uses int
	Role string
}

// CreateGuestLink is `server connect-link --email --for`: the operator lets someone into the account of the person
// with this address for a while, without their mailbox. Sessions from it have Role at most (never billing or
// deleting the workspace) and can't add a way in that outlasts the link (notGuest). workspaceID "" picks the
// person's only workspace.
func (s *Service) CreateGuestLink(ctx context.Context, email, workspaceID string, g GuestLink) (DeviceLink, error) {
	if g.Role != "" && g.Role != RoleEditor && g.Role != RoleViewer {
		return DeviceLink{}, invalid("role", "a guest is an editor or a viewer")
	}
	if g.For <= 0 || g.Uses < 0 {
		return DeviceLink{}, invalid("for", "a link needs a duration and 0 or more uses")
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return DeviceLink{}, err
	}
	user, err := s.Q.OldestUserByEmail(ctx, email)
	if err != nil {
		return DeviceLink{}, notFound("person with this address")
	}
	if workspaceID == "" {
		rows, err := s.Q.ListUserWorkspaces(ctx, user.ID)
		if err != nil {
			return DeviceLink{}, err
		}
		if len(rows) != 1 {
			return DeviceLink{}, invalid("workspace_id", email+" is in "+strconv.Itoa(len(rows))+" workspaces: pass one (see `server workspaces`)")
		}
		workspaceID = rows[0].ID
	}
	if _, err := s.userPrincipal(ctx, s.Q, user.ID, workspaceID); err != nil {
		return DeviceLink{}, notFound("membership of " + email + " in this workspace")
	}
	p := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "cli", Name: "Server CLI", UserID: user.ID}
	return s.createDeviceLink(ctx, p, "", g)
}

func (s *Service) createDeviceLink(ctx context.Context, p Principal, origin string, g GuestLink) (DeviceLink, error) {
	// In a workspace with people the link signs in its maker (their role is read on every request as usual);
	// without a person behind the session there is no one to sign in.
	if p.UserID == "" && s.workspaceHasPeople(ctx, s.Q, p.WorkspaceID) {
		return DeviceLink{}, forbidden("sign in as a person to connect another device")
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
	exp := formatTime(now.Add(g.For))
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.PurgeDeviceLinks(ctx, formatTime(now.Add(-time.Hour))); err != nil { // a failed statement aborts a PostgreSQL transaction
			return err
		}
		if err := q.CreateDeviceLink(ctx, db.CreateDeviceLinkParams{ID: newID("dvl"), WorkspaceID: p.WorkspaceID,
			TokenHash: hashSecret(token), CreatedBy: p.Kind + ":" + p.ID, CreatedAt: formatTime(now), ExpiresAt: exp, UserID: p.UserID,
			MaxUses: int64(g.Uses), GuestRole: g.Role}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "device_link.created", "", "", map[string]any{"expires_at": exp, "uses": g.Uses, "guest_role": g.Role})
		return err
	})
	if err != nil {
		return DeviceLink{}, err
	}
	if s.Cfg.DashboardExposed && p.Kind != PrincipalSystem && isLoopbackHost(u.Hostname()) {
		// Opened as localhost: point the phone at this computer's address on the network instead
		// (`connect-link` is opened on this computer, so it keeps localhost).
		if ip := LANIP(); ip != "" {
			if port := u.Port(); port != "" {
				ip = net.JoinHostPort(ip, port)
			}
			u.Host = ip
			base = u.String()
		}
	}
	link := DeviceLink{URL: strings.TrimRight(base, "/") + "/connect#" + token, ExpiresAt: exp, Reachable: !isLoopbackHost(u.Hostname())}
	if !link.Reachable && p.Kind != PrincipalSystem {
		link.Hint = "This address only works on this computer. Open the dashboard at this computer's network " +
			"address (http://<its IP on your Wi-Fi>:8080) and create the link there."
		if !s.Cfg.DashboardExposed {
			link.Hint = "The dashboard only listens on this computer. Restart it with `staqly serve --expose` to open it from a phone."
		}
	}
	return link, nil
}

// LANIP is this computer's private IPv4 address on the local network, or "" when unknown. Inside a container
// the address is the container's own, which phones can't reach, so it is not used there. A var for tests.
var LANIP = func() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return ""
	}
	// No packet is sent: dialing UDP only picks the interface that routes outward.
	c, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok && a.IP.IsPrivate() {
		return a.IP.String()
	}
	return ""
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
			if link.MaxUses > 0 && link.Uses >= link.MaxUses {
				return &Error{Code: "link_used", Status: 410, Message: "this connect link was already used; generate a new QR code"}
			}
			return &Error{Code: "link_expired", Status: 410, Message: "this connect link expired; generate a new QR code"}
		}
		if link.UserID != "" {
			// Still allowed in: removed members and deleted people get nothing.
			if _, err := s.userPrincipal(ctx, q, link.UserID, link.WorkspaceID); err != nil {
				return &Error{Code: "invalid_link", Status: 401, Message: "this connect link is not valid"}
			}
			if link.GuestRole != "" {
				until, _ := time.Parse(timeFormat, link.ExpiresAt)
				ns, _, err = s.newUserSession(ctx, q, link.WorkspaceID, link.UserID, userAgent, until, link.GuestRole)
			} else {
				ns, _, err = s.createUserSession(ctx, q, link.WorkspaceID, link.UserID, userAgent)
			}
		} else {
			// A link from before the workspace had people signs no one in now.
			n, err := q.CountMembers(ctx, link.WorkspaceID)
			if err != nil {
				return err
			}
			if n > 0 {
				return &Error{Code: "invalid_link", Status: 401, Message: "this connect link is not valid"}
			}
			ns, err = s.createSession(ctx, q, link.WorkspaceID, userAgent)
		}
		if err != nil {
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
