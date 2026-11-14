package httpapi_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The three sign-up emails, by the heading each one carries.
var signupTitles = []string{"Thanks for creating your staqly account", "Your first post, scheduled in 2 minutes", "Your free week is still waiting"}

// signups moves the clock on by d (for good), runs what is due and answers the sign-up emails that came.
func (w *welcomeEnv) signups(d time.Duration) []string {
	w.t.Helper()
	w.clock.add(d)
	return w.received(signupTitles...)
}

func (w *welcomeEnv) expectSignup(what string, d time.Duration, n int, want ...string) []string {
	w.t.Helper()
	got := w.signups(d)
	if len(got) != n {
		w.t.Fatalf("%s: %d sign-up emails, want %d:\n%s", what, len(got), n, strings.Join(got, "\n---\n"))
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

// welcomeJobs counts the queued welcome_email jobs (at sign-up with billing: only the sign-up emails).
func (w *welcomeEnv) welcomeJobs() int {
	w.t.Helper()
	var n int
	if err := w.svc.DB.QueryRow(`SELECT COUNT(*) FROM river_job WHERE kind = 'welcome_email'`).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// Someone who signs up and doesn't start the trial hears from staqly three times: the account is ready (10
// minutes), a first post (a day), the last reminder (three days). Each one has a one-click unsubscribe that stops
// the ones after it and leaves the personal notifications alone.
func TestSignupEmails(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	w.mustSignIn(ana, "")
	if n := w.welcomeJobs(); n != 3 {
		t.Fatalf("%d sign-up emails queued, want 3", n)
	}
	w.expectSignup("before 10 minutes", 5*time.Minute, 0)
	got := w.expectSignup("after 10 minutes", 6*time.Minute, 1, "<ana@example.com>", "Hi Ana", "Thanks for creating your staqly account",
		"Facebook, Instagram, Threads, LinkedIn, X, Bluesky", "Start your 7-day free trial and publish your first post today.",
		"Start my free week: https://staqly.example.com/plans", "The staqly team", "Help & Feedback",
		"List-Unsubscribe: <https://staqly.example.com/api/v1/notifications/unsubscribe?token=", "List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		"Unsubscribe from these emails: https://staqly.example.com/unsubscribe#")
	if strings.Contains(got[0], "Your free week is still waiting") {
		t.Fatalf("the first email is not the last reminder:\n%s", got[0])
	}
	w.expectSignup("a day later", 24*time.Hour, 1, "Your first post, scheduled in 2 minutes", "Hi Ana",
		"https://staqly.example.com/plans", "7-day free trial", "List-Unsubscribe-Post")

	// The mail client's one-click unsubscribe (a POST to the header's link) stops the last one.
	token := regexp.MustCompile(`/unsubscribe\?token=([A-Za-z0-9_\-.]+)`).FindStringSubmatch(got[0])
	if token == nil {
		t.Fatalf("no unsubscribe token:\n%s", got[0])
	}
	var res struct{ Event string }
	w.do(nil, "POST", "/v1/notifications/unsubscribe?token="+token[1], "", nil, 200, &res)
	if res.Event != "onboarding" {
		t.Fatalf("unsubscribed from %q", res.Event)
	}
	w.expectSignup("three days later, unsubscribed", 48*time.Hour, 0)
	// The personal notifications are still on: only the sign-up emails are off.
	var prefs string
	if err := w.svc.DB.QueryRow(`SELECT notification_prefs FROM users WHERE email = ?`, ana.Email).Scan(&prefs); err != nil {
		t.Fatal(err)
	}
	var st struct {
		Events map[string]struct{ Email bool }
	}
	if err := json.Unmarshal([]byte(prefs), &st); err != nil || len(st.Events) != 1 || st.Events["onboarding"].Email {
		t.Fatalf("notification settings after unsubscribing: %s", prefs)
	}
}

// The last reminder says it is the last; nobody unsubscribed gets all three.
func TestSignupEmailsLastReminder(t *testing.T) {
	w := newWelcomeEnv(t, true, true)
	w.mustSignIn(ana, "")
	w.expectSignup("after a day", 25*time.Hour, 2)
	w.expectSignup("after three days", 48*time.Hour, 1, "Your free week is still waiting", "This is the last reminder",
		"cancel any time in Settings → Plan & usage", "https://staqly.example.com/plans")
}

// A trial started within 10 minutes leaves only the trial's email; one started after the first email stops the
// rest. A plan the operator set, or a paid one, stops them too.
func TestSignupEmailsSkippedOnceTheWorkspaceHasAPlan(t *testing.T) {
	t.Run("trial at once", func(t *testing.T) {
		w := newWelcomeEnv(t, true, true)
		ws := w.session(w.mustSignIn(ana, "")).Workspace.ID
		trialing(w.env, w.f, "sub_a", ws, "")
		w.expect("trial started", 1, "Your free trial has started")
		w.expectSignup("three days", 73*time.Hour, 0)
	})
	t.Run("trial after the first email", func(t *testing.T) {
		w := newWelcomeEnv(t, true, true)
		ws := w.session(w.mustSignIn(ana, "")).Workspace.ID
		w.expectSignup("10 minutes", 11*time.Minute, 1)
		w.clock.add(-11 * time.Minute) // Stripe's signature is checked against the clock
		trialing(w.env, w.f, "sub_a", ws, "")
		w.expectSignup("three days", 73*time.Hour, 0)
	})
	t.Run("paid", func(t *testing.T) {
		w := newWelcomeEnv(t, true, true)
		ws := w.session(w.mustSignIn(ana, "")).Workspace.ID
		w.f.set(w.env, w.f.sub("sub_a", ws, "active", ""), "active")
		w.expectSignup("three days", 73*time.Hour, 0)
	})
	t.Run("operator plan", func(t *testing.T) {
		w := newWelcomeEnv(t, true, true)
		ws := w.session(w.mustSignIn(ana, "")).Workspace.ID
		if err := w.svc.SetPlan(context.Background(), ws, "pro", ""); err != nil {
			t.Fatal(err)
		}
		w.expectSignup("three days", 73*time.Hour, 0)
	})
}

// Someone no longer the owner, or a workspace deleted, gets nothing.
func TestSignupEmailsSkippedForGoneOwnerOrWorkspace(t *testing.T) {
	t.Run("not the owner", func(t *testing.T) {
		w := newWelcomeEnv(t, true, true)
		ws := w.session(w.mustSignIn(ana, "")).Workspace.ID
		if _, err := w.svc.DB.Exec(`UPDATE workspace_members SET role = 'admin' WHERE workspace_id = ?`, ws); err != nil {
			t.Fatal(err)
		}
		w.expectSignup("three days", 73*time.Hour, 0)
	})
	t.Run("workspace deleted", func(t *testing.T) {
		w := newWelcomeEnv(t, true, true)
		owner := w.mustSignIn(ana, "")
		w.as(owner, "DELETE", "/v1/workspace", map[string]any{"confirm": w.session(owner).Workspace.Name}, 204, nil)
		w.expectSignup("three days", 73*time.Hour, 0)
	})
}

// Only with billing and mail: without billing the server's trial starts at once (its own email), without mail
// nothing is queued.
func TestSignupEmailsNeedBillingAndMail(t *testing.T) {
	for _, c := range []struct {
		name          string
		billing, mail bool
		jobs          int
	}{{"no billing", false, true, 1}, {"no mail", true, false, 0}} {
		t.Run(c.name, func(t *testing.T) {
			w := newWelcomeEnv(t, c.billing, c.mail)
			w.mustSignIn(ana, "")
			if n := w.welcomeJobs(); n != c.jobs {
				t.Fatalf("%d welcome emails queued, want %d (the server trial's own)", n, c.jobs)
			}
			w.expectSignup("three days", 73*time.Hour, 0)
		})
	}
}
