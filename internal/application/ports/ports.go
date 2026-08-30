// Package ports declares the outbound interfaces the application layer
// depends on: repositories for each aggregate, an event publisher, and a
// clock. Adapters implement these; the application layer never imports an
// adapter package.
package ports

import (
	"context"
	"time"

	"github.com/claudioed/workforce-management/internal/domain/assignment"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// AssociateRepo persists and loads AssociateShift aggregates.
type AssociateRepo interface {
	Save(ctx context.Context, a *associate.AssociateShift) error
	FindByID(ctx context.Context, id shared.AssociateId) (*associate.AssociateShift, error)
}

// ShiftPlanRepo persists and loads ShiftPlan aggregates. A plan is keyed by
// buildingId + shiftId.
type ShiftPlanRepo interface {
	Save(ctx context.Context, sp *shiftplan.ShiftPlan) error
	FindByBuildingAndShift(ctx context.Context, buildingId, shiftId string) (*shiftplan.ShiftPlan, error)
}

// AssignmentRepo persists and loads LaborAssignment aggregates, one per
// associate, and answers staffing-gap queries.
type AssignmentRepo interface {
	Save(ctx context.Context, la *assignment.LaborAssignment) error
	FindByAssociateID(ctx context.Context, id shared.AssociateId) (*assignment.LaborAssignment, error)
	CountActiveByPath(ctx context.Context, pathId shared.PathId) (int, error)
}

// MeasuredRateClient queries labor-performance for a real, measured mean
// task duration for a path, so ProposePathPlan can propose headcount
// against reality rather than requiring a caller-supplied plannedRate
// every time. See ADR-000X in this repo's docs for the full "why a new
// outbound Supplier, why fail-open" rationale.
type MeasuredRateClient interface {
	// MeanActualSeconds returns the real measured mean duration (seconds)
	// labor-performance has recorded for pathId, or
	// ErrMeasuredRateUnavailable on ANY failure to produce one --
	// unreachable service, malformed response, or genuinely no data yet.
	// ProposePathPlan treats every ErrMeasuredRateUnavailable case
	// identically: fall back to the caller-supplied plannedRate. It never
	// needs (and this interface deliberately never exposes) a way to tell
	// those failure modes apart.
	MeanActualSeconds(ctx context.Context, pathId shared.PathId) (float64, error)
}

// ErrMeasuredRateUnavailable is returned by a MeasuredRateClient
// implementation for every failure mode a caller cannot usefully act on
// differently: an unreachable labor-performance, a malformed response, or
// a real 200 response reporting no measurable data yet for this path.
// ProposePathPlan's single error-handling branch depends on this being
// the ONLY error a MeasuredRateClient ever returns.

// EventPublisher publishes domain events raised by aggregates.
type EventPublisher interface {
	Publish(ctx context.Context, events ...shared.DomainEvent) error
}

// ProcessedEvents is the idempotency gate for at-least-once event consumption:
// it records which event ids have already been handled so a redelivery is a
// no-op. It is used by the analytics projector's inbound consumer; the OLTP
// write path does not depend on it.
type ProcessedEvents interface {
	// MarkProcessed records eventId if absent, returning true iff this call
	// newly recorded it (so the caller should process the event) and false if
	// it was already seen (a duplicate to skip).
	MarkProcessed(ctx context.Context, eventId string) (bool, error)
}

// Clock supplies the current time so domain logic never calls time.Now()
// directly.
type Clock interface {
	Now() time.Time
}
