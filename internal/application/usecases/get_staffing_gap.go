package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// StaffingGap is the read model: plannedHeads vs active assignments for a
// path. It surfaces the gap; it never decides or moves anyone.
type StaffingGap struct {
	PathId       shared.PathId
	PlannedHeads int
	ActiveHeads  int
	Understaffed bool
}

// GetStaffingGap computes the staffing gap for a path within a building's
// committed shift plan, raising PathUnderstaffed when active assignments
// fall short of plannedHeads.
type GetStaffingGap struct {
	ShiftPlans  ports.ShiftPlanRepo
	Assignments ports.AssignmentRepo
	Events      ports.EventPublisher
	Clock       ports.Clock
}

// Execute computes the gap for pathId within buildingId's shiftId plan.
func (uc *GetStaffingGap) Execute(ctx context.Context, buildingId, shiftId string, pathId shared.PathId) (StaffingGap, error) {
	sp, err := uc.ShiftPlans.FindByBuildingAndShift(ctx, buildingId, shiftId)
	if err != nil {
		return StaffingGap{}, err
	}
	plannedHeads := sp.PlannedHeadsFor(pathId)

	activeHeads, err := uc.Assignments.CountActiveByPath(ctx, pathId)
	if err != nil {
		return StaffingGap{}, err
	}

	gap := StaffingGap{
		PathId:       pathId,
		PlannedHeads: plannedHeads,
		ActiveHeads:  activeHeads,
		Understaffed: activeHeads < plannedHeads,
	}

	if gap.Understaffed {
		event := shared.NewPathUnderstaffed(uc.Clock.Now(), pathId, plannedHeads, activeHeads)
		if err := uc.Events.Publish(ctx, event); err != nil {
			return StaffingGap{}, err
		}
	}

	return gap, nil
}
