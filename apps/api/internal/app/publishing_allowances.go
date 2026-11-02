package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

type publishingCreditDraw struct {
	ID    string `json:"id"`
	Units int64  `json:"units"`
}

// Reserve the business allowance before HTTP, atomically across every worker. Charging the
// original period and grants also makes retries across midnight/month-end idempotent.
// An existing charge may represent a lost answer or a crashed worker: never refund it
// merely because a later attempt is refused. Scheduled cancellations never reach here.
func (s *Service) reservePublishingAllowance(ctx context.Context, workspaceID, ref, provider string, units int64, video bool) (bool, error) {
	if s.Cfg.Mode == ModeLocal || (provider != ProviderX && !isLinkedIn(provider)) {
		return false, nil
	}
	fresh := false
	err := s.tx(ctx, func(q *db.Queries) error {
		fresh = false // serializable transactions may be retried
		charged, err := q.GetPublishingAllowanceCharge(ctx, db.GetPublishingAllowanceChargeParams{WorkspaceID: workspaceID, Ref: ref})
		if err != nil {
			return err
		}
		if charged > 0 {
			meter, need := MeterLinkedIn, int64(1)
			if provider == ProviderX {
				meter, need = ProviderX, units
			} else if video {
				meter = MeterLinkedInVideo
			}
			reserved, err := q.GetPublishingAllowanceUnits(ctx, db.GetPublishingAllowanceUnitsParams{WorkspaceID: workspaceID, Ref: ref, Provider: meter})
			if err != nil {
				return err
			}
			if reserved < need {
				return conflict("This target already reserved a smaller publishing allowance; restore its original content or create a new post")
			}
			return nil
		}
		var draws []publishingCreditDraw
		if provider == ProviderX {
			if err := s.checkX(ctx, q, workspaceID, units); err != nil {
				return err
			}
			before, err := q.ListLiveCreditGrants(ctx, db.ListLiveCreditGrantsParams{WorkspaceID: workspaceID, Provider: ProviderX, ExpiresAt: s.now()})
			if err != nil {
				return err
			}
			if err := s.meterX(ctx, q, workspaceID, ref, units); err != nil {
				return err
			}
			for _, grant := range before {
				used, err := q.GetPublishingCreditGrantUsed(ctx, db.GetPublishingCreditGrantUsedParams{WorkspaceID: workspaceID, ID: grant.ID})
				if err != nil {
					return err
				}
				if used > grant.Used {
					draws = append(draws, publishingCreditDraw{ID: grant.ID, Units: used - grant.Used})
				}
			}
		} else {
			var videos int64
			if video {
				videos = 1
			}
			if err := s.checkLinkedIn(ctx, q, workspaceID, 1, videos); err != nil {
				return err
			}
			if err := s.meterLinkedIn(ctx, q, workspaceID, ref, video); err != nil {
				return err
			}
		}
		if err := q.InsertPublishingAllowanceReservation(ctx, db.InsertPublishingAllowanceReservationParams{WorkspaceID: workspaceID, Ref: ref, CreditDraws: mustJSON(draws)}); err != nil {
			return err
		}
		fresh = true
		return nil
	})
	if err == nil && ctx.Err() != nil {
		// The transaction committed, but the job was cancelled before provider HTTP.
		// Return a fresh reservation with an independent write budget.
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
		defer cancel()
		refundErr := s.finishPublishingAllowance(wctx, workspaceID, ref, provider, fresh,
			&providers.Failure{Kind: providers.ErrRejected, Msg: "cancelled before publishing"})
		return false, errors.Join(ctx.Err(), refundErr)
	}
	return fresh, err
}

// Definite rejection of a fresh attempt returns its exact charge, including purchased
// credits. Unknown outcomes keep it: a timeout or process crash may have published.
func (s *Service) finishPublishingAllowance(ctx context.Context, workspaceID, ref, provider string, fresh bool, outcome error) error {
	if s.Cfg.Mode == ModeLocal || (provider != ProviderX && !isLinkedIn(provider)) {
		return nil
	}
	if outcome == nil {
		return nil // marker cleanup belongs to the transaction that records the published target/reply
	}
	var failure *providers.Failure
	definite := errors.As(outcome, &failure) && !failure.OutcomeUnknown &&
		(errors.Is(outcome, providers.ErrRejected) || errors.Is(outcome, providers.ErrReauth) || !providers.RetryAt(outcome).IsZero())
	if outcome != nil && (!fresh || !definite) {
		return nil
	}
	return s.tx(ctx, func(q *db.Queries) error {
		if outcome != nil {
			raw, err := q.GetPublishingAllowanceReservation(ctx, db.GetPublishingAllowanceReservationParams{WorkspaceID: workspaceID, Ref: ref})
			if isNoRows(err) {
				return nil
			}
			if err != nil {
				return err
			}
			var draws []publishingCreditDraw
			if err := json.Unmarshal([]byte(raw), &draws); err != nil {
				return err
			}
			for _, draw := range draws {
				if err := q.RefundPublishingCreditGrant(ctx, db.RefundPublishingCreditGrantParams{WorkspaceID: workspaceID, ID: draw.ID, Units: draw.Units}); err != nil {
					return err
				}
			}
			if err := q.RefundPublishingAllowanceCharge(ctx, db.RefundPublishingAllowanceChargeParams{WorkspaceID: workspaceID, Ref: ref}); err != nil {
				return err
			}
		}
		return q.DeletePublishingAllowanceReservation(ctx, db.DeletePublishingAllowanceReservationParams{WorkspaceID: workspaceID, Ref: ref})
	})
}
