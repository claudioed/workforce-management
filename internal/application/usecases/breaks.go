package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// StartBreak begins a logged break for an associate.
type StartBreak struct {
	Associates ports.AssociateRepo
	Events     ports.EventPublisher
	Clock      ports.Clock
}

// Execute starts the break.
func (uc *StartBreak) Execute(ctx context.Context, associateId shared.AssociateId) error {
	shift, err := uc.Associates.FindByID(ctx, associateId)
	if err != nil {
		return err
	}
	if err := shift.StartBreak(uc.Clock.Now()); err != nil {
		return err
	}
	if err := uc.Associates.Save(ctx, shift); err != nil {
		return err
	}
	return uc.Events.Publish(ctx, shift.PullEvents()...)
}

// EndBreak ends a logged break for an associate.
type EndBreak struct {
	Associates ports.AssociateRepo
	Events     ports.EventPublisher
	Clock      ports.Clock
}

// Execute ends the break.
func (uc *EndBreak) Execute(ctx context.Context, associateId shared.AssociateId) error {
	shift, err := uc.Associates.FindByID(ctx, associateId)
	if err != nil {
		return err
	}
	if err := shift.EndBreak(uc.Clock.Now()); err != nil {
		return err
	}
	if err := uc.Associates.Save(ctx, shift); err != nil {
		return err
	}
	return uc.Events.Publish(ctx, shift.PullEvents()...)
}
