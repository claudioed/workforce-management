package usecases

import (
	"context"
	"errors"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// RateSourceCaller and RateSourceMeasured identify where the rate used by
// ProposePathPlan.Execute came from -- a caller-supplied plannedRate, or a
// measured rate fed back from labor-performance (feature: close-the-loop
// measured rate, ADR-0012). Surfaced on the response so a human is never
// left guessing why heads came back 0 or where the number came from.
const (
	RateSourceCaller   = "caller"
	RateSourceMeasured = "measured"
)

// ProposePathPlan is a pure computation: heads needed to cover a path's
// charge at a planned rate. It persists nothing — a human must still
// commit the plan via CommitShiftPlan.
//
// plannedRate is now OPTIONAL: a caller passing <= 0 (not supplied) gets a
// real measured rate fed back from labor-performance via MeasuredRate
// (feature: close-the-loop measured rate, ADR-0012) when one is available
// for this path's task type. This is a SOFT enrichment, not a mutation of
// real state: any failure to obtain a measured rate — labor-performance
// unreachable, no TaskType mapping for this path, or genuinely no data
// yet — falls through to the existing zero-rate behavior (0 proposed
// heads) rather than failing the request. A caller-supplied plannedRate
// always wins outright; MeasuredRate is never consulted when one is given.
type ProposePathPlan struct {
	Events       ports.EventPublisher
	Clock        ports.Clock
	MeasuredRate ports.MeasuredRateClient
}

// Execute computes proposed heads and publishes ShiftPlanProposed. It
// returns the heads, the resolved rate actually used, and which source
// that rate came from (RateSourceCaller or RateSourceMeasured).
func (uc *ProposePathPlan) Execute(ctx context.Context, buildingId string, pathId shared.PathId, charge, plannedRate float64) (heads int, resolvedRate float64, rateSource string, err error) {
	resolvedRate = plannedRate
	rateSource = RateSourceCaller

	if resolvedRate <= 0 && uc.MeasuredRate != nil {
		measured, mErr := uc.MeasuredRate.MeanActualSeconds(ctx, pathId)
		if mErr == nil {
			resolvedRate = measured
			rateSource = RateSourceMeasured
		} else if !errors.Is(mErr, ports.ErrMeasuredRateUnavailable) {
			// A MeasuredRateClient must only ever return
			// ErrMeasuredRateUnavailable; anything else is a
			// programming error in the adapter, not a business
			// condition to swallow.
			return 0, 0, "", mErr
		}
		// ErrMeasuredRateUnavailable: fall through with resolvedRate
		// unchanged (still <= 0, still RateSourceCaller) -- the
		// existing zero-rate behavior (0 heads) below.
	}

	heads = shiftplan.ProposedHeads(charge, resolvedRate)
	event := shared.NewShiftPlanProposed(uc.Clock.Now(), buildingId, pathId, heads, resolvedRate)
	if err := uc.Events.Publish(ctx, event); err != nil {
		return 0, 0, "", err
	}
	return heads, resolvedRate, rateSource, nil
}
