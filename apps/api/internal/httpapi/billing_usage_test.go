package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// scriptedX stands in for X: each publish takes the next error of errs (nil: published).
type scriptedX struct {
	providers.Base
	mu    sync.Mutex
	errs  []error
	calls int
}

func (x *scriptedX) Connect(context.Context, map[string]string) (providers.Credentials, providers.Profile, error) {
	return providers.Credentials{"token": "t"}, providers.Profile{Name: "Scripted X"}, nil
}

func (x *scriptedX) Publish(_ context.Context, _ providers.Credentials, p providers.PostContent, _ providers.Settings) (providers.Result, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.calls++
	if len(x.errs) > 0 {
		err := x.errs[0]
		x.errs = x.errs[1:]
		if err != nil {
			return providers.Result{}, err
		}
	}
	return providers.Result{ExternalID: p.PostID, URL: "https://x.test/" + p.PostID}, nil
}

func (x *scriptedX) next(errs ...error) {
	x.mu.Lock()
	x.errs = errs
	x.mu.Unlock()
}

func (x *scriptedX) published() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.calls
}

func withScriptedX(t *testing.T) *scriptedX {
	t.Helper()
	orig, _ := providers.Get("x")
	x := &scriptedX{Base: providers.NewBase("x", "X", providers.AuthToken, providers.Limits{MaxChars: 280, MaxMedia: 4})}
	providers.Register(x, providers.StatusAvailable)
	t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
	return x
}

// futureClock puts the service on a clock that starts on 10 February of next year at noon UTC and returns how
// to move it: always ahead of the real clock (nothing made at sign-up is in the future), and the month that
// follows is in the same year whenever the tests run, so a month's end is never also a year's.
func futureClock(e *env) (start time.Time, set func(time.Time)) {
	clock := time.Date(time.Now().UTC().Year()+1, time.February, 10, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	e.svc.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	e.t.Cleanup(func() { e.svc.Now = time.Now })
	return clock, func(t time.Time) {
		mu.Lock()
		clock = t
		mu.Unlock()
	}
}

// Posts to X through the router and the job queue, as a paying workspace spends them: the plan's included posts
// first, then the bought ones; a post X refuses gives its post back and its retry takes it once; a publish that
// failed without an answer keeps its one charge through the queue's retry; a scheduled post costs nothing until it
// goes out, and nothing when cancelled; at zero publishing and scheduling are refused with what is left and when it
// resets; the included posts come back on the 1st, at midnight UTC, and the bought ones stay as they were.
func TestXPostsThroughTheQueue(t *testing.T) {
	x := withScriptedX(t)
	e := newEnv(t)
	ctx := context.Background()
	ws := workspaceID(e)
	start, setClock := futureClock(e)
	if err := e.svc.SetPlan(ctx, ws, "starter", start.Add(25*24*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	e.svc.Cfg.Billing = app.BillingConfig{SecretKey: "sk_test_xq", WebhookSecret: testWhsec, APIBase: "http://127.0.0.1:1"} // never called
	c := e.collection("X")
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "x", "name": "Scripted X"}, 201, &ch)
	target := []map[string]any{{"channel_id": ch.ID}}
	// 49 of Starter's 50 used this month, and 2 bought posts (the paid Checkout session, told twice).
	if _, err := e.svc.DB.Exec(`INSERT INTO usage_events (workspace_id, provider, ref, period, units, included, credits, overage, created_at)
		VALUES (?, 'x', 'seed', ?, 49, 49, 0, 0, ?)`, ws, start.Format("2006-01"), start.Format("2006-01-02T15:04:05.000000Z")); err != nil {
		t.Fatal(err)
	}
	session := map[string]any{"id": "cs_two", "mode": "payment", "payment_status": "paid", "client_reference_id": ws,
		"metadata": map[string]string{"workspace_id": ws, "staqly_kind": "credits", "staqly_provider": "x", "staqly_posts": "2"}}
	for _, id := range []string{"evt_two", "evt_two", "evt_two_again"} {
		if code := e.stripeEvent(id, "checkout.session.completed", session, testWhsec); code != 200 {
			t.Fatalf("credits event %s: %d", id, code)
		}
	}
	// want: the meter after a step: used, included left, bought left; and how often X was asked so far.
	want := func(step string, used, included, credits int64, calls int) {
		t.Helper()
		var got app.XPosts
		e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &got)
		if got.Used != used || got.IncludedLeft != included || got.CreditsLeft != credits || got.Left != included+credits || x.published() != calls {
			t.Fatalf("%s: used %d, included left %d, bought left %d, left %d, X asked %d times; want %d, %d, %d, %d, %d",
				step, got.Used, got.IncludedLeft, got.CreditsLeft, got.Left, x.published(), used, included, credits, included+credits, calls)
		}
	}
	publish := func(content string, code int) app.Post {
		t.Helper()
		p := e.post(c.ID, map[string]any{"content": content, "targets": target})
		e.do(nil, "POST", "/v1/posts/"+p.ID+"/publish", e.key, nil, code, nil)
		return p
	}
	status := func(p app.Post) string {
		t.Helper()
		var got app.Post
		e.do(nil, "GET", "/v1/posts/"+p.ID, e.key, nil, 200, &got)
		return got.Status + "/" + got.Targets[0].Status
	}
	want("before", 49, 1, 2, 0)

	// The last included post goes first; the bought ones are untouched.
	a := publish("the last included one", 202)
	e.svc.RunDueJobsOnce(ctx)
	e.svc.RunDueJobsOnce(ctx) // nothing left to run: nothing is counted twice
	if got := status(a); got != "published/published" {
		t.Fatalf("first post: %s", got)
	}
	want("included first", 50, 0, 2, 1)

	// A scheduled post costs nothing while it waits, and nothing once cancelled.
	s := e.post(c.ID, map[string]any{"content": "later", "targets": target})
	e.do(nil, "POST", "/v1/posts/"+s.ID+"/schedule", e.key, map[string]any{"publish_at": start.Add(time.Hour).Format(time.RFC3339)}, 200, nil)
	want("scheduled", 50, 0, 2, 1)
	e.do(nil, "POST", "/v1/posts/"+s.ID+"/cancel", e.key, nil, 200, nil)
	setClock(start.Add(2 * time.Hour))
	e.svc.RunDueJobsOnce(ctx)
	want("cancelled before it went out", 50, 0, 2, 1)

	// X refuses a post for good: its bought post comes back. The retry takes one, once.
	x.next(&providers.Failure{Kind: providers.ErrRejected, Msg: "duplicate content"})
	b := publish("refused at first", 202)
	e.svc.RunDueJobsOnce(ctx)
	if got := status(b); got != "failed/failed" {
		t.Fatalf("refused post: %s", got)
	}
	want("refused: given back", 50, 0, 2, 2)
	e.do(nil, "POST", "/v1/posts/"+b.ID+"/targets/"+ch.ID+"/retry", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if got := status(b); got != "published/published" {
		t.Fatalf("retried post: %s", got)
	}
	want("retried: one bought post", 51, 0, 1, 3)

	// No answer from X (the post may be out): the charge stays, and the queue's retry doesn't take a second one.
	x.next(errors.New("connection reset"))
	d := publish("no answer at first", 202)
	e.svc.RunDueJobsOnce(ctx)
	want("unknown outcome: charged once", 52, 0, 0, 4)
	setClock(start.Add(3 * time.Hour)) // past the retry's backoff
	e.svc.RunDueJobsOnce(ctx)
	if got := status(d); got != "published/published" {
		t.Fatalf("post after the queue's retry: %s", got)
	}
	want("the queue's retry: still one charge", 52, 0, 0, 5)

	// Nothing left: publishing and scheduling within the month are refused before anything is queued.
	var errBody struct{ Error app.Error }
	z := e.post(c.ID, map[string]any{"content": "one too many", "targets": target})
	e.do(nil, "POST", "/v1/posts/"+z.ID+"/publish", e.key, nil, 402, &errBody)
	monthEnd := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	if d := errBody.Error.Details; errBody.Error.Code != "x_posts_exhausted" || fmt.Sprint(d["needed"]) != "1" || fmt.Sprint(d["left"]) != "0" ||
		!strings.HasPrefix(fmt.Sprint(d["period_end"]), monthEnd.Format("2006-01-02T15:04:05")) {
		t.Fatalf("at zero: %+v", errBody.Error)
	}
	lastSecond := monthEnd.Add(-time.Second).Format(time.RFC3339)
	e.do(nil, "POST", "/v1/posts/"+z.ID+"/schedule", e.key, map[string]any{"publish_at": lastSecond}, 402, nil)
	want("refused at zero", 52, 0, 0, 5)

	// The month's last second is still this month; midnight UTC on the 1st gives Starter's 50 back.
	setClock(monthEnd.Add(-time.Second))
	e.do(nil, "POST", "/v1/posts/"+z.ID+"/publish", e.key, nil, 402, nil)
	setClock(monthEnd)
	var got app.XPosts
	e.do(nil, "GET", "/v1/billing/x-posts", e.key, nil, 200, &got)
	nextReset := monthEnd.AddDate(0, 1, 0).Format("2006-01-02T15:04:05")
	if got.Period != monthEnd.Format("2006-01") || got.Used != 0 || got.IncludedLeft != 50 || got.CreditsLeft != 0 || !strings.HasPrefix(got.PeriodEnd, nextReset) {
		t.Fatalf("on the 1st: %+v, want a new month that resets on %s", got, nextReset)
	}
	e.do(nil, "POST", "/v1/posts/"+z.ID+"/publish", e.key, nil, 202, nil)
	e.svc.RunDueJobsOnce(ctx)
	if got := status(z); got != "published/published" {
		t.Fatalf("post on the 1st: %s", got)
	}
	want("the new month", 1, 49, 0, 6)
}

// The AI allowance is a calendar month's (UTC): what was spent up to the month's last moment counts against it,
// and a request at midnight on the 1st starts from nothing. Was untested: every test ran inside one month.
func TestAIAllowanceResetsOnTheFirst(t *testing.T) {
	e := newEnv(t)
	e.paid()
	fal := newFakeFal(t)
	e.aiSetup(fal, nil)
	start, setClock := futureClock(e)
	monthEnd := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	generate := func(prompt string) (int, string) {
		t.Helper()
		code, job, body := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": prompt}, nil, nil)
		if code == 202 {
			if done := e.runAI(job.ID); done.Status != "succeeded" {
				t.Fatalf("%s: %+v", prompt, done)
			}
		}
		return code, body
	}
	usage := func() app.AIUsage {
		t.Helper()
		var st app.AIStatus
		e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
		return st.Usage
	}
	if code, body := generate("first"); code != 202 {
		t.Fatalf("first request: %d %s", code, body)
	}
	one := usage().UsedUSD
	if one <= 0 || one >= 0.01 {
		t.Fatalf("setup: one picture costs $%v, want less than a cent", one)
	}
	// One cent a month: what fits is spent, the next request is refused.
	e.limits(map[string]int64{app.LimitAIMonthlyCents: 1})
	var refused struct {
		Error struct {
			Code    string
			Details map[string]float64
		}
	}
	n := 1
	for ; n < 20; n++ {
		code, body := generate(fmt.Sprint("more ", n))
		if code != 202 {
			if err := json.Unmarshal([]byte(body), &refused); err != nil || code != 402 {
				t.Fatalf("request %d: %d %s", n, code, body)
			}
			break
		}
	}
	spent := usage()
	d := refused.Error.Details
	if n < 2 || refused.Error.Code != "ai_limit_reached" || spent.Period != start.Format("2006-01") || spent.UsedUSD > 0.01 ||
		d["used_usd"] != spent.UsedUSD || d["limit_usd"] != 0.01 || d["used_usd"]+d["estimate_usd"] <= 0.01 {
		t.Fatalf("after %d requests: %+v, refused with %+v; want the cent spent until the next request no longer fits", n, spent, refused.Error)
	}
	// Still refused in the month's last second, with what was spent.
	setClock(monthEnd.Add(-time.Second))
	code, body := generate("in the last second")
	if code != 402 || !strings.Contains(body, "ai_limit_reached") || !strings.Contains(body, "renews on the 1st") {
		t.Fatalf("the month's last second: %d %s", code, body)
	}
	// Midnight on the 1st: nothing spent, the request goes through and is all the new month has.
	setClock(monthEnd)
	if u := usage(); u.UsedUSD != 0 || u.Period != monthEnd.Format("2006-01") || u.LimitUSD == nil || *u.LimitUSD != 0.01 {
		t.Fatalf("on the 1st: %+v", u)
	}
	if code, body := generate("on the first"); code != 202 {
		t.Fatalf("on the 1st: %d %s", code, body)
	}
	if u := usage(); u.UsedUSD != one {
		t.Fatalf("the new month's spending: $%v, want one request's $%v", u.UsedUSD, one)
	}
}
