package httpapi_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

// /v1/meta/webhook: off without a verify token; the subscription check echoes the challenge; an event needs the
// app secret's signature.
func TestMetaWebhookRoutes(t *testing.T) {
	e := newEnv(t)
	e.do(nil, "GET", "/v1/meta/webhook?hub.mode=subscribe&hub.verify_token=tok&hub.challenge=42", "", nil, 404, nil)
	env := map[string]string{"META_WEBHOOK_VERIFY_TOKEN": "tok", "PROVIDER_THREADS_CLIENT_SECRET": "th-secret"}
	e.svc.Getenv = func(k string) string { return env[k] }

	resp, err := http.Get(e.srv.URL + "/v1/meta/webhook?hub.mode=subscribe&hub.verify_token=tok&hub.challenge=42")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(got) != "42" {
		t.Fatalf("challenge: %d %q", resp.StatusCode, got)
	}
	e.do(nil, "GET", "/v1/meta/webhook?hub.mode=subscribe&hub.verify_token=nope&hub.challenge=42", "", nil, 403, nil)

	body := `{"values":[{"field":"replies","value":{"id":"r1","root_post":{"id":"none"}}}]}`
	post := func(sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/meta/webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", sig)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	mac := hmac.New(sha256.New, []byte("th-secret"))
	mac.Write([]byte(body))
	if code := post("sha256=" + hex.EncodeToString(mac.Sum(nil))); code != 200 {
		t.Fatalf("signed event: %d", code)
	}
	if code := post("sha256=00"); code != 403 {
		t.Fatalf("unsigned event: %d", code)
	}
}

// Meta's account callbacks take a form (signed_request): a forged one is 403, not 415.
func TestMetaAccountCallbackRoutes(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/v1/meta/deauthorize", "/v1/meta/data-deletion"} {
		resp, err := http.Post(e.srv.URL+path, "application/x-www-form-urlencoded", strings.NewReader("signed_request=abc.def"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
	e.do(nil, "GET", "/v1/meta/data-deletion/unknown", "", nil, 404, nil)
	// Any other route still refuses a form.
	resp, err := http.Post(e.srv.URL+"/v1/meta/webhook", "application/x-www-form-urlencoded", strings.NewReader("a=b"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 415 && resp.StatusCode != 404 {
		t.Fatalf("webhook with a form: %d", resp.StatusCode)
	}
}
