package httpapi_test

import (
	"context"
	"io"
	"mime/quotedprintable"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Personal notifications: defaults, choices, who gets a post's email (its author; everyone who follows
// every post), one email per post listing its channels, the unsubscribe link, and nothing without the email on.
func TestPersonalNotifications(t *testing.T) {
	e := newAccountsEnv(t, "")
	host, port, inbox := fakeSMTP(t)
	prev := e.svc.Getenv
	e.svc.Getenv = func(k string) string {
		if v, ok := map[string]string{"MAIL_SMTP_HOST": host, "MAIL_SMTP_PORT": itoa(port), "MAIL_SMTP_TLS": "none",
			"MAIL_FROM": "staqly <noreply@example.com>"}[k]; ok {
			return v
		}
		if prev != nil {
			return prev(k)
		}
		return ""
	}
	ctx := context.Background()
	owner := e.mustSignIn(ana, "")

	// Defaults: every event on, my posts only.
	var prefs app.NotificationPrefs
	e.as(owner, "GET", "/v1/account/notifications", nil, 200, &prefs)
	if prefs.Scope != "mine" || len(prefs.Events) != len(app.PersonalEvents) || !prefs.MailReady || prefs.Email != ana.Email {
		t.Fatalf("defaults: %+v", prefs)
	}
	for _, c := range prefs.Events {
		if !c.Email || !c.InApp {
			t.Fatalf("default choice: %+v", c)
		}
	}
	e.as(owner, "PUT", "/v1/account/notifications", map[string]any{"events": []map[string]any{{"event": "nope", "email": true}}}, 422, nil)
	e.as(owner, "PUT", "/v1/account/notifications", map[string]any{"scope": "everyone"}, 422, nil)

	var ch app.Channel
	e.as(owner, "POST", "/v1/channels", map[string]any{"provider": "custom", "name": "My n8n"}, 201, &ch)
	publish := func(content string) {
		t.Helper()
		var post app.Post
		e.as(owner, "POST", "/v1/posts", map[string]any{"title": "Post", "collection": "Default", "content": content,
			"targets": []map[string]any{{"channel_id": ch.ID}}, "publish_now": true}, 201, &post)
		e.svc.RunDueJobsOnce(ctx)
		e.as(owner, "POST", "/v1/posts/"+post.ID+"/targets/"+ch.ID+"/result",
			map[string]any{"status": "published", "url": "https://example.com/p/" + post.ID}, 200, nil)
		for range 3 { // notify_people, then notify_person
			e.svc.RunDueJobsOnce(ctx)
		}
	}

	publish("Launch day <b>is here</b>\nmore text")
	var msg smtpMsg
	select {
	case msg = <-inbox:
	case <-time.After(5 * time.Second):
		t.Fatal("no email for a published post")
	}
	body := decodeQP(msg.Data)
	for _, want := range []string{"Your post was published on Your own automation", "Launch day &lt;b&gt;is here&lt;/b&gt;",
		"View post", "/settings/account#notifications", "List-Unsubscribe: <", "List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		"it's your post"} {
		if !strings.Contains(body, want) {
			t.Fatalf("email lacks %q:\n%s", want, body)
		}
	}
	if i := strings.Index(body, "<!doctype html>"); i < 0 || strings.Contains(body[i:], "<b>is here</b>") {
		t.Fatal("post text not escaped in the HTML part")
	}
	select { // one email per post and person
	case extra := <-inbox:
		t.Fatalf("second email: %s", extra.Data)
	case <-time.After(300 * time.Millisecond):
	}

	// The one-click link turns that email off; a forged one is refused.
	token := regexp.MustCompile(`/unsubscribe\?token=([A-Za-z0-9_\-.]+)`).FindStringSubmatch(body)
	if token == nil {
		t.Fatalf("no unsubscribe token:\n%s", body)
	}
	e.do(nil, "POST", "/v1/notifications/unsubscribe?token="+token[1]+"x", "", nil, 400, nil)
	var res struct{ Event string }
	e.do(nil, "POST", "/v1/notifications/unsubscribe", "", map[string]any{"token": token[1]}, 200, &res)
	if res.Event != "post.published" {
		t.Fatalf("unsubscribed from %q", res.Event)
	}
	e.as(owner, "GET", "/v1/account/notifications", nil, 200, &prefs)
	for _, c := range prefs.Events {
		if c.Event == "post.published" && (c.Email || !c.InApp) {
			t.Fatalf("after unsubscribe: %+v", c)
		}
	}
	publish("Second post")
	select {
	case m := <-inbox:
		t.Fatalf("email after unsubscribing: %s", decodeQP(m.Data))
	case <-time.After(500 * time.Millisecond):
	}

	// An API key's posts belong to the key's creator; turning the email back on brings it back.
	e.as(owner, "PUT", "/v1/account/notifications", map[string]any{"events": []map[string]any{{"event": "post.published", "email": true, "in_app": true}}}, 200, &prefs)
	publish("Third post")
	select {
	case m := <-inbox:
		if !strings.Contains(decodeQP(m.Data), "Third post") {
			t.Fatalf("third: %s", m.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no email after turning it back on")
	}

	// An editor who follows every post gets the owner's posts too; one who keeps "my posts" doesn't.
	_, tok := e.invite(owner, bob.Email, app.RoleEditor)
	bobP := e.mustSignIn(bob, "invite="+url.QueryEscape(tok))
	e.as(bobP, "PUT", "/v1/account/notifications", map[string]any{"scope": "all"}, 200, nil)
	drain := func() {
		for {
			select {
			case <-inbox:
			case <-time.After(300 * time.Millisecond):
				return
			}
		}
	}
	for range 3 { // the owner's "joined" email goes out first
		e.svc.RunDueJobsOnce(ctx)
	}
	joined := false
	for !joined {
		select {
		case m := <-inbox:
			joined = strings.Contains(decodeQP(m.Data), "Bob joined")
		case <-time.After(5 * time.Second):
			t.Fatal("no email to the owner when Bob joined")
		}
	}
	drain()
	publish("Fourth post")
	got := map[string]bool{}
	for range 2 {
		select {
		case m := <-inbox:
			got[strings.Join(m.To, ",")] = true
			if strings.Contains(m.To[0], bob.Email) && !strings.Contains(decodeQP(m.Data), "you follow every post") {
				t.Fatalf("bob's reason: %s", decodeQP(m.Data))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("only %v got the fourth post", got)
		}
	}
	e.as(bobP, "PUT", "/v1/account/notifications", map[string]any{"scope": "mine"}, 200, nil)
	publish("Fifth post")
	select {
	case m := <-inbox:
		if strings.Contains(m.To[0], bob.Email) {
			t.Fatal("bob got a post he didn't write with my posts only")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the owner got no email for the fifth post")
	}
	select {
	case m := <-inbox:
		t.Fatalf("extra email to %v", m.To)
	case <-time.After(300 * time.Millisecond):
	}
}

func decodeQP(raw string) string {
	var b strings.Builder
	for _, part := range strings.Split(raw, "\r\n\r\n") {
		b.WriteString(part + "\n")
		if dec, err := readAllQP(part); err == nil {
			b.WriteString(dec + "\n")
		}
	}
	return b.String()
}

func readAllQP(s string) (string, error) {
	b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(s)))
	return string(b), err
}

// Independent partial updates must merge, including when two settings tabs save at once.
func TestConcurrentNotificationPrefs(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	for range 10 {
		reset := []app.NotificationChoice{}
		for _, event := range app.PersonalEvents {
			reset = append(reset, app.NotificationChoice{Event: event, Email: true, InApp: true})
		}
		e.as(owner, "PUT", "/v1/account/notifications", map[string]any{"scope": "mine", "events": reset}, 200, nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, event := range app.PersonalEvents {
			wg.Go(func() {
				<-start
				e.as(owner, "PUT", "/v1/account/notifications", map[string]any{"events": []app.NotificationChoice{{Event: event, Email: false, InApp: true}}}, 200, nil)
			})
		}
		close(start)
		wg.Wait()
		var prefs app.NotificationPrefs
		e.as(owner, "GET", "/v1/account/notifications", nil, 200, &prefs)
		for _, c := range prefs.Events {
			if c.Email {
				t.Fatalf("concurrent preference lost: %+v", prefs)
			}
		}
	}
}
