package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// An incomplete subscription (its first payment unpaid) never grants its plan: a paid plan the workspace has stays as
// it is while the payment is pending, a workspace without one stays without. incomplete_expired only records the
// status: it never ends or shortens a plan (the operator's, another subscription's) and never sets one.
func TestUnpaidFirstPaymentNeverGrantsPlan(t *testing.T) {
	type event struct {
		sub, status, plan string
	}
	type want struct {
		changed bool   // the plan was granted by the last event
		plan    string // when changed
		sub     string // the subscription on record
		status  string // its status
	}
	cases := []struct {
		name   string
		before string // "" (no plan), "paid" (a live subscription on record) or "operator" (a plan the operator set until a date)
		events []event
		want   want
	}{
		{"incomplete with a paid plan", "paid", []event{{"sub_B", "incomplete", "pro"}}, want{sub: "sub_A", status: "active"}},
		{"incomplete with an operator plan", "operator", []event{{"sub_B", "incomplete", "pro"}}, want{sub: "sub_B", status: "incomplete"}},
		{"incomplete with none", "", []event{{"sub_B", "incomplete", "pro"}}, want{sub: "sub_B", status: "incomplete"}},
		{"incomplete then active", "", []event{{"sub_B", "incomplete", "pro"}, {"sub_B", "active", "pro"}},
			want{changed: true, plan: "pro", sub: "sub_B", status: "active"}},
		{"incomplete then active over a paid plan on record as incomplete", "operator",
			[]event{{"sub_B", "incomplete", "pro"}, {"sub_B", "active", "pro"}},
			want{changed: true, plan: "pro", sub: "sub_B", status: "active"}},
		{"incomplete_expired with a paid plan", "paid", []event{{"sub_B", "incomplete", "pro"}, {"sub_B", "incomplete_expired", "pro"}},
			want{sub: "sub_A", status: "active"}},
		{"incomplete_expired with an operator plan", "operator",
			[]event{{"sub_B", "incomplete", "pro"}, {"sub_B", "incomplete_expired", "pro"}}, want{sub: "sub_B", status: "incomplete_expired"}},
		{"incomplete_expired with none", "", []event{{"sub_B", "incomplete", "pro"}, {"sub_B", "incomplete_expired", "pro"}},
			want{sub: "sub_B", status: "incomplete_expired"}},
		{"incomplete_expired told first", "operator", []event{{"sub_B", "incomplete_expired", "pro"}}, want{sub: "sub_B", status: "incomplete_expired"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			f := withStripe(t, e)
			ws := workspaceID(e)
			admin := e.keyWith(app.ScopeAdmin)
			e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil) // makes cus_1
			switch tc.before {
			case "":
				// An earlier subscription ended: no plan, read-only until one is paid for.
				e.stripeEvent("evt_A", "customer.subscription.created", subObject("sub_A", ws, "active", "plus", "month"), testWhsec)
				e.stripeEvent("evt_A2", "customer.subscription.deleted", subObject("sub_A", ws, "canceled", "plus", "month"), testWhsec)
				if b := e.billing(admin); !b.Suspended {
					t.Fatalf("setup: an ended subscription must leave no plan: %+v", b)
				}
			case "paid":
				e.stripeEvent("evt_A", "customer.subscription.created", subObject("sub_A", ws, "active", "plus", "month"), testWhsec)
				f.set("GET /v1/subscriptions/sub_A", func() (int, any) { return 200, subObject("sub_A", ws, "active", "plus", "month") })
				f.set("DELETE /v1/subscriptions/sub_B", func() (int, any) { return 200, map[string]any{} })
			case "operator":
				if err := e.svc.SetPlan(context.Background(), ws, "plus", time.Now().Add(60*24*time.Hour).Format(time.DateOnly)); err != nil {
					t.Fatal(err)
				}
			}
			before := e.billing(admin)
			check := func(when string) {
				t.Helper()
				b := e.billing(admin)
				if tc.want.changed {
					if b.Plan != tc.want.plan || b.Suspended || b.PlanExpiresAt == nil || *b.PlanExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
						t.Fatalf("%s: plan not granted: %+v", when, b)
					}
				} else if b.Plan != before.Plan || deref(b.PlanExpiresAt) != deref(before.PlanExpiresAt) || b.Suspended != before.Suspended {
					t.Fatalf("%s: plan changed from %s until %q (suspended %v) to %+v", when, before.Plan, deref(before.PlanExpiresAt), before.Suspended, b)
				}
				w, err := e.svc.Q.GetWorkspaceBilling(context.Background(), ws)
				if err != nil {
					t.Fatal(err)
				}
				if w.StripeSubscriptionID.String != tc.want.sub || w.SubscriptionStatus.String != tc.want.status {
					t.Fatalf("%s: on record %s %s, want %s %s", when, w.StripeSubscriptionID.String, w.SubscriptionStatus.String, tc.want.sub, tc.want.status)
				}
			}
			send := func(id string, ev event) {
				t.Helper()
				if code := e.stripeEvent(id, "customer.subscription.updated", subObject(ev.sub, ws, ev.status, ev.plan, "month"), testWhsec); code != 200 {
					t.Fatalf("event %s: %d", id, code)
				}
			}
			for i, ev := range tc.events {
				send(fmt.Sprint("evt_", i), ev)
			}
			check("applied")
			// Replayed: the same events again (already applied), and the last state told again by a new event.
			for i, ev := range tc.events {
				send(fmt.Sprint("evt_", i), ev)
			}
			send("evt_again", tc.events[len(tc.events)-1])
			check("replayed")
		})
	}
}

// The reconcile round applies the newest subscription when none is live: an incomplete_expired one changes nothing,
// so it must not hide that the older one on record ended (its event lost), nor end a plan itself.
func TestSyncSkipsIncompleteExpired(t *testing.T) {
	for _, ended := range []bool{false, true} {
		t.Run(fmt.Sprint("older ended ", ended), func(t *testing.T) {
			e := newEnv(t)
			f := withStripe(t, e)
			ws := workspaceID(e)
			admin := e.keyWith(app.ScopeAdmin)
			e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 200, nil)
			e.stripeEvent("evt_A", "customer.subscription.created", subObject("sub_A", ws, "active", "plus", "month"), testWhsec)
			before := e.billing(admin)
			list := []any{subObject("sub_B", ws, "incomplete_expired", "business", "month")}
			if ended {
				list = append(list, subObject("sub_A", ws, "canceled", "pro", "month"))
			}
			f.set("GET /v1/subscriptions", func() (int, any) { return 200, map[string]any{"data": list} })
			e.do(nil, "POST", "/v1/billing/sync", admin, nil, 200, nil)
			b := e.billing(admin)
			if ended && (!b.Suspended || b.Plan != "pro" || deref(b.SubscriptionStatus) != "canceled") {
				t.Fatalf("the older subscription's end was hidden: %+v", b)
			}
			if !ended && (b.Plan != before.Plan || deref(b.PlanExpiresAt) != deref(before.PlanExpiresAt) || b.Suspended) {
				t.Fatalf("incomplete_expired changed the plan: %+v", b)
			}
		})
	}
}
