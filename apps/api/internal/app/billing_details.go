package app

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// invoicesPerPage: how many invoices one billing details answer lists.
const invoicesPerPage = 24

// BillingDetails is what Stripe knows about the workspace's billing, read on request and never stored:
// the subscription and its period, the cards on file and the invoices.
type BillingDetails struct {
	Enabled         bool                 `json:"enabled"`
	Subscription    *BillingSubscription `json:"subscription"`
	PaymentMethods  []PaymentMethod      `json:"payment_methods"`
	Invoices        []Invoice            `json:"invoices"`
	HasMoreInvoices bool                 `json:"has_more_invoices"`
	// ManagedPayments: checkouts on this server use Stripe Managed Payments (STRIPE_MANAGED_PAYMENTS), so Stripe,
	// through Link, is the merchant of record and sends the customer its receipts and invoices.
	ManagedPayments bool `json:"managed_payments"`
}

type BillingSubscription struct {
	Status             string  `json:"status"`
	CurrentPeriodStart *string `json:"current_period_start"`
	CurrentPeriodEnd   *string `json:"current_period_end"`
	CancelAtPeriodEnd  bool    `json:"cancel_at_period_end"`
	CancelAt           *string `json:"cancel_at"`
	CanceledAt         *string `json:"canceled_at"`
	TrialEnd           *string `json:"trial_end"`
	// Amount per seat (user) per Interval in the smallest currency unit (cents), from the first item's price.
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Interval string `json:"interval"`
	// Quantity: the seats Stripe bills (the workspace's members; kept in step by the seat sync).
	Quantity int64 `json:"quantity"`
}

// PaymentMethod is a card on file: brand and last four digits only.
type PaymentMethod struct {
	Brand    string `json:"brand"`
	Last4    string `json:"last4"`
	ExpMonth int    `json:"exp_month"`
	ExpYear  int    `json:"exp_year"`
	Default  bool   `json:"default"`
}

type Invoice struct {
	ID               string  `json:"id"`
	Number           string  `json:"number"`
	Created          string  `json:"created"`
	PeriodStart      *string `json:"period_start"`
	PeriodEnd        *string `json:"period_end"`
	Total            int64   `json:"total"`
	AmountPaid       int64   `json:"amount_paid"`
	AmountDue        int64   `json:"amount_due"`
	Currency         string  `json:"currency"`
	Status           string  `json:"status"`
	HostedInvoiceURL string  `json:"hosted_invoice_url"`
	InvoicePDF       string  `json:"invoice_pdf"`
}

type stripeCard struct {
	ID   string `json:"id"`
	Card *struct {
		Brand    string `json:"brand"`
		Last4    string `json:"last4"`
		ExpMonth int    `json:"exp_month"`
		ExpYear  int    `json:"exp_year"`
	} `json:"card"`
}

// GetBillingDetails reads the subscription, cards and invoices from Stripe (admin, cloud with billing on).
// Without billing or before the first checkout it answers empty lists. `after` pages the invoices: the id of
// the last invoice of the previous page.
func (s *Service) GetBillingDetails(ctx context.Context, p Principal, after string) (BillingDetails, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return BillingDetails{}, err
	}
	out := BillingDetails{Enabled: s.BillingEnabled(), PaymentMethods: []PaymentMethod{}, Invoices: []Invoice{},
		ManagedPayments: s.BillingEnabled() && s.Cfg.Billing.ManagedPayments}
	if !out.Enabled {
		return out, nil
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return out, err
	}
	if !w.StripeCustomerID.Valid {
		return out, nil
	}
	customer := url.PathEscape(w.StripeCustomerID.String)

	if w.StripeSubscriptionID.Valid && after == "" {
		var sub struct {
			Status             string `json:"status"`
			CurrentPeriodStart int64  `json:"current_period_start"`
			CurrentPeriodEnd   int64  `json:"current_period_end"`
			CancelAtPeriodEnd  bool   `json:"cancel_at_period_end"`
			CancelAt           int64  `json:"cancel_at"`
			CanceledAt         int64  `json:"canceled_at"`
			TrialEnd           int64  `json:"trial_end"`
			DefaultPM          string `json:"default_payment_method"`
			Items              struct {
				Data []struct {
					CurrentPeriodStart int64 `json:"current_period_start"`
					CurrentPeriodEnd   int64 `json:"current_period_end"`
					Quantity           int64 `json:"quantity"`
					Price              struct {
						UnitAmount int64             `json:"unit_amount"`
						Currency   string            `json:"currency"`
						LookupKey  string            `json:"lookup_key"`
						Metadata   map[string]string `json:"metadata"`
						Recurring  struct {
							Interval string `json:"interval"`
						} `json:"recurring"`
					} `json:"price"`
				} `json:"data"`
			} `json:"items"`
		}
		if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String), nil, &sub); err != nil {
			return out, err
		}
		// Newer Stripe API versions keep the period on the items.
		start, end := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
		bs := BillingSubscription{Status: sub.Status, CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
			CancelAt: unixTime(sub.CancelAt), CanceledAt: unixTime(sub.CanceledAt), TrialEnd: unixTime(sub.TrialEnd)}
		// The plan's item, not an add-on's (an add-on is an item of its own).
		plan := -1
		for i, it := range sub.Items.Data {
			if plan < 0 && it.Price.Metadata["staqly_addon"] == "" && !strings.HasPrefix(it.Price.LookupKey, "staqly_addon_") {
				plan = i
			}
		}
		if plan >= 0 {
			it := sub.Items.Data[plan]
			if start == 0 {
				start, end = it.CurrentPeriodStart, it.CurrentPeriodEnd
			}
			bs.Amount, bs.Currency, bs.Interval = it.Price.UnitAmount, it.Price.Currency, it.Price.Recurring.Interval
			bs.Quantity = max(it.Quantity, 1)
		}
		bs.CurrentPeriodStart, bs.CurrentPeriodEnd = unixTime(start), unixTime(end)
		out.Subscription = &bs

		var cus struct {
			InvoiceSettings struct {
				DefaultPM string `json:"default_payment_method"`
			} `json:"invoice_settings"`
		}
		if err := s.stripe(ctx, "GET", "/v1/customers/"+customer, nil, &cus); err != nil {
			return out, err
		}
		def := firstNonEmpty(sub.DefaultPM, cus.InvoiceSettings.DefaultPM)
		var cards struct {
			Data []stripeCard `json:"data"`
		}
		if err := s.stripe(ctx, "GET", "/v1/customers/"+customer+"/payment_methods?type=card&limit=10", nil, &cards); err != nil {
			return out, err
		}
		for _, c := range cards.Data {
			if c.Card == nil {
				continue
			}
			out.PaymentMethods = append(out.PaymentMethods, PaymentMethod{Brand: c.Card.Brand, Last4: c.Card.Last4,
				ExpMonth: c.Card.ExpMonth, ExpYear: c.Card.ExpYear, Default: c.ID == def || len(cards.Data) == 1})
		}
	}

	q := url.Values{"customer": {w.StripeCustomerID.String}, "limit": {strconv.Itoa(invoicesPerPage)}}
	if after != "" {
		q.Set("starting_after", after)
	}
	var inv struct {
		HasMore bool `json:"has_more"`
		Data    []struct {
			ID               string `json:"id"`
			Number           string `json:"number"`
			Created          int64  `json:"created"`
			PeriodStart      int64  `json:"period_start"`
			PeriodEnd        int64  `json:"period_end"`
			Total            int64  `json:"total"`
			AmountPaid       int64  `json:"amount_paid"`
			AmountDue        int64  `json:"amount_due"`
			Currency         string `json:"currency"`
			Status           string `json:"status"`
			HostedInvoiceURL string `json:"hosted_invoice_url"`
			InvoicePDF       string `json:"invoice_pdf"`
		} `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/invoices?"+q.Encode(), nil, &inv); err != nil {
		return out, err
	}
	out.HasMoreInvoices = inv.HasMore
	for _, i := range inv.Data {
		if i.Status == "draft" {
			continue // not issued yet
		}
		created := ""
		if t := unixTime(i.Created); t != nil {
			created = *t
		}
		out.Invoices = append(out.Invoices, Invoice{ID: i.ID, Number: i.Number, Created: created,
			PeriodStart: unixTime(i.PeriodStart), PeriodEnd: unixTime(i.PeriodEnd), Total: i.Total,
			AmountPaid: i.AmountPaid, AmountDue: i.AmountDue, Currency: i.Currency, Status: i.Status,
			HostedInvoiceURL: i.HostedInvoiceURL, InvoicePDF: i.InvoicePDF})
	}
	return out, nil
}

// unixTime formats a Stripe timestamp; 0 (unset) is nil.
func unixTime(sec int64) *string {
	if sec <= 0 {
		return nil
	}
	v := formatTime(time.Unix(sec, 0))
	return &v
}
