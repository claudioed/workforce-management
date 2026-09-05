package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// CommitShiftPlan validates and commits a human-decided headcount split
// across paths for one building's shift. installedStations is supplied by
// the caller (Work Planning owns that number; this context has no
// dependency on that service, so the request carries it) and validated
// independently against plannedHeads.
//
// InstalledCapacity additionally fetches a LIVE ceiling from
// fulfillment-execution's real Station registry for every line, before
// ever calling into the domain aggregate. Per this fleet's own rule
// ("fail loud for anything that mutates real state"), an
// ErrInstalledCapacityUnavailable on ANY line fails the WHOLE commit --
// unlike ProposePathPlan's MeasuredRateClient, there is no fallback path
// here. See ADR-0014.
type CommitShiftPlan struct {
	ShiftPlans        ports.ShiftPlanRepo
	Events            ports.EventPublisher
	Clock             ports.Clock
	InstalledCapacity ports.InstalledCapacityClient
	MaxHoursPerShift  float64
}

// Execute fetches a live installed-capacity ceiling for every line from
// fulfillment-execution, then commits the plan. Any
// ErrInstalledCapacityUnavailable fails the entire commit -- see this
// struct's own doc comment for why this differs from ProposePathPlan's
// fail-open MeasuredRateClient policy.
func (uc *CommitShiftPlan) Execute(ctx context.Context, buildingId, shiftId string, lines []shiftplan.PathPlan, installedStations map[shared.PathId]int) (*shiftplan.ShiftPlan, error) {
	installedCapacity := make(map[shared.PathId]int, len(lines))
	for _, line := range lines {
		if _, ok := installedCapacity[line.PathId]; ok {
			continue // a path can appear on multiple lines in a malformed request; fetch each path once
		}
		capacity, err := uc.InstalledCapacity.InstalledCapacity(ctx, line.PathId)
		if err != nil {
			return nil, err
		}
		installedCapacity[line.PathId] = capacity
	}

	sp, err := shiftplan.CommitShiftPlan(buildingId, shiftId, lines, installedStations, installedCapacity, uc.MaxHoursPerShift, uc.Clock.Now())
	if err != nil {
		return nil, err
	}
	if err := uc.ShiftPlans.Save(ctx, sp); err != nil {
		return nil, err
	}
	if err := uc.Events.Publish(ctx, sp.PullEvents()...); err != nil {
		return nil, err
	}
	return sp, nil
}
