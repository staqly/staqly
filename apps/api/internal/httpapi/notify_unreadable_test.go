package httpapi_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// An alert whose sealed secrets this server's key can't open (a restore without notify_secret.key, a changed
// NOTIFY_SECRET_KEY) was a 500 on every read of it, the list included, and on its test. Now it lists with the
// reason, its test is a 422, a delivery counts as a failure of the alert and sends nothing, a rename keeps the
// stored secrets, and entering the secret again makes it work.
func TestAlertWithUnreadableSecrets(t *testing.T) {
	fake := newFakeChat(t)
	e, _ := newNotifyEnv(t, fake)
	col := e.collection("Posts")
	ch := e.channel(map[string]any{"name": "Push", "type": "ntfy", "events": []string{"post.created"},
		"config": map[string]any{"server_url": fake.srv.URL + "/ntfy", "topic": "mytopic", "access_token": "tk_secretsecretsecret"}}, 201)
	foreign := "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 48)) // sealed under some other key
	if _, err := e.svc.DB.Exec(`UPDATE notification_channels SET secrets = ? WHERE id = ?`, foreign, ch.ID); err != nil {
		t.Fatal(err)
	}
	csrf := []string{"X-CSRF-Token", e.csrf}

	var list struct{ Data []app.Alert }
	e.do(e.browser, "GET", "/v1/alerts", "", nil, 200, &list)
	if len(list.Data) != 1 || !strings.Contains(list.Data[0].LastError, "cannot be read") {
		t.Fatalf("list: %+v", list.Data)
	}
	var apiErr struct{ Error app.Error }
	e.do(e.browser, "POST", "/v1/alerts/"+ch.ID+"/test", "", nil, 422, &apiErr, csrf...)
	if apiErr.Error.Code != "secrets_unreadable" || !strings.Contains(apiErr.Error.Message, "enter them again") {
		t.Fatalf("test: %+v", apiErr)
	}

	e.post(col.ID, map[string]any{"content": "one"})
	e.process()
	var got app.Alert
	e.do(e.browser, "GET", "/v1/alerts/"+ch.ID, "", nil, 200, &got)
	if len(fake.find("/ntfy")) != 0 || got.FailCount != 1 || !strings.Contains(got.LastError, "cannot be read") {
		t.Fatalf("delivery: sent %d, alert %+v", len(fake.find("/ntfy")), got)
	}

	e.do(e.browser, "PATCH", "/v1/alerts/"+ch.ID, "", map[string]any{"name": "Renamed"}, 200, nil, csrf...)
	var sec string
	if err := e.svc.DB.QueryRow(`SELECT secrets FROM notification_channels WHERE id = ?`, ch.ID).Scan(&sec); err != nil || sec != foreign {
		t.Fatalf("a rename replaced the stored secrets: %v", err)
	}

	e.do(e.browser, "PATCH", "/v1/alerts/"+ch.ID, "", map[string]any{"config": map[string]any{"access_token": "tk_newsecretnewsecret"}}, 200, nil, csrf...)
	var res map[string]any
	e.do(e.browser, "POST", "/v1/alerts/"+ch.ID+"/test", "", nil, 200, &res, csrf...)
	if res["ok"] != true || len(fake.find("/ntfy")) != 1 {
		t.Fatalf("after entering the secret again: %v", res)
	}
}
