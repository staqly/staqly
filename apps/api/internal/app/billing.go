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
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// BillingConfig: Stripe settings (cloud mode). Billing is on when SecretKey,
// WebhookSecret and PricePro are all set.
type BillingConfig struct {
	SecretKey     string // STRIPE_SECRET_KEY (sk_test_... or sk_live_...)
	WebhookSecret string // STRIPE_WEBHOOK_SECRET (whsec_...)
	PricePro      string // STRIPE_PRICE_PRO: the recurring price of Pro
	APIBase       string // tests only; default https://api.stripe.com
}

// billingGrace keeps Pro active a little past the paid period, so a renewal
// paid a few hours late does not suspend the workspace in between.
const billingGrace = 3 * 24 * time.Hour

// stripeSignatureTolerance: maximum age of a webhook signature.
const stripeSignatureTolerance = 5 * time.Minute

func (s *Service) BillingEnabled() bool {
	b := s.Cfg.Billing
	return s.Cfg.Mode == ModeCloud && b.SecretKey != "" && b.WebhookSecret != "" && b.PricePro != ""
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
	Suspended     bool    `json:"suspended"`
	HasCustomer   bool    `json:"has_customer"`
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
	b := Billing{Enabled: s.BillingEnabled(), Plan: w.Plan, Suspended: suspended, HasCustomer: w.StripeCustomerID.Valid}
	if w.SubscriptionStatus.Valid {
		b.SubscriptionStatus = &w.SubscriptionStatus.String
	}
	if w.PlanExpiresAt.Valid {
		b.PlanExpiresAt = &w.PlanExpiresAt.String
	}
	if s.Cfg.Mode == ModeLocal {
		b.Plan = UnlockedPlan.ID
	}
	return b, nil
}

// CheckoutSession starts a Stripe Checkout for Pro and returns its URL.
func (s *Service) CheckoutSession(ctx context.Context, p Principal) (string, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return "", err
	}
	if !s.BillingEnabled() {
		return "", errBillingOff
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return "", err
	}
	if w.SubscriptionStatus.Valid && liveSubscription(w.SubscriptionStatus.String) {
		return "", &Error{Code: "conflict", Status: 409, Message: "this workspace already has a subscription; manage it in the billing portal"}
	}
	customer := w.StripeCustomerID.String
	if customer == "" {
		var c struct {
			ID string `json:"id"`
		}
		if err := s.stripe(ctx, "POST", "/v1/customers", url.Values{"name": {w.Name},
			"metadata[workspace_id]": {w.ID}}, &c); err != nil {
			return "", err
		}
		customer = c.ID
		if err := s.W.SetStripeCustomer(ctx, db.SetStripeCustomerParams{StripeCustomerID: nullStr(customer),
			UpdatedAt: s.now(), ID: w.ID}); err != nil {
			return "", err
		}
	}
	back := s.planPageURL()
	var cs struct {
		URL string `json:"url"`
	}
	err = s.stripe(ctx, "POST", "/v1/checkout/sessions", url.Values{
		"mode":                    {"subscription"},
		"customer":                {customer},
		"client_reference_id":     {w.ID},
		"line_items[0][price]":    {s.Cfg.Billing.PricePro},
		"line_items[0][quantity]": {"1"},
		"subscription_data[metadata][workspace_id]": {w.ID},
		"success_url": {back + "?checkout=success"},
		"cancel_url":  {back + "?checkout=canceled"},
	}, &cs)
	return cs.URL, err
}

// PortalSession opens the Stripe customer portal (payment method, invoices,
// cancel) and returns its URL.
func (s *Service) PortalSession(ctx context.Context, p Principal) (string, error) {
	if err := p.require(ScopeAdmin); err != nil {
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
// have) a paid Pro plan.
func liveSubscription(status string) bool {
	switch status {
	case "active", "trialing", "past_due", "incomplete":
		return true
	}
	return false
}

// stripeSubscription is the part of a Stripe subscription staqly reads.
// current_period_end moved to the items in newer API versions; both are read.
type stripeSubscription struct {
	ID               string            `json:"id"`
	Customer         string            `json:"customer"`
	Status           string            `json:"status"`
	CurrentPeriodEnd int64             `json:"current_period_end"`
	EndedAt          int64             `json:"ended_at"`
	Metadata         map[string]string `json:"metadata"`
	Items            struct {
		Data []struct {
			CurrentPeriodEnd int64 `json:"current_period_end"`
		} `json:"data"`
	} `json:"items"`
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
	case "checkout.session.completed":
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
		if err := json.Unmarshal(ev.Data.Object, &sub); err != nil {
			return invalid("body", "bad subscription object")
		}
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
	if err := q.SetSubscription(ctx, db.SetSubscriptionParams{StripeSubscriptionID: nullStr(sub.ID),
		SubscriptionStatus: nullStr(sub.Status), UpdatedAt: s.now(), ID: ws}); err != nil {
		return err
	}
	actor := Principal{WorkspaceID: ws, Kind: PrincipalSystem, ID: "stripe", Name: "Stripe"}
	reason := "stripe:" + sub.Status
	switch sub.Status {
	case "past_due", "incomplete":
		// Stripe advances the period even when the renewal fails: keep the
		// paid end (plus grace); the workspace is suspended if it passes.
		return nil
	case "active", "trialing":
		end := time.Unix(sub.periodEnd(), 0).Add(billingGrace)
		if sub.periodEnd() == 0 {
			end = s.Now().Add(31 * 24 * time.Hour)
		}
		return s.changePlan(ctx, q, actor, "pro", formatTime(end), reason)
	}
	if cur.Plan != "pro" {
		return nil // e.g. they already chose Free; nothing to suspend
	}
	// Canceled, unpaid, paused, incomplete_expired: Pro ends now, so the
	// workspace becomes read-only until it pays again or switches to Free.
	end := s.Now()
	if cur.PlanExpiresAt.Valid {
		if t, err := time.Parse(timeFormat, cur.PlanExpiresAt.String); err == nil && t.Before(end) {
			end = t
		}
	}
	return s.changePlan(ctx, q, actor, "pro", formatTime(end), reason)
}

// cancelSubscription ends a live subscription right away (workspace deleted).
func (s *Service) cancelSubscription(ctx context.Context, workspaceID string) error {
	if !s.BillingEnabled() {
		return nil
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if err != nil || !w.StripeSubscriptionID.Valid || !liveSubscription(w.SubscriptionStatus.String) {
		return err
	}
	return s.stripe(ctx, "DELETE", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String), nil, nil)
}

// stripe calls the Stripe API with a form-encoded body and decodes the JSON
// answer into out.
func (s *Service) stripe(ctx context.Context, method, path string, form url.Values, out any) error {
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
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		s.logger(ctx).Error("stripe request failed", "path", path, "status", resp.StatusCode, "err", e.Error.Message)
		return &Error{Code: "payment_provider_error", Status: 502, Message: "the payment provider rejected the request"}
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
