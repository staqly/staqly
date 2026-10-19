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
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeToken is a token platform: one credential field, a 20-character limit, a publish that can be made to fail.
type fakeToken struct {
	providers.Base
	fail     atomic.Bool
	publishd atomic.Int32
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
		byID["faketoken"].Limits.MaxChars != 20 || byID["bluesky"].Status != "soon" || !strings.HasPrefix(byID["faketoken"].DocsURL, "https://docs.staqly.ai/channels/") {
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
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "bluesky", "fields": map[string]string{"token": "x"}}, 409, nil) // soon
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

	// A running job whose lease ran out (a crashed worker) is picked up again.
	later := e.post(c.ID, map[string]any{"content": "later", "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+later.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)
	if _, err := e.svc.DB.Exec(`UPDATE jobs SET status = 'running', run_at = '2000-01-01T00:00:00Z', lease_until = '2000-01-01T00:00:00Z' WHERE key = ?`, "publish_post:"+later.ID); err != nil {
		t.Fatal(err)
	}
	e.svc.RunDueJobsOnce(ctx)
	e.svc.RunDueJobsOnce(ctx) // the publish_target job it queued
	e.do(nil, "GET", "/v1/posts/"+later.ID, e.key, nil, 200, &post)
	if post.Status != "published" {
		t.Fatalf("after lease reclaim: %+v", post)
	}

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
	if types["channel.connected"] != 1 || types["channel.updated"] != 2 || types["channel.disconnected"] != 1 {
		t.Fatalf("channel events: %v", types)
	}
}

// OAuth platforms: a developer app is registered once, the start answers the platform's URL with a signed
// state bound to the browser, the callback creates the channel (or reconnects the same account) and lands
// on the dashboard's callback page.
func TestChannelsOAuthFlow(t *testing.T) {
	e := newEnv(t)
	csrf := []string{"X-CSRF-Token", e.csrf}
	noRedirect := &http.Client{Jar: e.browser.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// Without an app the start is refused; a token platform does not start OAuth at all.
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
