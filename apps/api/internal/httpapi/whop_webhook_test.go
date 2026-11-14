package httpapi_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// /v1/whop/webhook/{channel}: a channel the server doesn't have answers 410 (Whop stops sending), an unsigned call
// to one it has 403.
func TestWhopWebhookRoute(t *testing.T) {
	e := newEnv(t)
	ch := e.customChannel("Forum")
	if _, err := e.svc.DB.Exec(`UPDATE channels SET provider = 'whop' WHERE id = ?`, ch.ID); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"member.created"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	post := func(channel, sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/whop/webhook/"+channel, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("webhook-id", "msg_1")
		req.Header.Set("webhook-timestamp", ts)
		req.Header.Set("webhook-signature", sig)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	m := hmac.New(sha256.New, []byte("ws_unknown"))
	m.Write([]byte("msg_1." + ts + "." + body))
	sig := "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
	if code := post("ch_nope", sig); code != 410 {
		t.Fatalf("unknown channel: %d", code)
	}
	if code := post(ch.ID, sig); code != 403 {
		t.Fatalf("a channel without this secret: %d", code)
	}
}
