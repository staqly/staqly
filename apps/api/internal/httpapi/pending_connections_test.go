package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakePages is an OAuth platform whose sign-in may publish as several accounts (like Facebook Pages): the code
// says how many ("one" or "two").
type fakePages struct{ providers.Base }

func (fakePages) AuthURL(_ providers.App, redirect, state string, _ map[string]string) (string, error) {
	return "https://pages.example/authorize?redirect_uri=" + url.QueryEscape(redirect) + "&state=" + state, nil
}

func (fakePages) Exchange(_ context.Context, _ providers.App, _, code string, _ map[string]string) (providers.Credentials, providers.Profile, error) {
	if code != "one" && code != "two" && code != "none" {
		return nil, providers.Profile{}, errors.New("bad code")
	}
	return providers.Credentials{"user_token": "ut_" + code}, providers.Profile{Name: "Jane Doe"}, nil
}

func (fakePages) Accounts(_ context.Context, c providers.Credentials) ([]providers.Account, error) {
	switch c["user_token"] {
	case "ut_none":
		return nil, nil
	case "ut_one":
		return []providers.Account{{ID: "p1", Name: "Page One"}}, nil
	}
	return []providers.Account{{ID: "p1", Name: "Page One", Handle: "@pageone"}, {ID: "p2", Name: "Page Two", Handle: "@pagetwo"}}, nil
}

func (fakePages) UseAccount(_ context.Context, c providers.Credentials, id string) (providers.Credentials, providers.Profile, error) {
	if c["user_token"] == "" {
		return nil, providers.Profile{}, errors.New("no sign-in")
	}
	return providers.Credentials{"page_token": "pt_" + id, "page_id": id, "refresh_token": pagesRefresh.Load().(string)},
		providers.Profile{Handle: "@page" + id, Name: "Page " + id}, nil
}

// pagesRefresh is the one refresh token the fake platform takes: each refresh retires it and hands out a new one.
var pagesRefresh atomic.Value

func (fakePages) Refresh(_ context.Context, _ providers.App, c providers.Credentials) (providers.Credentials, error) {
	if c["refresh_token"] != pagesRefresh.Load().(string) {
		return nil, &providers.Failure{Kind: providers.ErrReauth, Msg: "refresh token retired"}
	}
	next := c["refresh_token"] + "+"
	pagesRefresh.Store(next)
	out := providers.Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["refresh_token"] = next
	return out, nil
}

func init() {
	pagesRefresh.Store("rt")
	providers.Register(fakePages{providers.NewBase("fakepages", "Fake Pages", providers.AuthOAuth2, providers.Limits{MaxChars: 500})}, providers.StatusAvailable)
}

// pagesCallback runs a sign-in with code through the team's flow and returns where the browser landed.
func pagesCallback(t *testing.T, e *env, code string) url.Values {
	t.Helper()
	noRedirect := &http.Client{Jar: e.browser.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var start struct{ URL string }
	e.do(e.browser, "GET", "/v1/channels/oauth/fakepages/start", "", nil, 200, &start)
	u, _ := url.Parse(start.URL)
	resp := e.do(noRedirect, "GET", "/v1/channels/oauth/fakepages/callback?code="+code+"&state="+u.Query().Get("state"), "", nil, 302, nil)
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if !strings.HasSuffix(loc.Path, "/channels/callback/fakepages") {
		t.Fatalf("landed on %s", loc)
	}
	return loc.Query()
}

// A sign-in that may publish as several accounts waits for the person to pick them; each picked account becomes
// its own channel, picking one already connected reconnects it, and the sign-in is used once.
func TestPendingConnections(t *testing.T) {
	e := newEnv(t)
	csrf := []string{"X-CSRF-Token", e.csrf}
	e.do(e.browser, "PUT", "/v1/providers/fakepages/app", "", map[string]any{"client_id": "cid", "client_secret": "s3cret"}, 204, nil, csrf...)

	// One account: connected right away, no choice to make.
	q := pagesCallback(t, e, "one")
	if q.Get("channel") == "" || q.Get("pending") != "" {
		t.Fatalf("one account: %v", q)
	}
	// No account at all: an error the person can act on.
	if q := pagesCallback(t, e, "none"); !strings.Contains(q.Get("error"), "no account") {
		t.Fatalf("no account: %v", q)
	}

	// Two accounts: the browser lands with a pending connection to pick from.
	q = pagesCallback(t, e, "two")
	id := q.Get("pending")
	if id == "" || q.Get("channel") != "" {
		t.Fatalf("two accounts: %v", q)
	}
	var pc app.PendingConnection
	e.do(e.browser, "GET", "/v1/pending-connections/"+id, "", nil, 200, &pc)
	if pc.Provider != "fakepages" || len(pc.Accounts) != 2 || pc.Accounts[1].Handle != "@pagetwo" || pc.ExpiresAt == "" {
		t.Fatalf("pending: %+v", pc)
	}
	var sealed string
	if err := e.svc.DB.QueryRow(`SELECT credentials FROM pending_connections WHERE id = ?`, id).Scan(&sealed); err != nil || strings.Contains(sealed, "ut_two") {
		t.Fatalf("sign-in at rest: %q %v", sealed, err)
	}

	// Another workspace and a key without channels:manage can't see or finish it.
	other := newWorkspace(e, "Other")
	e.do(other.browser, "GET", "/v1/pending-connections/"+id, "", nil, 404, nil)
	e.do(other.browser, "POST", "/v1/pending-connections/"+id, "", map[string]any{"account_ids": []string{"p1"}}, 404, nil, "X-CSRF-Token", other.csrf)
	e.do(nil, "GET", "/v1/pending-connections/"+id, e.key, nil, 403, nil)

	// The choice must come from the list.
	e.do(e.browser, "POST", "/v1/pending-connections/"+id, "", map[string]any{"account_ids": []string{}}, 422, nil, csrf...)
	e.do(e.browser, "POST", "/v1/pending-connections/"+id, "", map[string]any{"account_ids": []string{"p9"}}, 422, nil, csrf...)

	var out struct{ Data []app.Channel }
	e.do(e.browser, "POST", "/v1/pending-connections/"+id, "", map[string]any{"account_ids": []string{"p1", "p2", "p1"}}, 201, &out, csrf...)
	if len(out.Data) != 2 || out.Data[0].Handle != "@pagep1" || out.Data[1].Handle != "@pagep2" {
		t.Fatalf("finished: %+v", out.Data)
	}
	var list struct{ Data []app.Channel }
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &list)
	if len(list.Data) != 2 { // p1 was connected by the one-account sign-in and reconnected now
		t.Fatalf("channels: %+v", list.Data)
	}
	// The two channels came from one sign-in: renewing it for one hands the new refresh token to the other, so the
	// platform retiring the old one doesn't sign the other out.
	for _, ch := range out.Data {
		var test map[string]any
		e.do(e.browser, "POST", "/v1/channels/"+ch.ID+"/test", "", nil, 200, &test, csrf...)
		if test["ok"] != true {
			t.Fatalf("test %s after a sibling refreshed: %v", ch.Handle, test)
		}
	}
	// Used once.
	e.do(e.browser, "POST", "/v1/pending-connections/"+id, "", map[string]any{"account_ids": []string{"p1"}}, 404, nil, csrf...)
	e.do(e.browser, "GET", "/v1/pending-connections/"+id, "", nil, 404, nil)
}

// Through a connect link: the customer picks on the link's page; every picked account takes one use.
func TestPendingConnectionsViaLink(t *testing.T) {
	e := newEnv(t)
	csrf := []string{"X-CSRF-Token", e.csrf}
	e.do(e.browser, "PUT", "/v1/providers/fakepages/app", "", map[string]any{"client_id": "cid", "client_secret": "s3cret"}, 204, nil, csrf...)
	var link app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{"providers": []string{"fakepages"}, "max_uses": 2}, 201, &link)
	token := tokenOf(link)
	jar, _ := cookiejar.New(nil)
	customer := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var start struct{ URL string }
	e.do(customer, "GET", "/v1/connect-links/"+token+"/oauth/fakepages/start", "", nil, 200, &start)
	u, _ := url.Parse(start.URL)
	resp := e.do(customer, "GET", "/v1/connect-links/oauth/fakepages/callback?code=two&state="+u.Query().Get("state"), "", nil, 302, nil)
	lu, _ := url.Parse(resp.Header.Get("Location"))
	id := lu.Query().Get("pending")
	if lu.Path != "/add-channel" || id == "" || lu.Fragment != token {
		t.Fatalf("landed on %s", lu)
	}
	// The sign-in took no use yet.
	var links struct{ Data []app.ChannelLink }
	e.do(nil, "GET", "/v1/channel-links", e.admin, nil, 200, &links)
	if links.Data[0].Uses != 0 {
		t.Fatalf("uses before picking: %+v", links.Data[0])
	}
	// The team's own endpoint doesn't finish a link's sign-in, nor does another link.
	e.do(e.browser, "GET", "/v1/pending-connections/"+id, "", nil, 404, nil)
	var other app.ChannelLink
	e.do(nil, "POST", "/v1/channel-links", e.admin, map[string]any{}, 201, &other)
	e.do(nil, "GET", "/v1/connect-links/"+tokenOf(other)+"/pending/"+id, "", nil, 404, nil)

	var pc app.PendingConnection
	e.do(nil, "GET", "/v1/connect-links/"+token+"/pending/"+id, "", nil, 200, &pc)
	if len(pc.Accounts) != 2 {
		t.Fatalf("pending: %+v", pc)
	}
	var out struct {
		Data []struct{ Name, Handle, Provider string }
	}
	e.do(nil, "POST", "/v1/connect-links/"+token+"/pending/"+id, "", map[string]any{"account_ids": []string{"p1", "p2"}}, 201, &out)
	if len(out.Data) != 2 || out.Data[1].Handle != "@pagep2" {
		t.Fatalf("finished: %+v", out)
	}
	e.do(nil, "GET", "/v1/channel-links", e.admin, nil, 200, &links)
	for _, l := range links.Data {
		if l.ID == link.ID && (l.Uses != 2 || l.Status != "used") {
			t.Fatalf("uses after picking: %+v", l)
		}
	}
}

// tokenOf is a connect link's token, from its URL's fragment.
func tokenOf(l app.ChannelLink) string { return l.URL[strings.Index(l.URL, "#")+1:] }
