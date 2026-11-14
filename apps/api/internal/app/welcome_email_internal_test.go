package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// Local mode has no plans, people or mail of its own: nothing is claimed and nothing is queued, even with mail
// set up (email alerts use their own settings).
func TestNoWelcomeEmailInLocalMode(t *testing.T) {
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: dir + "/uploads", LocalTokenPath: dir + "/access_token"})
	s.Getenv = func(k string) string {
		return map[string]string{"MAIL_SMTP_HOST": "127.0.0.1", "MAIL_FROM": "staqly <noreply@example.com>"}[k]
	}
	if err := s.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	var ws string
	if err := database.QueryRow(`SELECT id FROM workspaces`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	// The local workspace has no members; with one, nothing is queued either.
	now := s.now()
	if _, err := database.Exec(`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ('usr_1', 'owner@example.com', 'Owner', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at) VALUES (?, 'usr_1', 'owner', ?, ?)`, ws, now, now); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{welcomeTrial, welcomePaid, welcomePaidNew} {
		if err := s.tx(ctx, func(q *db.Queries) error { return s.queueWelcome(ctx, q, ws, kind, IntervalMonth) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.tx(ctx, func(q *db.Queries) error { return s.queueSignupEmails(ctx, q, ws) }); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM river_job WHERE kind = 'welcome_email'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("local mode queued %d welcome emails", jobs)
	}
	// The same call queues in cloud mode: the check above is not passing for want of an owner.
	s.Cfg.Mode = ModeCloud
	if err := s.tx(ctx, func(q *db.Queries) error { return s.queueWelcome(ctx, q, ws, welcomeTrial, "") }); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM river_job WHERE kind = 'welcome_email'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("cloud mode queued %d welcome emails (%v)", jobs, err)
	}
}

// What the two emails say. WELCOME_EMAIL_SAMPLES=<dir> also writes them there (HTML and text) to look at.
func TestWelcomeEmailTexts(t *testing.T) {
	base := "https://app.staqly.ai"
	ends := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		name    string
		f       welcomeFacts
		subject string
		want    []string
		not     []string
	}{
		{"trial", welcomeFacts{Kind: welcomeTrial, Workspace: "Acme <b>", Plan: "Starter", TrialEnds: ends, Card: true, Steps: true, Base: base},
			"Your staqly free trial has started",
			[]string{"free until 20 November 2026", "your card is charged for Starter", "Connect a channel", base + "/channels",
				base + "/posts?compose=1", base + "/schedule", base + "/settings/plan", "an owner of the workspace Acme <b>."},
			[]string{"read-only", "Unsubscribe"}},
		{"trial-without-card", welcomeFacts{Kind: welcomeTrial, Workspace: "Acme", Plan: "Pro", TrialEnds: ends, Steps: true, Base: base},
			"Your staqly free trial has started",
			[]string{"free until 20 November 2026", "becomes read-only until it has a plan", "Write your first post"},
			[]string{"card"}},
		{"paid", welcomeFacts{Kind: welcomePaid, Workspace: "Acme", Plan: "Plus", Interval: IntervalYear, Base: base},
			"You are on staqly Plus",
			[]string{"is now on the Plus plan, billed yearly.", "Invoices", "Manage billing: " + base + "/settings/plan"},
			[]string{"Connect a channel", "Unsubscribe"}},
		{"paid-without-trial", welcomeFacts{Kind: welcomePaid, Workspace: "Acme", Plan: "Starter", Interval: IntervalMonth, Steps: true, Base: base},
			"You are on staqly Starter",
			[]string{"billed monthly.", "Connect a channel", "Write your first post"}, nil},
		{"signup", welcomeFacts{Kind: emailSignup, FirstName: "Ana", Unsub: base + "/unsubscribe#t", Base: base},
			"Your staqly account is ready – start your free week",
			[]string{"Hi Ana 👋", "Thanks for creating your staqly account! 🎉", "Write once, publish everywhere", "Claude Code, Codex or n8n",
				"Start your 7-day free trial and publish your first post today.", "Start my free week: " + base + "/plans",
				"See you in your feed,\nThe staqly team", "PS: Questions?", "Unsubscribe from these emails: " + base + "/unsubscribe#t"},
			[]string{"**"}},
		{"signup-day1", welcomeFacts{Kind: emailSignupDay1, Unsub: base + "/unsubscribe#t", Base: base},
			"Your first post, scheduled in 2 minutes",
			[]string{"Hi there 👋", "Connect a channel", "Start your 7-day free trial.", "Start my free week: " + base + "/plans", "Unsubscribe"}, nil},
		{"signup-day3", welcomeFacts{Kind: emailSignupDay3, FirstName: "Ana", Unsub: base + "/unsubscribe#t", Base: base},
			"Your free week is still waiting",
			[]string{"7-day free trial hasn't started yet", "won't be charged before the trial ends", "last reminder",
				"Start my free week: " + base + "/plans", "Unsubscribe"}, nil},
	}
	out := os.Getenv("WELCOME_EMAIL_SAMPLES")
	for _, c := range cases {
		subject, d := welcomeEmail(c.f)
		d.To = "owner@example.com"
		htmlBody, text := renderEmail(d, base+"/brand/icon-192.png")
		if subject != c.subject {
			t.Errorf("%s: subject %q", c.name, subject)
		}
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: text lacks %q:\n%s", c.name, w, text)
			}
		}
		for _, w := range c.not {
			if strings.Contains(text, w) {
				t.Errorf("%s: text has %q:\n%s", c.name, w, text)
			}
		}
		if strings.Contains(htmlBody, "<b>") || strings.Contains(htmlBody, "**") {
			t.Errorf("%s: HTML not escaped or bold left as stars", c.name)
		}
		if out != "" {
			for ext, body := range map[string]string{".html": htmlBody, ".txt": "Subject: " + subject + "\n\n" + text} {
				if err := os.WriteFile(filepath.Join(out, "welcome-"+c.name+ext), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
