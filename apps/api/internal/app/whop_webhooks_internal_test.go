package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

func whopSign(secret, id, ts, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(id + "." + ts + "." + body))
	return "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// Whop's member events reach a channel through its own webhook: signed with that channel's secret, counted once each in
// the analytics' Audience; a wrong secret, an old call or a channel staqly doesn't have is refused.
func TestWhopWebhookAudience(t *testing.T) {
	s, p, _, _, chans := publishedOn(t, "fakewhopaud")
	ctx := context.Background()
	sealed, err := s.seal(providers.Credentials{"api_key": "k", "account_id": "biz_1", "webhook_id": "hook_1", "webhook_secret": "ws_secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'whop', credentials = ? WHERE id = ?`, sealed, chans[0]); err != nil {
		t.Fatal(err)
	}
	now := strconv.FormatInt(s.Now().Unix(), 10)
	call := func(id, ts, body, secret string) (int, error) {
		return s.HandleWhopWebhook(ctx, chans[0], WhopCall{ID: id, Timestamp: ts, Signature: whopSign(secret, id, ts, body)}, []byte(body))
	}
	member := `{"type":"member.created","data":{"id":"mber_1"}}`
	membership := `{"type":"membership.activated","data":{"id":"mem_1","status":"active"}}`
	if n, err := call("msg_1", now, member, "ws_secret"); err != nil || n != 1 {
		t.Fatalf("member: %d %v", n, err)
	}
	if n, err := call("msg_2", now, membership, "ws_secret"); err != nil || n != 1 {
		t.Fatalf("membership: %d %v", n, err)
	}
	if n, err := call("msg_1", now, member, "ws_secret"); err != nil || n != 0 {
		t.Fatalf("the same call again: %d %v", n, err)
	}
	if n, err := call("msg_3", now, `{"type":"payment.succeeded"}`, "ws_secret"); err != nil || n != 0 {
		t.Fatalf("an event staqly doesn't count: %d %v", n, err)
	}
	var e *Error
	if _, err := call("msg_4", now, member, "ws_other"); !errors.As(err, &e) || e.Status != 403 {
		t.Fatalf("another secret: %v", err)
	}
	old := strconv.FormatInt(s.Now().Unix()-600, 10)
	if _, err := call("msg_5", old, member, "ws_secret"); !errors.As(err, &e) || e.Status != 403 {
		t.Fatalf("ten minutes old: %v", err)
	}
	if _, err := s.HandleWhopWebhook(ctx, "ch_gone", WhopCall{ID: "m", Timestamp: now, Signature: "v1,x"}, []byte(member)); !errors.As(err, &e) || e.Status != 410 {
		t.Fatalf("no such channel: %v", err)
	}
	rep, err := s.Analytics(ctx, p, AnalyticsQuery{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Audience == nil || rep.Audience.Totals.Follows != 1 || rep.Audience.Totals.Subscriptions != 1 {
		t.Fatalf("audience %+v", rep.Audience)
	}
}
