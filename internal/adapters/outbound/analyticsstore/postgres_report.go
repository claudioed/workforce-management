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

// PostgresReport is the READER implementation of report.ReportStore, backed by
// a pgxpool over the analytical database. The pool it is given is expected to
// be pinned to a read-only role / default_transaction_read_only=on, so a bug
// in the reader cannot mutate the read model (ADR-0010). The reader never
// issues writes.
type PostgresReport struct {
	pool *pgxpool.Pool
}

// NewPostgresReport constructs a PostgresReport over pool.
func NewPostgresReport(pool *pgxpool.Pool) *PostgresReport {
	return &PostgresReport{pool: pool}
}

// Query returns the labor rows matching q. The average break duration is
// derived in SQL from the running sum and the count of breaks that had a paired
// end. From is inclusive, To is exclusive; empty PathId disables that filter.
func (r *PostgresReport) Query(ctx context.Context, q report.ReportQuery) (report.LaborReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT path_id, hour_bucket,
			shifts_started, shifts_ended, breaks, certifications,
			labor_assigned, labor_reassigned, understaffing_events,
			CASE WHEN breaks_with_duration > 0
			     THEN break_seconds / breaks_with_duration
			     ELSE 0 END AS avg_break_seconds
		 FROM labor_rollup
		 WHERE hour_bucket >= $1 AND hour_bucket < $2
		   AND ($3 = '' OR path_id = $3)
		 ORDER BY hour_bucket, path_id`,
		q.From, q.To, q.PathId)
	if err != nil {
		return report.LaborReport{}, fmt.Errorf("analyticsstore: query rollup: %w", err)
	}
	defer rows.Close()

	var out report.LaborReport
	for rows.Next() {
		var (
			row    report.Row
			bucket time.Time
		)
		if err := rows.Scan(
			&row.Key.PathId, &bucket,
			&row.ShiftsStarted, &row.ShiftsEnded, &row.Breaks, &row.Certifications,
			&row.LaborAssigned, &row.LaborReassigned, &row.UnderstaffingEvents,
			&row.AvgBreakSeconds,
		); err != nil {
			return report.LaborReport{}, fmt.Errorf("analyticsstore: scan row: %w", err)
		}
		row.Key.HourBucket = bucket.UTC()
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return report.LaborReport{}, fmt.Errorf("analyticsstore: iterate rows: %w", err)
	}
	return out, nil
}

// FreshnessLag returns now minus the most recent event's occurred_at, i.e. how
// far the read model trails real time. Zero when the read model is empty or
// (defensively) when the latest event is future-dated.
func (r *PostgresReport) FreshnessLag(ctx context.Context) (time.Duration, error) {
	// max() over an empty table returns a single NULL row (not zero rows), so
	// scan into a nullable *time.Time and treat NULL as "read model empty".
	var latest *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT max(occurred_at) FROM analytics_processed_events`).Scan(&latest)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("analyticsstore: freshness query: %w", err)
	}
	if latest == nil || latest.IsZero() {
		return 0, nil
	}
	lag := time.Since(*latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}

// Compile-time assertion that PostgresReport satisfies the read port.
var _ report.ReportStore = (*PostgresReport)(nil)
