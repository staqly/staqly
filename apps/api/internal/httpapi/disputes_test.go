package httpapi_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// A chargeback of a subscription payment: the owners and admins hear once per dispute, however often Stripe tells
// it; the billing status says disputed while it is open and the plan keeps working. Won: the alert clears. Lost:
// the subscription ends at once and the alert clears.
func TestSubscriptionDisputes(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	owner := w.mustSignIn(ana, "")
	ws := w.session(owner).Workspace.ID
	sub := w.f.sub("sub_d", ws, "active", "")
	w.f.set(w.env, sub, "active")
	if _, err := w.svc.DB.Exec(`UPDATE workspaces SET stripe_customer_id = 'cus_sub_d' WHERE id = ?`, ws); err != nil {
		t.Fatal(err)
	}
	_, tok := w.invite(owner, bob.Email, app.RoleAdmin)
	w.mustSignIn(bob, "invite="+url.QueryEscape(tok))
	_, tok = w.invite(owner, cleo.Email, app.RoleEditor)
	w.mustSignIn(cleo, "invite="+url.QueryEscape(tok))
	w.received("Subject:") // the welcome and the invitations

	dispute := func(id, status string) {
		w.f.mu.Lock()
		w.f.objects["/v1/disputes/"+id] = map[string]any{"id": id, "status": status, "charge": "ch_" + id}
		w.f.objects["/v1/charges/ch_"+id] = map[string]any{"id": "ch_" + id, "customer": "cus_sub_d", "payment_intent": "pi_" + id, "amount": 1500}
		w.f.mu.Unlock()
	}
	w.f.objects = map[string]any{
		"/v1/checkout/sessions": map[string]any{"data": []any{}}, // no bundle paid with it
		"/v1/invoice_payments":  map[string]any{"data": []any{map[string]any{"invoice": "in_d"}}},
		"/v1/invoices/in_d":     map[string]any{"id": "in_d", "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_d"}}},
	}
	send := func(evt, typ, id string) {
		t.Helper()
		if code := w.stripeEvent(evt, typ, map[string]any{"id": id}, testWhsec); code != 200 {
			t.Fatalf("%s %s: %d", typ, evt, code)
		}
	}
	billing := func() app.Billing {
		var b app.Billing
		w.as(owner, "GET", "/v1/billing", nil, 200, &b)
		return b
	}
	disputed := "Subject: A payment for your staqly workspace was disputed"

	// Opened, told three times: one email each to the owner and the admin, none to the editor.
	dispute("dp_won", "warning_needs_response")
	send("evt_1", "charge.dispute.created", "dp_won")
	send("evt_1", "charge.dispute.created", "dp_won")
	send("evt_2", "charge.dispute.updated", "dp_won")
	got := w.received(disputed)
	if len(got) != 2 || !strings.Contains(strings.Join(got, "\n"), "<ana@example.com>") || !strings.Contains(strings.Join(got, "\n"), "<bob@example.com>") {
		t.Fatalf("an open dispute: %d emails, want one each to the owner and the admin:\n%s", len(got), strings.Join(got, "\n---\n"))
	}
	if b := billing(); !b.Disputed || b.Plan != "starter" || b.Suspended {
		t.Fatalf("while disputed: %+v", b)
	}
	// Won: the alert clears, the plan stays.
	dispute("dp_won", "won")
	send("evt_3", "charge.dispute.closed", "dp_won")
	if b := billing(); b.Disputed || *b.SubscriptionStatus != "active" {
		t.Fatalf("a won dispute: %+v", b)
	}

	// Another one, lost: the subscription is cancelled now; told again, nothing more happens.
	dispute("dp_lost", "needs_response")
	send("evt_4", "charge.dispute.created", "dp_lost")
	dispute("dp_lost", "lost")
	send("evt_5", "charge.dispute.closed", "dp_lost")
	send("evt_5", "charge.dispute.closed", "dp_lost")
	if b := billing(); b.Disputed || *b.SubscriptionStatus != "canceled" {
		t.Fatalf("a lost dispute: %+v", b)
	}
	if got := w.received(disputed); len(got) != 0 {
		t.Fatalf("a dispute lost before its email went: %d emails, want none (it is closed)", len(got))
	}
}

// Stripe's trial_will_end notice: the owners hear once that the trial ends and the card will be charged.
func TestTrialEndingEmail(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	owner := w.mustSignIn(ana, "")
	ws := w.session(owner).Workspace.ID
	sub := w.f.sub("sub_t", ws, "trialing", "")
	w.f.set(w.env, sub, "trialing")
	w.received("Subject:")
	for range 2 {
		if code := w.stripeEvent("evt_trial_will_end", "customer.subscription.trial_will_end", sub, testWhsec); code != 200 {
			t.Fatalf("trial_will_end: %d", code)
		}
	}
	got := w.received("Subject: Your staqly free trial ends on")
	if len(got) != 1 || !strings.Contains(got[0], "<ana@example.com>") || !strings.Contains(got[0], "your card is charged for Starter") {
		t.Fatalf("trial ending: %d emails:\n%s", len(got), strings.Join(got, "\n---\n"))
	}
}
