package usecases

import (
	"context"
	"errors"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// EndAssociateShift closes an associate's shift, ending any active
// assignment first.
type EndAssociateShift struct {
	Associates       ports.AssociateRepo
	Assignments      ports.AssignmentRepo
	Events           ports.EventPublisher
	Clock            ports.Clock
	MaxHoursPerShift float64
}

// Execute ends associateId's shift.
func (uc *EndAssociateShift) Execute(ctx context.Context, associateId shared.AssociateId) error {
	shift, err := uc.Associates.FindByID(ctx, associateId)
	if err != nil {
		return err
	}

	now := uc.Clock.Now()

	la, err := uc.Assignments.FindByAssociateID(ctx, associateId)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return err
	}
	if la != nil {
		if closed, ok := la.EndActive(now); ok {
			if err := shift.LogHours(closed.Hours(now), uc.MaxHoursPerShift); err != nil {
				return err
			}
			if err := uc.Assignments.Save(ctx, la); err != nil {
				return err
			}
		}
	}

	shift.EndShift(now)
	if err := uc.Associates.Save(ctx, shift); err != nil {
		return err
	}
	return uc.Events.Publish(ctx, shift.PullEvents()...)
}
