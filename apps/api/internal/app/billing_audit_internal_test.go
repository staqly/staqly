package app

import (
	"context"
	"strconv"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Audit 5 (billing audit of 2026-11-13): a publish holds a bought X credit while X is asked; the purchase is
// refunded in full meanwhile; X then rejects the post. Releasing the reservation must not bring the refunded
// credit back.
func TestAuditRefundedCreditsStayRefundedWhenAReservationIsReleased(t *testing.T) {
	for _, tc := range []struct {
		name  string
		units int64 // the bought bundle
	}{{"a bundle of one", 1}, {"a bundle of five", 5}} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, posts, _, _ := allowanceFixture(t, "x", &allowanceProvider{})
			ctx := context.Background()
			if _, err := s.W.InsertUsageEvent(ctx, db.InsertUsageEventParams{WorkspaceID: p.WorkspaceID, Provider: ProviderX, Ref: "last-included",
				Period: s.period(), Units: 1, Included: 1, CreatedAt: s.now()}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.W.InsertCreditGrant(ctx, db.InsertCreditGrantParams{ID: "purchased", WorkspaceID: p.WorkspaceID, Provider: ProviderX,
				Units: tc.units, Source: "stripe:cs_1", ExpiresAt: formatTime(s.monthEnd()), CreatedAt: s.now()}); err != nil {
				t.Fatal(err)
			}
			ref := "audit:" + posts[0].ID
			fresh, err := s.reservePublishingAllowance(ctx, p.WorkspaceID, ref, ProviderX, 1, false)
			if err != nil || !fresh {
				t.Fatalf("reserve: fresh=%v, %v", fresh, err)
			}
			if x, err := s.xPosts(ctx, s.Q, p.WorkspaceID); err != nil || x.CreditsLeft != tc.units-1 {
				t.Fatalf("setup: one bought credit is reserved: %+v, %v", x, err)
			}
			// charge.refunded for the whole purchase, as HandleStripeWebhook applies it.
			cs := creditSession{ID: "cs_1", Metadata: map[string]string{"staqly_posts": strconv.FormatInt(tc.units, 10)}}
			if err := s.tx(ctx, func(q *db.Queries) error {
				return s.revokeCredits(ctx, q, cs, creditRefund{PaymentIntent: "pi_1", Amount: 900, Taken: 900}, "charge.refunded")
			}); err != nil {
				t.Fatal(err)
			}
			// X rejects the post: the reservation is released.
			if err := s.finishPublishingAllowance(ctx, p.WorkspaceID, ref, ProviderX, fresh,
				&providers.Failure{Kind: providers.ErrRejected, Msg: "invalid post"}); err != nil {
				t.Fatal(err)
			}
			x, err := s.xPosts(ctx, s.Q, p.WorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			if x.CreditsLeft != 0 {
				t.Fatalf("a fully refunded purchase left %d bought credits to publish with after the reserved one was released, want 0", x.CreditsLeft)
			}
		})
	}
}
