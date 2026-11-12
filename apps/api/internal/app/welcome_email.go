package app

import (
	"context"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Welcome emails (cloud mode only; local mode has no plans, people or mail of its own). A workspace's owners get
// one email when its free trial starts and one when it first pays for a plan:
//
//   - trial: Stripe's Starter trial (the subscription is first seen trialing), or, on a server without billing,
//     the server-side trial a new workspace starts with;
//   - paid: the subscription is first seen active, whether after a trial or from the first day.
//
// Once per workspace, for good, with nothing new to store: the first time is read from what the transaction that
// applies the subscription records anyway (workspace_trials: a row once a trial was seen, paid_at once it was seen
// paying, kept for good), and the jobs that send (one per owner) are queued in that same transaction. So a
// replayed or concurrent Stripe event, an upgrade, a renewal, a card fixed after a failed payment and a new
// subscription after a cancellation send nothing, and a restart loses nothing. A plan the operator sets (server
// set-plan, the admin panel) is not a welcome: there is no card, no interval and no invoice to tell about. The
// email is written when it is sent, from the workspace as it is then, and not sent at all when that is no longer
// true (a trial ended at once for its card).
//
// The same job tells the owners about a payment that failed (applySubscription queues both, in its transaction):
//
//   - payment failed: the subscription is first seen past_due, once per failure (not again while it stays so,
//     again when it fails after it was paid); not sent when the payment went through before the email did;
//   - subscription ended: Stripe ended it (or holds it unpaid) because the payment kept failing, once per
//     subscription (subscription_ended_for remembers it); the workspace is read-only until it has a plan again.
//
// A payment put right sends nothing: Stripe's receipt says so.

const (
	welcomeTrial   = "trial"
	welcomePaid    = "paid"          // after a trial: the first steps were in the trial's email
	welcomePaidNew = "paid_no_trial" // paid from the first day: with the first steps

	emailPaymentFailed     = "payment_failed"
	emailSubscriptionEnded = "subscription_ended"

	// welcomeTrialDelay: the trial's email waits, so a trial ended right after it began (guardCard) says nothing.
	welcomeTrialDelay = time.Minute
)

// welcomeSubscription runs in applySubscription's transaction, before settleTrial, for a subscription that is
// being applied: cur is the workspace before it. A trial is new when none was seen here yet; a payment is the
// first when the workspace was never seen paying. What the workspace has on record counts too, for subscriptions
// from before trials and payments were recorded.
func (s *Service) welcomeSubscription(ctx context.Context, q *db.Queries, cur db.GetWorkspaceBillingRow, sub stripeSubscription) error {
	t, err := q.GetWorkspaceTrial(ctx, cur.ID)
	seen := err == nil // a trial (or a payment) was seen here before
	if err != nil && !isNoRows(err) {
		return err
	}
	was := cur.SubscriptionStatus.String
	paid := t.PaidAt.Valid || was == "active" || was == "past_due"
	var kind string
	switch {
	case sub.Status == "trialing" && !seen && was != "trialing":
		kind = welcomeTrial
	case sub.Status == "active" && !paid && seen:
		kind = welcomePaid
	case sub.Status == "active" && !paid:
		kind = welcomePaidNew
	default:
		return nil
	}
	interval := ""
	if i, ok := s.planItem(sub); ok {
		interval = sub.Items.Data[i].Price.Recurring.Interval
	}
	return s.queueWelcome(ctx, q, cur.ID, kind, interval)
}

// welcomeServerTrial runs in the transaction that creates a workspace and its owner, so once: without billing
// the workspace's trial starts right there (with billing it starts at Stripe: welcomeSubscription).
func (s *Service) welcomeServerTrial(ctx context.Context, q *db.Queries, workspaceID string) error {
	if s.BillingEnabled() {
		return nil
	}
	return s.queueWelcome(ctx, q, workspaceID, welcomeTrial, "")
}

// queueWelcome queues one email per owner (the people who manage billing), in the caller's transaction. Without
// mail nothing is queued, and mail set up later doesn't welcome anyone to a trial half over.
func (s *Service) queueWelcome(ctx context.Context, q *db.Queries, workspaceID, kind, interval string) error {
	if s.Cfg.Mode != ModeCloud || !s.mailConfigured() {
		return nil
	}
	members, err := q.ListMembers(ctx, workspaceID)
	if err != nil {
		return err
	}
	at := s.Now()
	if kind == welcomeTrial {
		at = at.Add(welcomeTrialDelay)
	}
	for _, m := range members {
		if m.Role != RoleOwner {
			continue
		}
		if err := s.enqueue(ctx, q, workspaceID, "welcome_email", "welcome_email:"+workspaceID+":"+kind+":"+m.UserID,
			jobPayload{UserID: m.UserID, Welcome: kind, Interval: interval}, at, 0); err != nil {
			return err
		}
	}
	return nil
}

// runWelcomeEmail is the welcome_email job: one email to one owner. A failed send is retried like any job (three
// attempts), then dropped: a welcome is not worth a fourth.
func (s *Service) runWelcomeEmail(ctx context.Context, workspaceID string, p jobPayload) error {
	if !s.mailConfigured() {
		return nil
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, workspaceID)
	if isNoRows(err) {
		return nil // the workspace was deleted
	}
	if err != nil {
		return err
	}
	m, err := s.Q.GetMember(ctx, db.GetMemberParams{WorkspaceID: workspaceID, UserID: p.UserID})
	if isNoRows(err) || (err == nil && m.Role != RoleOwner) {
		return nil // no longer an owner
	}
	if err != nil {
		return err
	}
	u, err := s.Q.GetUser(ctx, p.UserID)
	if err != nil || u.Email == "" {
		return err
	}
	plan, ok := PlanByID(w.Plan)
	if !ok {
		return nil
	}
	f := welcomeFacts{Kind: p.Welcome, Workspace: w.Name, Plan: plan.Name, Interval: p.Interval, Base: s.webURL()}
	// day: a stored time in the workspace's time zone (zero when it can't be read).
	day := func(v string) time.Time {
		t, err := time.Parse(timeFormat, v)
		if err != nil {
			return time.Time{}
		}
		if ws, err := s.Q.GetWorkspace(ctx, workspaceID); err == nil {
			if loc, err := time.LoadLocation(ws.Timezone); err == nil {
				t = t.In(loc)
			}
		}
		return t
	}
	switch p.Welcome {
	case welcomeTrial:
		ends := s.trialEndsAt(w)
		if ends == nil {
			return nil // the trial is over already (ended for its card, cancelled)
		}
		f.TrialEnds = day(*ends)
		f.Card = w.SubscriptionStatus.Valid // Stripe's trial has a card on file; the server-side one has none
		f.Steps = true
	case emailPaymentFailed:
		if w.SubscriptionStatus.String != "past_due" {
			return nil // paid meanwhile, or ended (which has its own email)
		}
		f.ReadOnly = day(w.PlanExpiresAt.String)
	case emailSubscriptionEnded:
		if liveSubscription(w.SubscriptionStatus.String) {
			return nil // a plan again already
		}
	case welcomePaid, welcomePaidNew:
		if w.SubscriptionStatus.String != "active" {
			return nil // not paying after all (the payment failed, the plan was cancelled)
		}
		f.Steps = p.Welcome == welcomePaidNew
	default:
		return nil
	}
	subject, d := welcomeEmail(f)
	return s.sendSystemEmailDoc(ctx, u.Email, subject, d, nil)
}

// welcomeFacts is what a welcome email says. Nothing of the workspace's content: its name, its plan and dates.
type welcomeFacts struct {
	Kind      string // welcomeTrial, or a paid one
	Workspace string
	Plan      string    // the plan's name
	TrialEnds time.Time // trial: its last day, in the workspace's time zone
	Card      bool      // trial: a card is on file and is charged when the trial ends (else: read-only then)
	Interval  string    // paid: IntervalMonth or IntervalYear ("" when unknown)
	ReadOnly  time.Time // payment failed: the day the workspace becomes read-only if it stays unpaid (zero: unknown)
	Steps     bool      // show the first steps
	Base      string    // the app's address
}

// welcomeEmail writes the email: its subject and the document in the shared look.
func welcomeEmail(f welcomeFacts) (string, emailDoc) {
	planPage := f.Base + "/settings/plan"
	d := emailDoc{Footer: []string{"You get this email once, because you are an owner of the workspace " + f.Workspace + "."}}
	subject := ""
	const readOnly = "your posts and settings stay, but nothing new can be written or published"
	switch f.Kind {
	case emailPaymentFailed:
		subject = "The payment for staqly " + f.Plan + " failed"
		d.Title = "The payment for " + f.Plan + " failed"
		d.Preheader = "Update the card to keep the workspace open."
		d.Footer = []string{"You get this email because you are an owner of the workspace " + f.Workspace + "."}
		when := "If it stays unpaid"
		if !f.ReadOnly.IsZero() {
			when = "If it is still unpaid on **" + f.ReadOnly.Format("2 January 2006") + "**"
		}
		d.Paragraphs = []string{
			"The payment for the **" + f.Plan + "** plan of the workspace **" + f.Workspace + "** didn't go through.",
			"Stripe tries the card again over the next days, so there is nothing to do if the card is fine. " + when +
				", the workspace becomes read-only: " + readOnly + ".",
			"To pay with another card, update it in Settings → Plan & usage.",
		}
		d.Button = &emailLink{Text: "Update the card", URL: planPage}
		return subject, d
	case emailSubscriptionEnded:
		subject = "Your staqly workspace is read-only"
		d.Title = "The workspace is read-only"
		d.Preheader = "Choose a plan to open it again."
		d.Footer = []string{"You get this email because you are an owner of the workspace " + f.Workspace + "."}
		d.Paragraphs = []string{
			"The payment for the **" + f.Plan + "** plan of the workspace **" + f.Workspace + "** kept failing, so the subscription has ended.",
			"The workspace is read-only: " + readOnly + ".",
			"To open it again, choose a plan in Settings → Plan & usage. It opens as soon as the plan is paid.",
		}
		d.Button = &emailLink{Text: "Choose a plan", URL: planPage}
		return subject, d
	}
	if f.Kind == welcomeTrial {
		day := f.TrialEnds.Format("2 January 2006")
		subject = "Your staqly free trial has started"
		d.Title = "Your free trial has started"
		d.Preheader = f.Plan + " is free until " + day + "."
		d.Paragraphs = []string{"The workspace **" + f.Workspace + "** is on the **" + f.Plan + "** plan, free until **" + day + "**."}
		if f.Card {
			d.Paragraphs = append(d.Paragraphs, "On that day your card is charged for "+f.Plan+" and the plan goes on. "+
				"If you don't want to pay, cancel before then in Settings → Plan & usage: nothing is charged.")
		} else {
			d.Paragraphs = append(d.Paragraphs, "After that day the workspace becomes read-only until it has a plan: "+
				"your posts and settings stay, but nothing new can be written or published.")
		}
		d.Button = &emailLink{Text: "Open staqly", URL: f.Base + "/"}
		d.Links = []emailLink{{Text: "See your plan and usage", URL: planPage}}
	} else {
		subject = "You are on staqly " + f.Plan
		d.Title = "You are on " + f.Plan
		billed := map[string]string{IntervalMonth: ", billed **monthly**", IntervalYear: ", billed **yearly**"}[f.Interval]
		d.Preheader = "Your plan is active."
		d.Paragraphs = []string{
			"Thank you. The workspace **" + f.Workspace + "** is now on the **" + f.Plan + "** plan" + billed + ".",
			"Invoices, the card you pay with and the plan itself are in Settings → Plan & usage. You can change or cancel the plan there at any time.",
		}
		d.Button = &emailLink{Text: "Manage billing", URL: planPage}
	}
	if f.Steps {
		d.Paragraphs = append(d.Paragraphs, "Three things to do first:")
		d.Rows = []emailRow{
			{Label: "Connect a channel", Detail: "Sign in to the platforms you post to.",
				Link: &emailLink{Text: "Open Channels", URL: f.Base + "/channels"}},
			{Label: "Write your first post", Detail: "Draft it now, then publish it or schedule it.",
				Link: &emailLink{Text: "New post", URL: f.Base + "/posts?compose=1"}},
			{Label: "Look at the calendar", Detail: "See the week and plan what goes out when.",
				Link: &emailLink{Text: "Open the calendar", URL: f.Base + "/schedule"}},
		}
	}
	return subject, d
}
