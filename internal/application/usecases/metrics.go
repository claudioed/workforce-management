package usecases

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/assignment"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// meterName is this layer's OpenTelemetry instrumentation scope.
const meterName = "github.com/claudioed/workforce-management/internal/application/usecases"

// Attribute keys and values for the labor-assignment counter. Every value is
// drawn from a closed set so the metric stays low-cardinality.
const (
	attrOutcome = attribute.Key("workforce.assignment.outcome")
	attrReason  = attribute.Key("workforce.assignment.reason")
	attrPath    = attribute.Key("workforce.path.id")

	outcomeAccepted = "accepted"
	outcomeRejected = "rejected"
)

// laborAssignments counts every AssignLabor attempt, accepted or rejected.
// It is created against the global MeterProvider, which delegates to the
// real provider once telemetry.Setup installs it — so package-init ordering
// versus Setup does not matter.
var laborAssignments = newLaborAssignmentsCounter()

func newLaborAssignmentsCounter() metric.Int64Counter {
	counter, err := otel.Meter(meterName).Int64Counter(
		"workforce.labor_assignments",
		metric.WithDescription("Labor assignment attempts, by outcome and path."),
		metric.WithUnit("{assignment}"),
	)
	if err != nil {
		// The API contract guarantees a usable (no-op) instrument alongside
		// the error, so surface the problem and keep going rather than
		// failing a domain operation over a metric.
		otel.Handle(err)
	}
	return counter
}

// recordLaborAssignment records one AssignLabor attempt. A nil err is an
// accepted assignment; anything else is a rejection tagged with a
// closed-set reason so the "why are we failing to staff this path" question
// is answerable from the metric alone.
func recordLaborAssignment(ctx context.Context, pathId shared.PathId, err error) {
	if laborAssignments == nil {
		return
	}
	attrs := []attribute.KeyValue{attrPath.String(string(pathId))}
	if err == nil {
		attrs = append(attrs, attrOutcome.String(outcomeAccepted))
	} else {
		attrs = append(attrs,
			attrOutcome.String(outcomeRejected),
			attrReason.String(rejectionReason(err)),
		)
	}
	laborAssignments.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// rejectionReason maps a rejection to a stable low-cardinality slug.
//
// Note there is no "double_booked" reason: this context resolves a second
// active assignment by ending the prior one and raising LaborReassigned (see
// LaborAssignment.Assign), so double-booking is prevented by construction
// rather than rejected. The gates that genuinely reject are certification,
// break state, shift state and the max-hours limit.
func rejectionReason(err error) string {
	switch {
	case errors.Is(err, assignment.ErrCertificationRequired):
		return "uncertified"
	case errors.Is(err, associate.ErrOnBreak):
		return "on_break"
	case errors.Is(err, associate.ErrShiftEnded):
		return "shift_ended"
	case errors.Is(err, associate.ErrMaxHoursExceeded):
		return "max_hours_exceeded"
	case errors.Is(err, ports.ErrNotFound):
		return "associate_not_found"
	default:
		return "internal_error"
	}
}
