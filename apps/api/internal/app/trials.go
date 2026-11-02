package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Repeated free trials (cloud with billing only; local mode and servers without billing never run any of this).
// Starter's trial is once per workspace, so someone could let a trial end unpaid, sign up again with another
// address and start over with the same accounts or card. A workspace's channels and cards are kept as fingerprints:
// an HMAC with a key derived from the server's secret key, never the platform's id or Stripe's card fingerprint.
//
//   - Who a channel belongs to: a personal account (an X user, a Bluesky DID, a LinkedIn member...) is the account
//     ("account"); a space many people share (a Slack workspace, a Facebook Page, a LinkedIn Page...) is the platform
//     user who authorized the connection ("person"), never the space, so colleagues aren't taken for one another.
//     A space with nobody behind the connection (a bot token, a webhook, Telegram's /connect) has no fingerprint.
//   - The card is the strong signal: a workspace whose card an unpaid trial used gets no trial (the plan picker says
//     so before Checkout); one whose card is only known once Checkout completed has its trial ended at once.
//   - Channels never charge anyone by themselves: one personal account, or two people behind shared spaces, that
//     an unpaid trial had pause the trial for review (no publishing, no new channels; drafts and settings stay)
//     until the person chooses to start the paid plan or the operator clears it.
//
// Only trials that ended unpaid in the last 180 days count; a workspace that paid is never checked.

const (
	fingerprintAccount = "account"
	fingerprintPerson  = "person"
	fingerprintCard    = "card"

	// trialWindow: how long an unpaid trial counts against the accounts and cards it used.
	trialWindow = 180 * 24 * time.Hour
	// trialRevokedShown: how long the billing state keeps saying a trial was ended for its card once it is paid.
	trialRevokedShown = 14 * 24 * time.Hour
)

// Why a trial was ended early (Billing.TrialRevoked) or paused (Billing.TrialReview).
const (
	TrialRevokedCard    = "card"
	TrialReviewChannels = "channels"
)

// TrialCardUsed: Billing.Trial when the workspace's card was already used in an unpaid trial (no trial).
const TrialCardUsed = "card_used"

// SupportEmail: where someone whose trial was paused by mistake writes.
const SupportEmail = "support@staqly.ai"

// trialChecks: repeated trials are looked for only where there are trials to pay for: cloud with billing.
func (s *Service) trialChecks() bool { return s.BillingEnabled() }

func (s *Service) trialSince() string { return formatTime(s.Now().Add(-trialWindow)) }

// fingerprint: an HMAC of kind and value with a key of its own derived from the server's secret key, so the
// value can't be read back or matched without the key ("" without a value or a key).
func (s *Service) fingerprint(kind, value string) string {
	if value == "" || len(s.Cfg.SecretKey) == 0 {
		return ""
	}
	k := hmac.New(sha256.New, s.Cfg.SecretKey)
	k.Write([]byte("staqly trial fingerprints v1"))
	mac := hmac.New(sha256.New, k.Sum(nil))
	mac.Write([]byte(kind + "\x00" + value))
	return hex.EncodeToString(mac.Sum(nil))
}

// providerFamilies: platforms connected in more than one way are one account: the same Bluesky account by OAuth or
// an app password, the same Mastodon account by OAuth or a token, the same Discord server by the bot or a webhook.
var providerFamilies = map[string]string{
	"bluesky_app_password": "bluesky",
	"mastodon_token":       "mastodon",
	"discord_webhook":      "discord",
}

// accountIDKeys: for personal accounts, the credentials that carry the platform's own id for the account, which
// never changes (a handle can be renamed), in order of preference. A family not listed here and not a shared space
// falls back to the account URL (Mastodon) or the handle.
var accountIDKeys = map[string][]string{
	"bluesky":            {"did"},
	"x":                  {"user_id"},
	"threads":            {"user_id"},
	"tiktok":             {"open_id"},
	"instagram":          {"user_id"},
	"instagram_business": {"user_id"},
	"linkedin":           {"person_id"},
	"youtube":            {"channel_id", "google_id"},
	"twitch":             {"user_id"},
	"kick":               {"user_id"},
	"tumblr":             {"user", "blog"},
	"reddit":             {"username"},
	"pinterest":          {"username"},
}

// sharedSpaces: platforms whose channel is a space many people may use, with who authorized the connection ("" when
// nobody did: a bot or a webhook). Slack's user ids are per workspace, so the team goes with them.
var sharedSpaces = map[string]func(providers.Credentials) string{
	"slack": func(c providers.Credentials) string {
		if c["team_id"] == "" || c["user_id"] == "" {
			return ""
		}
		return c["team_id"] + ":" + c["user_id"]
	},
	"discord":         func(providers.Credentials) string { return "" }, // a bot added to a server; a webhook
	"telegram":        func(providers.Credentials) string { return "" }, // a bot in a chat (/connect or a token)
	"whop":            func(providers.Credentials) string { return "" }, // a company's API key
	"facebook":        func(c providers.Credentials) string { return c["authorized_by"] },
	"linkedin_page":   func(c providers.Credentials) string { return c["person_id"] },
	"google_business": func(c providers.Credentials) string { return c["google_id"] },
}

// channelIdentity: what a connected channel says about who connected it: the kind (account or person) and the
// platform's id for it, by family; "" when nothing tells people apart (your own automation, a bot, a webhook).
func channelIdentity(providerID string, creds providers.Credentials, profile providers.Profile) (string, string) {
	if providerID == "custom" {
		return "", ""
	}
	family := providerID
	if f, ok := providerFamilies[providerID]; ok {
		family = f
	}
	if who, ok := sharedSpaces[family]; ok {
		id := strings.ToLower(strings.TrimSpace(who(creds)))
		if id == "" {
			return "", ""
		}
		return fingerprintPerson, family + "\x00" + id
	}
	for _, k := range accountIDKeys[family] {
		if v := strings.TrimSpace(creds[k]); v != "" {
			return fingerprintAccount, family + "\x00" + strings.ToLower(v)
		}
	}
	if family == "mastodon" && profile.ProfileURL != "" { // the account's URL on its server
		return fingerprintAccount, family + "\x00" + strings.ToLower(strings.TrimRight(profile.ProfileURL, "/"))
	}
	if h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(profile.Handle), "@")); h != "" {
		return fingerprintAccount, family + "\x00" + h
	}
	if u := strings.ToLower(strings.TrimSpace(profile.ProfileURL)); u != "" {
		return fingerprintAccount, family + "\x00" + u
	}
	return "", ""
}

// channelPrint is a channel's fingerprint and its kind (zero: none).
type channelPrint struct{ kind, fp string }

func (s *Service) channelFingerprint(providerID string, creds providers.Credentials, profile providers.Profile) channelPrint {
	if !s.trialChecks() {
		return channelPrint{}
	}
	kind, id := channelIdentity(providerID, creds, profile)
	if id == "" {
		return channelPrint{}
	}
	return channelPrint{kind: kind, fp: s.fingerprint(kind, id)}
}

// recordFingerprint keeps a fingerprint for the workspace (in the caller's transaction).
func (s *Service) recordFingerprint(ctx context.Context, q *db.Queries, workspaceID, kind, fp string) error {
	if fp == "" {
		return nil
	}
	return q.RecordTrialFingerprint(ctx, db.RecordTrialFingerprintParams{Kind: kind, Fingerprint: fp, WorkspaceID: workspaceID, Now: s.now()})
}

// errTrialReview: what connecting a channel or publishing answers while the workspace's trial is paused.
var errTrialReview = &Error{Code: "trial_already_used", Status: 402,
	Message: "These accounts were used in an earlier free trial that wasn't paid for, so this workspace's free trial is paused: " +
		"publishing and new channels wait until you start your paid plan (Settings → Plan & usage). If this isn't right, write to " +
		SupportEmail + ".",
	Details: map[string]any{"reason": TrialReviewChannels}}

// workspaceTrial: the workspace's trial record (zero when it has none yet).
func workspaceTrial(ctx context.Context, q *db.Queries, workspaceID string) (db.WorkspaceTrial, error) {
	t, err := q.GetWorkspaceTrial(ctx, workspaceID)
	if isNoRows(err) {
		return db.WorkspaceTrial{WorkspaceID: workspaceID}, nil
	}
	return t, err
}

// trialPaused: the trial waits for review (it reused accounts of an unpaid trial) and nothing has settled it: no
// payment since, no operator clearing it.
func trialPaused(t db.WorkspaceTrial) bool {
	return t.ReviewAt.Valid && !t.PaidAt.Valid && !t.ClearedAt.Valid
}

// checkTrialReview: errTrialReview while the workspace's trial is paused (publishing and new channels wait).
func (s *Service) checkTrialReview(ctx context.Context, workspaceID string) error {
	if !s.trialChecks() || workspaceID == "" {
		return nil
	}
	t, err := workspaceTrial(ctx, s.Q, workspaceID)
	if err != nil {
		return err
	}
	if trialPaused(t) {
		return errTrialReview
	}
	return nil
}

// guardChannel runs before a channel is saved or reconnected, outside any transaction. While the trial is paused
// nothing connects. A workspace in its trial that connects a personal account an unpaid trial had (in the last 180
// days), or whose people behind shared spaces add up to two such matches, has its trial paused and the connect
// refused; nothing is charged. A workspace that paid, one the operator cleared, one not in a trial, and the
// workspace that had the account itself connect as usual.
func (s *Service) guardChannel(ctx context.Context, workspaceID string, cp channelPrint) error {
	if !s.trialChecks() {
		return nil
	}
	t, err := workspaceTrial(ctx, s.Q, workspaceID)
	if err != nil {
		return err
	}
	if trialPaused(t) {
		return errTrialReview
	}
	if cp.fp == "" || t.PaidAt.Valid || t.ClearedAt.Valid {
		return nil
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if err != nil || w.SubscriptionStatus.String != "trialing" {
		return err
	}
	since := nullStr(s.trialSince())
	n, err := s.Q.CountUnpaidTrialFingerprint(ctx, db.CountUnpaidTrialFingerprintParams{Kind: cp.kind, Fingerprint: cp.fp,
		WorkspaceID: workspaceID, Since: since})
	if err != nil || n == 0 {
		return err
	}
	if cp.kind == fingerprintPerson {
		// One person behind a shared space may be a colleague's coincidence: two are a pattern.
		matches, err := s.Q.CountWorkspaceUnpaidMatches(ctx, db.CountWorkspaceUnpaidMatchesParams{WorkspaceID: workspaceID,
			Kind: fingerprintPerson, Since: since})
		if err != nil {
			return err
		}
		has, err := s.Q.HasTrialFingerprint(ctx, db.HasTrialFingerprintParams{Kind: cp.kind, Fingerprint: cp.fp, WorkspaceID: workspaceID})
		if err != nil {
			return err
		}
		if has == 0 {
			matches++
		}
		if matches < 2 {
			return nil
		}
	}
	if err := s.pauseTrial(ctx, workspaceID, TrialReviewChannels); err != nil {
		return err
	}
	return errTrialReview
}

// pauseTrial records the review, with an event for the workspace and a content-free analytics event.
func (s *Service) pauseTrial(ctx context.Context, workspaceID, reason string) error {
	actor := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "billing", Name: "Billing"}
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.SetWorkspaceTrialReview(ctx, db.SetWorkspaceTrialReviewParams{WorkspaceID: workspaceID, Now: nullStr(s.now()),
			Reason: nullStr(reason)}); err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, actor, "workspace.trial_review", "", "", map[string]any{"state": "paused", "reason": reason}); err != nil {
			return err
		}
		s.trackAs(q, actor, "trial_paused", "reason", reason)
		return nil
	})
	if err == nil {
		s.logger(ctx).Info("billing: trial paused, it reused an unpaid trial's accounts", "workspace_id", workspaceID, "reason", reason)
	}
	return err
}

// endTrialNow asks Stripe to end the subscription's trial now, so the card is charged at once (outside any
// transaction), and answers the subscription as Stripe has it then. One end per subscription and purpose: two
// requests at once get the same answer back.
func (s *Service) endTrialNow(ctx context.Context, subscriptionID, purpose string) (stripeSubscription, error) {
	var sub stripeSubscription
	form := url.Values{"trial_end": {"now"}, "expand[]": {"default_payment_method"}}
	err := s.stripeKeyed(ctx, "POST", "/v1/subscriptions/"+url.PathEscape(subscriptionID), form, &sub,
		"staqly-trial-"+purpose+"-"+subscriptionID)
	return sub, err
}

// revokeTrial ends the trial of a workspace that pays with a card an unpaid trial used: Stripe charges it now. A
// charge that didn't go through ends the plan now (read-only until paid). It answers whether the charge went through.
func (s *Service) revokeTrial(ctx context.Context, workspaceID, subscriptionID, reason string) (bool, error) {
	sub, err := s.endTrialNow(ctx, subscriptionID, "revoke")
	if err != nil {
		return false, err
	}
	paid := sub.Status == "active"
	actor := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "billing", Name: "Billing"}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.SetWorkspaceTrialRevoked(ctx, db.SetWorkspaceTrialRevokedParams{WorkspaceID: workspaceID, Now: nullStr(s.now()),
			Reason: nullStr(reason)}); err != nil {
			return err
		}
		if sub.ID != "" {
			if err := s.applySubscription(ctx, q, sub, "trial.revoked"); err != nil {
				return err
			}
		}
		if !paid {
			cur, err := q.GetWorkspacePlan(ctx, workspaceID)
			if err != nil {
				return err
			}
			if err := s.changePlan(ctx, q, actor, cur.Plan, s.now(), "trial_revoked:"+reason); err != nil {
				return err
			}
		}
		if _, err := s.emit(ctx, q, actor, "workspace.trial_review", "", "", map[string]any{"state": "ended", "reason": reason,
			"charged": paid}); err != nil {
			return err
		}
		s.trackAs(q, actor, "trial_revoked", "reason", reason, "charged", paid)
		return nil
	})
	if err == nil {
		s.logger(ctx).Info("billing: trial ended early, its card was used in an unpaid trial", "workspace_id", workspaceID, "charged", paid)
	}
	return paid, err
}

// StartPaidPlan ends the workspace's free trial now and starts the paid plan: Stripe charges the card on file at
// once. The way out of a paused trial ("Start paid plan now"); any owner in a trial may use it. A charge that
// doesn't go through leaves the subscription unpaid (past due), to pay in the billing portal.
func (s *Service) StartPaidPlan(ctx context.Context, p Principal) (Billing, error) {
	if err := s.requireOwner(ctx, p); err != nil {
		return Billing{}, err
	}
	if !s.BillingEnabled() {
		return Billing{}, errBillingOff
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
	if err != nil {
		return Billing{}, err
	}
	if w.SubscriptionStatus.String != "trialing" || !w.StripeSubscriptionID.Valid {
		return Billing{}, conflict("this workspace is not in a free trial; choose a plan in Settings → Plan & usage")
	}
	sub, err := s.endTrialNow(ctx, w.StripeSubscriptionID.String, "start")
	if err != nil {
		return Billing{}, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if sub.ID != "" {
			if err := s.applySubscription(ctx, q, sub, "trial.ended"); err != nil {
				return err
			}
		}
		s.trackAs(q, p, "trial_ended_early", "charged", sub.Status == "active")
		return nil
	})
	if err != nil {
		return Billing{}, err
	}
	if sub.Status != "active" {
		return Billing{}, &Error{Code: "payment_failed", Status: 402,
			Message: "the card on file wasn't charged; update the payment method in the billing portal (Settings → Plan & usage → Manage billing)",
			Details: map[string]any{"reason": "unknown"}}
	}
	return s.GetBilling(ctx, p)
}

// ClearTrialReview is the operator's (server CLI `clear-trial-review`): the workspace's trial is no longer paused
// or ended for reuse, and it isn't checked again. An event records it.
func (s *Service) ClearTrialReview(ctx context.Context, workspaceID string) error {
	actor := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "cli", Name: "Server CLI"}
	return s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetWorkspace(ctx, workspaceID); err != nil {
			if isNoRows(err) {
				return notFound("workspace")
			}
			return err
		}
		if _, err := q.ClearWorkspaceTrialReview(ctx, db.ClearWorkspaceTrialReviewParams{WorkspaceID: workspaceID, Now: nullStr(s.now())}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, actor, "workspace.trial_review", "", "", map[string]any{"state": "cleared", "by": "operator"})
		return err
	})
}

// trialState: why the trial was ended early (the card; while unpaid, and for two weeks after paying so the
// dashboard can say why it was charged) and whether it is paused for review.
func (s *Service) trialState(ctx context.Context, workspaceID string) (revoked, review *string, err error) {
	if !s.trialChecks() {
		return nil, nil, nil
	}
	t, err := workspaceTrial(ctx, s.Q, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	if t.RevokedAt.Valid && t.RevokedReason.String != "" && !t.ClearedAt.Valid &&
		(!t.PaidAt.Valid || t.RevokedAt.String >= formatTime(s.Now().Add(-trialRevokedShown))) {
		revoked = &t.RevokedReason.String
	}
	if trialPaused(t) && t.ReviewReason.String != "" {
		review = &t.ReviewReason.String
	}
	return revoked, review, nil
}

// cardUsed: one of the workspace's cards was used in another workspace's unpaid trial (no trial for it).
func (s *Service) cardUsed(ctx context.Context, q *db.Queries, workspaceID string) (bool, error) {
	if !s.trialChecks() {
		return false, nil
	}
	t, err := workspaceTrial(ctx, q, workspaceID)
	if err != nil || t.ClearedAt.Valid {
		return false, err
	}
	n, err := q.CountWorkspaceUnpaidMatches(ctx, db.CountWorkspaceUnpaidMatchesParams{WorkspaceID: workspaceID, Kind: fingerprintCard,
		Since: nullStr(s.trialSince())})
	return n > 0, err
}

// settleTrial follows the subscription for repeated trials (in applySubscription's transaction): a card it pays
// with is fingerprinted; a trial is noted when seen; paying (active) marks the workspace paid and its fingerprints
// no longer count; a trial seen here that ended without ever paying marks them. A subscription whose trial wasn't
// seen (from before this shipped) never counts as an unpaid trial: it may well have been paid.
func (s *Service) settleTrial(ctx context.Context, q *db.Queries, workspaceID string, sub stripeSubscription) error {
	if !s.trialChecks() {
		return nil
	}
	if err := s.recordFingerprint(ctx, q, workspaceID, fingerprintCard, s.fingerprint(fingerprintCard, sub.cardFingerprint())); err != nil {
		return err
	}
	switch {
	case sub.Status == "trialing":
		return q.SetWorkspaceTrialSeen(ctx, db.SetWorkspaceTrialSeenParams{WorkspaceID: workspaceID, Now: s.now()})
	case sub.Status == "active":
		if err := q.SetWorkspacePaid(ctx, db.SetWorkspacePaidParams{WorkspaceID: workspaceID, Now: nullStr(s.now())}); err != nil {
			return err
		}
		return q.ClearTrialFingerprintsUnpaid(ctx, db.ClearTrialFingerprintsUnpaidParams{WorkspaceID: workspaceID, Now: s.now()})
	case sub.TrialEnd > 0 && (finalSubscription(sub.Status) || sub.Status == "unpaid"):
		t, err := q.GetWorkspaceTrial(ctx, workspaceID)
		if isNoRows(err) || (err == nil && t.PaidAt.Valid) {
			return nil // no trial seen here, or it was paid
		}
		if err != nil {
			return err
		}
		return s.markTrialUnpaid(ctx, q, workspaceID)
	}
	return nil
}

func (s *Service) markTrialUnpaid(ctx context.Context, q *db.Queries, workspaceID string) error {
	if err := q.SetWorkspaceTrialUnpaid(ctx, db.SetWorkspaceTrialUnpaidParams{WorkspaceID: workspaceID, Now: nullStr(s.now())}); err != nil {
		return err
	}
	return q.MarkTrialFingerprintsUnpaid(ctx, db.MarkTrialFingerprintsUnpaidParams{WorkspaceID: workspaceID, Now: nullStr(s.now())})
}

// guardCard runs after a subscription was applied (outside the transaction): a trial that pays with a card an
// unpaid trial used ends now (the card wasn't known before Checkout completed).
func (s *Service) guardCard(ctx context.Context, sub stripeSubscription) error {
	if !s.trialChecks() || sub.Status != "trialing" {
		return nil
	}
	return s.guardCardOf(ctx, sub.Customer, sub.Metadata["workspace_id"], sub.cardFingerprint())
}

// guardCardOf: the check of guardCard for a card of a customer (or a workspace named by the subscription).
func (s *Service) guardCardOf(ctx context.Context, customer, workspaceID, card string) error {
	fp := s.fingerprint(fingerprintCard, card)
	if fp == "" {
		return nil
	}
	if workspaceID == "" {
		id, err := s.Q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(customer))
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		workspaceID = id
	}
	n, err := s.Q.CountUnpaidTrialFingerprint(ctx, db.CountUnpaidTrialFingerprintParams{Kind: fingerprintCard, Fingerprint: fp,
		WorkspaceID: workspaceID, Since: nullStr(s.trialSince())})
	if err != nil || n == 0 {
		return err
	}
	t, err := workspaceTrial(ctx, s.Q, workspaceID)
	if err != nil || t.PaidAt.Valid || t.ClearedAt.Valid {
		return err
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if isNoRows(err) {
		return nil
	}
	if err != nil || w.SubscriptionStatus.String != "trialing" || !w.StripeSubscriptionID.Valid {
		return err
	}
	_, err = s.revokeTrial(ctx, workspaceID, w.StripeSubscriptionID.String, TrialRevokedCard)
	return err
}

// paymentMethodAttached: a card added to a customer (the billing portal, a new checkout) is fingerprinted, and
// checked while the workspace is in its trial.
func (s *Service) paymentMethodAttached(ctx context.Context, eventID string, raw json.RawMessage) error {
	var pm struct {
		Customer string `json:"customer"`
		Card     *struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"card"`
	}
	if err := json.Unmarshal(raw, &pm); err != nil || pm.Card == nil || pm.Card.Fingerprint == "" || pm.Customer == "" {
		return nil // not a card (or no customer): nothing to fingerprint
	}
	ws, err := s.Q.GetWorkspaceIDByStripeCustomer(ctx, nullStr(pm.Customer))
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if n, err := q.InsertStripeEvent(ctx, db.InsertStripeEventParams{ID: eventID, CreatedAt: s.now()}); err != nil || n == 0 {
			return err
		}
		return s.recordFingerprint(ctx, q, ws, fingerprintCard, s.fingerprint(fingerprintCard, pm.Card.Fingerprint))
	})
	if err != nil {
		return err
	}
	return s.guardCardOf(ctx, pm.Customer, ws, pm.Card.Fingerprint)
}

// settleTrialOnDelete: a workspace deleted during (or after) a trial it never paid for keeps its fingerprints,
// marked unpaid, so the next sign-up can't start over with the same accounts; a workspace that paid, or never
// had a trial, leaves none behind. In the deletion's transaction.
func (s *Service) settleTrialOnDelete(ctx context.Context, q *db.Queries, workspaceID string) error {
	if !s.trialChecks() {
		return nil
	}
	t, err := q.GetWorkspaceTrial(ctx, workspaceID)
	if isNoRows(err) {
		return q.DeleteTrialFingerprints(ctx, workspaceID) // no trial seen here
	}
	if err != nil {
		return err
	}
	if !t.PaidAt.Valid {
		return q.MarkTrialFingerprintsUnpaid(ctx, db.MarkTrialFingerprintsUnpaidParams{WorkspaceID: workspaceID, Now: nullStr(s.now())})
	}
	return q.DeleteTrialFingerprints(ctx, workspaceID)
}

// cardFingerprint: Stripe's fingerprint of the card the subscription is paid with, when its default payment
// method was expanded and is a card ("" otherwise: an id, another kind of payment method, none).
func (sub stripeSubscription) cardFingerprint() string {
	var pm struct {
		Card *struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"card"`
	}
	if len(sub.DefaultPaymentMethod) == 0 || sub.DefaultPaymentMethod[0] != '{' || json.Unmarshal(sub.DefaultPaymentMethod, &pm) != nil || pm.Card == nil {
		return ""
	}
	return pm.Card.Fingerprint
}
