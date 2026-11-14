package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
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
//	                                              linkedin_posts_per_period, linkedin_videos_per_period,
//	                                              posts_per_month, storage_gb, max_upload_mb,
//	                                              ai_monthly_cents (US cents: "+500" is $5 more a month)
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
	// Quantity: how many of the add-on granting it the subscription holds, when more than one (set at Stripe):
	// what it adds to a limit counts that many times.
	Quantity int64 `json:"quantity,omitempty"`
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
	{"linkedin_posts_per_period", 1, LimitLinkedInPosts, func(l *PlanLimits) *int64 { return &l.LinkedInPostsPerPeriod }},
	{"linkedin_videos_per_period", 1, LimitLinkedInVideos, func(l *PlanLimits) *int64 { return &l.LinkedInVideosPerPeriod }},
	{"storage_gb", gb, LimitStorage, func(l *PlanLimits) *int64 { return &l.StorageBytes }},
	{"max_upload_mb", 1 << 20, LimitMaxUpload, func(l *PlanLimits) *int64 { return &l.MaxUploadBytes }},
	{"ai_monthly_cents", 1, LimitAIMonthlyCents, func(l *PlanLimits) *int64 { return &l.AIMonthlyCents }},
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
			n *= max(e.Quantity, 1)
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
	// A feature made since the catalog was cached (10 minutes) would be kept without its metadata, so without
	// the limits it raises: read the catalog again, once.
	if slices.ContainsFunc(list.Data, func(a struct {
		Feature   string `json:"feature"`
		LookupKey string `json:"lookup_key"`
	}) bool {
		_, ok := features[a.Feature]
		return !ok
	}) {
		featureCache.Lock()
		featureCache.exp = time.Time{}
		featureCache.Unlock()
		if features, err = s.stripeFeatures(ctx); err != nil {
			return nil, err
		}
	}
	out := []Entitlement{}
	for _, a := range list.Data {
		e, ok := features[a.Feature]
		if !ok {
			e = Entitlement{LookupKey: a.LookupKey, Name: a.LookupKey} // past the catalog's first 100 (stripeFeatures)
		}
		if e.LookupKey == "" {
			e.LookupKey = a.LookupKey
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LookupKey < out[j].LookupKey })
	return out, nil
}

// addonQuantities marks the entitlements of add-ons the subscription holds more than once (a quantity set at
// Stripe: the app adds one). Stripe's active entitlements say nothing of quantities, so the features of such an
// add-on's product are read; with every quantity at one, the subscription is all it asks for.
func (s *Service) addonQuantities(ctx context.Context, subscriptionID string, ents []Entitlement) error {
	var sub stripeSubscription
	if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(subscriptionID), nil, &sub); err != nil {
		return err
	}
	for _, it := range sub.Items.Data {
		var product string
		if it.Quantity <= 1 || addonOf(it.Price) == "" || json.Unmarshal(it.Price.Product, &product) != nil || product == "" {
			continue
		}
		var list struct {
			Data []struct {
				Feature struct {
					LookupKey string `json:"lookup_key"`
				} `json:"entitlement_feature"`
			} `json:"data"`
		}
		if err := s.stripe(ctx, "GET", "/v1/products/"+url.PathEscape(product)+"/features?limit=100", nil, &list); err != nil {
			return err
		}
		for i := range ents {
			if slices.ContainsFunc(list.Data, func(f struct {
				Feature struct {
					LookupKey string `json:"lookup_key"`
				} `json:"entitlement_feature"`
			}) bool {
				return f.Feature.LookupKey == ents[i].LookupKey
			}) {
				ents[i].Quantity = it.Quantity
			}
		}
	}
	return nil
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
		if len(ents) > 0 && w.StripeSubscriptionID.Valid && liveSubscription(w.SubscriptionStatus.String) {
			if err := s.addonQuantities(ctx, w.StripeSubscriptionID.String, ents); err != nil {
				return err
			}
		}
	}
	raw, err := json.Marshal(ents)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		cur, err := q.GetWorkspacePlan(ctx, workspaceID)
		if err != nil {
			return err
		}
		if err := q.SetWorkspaceEntitlements(ctx, db.SetWorkspaceEntitlementsParams{Entitlements: string(raw), UpdatedAt: s.now(), ID: workspaceID}); err != nil {
			return err
		}
		if cur.Entitlements == string(raw) {
			return nil
		}
		keys := make([]string, 0, len(ents))
		for _, e := range ents {
			keys = append(keys, e.LookupKey)
		}
		actor := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "stripe", Name: "Stripe"}
		_, err = s.emit(ctx, q, actor, "workspace.entitlements_changed", "", "", map[string]any{"features": keys})
		return err
	})
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

// addonOf: the add-on a price sells ("" for a plan's price), from its metadata or its lookup key.
func addonOf(pr stripePrice) string {
	if id := pr.Metadata["staqly_addon"]; id != "" {
		return id
	}
	rest, ok := strings.CutPrefix(pr.LookupKey, "staqly_addon_")
	if !ok {
		return ""
	}
	rest = strings.TrimSuffix(rest, "_"+IntervalMonth)
	return strings.TrimSuffix(rest, "_"+IntervalYear)
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

// errAddonInTrial: Stripe charges nothing during a trial, so an add-on bought then would be free until its end.
var errAddonInTrial = &Error{Code: "conflict", Status: 409, Message: "add-ons come with a paid plan: choose a plan first (or start the paid plan now)",
	Details: map[string]any{"reason": "trialing"}}

var errHasAddon = &Error{Code: "conflict", Status: 409, Message: "the workspace already has this add-on"}

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
// subscription's interval, charged at once for the rest of the period and added only once that is paid (a pending
// update, like an upgrade: switchSubscription); the card on file pays it, so no Checkout is needed. A payment that
// doesn't go through answers 402 payment_failed and leaves the subscription as it was, without the add-on. Its
// features arrive with Stripe's entitlement events.
func (s *Service) AddAddon(ctx context.Context, p Principal, id string) error {
	if err := s.requireOwner(ctx, p); err != nil {
		return err
	}
	if !s.BillingEnabled() {
		return errBillingOff
	}
	if err := requirePerson(p); err != nil {
		return err
	}
	// Only what the list shows is sold: not a draft, a plan's product or an add-on without a price.
	all, err := s.stripeAddons(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(all, func(a Addon) bool { return a.ID == id }) {
		return notFound("add-on")
	}
	// In the trial, as staqly knows it: refused before Stripe is asked anything about the subscription.
	if w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID); err != nil {
		return err
	} else if w.SubscriptionStatus.String == "trialing" {
		return errAddonInTrial
	}
	sub, err := s.liveSubscriptionOf(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	if sub.Status == "trialing" { // (the trial began at Stripe and its event isn't in yet)
		return errAddonInTrial
	}
	if _, ok := addonItem(sub, id); ok {
		return errHasAddon
	}
	interval := s.subscriptionInterval(sub)
	key, missing := AddonLookupKey(id, interval), fmt.Sprintf("this add-on is not sold %sly on this instance", interval)
	pr, err := s.lookupPrice(ctx, key, "", missing)
	if err != nil {
		return err
	}
	// Through the subscription (an item without an id is added, the others stay): its answer carries the pending
	// update. No item metadata: a pending update takes only what changes the invoice, and the add-on is told by
	// its price (addonItem).
	var out struct {
		PendingUpdate *stripePendingUpdate `json:"pending_update"`
		LatestInvoice json.RawMessage      `json:"latest_invoice"`
	}
	// The two refusals handled below, a lost race and a replaced price, are no errors of this server.
	handled := stripeHandles(ctx, func(_ int, param string) bool { return param == "plan" || strings.HasSuffix(param, "[price]") })
	add := func(price string) error {
		return s.stripe(handled, "POST", "/v1/subscriptions/"+url.PathEscape(sub.ID), url.Values{
			"items[0][price]":    {price},
			"items[0][quantity]": {"1"},
			"proration_behavior": {"always_invoice"},
			"payment_behavior":   {"pending_if_incomplete"},
			"expand[]":           {"latest_invoice"},
		}, &out)
	}
	err = add(pr.id)
	// Stripe refused the price: it was replaced since lookupPrice cached it (archived: 400 without a code, "The
	// price specified is inactive"; deleted: resource_missing; both name the parameter items[0][price]). Once
	// more with the price the lookup key has now.
	if strings.HasSuffix(stripeParam(err), "[price]") {
		s.forgetPrice(key)
		fresh, lerr := s.lookupPrice(ctx, key, "", missing)
		if lerr != nil {
			return lerr
		}
		if fresh.id != pr.id {
			err = add(fresh.id)
		}
	}
	// Another request added it since the subscription was read above (two purchases at once): Stripe refuses a
	// second item with the same price (400 invalid_request_error without a code, parameter "plan").
	if stripeParam(err) == "plan" {
		return errHasAddon
	}
	if err != nil {
		return err
	}
	if out.PendingUpdate != nil {
		pp := s.pendingPayment(ctx, *out.PendingUpdate, out.LatestInvoice, "")
		pp.Plan, pp.Addon = "", id
		return pp.error("your subscription")
	}
	return s.tx(ctx, func(q *db.Queries) error { return s.queueEntitlementSync(ctx, q, p.WorkspaceID) })
}

// RemoveAddon: DELETE /v1/billing/addons/{id}. The item leaves the subscription now; the unused rest of the
// period is invoiced at once (always_invoice), and that invoice's negative total becomes the customer's balance
// at Stripe, which pays the next invoices (an upgrade's too). An add-on that was never paid (added before add-ons
// waited for their payment; the subscription is past_due) is not credited: its open invoice is voided instead,
// so nobody owes for an add-on they no longer have.
func (s *Service) RemoveAddon(ctx context.Context, p Principal, id string) error {
	if err := s.requireOwner(ctx, p); err != nil {
		return err
	}
	if !s.BillingEnabled() {
		return errBillingOff
	}
	if err := requirePerson(p); err != nil {
		return err
	}
	sub, err := s.liveSubscriptionOf(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	item, ok := addonItem(sub, id)
	if !ok {
		return s.cancelPendingAddon(ctx, sub.ID, id)
	}
	proration := "always_invoice"
	if sub.Status == "past_due" && s.voidInvoicesOf(ctx, sub.ID, item) {
		proration = "none"
	}
	gone := stripeHandles(ctx, func(status int, _ string) bool { return status == 404 }) // the item, removed meanwhile
	if err := s.stripe(gone, "DELETE", "/v1/subscription_items/"+url.PathEscape(item)+"?proration_behavior="+proration, nil, nil); err != nil {
		// Another request removed it since the subscription was read above. Stripe says so with a 404 that has
		// neither a code nor a parameter ("Invalid subscription_item id"), so the subscription is asked instead.
		if now, rerr := s.liveSubscriptionOf(ctx, p.WorkspaceID); rerr == nil {
			if _, still := addonItem(now, id); !still {
				return notFound("add-on")
			}
		}
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error { return s.queueEntitlementSync(ctx, q, p.WorkspaceID) })
}

// cancelPendingAddon: the add-on isn't on the subscription yet, it waits for its payment (a pending update).
// Removing it cancels that update the way Stripe has for it: voiding the invoice the update made (the
// subscription's latest), so nothing stays due for an add-on nobody wants. Not found when no update holds it.
func (s *Service) cancelPendingAddon(ctx context.Context, subscriptionID, id string) error {
	var sub struct {
		PendingUpdate *stripePendingUpdate `json:"pending_update"`
		LatestInvoice string               `json:"latest_invoice"`
	}
	if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(subscriptionID), nil, &sub); err != nil {
		return err
	}
	waits := false
	if sub.PendingUpdate != nil {
		for _, it := range sub.PendingUpdate.SubscriptionItems {
			var pr stripePrice
			waits = waits || (json.Unmarshal(it.Price, &pr) == nil && addonOf(pr) == id)
		}
	}
	if !waits || sub.LatestInvoice == "" {
		return notFound("add-on")
	}
	return s.stripe(ctx, "POST", "/v1/invoices/"+url.PathEscape(sub.LatestInvoice)+"/void", nil, nil)
}

// voidInvoicesOf voids the subscription's open invoices that bill nothing but one item (an add-on charged when it
// was added and never paid) and says whether there was one and all of them are void now. An invoice with other
// lines (a renewal) stays; one that can't be read or voided is logged and counts as not voided, so the removal
// credits the item as usual.
// shortcut: the newest 10 open invoices and each one's first 10 lines; an invoice of one add-on has one or two.
func (s *Service) voidInvoicesOf(ctx context.Context, subscriptionID, item string) bool {
	var list struct {
		Data []struct {
			ID    string `json:"id"`
			Lines struct {
				Data []struct {
					SubscriptionItem string `json:"subscription_item"` // before Stripe API 2025-03-31
					Parent           struct {
						SubscriptionItemDetails struct {
							SubscriptionItem string `json:"subscription_item"`
						} `json:"subscription_item_details"`
					} `json:"parent"`
				} `json:"data"`
				HasMore bool `json:"has_more"`
			} `json:"lines"`
		} `json:"data"`
	}
	q := url.Values{"subscription": {subscriptionID}, "status": {"open"}, "limit": {"10"}}
	if err := s.stripe(ctx, "GET", "/v1/invoices?"+q.Encode(), nil, &list); err != nil {
		return false
	}
	found, voided := 0, 0
	for _, inv := range list.Data {
		only := len(inv.Lines.Data) > 0 && !inv.Lines.HasMore
		for _, l := range inv.Lines.Data {
			only = only && firstNonEmpty(l.Parent.SubscriptionItemDetails.SubscriptionItem, l.SubscriptionItem) == item
		}
		if !only {
			continue
		}
		found++
		if err := s.stripe(ctx, "POST", "/v1/invoices/"+url.PathEscape(inv.ID)+"/void", nil, nil); err != nil {
			s.logger(ctx).Warn("billing: unpaid add-on invoice not voided", "err", err)
			continue
		}
		voided++
	}
	return found > 0 && voided == found
}
