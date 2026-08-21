package http

import (
	"errors"
	"net/http"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/assignment"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// statusFor maps a typed domain/application error to an HTTP status code.
func statusFor(err error) int {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, shared.ErrEmptyAssociateId),
		errors.Is(err, shared.ErrEmptyPathId),
		errors.Is(err, shared.ErrEmptyCertification),
		errors.Is(err, shiftplan.ErrNoPathPlans),
		errors.Is(err, shiftplan.ErrMissingInstalledStations):
		return http.StatusBadRequest
	case errors.Is(err, associate.ErrAlreadyOnBreak),
		errors.Is(err, associate.ErrNotOnBreak),
		errors.Is(err, associate.ErrOnBreak),
		errors.Is(err, associate.ErrShiftEnded),
		errors.Is(err, associate.ErrMaxHoursExceeded),
		errors.Is(err, assignment.ErrCertificationRequired),
		errors.Is(err, shiftplan.ErrPlannedHeadsExceedInstalled),
		errors.Is(err, shiftplan.ErrPlannedHoursExceedCapacity):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
