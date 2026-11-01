package app

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var errAdminNoBilling = &Error{Code: "billing_disabled", Status: 409, Message: "Stripe isn't configured on this server"}

var promoCodeRe = regexp.MustCompile(`^[A-Z0-9_-]{3,40}$`)

type AdminPayment struct {
	ID                 string `json:"id"`
	Number             string `json:"number"`
	WorkspaceID        string `json:"workspace_id"`
	WorkspaceName      string `json:"workspace_name"`
	CustomerEmail      string `json:"customer_email"`
	Status             string `json:"status"`
	AmountDue          int64  `json:"amount_due"`
	AmountPaid         int64  `json:"amount_paid"`
	Currency           string `json:"currency"`
	AttemptCount       int    `json:"attempt_count"`
	Created            string `json:"created"`
	PeriodEnd          string `json:"period_end"`
	NextPaymentAttempt string `json:"next_payment_attempt"`
	HostedInvoiceURL   string `json:"hosted_invoice_url"`
}

type AdminPaymentPage struct {
	Items      []AdminPayment `json:"items"`
	NextCursor string         `json:"next_cursor"`
}

type AdminPromoCoupon struct {
	ID               string  `json:"id"`
	PercentOff       float64 `json:"percent_off"`
	AmountOff        int64   `json:"amount_off"`
	Currency         string  `json:"currency"`
	Duration         string  `json:"duration"`
	DurationInMonths int     `json:"duration_in_months"`
}

type AdminPromo struct {
	ID             string           `json:"id"`
	Code           string           `json:"code"`
	Active         bool             `json:"active"`
	Coupon         AdminPromoCoupon `json:"coupon"`
	TimesRedeemed  int              `json:"times_redeemed"`
	MaxRedemptions int              `json:"max_redemptions"`
	ExpiresAt      string           `json:"expires_at"`
}

type AdminPromoPage struct {
	Items      []AdminPromo `json:"items"`
	NextCursor string       `json:"next_cursor"`
}

type AdminPromoInput struct {
	Code             string  `json:"code"`
	PercentOff       float64 `json:"percent_off"`
	AmountOff        int64   `json:"amount_off"`
	Currency         string  `json:"currency"`
	Duration         string  `json:"duration"`
	DurationInMonths int     `json:"duration_in_months"`
	MaxRedemptions   int     `json:"max_redemptions"`
	ExpiresAt        string  `json:"expires_at"`
}

func unixRFC3339(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

// AdminListPayments lists Stripe invoices, newest first. status is paid|open|uncollectible|void|failed
// ("failed" = open with a failed attempt, plus uncollectible); the failed filter runs after the fetch, so a page
// may hold fewer than limit items (next_cursor still continues). Drafts are skipped.
func (s *Service) AdminListPayments(ctx context.Context, status, wsID, cursor string, limit int) (AdminPaymentPage, error) {
	page := AdminPaymentPage{Items: []AdminPayment{}}
	if !s.BillingEnabled() {
		return page, errAdminNoBilling
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	form := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		form.Set("starting_after", cursor)
	}
	switch status {
	case "paid", "open", "uncollectible", "void":
		form.Set("status", status)
	case "failed":
		// fetched unfiltered, narrowed below
	}
	if wsID != "" {
		w, err := s.Q.GetWorkspace(ctx, wsID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return page, err
		}
		if w.StripeCustomerID.String == "" {
			return page, nil
		}
		form.Set("customer", w.StripeCustomerID.String)
	}
	var out struct {
		HasMore bool `json:"has_more"`
		Data    []struct {
			ID                 string `json:"id"`
			Number             string `json:"number"`
			Customer           string `json:"customer"`
			CustomerEmail      string `json:"customer_email"`
			Status             string `json:"status"`
			AmountDue          int64  `json:"amount_due"`
			AmountPaid         int64  `json:"amount_paid"`
			Currency           string `json:"currency"`
			AttemptCount       int    `json:"attempt_count"`
			Created            int64  `json:"created"`
			PeriodEnd          int64  `json:"period_end"`
			NextPaymentAttempt int64  `json:"next_payment_attempt"`
			HostedInvoiceURL   string `json:"hosted_invoice_url"`
		} `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/invoices?"+form.Encode(), nil, &out); err != nil {
		return page, err
	}
	names := map[string]string{}
	ids := map[string]string{}
	for _, in := range out.Data {
		if in.Status == "draft" {
			continue
		}
		if status == "failed" && !(in.Status == "uncollectible" || (in.Status == "open" && in.AttemptCount > 0)) {
			continue
		}
		id, ok := ids[in.Customer]
		if !ok {
			id, _ = s.Q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(in.Customer)) // unknown customer: ""
			ids[in.Customer] = id
			if id != "" {
				if w, err := s.Q.GetWorkspace(ctx, id); err == nil {
					names[id] = w.Name
				}
			}
		}
		page.Items = append(page.Items, AdminPayment{
			ID: in.ID, Number: in.Number, WorkspaceID: id, WorkspaceName: names[id], CustomerEmail: in.CustomerEmail,
			Status: in.Status, AmountDue: in.AmountDue, AmountPaid: in.AmountPaid, Currency: in.Currency,
			AttemptCount: in.AttemptCount, Created: unixRFC3339(in.Created), PeriodEnd: unixRFC3339(in.PeriodEnd),
			NextPaymentAttempt: unixRFC3339(in.NextPaymentAttempt), HostedInvoiceURL: in.HostedInvoiceURL,
		})
	}
	if out.HasMore && len(out.Data) > 0 {
		page.NextCursor = out.Data[len(out.Data)-1].ID
	}
	return page, nil
}

type stripePromo struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	Active         bool   `json:"active"`
	TimesRedeemed  int    `json:"times_redeemed"`
	MaxRedemptions int    `json:"max_redemptions"`
	ExpiresAt      int64  `json:"expires_at"`
	Coupon         struct {
		ID               string  `json:"id"`
		PercentOff       float64 `json:"percent_off"`
		AmountOff        int64   `json:"amount_off"`
		Currency         string  `json:"currency"`
		Duration         string  `json:"duration"`
		DurationInMonths int     `json:"duration_in_months"`
	} `json:"coupon"`
}

func (p stripePromo) admin() AdminPromo {
	return AdminPromo{ID: p.ID, Code: p.Code, Active: p.Active, Coupon: AdminPromoCoupon(p.Coupon),
		TimesRedeemed: p.TimesRedeemed, MaxRedemptions: p.MaxRedemptions, ExpiresAt: unixRFC3339(p.ExpiresAt)}
}

func (s *Service) AdminListPromotionCodes(ctx context.Context, cursor string, limit int) (AdminPromoPage, error) {
	page := AdminPromoPage{Items: []AdminPromo{}}
	if !s.BillingEnabled() {
		return page, errAdminNoBilling
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	form := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		form.Set("starting_after", cursor)
	}
	var out struct {
		HasMore bool          `json:"has_more"`
		Data    []stripePromo `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/promotion_codes?"+form.Encode(), nil, &out); err != nil {
		return page, err
	}
	for _, p := range out.Data {
		page.Items = append(page.Items, p.admin())
	}
	if out.HasMore && len(out.Data) > 0 {
		page.NextCursor = out.Data[len(out.Data)-1].ID
	}
	return page, nil
}

func (s *Service) AdminCreatePromotionCode(ctx context.Context, a Admin, in AdminPromoInput) (AdminPromo, error) {
	if !s.BillingEnabled() {
		return AdminPromo{}, errAdminNoBilling
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Currency = strings.ToLower(strings.TrimSpace(in.Currency))
	if !promoCodeRe.MatchString(in.Code) {
		return AdminPromo{}, invalid("code", "3-40 characters: A-Z, 0-9, - and _")
	}
	if (in.PercentOff > 0) == (in.AmountOff > 0) {
		return AdminPromo{}, invalid("percent_off", "set exactly one of percent_off or amount_off")
	}
	if in.PercentOff != 0 && (in.PercentOff < 1 || in.PercentOff > 100) {
		return AdminPromo{}, invalid("percent_off", "must be 1-100")
	}
	if in.AmountOff > 0 && len(in.Currency) != 3 {
		return AdminPromo{}, invalid("currency", "a 3-letter currency is required with amount_off")
	}
	switch in.Duration {
	case "once", "forever":
	case "repeating":
		if in.DurationInMonths < 1 || in.DurationInMonths > 36 {
			return AdminPromo{}, invalid("duration_in_months", "must be 1-36 for a repeating duration")
		}
	default:
		return AdminPromo{}, invalid("duration", "must be once, repeating or forever")
	}
	if in.MaxRedemptions < 0 {
		return AdminPromo{}, invalid("max_redemptions", "must not be negative")
	}
	var expires time.Time
	if in.ExpiresAt != "" {
		var err error
		if expires, err = time.Parse(time.RFC3339, in.ExpiresAt); err != nil {
			if expires, err = time.Parse("2006-01-02", in.ExpiresAt); err != nil {
				return AdminPromo{}, invalid("expires_at", "use YYYY-MM-DD or RFC3339")
			}
		}
		if !expires.After(s.Now()) {
			return AdminPromo{}, invalid("expires_at", "must be in the future")
		}
	}
	cf := url.Values{"duration": {in.Duration}}
	if in.PercentOff > 0 {
		cf.Set("percent_off", strconv.FormatFloat(in.PercentOff, 'f', -1, 64))
	} else {
		cf.Set("amount_off", strconv.FormatInt(in.AmountOff, 10))
		cf.Set("currency", in.Currency)
	}
	if in.Duration == "repeating" {
		cf.Set("duration_in_months", strconv.Itoa(in.DurationInMonths))
	}
	var coupon struct {
		ID string `json:"id"`
	}
	if err := s.stripe(ctx, "POST", "/v1/coupons", cf, &coupon); err != nil {
		return AdminPromo{}, err
	}
	pf := url.Values{"coupon": {coupon.ID}, "code": {in.Code}}
	if in.MaxRedemptions > 0 {
		pf.Set("max_redemptions", strconv.Itoa(in.MaxRedemptions))
	}
	if !expires.IsZero() {
		pf.Set("expires_at", strconv.FormatInt(expires.Unix(), 10))
	}
	var p stripePromo
	if err := s.stripe(ctx, "POST", "/v1/promotion_codes", pf, &p); err != nil {
		// Best effort: a coupon without its code is unreachable clutter in Stripe.
		if derr := s.stripe(ctx, "DELETE", "/v1/coupons/"+url.PathEscape(coupon.ID), nil, nil); derr != nil {
			s.logger(ctx).Warn("orphan Stripe coupon not deleted", "coupon", coupon.ID, "err", derr)
		}
		return AdminPromo{}, err
	}
	res := p.admin()
	return res, s.AdminAudit(ctx, a, "create_promotion_code", "", res.ID, map[string]any{
		"code": res.Code, "coupon": res.Coupon, "max_redemptions": res.MaxRedemptions, "expires_at": res.ExpiresAt,
	})
}

func (s *Service) AdminDeactivatePromotionCode(ctx context.Context, a Admin, id string) (AdminPromo, error) {
	if !s.BillingEnabled() {
		return AdminPromo{}, errAdminNoBilling
	}
	var p stripePromo
	if err := s.stripe(ctx, "POST", "/v1/promotion_codes/"+url.PathEscape(id), url.Values{"active": {"false"}}, &p); err != nil {
		return AdminPromo{}, err
	}
	res := p.admin()
	return res, s.AdminAudit(ctx, a, "deactivate_promotion_code", "", id, map[string]any{"code": res.Code})
}
