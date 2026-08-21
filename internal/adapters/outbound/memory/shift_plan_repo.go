package memory

import (
	"context"
	"sync"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

type shiftPlanKey struct {
	buildingId string
	shiftId    string
}

// ShiftPlanRepo is a thread-safe in-memory ports.ShiftPlanRepo.
type ShiftPlanRepo struct {
	mu   sync.RWMutex
	byID map[shiftPlanKey]*shiftplan.ShiftPlan
}

// NewShiftPlanRepo constructs an empty ShiftPlanRepo.
func NewShiftPlanRepo() *ShiftPlanRepo {
	return &ShiftPlanRepo{byID: make(map[shiftPlanKey]*shiftplan.ShiftPlan)}
}

// Save stores a snapshot of sp.
func (r *ShiftPlanRepo) Save(ctx context.Context, sp *shiftplan.ShiftPlan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := shiftPlanKey{sp.BuildingId(), sp.ShiftId()}
	r.byID[key] = shiftplan.Rehydrate(sp.BuildingId(), sp.ShiftId(), sp.Lines())
	return nil
}

// FindByBuildingAndShift loads the ShiftPlan for buildingId+shiftId, or
// ports.ErrNotFound.
func (r *ShiftPlanRepo) FindByBuildingAndShift(ctx context.Context, buildingId, shiftId string) (*shiftplan.ShiftPlan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sp, ok := r.byID[shiftPlanKey{buildingId, shiftId}]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return shiftplan.Rehydrate(sp.BuildingId(), sp.ShiftId(), sp.Lines()), nil
}
