package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// ShiftPlanRepo is a pgxpool-backed ports.ShiftPlanRepo.
type ShiftPlanRepo struct {
	pool *pgxpool.Pool
}

// NewShiftPlanRepo constructs a ShiftPlanRepo backed by pool.
func NewShiftPlanRepo(pool *pgxpool.Pool) *ShiftPlanRepo {
	return &ShiftPlanRepo{pool: pool}
}

// Save upserts sp and its PathPlan lines.
func (r *ShiftPlanRepo) Save(ctx context.Context, sp *shiftplan.ShiftPlan) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO shift_plan (building_id, shift_id) VALUES ($1, $2)
		ON CONFLICT (building_id, shift_id) DO NOTHING
	`, sp.BuildingId(), sp.ShiftId()); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM path_plan WHERE building_id = $1 AND shift_id = $2
	`, sp.BuildingId(), sp.ShiftId()); err != nil {
		return err
	}

	for _, line := range sp.Lines() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO path_plan (building_id, shift_id, path_id, planned_heads, planned_rate, planned_hours)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, sp.BuildingId(), sp.ShiftId(), string(line.PathId), line.PlannedHeads, line.PlannedRate, line.PlannedHours); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// FindByBuildingAndShift loads the ShiftPlan for buildingId+shiftId, or
// ports.ErrNotFound.
func (r *ShiftPlanRepo) FindByBuildingAndShift(ctx context.Context, buildingId, shiftId string) (*shiftplan.ShiftPlan, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM shift_plan WHERE building_id = $1 AND shift_id = $2)
	`, buildingId, shiftId).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ports.ErrNotFound
	}

	rows, err := r.pool.Query(ctx, `
		SELECT path_id, planned_heads, planned_rate, planned_hours
		FROM path_plan WHERE building_id = $1 AND shift_id = $2
	`, buildingId, shiftId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []shiftplan.PathPlan
	for rows.Next() {
		var pathId string
		var line shiftplan.PathPlan
		if err := rows.Scan(&pathId, &line.PlannedHeads, &line.PlannedRate, &line.PlannedHours); err != nil {
			return nil, err
		}
		line.PathId = shared.PathId(pathId)
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return shiftplan.Rehydrate(buildingId, shiftId, lines), nil
}
