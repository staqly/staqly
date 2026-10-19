package httpapi_test

import (
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

const changePath = "/v1/account/sign-in-methods/email/change/"

func TestEmailChangeNeedsBothAddresses(t *testing.T) {
	e, clock := newEmailEnv(t, "")
	zoe := e.emailSignIn("zoe@example.com", nil)
	me := e.session(zoe).User.ID
	e.emailSignIn("bob@example.com", nil)

	// Step 2 before step 1: refused.
	var errOut apiErr
	e.as(zoe, "POST", changePath+"new/start", map[string]any{"email": "zoe@example.com", "new_email": "zoe@new.example"}, 409, &errOut)
	if errOut.Error.Code != "change_not_verified" {
		t.Fatalf("new before current: %+v", errOut)
	}
	// Only her own address.
	e.as(zoe, "POST", changePath+"current/start", map[string]any{"email": "bob@example.com"}, 404, nil)

	clock.add(31 * time.Second)
	e.as(zoe, "POST", changePath+"current/start", map[string]any{"email": "zoe@example.com"}, 202, nil)
	code := e.devCode("zoe@example.com")
	// A change code never signs anyone in.
	e.verifyCode("zoe@example.com", code, nil, 410, nil)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	e.as(zoe, "POST", changePath+"current/verify", map[string]any{"email": "zoe@example.com", "code": wrong}, 401, &errOut)
	if errOut.Error.Code != "code_invalid" {
		t.Fatalf("wrong code: %+v", errOut)
	}
	e.as(zoe, "POST", changePath+"current/verify", map[string]any{"email": "zoe@example.com", "code": code}, 204, nil)
	// Single use: the same code again does not confirm twice.
	e.as(zoe, "POST", changePath+"current/verify", map[string]any{"email": "zoe@example.com", "code": code}, 410, nil)

	e.as(zoe, "POST", changePath+"new/start", map[string]any{"email": "zoe@example.com", "new_email": "Zoe@Example.com"}, 422, nil)

	// The new address signs in someone else: refused after its code, and the confirmation survives.
	e.as(zoe, "POST", changePath+"new/start", map[string]any{"email": "zoe@example.com", "new_email": "bob@example.com"}, 202, nil)
	e.as(zoe, "POST", changePath+"new/verify", map[string]any{"email": "zoe@example.com", "new_email": "bob@example.com",
		"code": e.devCode("bob@example.com")}, 409, &errOut)
	if errOut.Error.Code != "identity_taken" {
		t.Fatalf("taken: %+v", errOut)
	}

	// A sign-in code for the new address does not change anything.
	e.sendCode("zoe@new.example", 202)
	e.as(zoe, "POST", changePath+"new/verify", map[string]any{"email": "zoe@example.com", "new_email": "zoe@new.example",
		"code": e.devCode("zoe@new.example")}, 410, nil)

	e.as(zoe, "POST", changePath+"new/start", map[string]any{"email": "zoe@example.com", "new_email": "zoe@new.example"}, 202, nil)
	var list struct {
		Data []app.SignInMethod `json:"data"`
	}
	e.as(zoe, "POST", changePath+"new/verify", map[string]any{"email": "zoe@example.com", "new_email": "zoe@new.example",
		"code": e.devCode("zoe@new.example")}, 200, &list)
	if len(list.Data) != 1 || list.Data[0].Provider != app.ProviderEmail || list.Data[0].Subject != "zoe@new.example" {
		t.Fatalf("after change: %+v", list)
	}
	// Same person, session still valid, contact address followed.
	if s := e.session(zoe); s.User.ID != me || s.User.Email != "zoe@new.example" {
		t.Fatalf("session after change: %+v", s.User)
	}
	// The confirmation was used up with the change.
	e.as(zoe, "POST", changePath+"new/start", map[string]any{"email": "zoe@new.example", "new_email": "zoe@third.example"}, 409, nil)

	clock.add(31 * time.Second)
	if s := e.session(e.emailSignIn("zoe@new.example", nil)); s.User.ID != me {
		t.Fatalf("new address signs in someone else: %+v", s.User)
	}
	if s := e.session(e.emailSignIn("zoe@example.com", nil)); s.User.ID == me {
		t.Fatal("the old address still signs Zoe in")
	}

	var events struct {
		Data []struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	e.as(zoe, "GET", "/v1/events?type=sign_in_method.changed", nil, 200, &events)
	if len(events.Data) != 1 || events.Data[0].Data["to"] != "zoe@new.example" {
		t.Fatalf("events: %+v", events)
	}
}
