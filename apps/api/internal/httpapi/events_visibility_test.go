package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Was a bug: GET /v1/events (and the event export) returned webhook.*, invitation.* and member.* events to
// anyone with events:read: a viewer, refused on GET /v1/webhooks, /v1/invitations and /v1/members, read the
// webhook's URL and the invitee's email in the feed. A family is listed only for a caller who may read the
// resource itself.
func TestEventFamiliesFollowTheResourceScope(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, editor, viewer, _ := e.team() // the invitations: invitation.created, invitation.accepted
	var members struct {
		Data []app.Member `json:"data"`
	}
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	for _, m := range members.Data {
		if m.Email == cleo.Email {
			e.as(owner, "PATCH", "/v1/members/"+m.UserID, map[string]any{"role": app.RoleEditor}, 200, nil) // member.updated
		}
	}
	e.as(owner, "POST", "/v1/webhooks", map[string]any{"url": "http://127.0.0.1:9/secret-hook", "events": []string{"post.*"}}, 201, nil)
	e.as(owner, "POST", "/v1/collections", map[string]any{"name": "Visible"}, 201, nil)
	key := func(scopes ...string) string {
		var k app.APIKey
		e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": strings.Join(scopes, " "), "scopes": scopes}, 201, &k)

		return k.Key
	}

	for _, c := range []struct {
		name             string
		person           *person
		key              string
		webhooks, people bool
	}{
		{"owner", owner, "", true, true},
		{"admin", admin, "", true, true},
		{"editor", editor, "", false, false},
		{"viewer", viewer, "", false, false},
		{"key events:read", nil, key(app.ScopeEventsRead), false, false},
		{"key events:read webhooks:manage", nil, key(app.ScopeEventsRead, app.ScopeWebhooksManage), true, false},
		{"key admin", nil, key(app.ScopeAdmin), true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			get := func(path string, want int, out any) []byte {
				t.Helper()
				req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
				client := http.DefaultClient
				if c.person != nil {
					client = c.person.c
				} else {
					req.Header.Set("Authorization", "Bearer "+c.key)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				raw, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != want {
					t.Fatalf("GET %s: status %d, want %d: %s", path, resp.StatusCode, want, raw)
				}
				if out != nil {
					if err := json.Unmarshal(raw, out); err != nil {
						t.Fatalf("decode %s: %v: %s", path, err, raw)
					}
				}

				return raw
			}
			status := func(ok bool) int {
				if ok {
					return 200
				}

				return 403
			}
			// The resources' own endpoints say what the caller may read.
			get("/v1/webhooks", status(c.webhooks), nil)
			get("/v1/members", status(c.people), nil)

			var page app.EventPage
			get("/v1/events?limit=200", 200, &page)
			listed := map[string]bool{}
			for _, ev := range page.Data {
				fam, _, _ := strings.Cut(ev.Type, ".")
				listed[fam] = true
			}
			exported := map[string]bool{}
			for _, line := range strings.Split(strings.TrimSpace(string(get("/v1/events/export?format=ndjson", 200, nil))), "\n") {
				var ev app.Event
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					t.Fatalf("export line %q: %v", line, err)
				}
				fam, _, _ := strings.Cut(ev.Type, ".")
				exported[fam] = true
			}
			for name, got := range map[string]map[string]bool{"list": listed, "export": exported} {
				if !got["collection"] {
					t.Errorf("%s: collection events are for everyone with events:read: %v", name, got)
				}
				if got["webhook"] != c.webhooks {
					t.Errorf("%s: webhook events listed = %v, want %v", name, got["webhook"], c.webhooks)
				}
				if got["invitation"] != c.people || got["member"] != c.people {
					t.Errorf("%s: invitation events listed = %v, member events = %v, want %v", name, got["invitation"], got["member"], c.people)
				}
			}
		})
	}
}

// Was a bug: a removed member's API keys answered 401 but stayed active in the list and in the plan's key
// count, so the owner hit 402 until revoking them by hand. Removing a member, or leaving, revokes them.
func TestRemovedMembersKeysAreRevoked(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, _, viewer, _ := e.team()
	var members struct {
		Data []app.Member `json:"data"`
	}
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	ids := map[string]string{}
	for _, m := range members.Data {
		ids[m.Email] = m.UserID
	}
	e.as(owner, "PATCH", "/v1/members/"+ids[dan.Email], map[string]any{"role": app.RoleOperator}, 200, nil)
	var ownerKey, bobKey, danKey app.APIKey
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "owner", "scopes": []string{app.ScopePostsRead}}, 201, &ownerKey)
	e.as(admin, "POST", "/v1/api-keys", map[string]any{"name": "bob", "scopes": []string{app.ScopePostsRead}}, 201, &bobKey)
	e.as(viewer, "POST", "/v1/api-keys", map[string]any{"name": "dan", "scopes": []string{app.ScopePostsRead}}, 201, &danKey)

	e.as(owner, "DELETE", "/v1/members/"+ids[dan.Email], nil, 204, nil) // removed
	e.as(admin, "DELETE", "/v1/members/"+ids[bob.Email], nil, 204, nil) // leaves
	var keys struct {
		Data []app.APIKey `json:"data"`
	}
	e.as(owner, "GET", "/v1/api-keys", nil, 200, &keys)
	revoked := map[string]bool{}
	for _, k := range keys.Data {
		revoked[k.ID] = k.RevokedAt != nil
	}
	if !revoked[danKey.ID] || !revoked[bobKey.ID] || revoked[ownerKey.ID] {
		t.Fatalf("revoked keys: %v, want the removed member's (%s) and the leaver's (%s), not the owner's (%s)", revoked, danKey.ID, bobKey.ID, ownerKey.ID)
	}
	var usage app.Usage
	e.as(owner, "GET", "/v1/workspace/usage", nil, 200, &usage)
	if usage.Usage[app.LimitAPIKeys] != 1 {
		t.Fatalf("keys counted against the plan: %v, want the owner's one", usage.Usage)
	}
	e.do(nil, "GET", "/v1/posts", ownerKey.Key, nil, 200, nil)
}
