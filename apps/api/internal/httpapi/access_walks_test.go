package httpapi_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Two walks over the router's route table, next to the access matrix: another workspace's ids, and unsafe
// requests with a session but no CSRF token.

// foreignIDs: what a path parameter names, by the path segment before it ("ai/jobs" before "jobs"). "" with a
// reason = not a workspace's resource, or nothing a test can make here. A new id-addressed route whose
// parameter is not listed fails TestOtherWorkspaceIDs.
var foreignIDs = []struct{ segment, fixture, why string }{
	{"collections/{id}", "collection", ""},
	{"posts/{id}", "post", ""},
	{"targets/{channel}", "channel", ""},
	{"comments/{comment}", "comment", ""},
	{"previews/{preview}", "preview", ""},
	{"channels/{id}", "channel", ""},
	{"options/{key}", "", "an option's name, not an id"},
	{"channel-groups/{id}", "group", ""},
	{"channel-links/{id}", "link", ""},
	{"schedules/{id}", "schedule", ""},
	{"ai/jobs/{id}", "", "an AI job needs an AI provider: ai_test.go"},
	{"jobs/{id}", "job", ""},
	{"media/{id}", "media", ""},
	{"media-folders/{id}", "folder", ""},
	{"media-projects/{id}", "project", ""},
	{"webhooks/{id}", "webhook", ""},
	{"webhook-deliveries/{id}", "delivery", ""},
	{"alerts/{id}", "alert", ""},
	{"exports/{id}", "export", ""},
	{"api-keys/{id}", "key", ""},
	{"agents/{id}", "agent", ""},
	{"sessions/{id}", "session", ""},
	{"members/{user_id}", "member", ""},
	{"invitations/{id}", "invitation", ""},
	{"chats/{id}", "", "an assistant chat needs AI: assistant tests"},
	{"pending-connections/{id}", "", "made by a platform's OAuth answer: pending_connections_test.go"},
	{"connect/{code}", "", "made by the Telegram bot: telegram tests"},
	{"providers/{id}", "", "the platform catalog, the same for everyone"},
	{"oauth/{provider}", "", "the platform catalog"},
	{"addons/{id}", "", "the add-on catalog"},
	{"photos/{id}", "", "the stock library's id"},
	{"passkey/{id}", "", "the person's own passkey: passkeys_test.go"},
	{"sign-in-methods/{provider}", "", "the person's own sign-in method"},
	{"{provider}/{subject}", "", "the person's own sign-in method"},
}

var nextMonth = time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)

// foreignBodies: a body that passes validation, where {} would be refused before the id is looked up.
var foreignBodies = map[string]string{
	"POST /v1/posts/{id}/schedule":                 `{"publish_at":"` + nextMonth + `"}`,
	"POST /v1/posts/{id}/targets/{channel}/result": `{"status":"published"}`,
	"PATCH /v1/members/{user_id}":                  `{"role":"viewer"}`,
	"PATCH /v1/posts/{id}":                         `{"content":"taken over"}`,
	"PATCH /v1/collections/{id}":                   `{"name":"taken over"}`,
	"PATCH /v1/channels/{id}":                      `{"name":"taken over"}`,
	"PATCH /v1/channel-groups/{id}":                `{"name":"taken over"}`,
	"PATCH /v1/schedules/{id}":                     `{"name":"taken over"}`,
	"PATCH /v1/media-folders/{id}":                 `{"name":"taken over"}`,
	"PATCH /v1/alerts/{id}":                        `{"name":"taken over"}`,
	"POST /v1/posts/{id}/comments":                 `{"body":"taken over"}`,
}

// A workspace with one of everything; ids by fixture name.
func (e *matrixEnv) oneOfEverything(owner *person) map[string]string {
	t := e.t
	t.Helper()
	ids := map[string]string{}
	post := func(path string, body any, want int, out any) { e.as(owner, "POST", path, body, want, out) }
	var id struct {
		ID string `json:"id"`
	}
	make1 := func(name, path string, body any) {
		id.ID = ""
		post(path, body, 201, &id)
		ids[name] = id.ID
	}
	make1("collection", "/v1/collections", map[string]any{"name": "Private plans"})
	make1("channel", "/v1/channels", map[string]any{"provider": "custom", "name": "Private bot"})
	make1("post", "/v1/posts", map[string]any{"title": "Private", "collection": ids["collection"], "content": "private words",
		"targets": []map[string]any{{"channel_id": ids["channel"]}}})
	make1("comment", "/v1/posts/"+ids["post"]+"/comments", map[string]any{"body": "private note"})
	make1("preview", "/v1/posts/"+ids["post"]+"/previews", map[string]any{})
	make1("group", "/v1/channel-groups", map[string]any{"name": "Private client"})
	make1("link", "/v1/channel-links", map[string]any{})
	make1("schedule", "/v1/schedules", map[string]any{"collection": ids["collection"], "name": "Private daily", "kind": "recurring",
		"channels": []string{ids["channel"]}, "rule": map[string]any{"times": []string{"08:00"}}})
	make1("folder", "/v1/media-folders", map[string]any{"name": "Private folder"})
	make1("webhook", "/v1/webhooks", map[string]any{"url": "http://127.0.0.1:9/h", "events": []string{"post.*"}})
	make1("alert", "/v1/alerts", map[string]any{"name": "Private alert", "type": "ntfy", "config": map[string]any{"topic": "private"}})
	var key app.APIKey
	post("/v1/api-keys", map[string]any{"name": "Private key", "scopes": []string{app.ScopePostsRead}}, 201, &key)
	ids["key"] = key.ID
	make1("invitation", "/v1/invitations", map[string]any{"email": "private@example.com", "role": app.RoleViewer})
	post("/v1/workspace/export", nil, 202, &id)
	ids["export"] = id.ID

	upload := func(name, path string, fields map[string]string, filename string, content []byte) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		fw, _ := mw.CreateFormFile("file", filename)
		fw.Write(content)
		mw.Close()
		id.ID = ""
		req, _ := http.NewRequest("POST", e.srv.URL+path, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-CSRF-Token", owner.csrf)
		resp, err := owner.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 || json.NewDecoder(resp.Body).Decode(&id) != nil {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		ids[name] = id.ID
	}
	upload("media", "/v1/media", nil, "private.png", pngBytes)
	upload("project", "/v1/media-projects", map[string]string{"name": "Private reel"},
		"p.json", []byte(`{"version":1,"kind":"video","width":1080,"height":1920,"clips":[{"id":"c1"}],"captions":[],"sounds":[]}`))

	// Publishing to the custom channel queues a job; the key's first call makes it a connected agent.
	post("/v1/posts/"+ids["post"]+"/schedule", map[string]any{"publish_at": nextMonth}, 200, nil)
	list := func(path string) string {
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		e.as(owner, "GET", path, nil, 200, &page)
		if len(page.Data) == 0 {
			t.Fatalf("%s: nothing listed", path)
		}
		return page.Data[0].ID
	}
	ids["job"] = list("/v1/jobs")
	e.do(nil, "GET", "/v1/posts", key.Key, nil, 200, nil, "User-Agent", "private-agent/1")
	ids["agent"] = list("/v1/agents")
	post("/v1/webhooks/"+ids["webhook"]+"/test", nil, 0, nil)
	ids["delivery"] = list("/v1/webhook-deliveries")
	s := e.session(owner)
	ids["session"], ids["member"] = s.Principal.ID, s.User.ID
	return ids
}

// state: everything the owner can list, as the API answers it.
func (e *matrixEnv) state(owner *person, ids map[string]string) string {
	e.t.Helper()
	var b strings.Builder
	for _, path := range []string{"/v1/workspace", "/v1/collections", "/v1/posts", "/v1/posts/" + ids["post"] + "/comments",
		"/v1/posts/" + ids["post"] + "/previews", "/v1/channels", "/v1/channel-groups", "/v1/channel-links", "/v1/schedules",
		"/v1/jobs", "/v1/media", "/v1/media-folders", "/v1/media-projects", "/v1/webhooks", "/v1/webhook-deliveries", "/v1/alerts",
		"/v1/api-keys", "/v1/agents", "/v1/members", "/v1/invitations", "/v1/workspace/exports"} {
		var v any
		e.as(owner, "GET", path, nil, 200, &v)
		raw, _ := json.Marshal(v)
		b.WriteString(path + " " + string(raw) + "\n")
	}
	return b.String()
}

// Every route that names a resource by id, called by another workspace's owner and by that owner's admin key
// with this workspace's ids: 404 (403 where the caller may not ask at all), nothing told and nothing changed.
func TestOtherWorkspaceIDs(t *testing.T) {
	e := newMatrixEnv(t)
	owner := e.mustSignIn(ana, "")
	ids := e.oneOfEverything(owner)
	eve := e.mustSignIn(eve, "")
	var key app.APIKey
	e.as(eve, "POST", "/v1/api-keys", map[string]any{"name": "Eve", "scopes": []string{app.ScopeAdmin}}, 201, &key)
	outsiders := []caller{{name: "another workspace's owner", c: eve.c, csrf: eve.csrf}, {name: "another workspace's admin key", key: key.Key}}
	before := e.state(owner, ids)

	walked := 0
	for _, route := range routerRoutes(t, e.srv.Config.Handler) {
		r := accessRules[route]
		if r.public || !strings.Contains(route, "{") {
			continue // a public link carries its own token; no id, nothing to cross
		}
		path, skip := route, false
		for _, param := range pathParam.FindAllString(route, -1) {
			known := false
			for _, f := range foreignIDs {
				if !strings.Contains(route, "/"+f.segment) || !strings.HasSuffix(f.segment, param) {
					continue
				}
				known = true
				if f.fixture == "" {
					skip = skip || f.why != "an option's name, not an id"
					path = strings.Replace(path, param, "x", 1)
				} else if ids[f.fixture] == "" {
					t.Fatalf("no fixture %q", f.fixture)
				} else {
					path = strings.Replace(path, param, ids[f.fixture], 1)
				}
				break
			}
			if !known {
				t.Errorf("%s: %s is not in foreignIDs: say which fixture it names, or why none", route, param)
				skip = true
			}
		}
		if skip {
			continue
		}
		walked++
		r.body = foreignBodies[route]
		method, _, _ := strings.Cut(route, " ")
		// The ids are real: their owner finds what they name.
		if method == "GET" && !strings.Contains(route, "{key}") { // no custom channel has a setting with options
			if status, _, body := e.send(caller{c: owner.c, csrf: owner.csrf}, path, r); status == 404 || status == 403 {
				t.Errorf("%s as the owner: %d %s", route, status, body)
			}
		}
		for _, c := range outsiders {
			status, _, body := e.send(c, path, r)
			empty := status == 200 && (strings.Contains(body, `"data":[]`) || strings.TrimSpace(body) == "[]")
			if status != 404 && status != 403 && !empty {
				t.Errorf("%s as %s with another workspace's id: %d %s", route, c.name, status, body)
			}
			if strings.Contains(strings.ToLower(body), "private") {
				t.Errorf("%s as %s tells what the other workspace holds: %s", route, c.name, body)
			}
		}
	}
	// Ids in a body: the same answer as an id nobody has.
	for _, c := range []struct {
		method, path string
		body         any
		want         []int
	}{
		{"POST", "/v1/posts", map[string]any{"title": "x", "collection": ids["collection"], "content": "x"}, []int{404, 422}},
		{"POST", "/v1/posts/delete", map[string]any{"ids": []string{ids["post"]}}, []int{200, 404}},
		{"POST", "/v1/media/delete", map[string]any{"ids": []string{ids["media"]}}, []int{200, 404}},
		{"POST", "/v1/schedules", map[string]any{"collection": ids["collection"], "name": "x", "kind": "recurring", "channels": []string{ids["channel"]}, "rule": map[string]any{"times": []string{"08:00"}}}, []int{404, 422}},
		{"POST", "/v1/session/workspace", map[string]any{"workspace_id": e.session(owner).Workspace.ID}, []int{404}},
	} {
		resp := e.do(eve.c, c.method, c.path, "", c.body, 0, nil, "X-CSRF-Token", eve.csrf)
		if !slices.Contains(c.want, resp.StatusCode) {
			t.Errorf("%s %s with another workspace's ids: %d, want one of %v", c.method, c.path, resp.StatusCode, c.want)
		}
	}
	if after := e.state(owner, ids); after != before {
		t.Errorf("the other workspace changed:\nbefore %s\nafter  %s", before, after)
	}
	if walked < 75 {
		t.Fatalf("only %d id-addressed routes walked", walked)
	}
	t.Logf("%d id-addressed routes", walked)
}

// Every unsafe route with a session cookie and no (or a wrong) X-CSRF-Token is refused before anything runs.
func TestCSRFOnEveryUnsafeRoute(t *testing.T) {
	e := newMatrixEnv(t)
	owner := e.mustSignIn(ana, "")
	before := e.session(owner)
	n := 0
	for _, route := range routerRoutes(t, e.srv.Config.Handler) {
		method, path, _ := strings.Cut(route, " ")
		if method == "GET" || !strings.HasPrefix(path, "/v1/") {
			continue // /mcp reads no cookie at all (TestAccessMatrix)
		}
		for _, token := range []string{"", "wrong"} {
			status, _, body := e.send(caller{name: "owner", c: owner.c, csrf: token}, route, accessRules[route])
			if status != 403 || !strings.Contains(body, `"csrf_failed"`) {
				t.Errorf("%s with a session and X-CSRF-Token %q: %d %s", route, token, status, body)
			}
		}
		n++
	}
	if n < 100 {
		t.Fatalf("only %d unsafe routes", n)
	}
	t.Logf("%d unsafe routes", n)
	// Nothing ran: not even the sign-out.
	if after := e.session(owner); after.Workspace.ID != before.Workspace.ID {
		t.Fatalf("session after: %+v", after)
	}
}

// writableWhenSuspended: what an unpaid workspace (trial or plan over) may still do; the router's own list is
// allowedWhileSuspended. Sign out, secure itself, take its data out, delete itself, pay; and what belongs to the
// person, not to the workspace.
var writableWhenSuspended = []string{
	"DELETE /v1/session", "DELETE /v1/sessions/{id}", "DELETE /v1/api-keys/{id}", "POST /v1/device-links",
	"POST /v1/recovery-key/rotate", "POST /v1/workspace/export", "DELETE /v1/workspace", "POST /v1/posts/{id}/purge",
	"POST /v1/billing/checkout", "POST /v1/billing/portal", "POST /v1/billing/sync", "POST /v1/billing/promotion-code",
	"POST /v1/feedback", "POST /v1/session/workspace", "POST /v1/session/onboarded", "POST /v1/account/workspaces",
	"POST /v1/invitations/accept", "PATCH /v1/account", "PUT /v1/account/notifications",
	"POST /v1/account/sign-in-methods/email/start", "POST /v1/account/sign-in-methods/email/verify",
	"POST /v1/account/sign-in-methods/email/change/current/start", "POST /v1/account/sign-in-methods/email/change/current/verify",
	"POST /v1/account/sign-in-methods/email/change/new/start", "POST /v1/account/sign-in-methods/email/change/new/verify",
	"POST /v1/account/sign-in-methods/passkey/start", "POST /v1/account/sign-in-methods/passkey/finish",
	"PATCH /v1/account/sign-in-methods/passkey/{id}", "POST /v1/account/sign-in-methods/google",
	"DELETE /v1/account/sign-in-methods/{provider}/{subject}",
}

// A workspace whose trial ended is read-only: every route that changes something answers 402 plan_expired to its
// owner, by session and by key, but the ones writableWhenSuspended lists; reading goes on.
func TestSuspendedWorkspaceIsReadOnly(t *testing.T) {
	e := newMatrixEnv(t)
	owner := e.mustSignIn(ana, "")
	var key app.APIKey
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "Admin", "scopes": []string{app.ScopeAdmin}}, 201, &key)
	e.svc.Now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) } // the 7-day trial is over
	owner = e.mustSignIn(ana, "")
	for _, route := range writableWhenSuspended {
		if _, ok := accessRules[route]; !ok {
			t.Errorf("writableWhenSuspended names %q, which is not a route", route)
		}
	}
	n := 0
	for _, route := range routerRoutes(t, e.srv.Config.Handler) {
		r := accessRules[route]
		method, path, _ := strings.Cut(route, " ")
		if r.public || !strings.HasPrefix(path, "/v1/") {
			continue
		}
		callers := []caller{{name: "the owner", c: owner.c, csrf: owner.csrf, id: ana}}
		if !r.people {
			callers = append(callers, caller{name: "the owner's key", key: key.Key})
		}
		for _, c := range callers {
			if c.c != nil && r.ownSession {
				p := e.mustSignIn(c.id, "")
				c.c, c.csrf = p.c, p.csrf
			}
			status, _, body := e.send(c, route, r)
			readOnly := status == 402 && strings.Contains(body, `"plan_expired"`)
			if want := method != "GET" && !slices.Contains(writableWhenSuspended, route); readOnly != want {
				t.Errorf("%s as %s in a suspended workspace: refused as read-only = %v, want %v (%d %s)", route, c.name, readOnly, want, status, body)
			}
		}
		n++
	}
	if n < 150 {
		t.Fatalf("only %d routes", n)
	}
}
