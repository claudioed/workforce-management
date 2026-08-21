package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/assignment"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// AssignmentRepo is a pgxpool-backed ports.AssignmentRepo.
type AssignmentRepo struct {
	pool *pgxpool.Pool
}

// NewAssignmentRepo constructs an AssignmentRepo backed by pool.
func NewAssignmentRepo(pool *pgxpool.Pool) *AssignmentRepo {
	return &AssignmentRepo{pool: pool}
}

// Save upserts la's active assignment and appends any newly closed history
// entries.
func (r *AssignmentRepo) Save(ctx context.Context, la *assignment.LaborAssignment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var activePathId *string
	var activeStart *time.Time
	if iv, ok := la.ActiveInterval(); ok {
		p := string(iv.PathId)
		activePathId = &p
		start := iv.Start
		activeStart = &start
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO labor_assignment (associate_id, active_path_id, active_start)
		VALUES ($1, $2, $3)
		ON CONFLICT (associate_id) DO UPDATE SET
			active_path_id = EXCLUDED.active_path_id,
			active_start = EXCLUDED.active_start
	`, string(la.AssociateId()), activePathId, activeStart); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM labor_assignment_history WHERE associate_id = $1
	`, string(la.AssociateId())); err != nil {
		return err
	}

	for _, iv := range la.History() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO labor_assignment_history (associate_id, path_id, interval_start, interval_end)
			VALUES ($1, $2, $3, $4)
		`, string(la.AssociateId()), string(iv.PathId), iv.Start, iv.End); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// FindByAssociateID loads the LaborAssignment for id, or ports.ErrNotFound.
func (r *AssignmentRepo) FindByAssociateID(ctx context.Context, id shared.AssociateId) (*assignment.LaborAssignment, error) {
	var activePathId *string
	var activeStart *time.Time

	row := r.pool.QueryRow(ctx, `
		SELECT active_path_id, active_start FROM labor_assignment WHERE associate_id = $1
	`, string(id))
	if err := row.Scan(&activePathId, &activeStart); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}

	var active *assignment.Interval
	if activePathId != nil && activeStart != nil {
		active = &assignment.Interval{PathId: shared.PathId(*activePathId), Start: *activeStart}
	}

	rows, err := r.pool.Query(ctx, `
		SELECT path_id, interval_start, interval_end
		FROM labor_assignment_history WHERE associate_id = $1
		ORDER BY id ASC
	`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []assignment.Interval
	for rows.Next() {
		var pathId string
		var iv assignment.Interval
		var end time.Time
		if err := rows.Scan(&pathId, &iv.Start, &end); err != nil {
			return nil, err
		}
		iv.PathId = shared.PathId(pathId)
		iv.End = &end
		history = append(history, iv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return assignment.Rehydrate(id, active, history), nil
}

// CountActiveByPath counts how many associates currently have an active
// assignment to pathId.
func (r *AssignmentRepo) CountActiveByPath(ctx context.Context, pathId shared.PathId) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM labor_assignment WHERE active_path_id = $1
	`, string(pathId)).Scan(&count)
	return count, err
}
