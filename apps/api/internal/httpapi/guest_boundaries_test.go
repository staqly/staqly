package httpapi_test

import (
	"context"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func (e *acctEnv) guestFor(email, workspace string) *person {
	e.t.Helper()
	link, err := e.svc.CreateGuestLink(context.Background(), email, workspace, app.GuestLink{For: time.Hour, Role: app.RoleOperator})
	if err != nil {
		e.t.Fatal(err)
	}
	u, err := url.Parse(link.URL)
	if err != nil {
		e.t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	guest := &person{c: noRedirects(jar)}
	var redeemed struct {
		CSRF string `json:"csrf_token"`
	}
	e.do(guest.c, "POST", "/v1/device-links/redeem", "", map[string]any{"token": u.Fragment}, 201, &redeemed)
	guest.csrf = redeemed.CSRF
	if !e.session(guest).Guest {
		e.t.Fatal("expected a guest session")
	}
	return guest
}

func TestGuestCannotFinishOwnerEmailLink(t *testing.T) {
	e, _ := newEmailEnv(t, "")
	owner := e.mustSignIn(ana, "")
	guest := e.guestFor(ana.Email, "")
	const email = "new@example.com"
	e.as(owner, "POST", "/v1/account/sign-in-methods/email/start", map[string]any{"email": email}, 202, nil)
	body := map[string]any{"email": email, "code": e.devCode(email)}
	e.as(guest, "POST", "/v1/account/sign-in-methods/email/verify", body, 403, nil)
	e.as(owner, "POST", "/v1/account/sign-in-methods/email/verify", body, 200, nil)
	if e.session(e.emailSignIn(email, nil)).User.ID != e.session(owner).User.ID {
		t.Fatal("owner could not complete email link")
	}
}

func TestGuestCannotVerifyOwnerEmailChange(t *testing.T) {
	for _, step := range []string{"current", "new"} {
		t.Run(step, func(t *testing.T) {
			e, clock := newEmailEnv(t, "")
			const email = "owner@example.com"
			owner := e.emailSignIn(email, nil)
			guest := e.guestFor(email, "")
			clock.add(31 * time.Second)
			e.as(owner, "POST", changePath+"current/start", map[string]any{"email": email}, 202, nil)
			body := map[string]any{"email": email, "code": e.devCode(email)}
			status := 204
			if step == "new" {
				e.as(owner, "POST", changePath+"current/verify", body, 204, nil)
				body = map[string]any{"email": email, "new_email": "new@example.com"}
				e.as(owner, "POST", changePath+"new/start", body, 202, nil)
				body["code"] = e.devCode("new@example.com")
				status = 200
			}
			e.as(guest, "POST", changePath+step+"/verify", body, 403, nil)
			// Refusing the guest leaves the pending proof usable by the owner.
			e.as(owner, "POST", changePath+step+"/verify", body, status, nil)
		})
	}
}

func TestGuestStaysInLinkWorkspace(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	first := e.session(owner).Workspace.ID
	var other app.MyWorkspace
	e.as(owner, "POST", "/v1/account/workspaces", map[string]any{"name": "Private"}, 201, &other)
	guest := e.guestFor(ana.Email, first)
	t.Run("list", func(t *testing.T) {
		var mine struct {
			Data []app.MyWorkspace `json:"data"`
		}
		e.as(guest, "GET", "/v1/workspaces", nil, 200, &mine)
		if len(mine.Data) != 1 || mine.Data[0].ID != first || mine.Data[0].Role != app.RoleOperator {
			t.Fatalf("guest workspaces: %+v", mine.Data)
		}
	})
	e.as(guest, "POST", "/v1/session/workspace", map[string]any{"workspace_id": other.ID}, 403, nil)
	if e.session(guest).Workspace.ID != first {
		t.Fatal("guest left the link workspace")
	}
	e.as(owner, "POST", "/v1/session/workspace", map[string]any{"workspace_id": other.ID}, 204, nil)
}

func TestGuestCannotAcceptOwnerInvitation(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner := e.mustSignIn(ana, "")
	guest := e.guestFor(ana.Email, "")
	other := e.mustSignIn(eve, "")
	_, token := e.invite(other, ana.Email, app.RoleEditor)
	body := map[string]any{"token": token}
	e.as(guest, "POST", "/v1/invitations/accept", body, 403, nil)
	e.as(owner, "POST", "/v1/invitations/accept", body, 200, nil)
}
