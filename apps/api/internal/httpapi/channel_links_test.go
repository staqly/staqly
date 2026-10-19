package httpapi_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Connect links: the team makes a link, someone without access opens it and connects a channel (token platforms
// on the page, OAuth platforms through their sign-in with the state bound to the link). Uses, expiry, revoking.
func TestChannelLinks(t *testing.T) {
	e := newEnv(t)
	csrf := []string{"X-CSRF-Token", e.csrf}
	tokenOf := func(l app.ChannelLink) string { return l.URL[strings.Index(l.URL, "#")+1:] }

	// Scopes: channels:manage makes links; the default key only reads channels.
	e.do(nil, "POST", "/v1/channel-links", e.key, map[string]any{}, 403, nil)
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"providers": []string{"custom"}}, 422, nil)
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"max_uses": 99}, 422, nil)
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"group_id": "grp_nope"}, 422, nil)
	var link app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"note": "For ACME", "providers": []string{"faketoken"},
		"max_uses": 1}, 201, &link)
	token := tokenOf(link)
	if link.Status != "open" || token == "" || !strings.Contains(link.URL, "/add-channel#") || link.Uses != 0 {
		t.Fatalf("created: %+v", link)
	}
	var sealed string
	if err := e.svc.DB.QueryRow(`SELECT token_hash FROM channel_links WHERE id = ?`, link.ID).Scan(&sealed); err != nil || sealed == token {
		t.Fatalf("token at rest: %q %v", sealed, err)
	}

	// The public page: the workspace's name, the note and only the platforms the link allows; no session needed.
	var pub app.ChannelLinkPublic
	e.do(nil, "GET", "/v1/connect-links/"+token, "", nil, 200, &pub)
	if pub.Note != "For ACME" || pub.Workspace == "" || len(pub.Providers) != 1 || pub.Providers[0].ID != "faketoken" || pub.UsesLeft != 1 {
		t.Fatalf("public: %+v", pub)
	}
	e.do(nil, "GET", "/v1/connect-links/nope", "", nil, 404, nil)
	// A platform the link doesn't offer, and a refused token, take no use.
	e.do(nil, "POST", "/v1/connect-links/"+token+"/channels", "", map[string]any{"provider": "fakeoauth"}, 422, nil)
	e.do(nil, "POST", "/v1/connect-links/"+token+"/channels", "", map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "bad"}}, 422, nil)
	var done struct{ Name, Handle, Provider string }
	resp := e.do(nil, "POST", "/v1/connect-links/"+token+"/channels", "", map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &done)
	if done.Handle != "@fake" || done.Provider != "faketoken" {
		t.Fatalf("connected: %+v", done)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatalf("a link must not sign anyone in: %v", resp.Cookies())
	}
	// Used up: the page and connecting say so.
	e.do(nil, "GET", "/v1/connect-links/"+token, "", nil, 410, nil)
	e.do(nil, "POST", "/v1/connect-links/"+token+"/channels", "", map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 410, nil)
	var list struct{ Data []app.ChannelLink }
	e.do(nil, "GET", "/v1/channel-links", e.admin, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].Status != "used" || list.Data[0].Uses != 1 || list.Data[0].Token != "" || list.Data[0].URL != "" {
		t.Fatalf("list: %+v", list.Data)
	}
	// The channel landed in this workspace, connected by the link.
	var chans struct{ Data []app.Channel }
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &chans)
	if len(chans.Data) != 1 || chans.Data[0].Handle != "@fake" {
		t.Fatalf("channels: %+v", chans.Data)
	}
	var actor string
	if err := e.svc.DB.QueryRow(`SELECT actor_name FROM events WHERE type = 'channel.connected' ORDER BY seq DESC LIMIT 1`).Scan(&actor); err != nil ||
		!strings.Contains(actor, "Connected via link") {
		t.Fatalf("event actor: %q %v", actor, err)
	}

	// Revoke and expiry.
	var multi app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"max_uses": 3, "expires_in_hours": 1}, 201, &multi)
	e.do(nil, "DELETE", "/v1/channel-links/"+multi.ID, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/connect-links/"+tokenOf(multi), "", nil, 410, nil)
	e.do(nil, "DELETE", "/v1/channel-links/nope", e.admin, nil, 404, nil)
	var short app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"expires_in_hours": 1}, 201, &short)
	e.svc.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	e.do(nil, "GET", "/v1/connect-links/"+tokenOf(short), "", nil, 410, nil)
	e.svc.Now = time.Now

	// OAuth through a link: the state is bound to the link; the browser lands back on the link's page.
	e.do(e.browser, "PUT", "/v1/providers/fakeoauth/app", "", map[string]any{"client_id": "cid", "client_secret": "s3cret"}, 204, nil, csrf...)
	var oauth app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"providers": []string{"fakeoauth"}, "max_uses": 2}, 201, &oauth)
	jar, _ := cookiejar.New(nil)
	customer := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var start struct{ URL string }
	e.do(customer, "GET", "/v1/connect-links/"+tokenOf(oauth)+"/oauth/fakeoauth/start", "", nil, 200, &start)
	u, _ := url.Parse(start.URL)
	state := u.Query().Get("state")
	// A link's callback refuses the state from another browser (no state cookie there).
	e.do(&http.Client{CheckRedirect: customer.CheckRedirect}, "GET", "/v1/connect-links/oauth/fakeoauth/callback?code=ok&state="+state, "", nil, 302, nil)
	resp = e.do(customer, "GET", "/v1/connect-links/oauth/fakeoauth/callback?code=ok&state="+state, "", nil, 302, nil)
	loc := resp.Header.Get("Location")
	lu, _ := url.Parse(loc)
	if lu.Path != "/add-channel" || lu.Query().Get("connected") != "@oauth" || lu.Fragment != tokenOf(oauth) || lu.Query().Get("error") != "" {
		t.Fatalf("landed on %s", loc)
	}
	for _, c := range resp.Cookies() {
		if c.Name != "staqly_oauth_state" && c.Name != "staqly_connect_link" && c.MaxAge >= 0 {
			t.Fatalf("callback set cookie %s", c.Name)
		}
	}
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &chans)
	if len(chans.Data) != 2 {
		t.Fatalf("oauth channel missing: %+v", chans.Data)
	}
	// A team member's own state can't finish through the link's callback either.
	var own struct{ URL string }
	e.do(e.browser, "GET", "/v1/channels/oauth/fakeoauth/start", "", nil, 200, &own)
	ou, _ := url.Parse(own.URL)
	resp = e.do(customer, "GET", "/v1/connect-links/oauth/fakeoauth/callback?code=ok&state="+ou.Query().Get("state"), "", nil, 302, nil)
	if l2, _ := url.Parse(resp.Header.Get("Location")); l2.Query().Get("error") == "" {
		t.Fatalf("a team state finished through a link: %s", resp.Header.Get("Location"))
	}

	// Isolation: another workspace can't see or revoke this workspace's links.
	other := newWorkspace(e, "Other")
	e.do(nil, "GET", "/v1/channel-links", other.key, nil, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("other workspace sees links: %+v", list.Data)
	}
	e.do(nil, "DELETE", "/v1/channel-links/"+oauth.ID, other.key, nil, 404, nil)

	// The plan's channel limit applies to a link too, and a refused connection gives its use back.
	e.free() // 3 channels
	e.customChannel("Third")
	var full app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"providers": []string{"faketoken"}}, 201, &full)
	e.do(nil, "POST", "/v1/connect-links/"+tokenOf(full)+"/channels", "", map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 402, nil)
	e.do(nil, "GET", "/v1/connect-links/"+tokenOf(full), "", nil, 200, &pub)
	if pub.UsesLeft != 1 {
		t.Fatalf("use not given back after the plan refused: %+v", pub)
	}
}
