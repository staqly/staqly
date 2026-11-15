package app

import (
	"context"
	"net/url"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Refunds (charge.refunded) and chargebacks (charge.dispute.created, .updated, .closed). Each event reads its charge
// or dispute again from Stripe, and every effect follows the state read, not the event: a lost dispute's
// subscription is cancelled while it still runs, a bundle's posts are only ever lowered. Replays and any order of
// delivery therefore end the same way.

type stripeDispute struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Charge string `json:"charge"`
}

func disputeOpen(status string) bool {
	return status != "won" && status != "lost" && status != "warning_closed"
}

func (s *Service) getCharge(ctx context.Context, id string) (ch stripeCharge, err error) {
	err = s.stripe(ctx, "GET", "/v1/charges/"+url.PathEscape(id), nil, &ch)
	return ch, err
}

func (s *Service) getDispute(ctx context.Context, id string) (d stripeDispute, err error) {
	err = s.stripe(ctx, "GET", "/v1/disputes/"+url.PathEscape(id), nil, &d)
	return d, err
}

// creditWorkspace: the workspace a bundle's Checkout session was for.
func creditWorkspace(cs creditSession) string {
	return firstNonEmpty(cs.Metadata["workspace_id"], cs.ClientReferenceID)
}

// paidFor: what a charge paid for in a workspace here: a bundle of extra posts (its Checkout session) or a
// subscription's invoice (the subscription's id). ws is "" for a payment of no workspace here, or of another customer
// than the workspace's.
func (s *Service) paidFor(ctx context.Context, ch stripeCharge) (ws, subID string, cs *creditSession, err error) {
	if cs, err = s.creditSessionOf(ctx, ch.PaymentIntent); err != nil {
		return "", "", nil, err
	}
	if cs == nil {
		if subID, err = s.chargeSubscription(ctx, ch.PaymentIntent); err != nil || subID == "" {
			return "", "", nil, err
		}
	}
	if cs != nil {
		ws = creditWorkspace(*cs)
	} else if ws, err = s.Q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(ch.Customer)); isNoRows(err) {
		return "", "", nil, nil
	}
	if err != nil || ws == "" {
		return "", "", nil, err
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, ws)
	if isNoRows(err) {
		return "", "", nil, nil
	}
	if err != nil {
		return "", "", nil, err
	}
	if ch.Customer != "" && w.StripeCustomerID.String != "" && w.StripeCustomerID.String != ch.Customer {
		s.logger(ctx).Warn("stripe: ignoring a charge of another customer", "charge", ch.ID, "workspace_id", ws)
		return "", "", nil, nil
	}
	return ws, subID, cs, nil
}

// chargeSubscription: the subscription whose invoice a payment paid, "" for none (Stripe API 2025-03-31 on: charges
// and invoices no longer name each other; the invoice payment links them).
func (s *Service) chargeSubscription(ctx context.Context, paymentIntent string) (string, error) {
	if paymentIntent == "" {
		return "", nil
	}
	var list struct {
		Data []struct {
			Invoice string `json:"invoice"`
		} `json:"data"`
	}
	q := url.Values{"payment[type]": {"payment_intent"}, "payment[payment_intent]": {paymentIntent}, "limit": {"1"}}
	if err := s.stripe(ctx, "GET", "/v1/invoice_payments?"+q.Encode(), nil, &list); err != nil {
		return "", err
	}
	if len(list.Data) == 0 || list.Data[0].Invoice == "" {
		return "", nil
	}
	var inv struct {
		Parent struct {
			SubscriptionDetails struct {
				Subscription string `json:"subscription"`
			} `json:"subscription_details"`
		} `json:"parent"`
	}
	if err := s.stripe(ctx, "GET", "/v1/invoices/"+url.PathEscape(list.Data[0].Invoice), nil, &inv); err != nil {
		return "", err
	}
	return inv.Parent.SubscriptionDetails.Subscription, nil
}

// refund follows charge.refunded. A bundle of extra posts loses the refunded share of its posts (revokeCredits). A
// subscription payment is refunded by the operator, who also decides whether it ends: that is only logged.
func (s *Service) refund(ctx context.Context, eventID, chargeID string) error {
	ch, err := s.getCharge(ctx, chargeID)
	if err != nil {
		return err
	}
	cs, err := s.creditSessionOf(ctx, ch.PaymentIntent)
	if err != nil {
		return err
	}
	if cs == nil {
		s.logger(ctx).Info("stripe: a payment was refunded; nothing changes", "charge", ch.ID,
			"refunded_cents", ch.AmountRefunded, "charge_cents", ch.Amount)
		return nil
	}
	expires := s.creditsExpiry(ctx, creditWorkspace(*cs))
	return s.tx(ctx, func(q *db.Queries) error {
		if n, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: eventID, CreatedAt: s.now()}); err != nil || n == 0 {
			return err
		}
		// A refund can arrive before Checkout's paid event: create its grant before lowering it.
		if err := s.grantCredits(ctx, q, *cs, expires); err != nil {
			return err
		}
		return s.revokeCredits(ctx, q, *cs, ch.Amount, ch.AmountRefunded, "charge.refunded")
	})
}

// dispute follows a chargeback. Opened: the owners and admins hear once and GET /v1/billing says disputed; the plan
// and the bought posts keep working. Lost: the subscription it paid ends now, or the bundle's posts that are left
// are taken back. Won or lost: the alert goes.
func (s *Service) dispute(ctx context.Context, eventID, id string) error {
	d, err := s.getDispute(ctx, id)
	if err != nil || d.Charge == "" {
		return err
	}
	ch, err := s.getCharge(ctx, d.Charge)
	if err != nil {
		return err
	}
	ws, subID, cs, err := s.paidFor(ctx, ch)
	if err != nil || ws == "" {
		return err
	}
	expires := ""
	if cs != nil {
		expires = s.creditsExpiry(ctx, ws)
	}
	unlock, err := s.lockBilling(ctx, ch.Customer)
	if err != nil {
		return err
	}
	defer unlock()
	// A dispute read before the lock may be older than another delivery's commit.
	if d, err = s.getDispute(ctx, id); err != nil {
		return err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		// Recorded, but not skipped when seen: every effect below follows the dispute as read.
		if _, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: eventID, CreatedAt: s.now()}); err != nil {
			return err
		}
		fresh, err := q.InsertBillingDispute(ctx, db.InsertBillingDisputeParams{ID: d.ID, WorkspaceID: ws, Status: d.Status, UpdatedAt: s.now()})
		if err != nil {
			return err
		}
		if err := q.SetBillingDisputeStatus(ctx, db.SetBillingDisputeStatusParams{Status: d.Status, UpdatedAt: s.now(), ID: d.ID}); err != nil {
			return err
		}
		if fresh > 0 && disputeOpen(d.Status) {
			if err := s.queueWelcome(ctx, q, ws, emailPaymentDisputed, ""); err != nil {
				return err
			}
		}
		if d.Status == "lost" && cs != nil {
			if err := s.grantCredits(ctx, q, *cs, expires); err != nil {
				return err
			}
			return s.revokeCredits(ctx, q, *cs, 1, 1, "charge.dispute.lost") // all that is left of it
		}
		return nil
	})
	if err != nil || d.Status != "lost" || subID == "" {
		return err
	}
	return s.cancelNow(ctx, subID)
}

// cancelNow ends a subscription at once after a lost dispute, unless it has ended, and applies it as a webhook would
// (under the caller's lockBilling). A failure is returned, so Stripe delivers the dispute again and this runs again.
func (s *Service) cancelNow(ctx context.Context, subID string) error {
	var sub stripeSubscription
	path := "/v1/subscriptions/" + url.PathEscape(subID)
	if err := s.stripe(ctx, "GET", path+subscriptionExpand, nil, &sub); err != nil {
		return err
	}
	if !finalSubscription(sub.Status) {
		s.logger(ctx).Warn("stripe: cancelling a subscription after a lost dispute", "subscription", subID)
		if err := s.stripe(ctx, "DELETE", path, nil, nil); err != nil {
			return err
		}
		if err := s.stripe(ctx, "GET", path+subscriptionExpand, nil, &sub); err != nil {
			return err
		}
	}
	return s.tx(ctx, func(q *db.Queries) error { return s.applySubscription(ctx, q, sub, "charge.dispute.closed") })
}
