package analyticsstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/workforce-management/internal/analytics/report"
)

// PostgresProjection is the WRITER implementation of report.ProjectionStore,
// backed by a pgxpool over the analytical database. Every Apply* runs in a
// transaction that first claims the event id in analytics_processed_events
// (ON CONFLICT DO NOTHING); it only mutates the rollup when the claim is new,
// making each apply idempotent per eventId under Kafka's at-least-once
// delivery. It is the only writer of the analytical database.
type PostgresProjection struct {
	pool *pgxpool.Pool
}

// NewPostgresProjection constructs a PostgresProjection over pool.
func NewPostgresProjection(pool *pgxpool.Pool) *PostgresProjection {
	return &PostgresProjection{pool: pool}
}

// claim inserts eventId into analytics_processed_events, returning true iff
// this call newly recorded it (so the caller should apply the effect). It runs
// inside tx so the claim and the effect commit atomically.
func claim(ctx context.Context, tx pgx.Tx, eventId string, occurredAt time.Time) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO analytics_processed_events (event_id, occurred_at)
		 VALUES ($1, $2) ON CONFLICT (event_id) DO NOTHING`,
		eventId, occurredAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// inTx runs fn in a transaction, committing on success and rolling back on
// error.
func (p *PostgresProjection) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// applyRollup is the shared skeleton for the counter-only events: claim the
// event id, and on a new claim add delta into the (pathId, hour) rollup row.
func (p *PostgresProjection) applyRollup(ctx context.Context, eventId, pathId string, at time.Time, delta rollupDelta) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		isNew, err := claim(ctx, tx, eventId, at)
		if err != nil {
			return fmt.Errorf("analyticsstore: claim event: %w", err)
		}
		if !isNew {
			return nil
		}
		return upsertRollup(ctx, tx, pathId, at, delta)
	})
}

// ApplyShiftStarted records an AssociateShiftStarted. Idempotent on eventId.
func (p *PostgresProjection) ApplyShiftStarted(ctx context.Context, eventId, _ string, at time.Time) error {
	return p.applyRollup(ctx, eventId, "", at, rollupDelta{shiftsStarted: 1})
}

// ApplyShiftEnded records an AssociateShiftEnded. Idempotent on eventId.
func (p *PostgresProjection) ApplyShiftEnded(ctx context.Context, eventId, _ string, at time.Time) error {
	return p.applyRollup(ctx, eventId, "", at, rollupDelta{shiftsEnded: 1})
}

// ApplyCertified records an AssociateCertified. Idempotent on eventId.
func (p *PostgresProjection) ApplyCertified(ctx context.Context, eventId, _ string, at time.Time) error {
	return p.applyRollup(ctx, eventId, "", at, rollupDelta{certifications: 1})
}

// ApplyLaborAssigned records a LaborAssigned to pathId. Idempotent on eventId.
func (p *PostgresProjection) ApplyLaborAssigned(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyRollup(ctx, eventId, pathId, at, rollupDelta{laborAssigned: 1})
}

// ApplyLaborReassigned records a LaborReassigned into toPathId. Idempotent on
// eventId.
func (p *PostgresProjection) ApplyLaborReassigned(ctx context.Context, eventId, toPathId string, at time.Time) error {
	return p.applyRollup(ctx, eventId, toPathId, at, rollupDelta{laborReassigned: 1})
}

// ApplyPathUnderstaffed records a PathUnderstaffed for pathId. Idempotent on
// eventId.
func (p *PostgresProjection) ApplyPathUnderstaffed(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyRollup(ctx, eventId, pathId, at, rollupDelta{understaffingEvents: 1})
}

// ApplyBreakStarted increments the break count and records the break start so
// a later end can derive break duration. Idempotent on eventId.
func (p *PostgresProjection) ApplyBreakStarted(ctx context.Context, eventId, associateId string, at time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		isNew, err := claim(ctx, tx, eventId, at)
		if err != nil {
			return fmt.Errorf("analyticsstore: claim event: %w", err)
		}
		if !isNew {
			return nil
		}
		if err := upsertRollup(ctx, tx, "", at, rollupDelta{breaks: 1}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO analytics_pending_breaks (associate_id, started_at)
			 VALUES ($1, $2)
			 ON CONFLICT (associate_id) DO UPDATE SET started_at = EXCLUDED.started_at`,
			associateId, at)
		if err != nil {
			return fmt.Errorf("analyticsstore: record break start: %w", err)
		}
		return nil
	})
}

// ApplyBreakEnded folds the paired start-to-end break duration into the bucket
// of the break's start, when the start is known. Idempotent on eventId.
func (p *PostgresProjection) ApplyBreakEnded(ctx context.Context, eventId, associateId string, at time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		isNew, err := claim(ctx, tx, eventId, at)
		if err != nil {
			return fmt.Errorf("analyticsstore: claim event: %w", err)
		}
		if !isNew {
			return nil
		}

		var startedAt time.Time
		row := tx.QueryRow(ctx,
			`SELECT started_at FROM analytics_pending_breaks WHERE associate_id = $1`,
			associateId)
		switch err := row.Scan(&startedAt); {
		case err == nil:
		case errors.Is(err, pgx.ErrNoRows):
			// No paired start (e.g. started before this projector's history):
			// count the end without a duration contribution.
			return nil
		default:
			return fmt.Errorf("analyticsstore: lookup break start: %w", err)
		}

		if err := upsertRollup(ctx, tx, "", startedAt, rollupDelta{
			breakSeconds:       at.Sub(startedAt).Seconds(),
			breaksWithDuration: 1,
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM analytics_pending_breaks WHERE associate_id = $1`,
			associateId); err != nil {
			return fmt.Errorf("analyticsstore: clear break start: %w", err)
		}
		return nil
	})
}

// rollupDelta is the set of counter increments a single event contributes to a
// labor row.
type rollupDelta struct {
	shiftsStarted       int
	shiftsEnded         int
	breaks              int
	certifications      int
	laborAssigned       int
	laborReassigned     int
	understaffingEvents int
	breakSeconds        float64
	breaksWithDuration  int
}

// upsertRollup adds delta into the (path_id, hour_bucket) row, inserting it if
// absent. hour_bucket is derived by truncating at to the UTC hour.
func upsertRollup(ctx context.Context, tx pgx.Tx, pathId string, at time.Time, delta rollupDelta) error {
	bucket := at.UTC().Truncate(time.Hour)
	_, err := tx.Exec(ctx,
		`INSERT INTO labor_rollup (
			path_id, hour_bucket,
			shifts_started, shifts_ended, breaks, certifications,
			labor_assigned, labor_reassigned, understaffing_events,
			break_seconds, breaks_with_duration)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (path_id, hour_bucket) DO UPDATE SET
			shifts_started       = labor_rollup.shifts_started + EXCLUDED.shifts_started,
			shifts_ended         = labor_rollup.shifts_ended + EXCLUDED.shifts_ended,
			breaks               = labor_rollup.breaks + EXCLUDED.breaks,
			certifications       = labor_rollup.certifications + EXCLUDED.certifications,
			labor_assigned       = labor_rollup.labor_assigned + EXCLUDED.labor_assigned,
			labor_reassigned     = labor_rollup.labor_reassigned + EXCLUDED.labor_reassigned,
			understaffing_events = labor_rollup.understaffing_events + EXCLUDED.understaffing_events,
			break_seconds        = labor_rollup.break_seconds + EXCLUDED.break_seconds,
			breaks_with_duration = labor_rollup.breaks_with_duration + EXCLUDED.breaks_with_duration`,
		pathId, bucket,
		delta.shiftsStarted, delta.shiftsEnded, delta.breaks, delta.certifications,
		delta.laborAssigned, delta.laborReassigned, delta.understaffingEvents,
		delta.breakSeconds, delta.breaksWithDuration)
	if err != nil {
		return fmt.Errorf("analyticsstore: upsert rollup: %w", err)
	}
	return nil
}

// Compile-time assertion that PostgresProjection satisfies the write port.
var _ report.ProjectionStore = (*PostgresProjection)(nil)
