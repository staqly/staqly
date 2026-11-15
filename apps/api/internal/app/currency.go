package app

import (
	"context"
	"net/url"
	"sync"
)

// Regional pricing: every price is the same number in each currency (15 EUR = 15 CHF = 15 USD); the currency
// follows the customer's country. Switzerland and Liechtenstein pay in CHF, the EU in EUR, everyone else in USD.
// A Stripe customer that already has a currency keeps it: Stripe locks a customer to one currency. Otherwise the
// country is Cloudflare's (CF-IPCountry, believed only from Cloudflare's addresses: web.ClientCountry); without
// one (a host Cloudflare doesn't proxy), an IANA time zone stands in: the workspace's (set from the browser at
// sign-up) or the one the page sends.
const (
	CurrencyCHF = "chf"
	CurrencyEUR = "eur"
	CurrencyUSD = "usd"
)

// euCountries: the 27 EU member states (ISO 3166-1 alpha-2).
var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "HR": true, "CY": true, "CZ": true, "DK": true, "EE": true, "FI": true,
	"FR": true, "DE": true, "GR": true, "HU": true, "IE": true, "IT": true, "LV": true, "LT": true, "LU": true,
	"MT": true, "NL": true, "PL": true, "PT": true, "RO": true, "SK": true, "SI": true, "ES": true, "SE": true,
}

// currencyForCountry: CHF for CH and LI, EUR for the EU, USD for any other country or an unknown one.
func currencyForCountry(country string) string {
	switch {
	case country == "CH" || country == "LI":
		return CurrencyCHF
	case euCountries[country]:
		return CurrencyEUR
	}
	return CurrencyUSD
}

// zoneCountries: the time zones of Switzerland, Liechtenstein and the EU member states (in Europe, the Azores,
// Madeira, the Canaries and Ceuta included) with their country. Any other zone counts as "elsewhere" (USD), so
// only these need listing.
var zoneCountries = map[string]string{
	"Europe/Zurich": "CH", "Europe/Vaduz": "LI",
	"Europe/Vienna": "AT", "Europe/Brussels": "BE", "Europe/Sofia": "BG", "Europe/Zagreb": "HR",
	"Asia/Nicosia": "CY", "Asia/Famagusta": "CY", "Europe/Nicosia": "CY", "Europe/Prague": "CZ",
	"Europe/Copenhagen": "DK", "Europe/Tallinn": "EE", "Europe/Helsinki": "FI", "Europe/Mariehamn": "FI",
	"Europe/Paris": "FR", "Europe/Berlin": "DE", "Europe/Busingen": "DE", "Europe/Athens": "GR",
	"Europe/Budapest": "HU", "Europe/Dublin": "IE", "Europe/Rome": "IT", "Europe/Riga": "LV",
	"Europe/Vilnius": "LT", "Europe/Luxembourg": "LU", "Europe/Malta": "MT", "Europe/Amsterdam": "NL",
	"Europe/Warsaw": "PL", "Europe/Lisbon": "PT", "Atlantic/Madeira": "PT", "Atlantic/Azores": "PT",
	"Europe/Bucharest": "RO", "Europe/Bratislava": "SK", "Europe/Ljubljana": "SI", "Europe/Madrid": "ES",
	"Africa/Ceuta": "ES", "Atlantic/Canary": "ES", "Europe/Stockholm": "SE",
}

type countryKey struct{}

// WithCountry: the request's country as Cloudflare saw it (ISO 3166-1 alpha-2), "" when unknown.
func WithCountry(ctx context.Context, country string) context.Context {
	return context.WithValue(ctx, countryKey{}, country)
}

// requestCurrency: the currency of the request's country (WithCountry), "" when it has none.
func requestCurrency(ctx context.Context) string {
	if cc, _ := ctx.Value(countryKey{}).(string); cc != "" {
		return currencyForCountry(cc)
	}
	return ""
}

// knownZone: whether a zone says where someone is ("" and UTC say nothing; a workspace starts in UTC).
func knownZone(tz string) bool { return tz != "" && tz != "UTC" && tz != "Etc/UTC" }

// currencyForZones: the currency of the first zone that says where someone is; USD when none does.
func currencyForZones(zones ...string) string {
	for _, tz := range zones {
		if knownZone(tz) {
			return currencyForCountry(zoneCountries[tz])
		}
	}
	return CurrencyUSD
}

// PlansCurrency: the currency a caller is shown prices in: a workspace's (see workspaceCurrency) when signed
// in, else the one of the time zone the page sent (timezone, an IANA name).
func (s *Service) PlansCurrency(ctx context.Context, p Principal, timezone string) string {
	if p.WorkspaceID != "" {
		return s.workspaceCurrency(ctx, p.WorkspaceID, timezone)
	}
	if cur := requestCurrency(ctx); cur != "" {
		return cur
	}
	return currencyForZones(timezone)
}

// workspaceCurrency: the currency of the workspace's Stripe customer when it has one (Stripe sets it with the
// first subscription or payment and refuses any other later), else the request's country's (Cloudflare's), else
// the one of the workspace's time zone, else of the fallback zone.
func (s *Service) workspaceCurrency(ctx context.Context, workspaceID, fallbackZone string) string {
	if s.BillingEnabled() {
		if w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID); err == nil && w.StripeCustomerID.Valid && w.StripeCustomerID.String != "" {
			if cur := s.customerCurrency(ctx, w.StripeCustomerID.String); cur != "" {
				return cur
			}
		}
	}
	if cur := requestCurrency(ctx); cur != "" {
		return cur
	}
	tz := ""
	if w, err := s.Q.GetWorkspace(ctx, workspaceID); err == nil {
		tz = w.Timezone
	}
	return currencyForZones(tz, fallbackZone)
}

// setCheckoutCurrency: a new Checkout charges the workspace's currency (workspaceCurrency) when the price has it,
// else the price's own.
func (s *Service) setCheckoutCurrency(ctx context.Context, form url.Values, workspaceID string, pr priceCacheEntry) {
	if cur, _ := pr.in(s.workspaceCurrency(ctx, workspaceID, "")); cur != "" {
		form.Set("currency", cur)
	}
}

// customerCurrencies: Stripe customer (per account) -> its currency, once it has one (it never changes then).
var customerCurrencies sync.Map

// customerCurrency: the Stripe customer's currency, "" while it has none (or it can't be read).
func (s *Service) customerCurrency(ctx context.Context, customer string) string {
	key := s.Cfg.Billing.APIBase + "|" + s.Cfg.Billing.SecretKey + "|" + customer
	if cur, ok := customerCurrencies.Load(key); ok {
		return cur.(string)
	}
	var c struct {
		Currency string `json:"currency"`
	}
	if err := s.stripe(ctx, "GET", "/v1/customers/"+url.PathEscape(customer), nil, &c); err != nil {
		s.logger(ctx).Warn("billing: customer currency not read", "err", err)
		return ""
	}
	if c.Currency != "" {
		customerCurrencies.Store(key, c.Currency)
	}
	return c.Currency
}

// in: the price's amount (cents) in a currency, from its currency options; its own currency and amount when it
// has none in that one (a price made before the options, a STRIPE_PRICE_* pin), so what is shown is charged.
func (pr priceCacheEntry) in(currency string) (string, int64) {
	if c, ok := pr.options[currency]; ok && c > 0 {
		return currency, c
	}
	return pr.currency, pr.cents
}
