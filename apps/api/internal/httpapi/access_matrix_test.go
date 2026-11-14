package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// The access matrix: who may call what, declared once (accessRules) and checked against the real router for
// every route × role × API key × nobody. A route without a rule fails the test, so does a rule without a route.

// rule: who a route lets in. The zero value is "anyone signed in" (every role, every key).
type rule struct {
	public   bool     // no credentials: the sign-in gate never answers
	scopes   []string // every one is needed (admin covers all); a role has them through wantRoleScopes
	keyAdmin bool     // an API key needs the admin scope whatever scopes say (keys are managed with a session or an admin key)
	minRole  string   // on top of the scopes: the lowest role, for a person and for the person who made the key
	people   bool     // a signed-in person only: every API key is refused
	keysOnly bool     // API keys only: a session cookie is not read (/mcp)
	upload   bool     // the body is multipart/form-data with one small file
	body     string   // JSON body instead of {}
	// ownSession: the call ends or moves the caller's session, so each role makes it from a second sign-in.
	ownSession bool
}

func anyone() rule                 { return rule{} }
func public() rule                 { return rule{public: true} }
func scope(s ...string) rule       { return rule{scopes: s} }
func byPerson(minRole string) rule { return rule{people: true, minRole: minRole} }
func (r rule) owner() rule         { r.minRole = app.RoleOwner; return r }
func (r rule) adminKey() rule      { r.keyAdmin = true; return r }
func (r rule) file() rule          { r.upload = true; return r }
func (r rule) json(b string) rule  { r.body = b; return r }
func (r rule) own() rule           { r.ownSession = true; return r }

const (
	colR, colW, colD         = app.ScopeCollectionsRead, app.ScopeCollectionsWrite, app.ScopeCollectionsDelete
	postR, postW, postD, pub = app.ScopePostsRead, app.ScopePostsWrite, app.ScopePostsDelete, app.ScopePostsPublish
	mediaR, mediaW           = app.ScopeMediaRead, app.ScopeMediaWrite
	chR, chM                 = app.ScopeChannelsRead, app.ScopeChannelsManage
	sched, hooks, alerts     = app.ScopeSchedulesManage, app.ScopeWebhooksManage, app.ScopeAlertsManage
	events, integ, admin     = app.ScopeEventsRead, app.ScopeIntegrationsManage, app.ScopeAdmin
)

// wantRoleScopes pins the permission model (app.RoleScopes): changing what a role may do is a decision, made here too.
var wantRoleScopes = map[string][]string{
	app.RoleViewer:   {colR, postR, mediaR, events, chR},
	app.RoleEditor:   {colR, colW, postR, postW, postD, pub, mediaR, mediaW, events, chR, sched},
	app.RoleOperator: {colR, colW, colD, postR, postW, postD, pub, mediaR, mediaW, events, chR, chM, sched, hooks, alerts, integ},
	app.RoleAdmin:    {admin},
	app.RoleOwner:    {admin},
}

var accessRules = map[string]rule{
	// Nobody signed in: status, sign-in, links that carry their own token, callbacks that check their own signature.
	"GET /health":                                          public(),
	"GET /openapi.yaml":                                    public(),
	"GET /v1/setup":                                        public(),
	"GET /v1/plans":                                        public(),
	"POST /v1/workspaces":                                  public(),
	"POST /v1/sessions/recover":                            public(),
	"POST /v1/sessions/api-key":                            public(),
	"POST /v1/device-links/redeem":                         public(),
	"GET /v1/auth/google/start":                            public(),
	"GET /v1/auth/google/callback":                         public(),
	"POST /v1/auth/email/start":                            public(),
	"POST /v1/auth/email/verify":                           public(),
	"GET /v1/auth/email/dev-code":                          public(),
	"POST /v1/auth/passkey/start":                          public(),
	"POST /v1/auth/passkey/finish":                         public(),
	"GET /v1/invitations/lookup":                           public(),
	"POST /v1/notifications/unsubscribe":                   public(),
	"GET /v1/channels/oauth/{provider}/callback":           public(),
	"GET /v1/oauth/{provider}/client-metadata.json":        public(),
	"GET /v1/connect-links/oauth/{provider}/callback":      public(),
	"GET /v1/connect-links/{token}":                        public(),
	"POST /v1/connect-links/{token}/channels":              public(),
	"GET /v1/connect-links/{token}/oauth/{provider}/start": public(),
	"GET /v1/connect-links/{token}/pending/{id}":           public(),
	"POST /v1/connect-links/{token}/pending/{id}":          public(),
	"GET /v1/previews/{token}":                             public(),
	"POST /v1/previews/{token}/comments":                   public(),
	"POST /v1/previews/{token}/comments/{comment}/resolve": public(),
	"POST /v1/previews/{token}/comments/{comment}/reopen":  public(),
	"GET /v1/previews/{token}/media/{media}":               public(),
	"GET /v1/media-links/{workspace}/{media}/{signature}":  public(),
	"POST /v1/billing/webhook":                             public(),
	"GET /v1/meta/webhook":                                 public(),
	"POST /v1/meta/webhook":                                public(),
	"POST /v1/meta/deauthorize":                            public(),
	"POST /v1/meta/data-deletion":                          public(),
	"GET /v1/meta/data-deletion/{code}":                    public(),
	"POST /v1/tiktok/webhook":                              public(),
	"POST /v1/kick/webhook":                                public(),
	"POST /v1/whop/webhook/{channel}":                      public(),

	"POST /mcp": {keysOnly: true},

	// Anyone signed in.
	"GET /v1/session":                 anyone(),
	"DELETE /v1/session":              anyone().own(),
	"GET /v1/workspace":               anyone(),
	"GET /v1/workspace/usage":         anyone(),
	"GET /v1/workspaces":              anyone(),
	"GET /v1/access-token":            anyone(), // local mode only: 404 here
	"POST /v1/access-token/rotate":    anyone(),
	"POST /v1/feedback":               byPerson(app.RoleViewer).file(),
	"POST /v1/assistant/chat":         scope(postR),
	"GET /v1/assistant/chats":         anyone(),
	"GET /v1/assistant/chats/{id}":    anyone(),
	"DELETE /v1/assistant/chats/{id}": anyone(),

	// A signed-in person, whatever their role: their own account, sign-in methods and devices.
	"PATCH /v1/account":                                            byPerson(app.RoleViewer),
	"POST /v1/account/workspaces":                                  byPerson(app.RoleViewer),
	"GET /v1/account/notifications":                                byPerson(app.RoleViewer),
	"PUT /v1/account/notifications":                                byPerson(app.RoleViewer),
	"GET /v1/account/sign-in-methods":                              byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/email/start":                 byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/email/verify":                byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/email/change/current/start":  byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/email/change/current/verify": byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/email/change/new/start":      byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/email/change/new/verify":     byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/passkey/start":               byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/passkey/finish":              byPerson(app.RoleViewer),
	"PATCH /v1/account/sign-in-methods/passkey/{id}":               byPerson(app.RoleViewer),
	"POST /v1/account/sign-in-methods/google":                      byPerson(app.RoleViewer),
	"DELETE /v1/account/sign-in-methods/{provider}/{subject}":      byPerson(app.RoleViewer),
	"POST /v1/session/onboarded":                                   byPerson(app.RoleViewer),
	"POST /v1/session/workspace":                                   byPerson(app.RoleViewer),
	"POST /v1/device-links":                                        byPerson(app.RoleViewer),
	"POST /v1/invitations/accept":                                  byPerson(app.RoleViewer),
	"POST /v1/recovery-key/rotate":                                 byPerson(app.RoleViewer), // 409 with accounts: recovery keys are from before them

	// People are managed by people: admins and owners, never a key.
	"GET /v1/invitations":              byPerson(app.RoleAdmin),
	"POST /v1/invitations":             byPerson(app.RoleAdmin),
	"POST /v1/invitations/{id}/resend": byPerson(app.RoleAdmin),
	"DELETE /v1/invitations/{id}":      byPerson(app.RoleAdmin),
	"PATCH /v1/members/{user_id}":      byPerson(app.RoleAdmin),
	"DELETE /v1/members/{user_id}":     byPerson(app.RoleAdmin), // someone else; leaving is TestRoleRules
	"GET /v1/members":                  scope(admin),

	// Workspace administration.
	"PATCH /v1/workspace":                     scope(admin),
	"GET /v1/sessions":                        scope(admin),
	"DELETE /v1/sessions/{id}":                scope(admin),
	"POST /v1/workspace/export":               scope(admin),
	"GET /v1/workspace/exports":               scope(admin),
	"GET /v1/workspace/exports/{id}":          scope(admin),
	"GET /v1/workspace/exports/{id}/download": scope(admin),
	"POST /v1/posts/{id}/purge":               scope(admin),
	"DELETE /v1/workspace":                    byPerson(app.RoleOwner),

	// Billing: admins read it, owners pay, and paying is a person's (a key never spends the card on file).
	"GET /v1/billing":                   scope(admin),
	"GET /v1/billing/details":           scope(admin),
	"GET /v1/billing/addons":            scope(admin),
	"POST /v1/billing/sync":             scope(admin),
	"GET /v1/billing/pending-payment":   scope(admin).owner(),
	"POST /v1/billing/promotion-code":   scope(admin).owner(),
	"POST /v1/billing/checkout":         byPerson(app.RoleOwner),
	"POST /v1/billing/portal":           byPerson(app.RoleOwner),
	"POST /v1/billing/start-paid-plan":  byPerson(app.RoleOwner),
	"POST /v1/billing/credits/checkout": byPerson(app.RoleOwner),
	"POST /v1/billing/addons/{id}":      byPerson(app.RoleOwner),
	"DELETE /v1/billing/addons/{id}":    byPerson(app.RoleOwner),
	"GET /v1/billing/x-posts":           scope(postR),

	// Keys and agents: operators manage their own in the app; a key needs admin.
	"GET /v1/api-keys":              scope(integ).adminKey(),
	"POST /v1/api-keys":             scope(integ).adminKey(),
	"DELETE /v1/api-keys/{id}":      scope(integ).adminKey(),
	"POST /v1/api-keys/{id}/rotate": scope(integ).adminKey(),
	"GET /v1/agents":                scope(integ).adminKey(),
	"DELETE /v1/agents/{id}":        scope(integ).adminKey(),

	// Collections.
	"GET /v1/collections":            scope(colR),
	"GET /v1/collections/{id}":       scope(colR),
	"POST /v1/collections":           scope(colW),
	"PATCH /v1/collections/{id}":     scope(colW),
	"DELETE /v1/collections/{id}":    scope(colD),
	"GET /v1/collections/{id}/posts": scope(postR),

	// Posts: reading, writing, deleting and publishing are four scopes.
	"GET /v1/posts":                                  scope(postR),
	"GET /v1/posts/{id}":                             scope(postR),
	"GET /v1/posts/{id}/preview":                     scope(postR),
	"GET /v1/posts/{id}/previews":                    scope(postR),
	"GET /v1/posts/{id}/comments":                    scope(postR),
	"GET /v1/posts/{id}/metrics":                     scope(postR),
	"GET /v1/posts/{id}/responses":                   scope(postR),
	"GET /v1/tags":                                   scope(postR),
	"GET /v1/work":                                   scope(postR),
	"GET /v1/analytics":                              scope(postR),
	"GET /v1/posts/{id}/events":                      scope(events),
	"POST /v1/posts":                                 scope(postW),
	"PATCH /v1/posts/{id}":                           scope(postW),
	"POST /v1/posts/{id}/archive":                    scope(postW),
	"POST /v1/posts/{id}/unarchive":                  scope(postW),
	"POST /v1/posts/{id}/duplicate":                  scope(postW),
	"POST /v1/posts/{id}/previews":                   scope(postW),
	"DELETE /v1/posts/{id}/previews/{preview}":       scope(postW),
	"POST /v1/posts/{id}/comments":                   scope(postW),
	"POST /v1/posts/{id}/comments/{comment}/resolve": scope(postW),
	"POST /v1/posts/{id}/comments/{comment}/reopen":  scope(postW),
	"DELETE /v1/posts/{id}/comments/{comment}":       scope(postW),
	"POST /v1/posts/{id}/responses/refresh":          scope(postW, postR),
	"POST /v1/ai/post":                               scope(postW),
	"DELETE /v1/posts/{id}":                          scope(postD),
	"POST /v1/posts/delete":                          scope(postD),
	"POST /v1/posts/{id}/schedule":                   scope(pub),
	"POST /v1/posts/{id}/publish":                    scope(pub),
	"POST /v1/posts/{id}/cancel":                     scope(pub),
	"POST /v1/posts/{id}/unpublish":                  scope(pub),
	"POST /v1/posts/{id}/targets/{channel}/result":   scope(pub),
	"POST /v1/posts/{id}/targets/{channel}/retry":    scope(pub),
	"PUT /v1/posts/{id}/targets/{channel}/metrics":   scope(pub),

	// The queue.
	"GET /v1/jobs":              scope(postR),
	"GET /v1/jobs/stats":        scope(postR),
	"GET /v1/jobs/{id}":         scope(postR),
	"POST /v1/jobs/{id}/retry":  scope(pub),
	"POST /v1/jobs/{id}/cancel": scope(pub),

	// Schedules.
	"GET /v1/schedules":           scope(postR),
	"GET /v1/schedules/{id}":      scope(postR),
	"POST /v1/schedules":          scope(sched),
	"PATCH /v1/schedules/{id}":    scope(sched),
	"DELETE /v1/schedules/{id}":   scope(sched),
	"POST /v1/schedules/{id}/run": scope(sched),

	// Media: deleting is posts:delete, never media:write.
	"GET /v1/media":                    scope(mediaR),
	"GET /v1/media/{id}":               scope(mediaR),
	"GET /v1/media/{id}/info":          scope(mediaR),
	"GET /v1/media/{id}/usage":         scope(mediaR),
	"GET /v1/media-folders":            scope(mediaR),
	"GET /v1/media-projects":           scope(mediaR),
	"GET /v1/media-projects/{id}":      scope(mediaR),
	"GET /v1/media-projects/{id}/file": scope(mediaR),
	"GET /v1/design/brand":             scope(mediaR),
	"GET /v1/stock/photos":             scope(mediaR),
	"GET /v1/ai":                       scope(mediaR),
	"GET /v1/ai/jobs/{id}":             scope(mediaR),
	"GET /v1/ai/jobs/{id}/result":      scope(mediaR),
	"POST /v1/media":                   scope(mediaW).file(),
	"PATCH /v1/media/{id}":             scope(mediaW),
	"POST /v1/media/bulk":              scope(mediaW),
	"POST /v1/media/{id}/replace":      scope(mediaW, postW, postD).file(),
	"POST /v1/media-folders":           scope(mediaW),
	"PATCH /v1/media-folders/{id}":     scope(mediaW),
	"DELETE /v1/media-folders/{id}":    scope(mediaW),
	"POST /v1/media-projects":          scope(mediaW).file(),
	"PUT /v1/media-projects/{id}":      scope(mediaW).file(),
	"PUT /v1/design/brand":             scope(mediaW).json(`{"colors":[],"fonts":[],"logos":[]}`),
	"POST /v1/stock/photos/{id}/use":   scope(mediaW),
	"POST /v1/ai/text":                 scope(mediaW),
	"POST /v1/ai/jobs":                 scope(mediaW).file(),
	"POST /v1/ai/jobs/{id}/accept":     scope(mediaW),
	"DELETE /v1/ai/jobs/{id}":          scope(mediaW),
	"DELETE /v1/media/{id}":            scope(postD),
	"POST /v1/media/delete":            scope(postD),
	"POST /v1/media/{id}/restore":      scope(postD),
	"POST /v1/media/{id}/keep":         scope(postD, postW),
	"DELETE /v1/media-projects/{id}":   scope(postD),

	// Channels: reading is channels:read; whatever touches a platform's tokens is channels:manage.
	"GET /v1/providers":                        scope(chR),
	"GET /v1/channels":                         scope(chR),
	"GET /v1/channels/{id}":                    scope(chR),
	"GET /v1/channels/{id}/options/{key}":      scope(chR),
	"GET /v1/channel-groups":                   scope(chR),
	"GET /v1/channels/telegram/connect/{code}": scope(chR),
	"PUT /v1/providers/{id}/app":               scope(chM),
	"DELETE /v1/providers/{id}/app":            scope(chM),
	"POST /v1/providers/{id}/check":            scope(chM),
	"POST /v1/channels":                        scope(chM),
	"PATCH /v1/channels/{id}":                  scope(chM),
	"DELETE /v1/channels/{id}":                 scope(chM),
	"POST /v1/channels/{id}/test":              scope(chM),
	"GET /v1/channels/oauth/{provider}/start":  scope(chM),
	"GET /v1/pending-connections/{id}":         scope(chM),
	"POST /v1/pending-connections/{id}":        scope(chM),
	"POST /v1/channel-groups":                  scope(chM),
	"PATCH /v1/channel-groups/{id}":            scope(chM),
	"DELETE /v1/channel-groups/{id}":           scope(chM),
	"GET /v1/channel-links":                    scope(chM),
	"POST /v1/channel-links":                   scope(chM),
	"DELETE /v1/channel-links/{id}":            scope(chM),
	"POST /v1/channels/moltbook/register":      scope(chM),
	"POST /v1/channels/telegram/connect":       scope(chM),

	// Events, webhooks, alerts.
	"GET /v1/events":                         scope(events),
	"GET /v1/events/export":                  scope(events),
	"GET /v1/webhooks":                       scope(hooks),
	"POST /v1/webhooks":                      scope(hooks),
	"GET /v1/webhooks/{id}":                  scope(hooks),
	"PATCH /v1/webhooks/{id}":                scope(hooks),
	"DELETE /v1/webhooks/{id}":               scope(hooks),
	"POST /v1/webhooks/{id}/test":            scope(hooks),
	"GET /v1/webhook-deliveries":             scope(hooks),
	"POST /v1/webhook-deliveries/{id}/retry": scope(hooks),
	"GET /v1/alerts":                         scope(alerts),
	"POST /v1/alerts":                        scope(alerts),
	"GET /v1/alerts/{id}":                    scope(alerts),
	"PATCH /v1/alerts/{id}":                  scope(alerts),
	"DELETE /v1/alerts/{id}":                 scope(alerts),
	"POST /v1/alerts/{id}/test":              scope(alerts),
	"GET /v1/alerts/{id}/deliveries":         scope(alerts),
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// routerRoutes: every "METHOD /path" the router serves (/mcp, mounted for every method, once as POST).
func routerRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	seen := map[string]bool{}
	_ = chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/mcp" {
			method = "POST"
		}
		seen[method+" "+route] = true
		return nil
	})
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func covers(have, need []string) bool {
	for _, n := range need {
		if !slices.Contains(have, n) && !slices.Contains(have, admin) {
			return false
		}
	}
	return true
}

func rank(role string) int { return slices.Index(app.Roles, role) }

// caller: someone the matrix calls as.
type caller struct {
	name   string
	c      *http.Client // a person's browser (cookie + csrf)
	csrf   string
	key    string
	scopes []string // a key's scopes
	role   string   // the person's role, or the role of the person who made the key
	id     fakeIdentity
}

func (c caller) allowed(r rule) bool {
	switch {
	case c.key == "" && c.c == nil:
		return r.public
	case c.key == "":
		return !r.keysOnly && covers(wantRoleScopes[c.role], r.scopes) && rank(c.role) >= rank(r.minRole)
	case r.people:
		return false
	case r.keyAdmin && !slices.Contains(c.scopes, admin):
		return false
	}
	// A key never does more than the person who made it.
	return covers(c.scopes, r.scopes) && covers(wantRoleScopes[c.role], r.scopes) && rank(c.role) >= rank(r.minRole)
}

type matrixEnv struct {
	*acctEnv
	ip           int
	owner, admin *person
	keys         map[string]caller
}

// keyOf: a key with exactly these scopes, made by the owner (or by the admin: never more than an admin).
func (e *matrixEnv) keyOf(by *person, scopes ...string) caller {
	e.t.Helper()
	role, name := app.RoleOwner, "key "+strings.Join(scopes, "+")
	if len(scopes) > 8 { // every scope but one or two: name what is missing
		missing := slices.DeleteFunc(slices.Clone(app.AllScopes), func(s string) bool { return s == admin || slices.Contains(scopes, s) })
		name = "key without " + strings.Join(missing, "+")
	}
	if by == e.admin {
		role, name = app.RoleAdmin, name+" made by an admin"
	}
	if c, ok := e.keys[name]; ok {
		return c
	}
	var k app.APIKey
	e.as(by, "POST", "/v1/api-keys", map[string]any{"name": name, "scopes": scopes}, 201, &k)
	c := caller{name: name, key: k.Key, scopes: scopes, role: role}
	e.keys[name] = c
	return c
}

// keysFor: the keys that say something about a rule. The admin key of the owner and of an admin; a key with
// exactly the rule's scopes (enough); and, per scope, a key with every other scope but admin (never enough).
func (e *matrixEnv) keysFor(r rule) []caller {
	out := []caller{e.keyOf(e.owner, admin), e.keyOf(e.admin, admin)}
	if len(r.scopes) == 0 {
		return append(out, e.keyOf(e.owner, events)) // the narrowest key there is
	}
	if !slices.Contains(r.scopes, admin) {
		out = append(out, e.keyOf(e.owner, r.scopes...))
	}
	for _, sc := range r.scopes {
		rest := slices.DeleteFunc(slices.Clone(app.AllScopes), func(s string) bool { return s == sc || s == admin })
		out = append(out, e.keyOf(e.owner, rest...))
	}
	return out
}

// newMatrixEnv: cloud accounts behind a trusted proxy, so every request can come from an address of its own and
// the per-address limits (sign-up, email codes, passkeys) never answer for the gate under test.
func newMatrixEnv(t *testing.T) *matrixEnv {
	t.Helper()
	database, err := storetest.Open(t, t.TempDir()+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	return &matrixEnv{acctEnv: newAccountsEnvCfg(t, "", database, httpapi.Config{TrustProxy: true}), keys: map[string]caller{}}
}

// send makes the request the rule describes (path parameters filled with ids nobody has) and returns the answer.
func (e *matrixEnv) send(c caller, route string, r rule, hdr ...string) (int, http.Header, string) {
	e.t.Helper()
	method, path, _ := strings.Cut(route, " ")
	path = pathParam.ReplaceAllString(path, "nobody")
	var body io.Reader
	ctype := "application/json"
	switch {
	case r.upload:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "dot.png")
		fw.Write(pngBytes)
		mw.Close()
		body, ctype = &buf, mw.FormDataContentType()
	case r.body != "":
		body = strings.NewReader(r.body)
	case method != "GET" && method != "DELETE":
		body = strings.NewReader("{}")
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if body != nil {
		req.Header.Set("Content-Type", ctype)
	}
	e.ip++
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.%d.%d.%d", e.ip>>16&255, e.ip>>8&255, e.ip&255))
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	client := c.c
	if client == nil {
		client = noRedirects(nil)
	}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, resp.Header, string(raw)
}

// gated: the sign-in gate's own answer (requireAuth), not a handler's 401 for a wrong code or key.
func gated(status int, h http.Header) bool {
	return status == 401 && h.Get("WWW-Authenticate") != ""
}

// callers: nobody, then one person per role.
func (e *matrixEnv) callers() []caller {
	e.t.Helper()
	owner, adminP, editor, viewer, _ := e.team()
	_, token := e.invite(owner, frank.Email, app.RoleOperator)
	operator := e.mustSignIn(frank, "invite="+url.QueryEscape(token))
	keys := int64(100) // more than a plan gives
	if err := e.svc.AdminSetWorkspaceLimit(context.Background(), app.Admin{Email: "tests@staqly.ai"}, e.session(owner).Workspace.ID, app.LimitAPIKeys, &keys, ""); err != nil {
		e.t.Fatal(err)
	}
	out := []caller{{name: "nobody"}}
	for _, p := range []struct {
		role string
		p    *person
	}{{app.RoleOwner, owner}, {app.RoleAdmin, adminP}, {app.RoleOperator, operator}, {app.RoleEditor, editor}, {app.RoleViewer, viewer}} {
		out = append(out, caller{name: p.role, c: p.p.c, csrf: p.p.csrf, role: p.role, id: p.p.id})
	}
	e.owner, e.admin = owner, adminP
	return out
}

func TestRoleScopesArePinned(t *testing.T) {
	if !reflect.DeepEqual(app.RoleScopes, wantRoleScopes) {
		t.Fatalf("app.RoleScopes changed:\n got %v\nwant %v", app.RoleScopes, wantRoleScopes)
	}
	// Each role holds everything the role below it does.
	for i := 1; i < len(app.Roles); i++ {
		if !covers(wantRoleScopes[app.Roles[i]], wantRoleScopes[app.Roles[i-1]]) {
			t.Fatalf("%s may do less than %s", app.Roles[i], app.Roles[i-1])
		}
	}
}

// Every route × every role, every kind of key and nobody answers what accessRules declares. A refusal is 401 from
// the sign-in gate or 403; being let in is anything else a handler says about ids and bodies nobody has (404, 422...).
func TestAccessMatrix(t *testing.T) {
	e := newMatrixEnv(t)
	people := e.callers()
	// Billing on, once the workspace has its trial: without Stripe the payment routes answer 409 before they ask
	// who is calling.
	withStripe(t, e.env).set("GET /v1/products", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	routes := routerRoutes(t, e.srv.Config.Handler)
	for name := range accessRules {
		if !slices.Contains(routes, name) {
			t.Errorf("accessRules names %q, which the router does not serve: remove the rule", name)
		}
	}
	checks := 0
	for _, route := range routes {
		r, ok := accessRules[route]
		if !ok {
			t.Errorf("%s has no rule in accessRules: declare who may call it", route)
			continue
		}
		var wrong []string
		callers := people[:1] // a public route has no gate to pass: who else asks changes nothing
		if !r.public {
			callers = append(slices.Clone(people), e.keysFor(r)...)
		}
		for _, c := range callers {
			if c.c != nil && r.ownSession {
				p := e.mustSignIn(c.id, "")
				c.c, c.csrf = p.c, p.csrf
			}
			status, hdr, _ := e.send(c, route, r)
			checks++
			want := c.allowed(r)
			var got bool
			switch {
			case c.key == "" && c.c == nil, r.keysOnly:
				got = !gated(status, hdr) // nobody, and a cookie where cookies are not read: only the gate refuses
			default:
				got = status != 401 && status != 403
			}
			if got != want || status == 500 || status == 429 {
				wrong = append(wrong, fmt.Sprintf("%s got %d", c.name, status))
			}
		}
		if len(wrong) > 0 {
			t.Errorf("%s: the answer does not match its rule %+v for: %s", route, r, strings.Join(wrong, "; "))
		}
	}
	t.Logf("%d routes, %d checks", len(routes), checks)
}
