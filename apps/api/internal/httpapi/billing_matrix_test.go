package httpapi_test

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Every billing endpoint against every role and against API keys. Who may: everyone who reads posts (the X meter),
// admins and owners (reading the plan, its add-ons and invoices; a sync), owners only (the pending payment and
// checking a code, which read the payer's subscription), and the signed-in owner alone for whatever spends money or
// changes the subscription: a key never does, whoever made it. A refused request never reaches Stripe.
func TestBillingRoleMatrix(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, editor, viewer, _ := e.team()
	_, token := e.invite(owner, frank.Email, app.RoleOperator)
	operator := e.mustSignIn(frank, "invite="+url.QueryEscape(token))
	var adminKey, readKey app.APIKey
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "owner's admin key", "scopes": []string{app.ScopeAdmin}}, 201, &adminKey)
	e.as(owner, "POST", "/v1/api-keys", map[string]any{"name": "read key", "scopes": []string{app.ScopePostsRead}}, 201, &readKey)
	f := withStripe(t, e.env)
	f.set("GET /v1/products", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("GET /v1/promotion_codes", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("GET /v1/prices", func() (int, any) {
		return 200, map[string]any{"data": []any{map[string]any{"id": "price_credits", "unit_amount": 1000}}}
	})
	f.set("POST /v1/customers/cus_1", func() (int, any) { return 200, map[string]string{"id": "cus_1"} })
	f.set("GET /v1/checkout/sessions", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("POST /v1/billing_portal/sessions", func() (int, any) { return 200, map[string]string{"url": "https://billing.stripe.test/p"} })

	const (
		readers     = "viewer editor operator admin owner read-key admin-key"
		admins      = "admin owner admin-key"
		owners      = "owner admin-key" // an owner, or the key an owner made
		ownerPerson = "owner"
	)
	callers := []string{"viewer", "editor", "operator", "admin", "read-key", "admin-key", "owner"}
	people := map[string]*person{"viewer": viewer, "editor": editor, "operator": operator, "admin": admin, "owner": owner}
	keys := map[string]string{"read-key": readKey.Key, "admin-key": adminKey.Key}
	for _, c := range []struct {
		method, path string
		body         any
		allowed      string
		ok           int // what an allowed caller gets in this workspace (no plan bought yet)
	}{
		{"GET", "/v1/billing/x-posts", nil, readers, 200},
		{"GET", "/v1/billing", nil, admins, 200},
		{"GET", "/v1/billing/details", nil, admins, 200},
		{"GET", "/v1/billing/addons", nil, admins, 200},
		{"POST", "/v1/billing/sync", nil, admins, 200},
		{"GET", "/v1/billing/pending-payment", nil, owners, 200},
		{"POST", "/v1/billing/promotion-code", map[string]string{"promotion_code": "NOPE"}, owners, 422},
		{"POST", "/v1/billing/start-paid-plan", nil, ownerPerson, 409},                           // not in a trial
		{"POST", "/v1/billing/addons/webhooks_pack", nil, ownerPerson, 404},                      // not on sale
		{"DELETE", "/v1/billing/addons/webhooks_pack", nil, ownerPerson, 409},                    // no plan yet
		{"POST", "/v1/billing/portal", nil, ownerPerson, 409},                                    // no customer yet
		{"POST", "/v1/billing/credits/checkout", map[string]any{"posts": 200}, ownerPerson, 200}, // makes the customer
		{"POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, ownerPerson, 200},
		{"POST", "/v1/billing/portal", nil, ownerPerson, 200},
	} {
		for _, who := range callers {
			want := 403
			if slices.Contains(strings.Fields(c.allowed), who) {
				want = c.ok
			}
			before := len(f.calls)
			var errBody struct{ Error app.Error }
			if p, ok := people[who]; ok {
				e.as(p, c.method, c.path, c.body, want, &errBody)
			} else {
				e.do(nil, c.method, c.path, keys[who], c.body, want, &errBody)
			}
			if want == 403 && (errBody.Error.Code != "forbidden" || len(f.calls) != before) {
				t.Fatalf("%s %s as %s: %+v, Stripe asked %v", c.method, c.path, who, errBody.Error, f.calls[before:])
			}
		}
	}
	// The key lost what its maker had: an owner made admin can no longer read the pending payment with it.
	var members struct{ Data []app.Member }
	e.as(owner, "GET", "/v1/members", nil, 200, &members)
	for _, m := range members.Data {
		if m.Email == bob.Email {
			e.as(owner, "PATCH", "/v1/members/"+m.UserID, map[string]string{"role": app.RoleOwner}, 200, nil)
		}
	}
	for _, m := range members.Data {
		if m.Email == ana.Email {
			e.as(admin, "PATCH", "/v1/members/"+m.UserID, map[string]string{"role": app.RoleAdmin}, 200, nil)
		}
	}
	e.do(nil, "GET", "/v1/billing/pending-payment", adminKey.Key, nil, 403, nil)
	e.do(nil, "GET", "/v1/billing", adminKey.Key, nil, 200, nil)
	e.as(owner, "POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 403, nil)
	e.as(admin, "POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // Bob owns it now
	// One seat per member, whatever the role: the five of them, not the invitations nor the keys.
	if form := f.forms["POST /v1/checkout/sessions"]; form.Get("line_items[0][quantity]") != "5" || form.Get("mode") != "subscription" ||
		form.Get("subscription_data[metadata][workspace_id]") == "" || form.Get("customer") != "cus_1" {
		t.Fatalf("checkout of a workspace of five: %v", form)
	}
	// And a switch of the plan they then pay for keeps the five seats.
	ws := e.session(admin).Workspace.ID
	sub := subObject("sub_1", ws, "active", "starter", "month")
	sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["quantity"] = 5
	if code := e.stripeEvent("evt_five", "customer.subscription.created", sub, testWhsec); code != 200 {
		t.Fatalf("subscription event: %d", code)
	}
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, sub })
	f.set("POST /v1/subscriptions/sub_1", func() (int, any) { return 200, map[string]string{"id": "sub_1"} })
	e.as(admin, "POST", "/v1/billing/checkout", map[string]string{"plan": "plus"}, 200, nil)
	if form := f.forms["POST /v1/subscriptions/sub_1"]; form.Get("items[0][quantity]") != "5" || form.Get("items[0][id]") != "si_sub_1" {
		t.Fatalf("plan switch of a workspace of five: %v", form)
	}
}

// A read-only workspace (its plan ended) can still pay: the checkout, the portal, a sync and checking a code go
// through, and so do the account-level moves; everything else that changes the workspace, billing extras
// included, answers 402 plan_expired until a plan is paid for. Reads always work.
func TestBillingWhileSuspended(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	f.set("GET /v1/products", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("GET /v1/promotion_codes", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("GET /v1/checkout/sessions", func() (int, any) { return 200, map[string]any{"data": []any{}} })
	f.set("POST /v1/customers/cus_1", func() (int, any) { return 200, map[string]string{"id": "cus_1"} })
	f.set("POST /v1/billing_portal/sessions", func() (int, any) { return 200, map[string]string{"url": "https://billing.stripe.test/p"} })
	admin := e.keyWith(app.ScopeAdmin)
	ws := workspaceID(e)
	c := e.collection("Before")
	if err := e.svc.SetPlan(context.Background(), ws, "starter", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if b := e.billing(admin); !b.Suspended {
		t.Fatalf("setup: %+v", b)
	}
	for _, c := range []struct {
		method, path string
		body         any
		want         int
	}{
		// Reads (before the checkout makes the Stripe customer whose invoices the details list).
		{"GET", "/v1/billing", nil, 200},
		{"GET", "/v1/billing/details", nil, 200},
		{"GET", "/v1/billing/addons", nil, 200},
		{"GET", "/v1/billing/pending-payment", nil, 200},
		{"GET", "/v1/billing/x-posts", nil, 200},
		{"GET", "/v1/posts", nil, 200},
		// Paying for a plan.
		{"POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200},
		{"POST", "/v1/billing/portal", nil, 200},
		{"POST", "/v1/billing/sync", nil, 200},
		{"POST", "/v1/billing/promotion-code", map[string]string{"promotion_code": "NOPE"}, 422},
		// Extras come with a plan.
		{"POST", "/v1/billing/start-paid-plan", nil, 402},
		{"POST", "/v1/billing/credits/checkout", map[string]any{"posts": 200}, 402},
		{"POST", "/v1/billing/addons/webhooks_pack", nil, 402},
		{"DELETE", "/v1/billing/addons/webhooks_pack", nil, 402},
		// The workspace itself.
		{"POST", "/v1/collections", map[string]string{"name": "After"}, 402},
		{"POST", "/v1/posts", map[string]any{"title": "t", "collection": c.ID, "content": "x"}, 402},
	} {
		var errBody struct{ Error app.Error }
		e.pay(c.method, c.path, c.body, c.want, &errBody)
		if c.want == 402 && errBody.Error.Code != "plan_expired" {
			t.Fatalf("%s %s: %+v", c.method, c.path, errBody.Error)
		}
	}
}

// With billing off (no Stripe key: an instance that sells nothing) reads say so and nothing can be bought.
func TestBillingEndpointsWithBillingOff(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		method, path string
		body         any
		want         int
	}{
		{"GET", "/v1/billing", nil, 200},
		{"GET", "/v1/billing/details", nil, 200},
		{"GET", "/v1/billing/addons", nil, 200},
		{"GET", "/v1/billing/x-posts", nil, 200},
		{"POST", "/v1/billing/sync", nil, 200},
		{"GET", "/v1/billing/pending-payment", nil, 409},
		{"POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 409},
		{"POST", "/v1/billing/portal", nil, 409},
		{"POST", "/v1/billing/promotion-code", map[string]string{"promotion_code": "SPRING20"}, 409},
		{"POST", "/v1/billing/start-paid-plan", nil, 409},
		{"POST", "/v1/billing/credits/checkout", map[string]any{"posts": 200}, 409},
		{"POST", "/v1/billing/addons/webhooks_pack", nil, 409},
		{"DELETE", "/v1/billing/addons/webhooks_pack", nil, 409},
	} {
		var out struct {
			Enabled *bool
			Data    []any
			Error   app.Error
		}
		e.pay(c.method, c.path, c.body, c.want, &out)
		if c.want == 409 && out.Error.Code != "not_available" {
			t.Fatalf("%s %s: %+v", c.method, c.path, out.Error)
		}
		if out.Enabled != nil && *out.Enabled {
			t.Fatalf("%s %s says billing is on", c.method, c.path)
		}
	}
}
