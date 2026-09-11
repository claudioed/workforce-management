package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// StaffingGap is the read model: plannedHeads vs active assignments for a
// path. It surfaces the gap; it never decides or moves anyone.
// ObservedIdlePct is a pure surfacing addition (idleness-as-staffing-signal):
// nil whenever no idle-share signal is available for this path's task type
// -- IdleShare is unwired (mode is permissive/http, i.e. the cache isn't
// the active MeasuredRateClient implementation) or the cache has no idle
// observation yet for this TaskType. NEVER coerced to 0; a nil here means
// "no signal", not "zero idle".
type StaffingGap struct {
	PathId          shared.PathId
	PlannedHeads    int
	ActiveHeads     int
	Understaffed    bool
	ObservedIdlePct *float64
}

// GetStaffingGap computes the staffing gap for a path within a building's
// committed shift plan, raising PathUnderstaffed when active assignments
// fall short of plannedHeads.
type GetStaffingGap struct {
	ShiftPlans  ports.ShiftPlanRepo
	Assignments ports.AssignmentRepo
	Events      ports.EventPublisher
	Clock       ports.Clock
	// IdleShare surfaces the observed idle share for pathId's task type
	// on the response, as ObservedIdlePct. Pure surfacing -- no behavior
	// change to the gap computation below. nil (the zero value) is a
	// valid, fully supported configuration: every composition root that
	// does not wire LABOR_PERFORMANCE_MODE=kafka-cache leaves this nil,
	// and ObservedIdlePct is then always nil on the response, matching
	// every other *_MODE permissive-by-default pattern in this fleet.
	IdleShare ports.IdleShareClient
	// UnitOfWork brackets the Publish (ADR 0016). This use case saves
	// nothing, so the scope is trivial, but wrapping it keeps every
	// publishing use case uniform: the outbox row commits on its own.
	UnitOfWork ports.UnitOfWork
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
		PathId:          pathId,
		PlannedHeads:    plannedHeads,
		ActiveHeads:     activeHeads,
		Understaffed:    activeHeads < plannedHeads,
		ObservedIdlePct: uc.observedIdlePct(ctx, pathId),
	}

	if gap.Understaffed {
		event := shared.NewPathUnderstaffed(uc.Clock.Now(), pathId, plannedHeads, activeHeads)
		err := atomically(ctx, uc.UnitOfWork, func(ctx context.Context) error {
			return uc.Events.Publish(ctx, event)
		})
		if err != nil {
			return StaffingGap{}, err
		}
	}

	return gap, nil
}

// observedIdlePct returns the idle share for pathId's task type, or nil
// when IdleShare is unwired or reports ErrIdleShareUnavailable -- pure
// surfacing, this never fails Execute.
func (uc *GetStaffingGap) observedIdlePct(ctx context.Context, pathId shared.PathId) *float64 {
	if uc.IdleShare == nil {
		return nil
	}
	share, err := uc.IdleShare.IdleSharePct(ctx, pathId)
	if err != nil {
		return nil
	}
	return &share
}
