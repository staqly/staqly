package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Stripe Entitlements: features a workspace's subscription grants on top of its plan. Plans stay the baseline
// (Plans in plans.go); a feature can switch a capability on or raise a limit, through its metadata:
//
//	staqly_limit_<limit> = "+10" | "unlimited"   channels, alerts, webhooks, api_keys, x_posts_per_period,
//	                                              posts_per_month, storage_gb, max_upload_mb
//	staqly_enable        = "data_export,..."      capabilities a plan may lack
//
// Features come from the products a subscription holds: each plan's product (its base features, no deltas),
// add-on products (Channels pack, ...) and, for one customer's deal, a product with that feature added to their
// subscription. Stripe works out the active set; the sync_entitlements job stores it on the workspace, so a
// limit check reads it with the plan and never calls Stripe.

// Entitlement is one active feature, as the workspace keeps it.
type Entitlement struct {
	LookupKey string            `json:"lookup_key"`
	Name      string            `json:"name"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// LimitBonus: what one entitlement adds to a limit (Add), or that it lifts it (Unlimited).
type LimitBonus struct {
	Limit     string `json:"limit"`
	Add       int64  `json:"add"`
	Unlimited bool   `json:"unlimited"`
	From      string `json:"from"` // the feature's name
}

// limitFields: the limits an entitlement can raise, with the unit its metadata counts in.
var limitFields = []struct {
	name  string
	unit  int64
	limit string
	field func(*PlanLimits) *int64
}{
	{"channels", 1, LimitChannels, func(l *PlanLimits) *int64 { return &l.Channels }},
	{"alerts", 1, LimitAlerts, func(l *PlanLimits) *int64 { return &l.Alerts }},
	{"webhooks", 1, LimitWebhooks, func(l *PlanLimits) *int64 { return &l.Webhooks }},
	{"api_keys", 1, LimitAPIKeys, func(l *PlanLimits) *int64 { return &l.APIKeys }},
	{"posts_per_month", 1, LimitPostsPerMonth, func(l *PlanLimits) *int64 { return &l.PostsPerMonth }},
	{"x_posts_per_period", 1, LimitXPosts, func(l *PlanLimits) *int64 { return &l.XPostsPerPeriod }},
	{"storage_gb", gb, LimitStorage, func(l *PlanLimits) *int64 { return &l.StorageBytes }},
	{"max_upload_mb", 1 << 20, LimitMaxUpload, func(l *PlanLimits) *int64 { return &l.MaxUploadBytes }},
}

// ApplyEntitlements is the one place a plan meets its entitlements: the effective plan every limit check uses
// (planState). Deltas add up across features; "unlimited" lifts a limit; a limit the plan has unlimited stays
// so; unknown or malformed metadata is ignored. Without entitlements the plan comes back unchanged.
func ApplyEntitlements(p Plan, ents []Entitlement) Plan {
	if len(ents) == 0 {
		return p
	}
	base := p.Limits
	p.Entitlements = nil
	p.LimitBonuses = nil
	for _, e := range ents {
		p.Entitlements = append(p.Entitlements, e.LookupKey)
		for _, f := range limitFields {
			v := strings.TrimSpace(e.Metadata["staqly_limit_"+f.name])
			if v == "" {
				continue
			}
			ptr := f.field(&p.Limits)
			if strings.EqualFold(v, "unlimited") {
				if *ptr != Unlimited {
					*ptr = Unlimited
					p.LimitBonuses = append(p.LimitBonuses, LimitBonus{Limit: f.limit, Unlimited: true, From: e.Name})
				}
				continue
			}
			n, err := strconv.ParseInt(strings.TrimPrefix(v, "+"), 10, 64)
			if err != nil || n <= 0 || *ptr == Unlimited {
				continue
			}
			*ptr += n * f.unit
			p.LimitBonuses = append(p.LimitBonuses, LimitBonus{Limit: f.limit, Add: n * f.unit, From: e.Name})
		}
		for _, c := range strings.Split(e.Metadata["staqly_enable"], ",") {
			if strings.TrimSpace(c) == LimitDataExport && !p.Limits.DataExport {
				p.Limits.DataExport = true
				p.LimitBonuses = append(p.LimitBonuses, LimitBonus{Limit: LimitDataExport, Unlimited: true, From: e.Name})
			}
		}
	}
	sort.Strings(p.Entitlements)
	if p.Limits != base {
		p.BaseLimits = &base
	}
	return p
}

// HasEntitlement: the workspace's subscription grants the feature (e.g. "ai_pack").
func (p Plan) HasEntitlement(lookupKey string) bool {
	for _, k := range p.Entitlements {
		if k == lookupKey {
			return true
		}
	}
	return false
}

// parseEntitlements reads the workspace's stored entitlements; a broken value counts as none.
func parseEntitlements(raw string) []Entitlement {
	var out []Entitlement
	if raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// ---- reading them from Stripe ----

// featureCache: the account's features by id (name, lookup key, metadata), for 10 minutes.
var featureCache = struct {
	sync.Mutex
	key string
	exp time.Time
	m   map[string]Entitlement
}{}

// stripeFeatures: the Stripe account's entitlement features, by id. Ceiling: the first 100 (a catalog far
// smaller than that); page through them when it grows.
func (s *Service) stripeFeatures(ctx context.Context) (map[string]Entitlement, error) {
	key := s.Cfg.Billing.APIBase + "|" + s.Cfg.Billing.SecretKey
	featureCache.Lock()
	if featureCache.key == key && s.Now().Before(featureCache.exp) {
		m := featureCache.m
		featureCache.Unlock()
		return m, nil
	}
	featureCache.Unlock()
	var list struct {
		Data []struct {
			ID        string            `json:"id"`
			Name      string            `json:"name"`
			LookupKey string            `json:"lookup_key"`
			Metadata  map[string]string `json:"metadata"`
		} `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/entitlements/features?limit=100", nil, &list); err != nil {
		return nil, err
	}
	m := map[string]Entitlement{}
	for _, f := range list.Data {
		m[f.ID] = Entitlement{LookupKey: f.LookupKey, Name: f.Name, Metadata: f.Metadata}
	}
	featureCache.Lock()
	featureCache.key, featureCache.exp, featureCache.m = key, s.Now().Add(priceCacheTTL), m
	featureCache.Unlock()
	return m, nil
}

// activeEntitlements: what Stripe grants a customer now, with each feature's name and metadata.
func (s *Service) activeEntitlements(ctx context.Context, customer string) ([]Entitlement, error) {
	var list struct {
		Data []struct {
			Feature   string `json:"feature"`
			LookupKey string `json:"lookup_key"`
		} `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/entitlements/active_entitlements?limit=100&customer="+url.QueryEscape(customer), nil, &list); err != nil {
		return nil, err
	}
	features, err := s.stripeFeatures(ctx)
	if err != nil {
		return nil, err
	}
	out := []Entitlement{}
	for _, a := range list.Data {
		e, ok := features[a.Feature]
		if !ok {
			e = Entitlement{LookupKey: a.LookupKey, Name: a.LookupKey} // a feature made in the last 10 minutes
		}
		if e.LookupKey == "" {
			e.LookupKey = a.LookupKey
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LookupKey < out[j].LookupKey })
	return out, nil
}

// queueEntitlementSync: the subscription (or Stripe's summary of the customer's features) changed. In the
// caller's transaction; the job reads Stripe after it commits. Queued syncs of one workspace coalesce.
func (s *Service) queueEntitlementSync(ctx context.Context, q *db.Queries, workspaceID string) error {
	if !s.BillingEnabled() || s.Cfg.Mode == ModeLocal {
		return nil
	}
	return s.enqueue(ctx, q, workspaceID, "sync_entitlements", "sync_entitlements:"+workspaceID, jobPayload{}, s.Now(), 0)
}

// runSyncEntitlements stores what Stripe grants the workspace's customer now (none without a customer).
// Idempotent: it always writes the current set.
func (s *Service) runSyncEntitlements(ctx context.Context, workspaceID string) error {
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	ents := []Entitlement{}
	if w.StripeCustomerID.Valid && w.StripeCustomerID.String != "" {
		if ents, err = s.activeEntitlements(ctx, w.StripeCustomerID.String); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(ents)
	if err != nil {
		return err
	}
	return s.W.SetWorkspaceEntitlements(ctx, db.SetWorkspaceEntitlementsParams{Entitlements: string(raw), UpdatedAt: s.now(), ID: workspaceID})
}

// ---- add-ons ----

// Addon is a product a workspace can add to its subscription (Stripe products with metadata staqly_kind=addon;
// their prices hold the lookup keys staqly_addon_<id>_<month|year>). Products still marked draft in the
// catalog are not in Stripe yet, so they are never offered.
type Addon struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Features    []string `json:"features"`
	// PriceMonth, PriceYear: per workspace, in whole units of Currency (0: not sold in that interval).
	PriceMonth int    `json:"price_month"`
	PriceYear  int    `json:"price_year"`
	Currency   string `json:"currency"`
	// Active: on the workspace's subscription now.
	Active bool `json:"active"`
}

// AddonLookupKey: the Stripe lookup key of an add-on's price, staqly_addon_ai_pack_month.
func AddonLookupKey(id, interval string) string { return "staqly_addon_" + id + "_" + interval }

var addonCache = struct {
	sync.Mutex
	key string
	exp time.Time
	v   []Addon
}{}

// stripeAddons: the add-ons on sale, with their prices, cached 10 minutes.
func (s *Service) stripeAddons(ctx context.Context) ([]Addon, error) {
	key := s.Cfg.Billing.APIBase + "|" + s.Cfg.Billing.SecretKey
	addonCache.Lock()
	if addonCache.key == key && s.Now().Before(addonCache.exp) {
		v := addonCache.v
		addonCache.Unlock()
		return v, nil
	}
	addonCache.Unlock()
	var list struct {
		Data []struct {
			Name              string            `json:"name"`
			Description       string            `json:"description"`
			Metadata          map[string]string `json:"metadata"`
			MarketingFeatures []struct {
				Name string `json:"name"`
			} `json:"marketing_features"`
		} `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/products?active=true&limit=100", nil, &list); err != nil {
		return nil, err
	}
	out := []Addon{}
	for _, p := range list.Data {
		id := p.Metadata["staqly_addon"]
		if p.Metadata["staqly_kind"] != "addon" || id == "" {
			continue
		}
		a := Addon{ID: id, Name: p.Name, Description: p.Description, Currency: Currency, Features: []string{}}
		for _, f := range p.MarketingFeatures {
			a.Features = append(a.Features, f.Name)
		}
		for _, iv := range []string{IntervalMonth, IntervalYear} {
			pr, err := s.lookupPrice(ctx, AddonLookupKey(id, iv), "", "")
			if err != nil || pr.cents <= 0 || (pr.currency != "" && pr.currency != Currency) {
				continue
			}
			if iv == IntervalMonth {
				a.PriceMonth = int(pr.cents / 100)
			} else {
				a.PriceYear = int(pr.cents / 100)
			}
		}
		if a.PriceMonth > 0 || a.PriceYear > 0 {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	addonCache.Lock()
	addonCache.key, addonCache.exp, addonCache.v = key, s.Now().Add(priceCacheTTL), out
	addonCache.Unlock()
	return out, nil
}

// addonItem: the subscription item holding an add-on, if the subscription has it.
func addonItem(sub stripeSubscription, id string) (string, bool) {
	for _, it := range sub.Items.Data {
		if it.Price.Metadata["staqly_addon"] == id || strings.HasPrefix(it.Price.LookupKey, "staqly_addon_"+id+"_") {
			return it.ID, true
		}
	}
	return "", false
}

// subscriptionInterval: month or year, from the plan's item (every item of a subscription bills alike).
func (s *Service) subscriptionInterval(sub stripeSubscription) string {
	if i, ok := s.planItem(sub); ok && sub.Items.Data[i].Price.Recurring.Interval == IntervalYear {
		return IntervalYear
	}
	return IntervalMonth
}

// liveSubscriptionOf: the workspace's live subscription from Stripe, or a conflict saying a plan comes first.
func (s *Service) liveSubscriptionOf(ctx context.Context, workspaceID string) (stripeSubscription, error) {
	var sub stripeSubscription
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if err != nil {
		return sub, err
	}
	if !w.StripeSubscriptionID.Valid || !liveSubscription(w.SubscriptionStatus.String) {
		return sub, &Error{Code: "conflict", Status: 409, Message: "add-ons come with a plan: choose a plan first"}
	}
	err = s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String), nil, &sub)
	return sub, err
}

// ListAddons: GET /v1/billing/addons, the add-ons on sale and which the workspace has. Without billing: none.
func (s *Service) ListAddons(ctx context.Context, p Principal) ([]Addon, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return nil, err
	}
	if !s.BillingEnabled() {
		return []Addon{}, nil
	}
	all, err := s.stripeAddons(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]Addon{}, all...)
	if sub, err := s.liveSubscriptionOf(ctx, p.WorkspaceID); err == nil {
		for i := range out {
			_, out[i].Active = addonItem(sub, out[i].ID)
		}
	}
	return out, nil
}

// AddAddon: POST /v1/billing/addons/{id}. The add-on joins the live subscription as one more item, in the
// subscription's interval, charged at once for the rest of the period (always_invoice, like an added seat);
// the card on file pays it, so no Checkout is needed. Its features arrive with Stripe's entitlement events.
func (s *Service) AddAddon(ctx context.Context, p Principal, id string) error {
	if err := s.requireAccountOwner(ctx, p); err != nil {
		return err
	}
	if !s.BillingEnabled() {
		return errBillingOff
	}
	sub, err := s.liveSubscriptionOf(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	if _, ok := addonItem(sub, id); ok {
		return &Error{Code: "conflict", Status: 409, Message: "the workspace already has this add-on"}
	}
	interval := s.subscriptionInterval(sub)
	pr, err := s.lookupPrice(ctx, AddonLookupKey(id, interval), "", fmt.Sprintf("this add-on is not sold %sly on this instance", interval))
	if err != nil {
		return err
	}
	if err := s.stripe(ctx, "POST", "/v1/subscription_items", url.Values{
		"subscription":           {sub.ID},
		"price":                  {pr.id},
		"quantity":               {"1"},
		"proration_behavior":     {"always_invoice"},
		"metadata[staqly_addon]": {id},
	}, nil); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error { return s.queueEntitlementSync(ctx, q, p.WorkspaceID) })
}

// RemoveAddon: DELETE /v1/billing/addons/{id}. The item leaves the subscription now; the unused rest of the
// period is credited on the next invoice.
func (s *Service) RemoveAddon(ctx context.Context, p Principal, id string) error {
	if err := s.requireAccountOwner(ctx, p); err != nil {
		return err
	}
	if !s.BillingEnabled() {
		return errBillingOff
	}
	sub, err := s.liveSubscriptionOf(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	item, ok := addonItem(sub, id)
	if !ok {
		return notFound("add-on")
	}
	if err := s.stripe(ctx, "DELETE", "/v1/subscription_items/"+url.PathEscape(item)+"?proration_behavior=create_prorations", nil, nil); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error { return s.queueEntitlementSync(ctx, q, p.WorkspaceID) })
}
