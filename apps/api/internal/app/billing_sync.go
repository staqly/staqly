package app

import (
	"context"
	"net/url"
	"slices"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Webhooks are how Stripe tells staqly about a subscription, but one can be lost (the server down past Stripe's
// retries, a misconfigured endpoint) or simply not be in yet when someone comes back from Checkout. Two ways
// around that, both reading the subscription as Stripe has it now and applying it like the webhook would:
// SyncBilling when the dashboard comes back from Checkout, and the billing-sync worker every few minutes.

const billingSyncEvery = 10 * time.Minute

// syncSubscription reads the customer's subscription from Stripe and applies it to the workspace: the newest live
// one, else the newest (a cancelled duplicate is newer than the one that was kept: settleDuplicate).
func (s *Service) syncSubscription(ctx context.Context, customer string) error {
	var list struct {
		Data []stripeSubscription `json:"data"`
	}
	// Read and applied under the customer's lock: see lockBilling.
	unlock, err := s.lockBilling(ctx, customer)
	if err != nil {
		return err
	}
	defer unlock()
	q := url.Values{"customer": {customer}, "status": {"all"}, "limit": {"10"}, "expand[]": {"data.default_payment_method", "data.latest_invoice"}}
	if err := s.stripe(ctx, "GET", "/v1/subscriptions?"+q.Encode(), nil, &list); err != nil {
		return err
	}
	if len(list.Data) == 0 {
		return nil // no subscription yet: a checkout not finished
	}
	sub := list.Data[0]
	if i := slices.IndexFunc(list.Data, func(s stripeSubscription) bool { return liveSubscription(s.Status) }); i >= 0 {
		sub = list.Data[i]
	}
	if err := s.tx(ctx, func(q *db.Queries) error { return s.applySubscription(ctx, q, sub, "billing.sync") }); err != nil {
		return err
	}
	if err := s.settleDuplicate(ctx, sub); err != nil {
		return err
	}
	return s.guardCard(ctx, sub)
}

// SyncBilling asks Stripe for the workspace's subscription now (the dashboard calls it when it comes back from
// Checkout), then answers like GetBilling.
func (s *Service) SyncBilling(ctx context.Context, p Principal) (Billing, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return Billing{}, err
	}
	if !s.BillingEnabled() {
		return s.GetBilling(ctx, p)
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return Billing{}, err
	}
	if w.StripeCustomerID.Valid && w.StripeCustomerID.String != "" {
		if err := s.syncSubscription(ctx, w.StripeCustomerID.String); err != nil {
			return Billing{}, err
		}
	}
	return s.GetBilling(ctx, p)
}

// ReconcileBilling syncs every workspace whose plan may be out of date: a customer with no subscription on record,
// or a plan whose period has ended. One that fails is tried again on the next round.
// ponytail: one Stripe request per such workspace per round (500 at most); a customer who never finished a
// checkout is asked every round. Narrow it by the customer's age if that ever adds up.
func (s *Service) ReconcileBilling(ctx context.Context) (synced int) {
	rows, err := s.Q.ListBillingToReconcile(ctx, nullStr(s.now()))
	if err != nil {
		s.logger(ctx).Error("billing sync: list failed", "err", err)
		return 0
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			return synced
		}
		if err := s.syncSubscription(ctx, r.StripeCustomerID.String); err != nil {
			s.logger(ctx).Warn("billing sync failed", "workspace_id", r.ID, "err", err)
			continue
		}
		synced++
	}
	return synced
}

func init() {
	RegisterWorker(BackgroundWorker{Name: "billing-sync", Run: func(ctx context.Context, s *Service) {
		wait := 2 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = billingSyncEvery
			if !s.BillingEnabled() {
				continue
			}
			if n := s.ReconcileBilling(ctx); n > 0 {
				s.logger(ctx).Info("billing sync", "workspaces", n)
			}
		}
	}})
}
