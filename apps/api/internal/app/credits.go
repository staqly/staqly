package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// ProviderX is the metered provider: posts to X cost staqly money at X's API (pay per use).
const ProviderX = "x"

// XLinkPostWeight: X bills a post with a live link $0.200 and one without $0.015 (docs.x.com, pricing,
// 2026-10-20), so a post or reply with a live link counts as 14 posts against the allowance. Links the
// channel removes or defangs (its links mode) don't count.
const XLinkPostWeight = 14

// Bundle is a one-time purchase of extra posts to a metered provider (Stripe lookup key
// staqly_credits_<provider>_<posts>). Bought posts are used after the plan's included ones and expire at the
// end of the billing period they were bought in.
type Bundle struct {
	Posts int64 `json:"posts"`
	// Price in whole units of Currency (euros).
	Price int `json:"price"`
}

// XBundles: €10, €40 and €150, so €0.05, €0.04 and €0.0375 a post: 2.5x or more of what the posts cost at X
// ($0.015 each; reckoned €1 = $1, which understates the margin while the euro is worth more).
// The Stripe catalog must match.
var XBundles = []Bundle{{Posts: 200, Price: 10}, {Posts: 1000, Price: 40}, {Posts: 4000, Price: 150}}

// BundleLookupKey is the Stripe lookup key of a bundle: staqly_credits_x_1000.
func BundleLookupKey(provider string, posts int64) string {
	return fmt.Sprintf("staqly_credits_%s_%d", provider, posts)
}

// xUnits: how many posts one post or reply to X counts as (its text as the channel sends it).
func xUnits(text string) int64 {
	if providers.HasLiveLink(text) {
		return XLinkPostWeight
	}
	return 1
}

// XPosts is a workspace's posts to X this period: the plan's included ones, bought credits and what's left.
type XPosts struct {
	// Period: the month the included posts are counted in (YYYY-MM); PeriodEnd: when they reset.
	Period    string `json:"period"`
	PeriodEnd string `json:"period_end"`
	// Unlimited: no allowance applies (a local install).
	Unlimited bool  `json:"unlimited"`
	Included  int64 `json:"included"`
	// Used: posts counted this period (included, credits and any overage).
	Used         int64 `json:"used"`
	IncludedLeft int64 `json:"included_left"`
	// CreditsLeft: bought posts not used yet; CreditsExpireAt: when the soonest of them expire.
	CreditsLeft     int64    `json:"credits_left"`
	CreditsExpireAt *string  `json:"credits_expire_at"`
	Left            int64    `json:"left"`
	Bundles         []Bundle `json:"bundles"`
	Currency        string   `json:"currency"`
	// LinkPostWeight: how many posts a post with a live link counts as.
	LinkPostWeight int64 `json:"link_post_weight"`
}

// monthEnd: the start of the next calendar month (UTC), when the included posts reset.
func (s *Service) monthEnd() time.Time {
	n := s.Now().UTC()
	return time.Date(n.Year(), n.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

func (s *Service) xPosts(ctx context.Context, q *db.Queries, workspaceID string) (XPosts, error) {
	out := XPosts{Period: s.period(), PeriodEnd: formatTime(s.monthEnd()), Bundles: XBundles, Currency: Currency, LinkPostWeight: XLinkPostWeight}
	plan, err := s.planFor(ctx, q, workspaceID)
	if err != nil {
		return out, err
	}
	used, err := q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: workspaceID, Provider: ProviderX, Period: out.Period})
	if err != nil {
		return out, err
	}
	out.Used = used
	if plan.Limits.XPostsPerPeriod == Unlimited {
		out.Unlimited, out.Included, out.Left = true, Unlimited, Unlimited
		return out, nil
	}
	fromIncluded, err := q.SumIncludedUsage(ctx, db.SumIncludedUsageParams{WorkspaceID: workspaceID, Provider: ProviderX, Period: out.Period})
	if err != nil {
		return out, err
	}
	out.Included = plan.Limits.XPostsPerPeriod
	out.IncludedLeft = max(out.Included-fromIncluded, 0)
	grants, err := q.ListLiveCreditGrants(ctx, db.ListLiveCreditGrantsParams{WorkspaceID: workspaceID, Provider: ProviderX, ExpiresAt: s.now()})
	if err != nil {
		return out, err
	}
	for _, g := range grants {
		out.CreditsLeft += g.Units - g.Used
		if out.CreditsExpireAt == nil || g.ExpiresAt < *out.CreditsExpireAt {
			e := g.ExpiresAt
			out.CreditsExpireAt = &e
		}
	}
	out.Left = out.IncludedLeft + out.CreditsLeft
	return out, nil
}

// GetXPosts: GET /v1/billing/x-posts.
func (s *Service) GetXPosts(ctx context.Context, p Principal) (XPosts, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return XPosts{}, err
	}
	return s.xPosts(ctx, s.Q, p.WorkspaceID)
}

// errXExhausted: the allowance can't cover a post; the date is when the included posts reset.
func (s *Service) errXExhausted(x XPosts, need int64) *Error {
	reset, _ := time.Parse(timeFormat, x.PeriodEnd)
	return &Error{Code: "x_posts_exhausted", Status: 402,
		Message: fmt.Sprintf("Your X posts for this period are used up. Buy more or wait until %s.", reset.Format("Jan 2")),
		Details: map[string]any{"needed": need, "left": x.Left, "period_end": x.PeriodEnd}}
}

// checkX fails when the workspace can't publish need more posts to X (a cloud plan's allowance).
func (s *Service) checkX(ctx context.Context, q *db.Queries, workspaceID string, need int64) error {
	if need <= 0 || s.Cfg.Mode == ModeLocal {
		return nil
	}
	x, err := s.xPosts(ctx, q, workspaceID)
	if err != nil || x.Unlimited || x.Left >= need {
		return err
	}
	return s.errXExhausted(x, need)
}

// meterX charges a reserved or published post/reply to X once (ref: post:<post>:<channel> or reply:<post>:<channel>:<n>):
// from the included posts first, then bought credits, oldest grant first. What neither covers is recorded as
// overage (a post already live is never undone). In the caller's transaction.
func (s *Service) meterX(ctx context.Context, q *db.Queries, workspaceID, ref string, units int64) error {
	if s.Cfg.Mode == ModeLocal || units <= 0 {
		return nil
	}
	x, err := s.xPosts(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	fromIncluded := min(units, x.IncludedLeft)
	if x.Unlimited {
		fromIncluded = units
	}
	rest := units - fromIncluded
	type draw struct {
		id string
		n  int64
	}
	var draws []draw
	if rest > 0 {
		grants, err := q.ListLiveCreditGrants(ctx, db.ListLiveCreditGrantsParams{WorkspaceID: workspaceID, Provider: ProviderX, ExpiresAt: s.now()})
		if err != nil {
			return err
		}
		for _, g := range grants {
			if rest == 0 {
				break
			}
			n := min(rest, g.Units-g.Used)
			draws = append(draws, draw{g.ID, n})
			rest -= n
		}
	}
	fromCredits := units - fromIncluded - rest
	n, err := q.InsertUsageEvent(ctx, db.InsertUsageEventParams{WorkspaceID: workspaceID, Provider: ProviderX, Ref: ref, Period: x.Period,
		Units: units, Included: fromIncluded, Credits: fromCredits, Overage: rest, CreatedAt: s.now()})
	if err != nil || n == 0 {
		return err // n == 0: counted already (a retried job)
	}
	for _, d := range draws {
		if err := q.UseCreditGrant(ctx, db.UseCreditGrantParams{Used: d.n, ID: d.id}); err != nil {
			return err
		}
	}
	return nil
}

// xUnitsForPost: the posts publishing a post would count on X: its main post and thread parts, on every X
// channel it still goes to, with each channel's links mode applied.
func (s *Service) xUnitsForPost(ctx context.Context, q *db.Queries, postID string) (int64, string, error) {
	row, err := q.GetPostByID(ctx, postID)
	if err != nil {
		return 0, "", err
	}
	targets, err := q.ListTargets(ctx, postID)
	if err != nil {
		return 0, "", err
	}
	var units int64
	for _, t := range targets {
		if t.Status == "published" {
			continue
		}
		ch, err := q.GetChannelByID(ctx, t.ChannelID)
		if err != nil || ch.Provider != ProviderX {
			continue
		}
		text := row.Content
		if t.Content.Valid {
			text = t.Content.String
		}
		links := channelLinks(ch)
		main, _ := providers.ApplyLinks(links, text)
		units += xUnits(main)
		for _, part := range targetThread(row.Thread, t.Settings) {
			reply, _ := providers.ApplyLinks(links, part.Content)
			units += xUnits(reply)
		}
	}
	return units, row.WorkspaceID, nil
}

// checkAllowancesForPost: block early, when a post is published or scheduled within this period and the X or
// LinkedIn allowances can't cover it. Publishing re-checks each post and reply (posts scheduled later may use
// the room up).
func (s *Service) checkAllowancesForPost(ctx context.Context, q *db.Queries, postID string, at time.Time) error {
	if s.Cfg.Mode == ModeLocal || !at.Before(s.monthEnd()) {
		return nil
	}
	units, ws, err := s.xUnitsForPost(ctx, q, postID)
	if err != nil {
		return err
	}
	if err := s.checkX(ctx, q, ws, units); err != nil {
		return err
	}
	row, err := q.GetPostByID(ctx, postID)
	if err != nil {
		return err
	}
	posts, videos, err := s.linkedInForPost(ctx, q, row)
	if err != nil {
		return err
	}
	return s.checkLinkedIn(ctx, q, ws, posts, videos)
}

// CreditsCheckout starts a Stripe Checkout (one-time payment) for a bundle of extra posts and returns its URL.
func (s *Service) CreditsCheckout(ctx context.Context, p Principal, provider string, posts int64) (string, error) {
	if err := s.requireOwner(ctx, p); err != nil {
		return "", err
	}
	if !s.BillingEnabled() {
		return "", errBillingOff
	}
	if provider == "" {
		provider = ProviderX
	}
	var bundle *Bundle
	for _, b := range XBundles {
		if b.Posts == posts {
			bundle = &b
		}
	}
	if provider != ProviderX || bundle == nil {
		return "", invalid("posts", "pick a bundle: 200, 1000 or 4000 posts to x")
	}
	pr, err := s.lookupPrice(ctx, BundleLookupKey(provider, posts), "",
		fmt.Sprintf("the bundle of %d X posts is not set up for payment on this instance", posts))
	if err != nil {
		return "", err
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return "", err
	}
	customer, err := s.ensureCustomer(ctx, w)
	if err != nil {
		return "", err
	}
	back := s.planPageURL()
	var cs struct {
		URL string `json:"url"`
	}
	form := url.Values{
		"mode":                       {"payment"},
		"customer":                   {customer},
		"client_reference_id":        {w.ID},
		"line_items[0][price]":       {pr.id},
		"line_items[0][quantity]":    {"1"},
		"metadata[workspace_id]":     {w.ID},
		"metadata[staqly_kind]":      {"credits"},
		"metadata[staqly_provider]":  {provider},
		"metadata[staqly_posts]":     {strconv.FormatInt(posts, 10)},
		"invoice_creation[enabled]":  {"true"}, // a receipt-like invoice in the billing history
		"billing_address_collection": {"auto"},
		"tax_id_collection[enabled]": {"true"},
		"customer_update[name]":      {"auto"},
		"customer_update[address]":   {"auto"},
		"success_url":                {back + "?credits=success"},
		"cancel_url":                 {back + "?credits=canceled"},
	}
	s.managedCheckout(form)
	err = s.stripe(ctx, "POST", "/v1/checkout/sessions", form, &cs)
	return cs.URL, err
}

// creditSession is a paid Checkout session for a bundle, as the webhook carries it.
type creditSession struct {
	ID                string            `json:"id"`
	Mode              string            `json:"mode"`
	PaymentStatus     string            `json:"payment_status"`
	ClientReferenceID string            `json:"client_reference_id"`
	Metadata          map[string]string `json:"metadata"`
}

// creditsExpiry: bought posts last until the end of the billing period: the live subscription's period (a
// month, or a year on yearly billing), else the trial's end, else the end of this month. Read before the
// webhook's transaction (it may ask Stripe).
func (s *Service) creditsExpiry(ctx context.Context, workspaceID string) string {
	end := s.monthEnd()
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if err != nil {
		return formatTime(end)
	}
	if w.StripeSubscriptionID.Valid && liveSubscription(w.SubscriptionStatus.String) {
		var sub stripeSubscription
		if err := s.stripe(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(w.StripeSubscriptionID.String), nil, &sub); err == nil && sub.periodEnd() > 0 {
			return formatTime(time.Unix(sub.periodEnd(), 0))
		}
	}
	if w.PlanExpiresAt.Valid && w.PlanExpiresAt.String > s.now() {
		return w.PlanExpiresAt.String
	}
	return formatTime(end)
}

// grantCredits records a paid bundle once (the session id is the grant's unique source).
func (s *Service) grantCredits(ctx context.Context, q *db.Queries, cs creditSession, expiresAt string) error {
	ws := cs.Metadata["workspace_id"]
	if ws == "" {
		ws = cs.ClientReferenceID
	}
	posts, _ := strconv.ParseInt(cs.Metadata["staqly_posts"], 10, 64)
	provider := cs.Metadata["staqly_provider"]
	if ws == "" || posts <= 0 || provider != ProviderX || cs.PaymentStatus != "paid" {
		s.logger(ctx).Warn("stripe: credits checkout not granted", "session", cs.ID, "workspace", ws, "posts", posts, "status", cs.PaymentStatus)
		return nil
	}
	if _, err := q.GetWorkspaceBilling(ctx, ws); isNoRows(err) {
		return nil // workspace deleted
	}
	n, err := q.InsertCreditGrant(ctx, db.InsertCreditGrantParams{ID: newID("crg"), WorkspaceID: ws, Provider: provider, Units: posts,
		Source: "stripe:" + cs.ID, ExpiresAt: expiresAt, CreatedAt: s.now()})
	if err != nil || n == 0 {
		return err
	}
	actor := Principal{WorkspaceID: ws, Kind: PrincipalSystem, ID: "stripe", Name: "Stripe"}
	_, err = s.emit(ctx, q, actor, "workspace.credits_added", "", "", map[string]any{"provider": provider, "posts": posts, "expires_at": expiresAt})
	return err
}

// decodeCreditSession: a Checkout session object that paid for credits, or nil.
func decodeCreditSession(raw json.RawMessage) *creditSession {
	var cs creditSession
	if json.Unmarshal(raw, &cs) != nil || cs.Mode != "payment" || cs.Metadata["staqly_kind"] != "credits" {
		return nil
	}
	return &cs
}

// meterTarget counts a published X or LinkedIn target (its main post) once.
func (s *Service) meterTarget(ctx context.Context, q *db.Queries, row db.Post, t db.PostTarget, channelID string) error {
	ref := "post:" + row.ID + ":" + channelID
	ch, err := q.GetChannelByID(ctx, channelID)
	if err == nil && isLinkedIn(ch.Provider) {
		video, err := hasVideo(ctx, q, row.WorkspaceID, row.Media)
		if err != nil {
			return err
		}
		if err := s.meterLinkedIn(ctx, q, row.WorkspaceID, ref, video); err != nil {
			return err
		}
		return q.DeletePublishingAllowanceReservation(ctx, db.DeletePublishingAllowanceReservationParams{WorkspaceID: row.WorkspaceID, Ref: ref})
	}
	if err != nil || ch.Provider != ProviderX {
		return nil
	}
	text := row.Content
	if t.Content.Valid {
		text = t.Content.String
	}
	text, _ = providers.ApplyLinks(channelLinks(ch), text)
	if err := s.meterX(ctx, q, row.WorkspaceID, ref, xUnits(text)); err != nil {
		return err
	}
	return q.DeletePublishingAllowanceReservation(ctx, db.DeletePublishingAllowanceReservationParams{WorkspaceID: row.WorkspaceID, Ref: ref})
}

// errorMessage: an error's text for a target or reply (an *Error's message without its code).
func errorMessage(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Message
	}
	return err.Error()
}

// creditRefund is what a charge.refunded or charge.dispute.created event says about one payment.
type creditRefund struct {
	PaymentIntent string
	Amount, Taken int64 // the charge and what was refunded or disputed of it, in cents
}

// decodeCreditRefund reads a refunded charge (amount_refunded is the total so far) or a dispute (its
// amount, on top of what the charge had refunded: the charge is read from Stripe).
func (s *Service) decodeCreditRefund(ctx context.Context, typ string, raw json.RawMessage) (creditRefund, error) {
	var obj struct {
		Charge         string `json:"charge"`
		PaymentIntent  string `json:"payment_intent"`
		Amount         int64  `json:"amount"`
		AmountRefunded int64  `json:"amount_refunded"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return creditRefund{}, invalid("body", "bad charge object")
	}
	r := creditRefund{PaymentIntent: obj.PaymentIntent, Amount: obj.Amount, Taken: obj.AmountRefunded}
	if typ == "charge.dispute.created" {
		var ch struct {
			PaymentIntent  string `json:"payment_intent"`
			Amount         int64  `json:"amount"`
			AmountRefunded int64  `json:"amount_refunded"`
		}
		if obj.Charge == "" {
			return creditRefund{}, nil
		}
		if err := s.stripe(ctx, "GET", "/v1/charges/"+url.PathEscape(obj.Charge), nil, &ch); err != nil {
			return creditRefund{}, err
		}
		r = creditRefund{PaymentIntent: firstNonEmpty(obj.PaymentIntent, ch.PaymentIntent), Amount: ch.Amount,
			Taken: min(ch.Amount, ch.AmountRefunded+obj.Amount)}
	}
	return r, nil
}

// creditSessionOf: the credits Checkout session that took this payment, or nil (a subscription invoice, or
// a payment staqly didn't start).
func (s *Service) creditSessionOf(ctx context.Context, paymentIntent string) (*creditSession, error) {
	if paymentIntent == "" {
		return nil, nil
	}
	var list struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := s.stripe(ctx, "GET", "/v1/checkout/sessions?limit=1&payment_intent="+url.QueryEscape(paymentIntent), nil, &list); err != nil {
		return nil, err
	}
	if len(list.Data) == 0 {
		return nil, nil
	}
	return decodeCreditSession(list.Data[0]), nil
}

// revokeCredits takes back the refunded share of a bought bundle's posts that weren't used yet (rounded in
// the customer's favour). Idempotent: it sets the grant from the totals Stripe sends, and only lowers it.
func (s *Service) revokeCredits(ctx context.Context, q *db.Queries, cs creditSession, r creditRefund, eventType string) error {
	posts, _ := strconv.ParseInt(cs.Metadata["staqly_posts"], 10, 64)
	if posts <= 0 || r.Amount <= 0 || r.Taken <= 0 {
		return nil
	}
	keep := posts - posts*min(r.Taken, r.Amount)/r.Amount
	n, err := q.LowerCreditGrant(ctx, "stripe:"+cs.ID, keep)
	if err == nil && n > 0 {
		s.logger(ctx).Info("stripe: credits taken back", "session", cs.ID, "event", eventType, "posts", posts, "kept", keep,
			"refunded_cents", r.Taken, "charge_cents", r.Amount)
	}
	return err
}
