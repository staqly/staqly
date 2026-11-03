package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
)

func TestAdminDeleteUserRechecksMembershipAfterBillingCancellation(t *testing.T) {
	for _, change := range []string{"shared_last_owner", "solo_gains_member"} {
		t.Run(change, func(t *testing.T) {
			s, a, _ := actionsTest(t)
			ctx := t.Context()
			person(t, s, "u_delete", "ws_solo", RoleOwner)
			person(t, s, "u_delete", "ws_shared", RoleOwner)
			person(t, s, "u_other", "ws_shared", RoleOwner)
			// Paused subscriptions still have to be cancelled before deleting a solo workspace.
			if err := s.W.SetSubscription(ctx, db.SetSubscriptionParams{StripeSubscriptionID: nullStr("sub_paused"), SubscriptionStatus: nullStr("paused"), UpdatedAt: s.now(), ID: "ws_solo"}); err != nil {
				t.Fatal(err)
			}
			changed := make(chan error, 1)
			stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if change == "shared_last_owner" {
					p := Principal{WorkspaceID: "ws_shared", Kind: PrincipalSession, UserID: "u_delete", Role: RoleOwner, Scopes: RoleScopes[RoleOwner]}
					_, err := s.UpdateMemberRole(ctx, p, "u_other", RoleEditor)
					changed <- err
				} else {
					changed <- s.tx(ctx, func(q *db.Queries) error {
						_, err := q.AddMember(ctx, db.AddMemberParams{WorkspaceID: "ws_solo", UserID: "u_other", Role: RoleEditor, CreatedAt: s.now(), UpdatedAt: s.now()})
						return err
					})
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer stripe.Close()
			s.Cfg.Billing = BillingConfig{SecretKey: "sk_test", WebhookSecret: "whsec", APIBase: stripe.URL}
			err := s.AdminDeleteUser(ctx, a, "u_delete", "u_delete@x.io")
			select {
			case changeErr := <-changed:
				if changeErr != nil {
					t.Fatal(changeErr)
				}
			default:
				t.Fatalf("mocked billing cancellation was not called: %v", err)
			}
			if e, ok := err.(*Error); !ok || e.Status != 409 {
				t.Errorf("delete after membership changed: %v, want conflict", err)
			}
			if _, err := s.Q.GetUser(ctx, "u_delete"); err != nil {
				t.Errorf("user removed after membership changed: %v", err)
			}
			ws := "ws_shared"
			if change == "solo_gains_member" {
				ws = "ws_solo"
			}
			if _, err := s.Q.GetWorkspace(ctx, ws); err != nil {
				t.Errorf("shared workspace deleted: %v", err)
			}
			owners, err := s.Q.CountOwners(ctx, ws)
			if err != nil || owners == 0 {
				t.Fatalf("shared workspace left without an owner: %d %v", owners, err)
			}
			if auditCount(t, s, "delete_user") != 0 {
				t.Fatal("successful deletion audit recorded for refused deletion")
			}
		})
	}
}
