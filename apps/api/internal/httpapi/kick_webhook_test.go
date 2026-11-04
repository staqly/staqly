package httpapi_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// /v1/kick/webhook: off without Kick's app; an event needs Kick's signature; one for a channel the server doesn't
// have answers 410, so Kick drops the subscription.
func TestKickWebhookRoute(t *testing.T) {
	e := newEnv(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	t.Cleanup(providers.UseKickKey(&key.PublicKey))
	body := `{"message_id":"m1","broadcaster":{"user_id":1,"channel_slug":"nobody-here"},"sender":{"user_id":2},"content":"hi"}`
	ts := time.Now().UTC().Format(time.RFC3339)
	post := func(sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/kick/webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Kick-Event-Message-Id", "01ROUTE")
		req.Header.Set("Kick-Event-Message-Timestamp", ts)
		req.Header.Set("Kick-Event-Signature", sig)
		req.Header.Set("Kick-Event-Type", "chat.message.sent")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	sum := sha256.Sum256([]byte("01ROUTE." + ts + "." + body))
	raw, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	sig := base64.StdEncoding.EncodeToString(raw)
	if code := post(sig); code != 404 {
		t.Fatalf("off: %d", code)
	}
	e.svc.Getenv = func(k string) string { return map[string]string{"PROVIDER_KICK_CLIENT_ID": "kick-app"}[k] }
	if code := post("AAAA"); code != 403 {
		t.Fatalf("unsigned event: %d", code)
	}
	if code := post(sig); code != 410 {
		t.Fatalf("unknown channel: %d", code)
	}
}
