package httpapi_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

// /v1/tiktok/webhook: off without TikTok's app; an event needs the client secret's signature (TikTok's Test URL
// sends one).
func TestTikTokWebhookRoute(t *testing.T) {
	e := newEnv(t)
	body := `{"client_key":"ck","event":"post.publish.complete","create_time":1,"user_openid":"o1","content":"{}"}`
	post := func(sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/tiktok/webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("TikTok-Signature", sig)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post("t=1,s=00"); code != 404 {
		t.Fatalf("off: %d", code)
	}
	e.svc.Getenv = func(k string) string { return map[string]string{"PROVIDER_TIKTOK_CLIENT_SECRET": "tt-secret"}[k] }
	mac := hmac.New(sha256.New, []byte("tt-secret"))
	mac.Write([]byte("1." + body))
	if code := post("t=1,s=" + hex.EncodeToString(mac.Sum(nil))); code != 200 {
		t.Fatalf("signed event: %d", code)
	}
	if code := post("t=1,s=00"); code != 403 {
		t.Fatalf("unsigned event: %d", code)
	}
}
