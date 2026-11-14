package httpapi_test

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// welcomeEnv is a cloud server with accounts, a fake SMTP relay and (billing) a fake Stripe.
type welcomeEnv struct {
	*acctEnv
	f     *trialStripe
	clock *testClock
	mu    sync.Mutex
	mail  []smtpMsg // everything the relay received since the last look
}

func newWelcomeEnv(t *testing.T, billing, mail bool) *welcomeEnv {
	t.Helper()
	w := &welcomeEnv{acctEnv: newAccountsEnv(t, ""), clock: &testClock{t: time.Now().UTC()}}
	w.svc.Now = w.clock.now
	if billing {
		w.f = withTrialStripe(t, w.env)
	}
	if mail {
		w.mailOn(t)
	}
	return w
}

func (w *welcomeEnv) mailOn(t *testing.T) {
	t.Helper()
	host, port, inbox := fakeSMTP(t)
	go func() { // the relay's channel is small: keep it empty, so no send waits for the test to read
		for m := range inbox {
			w.mu.Lock()
			w.mail = append(w.mail, m)
			w.mu.Unlock()
		}
	}()
	prev := w.svc.Getenv
	w.svc.Getenv = func(k string) string {
		if v, ok := map[string]string{"MAIL_SMTP_HOST": host, "MAIL_SMTP_PORT": itoa(port), "MAIL_SMTP_TLS": "none",
			"MAIL_FROM": "staqly <noreply@example.com>"}[k]; ok {
			return v
		}
		return prev(k)
	}
}

// welcomes runs the jobs that are due within two minutes (the trial's email waits one) and answers the welcome
// emails that came, each as its recipient and the decoded message; other mail (invitations, "someone joined") is
// dropped. The clock goes back afterwards: Stripe's signatures are checked against it.
func (w *welcomeEnv) welcomes() []string {
	w.t.Helper()
	return w.received("Subject: Your staqly free trial has started", "Subject: You are on staqly")
}

// received is welcomes for emails with any of these texts.
func (w *welcomeEnv) received(texts ...string) []string {
	w.t.Helper()
	ctx := context.Background()
	w.svc.RunDueJobsOnce(ctx)
	w.clock.add(2 * time.Minute)
	for range 2 {
		w.svc.RunDueJobsOnce(ctx)
	}
	w.clock.add(-2 * time.Minute)
	time.Sleep(100 * time.Millisecond) // a send is over when its job is; the collector may be a moment behind
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, m := range w.mail {
		body := decodeQP(m.Data)
		if slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(body, s) }) {
			out = append(out, strings.Join(m.To, ",")+"\n"+body)
		}
	}
	w.mail = nil
	return out
}

// expect: exactly n welcome emails came, each with every wanted text.
func (w *welcomeEnv) expect(what string, n int, want ...string) []string {
	w.t.Helper()
	got := w.welcomes()
	if len(got) != n {
		w.t.Fatalf("%s: %d welcome emails, want %d:\n%s", what, len(got), n, strings.Join(got, "\n---\n"))
	}
	for _, m := range got {
		for _, s := range want {
			if !strings.Contains(m, s) {
				w.t.Fatalf("%s: email lacks %q:\n%s", what, s, m)
			}
		}
	}
	return got
}

// plan puts the fake subscription on another plan and interval, for so many seats.
func (w *welcomeEnv) plan(sub map[string]any, plan, interval string, seats int) {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	item := sub["items"].(map[string]any)["data"].([]any)[0].(map[string]any)
	item["price"], item["quantity"] = stripePriceObject(plan, interval), seats
}

// The owner is told once that the trial started and once that the workspace pays, whatever Stripe sends after:
// the same event again, the same state in another event, an upgrade, a renewal, a failed payment put right, a new
// subscription after a cancellation. A plan the operator sets tells nobody.
func TestWelcomeEmailsOnTrialAndFirstPayment(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	owner := w.mustSignIn(ana, "")
	ws := w.session(owner).Workspace.ID
	w.expect("signing up with billing on (no plan yet)", 0)

	// The trial starts: Stripe's event, delivered twice, and the same state told by a second event.
	sub := w.f.sub("sub_a", ws, "trialing", "")
	for range 2 {
		if code := w.stripeEvent("evt_trial_started", "customer.subscription.created", sub, testWhsec); code != 200 {
			t.Fatalf("trial event: %d", code)
		}
	}
	w.f.set(w.env, sub, "trialing")
	ends := time.Unix(sub["trial_end"].(int64), 0).UTC().Format("2 January 2006")
	got := w.expect("trial started", 1, "<ana@example.com>", "Your free trial has started", "on the Starter plan, free until "+ends,
		"your card is charged for Starter", "Connect a channel", "https://staqly.example.com/channels",
		"https://staqly.example.com/posts?compose=1", "https://staqly.example.com/settings/plan", "you are an owner of the workspace")
	if strings.Contains(got[0], "List-Unsubscribe") || strings.Contains(got[0], "read-only") {
		t.Fatalf("trial email: an unsubscribe header or the wrong ending:\n%s", got[0])
	}

	// The trial turns into the paid plan: the second email, without the first steps again.
	w.f.set(w.env, sub, "active")
	got = w.expect("first payment after the trial", 1, "<ana@example.com>", "Subject: You are on staqly Starter",
		"is now on the Starter plan, billed monthly.", "Manage billing: https://staqly.example.com/settings/plan", "Invoices")
	if strings.Contains(got[0], "Connect a channel") {
		t.Fatalf("paid email after a trial repeats the first steps:\n%s", got[0])
	}

	// An upgrade, a renewal, a failed renewal and the card fixed.
	w.plan(sub, "plus", "year", 1)
	w.f.set(w.env, sub, "active")
	w.f.set(w.env, sub, "active")
	w.f.set(w.env, sub, "past_due")
	w.f.set(w.env, sub, "active")
	w.expect("upgrade, renewal, past_due recovery", 0)

	// Cancelled, then a new subscription: they know the product, and Stripe sends the receipt.
	w.f.set(w.env, sub, "canceled")
	again := w.f.sub("sub_a2", ws, "active", "")
	w.f.set(w.env, again, "active")
	w.expect("subscribing again", 0)

	// The operator's plan is no customer's purchase.
	if err := w.svc.SetPlan(context.Background(), ws, "pro", ""); err != nil {
		t.Fatal(err)
	}
	w.expect("operator set-plan", 0)
}

// A workspace that pays from the first day gets the paid email with the first steps: one per owner, however many
// events tell Stripe's state at once.
func TestWelcomeEmailFirstPaymentWithoutTrial(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	owner := w.mustSignIn(eve, "")
	ws := w.session(owner).Workspace.ID
	// The operator opens the workspace first (people can be invited): that is no welcome, and takes none away.
	if err := w.svc.SetPlan(context.Background(), ws, "starter", ""); err != nil {
		t.Fatal(err)
	}
	_, tok := w.invite(owner, frank.Email, app.RoleOwner)
	w.mustSignIn(frank, "invite="+url.QueryEscape(tok))
	_, tok = w.invite(owner, bob.Email, app.RoleAdmin)
	w.mustSignIn(bob, "invite="+url.QueryEscape(tok))
	w.expect("operator plan, people joining", 0)

	sub := w.f.sub("sub_e", ws, "active", "")
	w.plan(sub, "plus", "year", 3) // as many seats as members: no seat sync
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = w.stripeEvent(fmt.Sprintf("evt_paid_%d", i), "customer.subscription.updated", sub, testWhsec)
		}()
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("event %d: %d", i, c)
		}
	}
	got := w.expect("first payment, six events at once", 2, "Subject: You are on staqly Plus", "billed yearly.",
		"Connect a channel", "Write your first post", "https://staqly.example.com/posts?compose=1")
	to := got[0][:strings.Index(got[0], "\n")] + " " + got[1][:strings.Index(got[1], "\n")]
	if !strings.Contains(to, eve.Email) || !strings.Contains(to, frank.Email) {
		t.Fatalf("recipients: %s (want both owners, not the admin)", to)
	}
}

// A trial that ends before its email went out (Start paid plan now, a card an unpaid trial used) is not announced
// as running: only the payment is.
func TestWelcomeEmailTrialOverBeforeItsEmail(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	ws := w.session(w.mustSignIn(ana, "")).Workspace.ID
	sub := trialing(w.env, w.f, "sub_a", ws, "")
	w.f.set(w.env, sub, "active")
	w.expect("trial ended at once", 1, "Subject: You are on staqly Starter")
}

// Without mail the plan starts all the same and nothing waits in the queue; mail set up later tells nobody about
// what already happened, and the next first (the payment) is told.
func TestWelcomeEmailWithoutMail(t *testing.T) {
	w := newWelcomeEnv(t, true, false)
	owner := w.mustSignIn(ana, "")
	ws := w.session(owner).Workspace.ID
	sub := trialing(w.env, w.f, "sub_a", ws, "")
	var b app.Billing
	w.as(owner, "GET", "/v1/billing", nil, 200, &b)
	if b.SubscriptionStatus == nil || *b.SubscriptionStatus != "trialing" {
		t.Fatalf("trial without mail: %+v", b)
	}
	var queued int
	if err := w.svc.DB.QueryRow(`SELECT COUNT(*) FROM river_job WHERE kind = 'welcome_email'`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("without mail %d welcome emails are queued (%v)", queued, err)
	}
	w.mailOn(t)
	w.f.set(w.env, sub, "trialing")
	w.expect("mail set up during the trial", 0)
	w.f.set(w.env, sub, "active")
	w.expect("first payment", 1, "Subject: You are on staqly Starter")
}

// Without billing a new workspace starts on the server's trial: its email says the workspace becomes read-only
// when it ends, and names no card.
func TestWelcomeEmailServerTrial(t *testing.T) {
	w := newWelcomeEnv(t, false, true)
	w.mustSignIn(ana, "")
	got := w.expect("server-side trial", 1, "<ana@example.com>", "on the Pro plan, free until ", "becomes read-only until it has a plan",
		"Connect a channel")
	if strings.Contains(got[0], "your card") {
		t.Fatalf("server trial names a card:\n%s", got[0])
	}
	w.mustSignIn(ana, "") // signing in again creates nothing
	w.expect("signing in again", 0)
}
