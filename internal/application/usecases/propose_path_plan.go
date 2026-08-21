package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// ProposePathPlan is a pure computation: heads needed to cover a path's
// charge at the given planned rate. It persists nothing — a human must
// still commit the plan via CommitShiftPlan.
type ProposePathPlan struct {
	Events ports.EventPublisher
	Clock  ports.Clock
}

// Execute computes proposed heads and publishes ShiftPlanProposed.
func (uc *ProposePathPlan) Execute(ctx context.Context, buildingId string, pathId shared.PathId, charge, plannedRate float64) (int, error) {
	heads := shiftplan.ProposedHeads(charge, plannedRate)
	event := shared.NewShiftPlanProposed(uc.Clock.Now(), buildingId, pathId, heads, plannedRate)
	if err := uc.Events.Publish(ctx, event); err != nil {
		return 0, err
	}
	return heads, nil
}
