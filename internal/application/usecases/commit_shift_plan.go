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
type CommitShiftPlan struct {
	ShiftPlans       ports.ShiftPlanRepo
	Events           ports.EventPublisher
	Clock            ports.Clock
	MaxHoursPerShift float64
}

// Execute commits the plan.
func (uc *CommitShiftPlan) Execute(ctx context.Context, buildingId, shiftId string, lines []shiftplan.PathPlan, installedStations map[shared.PathId]int) (*shiftplan.ShiftPlan, error) {
	sp, err := shiftplan.CommitShiftPlan(buildingId, shiftId, lines, installedStations, uc.MaxHoursPerShift, uc.Clock.Now())
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
