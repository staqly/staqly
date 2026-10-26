package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeToken is a token platform: one credential field, a 20-character limit, a publish that can be made to fail.
type fakeToken struct {
	providers.Base
	fail     atomic.Bool
	publishd atomic.Int32
	lastText atomic.Value // the text of the last post it published
}

func (f *fakeToken) CredentialFields() []providers.Field {
	return []providers.Field{{Key: "token", Label: "Bot token", Kind: "secret", Required: true, Placeholder: "123:abc"}}
}

func (f *fakeToken) Connect(_ context.Context, fields map[string]string) (providers.Credentials, providers.Profile, error) {
	if fields["token"] != "good" {
		return nil, providers.Profile{}, errors.New("the platform refused the token")
	}
	return providers.Credentials{"token": fields["token"]}, providers.Profile{Handle: "@fake", Name: "Fake Account", AvatarURL: "https://img.example/a.png", ProfileURL: "https://fake.example/@fake"}, nil
}

func (f *fakeToken) Publish(_ context.Context, c providers.Credentials, p providers.PostContent, _ providers.Settings) (providers.Result, error) {
	if c["token"] != "good" {
		return providers.Result{}, errors.New("credentials missing")
	}
	if f.fail.Load() {
		return providers.Result{}, errors.New("platform down")
	}
	f.publishd.Add(1)
	f.lastText.Store(p.Text)
	return providers.Result{ExternalID: "ext_1", URL: "https://fake.example/p/1"}, nil
}

// fakeOAuth is an OAuth platform whose authorization page is at oauth.example.
type fakeOAuth struct{ providers.Base }

// The PKCE verifiers the server handed the fake platform at the start and at the exchange.
var fakeOAuthVerifiers struct {
	sync.Mutex
	start, exchange string
}

func (fakeOAuth) AuthURL(app providers.App, redirect, state string, extra map[string]string) (string, error) {
	fakeOAuthVerifiers.Lock()
	fakeOAuthVerifiers.start = extra["code_verifier"]
	fakeOAuthVerifiers.Unlock()
	return "https://oauth.example/authorize?client_id=" + app.ClientID + "&redirect_uri=" + url.QueryEscape(redirect) + "&state=" + state + "&instance=" + extra["instance"], nil
}

func (fakeOAuth) Exchange(_ context.Context, app providers.App, _, code string, extra map[string]string) (providers.Credentials, providers.Profile, error) {
	fakeOAuthVerifiers.Lock()
	fakeOAuthVerifiers.exchange = extra["code_verifier"]
	fakeOAuthVerifiers.Unlock()
	if app.ClientSecret != "s3cret" || code != "ok" {
		return nil, providers.Profile{}, errors.New("bad code")
	}
	return providers.Credentials{"access_token": "at_" + code, "expires_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)},
		providers.Profile{Handle: "@oauth", Name: "OAuth Account"}, nil
}

var fakeTok = &fakeToken{Base: providers.NewBase("faketoken", "Fake Token", providers.AuthToken, providers.Limits{MaxChars: 20, MaxMedia: 1, MediaKinds: []string{"image"}})}

func init() {
	providers.Register(fakeTok, providers.StatusAvailable)
	providers.Register(fakeOAuth{providers.NewBase("fakeoauth", "Fake OAuth", providers.AuthOAuth2, providers.Limits{MaxChars: 500})}, providers.StatusAvailable)
}

// Token platforms: the catalog lists the fields, connecting checks them, credentials stay sealed and hidden,
// publishing goes through the queue with retries, a disabled channel fails its targets without trying.
func TestChannelsTokenProvider(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Social")

	var providersOut struct{ Data []app.ProviderInfo }
	e.do(nil, "GET", "/v1/providers", e.key, nil, 200, &providersOut)
	byID := map[string]app.ProviderInfo{}
	for _, p := range providersOut.Data {
		byID[p.ID] = p
	}
	if byID["custom"].Status != "available" || byID["custom"].Auth != "none" || byID["faketoken"].Auth != "token" ||
		len(byID["faketoken"].CredentialFields) != 1 || byID["faketoken"].CredentialFields[0].Placeholder != "123:abc" ||
		byID["faketoken"].Limits.MaxChars != 20 || byID["farcaster"].Status != "soon" || !strings.HasPrefix(byID["faketoken"].DocsURL, "https://docs.staqly.ai/channels/") {
		t.Fatalf("providers: %+v", byID)
	}

	// Scopes: channels:manage connects; the default key only reads.
	e.do(nil, "POST", "/v1/channels", e.key, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 403, nil)
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken"}, 422, &errBody)
	if errBody.Error.Fields["fields.token"] == "" {
		t.Fatalf("missing field: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "bad"}}, 422, &errBody)
	if !strings.Contains(errBody.Error.Message+errBody.Error.Fields["fields"], "refused") {
		t.Fatalf("refused token: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "farcaster", "fields": map[string]string{"token": "x"}}, 409, nil) // soon
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "nope"}, 422, nil)
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &ch)
	if ch.Name != "Fake Account" || ch.Handle != "@fake" || ch.Status != "connected" || ch.AvatarURL == "" || ch.Provider != "faketoken" {
		t.Fatalf("connected: %+v", ch)
	}
	// Credentials are sealed at rest and never returned.
	var sealed string
	if err := e.svc.DB.QueryRow(`SELECT credentials FROM channels WHERE id = ?`, ch.ID).Scan(&sealed); err != nil || strings.Contains(sealed, "good") || sealed == "" {
		t.Fatalf("credentials at rest: %q %v", sealed, err)
	}
	var list struct{ Data []app.Channel }
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &list)
	e.do(nil, "GET", "/v1/channels/"+ch.ID, e.key, nil, 200, &ch)
	if len(list.Data) != 1 {
		t.Fatalf("list: %+v", list.Data)
	}
	var test map[string]any
	e.do(nil, "POST", "/v1/channels/"+ch.ID+"/test", e.admin, nil, 200, &test)
	if test["ok"] != true {
		t.Fatalf("test: %v", test)
	}
	// Options exist only for a remote setting of a platform that lists them (Slack's channels).
	e.do(nil, "GET", "/v1/channels/"+ch.ID+"/options/channel", e.key, nil, 404, nil)
	// Telegram through the server's bot needs a bot on the server; an unknown code is not there.
	e.do(nil, "POST", "/v1/channels/telegram/connect", e.admin, nil, 409, nil)
	e.do(nil, "GET", "/v1/channels/telegram/connect/ABC234", e.key, nil, 404, nil)

	// The provider's limit is applied per target when the post is written.
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "this text is far longer than twenty characters",
		"targets": []map[string]any{{"channel_id": ch.ID}}}, 422, &errBody)
	if errBody.Error.Fields["targets."+ch.ID+".content"] == "" {
		t.Fatalf("limit: %+v", errBody.Error)
	}
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "this text is far longer than twenty characters",
		"targets": []map[string]any{{"channel_id": ch.ID, "content": "short version"}}}, 201, &post)
	var preview app.PostPreview
	e.do(nil, "GET", "/v1/posts/"+post.ID+"/preview", e.key, nil, 200, &preview)
	if preview.Targets[0].Text != "short version" || !preview.Targets[0].OK {
		t.Fatalf("preview: %+v", preview)
	}

	// Publishing fails three times with backoff, the target and the post fail, the job can be retried.
	fakeTok.fail.Store(true)
	defer fakeTok.fail.Store(false)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 202, nil)
	base := time.Now()
	defer func() { e.svc.Now = time.Now }()
	for i, ahead := range []time.Duration{0, 2 * time.Minute, 10 * time.Minute} {
		e.svc.Now = func() time.Time { return base.Add(ahead) }
		e.svc.RunDueJobsOnce(ctx)
		e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &post)
		if i < 2 && post.Status != "publishing" {
			t.Fatalf("attempt %d: %+v", i+1, post)
		}
	}
	if post.Status != "failed" || post.Targets[0].Status != "failed" || post.Targets[0].Error != "platform down" || post.Targets[0].Attempts != 3 {
		t.Fatalf("after three attempts: %+v", post)
	}
	var jobs app.JobPage
	e.do(nil, "GET", "/v1/jobs?status=failed", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 || jobs.Data[0].Kind != "publish_target" || jobs.Data[0].Attempts != 3 || jobs.Data[0].LastError != "platform down" {
		t.Fatalf("failed job: %+v", jobs.Data)
	}
	e.do(nil, "POST", "/v1/jobs/"+jobs.Data[0].ID+"/retry", e.key, nil, 202, nil)
	e.do(nil, "POST", "/v1/jobs/"+jobs.Data[0].ID+"/retry", e.key, nil, 409, nil) // queued now
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &post)
	if post.Status != "publishing" {
		t.Fatalf("after retry: %+v", post)
	}
	fakeTok.fail.Store(false)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &post)
	if post.Status != "published" || post.Targets[0].ExternalURL != "https://fake.example/p/1" || post.Targets[0].ExternalID != "ext_1" {
		t.Fatalf("published: %+v", post)
	}

	// A crashed worker's running job is put back by River's rescuer (jobRescueAfter): River's own behaviour,
	// not tested again here.

	// Links: the channel removes them, so the limit counts the text without them, the preview names the
	// channel and the links, and the platform receives the text without them; the post keeps it as written.
	if ch.Links != "keep" {
		t.Fatalf("default links mode: %+v", ch)
	}
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.admin, map[string]any{"settings": map[string]any{"links": "nope"}}, 422, &errBody)
	if errBody.Error.Fields["settings.links"] == "" {
		t.Fatalf("bad links mode: %+v", errBody.Error)
	}
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.admin, map[string]any{"settings": map[string]any{"links": "remove"}}, 200, &ch)
	if ch.Links != "remove" {
		t.Fatalf("links mode: %+v", ch)
	}
	linked := e.post(c.ID, map[string]any{"content": "New: https://example.com/a/long/path", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "GET", "/v1/posts/"+linked.ID+"/preview", e.key, nil, 200, &preview)
	lp := preview.Targets[0]
	if lp.Text != "New:" || lp.Links != "remove" || len(lp.LinksFound) != 1 || len(lp.Warnings) != 1 ||
		lp.Warnings[0] != "Links will be removed from: Fake Account (https://example.com/a/long/path)" {
		t.Fatalf("links preview: %+v", lp)
	}
	e.do(nil, "POST", "/v1/posts/"+linked.ID+"/publish", e.key, nil, 202, nil)
	for range 3 {
		e.svc.RunDueJobsOnce(ctx)
	}
	if got, _ := fakeTok.lastText.Load().(string); got != "New:" {
		t.Fatalf("published text: %q", got)
	}
	e.do(nil, "GET", "/v1/posts/"+linked.ID, e.key, nil, 200, &linked)
	if linked.Content != "New: https://example.com/a/long/path" {
		t.Fatalf("stored text changed: %q", linked.Content)
	}
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.admin, map[string]any{"settings": map[string]any{}}, 200, &ch)

	// Cancelling a queued job.
	queued := e.post(c.ID, map[string]any{"content": "queued", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+queued.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	e.do(nil, "GET", "/v1/jobs?status=queued&kind=publish_post", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 {
		t.Fatalf("queued jobs: %+v", jobs.Data)
	}
	var job app.Job
	e.do(nil, "POST", "/v1/jobs/"+jobs.Data[0].ID+"/cancel", e.key, nil, 200, &job)
	if job.Status != "cancelled" {
		t.Fatalf("cancelled: %+v", job)
	}
	e.do(nil, "POST", "/v1/jobs/"+jobs.Data[0].ID+"/cancel", e.key, nil, 409, nil)
	e.do(nil, "POST", "/v1/jobs/job_nope/cancel", e.key, nil, 404, nil)

	// Rename, turn off (existing targets fail without trying, new ones are refused), reconnect with fresh
	// fields, remove.
	off := e.post(c.ID, map[string]any{"content": "while off", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.key, map[string]any{"name": "Main"}, 403, nil)
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.admin, map[string]any{"name": "Main", "status": "disabled"}, 200, &ch)
	if ch.Name != "Main" || ch.Status != "disabled" {
		t.Fatalf("patched: %+v", ch)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "new", "targets": []map[string]any{{"channel_id": ch.ID}}}, 422, nil)
	e.do(nil, "POST", "/v1/posts/"+off.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+off.ID, e.key, nil, 200, &post)
	if post.Status != "failed" || !strings.Contains(post.Targets[0].Error, "turned off") {
		t.Fatalf("disabled channel: %+v", post)
	}
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.admin, map[string]any{"fields": map[string]string{"token": "bad"}}, 422, nil)
	e.do(nil, "PATCH", "/v1/channels/"+ch.ID, e.admin, map[string]any{"status": "connected", "fields": map[string]string{"token": "good"}}, 200, &ch)
	if ch.Status != "connected" {
		t.Fatalf("reconnected: %+v", ch)
	}
	e.do(nil, "DELETE", "/v1/channels/"+ch.ID, e.key, nil, 403, nil)
	e.do(nil, "DELETE", "/v1/channels/"+ch.ID, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/channels/"+ch.ID, e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/posts/"+off.ID, e.key, nil, 200, &post)
	if len(post.Targets) != 0 {
		t.Fatalf("targets after the channel went: %+v", post.Targets)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/events?type_prefix=channel.", e.key, nil, 200, &evs)
	types := map[string]int{}
	for _, ev := range evs.Data {
		types[ev.Type]++
	}
	if types["channel.connected"] != 1 || types["channel.updated"] != 4 || // rename + off, links on and off
		types["channel.disconnected"] != 1 {
		t.Fatalf("channel events: %v", types)
	} // Several types at once (the dashboard's notifications): only those.
	e.do(nil, "GET", "/v1/events?types=channel.connected&types=channel.disconnected", e.key, nil, 200, &evs)
	if len(evs.Data) != 2 || evs.Data[0].Type != "channel.disconnected" || evs.Data[1].Type != "channel.connected" {
		t.Fatalf("types filter: %+v", evs.Data)
	}
}

// OAuth platforms: a developer app is registered once, the start answers the platform's URL with a signed
// state bound to the browser, the callback creates the channel (or reconnects the same account) and lands
// on the dashboard's callback page.
func TestChannelsOAuthFlow(t *testing.T) {
	e := newEnv(t)
	csrf := []string{"X-CSRF-Token", e.csrf}
	noRedirect := &http.Client{Jar: e.browser.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// Without an app the platform is listed as soon and the start is refused; a token platform does not start
	// OAuth at all.
	var before struct{ Data []app.ProviderInfo }
	e.do(nil, "GET", "/v1/providers", e.key, nil, 200, &before)
	for _, p := range before.Data {
		if p.ID == "fakeoauth" && (p.Status != "soon" || p.AppConfigured) {
			t.Fatalf("oauth platform without an app: %+v", p)
		}
	}
	e.do(e.browser, "GET", "/v1/channels/oauth/fakeoauth/start", "", nil, 409, nil)
	e.do(e.browser, "GET", "/v1/channels/oauth/faketoken/start", "", nil, 422, nil)
	e.do(e.browser, "GET", "/v1/channels/oauth/nope/start", "", nil, 422, nil)
	e.do(nil, "PUT", "/v1/providers/fakeoauth/app", e.key, map[string]any{"client_id": "cid", "client_secret": "s3cret"}, 403, nil)
	e.do(e.browser, "PUT", "/v1/providers/fakeoauth/app", "", map[string]any{"client_id": "cid"}, 422, nil, csrf...)
	e.do(e.browser, "PUT", "/v1/providers/fakeoauth/app", "", map[string]any{"client_id": "cid", "client_secret": "s3cret"}, 204, nil, csrf...)
	var providersOut struct{ Data []app.ProviderInfo }
	e.do(nil, "GET", "/v1/providers", e.key, nil, 200, &providersOut)
	for _, p := range providersOut.Data {
		if p.ID == "fakeoauth" && !p.AppConfigured {
			t.Fatalf("app not reported: %+v", p)
		}
	}
	var sealed string
	if err := e.svc.DB.QueryRow(`SELECT credentials FROM provider_apps WHERE provider = 'fakeoauth'`).Scan(&sealed); err != nil || strings.Contains(sealed, "s3cret") || sealed == "" {
		t.Fatalf("client secret at rest: %q %v", sealed, err)
	}

	for _, p := range providersOut.Data {
		if p.ID == "fakeoauth" && p.Status != "available" {
			t.Fatalf("oauth platform with an app: %+v", p)
		}
	}

	var start struct{ URL string }
	e.do(e.browser, "GET", "/v1/channels/oauth/fakeoauth/start?instance=mastodon.example", "", nil, 200, &start)
	u, err := url.Parse(start.URL)
	if err != nil || u.Host != "oauth.example" || u.Query().Get("client_id") != "cid" || u.Query().Get("instance") != "mastodon.example" ||
		!strings.HasSuffix(u.Query().Get("redirect_uri"), "/channels/callback/fakeoauth") {
		t.Fatalf("start url: %s", start.URL)
	}
	state := u.Query().Get("state")

	// The platform refused; a state from another browser; a bad code: all land with an error.
	for _, q := range []string{"error=access_denied&error_description=Nope", "code=ok&state=forged", "code=bad&state=" + state} {
		resp := e.do(noRedirect, "GET", "/v1/channels/oauth/fakeoauth/callback?"+q, "", nil, 302, nil)
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if !strings.HasSuffix(loc.Path, "/channels/callback/fakeoauth") || loc.Query().Get("error") == "" || loc.Query().Get("channel") != "" {
			t.Fatalf("%s: landed on %s", q, resp.Header.Get("Location"))
		}
	}
	if _, err := e.svc.DB.Exec(`SELECT 1`); err != nil {
		t.Fatal(err)
	}
	// The state cookie was cleared by the failed attempts: start again, then succeed.
	e.do(e.browser, "GET", "/v1/channels/oauth/fakeoauth/start", "", nil, 200, &start)
	u, _ = url.Parse(start.URL)
	state = u.Query().Get("state")
	// The server derives the PKCE verifier from the signed state and hands the same one to both steps.
	resp := e.do(noRedirect, "GET", "/v1/channels/oauth/fakeoauth/callback?code=ok&state="+state, "", nil, 302, nil)
	fakeOAuthVerifiers.Lock()
	vStart, vExchange := fakeOAuthVerifiers.start, fakeOAuthVerifiers.exchange
	fakeOAuthVerifiers.Unlock()
	if len(vStart) != 43 || vExchange != vStart {
		t.Fatalf("PKCE verifier: start %q, exchange %q", vStart, vExchange)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	chID := loc.Query().Get("channel")
	if chID == "" || loc.Query().Get("error") != "" {
		t.Fatalf("landed on %s", resp.Header.Get("Location"))
	}
	var ch app.Channel
	e.do(nil, "GET", "/v1/channels/"+chID, e.key, nil, 200, &ch)
	if ch.Provider != "fakeoauth" || ch.Handle != "@oauth" || ch.Status != "connected" {
		t.Fatalf("oauth channel: %+v", ch)
	}
	// The token expires in 48 h: a refresh is queued a day before.
	var jobs app.JobPage
	e.do(nil, "GET", "/v1/jobs?kind=refresh_channel", e.key, nil, 200, &jobs)
	if len(jobs.Data) != 1 || jobs.Data[0].Payload.ChannelID != chID {
		t.Fatalf("refresh job: %+v", jobs.Data)
	}
	// Reconnecting through fields is for token platforms only; the same account through OAuth reconnects.
	e.do(e.browser, "PATCH", "/v1/channels/"+chID, "", map[string]any{"fields": map[string]string{"token": "x"}}, 422, nil, csrf...)
	e.do(e.browser, "GET", "/v1/channels/oauth/fakeoauth/start", "", nil, 200, &start)
	u, _ = url.Parse(start.URL)
	resp = e.do(noRedirect, "GET", "/v1/channels/oauth/fakeoauth/callback?code=ok&state="+u.Query().Get("state"), "", nil, 302, nil)
	loc, _ = url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("channel") != chID {
		t.Fatalf("reconnect made a new channel: %s", resp.Header.Get("Location"))
	}
	var list struct{ Data []app.Channel }
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &list)
	if len(list.Data) != 1 {
		t.Fatalf("channels after reconnect: %+v", list.Data)
	}
	var test map[string]any
	e.do(e.browser, "POST", "/v1/channels/"+chID+"/test", "", nil, 200, &test, csrf...)
	if test["ok"] != true {
		t.Fatalf("test: %v", test)
	}
	// Another workspace cannot see the app, the channel or the state.
	other := newWorkspace(e, "Other")
	e.do(nil, "GET", "/v1/channels/"+chID, other.key, nil, 404, nil)
	e.do(other.browser, "GET", "/v1/channels/oauth/fakeoauth/start", "", nil, 409, nil)

	e.do(e.browser, "DELETE", "/v1/providers/fakeoauth/app", "", nil, 204, nil, csrf...)
	e.do(e.browser, "DELETE", "/v1/providers/fakeoauth/app", "", nil, 404, nil, csrf...)
	e.do(e.browser, "GET", "/v1/channels/oauth/fakeoauth/start", "", nil, 409, nil)
}

// PROVIDER_<ID>_ENABLED=false takes a platform off the catalog and refuses new posts to it, while what was
// already scheduled there still goes out (and can still be edited).
func TestProviderDisabled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Social")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &ch)
	var post app.Post
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "scheduled",
		"targets": []map[string]any{{"channel_id": ch.ID}}}, 201, &post)

	disabled := func(k string) string {
		if k == "PROVIDER_FAKETOKEN_ENABLED" {
			return "false"
		}
		return ""
	}
	prev := e.svc.Getenv
	e.svc.Getenv = func(k string) string {
		if v := disabled(k); v != "" {
			return v
		}
		if prev != nil {
			return prev(k)
		}
		return ""
	}
	var out struct{ Data []app.ProviderInfo }
	e.do(nil, "GET", "/v1/providers", e.key, nil, 200, &out)
	for _, p := range out.Data {
		if p.ID == "faketoken" {
			t.Fatalf("disabled provider listed: %+v", p)
		}
	}
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 409, &errBody)
	if errBody.Error.Code != "provider_disabled" {
		t.Fatalf("connect a disabled provider: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": c.ID, "content": "new",
		"targets": []map[string]any{{"channel_id": ch.ID}}}, 409, &errBody)
	if errBody.Error.Code != "provider_disabled" {
		t.Fatalf("new post to a disabled provider: %+v", errBody.Error)
	}

	// The post that already had the channel: edited, published, done.
	e.do(nil, "PATCH", "/v1/posts/"+post.ID, e.key, map[string]any{"content": "scheduled, edited"}, 200, nil)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.do(nil, "GET", "/v1/posts/"+post.ID, e.key, nil, 200, &post)
	if post.Status != "published" {
		t.Fatalf("already scheduled post: %+v", post)
	}
}

// Activity: the last published post and time, the next scheduled one and the 30-day count come with list and
// get, read from the targets, and only for the workspace's own channels.
func TestChannelActivity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.collection("Social")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "faketoken", "fields": map[string]string{"token": "good"}}, 201, &ch)
	var list struct{ Data []app.Channel }

	// Nothing went out yet: an empty activity, not a missing one.
	e.do(nil, "GET", "/v1/channels/"+ch.ID, e.key, nil, 200, &ch)
	if ch.Activity == nil || ch.Activity.LastPublishedAt != nil || ch.Activity.NextPublishAt != nil || ch.Activity.Published30d != 0 {
		t.Fatalf("empty activity: %+v", ch.Activity)
	}

	sent := e.post(c.ID, map[string]any{"content": "out", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+sent.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	at := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	later := e.post(c.ID, map[string]any{"content": "later", "targets": []map[string]any{{"channel_id": ch.ID}},
		"publish_at": at, "status": "scheduled"})

	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &list)
	var got *app.Channel
	for i := range list.Data {
		if list.Data[i].ID == ch.ID {
			got = &list.Data[i]
		}
	}
	if got == nil || got.Activity == nil || got.Activity.LastPostID == nil || *got.Activity.LastPostID != sent.ID ||
		got.Activity.LastPublishedAt == nil || got.Activity.Published30d != 1 || got.Activity.NextPublishAt == nil {
		t.Fatalf("activity after publishing: %+v", got)
	}
	var laterOut app.Post
	e.do(nil, "GET", "/v1/posts/"+later.ID, e.key, nil, 200, &laterOut)
	if want := laterOut.PublishAt; want == nil || *got.Activity.NextPublishAt != *want {
		t.Fatalf("next publish: %v, want %v", *got.Activity.NextPublishAt, want)
	}

	// Another workspace sees none of it.
	q := db.New(e.svc.DB)
	if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_other", Name: "Other", RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	stranger := app.Principal{WorkspaceID: "ws_other", Kind: app.PrincipalAPIKey, ID: "key_other", Name: "other", Scopes: app.DefaultKeyScopes}
	if theirs, err := e.svc.ListChannels(ctx, stranger, app.ChannelFilter{}); err != nil || len(theirs) != 0 {
		t.Fatalf("cross-workspace channels: %v %v", theirs, err)
	}
	if _, err := e.svc.GetChannel(ctx, stranger, ch.ID); err == nil {
		t.Fatal("cross-workspace channel read")
	}
}

// Channel groups: a Default group always exists and holds new channels; groups are created, renamed and deleted
// (their channels go back to Default); Default can't be deleted; names are unique; ?group= filters; workspaces
// don't see each other's groups.
func TestChannelGroups(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var groups struct{ Data []app.ChannelGroup }
	e.do(nil, "GET", "/v1/channel-groups", e.key, nil, 200, &groups)
	if len(groups.Data) != 1 || !groups.Data[0].Default || groups.Data[0].Name != "Default" {
		t.Fatalf("default group: %+v", groups.Data)
	}
	def := groups.Data[0]
	a := e.customChannel("Alpha")
	if a.GroupID != def.ID {
		t.Fatalf("new channel group: %q, want %q", a.GroupID, def.ID)
	}

	e.do(nil, "POST", "/v1/channel-groups", e.key, map[string]any{"name": "Client A"}, 403, nil) // channels:manage
	var client app.ChannelGroup
	e.do(nil, "POST", "/v1/channel-groups", e.admin, map[string]any{"name": " Client A ", "color": "#22c55e"}, 201, &client)
	if client.Name != "Client A" || client.Default || client.Color != "#22c55e" {
		t.Fatalf("created group: %+v", client)
	}
	e.do(nil, "POST", "/v1/channel-groups", e.admin, map[string]any{"name": "client a"}, 409, nil)
	e.do(nil, "POST", "/v1/channel-groups", e.admin, map[string]any{"name": "Bad", "color": "green"}, 422, nil)
	e.do(nil, "POST", "/v1/channel-groups", e.admin, map[string]any{}, 422, nil)

	// A channel connected straight into a group, another moved there, one back to Default.
	var b app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": "Beta", "group_id": client.ID}, 201, &b)
	if b.GroupID != client.ID {
		t.Fatalf("connect into group: %+v", b)
	}
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": "X", "group_id": "grp_nope"}, 422, nil)
	e.do(nil, "PATCH", "/v1/channels/"+a.ID, e.admin, map[string]any{"group_id": client.ID}, 200, &a)
	if a.GroupID != client.ID {
		t.Fatalf("moved: %+v", a)
	}
	var list struct{ Data []app.Channel }
	e.do(nil, "GET", "/v1/channels?group="+client.ID, e.key, nil, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("group filter: %+v", list.Data)
	}
	e.do(nil, "GET", "/v1/channel-groups", e.key, nil, 200, &groups)
	if len(groups.Data) != 2 || groups.Data[0].ID != def.ID || groups.Data[0].Channels != 0 || groups.Data[1].Channels != 2 {
		t.Fatalf("counts: %+v", groups.Data)
	}

	e.do(nil, "PATCH", "/v1/channel-groups/"+client.ID, e.admin, map[string]any{"name": "Client B"}, 200, &client)
	if client.Name != "Client B" {
		t.Fatalf("renamed: %+v", client)
	}
	e.do(nil, "PATCH", "/v1/channel-groups/"+def.ID, e.admin, map[string]any{"name": "Client B"}, 409, nil)

	// Default stays; deleting another group moves its channels to Default.
	var errBody struct{ Error app.Error }
	e.do(nil, "DELETE", "/v1/channel-groups/"+def.ID, e.admin, nil, 409, &errBody)
	if errBody.Error.Code != "default_group" {
		t.Fatalf("delete default: %+v", errBody.Error)
	}
	e.do(nil, "DELETE", "/v1/channel-groups/"+client.ID, e.admin, nil, 204, nil)
	e.do(nil, "DELETE", "/v1/channel-groups/"+client.ID, e.admin, nil, 404, nil)
	e.do(nil, "GET", "/v1/channels", e.key, nil, 200, &list)
	for _, c := range list.Data {
		if c.GroupID != def.ID {
			t.Fatalf("after delete, %s is in %q", c.Name, c.GroupID)
		}
	}

	// Another workspace has its own Default and can't touch these.
	q := db.New(e.svc.DB)
	if err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_other", Name: "Other", RecoveryVerifier: "x", RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	stranger := app.Principal{WorkspaceID: "ws_other", Kind: app.PrincipalAPIKey, ID: "key_other", Name: "other", Scopes: app.AdminKeyScopes}
	theirs, err := e.svc.ListChannelGroups(ctx, stranger)
	if err != nil || len(theirs) != 1 || theirs[0].ID == def.ID {
		t.Fatalf("other workspace groups: %+v %v", theirs, err)
	}
	mine := "Mine"
	if _, err := e.svc.UpdateChannelGroup(ctx, stranger, def.ID, app.ChannelGroupInput{Name: &mine}); err == nil {
		t.Fatal("cross-workspace group rename")
	}
	if _, err := e.svc.UpdateChannel(ctx, stranger, a.ID, app.ChannelPatch{GroupID: &theirs[0].ID}); err == nil {
		t.Fatal("cross-workspace channel move")
	}
	// Our channel can't be moved into their group either.
	e.do(nil, "PATCH", "/v1/channels/"+a.ID, e.admin, map[string]any{"group_id": theirs[0].ID}, 422, nil)
}
