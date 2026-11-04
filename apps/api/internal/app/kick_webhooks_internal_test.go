package app

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

func kickCall(t *testing.T, key *rsa.PrivateKey, id, ts, typ, body string) KickCall {
	t.Helper()
	sum := sha256.Sum256([]byte(id + "." + ts + "." + body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return KickCall{MessageID: id, Timestamp: ts, Signature: base64.StdEncoding.EncodeToString(sig), Type: typ}
}

// A chat reply to a post's Kick message becomes a comment under the post, once per Kick message id; the
// channel's own messages, unsigned or old calls and other channels are not.
func TestKickWebhookChatReply(t *testing.T) {
	s, p, post, _, chans := publishedOn(t, "fakekickpub")
	ctx := context.Background()
	s.Getenv = func(k string) string { return map[string]string{"PROVIDER_KICK_CLIENT_ID": "kick-app"}[k] }
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'kick', profile_url = 'https://kick.com/jane-doe' WHERE id = ?`, chans[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE post_targets SET external_id = 'msg-post' WHERE post_id = ?`, post.ID); err != nil {
		t.Fatal(err)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	t.Cleanup(providers.UseKickKey(&key.PublicKey))
	ts := s.Now().UTC().Format(time.RFC3339)
	reply := `{"message_id":"msg-r1","replies_to":{"message_id":"msg-post","content":"Live at 8"},` +
		`"broadcaster":{"user_id":4242,"username":"Jane","channel_slug":"jane-doe"},` +
		`"sender":{"user_id":7,"username":"Viewer","channel_slug":"viewer"},"content":"See you [emote:1:HYPERCLAP]",` +
		`"created_at":"2026-11-04T10:00:00Z"}`

	bad := kickCall(t, key, "01A", ts, "chat.message.sent", reply)
	bad.Signature = kickCall(t, key, "01B", ts, "chat.message.sent", reply).Signature
	if _, err := s.HandleKickWebhook(ctx, bad, []byte(reply)); err == nil {
		t.Fatal("a signature for another message id must be refused")
	}
	old := s.Now().Add(-4 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := s.HandleKickWebhook(ctx, kickCall(t, key, "01C", old, "chat.message.sent", reply), []byte(reply)); err == nil {
		t.Fatal("a call days old must be refused")
	}
	if n, err := s.HandleKickWebhook(ctx, kickCall(t, key, "01A", ts, "chat.message.sent", reply), []byte(reply)); err != nil || n != 1 {
		t.Fatalf("reply: %d %v", n, err)
	}
	if n, err := s.HandleKickWebhook(ctx, kickCall(t, key, "01A", ts, "chat.message.sent", reply), []byte(reply)); err != nil || n != 0 {
		t.Fatalf("the same call again: %d %v", n, err)
	}
	got, err := s.PostResponses(ctx, p, post.ID, "")
	if err != nil || len(got.Channels) != 1 || !got.Channels[0].Available || len(got.Channels[0].Responses) != 1 {
		t.Fatalf("responses %+v %v", got, err)
	}
	if r := got.Channels[0].Responses[0]; r.Text != "See you HYPERCLAP" || r.AuthorHandle != "@viewer" || r.Author != "Viewer" {
		t.Fatalf("comment %+v", r)
	}

	own := `{"message_id":"msg-r2","replies_to":{"message_id":"msg-post"},"broadcaster":{"user_id":4242,"channel_slug":"jane-doe"},` +
		`"sender":{"user_id":4242,"username":"Jane","channel_slug":"jane-doe"},"content":"thread part"}`
	if n, err := s.HandleKickWebhook(ctx, kickCall(t, key, "01D", ts, "chat.message.sent", own), []byte(own)); err != nil || n != 0 {
		t.Fatalf("own message: %d %v", n, err)
	}
	chat := `{"message_id":"msg-r3","broadcaster":{"user_id":4242,"channel_slug":"jane-doe"},"sender":{"user_id":8},"content":"hi"}`
	if n, err := s.HandleKickWebhook(ctx, kickCall(t, key, "01E", ts, "chat.message.sent", chat), []byte(chat)); err != nil || n != 0 {
		t.Fatalf("not a reply: %d %v", n, err)
	}
	gone := `{"message_id":"msg-x","replies_to":{"message_id":"msg-post"},"broadcaster":{"user_id":1,"channel_slug":"someone-else"},"sender":{"user_id":8}}`
	var e *Error
	if _, err := s.HandleKickWebhook(ctx, kickCall(t, key, "01F", ts, "chat.message.sent", gone), []byte(gone)); !errors.As(err, &e) || e.Status != 410 {
		t.Fatalf("unknown channel: %v", err)
	}
}
