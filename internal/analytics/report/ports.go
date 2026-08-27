package report

import (
	"context"
	"time"
)

// ReportStore is the read side of the Labor Utilization & Staffing data
// product: the reader process queries it to serve reports. It is read-only by
// contract — the Postgres implementation runs over a pool pinned to a
// read-only role.
type ReportStore interface {
	// Query returns the labor rows matching q.
	Query(ctx context.Context, q ReportQuery) (LaborReport, error)
	// FreshnessLag reports how far the read model lags real time: the age of
	// the most recently applied event. A larger lag means the projection is
	// further behind the event stream.
	FreshnessLag(ctx context.Context) (time.Duration, error)
}

// ProjectionStore is the write side of the Labor Utilization & Staffing data
// product: the projector process applies each consumed event to it. Every
// Apply* method is idempotent on eventId — applying the same eventId twice
// records the effect once, so the at-least-once Kafka stream can be projected
// exactly once.
//
// The methods take the derivation-relevant fields already extracted from the
// analytics envelope (rather than a domain event) so this port stays free of
// any OLTP domain dependency. Associate-scoped events pass an empty pathId;
// path-scoped events carry the path.
type ProjectionStore interface {
	// ApplyShiftStarted records an AssociateShiftStarted at `at`.
	ApplyShiftStarted(ctx context.Context, eventId, associateId string, at time.Time) error
	// ApplyShiftEnded records an AssociateShiftEnded at `at`.
	ApplyShiftEnded(ctx context.Context, eventId, associateId string, at time.Time) error
	// ApplyBreakStarted records the break start of associateId at `at`, so a
	// later break end can compute break duration, and increments the break
	// count.
	ApplyBreakStarted(ctx context.Context, eventId, associateId string, at time.Time) error
	// ApplyBreakEnded records a break end for associateId at `at`, folding the
	// paired start-to-end duration into the bucket when the start is known.
	ApplyBreakEnded(ctx context.Context, eventId, associateId string, at time.Time) error
	// ApplyCertified records an AssociateCertified at `at`.
	ApplyCertified(ctx context.Context, eventId, associateId string, at time.Time) error
	// ApplyLaborAssigned records a LaborAssigned to pathId at `at`.
	ApplyLaborAssigned(ctx context.Context, eventId, pathId string, at time.Time) error
	// ApplyLaborReassigned records a LaborReassigned into toPathId at `at`.
	ApplyLaborReassigned(ctx context.Context, eventId, toPathId string, at time.Time) error
	// ApplyPathUnderstaffed records a PathUnderstaffed for pathId at `at`.
	ApplyPathUnderstaffed(ctx context.Context, eventId, pathId string, at time.Time) error
}
