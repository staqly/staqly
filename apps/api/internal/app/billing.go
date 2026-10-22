package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// BillingConfig: Stripe settings (cloud mode). Billing is on when SecretKey and WebhookSecret are set.
//
// Each plan and interval is one recurring Stripe price. The server finds it by its lookup key,
// staqly_<plan>_<interval> (e.g. staqly_plus_year), so creating the prices with those lookup keys is all the
// setup it needs; Prices overrides a lookup with a price id (STRIPE_PRICE_<PLAN>_<MONTHLY|YEARLY>).
type BillingConfig struct {
	SecretKey     string            // STRIPE_SECRET_KEY (sk_test_... or sk_live_...)
	WebhookSecret string            // STRIPE_WEBHOOK_SECRET (whsec_...)
	Prices        map[string]string // "<plan>_<interval>" -> price id, from STRIPE_PRICE_<PLAN>_<MONTHLY|YEARLY>
	// ManagedPayments (STRIPE_MANAGED_PAYMENTS): checkouts use Stripe Managed Payments, so Stripe (through Link)
	// is the merchant of record and handles tax, invoices to the customer, fraud and disputes. The account must
	// have it enabled in the Dashboard and the products need an eligible tax code (txcd_10103001, SaaS).
	ManagedPayments bool
	APIBase         string // tests only; default https://api.stripe.com
}

// managedCheckout turns a Checkout Session form into a Managed Payments one when that is on: Stripe then
// collects the name, address and tax, so the parameters for doing that ourselves are refused and go.
func (s *Service) managedCheckout(form url.Values) {
	if !s.Cfg.Billing.ManagedPayments {
		return
	}
	for _, k := range []string{"automatic_tax[enabled]", "tax_id_collection[enabled]", "customer_update[name]",
		"customer_update[address]", "invoice_creation[enabled]"} {
		form.Del(k)
	}
	form.Set("managed_payments[enabled]", "true")
}

// PriceLookupKey is the Stripe lookup key of a plan's price in an interval (month or year).
func PriceLookupKey(plan, interval string) string { return "staqly_" + plan + "_" + interval }

// priceIDs caches lookup key (or pinned price id) -> the price's id and amount for 10 minutes (a price never
// changes its amount; a new amount is a new price that takes the key over).
var priceIDs = struct {
	sync.Mutex
	m map[string]priceCacheEntry
}{m: map[string]priceCacheEntry{}}

type priceCacheEntry struct {
	id       string
	cents    int64  // unit amount per seat, in cents of currency
	currency string // e.g. eur
	err      error  // a price Stripe doesn't have, or a failed lookup (kept a minute, so pages don't hammer Stripe)
	exp      time.Time
}

// priceCacheTTL: how long a looked-up price (id and amount) is reused.
const priceCacheTTL = 10 * time.Minute

// billingGrace keeps a paid plan active a little past the paid period, so a renewal
// paid a few hours late does not suspend the workspace in between.
const billingGrace = 3 * 24 * time.Hour

// stripeSignatureTolerance: maximum age of a webhook signature.
const stripeSignatureTolerance = 5 * time.Minute

func (s *Service) BillingEnabled() bool {
	b := s.Cfg.Billing
	return s.Cfg.Mode == ModeCloud && b.SecretKey != "" && b.WebhookSecret != ""
}

// checkoutPlan validates the plan and interval a checkout or a switch asks for.
func checkoutPlan(planID, interval string) (Plan, string, error) {
	if planID == "" {
		planID = "pro"
	}
	if interval == "" {
		interval = IntervalMonth
	}
	p, ok := PlanByID(planID)
	if !ok || p.Hidden {
		return Plan{}, "", invalid("plan", "unknown plan: use starter, plus, pro or business")
	}
	if interval != IntervalMonth && interval != IntervalYear {
		return Plan{}, "", invalid("interval", "use month or year")
	}
	return p, interval, nil
}

// priceID: the Stripe price of a plan in an interval, from STRIPE_PRICE_* or found by its lookup key.
func (s *Service) priceID(ctx context.Context, plan Plan, interval string) (string, error) {
	pr, err := s.livePrice(ctx, plan, interval)
	if pinned := s.Cfg.Billing.Prices[plan.ID+"_"+interval]; err != nil && pinned != "" {
		// Couldn't read the pinned price: try it anyway (a missing one falls back in unpinnedPrice).
		return pinned, nil
	}
	return pr.id, err
}

// livePrice: the Stripe price (id and per-seat amount) a plan is charged at in an interval, cached. A pinned
// STRIPE_PRICE_* id is read by id; otherwise the active price holding the lookup key.
func (s *Service) livePrice(ctx context.Context, plan Plan, interval string) (priceCacheEntry, error) {
	return s.lookupPrice(ctx, PriceLookupKey(plan.ID, interval), s.Cfg.Billing.Prices[plan.ID+"_"+interval],
		fmt.Sprintf("the %s plan billed %sly is not set up for payment on this instance", plan.Name, interval))
}

// lookupPrice: a Stripe price (id and unit amount) by lookup key, or a pinned id, cached; missing says what
// isn't set up.
func (s *Service) lookupPrice(ctx context.Context, key, pinned, missing string) (priceCacheEntry, error) {
	cacheKey := s.Cfg.Billing.APIBase + "|" + s.Cfg.Billing.SecretKey + "|" + key + "|" + pinned // one Stripe account's prices
	priceIDs.Lock()
	c, ok := priceIDs.m[cacheKey]
	priceIDs.Unlock()
	if ok && s.Now().Before(c.exp) {
		return c, c.err
	}
	remember := func(c priceCacheEntry) (priceCacheEntry, error) {
		// A request that was canceled or timed out says nothing about Stripe: the next caller asks again.
		if c.err != nil && ctx.Err() != nil {
			return c, c.err
		}
		priceIDs.Lock()
		priceIDs.m[cacheKey] = c
		priceIDs.Unlock()
		return c, c.err
	}
	type price struct {
		ID         string `json:"id"`
		Active     *bool  `json:"active"`
		UnitAmount int64  `json:"unit_amount"`
		Currency   string `json:"currency"`
	}
	var found price
	if pinned != "" {
		if err := s.stripe(ctx, "GET", "/v1/prices/"+url.PathEscape(pinned), nil, &found); err != nil {
			return remember(priceCacheEntry{err: err, exp: s.Now().Add(time.Minute)})
		}
		found.ID = pinned
		if found.Active != nil && !*found.Active {
			// Archived: Checkout refuses inactive prices, so the lookup key's active price stands in.
			s.logger(ctx).Warn("billing: STRIPE_PRICE_* names an archived price; used the lookup key instead (remove the setting)",
				"key", key, "price", pinned)
			pinned = ""
		}
	}
	if pinned == "" {
		var list struct {
			Data []price `json:"data"`
		}
		if err := s.stripe(ctx, "GET", "/v1/prices?active=true&limit=1&lookup_keys[]="+url.QueryEscape(key), nil, &list); err != nil {
			return remember(priceCacheEntry{err: err, exp: s.Now().Add(time.Minute)})
		}
		if len(list.Data) == 0 {
			return remember(priceCacheEntry{exp: s.Now().Add(time.Minute), err: &Error{Code: "not_available", Status: 409, Message: missing}})
		}
		found = list.Data[0]
	}
	return remember(priceCacheEntry{id: found.ID, cents: found.UnitAmount, currency: found.Currency, exp: s.Now().Add(priceCacheTTL)})
}

// withLivePrices: a plan with the amounts Stripe actually charges (per seat, monthly and yearly) when billing
// is on, so what people read is what they pay. Without billing, or when Stripe can't be reached or a price is
// missing, not in whole units or in another currency, the plan keeps its amounts from Plans (and the reason is logged).
func (s *Service) withLivePrices(ctx context.Context, p Plan) Plan {
	if !s.BillingEnabled() || p.Hidden {
		return p
	}
	if _, ok := PlanByID(p.ID); !ok {
		return p
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, iv := range []string{IntervalMonth, IntervalYear} {
		pr, err := s.livePrice(ctx, p, iv)
		if err != nil || pr.cents <= 0 || pr.cents%100 != 0 || (pr.currency != "" && pr.currency != Currency) {
			s.logger(ctx).Warn("billing: showing the built-in price", "plan", p.ID, "interval", iv, "err", err, "cents", pr.cents, "currency", pr.currency)
			continue
		}
		if iv == IntervalMonth {
			p.PriceMonth = int(pr.cents / 100)
		} else {
			p.PriceYear = int(pr.cents / 100)
		}
	}
	return p
}

// LivePlans: the offered plans with their charged amounts (see withLivePrices); GET /plans, MCP, the dashboard.
func (s *Service) LivePlans(ctx context.Context) []Plan {
	out := OfferedPlans()
	for i := range out {
		out[i] = s.withLivePrices(ctx, out[i])
	}
	return out
}

// stripePrice is the part of a Stripe price staqly reads to know which plan and interval was bought.
type stripePrice struct {
	ID        string            `json:"id"`
	LookupKey string            `json:"lookup_key"`
	Metadata  map[string]string `json:"metadata"`
	Recurring struct {
		Interval string `json:"interval"`
	} `json:"recurring"`
}

// planOf maps a price back to its plan: the staqly_plan metadata, the lookup key, or a STRIPE_PRICE_* id.
func (s *Service) planOf(pr stripePrice) (string, bool) {
	if id := pr.Metadata["staqly_plan"]; id != "" {
		if p, ok := PlanByID(id); ok {
			return p.ID, true
		}
	}
	if rest, ok := strings.CutPrefix(pr.LookupKey, "staqly_"); ok {
		for _, iv := range []string{IntervalMonth, IntervalYear} {
			if id, ok := strings.CutSuffix(rest, "_"+iv); ok {
				if p, ok := PlanByID(id); ok {
					return p.ID, true
				}
			}
		}
	}
	for k, id := range s.Cfg.Billing.Prices {
		if id == pr.ID && pr.ID != "" {
			planID, _, _ := strings.Cut(k, "_")
			return planID, true
		}
	}
	return "", false
}

var errBillingOff = &Error{Code: "not_available", Status: 409, Message: "billing is not enabled on this instance"}

// Billing is the workspace's subscription as staqly knows it.
type Billing struct {
	Enabled bool   `json:"enabled"`
	Plan    string `json:"plan"`
	// SubscriptionStatus: Stripe's status (active, trialing, past_due,
	// canceled, unpaid, incomplete, ...); null when never subscribed.
	SubscriptionStatus *string `json:"subscription_status"`
	// PlanExpiresAt: end of the trial or paid period (null = no end).
	PlanExpiresAt *string `json:"plan_expires_at"`
	// TrialEndsAt: when the free trial ends (null = not in one); see trialEndsAt.
	TrialEndsAt *string `json:"trial_ends_at"`
	Suspended   bool    `json:"suspended"`
	HasCustomer bool    `json:"has_customer"`
	// Seats: the people billed, every workspace member whatever their role (open invitations are billed once
	// accepted). The subscription's quantity follows it.
	Seats int64 `json:"seats"`
	// Trial: whether choosing Starter now starts its 7-day trial: "available"; "used" (this workspace had a
	// subscription before); "other_workspace" (another workspace of the account is in its trial); "none"
	// (billing is off, or the workspace already pays).
	Trial string `json:"trial"`
}

// Trial availability (Billing.Trial).
const (
	TrialAvailable      = "available"
	TrialUsed           = "used"
	TrialOtherWorkspace = "other_workspace"
	TrialNone           = "none"
)

// trialOffer: Starter's 7-day Stripe trial (card required) goes to each workspace once (one that ever had a
// subscription doesn't get it again), and to one workspace per person at a time: a workspace whose owner already
// trials another one pays from the first day. Workspaces without people (local mode) have no others.
func (s *Service) trialOffer(ctx context.Context, q *db.Queries, w db.GetWorkspaceBillingRow) (string, error) {
	switch {
	case !s.BillingEnabled() || (w.SubscriptionStatus.Valid && liveSubscription(w.SubscriptionStatus.String)):
		return TrialNone, nil
	case w.StripeSubscriptionID.Valid && w.StripeSubscriptionID.String != "":
		return TrialUsed, nil
	}
	n, err := q.CountOwnersOtherTrials(ctx, w.ID)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return TrialOtherWorkspace, nil
	}
	return TrialAvailable, nil
}

// trialEndsAt: when the workspace's free trial ends, nil when it is in none. Stripe's trial (status trialing):
// Stripe's trial_end, which is the trial period's end; plan_expires_at carries billingGrace on top of it (see
// applySubscription), so counting down to plan_expires_at would promise 3 days too many. Without billing: the
// server-side trial (no subscription, an end still ahead), whose end is plan_expires_at itself.
func (s *Service) trialEndsAt(w db.GetWorkspaceBillingRow) *string {
	if s.Cfg.Mode == ModeLocal || !w.PlanExpiresAt.Valid || w.PlanExpiresAt.String == "" {
		return nil
	}
	if w.SubscriptionStatus.Valid {
		if w.SubscriptionStatus.String != "trialing" {
			return nil
		}
		t, err := time.Parse(timeFormat, w.PlanExpiresAt.String)
		if err != nil {
			return nil
		}
		v := formatTime(t.Add(-billingGrace))
		return &v
	}
	if s.BillingEnabled() || w.PlanExpiresAt.String <= s.now() {
		return nil
	}
	v := w.PlanExpiresAt.String
	return &v
}

func (s *Service) GetBilling(ctx context.Context, p Principal) (Billing, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return Billing{}, err
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return Billing{}, err
	}
	_, _, suspended, err := s.planState(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Billing{}, err
	}
	seats, err := s.seatCount(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Billing{}, err
	}
	trial, err := s.trialOffer(ctx, s.Q, w)
	if err != nil {
		return Billing{}, err
	}
	b := Billing{Enabled: s.BillingEnabled(), Plan: w.Plan, Suspended: suspended, HasCustomer: w.StripeCustomerID.Valid, Seats: seats, Trial: trial}
	if w.SubscriptionStatus.Valid {
		b.SubscriptionStatus = &w.SubscriptionStatus.String
	}
	if w.PlanExpiresAt.Valid {
		b.PlanExpiresAt = &w.PlanExpiresAt.String
	}
	b.TrialEndsAt = s.trialEndsAt(w)
	if s.Cfg.Mode == ModeLocal {
		b.Plan = UnlockedPlan.ID
	}
	return b, nil
}

// CheckoutSession starts a Stripe Checkout for a plan billed monthly or yearly and returns its URL. A workspace
// that already pays switches its subscription to that price instead (Stripe prorates the difference) and gets
// the billing page back.
// CheckoutOptions: where Stripe sends the person back (`welcome`: the dashboard's welcome steps) and a promotion
// code typed in staqly (a switch of a live subscription never opens Checkout, whose own code box needs none).
type CheckoutOptions struct {
	ReturnTo      string
	PromotionCode string
}

func (s *Service) CheckoutSession(ctx context.Context, p Principal, planID, interval string, opts ...CheckoutOptions) (string, error) {
	var opt CheckoutOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if err := s.requireOwner(ctx, p); err != nil {
		return "", err
	}
	if !s.BillingEnabled() {
		return "", errBillingOff
	}
	plan, interval, err := checkoutPlan(planID, interval)
	if err != nil {
		return "", err
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return "", err
	}
	price, err := s.priceID(ctx, plan, interval)
	if err != nil {
		return "", err
	}
	promo := ""
	if code := strings.TrimSpace(opt.PromotionCode); code != "" {
		promo, err = s.promotionCodeID(ctx, code, plan, price)
		if found, ok := s.unpinnedPrice(ctx, plan, interval, err); ok { // a stale STRIPE_PRICE_* (as below)
			price = found
			promo, err = s.promotionCodeID(ctx, code, plan, price)
		}
		if err != nil {
			return "", err
		}
	}
	if w.SubscriptionStatus.Valid && liveSubscription(w.SubscriptionStatus.String) && w.StripeSubscriptionID.Valid {
		err := s.switchSubscription(ctx, w.ID, w.StripeSubscriptionID.String, price, plan.ID, interval, promo)
		if found, ok := s.unpinnedPrice(ctx, plan, interval, err); ok {
			err = s.switchSubscription(ctx, w.ID, w.StripeSubscriptionID.String, found, plan.ID, interval, promo)
		}
		if err != nil {
			return "", err
		}
		return s.planPageURL() + "?checkout=switched", nil
	}
	customer, err := s.ensureCustomer(ctx, w)
	if err != nil {
		return "", err
	}
	seats, err := s.seatCount(ctx, s.Q, w.ID)
	if err != nil {
		return "", err
	}
	back := s.planPageURL()
	success, cancel := back+"?checkout=success", back+"?checkout=canceled"
	if opt.ReturnTo == "welcome" { // started from the welcome steps: back to them
		welcome := strings.TrimRight(firstNonEmpty(s.Cfg.PublicDashboardURL, s.Cfg.PublicURL), "/") + "/welcome"
		success, cancel = welcome+"?step=done&checkout=success", welcome+"?step=plan"
	}
	var cs struct {
		URL string `json:"url"`
	}
	form := url.Values{
		"mode":                    {"subscription"},
		"customer":                {customer},
		"client_reference_id":     {w.ID},
		"line_items[0][price]":    {price},
		"line_items[0][quantity]": {strconv.FormatInt(seats, 10)}, // one seat per member
		"subscription_data[metadata][workspace_id]":    {w.ID},
		"subscription_data[metadata][staqly_plan]":     {plan.ID},
		"subscription_data[metadata][staqly_interval]": {interval},
		// The tax ID on the invoice (saved on the customer).
		"tax_id_collection[enabled]": {"true"},
		"customer_update[name]":      {"auto"},
		"customer_update[address]":   {"auto"},
		"success_url":                {success},
		"cancel_url":                 {cancel},
	}
	// Discount codes made in Stripe (Coupons, then a promotion code): the one typed in staqly, else Checkout's
	// own code box (Stripe takes one or the other).
	if promo != "" {
		form.Set("discounts[0][promotion_code]", promo)
	} else {
		form.Set("allow_promotion_codes", "true")
	}
	// Only Starter has a trial, Stripe's own: 7 days with a card required, cancelled if no card is on file when
	// it ends; trialOffer says who gets it (once per workspace, one at a time per account). Plus, Pro and
	// Business charge at once.
	trial, err := s.trialOffer(ctx, s.Q, w)
	if err != nil {
		return "", err
	}
	if plan.ID == TrialPlan && trial == TrialAvailable {
		form.Set("subscription_data[trial_period_days]", strconv.Itoa(TrialDays))
		form.Set("subscription_data[trial_settings][end_behavior][missing_payment_method]", "cancel")
	}
	// Business customers: tax by Stripe Tax on prices that exclude it, from the billing address and tax ID.
	form.Set("payment_method_collection", "always")
	form.Set("automatic_tax[enabled]", "true")
	form.Set("billing_address_collection", "required")
	s.managedCheckout(form)
	err = s.stripe(ctx, "POST", "/v1/checkout/sessions", form, &cs)
	if found, ok := s.unpinnedPrice(ctx, plan, interval, err); ok {
		form.Set("line_items[0][price]", found)
		err = s.stripe(ctx, "POST", "/v1/checkout/sessions", form, &cs)
	}
	return cs.URL, err
}

// unpinnedPrice: when Stripe doesn't know a price pinned with STRIPE_PRICE_* (archived, from another account
// or mode), the plan's price by its lookup key, to retry once with.
func (s *Service) unpinnedPrice(ctx context.Context, plan Plan, interval string, err error) (string, bool) {
	var e *Error
	if !errors.As(err, &e) || s.Cfg.Billing.Prices[plan.ID+"_"+interval] == "" {
		return "", false
	}
	// resource_missing: a price Stripe doesn't have; any other refusal of the price parameter: one it won't
	// take (archived since it was read and cached).
	if param, _ := e.Details["provider_param"].(string); e.Details["provider_code"] != "resource_missing" && !strings.HasSuffix(param, "[price]") {
		return "", false
	}
	pr, lerr := s.lookupPrice(ctx, PriceLookupKey(plan.ID, interval), "", "")
	if lerr != nil || pr.id == "" {
		return "", false
	}
	s.logger(ctx).Warn("billing: STRIPE_PRICE_* names a price Stripe doesn't have; used the lookup key instead (remove the setting)",
		"plan", plan.ID, "interval", interval, "price", pr.id)
	return pr.id, true
}

// customerDetails: who a workspace's Stripe customer is, so it can be found in Stripe: the workspace (its name as
// the business, its id in the description and metadata) and the person who pays, its first owner (their name and
// the email they sign in with: Google or an email code). Checkout may still change `name` to the cardholder's.
func (s *Service) customerDetails(ctx context.Context, w db.GetWorkspaceBillingRow) url.Values {
	v := url.Values{
		"name":                   {w.Name},
		"business_name":          {w.Name},
		"description":            {"staqly workspace " + w.ID},
		"metadata[workspace_id]": {w.ID},
	}
	// The customer's contact: the workspace's first owner.
	if members, err := s.Q.ListMembers(ctx, w.ID); err == nil {
		for _, m := range members {
			if m.Role != RoleOwner {
				continue
			}
			if m.Email != "" {
				v.Set("email", m.Email)
			}
			if m.Name != "" && !strings.Contains(m.Name, "@") {
				v.Set("individual_name", m.Name)
			}
			break
		}
	}
	return v
}

// ensureCustomer: the workspace's Stripe customer, made on its first checkout and brought up to date (workspace,
// payer's name and email) on every later one.
func (s *Service) ensureCustomer(ctx context.Context, w db.GetWorkspaceBillingRow) (string, error) {
	details := s.customerDetails(ctx, w)
	if w.StripeCustomerID.Valid && w.StripeCustomerID.String != "" {
		// Best effort: a customer that can't be updated still checks out.
		if err := s.stripe(ctx, "POST", "/v1/customers/"+url.PathEscape(w.StripeCustomerID.String), details, nil); err != nil {
			s.logger(ctx).Warn("billing: customer details not updated", "err", err)
		}
		return w.StripeCustomerID.String, nil
	}
	var c struct {
		ID string `json:"id"`
	}
	// Concurrent first checkouts (a double click, two tabs) get the same customer back: Stripe answers a repeated
	// idempotency key (24 hours) with the first result. The key covers the parameters, as Stripe refuses a key
	// reused with others (a renamed workspace, another email).
	sum := sha256.Sum256([]byte(details.Encode()))
	key := "staqly-customer-" + w.ID + "-" + hex.EncodeToString(sum[:8])
	if err := s.stripeKeyed(ctx, "POST", "/v1/customers", details, &c, key); err != nil {
		return "", err
	}
	return c.ID, s.W.SetStripeCustomer(ctx, db.SetStripeCustomerParams{StripeCustomerID: nullStr(c.ID), UpdatedAt: s.now(), ID: w.ID})
}

// switchSubscription moves a live subscription to another price (plan or interval); Stripe prorates.
// The plan changes here when Stripe's subscription.updated event arrives.
// With a promotion code (its id), the code's discount replaces the subscription's; with the same price only the
// code is applied.
func (s *Service) switchSubscription(ctx context.Context, workspaceID, subscriptionID, price, planID, interval, promo string) error {
	var sub stripeSubscription
	if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(subscriptionID), nil, &sub); err != nil {
		return err
	}
	i, ok := s.planItem(sub)
	if !ok {
		return &Error{Code: "conflict", Status: 409, Message: "the subscription has no item to change; use the billing portal"}
	}
	if sub.Items.Data[i].Price.ID == price {
		if promo == "" {
			return &Error{Code: "conflict", Status: 409, Message: "the workspace is already on this plan and interval"}
		}
		return s.stripe(ctx, "POST", "/v1/subscriptions/"+url.PathEscape(subscriptionID),
			url.Values{"discounts[0][promotion_code]": {promo}}, nil)
	}
	seats, err := s.seatCount(ctx, s.Q, workspaceID)
	if err != nil {
		return err
	}
	form := url.Values{
		"items[0][id]":              {sub.Items.Data[i].ID},
		"items[0][price]":           {price},
		"items[0][quantity]":        {strconv.FormatInt(seats, 10)},
		"proration_behavior":        {"create_prorations"},
		"metadata[staqly_plan]":     {planID},
		"metadata[staqly_interval]": {interval},
	}
	if promo != "" {
		form.Set("discounts[0][promotion_code]", promo)
	}
	// Leaving the trial for another plan: the trial ends now and the new plan is charged at once; the change
	// waits for that payment (a pending update), so a card that fails leaves the trial as it was.
	trial := sub.Status == "trialing"
	if trial {
		form.Set("trial_end", "now")
		form.Set("proration_behavior", "always_invoice")
		form.Set("payment_behavior", "pending_if_incomplete")
	}
	var out struct {
		PendingUpdate json.RawMessage `json:"pending_update"`
	}
	if err := s.stripe(ctx, "POST", "/v1/subscriptions/"+url.PathEscape(subscriptionID), form, &out); err != nil {
		return err
	}
	if trial && len(out.PendingUpdate) > 0 && string(out.PendingUpdate) != "null" {
		return &Error{Code: "payment_failed", Status: 402, Message: "the payment for the new plan didn't go through, so the trial stays as it was: check the card in Manage billing and try again"}
	}
	return nil
}

// promotionCodeID: the id of the active promotion code `code` (as typed), checked against the plan the person
// picked: a code limited to other plans is refused here rather than giving nothing at Stripe.
func (s *Service) promotionCodeID(ctx context.Context, code string, plan Plan, price string) (string, error) {
	notValid := invalid("promotion_code", "this code isn't valid (or has been used up)")
	var list struct {
		Data []struct {
			ID     string          `json:"id"`
			Coupon json.RawMessage `json:"coupon"` // before API 2025-09: the coupon (an object)
			// From API 2025-09: promotion.coupon (an id).
			Promotion struct {
				Coupon string `json:"coupon"`
			} `json:"promotion"`
		} `json:"data"`
	}
	q := url.Values{"code": {code}, "active": {"true"}, "limit": {"1"}}
	if err := s.stripe(ctx, "GET", "/v1/promotion_codes?"+q.Encode(), nil, &list); err != nil {
		return "", err
	}
	if len(list.Data) == 0 {
		return "", notValid
	}
	pc := list.Data[0]
	couponID := pc.Promotion.Coupon
	if couponID == "" {
		var c struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(pc.Coupon, &c) == nil {
			couponID = c.ID
		}
	}
	if couponID == "" {
		return pc.ID, nil
	}
	var coupon struct {
		Valid     bool `json:"valid"`
		AppliesTo *struct {
			Products []string `json:"products"`
		} `json:"applies_to"`
	}
	if err := s.stripe(ctx, "GET", "/v1/coupons/"+url.PathEscape(couponID)+"?expand[]=applies_to", nil, &coupon); err != nil {
		return "", err
	}
	if !coupon.Valid {
		return "", notValid
	}
	if coupon.AppliesTo != nil && len(coupon.AppliesTo.Products) > 0 {
		var pr struct {
			Product string `json:"product"`
		}
		if err := s.stripe(ctx, "GET", "/v1/prices/"+url.PathEscape(price), nil, &pr); err != nil {
			return "", err
		}
		if !slices.Contains(coupon.AppliesTo.Products, pr.Product) {
			return "", invalid("promotion_code", fmt.Sprintf("this code doesn't apply to the %s plan", plan.Name))
		}
	}
	return pc.ID, nil
}

// PortalSession opens the Stripe customer portal (payment method, invoices,
// cancel) and returns its URL.
func (s *Service) PortalSession(ctx context.Context, p Principal) (string, error) {
	if err := s.requireOwner(ctx, p); err != nil {
		return "", err
	}
	if !s.BillingEnabled() {
		return "", errBillingOff
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return "", err
	}
	if !w.StripeCustomerID.Valid {
		return "", &Error{Code: "conflict", Status: 409, Message: "no billing account yet; start a checkout first"}
	}
	var ps struct {
		URL string `json:"url"`
	}
	err = s.stripe(ctx, "POST", "/v1/billing_portal/sessions", url.Values{"customer": {w.StripeCustomerID.String},
		"return_url": {s.planPageURL()}}, &ps)
	return ps.URL, err
}

func (s *Service) planPageURL() string {
	return strings.TrimRight(firstNonEmpty(s.Cfg.PublicDashboardURL, s.Cfg.PublicURL), "/") + "/settings/plan"
}

// liveSubscription: statuses in which the customer still has (or is about to
// have) a paid plan.
// cancellableSubscription: any status but the two that are over for good.
func cancellableSubscription(status string) bool {
	return status != "" && status != "canceled" && status != "incomplete_expired"
}

func liveSubscription(status string) bool {
	switch status {
	case "active", "trialing", "past_due", "incomplete":
		return true
	}
	return false
}

// finalSubscription: statuses a Stripe subscription never leaves (paying again makes a new subscription).
func finalSubscription(status string) bool {
	return status == "canceled" || status == "incomplete_expired"
}

// stripeSubscription is the part of a Stripe subscription staqly reads.
// current_period_end moved to the items in newer API versions; both are read.
type stripeSubscription struct {
	ID                 string            `json:"id"`
	Customer           string            `json:"customer"`
	Status             string            `json:"status"`
	CurrentPeriodStart int64             `json:"current_period_start"`
	CurrentPeriodEnd   int64             `json:"current_period_end"`
	EndedAt            int64             `json:"ended_at"`
	TrialEnd           int64             `json:"trial_end"`
	Metadata           map[string]string `json:"metadata"`
	Items              struct {
		Data []struct {
			ID                 string      `json:"id"`
			Quantity           int64       `json:"quantity"`
			CurrentPeriodStart int64       `json:"current_period_start"`
			CurrentPeriodEnd   int64       `json:"current_period_end"`
			Price              stripePrice `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

// plan: the plan the subscription's price stands for ("" when it maps to none).
func (s *Service) planOfSubscription(sub stripeSubscription) string {
	for _, it := range sub.Items.Data {
		if id, ok := s.planOf(it.Price); ok {
			return id
		}
	}
	if id := sub.Metadata["staqly_plan"]; id != "" {
		if _, ok := PlanByID(id); ok {
			return id
		}
	}
	return ""
}

// planItem: the subscription item that bills the plan per seat (add-ons are items of their own); the first
// item when none maps to a plan (a price staqly can't place).
func (s *Service) planItem(sub stripeSubscription) (int, bool) {
	if len(sub.Items.Data) == 0 {
		return 0, false
	}
	for i, it := range sub.Items.Data {
		if _, ok := s.planOf(it.Price); ok {
			return i, true
		}
	}
	for i, it := range sub.Items.Data {
		if it.Price.Metadata["staqly_addon"] == "" && !strings.HasPrefix(it.Price.LookupKey, "staqly_addon_") {
			return i, true
		}
	}
	return 0, false
}

func (sub stripeSubscription) periodEnd() int64 {
	if sub.CurrentPeriodEnd > 0 {
		return sub.CurrentPeriodEnd
	}
	var end int64
	for _, it := range sub.Items.Data {
		end = max(end, it.CurrentPeriodEnd)
	}
	return end
}

// HandleStripeWebhook verifies and applies one Stripe event. Unknown event
// types are acknowledged and ignored; each event id is applied once.
func (s *Service) HandleStripeWebhook(ctx context.Context, payload []byte, signature string) error {
	if !s.BillingEnabled() {
		return errBillingOff
	}
	if err := verifyStripeSignature(payload, signature, s.Cfg.Billing.WebhookSecret, s.Now()); err != nil {
		return &Error{Code: "unauthorized", Status: 400, Message: "invalid Stripe signature: " + err.Error()}
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil || ev.ID == "" {
		return invalid("body", "not a Stripe event")
	}
	var sub stripeSubscription
	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		// A paid bundle of extra posts: granted once per session, expiring at the end of the billing period
		// (read here, outside the transaction, as it may ask Stripe).
		if cs := decodeCreditSession(ev.Data.Object); cs != nil {
			ws := cs.Metadata["workspace_id"]
			if ws == "" {
				ws = cs.ClientReferenceID
			}
			expires := s.creditsExpiry(ctx, ws)
			return s.tx(ctx, func(q *db.Queries) error {
				if n, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: ev.ID, CreatedAt: s.now()}); err != nil || n == 0 {
					return err
				}
				return s.grantCredits(ctx, q, *cs, expires)
			})
		}
		if ev.Type != "checkout.session.completed" {
			return nil
		}
		var cs struct {
			Subscription string `json:"subscription"`
		}
		if err := json.Unmarshal(ev.Data.Object, &cs); err != nil || cs.Subscription == "" {
			return nil // not a subscription checkout
		}
		// The session carries only the id; read the subscription itself.
		if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(cs.Subscription), nil, &sub); err != nil {
			return err
		}
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted",
		"customer.subscription.paused", "customer.subscription.resumed":
		if err := json.Unmarshal(ev.Data.Object, &sub); err != nil || sub.ID == "" {
			return invalid("body", "bad subscription object")
		}
		// Stripe sends events out of order and retries failed ones for days, so the payload may be older than
		// what was already applied: apply the subscription as Stripe has it now. Subscriptions of no workspace
		// here are ignored without asking; when Stripe can't be reached the event fails and Stripe retries it.
		if ok, err := s.subscriptionIsOurs(ctx, sub); err != nil || !ok {
			if err == nil {
				s.logger(ctx).Warn("stripe: subscription for an unknown customer", "subscription", sub.ID, "event", ev.Type)
			}
			return err
		}
		id := sub.ID
		sub = stripeSubscription{}
		if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(id), nil, &sub); err != nil {
			return err
		}
	case "charge.refunded", "charge.dispute.created":
		// A refunded or disputed bundle of extra posts: its unused posts go back in the refunded share. Read
		// outside the transaction (it asks Stripe); unreachable Stripe fails the event so it is retried.
		r, err := s.decodeCreditRefund(ctx, ev.Type, ev.Data.Object)
		if err != nil {
			return err
		}
		cs, err := s.creditSessionOf(ctx, r.PaymentIntent)
		if err != nil || cs == nil {
			return err
		}
		return s.tx(ctx, func(q *db.Queries) error {
			if n, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: ev.ID, CreatedAt: s.now()}); err != nil || n == 0 {
				return err
			}
			return s.revokeCredits(ctx, q, *cs, r, ev.Type)
		})
	case "entitlements.active_entitlement_summary.updated":
		// The customer's features changed (an add-on, a plan switch, a deal set in the Dashboard): the job reads
		// the current set, so events arriving out of order can't leave an old one.
		var sum struct {
			Customer string `json:"customer"`
		}
		if err := json.Unmarshal(ev.Data.Object, &sum); err != nil || sum.Customer == "" {
			return invalid("body", "bad entitlement summary")
		}
		return s.tx(ctx, func(q *db.Queries) error {
			if n, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: ev.ID, CreatedAt: s.now()}); err != nil || n == 0 {
				return err
			}
			ws, err := q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(sum.Customer))
			if isNoRows(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return s.queueEntitlementSync(ctx, q, ws)
		})
	default:
		return nil
	}
	return s.tx(ctx, func(q *db.Queries) error {
		if n, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: ev.ID, CreatedAt: s.now()}); err != nil || n == 0 {
			return err // n == 0: already applied
		}
		return s.applySubscription(ctx, q, sub, ev.Type)
	})
}

// subscriptionIsOurs: the subscription names a workspace of this instance (metadata or a known customer).
func (s *Service) subscriptionIsOurs(ctx context.Context, sub stripeSubscription) (bool, error) {
	if ws := sub.Metadata["workspace_id"]; ws != "" {
		_, err := s.Q.GetWorkspaceBilling(ctx, ws)
		if isNoRows(err) {
			return false, nil
		}
		return err == nil, err
	}
	_, err := s.Q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(sub.Customer))
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) applySubscription(ctx context.Context, q *db.Queries, sub stripeSubscription, eventType string) error {
	ws := sub.Metadata["workspace_id"]
	if ws == "" {
		id, err := q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(sub.Customer))
		if isNoRows(err) {
			s.logger(ctx).Warn("stripe: subscription for an unknown customer", "subscription", sub.ID, "event", eventType)
			return nil
		}
		if err != nil {
			return err
		}
		ws = id
	}
	cur, err := q.GetWorkspaceBilling(ctx, ws)
	if isNoRows(err) {
		return nil // workspace deleted
	}
	if err != nil {
		return err
	}
	if cur.StripeSubscriptionID.Valid && cur.StripeSubscriptionID.String != sub.ID && cur.SubscriptionStatus.Valid &&
		liveSubscription(cur.SubscriptionStatus.String) && !liveSubscription(sub.Status) {
		return nil // an old subscription ending does not touch the current one
	}
	if cur.StripeSubscriptionID.Valid && cur.StripeSubscriptionID.String == sub.ID && cur.SubscriptionStatus.Valid &&
		finalSubscription(cur.SubscriptionStatus.String) && sub.Status != cur.SubscriptionStatus.String {
		// Stripe never revives a canceled subscription: an event saying otherwise was sent before the
		// cancellation and arrived late (out of order or retried), so it must not bring the paid plan back.
		s.logger(ctx).Warn("stripe: stale event for an ended subscription", "subscription", sub.ID, "event", eventType, "status", sub.Status)
		return nil
	}
	if err := q.SetSubscription(ctx, db.SetSubscriptionParams{StripeSubscriptionID: nullStr(sub.ID),
		SubscriptionStatus: nullStr(sub.Status), UpdatedAt: s.now(), ID: ws}); err != nil {
		return err
	}
	// The quantity follows the members: a subscription whose seats differ (changed at Stripe, or a join that
	// raced the checkout) is put right by the seat sync job, after this transaction.
	if i, ok := s.planItem(sub); liveSubscription(sub.Status) && ok {
		if n, err := s.seatCount(ctx, q, ws); err == nil && n != sub.Items.Data[i].Quantity {
			if err := s.queueSeatSync(ctx, q, ws); err != nil {
				return err
			}
		}
	}
	// Plan switches, add-ons and cancellations change the features Stripe grants.
	if err := s.queueEntitlementSync(ctx, q, ws); err != nil {
		return err
	}
	actor := Principal{WorkspaceID: ws, Kind: PrincipalSystem, ID: "stripe", Name: "Stripe"}
	reason := "stripe:" + sub.Status
	planID := s.planOfSubscription(sub)
	if planID == "" {
		// A price staqly can't place: keep the plan the workspace has (a known one), else the cheapest.
		s.logger(ctx).Warn("stripe: subscription price maps to no plan", "subscription", sub.ID, "event", eventType)
		planID = cur.Plan
		if _, ok := PlanByID(planID); !ok {
			planID = Plans[0].ID
		}
	}
	switch sub.Status {
	case "past_due", "incomplete":
		// Stripe advances the period even when the renewal fails: keep the
		// paid end (plus grace); the workspace is suspended if it passes.
		return nil
	case "active", "trialing":
		paidUntil := sub.periodEnd()
		if sub.Status == "trialing" && sub.TrialEnd > 0 {
			// The trial's period ends with it; trialEndsAt reads it back as plan_expires_at - billingGrace.
			paidUntil = sub.TrialEnd
		}
		end := time.Unix(paidUntil, 0).Add(billingGrace)
		if paidUntil == 0 {
			end = s.Now().Add(31 * 24 * time.Hour)
		}
		return s.changePlan(ctx, q, actor, planID, formatTime(end), reason)
	}
	// Canceled, unpaid, paused, incomplete_expired: the plan ends now, so the
	// workspace becomes read-only until it pays again.
	end := s.Now()
	if cur.PlanExpiresAt.Valid {
		if t, err := time.Parse(timeFormat, cur.PlanExpiresAt.String); err == nil && t.Before(end) {
			end = t
		}
	}
	return s.changePlan(ctx, q, actor, planID, formatTime(end), reason)
}

// cancelSubscription ends a live subscription right away (workspace deleted).
func (s *Service) cancelSubscription(ctx context.Context, workspaceID string) error {
	if !s.BillingEnabled() {
		return nil
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	// Every subscription Stripe could still bill or resume, unpaid and paused included (not only the live
	// ones): after the workspace is gone nobody could reach it to cancel.
	if err != nil || !w.StripeSubscriptionID.Valid || !cancellableSubscription(w.SubscriptionStatus.String) {
		return err
	}
	return s.stripe(ctx, "DELETE", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String), nil, nil)
}

// stripe calls the Stripe API with a form-encoded body and decodes the JSON
// answer into out.
func (s *Service) stripe(ctx context.Context, method, path string, form url.Values, out any) error {
	return s.stripeKeyed(ctx, method, path, form, out, "")
}

// stripeKeyed is stripe with an Idempotency-Key ("" for none).
func (s *Service) stripeKeyed(ctx context.Context, method, path string, form url.Values, out any, idempotencyKey string) error {
	base := firstNonEmpty(s.Cfg.Billing.APIBase, "https://api.stripe.com")
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.Cfg.Billing.SecretKey, "")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
				Param   string `json:"param"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		s.logger(ctx).Error("stripe request failed", "path", path, "status", resp.StatusCode, "err", e.Error.Message)
		return &Error{Code: "payment_provider_error", Status: 502, Message: "the payment provider rejected the request",
			Details: map[string]any{"provider_code": e.Error.Code, "provider_param": e.Error.Param}}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// verifyStripeSignature checks the Stripe-Signature header ("t=...,v1=..."):
// HMAC-SHA256 of "<t>.<payload>" with the endpoint secret, at most 5 minutes old.
func verifyStripeSignature(payload []byte, header, secret string, now time.Time) error {
	var ts int64
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return errors.New("missing timestamp or v1 signature")
	}
	if d := now.Sub(time.Unix(ts, 0)); d > stripeSignatureTolerance || d < -stripeSignatureTolerance {
		return errors.New("timestamp outside the tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	mac.Write(payload)
	want := mac.Sum(nil)
	for _, sig := range sigs {
		if hmac.Equal(sig, want) {
			return nil
		}
	}
	return errors.New("no matching signature")
}

// seatCount: the seats a workspace is billed for, one per member whatever the role. Open invitations are not
// billed until accepted.
func (s *Service) seatCount(ctx context.Context, q *db.Queries, workspaceID string) (int64, error) {
	n, err := q.CountMembers(ctx, workspaceID)
	return max(n, 1), err
}

// queueSeatSync: someone joined or left; in the same transaction, so the job runs only once that is saved,
// and Stripe is called from the worker (never inside a transaction). One queued sync per workspace: changes
// that come before it runs are coalesced (it sets the absolute member count), and it prorates from the time
// of the latest change.
func (s *Service) queueSeatSync(ctx context.Context, q *db.Queries, workspaceID string) error {
	if !s.BillingEnabled() || s.Cfg.Mode == ModeLocal {
		return nil
	}
	return s.enqueue(ctx, q, workspaceID, "sync_seats", "sync_seats:"+workspaceID, jobPayload{At: s.now()}, s.Now(), 0)
}

// runSyncSeats sets the live subscription's quantity to the workspace's members, prorated from when the
// membership changed (at), so a person pays exactly for the days they were a member:
//   - more seats: always_invoice, the rest of the period is charged at once (waiting for the next invoice
//     would leave the charge unpaid if the subscription is cancelled at the period's end);
//   - fewer seats: create_prorations, the unused rest is credited on the next invoice;
//   - added and removed in one period: the charge and the credit net to the days in between;
//   - during the Starter trial: no proration (seats are free; the first invoice uses the count at its end).
//
// Idempotent: it reads both counts and changes nothing when they agree.
func (s *Service) runSyncSeats(ctx context.Context, workspaceID, at string) error {
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !w.StripeSubscriptionID.Valid || !liveSubscription(w.SubscriptionStatus.String) {
		return nil // nothing to bill yet: the checkout takes the count then
	}
	seats, err := s.seatCount(ctx, s.Q, workspaceID)
	if err != nil {
		return err
	}
	var sub stripeSubscription
	if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String), nil, &sub); err != nil {
		return err
	}
	i, ok := s.planItem(sub)
	if !ok || !liveSubscription(sub.Status) {
		return nil
	}
	item := sub.Items.Data[i]
	if item.Quantity == seats {
		return nil
	}
	form := url.Values{
		"items[0][id]":       {item.ID},
		"items[0][quantity]": {strconv.FormatInt(seats, 10)},
	}
	switch {
	case sub.Status == "trialing":
		form.Set("proration_behavior", "none")
	case seats > item.Quantity:
		form.Set("proration_behavior", "always_invoice")
	default:
		form.Set("proration_behavior", "create_prorations")
	}
	if sub.Status != "trialing" {
		// Prorate from the change, not from when this job ran; Stripe wants a date inside the current period.
		when := s.Now().Unix()
		if t, err := time.Parse(timeFormat, at); err == nil {
			when = t.Unix()
		}
		start := max(sub.CurrentPeriodStart, item.CurrentPeriodStart)
		if start > 0 && when < start {
			when = start
		}
		form.Set("proration_date", strconv.FormatInt(min(when, s.Now().Unix()), 10))
	}
	return s.stripe(ctx, "POST", "/v1/subscriptions/"+url.PathEscape(sub.ID), form, nil)
}
