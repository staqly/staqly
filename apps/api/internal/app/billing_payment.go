package app

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Why a plan change that is charged at once (leaving Starter's trial) hasn't been paid. Stripe keeps such a change
// as the subscription's pending update until the invoice it made is paid; the person pays or confirms on Stripe's
// own pages (the hosted invoice page, the billing portal), never in staqly, and staqly only reads where it stands.
const (
	// PaymentAuthRequired: the bank asks the customer to confirm the payment (3-D Secure / SCA). The hosted
	// invoice page (pay_url) lets them confirm and pay; Stripe then applies the change.
	PaymentAuthRequired = "authentication_required"
	// PaymentDeclined and the more precise declines below: the card was refused; another card is needed.
	PaymentDeclined          = "card_declined"
	PaymentInsufficientFunds = "insufficient_funds"
	PaymentExpiredCard       = "expired_card"
	PaymentIncorrectDetails  = "incorrect_card_details"
	// PaymentProcessing: the payment is under way (or paid and not applied yet); nothing to do but wait.
	PaymentProcessing = "processing"
	// PaymentUnknown: Stripe's answer couldn't be read; the change still waits for its payment.
	PaymentUnknown = "unknown"
)

// PendingPayment: a plan change waiting for its payment, and why that payment hasn't gone through.
type PendingPayment struct {
	// Pending: a plan change waits for its payment; false when none does (nothing else is set then).
	Pending bool `json:"pending"`
	// Reason: one of the Payment* constants.
	Reason string `json:"reason,omitempty"`
	// DeclineCode: Stripe's decline code or error code, as Stripe gives it (for integrations; may be empty).
	DeclineCode string `json:"decline_code,omitempty"`
	// PayURL: Stripe's hosted invoice page, where the customer confirms or pays the invoice (another card too).
	PayURL string `json:"pay_url,omitempty"`
	// Plan: the plan waiting for the payment, when it can be told.
	Plan string `json:"plan,omitempty"`
	// ExpiresAt: when Stripe drops the waiting change if it is still unpaid.
	ExpiresAt string `json:"expires_at,omitempty"`
}

// error: the 402 a plan switch answers when its payment didn't go through, with the PendingPayment as details.
func (pp PendingPayment) error() *Error {
	msg := "the payment for the new plan didn't go through, so the trial stays as it was: check the card in Manage billing and try again"
	switch pp.Reason {
	case PaymentAuthRequired:
		msg = "your bank asks you to confirm the payment for the new plan; until then the trial stays as it was"
	case PaymentDeclined, PaymentInsufficientFunds, PaymentExpiredCard, PaymentIncorrectDetails:
		msg = "the card was declined, so the trial stays as it was: update the card in Manage billing and try again"
	case PaymentProcessing:
		msg = "the payment for the new plan is still being processed; the plan changes once it goes through"
	}
	details := map[string]any{"reason": pp.Reason}
	for k, v := range map[string]string{"pay_url": pp.PayURL, "decline_code": pp.DeclineCode, "plan": pp.Plan, "expires_at": pp.ExpiresAt} {
		if v != "" {
			details[k] = v
		}
	}
	return &Error{Code: "payment_failed", Status: 402, Message: msg, Details: details}
}

// stripePendingUpdate: a subscription's pending_update (null when nothing waits).
type stripePendingUpdate struct {
	ExpiresAt         int64 `json:"expires_at"`
	SubscriptionItems []struct {
		Price json.RawMessage `json:"price"` // a price object (or, in older answers, its id)
	} `json:"subscription_items"`
}

// GetPendingPayment reads from Stripe whether a plan change waits for its payment and why it hasn't been paid
// (account owner). The dashboard's payment page asks it, also after the person comes back from Stripe.
func (s *Service) GetPendingPayment(ctx context.Context, p Principal) (PendingPayment, error) {
	if err := s.requireOwner(ctx, p); err != nil {
		return PendingPayment{}, err
	}
	if !s.BillingEnabled() {
		return PendingPayment{}, errBillingOff
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return PendingPayment{}, err
	}
	if !w.StripeSubscriptionID.Valid || w.StripeSubscriptionID.String == "" {
		return PendingPayment{}, nil
	}
	var sub struct {
		PendingUpdate *stripePendingUpdate `json:"pending_update"`
		LatestInvoice json.RawMessage      `json:"latest_invoice"`
	}
	q := url.Values{"expand[]": {"latest_invoice"}}
	if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String)+"?"+q.Encode(), nil, &sub); err != nil {
		return PendingPayment{}, err
	}
	if sub.PendingUpdate == nil {
		return PendingPayment{}, nil
	}
	return s.pendingPayment(ctx, *sub.PendingUpdate, sub.LatestInvoice, ""), nil
}

// pendingPayment reads why the invoice of a pending update hasn't been paid: the invoice (expanded on the
// subscription, or its id), then its payment intent: on the invoice before Stripe API 2025-03-31, in the
// invoice's payments from then on. A read that fails leaves the reason unknown (logged), never an error: the
// change still waits either way.
func (s *Service) pendingPayment(ctx context.Context, pu stripePendingUpdate, rawInvoice json.RawMessage, plan string) PendingPayment {
	pp := PendingPayment{Pending: true, Reason: PaymentUnknown, Plan: plan}
	if pu.ExpiresAt > 0 {
		pp.ExpiresAt = time.Unix(pu.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	if pp.Plan == "" {
		for _, it := range pu.SubscriptionItems {
			var pr stripePrice
			if json.Unmarshal(it.Price, &pr) == nil {
				if id, ok := s.planOf(pr); ok {
					pp.Plan = id
					break
				}
			}
		}
	}
	var inv struct {
		ID               string          `json:"id"`
		Status           string          `json:"status"`
		HostedInvoiceURL string          `json:"hosted_invoice_url"`
		PaymentIntent    json.RawMessage `json:"payment_intent"`
	}
	var invoiceID string
	if json.Unmarshal(rawInvoice, &invoiceID) == nil && invoiceID != "" {
		if err := s.stripe(ctx, "GET", "/v1/invoices/"+url.PathEscape(invoiceID), nil, &inv); err != nil {
			return pp
		}
	} else if json.Unmarshal(rawInvoice, &inv) != nil || inv.ID == "" {
		s.logger(ctx).Warn("billing: pending update without a readable invoice")
		return pp
	}
	if inv.Status == "open" && safeStripeURL(inv.HostedInvoiceURL) {
		pp.PayURL = inv.HostedInvoiceURL
	}
	if inv.Status == "paid" { // paid, the change not applied yet (Payment Records may take a moment)
		pp.Reason = PaymentProcessing
		return pp
	}

	var pi struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		LastPaymentError *struct {
			Code        string `json:"code"`
			DeclineCode string `json:"decline_code"`
		} `json:"last_payment_error"`
	}
	var piID string
	if json.Unmarshal(inv.PaymentIntent, &piID) != nil || piID == "" {
		if json.Unmarshal(inv.PaymentIntent, &pi) != nil || pi.ID == "" {
			piID = s.invoicePaymentIntent(ctx, inv.ID)
		}
	}
	if piID != "" {
		if err := s.stripe(ctx, "GET", "/v1/payment_intents/"+url.PathEscape(piID), nil, &pi); err != nil {
			return pp
		}
	}
	if pi.ID == "" {
		return pp
	}
	code, decline := "", ""
	if e := pi.LastPaymentError; e != nil {
		code, decline = e.Code, e.DeclineCode
	}
	pp.DeclineCode = firstNonEmpty(decline, code)
	pp.Reason = paymentReason(pi.Status, code, decline)
	return pp
}

// invoicePaymentIntent: the payment intent of an invoice's default payment (Stripe API 2025-03-31 on), "" if none.
func (s *Service) invoicePaymentIntent(ctx context.Context, invoiceID string) string {
	var list struct {
		Data []struct {
			IsDefault bool `json:"is_default"`
			Payment   struct {
				Type          string `json:"type"`
				PaymentIntent string `json:"payment_intent"`
			} `json:"payment"`
		} `json:"data"`
	}
	q := url.Values{"invoice": {invoiceID}, "limit": {"10"}}
	if err := s.stripe(ctx, "GET", "/v1/invoice_payments?"+q.Encode(), nil, &list); err != nil {
		return ""
	}
	id := ""
	for _, p := range list.Data {
		if p.Payment.Type != "payment_intent" || p.Payment.PaymentIntent == "" {
			continue
		}
		if p.IsDefault {
			return p.Payment.PaymentIntent
		}
		if id == "" {
			id = p.Payment.PaymentIntent
		}
	}
	return id
}

// paymentReason maps a payment intent's status and last error onto a PendingPayment reason.
func paymentReason(status, code, decline string) string {
	switch {
	case status == "succeeded" || status == "processing":
		return PaymentProcessing
	case status == "requires_action" || status == "requires_confirmation" ||
		code == "authentication_required" || decline == "authentication_required":
		return PaymentAuthRequired
	case decline == "insufficient_funds":
		return PaymentInsufficientFunds
	case code == "expired_card" || decline == "expired_card":
		return PaymentExpiredCard
	case code == "incorrect_cvc" || code == "incorrect_number" || code == "invalid_cvc" || code == "invalid_expiry_month" ||
		code == "invalid_expiry_year" || code == "incorrect_zip" || decline == "incorrect_cvc" || decline == "incorrect_number" ||
		decline == "invalid_cvc" || decline == "invalid_expiry_year" || decline == "incorrect_zip":
		return PaymentIncorrectDetails
	case status == "requires_payment_method" || code == "card_declined" || decline != "":
		return PaymentDeclined
	}
	return PaymentUnknown
}

// safeStripeURL: an https link on a Stripe host, so the dashboard only ever opens Stripe's own pages from it.
func safeStripeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h == "stripe.com" || strings.HasSuffix(h, ".stripe.com")
}
