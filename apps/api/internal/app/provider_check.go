package app

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// SetupCheck is one thing a platform needs from this server, and whether it has it.
type SetupCheck struct {
	Key    string `json:"key"` // keys, platform, return_address, media
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// ProviderCheck: whether this server is set up to connect and publish to a platform, independent of any account.
type ProviderCheck struct {
	Provider string       `json:"provider"`
	OK       bool         `json:"ok"`
	Checks   []SetupCheck `json:"checks"`
}

// CheckProvider checks the server's side of a platform: its app keys (with the platform itself where it can tell
// without an account), the return address to register, and media storage the platform can download from.
func (s *Service) CheckProvider(ctx context.Context, p Principal, providerID string) (ProviderCheck, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return ProviderCheck{}, err
	}
	e, ok := providers.Get(providerID)
	if !ok {
		return ProviderCheck{}, notFound("provider")
	}
	if e.Auth() != providers.AuthOAuth2 {
		return ProviderCheck{}, invalid("provider", "only platforms connected through their sign-in have server setup to check")
	}
	out := ProviderCheck{Provider: providerID, OK: true, Checks: []SetupCheck{}}
	add := func(c SetupCheck) {
		out.Checks = append(out.Checks, c)
		out.OK = out.OK && c.OK
	}

	app, appErr := s.providerApp(ctx, p.WorkspaceID, providerID)
	switch {
	case !s.providerEnabled(providerID):
		add(SetupCheck{Key: "keys", Label: "App keys", Detail: e.Name() + " is turned off on this server"})
	case appErr != nil:
		add(SetupCheck{Key: "keys", Label: "App keys", Detail: "No app keys yet: register an app with " + e.Name() + " and give this server its keys"})
	default:
		add(SetupCheck{Key: "keys", Label: "App keys", OK: true, Detail: "Set"})
	}

	redirect := s.WebURL() + "/channels/callback/" + providerID
	if checker, ok := e.Provider.(providers.AppChecker); ok && appErr == nil {
		cctx, cancel := context.WithTimeout(providers.WithHTTPClient(ctx, s.safeClient()), 10*time.Second)
		verified, err := checker.CheckApp(cctx, app, redirect)
		cancel()
		c := SetupCheck{Key: "platform", Label: e.Name(), OK: err == nil}
		switch {
		case err != nil:
			c.Detail = err.Error()
		case verified:
			c.Detail = e.Name() + " accepts the app keys"
		default:
			c.Detail = e.Name() + " answers; it checks the app keys when an account connects"
		}
		add(c)
	}

	var needs providers.Needs
	if n, ok := e.Provider.(providers.Needer); ok {
		needs = n.Needs()
	}
	ret := SetupCheck{Key: "return_address", Label: "Return address", OK: true, Detail: redirect}
	if u, err := url.Parse(redirect); err != nil || (needs.HTTPS && u.Scheme != "https") {
		ret.OK = false
		ret.Detail = e.Name() + " only sends people back to an https address, and this server runs at " + s.WebURL() + ". Serve it over https (your own domain or a tunnel) to connect " + e.Name() + "."
	}
	add(ret)

	if needs.PublicMedia {
		add(s.mediaCheck(ctx, e.Name()))
	}
	return out, nil
}

// mediaCheck: a platform that downloads media needs links to it on an address the internet can reach.
func (s *Service) mediaCheck(ctx context.Context, name string) SetupCheck {
	c := SetupCheck{Key: "media", Label: "Media storage"}
	link := ""
	if s.Cfg.Blob != nil {
		link, _ = s.Cfg.Blob.SignedURL(ctx, "staqly-setup-check", time.Minute)
	}
	if link == "" {
		// For whoever runs the server: STORAGE_DRIVER=s3 with a bucket the platform can reach.
		c.Detail = name + " downloads pictures and videos from a link, and this server keeps them on its own disk: store media in cloud storage"
		return c
	}
	u, err := url.Parse(link)
	if err != nil || !publicHost(u.Hostname()) {
		c.Detail = name + " downloads pictures and videos from a link, and media links point at " + u.Host + ", which the internet can't reach"
		return c
	}
	c.OK, c.Detail = true, "Links point at "+u.Host
	return c
}

// publicHost: a name or address the internet can reach (not localhost, a bare container name or a private address).
func publicHost(h string) bool {
	if ip := net.ParseIP(h); ip != nil {
		return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
	}
	return h != "" && h != "localhost" && strings.Contains(h, ".") && !strings.HasSuffix(h, ".local") && !strings.HasSuffix(h, ".localhost")
}
