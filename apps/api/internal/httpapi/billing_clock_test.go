package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The dunning clock at its edges. A paid period keeps the workspace open for three days past its end, to the
// second: writable in the last second of the grace, read-only (402 plan_expired) from the grace's end on. A
// renewal that wasn't paid counts the three days from where the paid period stopped, not from the period Stripe
// already rolled; paying opens the workspace again at once. A subscription that ends later never moves the end.
func TestGraceClockBoundaries(t *testing.T) {
	e := newEnv(t)
	withStripe(t, e)
	ws := workspaceID(e)
	admin := e.keyWith(app.ScopeAdmin)
	start, setClock := futureClock(e)
	const day = 24 * time.Hour
	grace := 3 * day
	n := 0
	// tell: Stripe's subscription in a status, its period starting at from, with its latest invoice.
	tell := func(status string, from time.Time, reason, invoice string) app.Billing {
		t.Helper()
		n++
		sub := subObject("sub_1", ws, status, "starter", "month")
		item := sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)
		item["current_period_start"], item["current_period_end"] = from.Unix(), from.Add(30*day).Unix()
		sub["latest_invoice"] = map[string]any{"id": fmt.Sprint("in_", n), "billing_reason": reason, "status": invoice}
		if status == "canceled" {
			sub["cancellation_details"] = map[string]string{"reason": "payment_failed"}
		}
		if code := e.stripeEvent(fmt.Sprint("evt_clock_", n), "customer.subscription.updated", sub, testWhsec); code != 200 {
			t.Fatalf("%s event: %d", status, code)
		}
		return e.billing(admin)
	}
	// at: the workspace at a moment: its plan's end as told, whether it is read-only, and whether a write goes through.
	at := func(step string, when, ends time.Time, open bool) {
		t.Helper()
		setClock(when)
		b := e.billing(admin)
		if b.PlanExpiresAt == nil {
			t.Fatalf("%s: no end date: %+v", step, b)
		}
		got, err := time.Parse(time.RFC3339, *b.PlanExpiresAt)
		if err != nil || !got.Equal(ends) || b.Suspended == open {
			t.Fatalf("%s at %s: the plan ends %s (want %s), suspended %v (want %v)", step, when.Format(time.RFC3339), *b.PlanExpiresAt, ends.Format(time.RFC3339), b.Suspended, !open)
		}
		want := 201
		if !open {
			want = 402
		}
		var errBody struct{ Error app.Error }
		e.do(nil, "POST", "/v1/collections", e.key, map[string]string{"name": step + when.Format(" 15:04:05")}, want, &errBody)
		if !open && errBody.Error.Code != "plan_expired" {
			t.Fatalf("%s: %+v", step, errBody.Error)
		}
		if err := e.svc.CheckWritable(context.Background(), ws); (err == nil) != open {
			t.Fatalf("%s: CheckWritable says %v", step, err)
		}
	}
	paidFrom := start.Add(-20 * day)
	periodEnd := paidFrom.Add(30 * day)
	tell("active", paidFrom, "subscription_cycle", "paid")
	at("a paid period, its last second", periodEnd.Add(-time.Second), periodEnd.Add(grace), true)
	at("the grace's last second", periodEnd.Add(grace-time.Second), periodEnd.Add(grace), true)
	at("the grace's end", periodEnd.Add(grace), periodEnd.Add(grace), false)
	at("a second later", periodEnd.Add(grace+time.Second), periodEnd.Add(grace), false)

	// Stripe rolls the period (still active, the invoice a draft) about an hour before it charges.
	setClock(periodEnd)
	tell("active", periodEnd, "subscription_cycle", "draft")
	at("the rolled period", periodEnd.Add(time.Minute), periodEnd.Add(30*day+grace), true)
	// The charge fails: three days from the paid period's end, not thirty-three.
	if b := tell("past_due", periodEnd, "subscription_cycle", "open"); b.EndedFor != nil {
		t.Fatalf("past_due is not an ended subscription: %+v", b)
	}
	at("unpaid renewal, the grace's last second", periodEnd.Add(grace-time.Second), periodEnd.Add(grace), true)
	at("unpaid renewal, the grace's end", periodEnd.Add(grace), periodEnd.Add(grace), false)
	// Stripe gives up a week later: the end stays where the grace stopped, and the reason is told.
	setClock(periodEnd.Add(7 * day))
	b := tell("canceled", periodEnd, "subscription_cycle", "open")
	if b.EndedFor == nil || *b.EndedFor != "payment_failed" {
		t.Fatalf("a subscription Stripe ended for non-payment: %+v", b)
	}
	at("ended for non-payment", periodEnd.Add(7*day), periodEnd.Add(grace), false)
}

// A subscription Stripe holds as unpaid (retries used up, "leave the subscription unpaid") ends the plan at once
// and says why, like one it cancelled for non-payment; one the customer cancelled says nothing.
func TestEndedForNonPayment(t *testing.T) {
	for _, c := range []struct {
		status, reason, want string
	}{
		{"unpaid", "", "payment_failed"},
		{"canceled", "payment_failed", "payment_failed"},
		{"canceled", "cancellation_requested", ""},
		{"paused", "", ""},
	} {
		e := newEnv(t)
		f := withStripe(t, e)
		f.set("GET /v1/invoices", func() (int, any) { return 200, map[string]any{"data": []any{}} })
		ws := workspaceID(e)
		admin := e.keyWith(app.ScopeAdmin)
		e.stripeEvent("evt_1", "customer.subscription.created", subObject("sub_1", ws, "active", "starter", "month"), testWhsec)
		sub := subObject("sub_1", ws, c.status, "starter", "month")
		sub["cancellation_details"] = map[string]string{"reason": c.reason}
		e.stripeEvent("evt_2", "customer.subscription.updated", sub, testWhsec)
		b, why := e.billing(admin), ""
		if b.EndedFor != nil {
			why = *b.EndedFor
		}
		if !b.Suspended || why != c.want {
			t.Fatalf("%s (%s): suspended %v, ended_for %q, want %q", c.status, c.reason, b.Suspended, why, c.want)
		}
	}
}

// A subscription without an item can't be switched from staqly: 409, nothing is sent to Stripe.
func TestSwitchOfSubscriptionWithoutItems(t *testing.T) {
	e := newEnv(t)
	f := withStripe(t, e)
	ws := workspaceID(e)
	empty := map[string]any{"id": "sub_1", "customer": "cus_1", "status": "active", "metadata": map[string]string{"workspace_id": ws},
		"items": map[string]any{"data": []any{}}}
	e.stripeEvent("evt_1", "customer.subscription.created", empty, testWhsec)
	f.set("GET /v1/subscriptions/sub_1", func() (int, any) { return 200, empty })
	var errBody struct{ Error app.Error }
	e.pay("POST", "/v1/billing/checkout", map[string]string{"plan": "pro"}, 409, &errBody)
	if errBody.Error.Code != "conflict" || f.called("POST /v1/subscriptions/sub_1") != 0 || f.called("POST /v1/checkout/sessions") != 0 {
		t.Fatalf("a subscription without items: %+v, Stripe asked %v", errBody.Error, f.calls)
	}
}
