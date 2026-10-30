package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// withFakeX puts a fake platform in X's place for one test (it never reaches X), then restores X.
func withFakeX(t *testing.T) *replier {
	t.Helper()
	orig, _ := providers.Get("x")
	r := &replier{Base: providers.NewBase("x", "X", providers.AuthToken, providers.Limits{MaxChars: 280, MaxMedia: 4})}
	providers.Register(r, providers.StatusAvailable)
	t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
	return r
}

// Posts to X are metered per workspace and period: the plan's included posts first, then bought credits
// (oldest first), each post and reply counted once; when both are used up, publishing to X is refused early
// (402) and again at publish time; a paid bundle is granted once and expires at the end of the period.
func TestXPostsAreMetered(t *testing.T) {
	withFakeX(t)
	e := newEnv(t)
	ctx := context.Background()
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	periodEnd := time.Now().Add(20 * 24 * time.Hour)
	if err := e.svc.SetPlan(ctx, ws.ID, "starter", periodEnd.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	c := e.collection("X")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "x", "name": "Test X"}, 201, &ch)
	target := []map[string]any{{"channel_id": ch.ID}}
	period := time.Now().UTC().Format("2006-01")
	// 47 of Starter's 50 already used this month.
	if _, err := e.svc.DB.Exec(`INSERT INTO usage_events (workspace_id, provider, ref, period, units, included, credits, overage, created_at)
		VALUES (?, 'x', 'seed', ?, 47, 47, 0, 0, ?)`, ws.ID, period, time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")); err != nil {
		t.Fatal(err)
	}
	xposts := func() app.XPosts {
		t.Helper()
		var x app.XPosts
		e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &x)
		return x
	}
	if x := xposts(); x.Included != 50 || x.Used != 47 || x.IncludedLeft != 3 || x.Left != 3 || len(x.Bundles) != 3 || x.LinkPostWeight != 14 {
		t.Fatalf("before: %+v", x)
	}

	// A post with one reply: two posts. Published, counted once each (running the jobs again changes nothing).
	p := e.post(c.ID, map[string]any{"content": "hello", "targets": target, "thread": []map[string]any{{"content": "and more"}}})
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	e.svc.RunDueJobsOnce(ctx)
	if x := xposts(); x.Used != 49 || x.IncludedLeft != 1 {
		t.Fatalf("after a post and a reply: %+v", x)
	}
	var rows int
	e.svc.DB.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE workspace_id = ? AND ref <> 'seed'`, ws.ID).Scan(&rows)
	if rows != 2 {
		t.Fatalf("usage rows: %d", rows)
	}

	// Two more posts don't fit in the one left: refused before anything is queued, also when scheduled this month.
	p2 := e.post(c.ID, map[string]any{"content": "two parts", "targets": target, "thread": []map[string]any{{"content": "second"}}})
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/publish", e.key, nil, 402, &errBody)
	if errBody.Error.Code != "x_posts_exhausted" || !strings.Contains(errBody.Error.Message, "Your X posts for this period are used up") {
		t.Fatalf("exhausted: %+v", errBody.Error)
	}
	soon := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/schedule", e.key, map[string]any{"publish_at": soon}, 402, nil)
	nextMonth := time.Date(time.Now().Year(), time.Now().Month()+1, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/schedule", e.key, map[string]any{"publish_at": nextMonth}, 200, nil) // next period: fine

	// A bundle is bought: the webhook grants it once (the same session twice, under two event ids).
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/prices":
			key := r.URL.Query().Get("lookup_keys[]")
			fmt.Fprintf(w, `{"data":[{"id":"price_%s","unit_amount":4000}]}`, strings.TrimPrefix(key, "staqly_"))
		case "POST /v1/customers":
			fmt.Fprint(w, `{"id":"cus_x"}`)
		case "POST /v1/checkout/sessions":
			r.ParseForm()
			if r.Form.Get("mode") != "payment" || r.Form.Get("line_items[0][price]") != "price_credits_x_1000" ||
				r.Form.Get("metadata[staqly_posts]") != "1000" || r.Form.Get("metadata[workspace_id]") != ws.ID {
				t.Errorf("credits checkout form: %v", r.Form)
			}
			fmt.Fprint(w, `{"url":"https://checkout.stripe.test/cs_credits"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer stripe.Close()
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_credits", WebhookSecret: testWhsec, APIBase: stripe.URL}
	var out struct{ URL string }
	e.do(nil, "POST", "/v1/billing/credits/checkout", e.admin, map[string]any{"posts": 1000}, 200, &out)
	if out.URL != "https://checkout.stripe.test/cs_credits" {
		t.Fatalf("checkout url: %q", out.URL)
	}
	e.do(nil, "POST", "/v1/billing/credits/checkout", e.admin, map[string]any{"posts": 300}, 422, nil)
	e.do(nil, "POST", "/v1/billing/credits/checkout", e.key, map[string]any{"posts": 1000}, 403, nil)
	session := map[string]any{"id": "cs_credits", "mode": "payment", "payment_status": "paid", "client_reference_id": ws.ID,
		"metadata": map[string]string{"workspace_id": ws.ID, "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": "200"}}
	for _, id := range []string{"evt_c1", "evt_c1", "evt_c2"} {
		if code := e.stripeEvent(id, "checkout.session.completed", session, testWhsec); code != 200 {
			t.Fatalf("webhook %s: %d", id, code)
		}
	}
	// A session for another (unknown) workspace grants nothing here.
	stranger := map[string]any{"id": "cs_other", "mode": "payment", "payment_status": "paid",
		"metadata": map[string]string{"workspace_id": "ws_someone_else", "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": "4000"}}
	e.stripeEvent("evt_c3", "checkout.session.completed", stranger, testWhsec)
	x := xposts()
	if x.CreditsLeft != 200 || x.Left != 201 || x.CreditsExpireAt == nil {
		t.Fatalf("after buying 200: %+v", x)
	}
	if exp, _ := time.Parse(time.RFC3339, *x.CreditsExpireAt); exp.Sub(periodEnd).Abs() > time.Minute {
		t.Fatalf("credits expire at the end of the period (%s), not %s", periodEnd, *x.CreditsExpireAt)
	}

	// Now the two-part post fits: one included post, then one bought.
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/duplicate", e.key, nil, 201, &p2)
	e.do(nil, "POST", "/v1/posts/"+p2.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if x := xposts(); x.IncludedLeft != 0 || x.CreditsLeft != 199 || x.Used != 51 {
		t.Fatalf("included first, then credits: %+v", x)
	}

	// Expired credits don't count.
	e.svc.DB.Exec(`UPDATE credit_grants SET expires_at = '2000-01-01T00:00:00.000000Z' WHERE workspace_id = ?`, ws.ID)
	if x := xposts(); x.CreditsLeft != 0 || x.Left != 0 {
		t.Fatalf("after expiry: %+v", x)
	}
	p3 := e.post(c.ID, map[string]any{"content": "one more", "targets": target})
	e.do(nil, "POST", "/v1/posts/"+p3.ID+"/publish", e.key, nil, 402, nil)

	// Published elsewhere than X: not metered.
	var other app.Channel
	withReplier(t)
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "test_replier", "name": "Other"}, 201, &other)
	p4 := e.post(c.ID, map[string]any{"content": "elsewhere", "targets": []map[string]any{{"channel_id": other.ID}}})
	e.do(nil, "POST", "/v1/posts/"+p4.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if x := xposts(); x.Used != 51 {
		t.Fatalf("other platforms are not metered: %+v", x)
	}

	// Reading the meter needs posts:read.
	e.do(nil, "GET", "/v1/billing/x-posts", e.keyWith(app.ScopeChannelsRead), nil, 403, nil)
}

// A post scheduled for when the room is gone fails at publish time with the reason (the early check only sees
// the posts known then).
func TestXPostsCheckedAtPublishTime(t *testing.T) {
	withFakeX(t)
	e := newEnv(t)
	ctx := context.Background()
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.key, nil, 200, &ws)
	if err := e.svc.SetPlan(ctx, ws.ID, "starter", time.Now().Add(20*24*time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	c := e.collection("X")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "x", "name": "Test X"}, 201, &ch)
	p := e.post(c.ID, map[string]any{"content": "later", "targets": []map[string]any{{"channel_id": ch.ID}}})
	at := time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano)
	e.do(nil, "POST", "/v1/posts/"+p.ID+"/schedule", e.key, map[string]any{"publish_at": at}, 200, nil)
	// Meanwhile the month's posts are used up.
	e.svc.DB.Exec(`INSERT INTO usage_events (workspace_id, provider, ref, period, units, included, credits, overage, created_at)
		VALUES (?, 'x', 'seed', ?, 50, 50, 0, 0, ?)`, ws.ID, time.Now().UTC().Format("2006-01"), time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"))
	deadline := time.Now().Add(15 * time.Second)
	var got app.Post
	for time.Now().Before(deadline) {
		e.svc.RunDueJobsOnce(ctx)
		e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
		if got.Status == "failed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got.Status != "failed" || !strings.Contains(got.Targets[0].Error, "used up") {
		t.Fatalf("at publish time: %+v", got)
	}
}
