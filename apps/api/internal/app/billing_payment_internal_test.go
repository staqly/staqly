package app

import (
	"strings"
	"testing"
)

// Every reason a payment_failed (402) and the pending payment can carry, from what Stripe says about the payment:
// the dashboard picks the page to send the person to by it (the invoice page to confirm with the bank, the billing
// portal for another card), so a wrong one sends them to the wrong place.
func TestPaymentReasons(t *testing.T) {
	for _, c := range []struct {
		status, code, decline, want string
	}{
		{"succeeded", "", "", PaymentProcessing},
		{"processing", "", "", PaymentProcessing},
		{"processing", "card_declined", "insufficient_funds", PaymentProcessing}, // an earlier attempt's error
		{"requires_action", "", "", PaymentAuthRequired},
		{"requires_confirmation", "", "", PaymentAuthRequired},
		{"requires_payment_method", "authentication_required", "", PaymentAuthRequired},
		{"requires_payment_method", "card_declined", "authentication_required", PaymentAuthRequired},
		{"requires_payment_method", "card_declined", "insufficient_funds", PaymentInsufficientFunds},
		{"requires_payment_method", "expired_card", "", PaymentExpiredCard},
		{"requires_payment_method", "card_declined", "expired_card", PaymentExpiredCard},
		{"requires_payment_method", "incorrect_cvc", "", PaymentIncorrectDetails},
		{"requires_payment_method", "incorrect_number", "", PaymentIncorrectDetails},
		{"requires_payment_method", "invalid_cvc", "", PaymentIncorrectDetails},
		{"requires_payment_method", "invalid_expiry_month", "", PaymentIncorrectDetails},
		{"requires_payment_method", "invalid_expiry_year", "", PaymentIncorrectDetails},
		{"requires_payment_method", "incorrect_zip", "", PaymentIncorrectDetails},
		{"requires_payment_method", "card_declined", "incorrect_cvc", PaymentIncorrectDetails},
		{"requires_payment_method", "card_declined", "incorrect_number", PaymentIncorrectDetails},
		{"requires_payment_method", "card_declined", "invalid_cvc", PaymentIncorrectDetails},
		{"requires_payment_method", "card_declined", "invalid_expiry_year", PaymentIncorrectDetails},
		{"requires_payment_method", "card_declined", "incorrect_zip", PaymentIncorrectDetails},
		{"requires_payment_method", "", "", PaymentDeclined},
		{"", "card_declined", "", PaymentDeclined},
		{"", "", "generic_decline", PaymentDeclined},
		{"", "card_declined", "do_not_honor", PaymentDeclined},
		{"canceled", "", "", PaymentUnknown},
		{"", "", "", PaymentUnknown},
	} {
		if got := paymentReason(c.status, c.code, c.decline); got != c.want {
			t.Errorf("status %q, code %q, decline %q: %s, want %s", c.status, c.code, c.decline, got, c.want)
		}
	}
}

// The 402 says what stays as it was and carries only what is known; the pay link is Stripe's or nothing.
func TestPaymentFailedError(t *testing.T) {
	for _, c := range []struct {
		pp      PendingPayment
		message string
		details string // the keys, sorted
	}{
		{PendingPayment{Reason: PaymentAuthRequired, Plan: "pro", PayURL: "https://invoice.stripe.com/i/1"}, "your bank asks you to confirm the payment for the new plan; until then the trial stays", "pay_url plan reason"},
		{PendingPayment{Reason: PaymentInsufficientFunds, Addon: "ai_pack", DeclineCode: "insufficient_funds"}, "the card was declined, so the trial stays", "addon decline_code reason"},
		{PendingPayment{Reason: PaymentExpiredCard}, "the card was declined", "reason"},
		{PendingPayment{Reason: PaymentIncorrectDetails}, "the card was declined", "reason"},
		{PendingPayment{Reason: PaymentDeclined}, "the card was declined", "reason"},
		{PendingPayment{Reason: PaymentProcessing, Addon: "ai_pack"}, "the payment for the add-on is still being processed; the add-on is added once it goes through", "addon reason"},
		{PendingPayment{Reason: PaymentProcessing, Plan: "plus", ExpiresAt: "2030-01-01T00:00:00Z"}, "the payment for the new plan is still being processed; the plan changes once", "expires_at plan reason"},
		{PendingPayment{Reason: PaymentUnknown}, "the payment for the new plan didn't go through, so the trial stays as it was", "reason"},
	} {
		e := c.pp.error("the trial")
		var keys []string
		for _, k := range []string{"addon", "decline_code", "expires_at", "pay_url", "plan", "reason"} {
			if _, ok := e.Details[k]; ok {
				keys = append(keys, k)
			}
		}
		if e.Code != "payment_failed" || e.Status != 402 || !strings.Contains(e.Message, c.message) || strings.Join(keys, " ") != c.details ||
			len(keys) != len(e.Details) || e.Details["reason"] != c.pp.Reason {
			t.Errorf("%+v: %d %s %q, details %v", c.pp, e.Status, e.Code, e.Message, e.Details)
		}
	}
	for link, want := range map[string]bool{
		"https://invoice.stripe.com/i/acct_1/test_1": true,
		"https://stripe.com/pay":                     true,
		"http://invoice.stripe.com/i/acct_1":         false, // not https
		"https://invoice.stripe.com.evil.example/i":  false,
		"https://evilstripe.com/i":                   false,
		"https://evil.example/?u=invoice.stripe.com": false,
		"javascript:alert(1)":                        false,
		"":                                           false,
	} {
		if got := safeStripeURL(link); got != want {
			t.Errorf("safeStripeURL(%q) = %v, want %v", link, got, want)
		}
	}
}
