package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeAccount is a token platform whose personal account is the "account" field: the same value is the same account
// on the platform, whichever workspace connects it. It has no id in its credentials, so the handle tells accounts
// apart (the fallback most platforms without a listed id use).
type fakeAccount struct{ providers.Base }

func (fakeAccount) CredentialFields() []providers.Field {
	return []providers.Field{{Key: "account", Label: "Account", Kind: "text", Required: true}}
}

func (fakeAccount) Connect(_ context.Context, fields map[string]string) (providers.Credentials, providers.Profile, error) {
	return providers.Credentials{"token": "t"}, providers.Profile{Handle: "@" + fields["account"], Name: fields["account"]}, nil
}

// fakeSlack stands in for Slack, a space many people share: the team and the member who authorized the connection
// ("user" empty: only a bot, as with a bot token).
type fakeSlack struct{ providers.Base }

func (fakeSlack) CredentialFields() []providers.Field {
	return []providers.Field{{Key: "team", Label: "Team", Kind: "text", Required: true}, {Key: "user", Label: "User", Kind: "text"}}
}

func (fakeSlack) Connect(_ context.Context, fields map[string]string) (providers.Credentials, providers.Profile, error) {
	c := providers.Credentials{"access_token": "xoxb", "team_id": fields["team"]}
	if fields["user"] != "" {
		c["user_id"] = fields["user"]
	}
	return c, providers.Profile{Handle: fields["team"] + "/" + fields["user"], Name: "Acme"}, nil
}

func init() {
	providers.Register(fakeAccount{providers.NewBase("fakeacct", "Fake Account", providers.AuthToken, providers.Limits{MaxChars: 500})}, providers.StatusAvailable)
}

// withFakeSlack puts fakeSlack in Slack's place for one test, then restores Slack.
func withFakeSlack(t *testing.T) {
	t.Helper()
	orig, _ := providers.Get("slack")
	providers.Register(fakeSlack{providers.NewBase("slack", "Slack", providers.AuthToken, providers.Limits{MaxChars: 500})}, providers.StatusAvailable)
	t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
}

// trialStripe is a fake Stripe for repeated trials: the subscriptions it knows, by id, what ending a trial early
// (POST trial_end=now) does to the charge ("active": the card paid; "past_due": it didn't), and the checkouts.
type trialStripe struct {
	mu       sync.Mutex
	subs     map[string]map[string]any
	charge   string
	ended    []string // subscriptions whose trial was ended early
	checkout url.Values
}

func withTrialStripe(t *testing.T, e *env) *trialStripe {
	t.Helper()
	f := &trialStripe{subs: map[string]map[string]any{}, charge: "past_due"}
	srv := httptest.NewServer(subsFirst(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		r.ParseForm()
		id, isSub := strings.CutPrefix(r.URL.Path, "/v1/subscriptions/")
		sub := f.subs[id]
		switch {
		case isSub && sub != nil && r.Method == "GET":
			json.NewEncoder(w).Encode(sub)
		case isSub && sub != nil && r.Method == "DELETE": // a deleted workspace's subscription is cancelled
			sub["status"] = "canceled"
			json.NewEncoder(w).Encode(sub)
		case isSub && sub != nil && r.Method == "POST":
			if r.PostForm.Get("trial_end") != "now" {
				t.Errorf("subscription update without trial_end=now: %v", r.PostForm)
			}
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("ending a trial early must be idempotent")
			}
			f.ended = append(f.ended, id)
			sub["status"] = f.charge
			json.NewEncoder(w).Encode(sub)
		case r.URL.Path == "/v1/prices":
			fmt.Fprint(w, `{"data":[{"id":"price_starter_month"}]}`)
		case strings.HasPrefix(r.URL.Path, "/v1/customers"):
			fmt.Fprint(w, `{"id":"cus_new"}`)
		case r.URL.Path == "/v1/checkout/sessions":
			f.checkout = r.PostForm
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"no such route"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_1", WebhookSecret: testWhsec, APIBase: srv.URL}
	return f
}

// sub is a Starter subscription of workspace ws in status; card is its card's fingerprint at Stripe ("" for none,
// "-" for a payment method that isn't a card).
func (f *trialStripe) sub(id, ws, status, card string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	end := time.Now().Add(app.TrialDays * 24 * time.Hour).Unix()
	s := map[string]any{"id": id, "customer": "cus_" + id, "status": status, "trial_end": end, "current_period_end": end,
		"metadata": map[string]string{"workspace_id": ws},
		"items":    map[string]any{"data": []any{map[string]any{"id": "si_" + id, "quantity": 1, "current_period_end": end, "price": stripePriceObject("starter", "month")}}}}
	switch card {
	case "":
	case "-":
		s["default_payment_method"] = map[string]any{"id": "pm_" + id, "type": "sepa_debit"}
	default:
		s["default_payment_method"] = map[string]any{"id": "pm_" + id, "type": "card", "card": map[string]any{"fingerprint": card, "last4": "4242"}}
	}
	f.subs[id] = s
	return s
}

func (f *trialStripe) endedCount(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.ended {
		if r == id {
			n++
		}
	}
	return n
}

func (f *trialStripe) setCharge(status string) {
	f.mu.Lock()
	f.charge = status
	f.mu.Unlock()
}

// set applies a subscription in a status through its webhook.
func (f *trialStripe) set(e *env, sub map[string]any, status string) {
	e.t.Helper()
	f.mu.Lock()
	sub["status"] = status
	f.mu.Unlock()
	evt := fmt.Sprintf("evt_%s_%s_%d", sub["id"], status, time.Now().UnixNano())
	if code := e.stripeEvent(evt, "customer.subscription.updated", sub, testWhsec); code != 200 {
		e.t.Fatalf("subscription %s %s: %d", sub["id"], status, code)
	}
}

// trialWorkspace signs up another workspace (with billing on: read-only until a subscription starts).
func (e *env) trialWorkspace(name string) (key, id string) {
	e.t.Helper()
	var res struct {
		APIKey    app.APIKey    `json:"api_key"`
		Workspace app.Workspace `json:"workspace"`
	}
	e.do(nil, "POST", "/v1/workspaces", "", map[string]string{"name": name, "setup_token": "tok"}, 201, &res)
	return res.APIKey.Key, res.Workspace.ID
}

type connectOut struct {
	app.Channel
	Error app.Error `json:"error"`
}

func (e *env) connectAccount(key, account string, want int) (app.Channel, app.Error) {
	e.t.Helper()
	var out connectOut
	e.do(nil, "POST", "/v1/channels", key, map[string]any{"provider": "fakeacct", "fields": map[string]string{"account": account}}, want, &out)
	return out.Channel, out.Error
}

func (e *env) connectSlack(key, team, user string, want int) (app.Channel, app.Error) {
	e.t.Helper()
	var out connectOut
	e.do(nil, "POST", "/v1/channels", key, map[string]any{"provider": "slack", "fields": map[string]string{"team": team, "user": user}}, want, &out)
	return out.Channel, out.Error
}

func (e *env) billingOf(key string) app.Billing {
	e.t.Helper()
	var b app.Billing
	e.do(nil, "GET", "/v1/billing", key, nil, 200, &b)
	return b
}

func trialing(e *env, f *trialStripe, subID, ws, card string) map[string]any {
	e.t.Helper()
	s := f.sub(subID, ws, "trialing", card)
	f.set(e, s, "trialing")
	return s
}

// A trial that ended unpaid marks the accounts it connected. A later workspace in its trial that connects one of
// its personal accounts, or two of the people behind its shared spaces, has its trial paused for review: nothing
// is charged; publishing and new channels wait; drafts stay. Colleagues sharing a Slack workspace, re-adding in the
// same workspace, a trial still running elsewhere and a paying workspace are never affected. Start paid plan now,
// or the operator, lifts the pause.
func TestRepeatedTrialChannels(t *testing.T) {
	withFakeSlack(t)
	e := newEnv(t)
	f := withTrialStripe(t, e)
	keyA, wsA := e.admin, workspaceID(e)
	subA := trialing(e, f, "sub_a", wsA, "")

	// A's trial: brand-new accounts connect; removing and adding one again in the same workspace is fine. Its
	// person connected two Slack workspaces.
	jane, _ := e.connectAccount(keyA, "jane", 201)
	e.do(nil, "DELETE", "/v1/channels/"+jane.ID, keyA, nil, 204, nil)
	e.connectAccount(keyA, "Jane", 201) // the same account (handles compare without case)
	e.connectSlack(keyA, "T1", "U1", 201)
	e.connectSlack(keyA, "T2", "U2", 201)
	e.connectSlack(keyA, "T3", "", 201) // a bot only: no fingerprint

	// B trials at the same time with the same account: A's trial hasn't ended unpaid, so nothing is wrong yet.
	keyB, wsB := e.trialWorkspace("B")
	trialing(e, f, "sub_b", wsB, "")
	janeB, _ := e.connectAccount(keyB, "jane", 201)

	// A's trial ends without a payment (cancelled).
	f.set(e, subA, "canceled")

	// C: a colleague in A's first Slack workspace isn't A's person; the same bot-only Slack isn't anyone; A's person
	// in one Slack workspace may be a coincidence. Their second Slack workspace makes two: C's trial is paused.
	keyC, wsC := e.trialWorkspace("C")
	trialing(e, f, "sub_c", wsC, "")
	e.connectSlack(keyC, "T1", "U9", 201)
	e.connectSlack(keyC, "T3", "", 201)
	e.connectSlack(keyC, "T1", "U1", 201)
	if b := e.billingOf(keyC); b.TrialReview != nil {
		t.Fatalf("one person match: %+v", b)
	}
	_, apiErr := e.connectSlack(keyC, "T2", "U2", 402)
	if apiErr.Code != "trial_already_used" || apiErr.Details["reason"] != app.TrialReviewChannels ||
		!strings.Contains(apiErr.Message, "paused") || !strings.Contains(apiErr.Message, app.SupportEmail) {
		t.Fatalf("second person match: %+v", apiErr)
	}
	if n := f.endedCount("sub_c"); n != 0 {
		t.Fatal("a channel match must never charge")
	}
	b := e.billingOf(keyC)
	if b.TrialReview == nil || *b.TrialReview != app.TrialReviewChannels || b.Suspended || deref(b.SubscriptionStatus) != "trialing" {
		t.Fatalf("C paused: %+v", b)
	}
	// Paused: nothing connects, nothing is scheduled or published; drafts are written as usual.
	e.connectAccount(keyC, "brand-new", 402)
	col := e.collectionFor(keyC)
	var post app.Post
	e.do(nil, "POST", "/v1/posts", keyC, map[string]any{"collection": col, "content": "hello"}, 201, &post)
	e.do(nil, "POST", "/v1/posts/"+post.ID+"/publish", keyC, nil, 402, nil)
	e.do(nil, "POST", "/v1/posts", keyC, map[string]any{"collection": col, "content": "later", "status": "scheduled",
		"publish_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 402, nil)

	// C starts the paid plan: the trial ends now and the card is charged; everything works again.
	f.setCharge("active")
	e.do(nil, "POST", "/v1/billing/start-paid-plan", keyC, nil, 200, &b)
	if f.endedCount("sub_c") != 1 || b.TrialReview != nil || deref(b.SubscriptionStatus) != "active" {
		t.Fatalf("C after starting the paid plan: %+v (%v)", b, f.ended)
	}
	e.connectAccount(keyC, "brand-new", 201)
	e.do(nil, "POST", "/v1/billing/start-paid-plan", keyC, nil, 409, nil) // no trial any more

	// B reconnects A's account (a reconnect counts as a connect): one personal account is enough to pause.
	f.setCharge("past_due")
	var out struct{ Error app.Error }
	e.do(nil, "PATCH", "/v1/channels/"+janeB.ID, keyB, map[string]any{"fields": map[string]string{"account": "jane"}}, 402, &out)
	if out.Error.Code != "trial_already_used" || f.endedCount("sub_b") != 0 {
		t.Fatalf("reconnect: %+v, %v", out.Error, f.ended)
	}
	// The operator looks into it and clears B: the pause lifts, B isn't checked again, an event records it.
	if err := e.svc.ClearTrialReview(context.Background(), wsB); err != nil {
		t.Fatal(err)
	}
	if b := e.billingOf(keyB); b.TrialReview != nil {
		t.Fatalf("B after clearing: %+v", b)
	}
	e.do(nil, "PATCH", "/v1/channels/"+janeB.ID, keyB, map[string]any{"fields": map[string]string{"account": "jane"}}, 200, nil)
	var events struct{ Data []app.Event }
	e.do(nil, "GET", "/v1/events?limit=100", keyB, nil, 200, &events)
	states := []string{}
	for _, ev := range events.Data {
		if ev.Type == "workspace.trial_review" {
			var d map[string]any
			_ = json.Unmarshal(ev.Data, &d)
			states = append(states, fmt.Sprint(d["state"]))
		}
	}
	if strings.Join(states, ",") != "cleared,paused" {
		t.Fatalf("trial review events (newest first): %v", states)
	}
	if err := e.svc.ClearTrialReview(context.Background(), "ws_nope"); err == nil {
		t.Fatal("clearing an unknown workspace")
	}

	// D pays from the first day (Plus): never in a trial, never checked.
	keyD, wsD := e.trialWorkspace("D")
	subD := f.sub("sub_d", wsD, "active", "")
	subD["trial_end"] = 0
	f.set(e, subD, "active")
	e.connectAccount(keyD, "jane", 201)
}

// collectionFor: the workspace's Default collection.
func (e *env) collectionFor(key string) string {
	e.t.Helper()
	var list struct{ Data []app.Collection }
	e.do(nil, "GET", "/v1/collections", key, nil, 200, &list)
	return list.Data[0].ID
}

// Only unpaid trials of the last 180 days count.
func TestRepeatedTrialWindow(t *testing.T) {
	e := newEnv(t)
	f := withTrialStripe(t, e)
	wsA := workspaceID(e)
	subA := trialing(e, f, "sub_a", wsA, "")
	e.connectAccount(e.admin, "jane", 201)
	f.set(e, subA, "canceled")
	old := time.Now().Add(-181 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000000Z")
	if _, err := e.svc.DB.Exec(`UPDATE trial_fingerprints SET unpaid_trial_at = ? WHERE workspace_id = ?`, old, wsA); err != nil {
		t.Fatal(err)
	}
	keyB, wsB := e.trialWorkspace("B")
	subB := trialing(e, f, "sub_b", wsB, "")
	e.connectAccount(keyB, "jane", 201)

	// Past the window, the daily retention deletes the old trial's fingerprints (the privacy policy says so), and
	// a workspace that paid keeps none.
	f.set(e, subB, "active")
	if _, err := e.svc.RunRetention(context.Background()); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := e.svc.DB.QueryRow(`SELECT COUNT(*) FROM trial_fingerprints WHERE workspace_id IN (?, ?)`, wsA, wsB).Scan(&left); err != nil || left != 0 {
		t.Fatalf("fingerprints left after retention: %d %v", left, err)
	}
}

// The card is the strong signal: a trial whose card an unpaid trial used ends at once (it was only known after
// Checkout), and the billing state says so (charged or not). The same card in the same workspace, another card, a
// payment method that isn't a card and the card of a trial that was paid are fine.
func TestRepeatedTrialCard(t *testing.T) {
	e := newEnv(t)
	f := withTrialStripe(t, e)
	wsA := workspaceID(e)
	subA := trialing(e, f, "sub_a", wsA, "fp_card_1")
	f.set(e, subA, "trialing") // the same card seen again in the same workspace
	if n := f.endedCount("sub_a"); n != 0 {
		t.Fatal("a workspace's own card is no reuse")
	}
	f.set(e, subA, "incomplete_expired") // the trial ended unpaid

	// B starts its trial with the same card (Checkout completed): the trial ends now; the charge fails.
	keyB, wsB := e.trialWorkspace("B")
	f.sub("sub_b", wsB, "trialing", "fp_card_1")
	if code := e.stripeEvent("evt_cs_b", "checkout.session.completed", map[string]any{"subscription": "sub_b"}, testWhsec); code != 200 {
		t.Fatalf("checkout completed: %d", code)
	}
	if n := f.endedCount("sub_b"); n != 1 {
		t.Fatalf("B's trial ended %d times", n)
	}
	if b := e.billingOf(keyB); b.TrialRevoked == nil || *b.TrialRevoked != app.TrialRevokedCard || !b.Suspended {
		t.Fatalf("B: %+v", b)
	}

	// C: the same card, and this time the charge goes through: C pays from today and is told why.
	f.setCharge("active")
	keyC, wsC := e.trialWorkspace("C")
	trialing(e, f, "sub_c", wsC, "fp_card_1")
	if b := e.billingOf(keyC); f.endedCount("sub_c") != 1 || b.TrialRevoked == nil || b.Suspended || deref(b.SubscriptionStatus) != "active" {
		t.Fatalf("C: %+v", b)
	}

	// D: another card; E: a payment method that isn't a card. Neither is touched. D pays, then cancels: its card is
	// no unpaid trial's.
	_, wsD := e.trialWorkspace("D")
	subD := trialing(e, f, "sub_d", wsD, "fp_card_2")
	_, wsE := e.trialWorkspace("E")
	trialing(e, f, "sub_e", wsE, "-")
	if f.endedCount("sub_d")+f.endedCount("sub_e") != 0 {
		t.Fatalf("another card or no card: %v", f.ended)
	}
	f.set(e, subD, "active")
	f.set(e, subD, "canceled")
}

// A card added in the billing portal during a trial (payment_method.attached) ends it; a card already on file
// that an unpaid trial used means no trial at all, said before Checkout and left out of it; a trial from before
// any of this was seen never counts as unpaid.
func TestRepeatedTrialCardAttached(t *testing.T) {
	e := newEnv(t)
	f := withTrialStripe(t, e)
	wsA := workspaceID(e)
	subA := trialing(e, f, "sub_a", wsA, "fp_card_9")
	f.set(e, subA, "canceled")

	setCustomer := func(ws, customer string) {
		t.Helper()
		if err := e.svc.W.SetStripeCustomer(context.Background(), db.SetStripeCustomerParams{StripeCustomerID: sql.NullString{String: customer, Valid: true},
			UpdatedAt: time.Now().UTC().Format(time.RFC3339), ID: ws}); err != nil {
			t.Fatal(err)
		}
	}
	keyB, wsB := e.trialWorkspace("B")
	trialing(e, f, "sub_b", wsB, "-")
	setCustomer(wsB, "cus_sub_b")
	if code := e.stripeEvent("evt_pm_1", "payment_method.attached", map[string]any{"id": "pm_1", "customer": "cus_sub_b",
		"type": "sepa_debit"}, testWhsec); code != 200 || f.endedCount("sub_b") != 0 {
		t.Fatalf("not a card: %d, %v", code, f.ended)
	}
	if code := e.stripeEvent("evt_pm_2", "payment_method.attached", map[string]any{"id": "pm_2", "customer": "cus_sub_b",
		"type": "card", "card": map[string]any{"fingerprint": "fp_card_9"}}, testWhsec); code != 200 {
		t.Fatalf("payment_method.attached: %d", code)
	}
	if n := f.endedCount("sub_b"); n != 1 {
		t.Fatalf("B's trial ended %d times", n)
	}
	if b := e.billingOf(keyB); b.TrialRevoked == nil || *b.TrialRevoked != app.TrialRevokedCard {
		t.Fatalf("B: %+v", b)
	}

	// C has no subscription yet but A's card on file: no trial, said by the billing state and left out of Checkout.
	keyC, wsC := e.trialWorkspace("C")
	setCustomer(wsC, "cus_c")
	e.stripeEvent("evt_pm_3", "payment_method.attached", map[string]any{"id": "pm_3", "customer": "cus_c",
		"type": "card", "card": map[string]any{"fingerprint": "fp_card_9"}}, testWhsec)
	if b := e.billingOf(keyC); b.Trial != app.TrialCardUsed {
		t.Fatalf("C's trial offer: %q", b.Trial)
	}
	e.do(nil, "POST", "/v1/billing/checkout", keyC, map[string]string{"plan": "starter", "interval": "month"}, 200, nil)
	if f.checkout == nil || f.checkout.Get("subscription_data[trial_period_days]") != "" {
		t.Fatalf("checkout with a used card: %v", f.checkout)
	}

	// L's subscription had a trial before any of this was seen (first seen cancelled): it may have been paid, so
	// its card is no unpaid trial's and M trials with it as usual.
	_, wsL := e.trialWorkspace("L")
	f.set(e, f.sub("sub_l", wsL, "canceled", "fp_card_old"), "canceled")
	_, wsM := e.trialWorkspace("M")
	trialing(e, f, "sub_m", wsM, "fp_card_old")
	if n := f.endedCount("sub_m"); n != 0 {
		t.Fatalf("a trial not seen here counted as unpaid: %v", f.ended)
	}
}

// A workspace deleted during its trial leaves its accounts marked: the next sign-up can't start over with them.
func TestRepeatedTrialAfterDeletion(t *testing.T) {
	e := newEnv(t)
	f := withTrialStripe(t, e)
	trialing(e, f, "sub_a", workspaceID(e), "")
	e.connectAccount(e.admin, "jane", 201)
	csrf := []string{"X-CSRF-Token", e.csrf} // the deletion cancels the subscription first
	e.do(e.browser, "DELETE", "/v1/workspace", "", map[string]string{"confirm": "Test"}, 204, nil, csrf...)

	keyB, wsB := e.trialWorkspace("B")
	trialing(e, f, "sub_b", wsB, "")
	if _, apiErr := e.connectAccount(keyB, "jane", 402); apiErr.Code != "trial_already_used" {
		t.Fatalf("after deletion: %+v", apiErr)
	}
}

// Without billing (and in local mode) none of this runs: nothing is fingerprinted or refused.
func TestRepeatedTrialNeedsBilling(t *testing.T) {
	e := newEnv(t) // cloud without Stripe: the server-side trial
	e.connectAccount(e.admin, "jane", 201)
	if _, err := e.svc.Q.GetWorkspaceTrial(context.Background(), workspaceID(e)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("no trial record without billing: %v", err)
	}
	key, _ := e.trialWorkspace("B")
	e.connectAccount(key, "jane", 201)

	_, srv := newLocal(t)
	resp, err := http.Post(srv.URL+"/v1/channels", "application/json", strings.NewReader(`{"provider":"fakeacct","fields":{"account":"jane"}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("local connect: %d", resp.StatusCode)
	}
}
