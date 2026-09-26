package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
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
// fall short of plannedHeads. Execute answers the single-path lookup;
// ExecuteAll answers the fleet-wide "every path in this building+shift"
// list, reusing the exact same per-path computation core so the two can
// never drift (ADR-0011's deferred fast-follow: a list-by-building/shift
// endpoint).
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

	gap, event, err := uc.gapForLine(ctx, sp, pathId)
	if err != nil {
		return StaffingGap{}, err
	}

	if event != nil {
		if err := uc.publish(ctx, event); err != nil {
			return StaffingGap{}, err
		}
	}
	return gap, nil
}

// ExecuteAll computes the staffing gap for EVERY path planned within
// buildingId's shiftId committed plan -- the fleet-wide list ADR-0011
// flagged as a deferred fast-follow ("a fleet-wide 'all paths, one
// building/shift' list endpoint"). It fetches the ShiftPlan exactly once
// and reuses gapForLine per line, so the per-path computation can never
// drift from Execute's single-path answer. Every PathUnderstaffed event
// raised across the whole plan is published together, in one atomic
// scope, rather than one outbox write per line.
func (uc *GetStaffingGap) ExecuteAll(ctx context.Context, buildingId, shiftId string) ([]StaffingGap, error) {
	sp, err := uc.ShiftPlans.FindByBuildingAndShift(ctx, buildingId, shiftId)
	if err != nil {
		return nil, err
	}

	lines := sp.Lines()
	gaps := make([]StaffingGap, 0, len(lines))
	var events []shared.DomainEvent
	for _, line := range lines {
		gap, event, err := uc.gapForLine(ctx, sp, line.PathId)
		if err != nil {
			return nil, err
		}
		gaps = append(gaps, gap)
		if event != nil {
			events = append(events, event)
		}
	}

	if len(events) > 0 {
		if err := uc.publish(ctx, events...); err != nil {
			return nil, err
		}
	}
	return gaps, nil
}

// gapForLine is the shared per-path core: plannedHeads (already known from
// sp, no extra repo call), active assignments, and the idle-share
// surfacing. It returns the PathUnderstaffed event to raise (nil when the
// path is adequately staffed) rather than publishing it itself, so both
// Execute and ExecuteAll can batch their own publish call.
func (uc *GetStaffingGap) gapForLine(ctx context.Context, sp *shiftplan.ShiftPlan, pathId shared.PathId) (StaffingGap, shared.DomainEvent, error) {
	plannedHeads := sp.PlannedHeadsFor(pathId)

	activeHeads, err := uc.Assignments.CountActiveByPath(ctx, pathId)
	if err != nil {
		return StaffingGap{}, nil, err
	}

	gap := StaffingGap{
		PathId:          pathId,
		PlannedHeads:    plannedHeads,
		ActiveHeads:     activeHeads,
		Understaffed:    activeHeads < plannedHeads,
		ObservedIdlePct: uc.observedIdlePct(ctx, pathId),
	}

	var event shared.DomainEvent
	if gap.Understaffed {
		event = shared.NewPathUnderstaffed(uc.Clock.Now(), pathId, plannedHeads, activeHeads)
	}
	return gap, event, nil
}

// publish wraps events in the UnitOfWork scope, mirroring the exact
// bracketing Execute always performed for its single event.
func (uc *GetStaffingGap) publish(ctx context.Context, events ...shared.DomainEvent) error {
	return atomically(ctx, uc.UnitOfWork, func(ctx context.Context) error {
		return uc.Events.Publish(ctx, events...)
	})
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
