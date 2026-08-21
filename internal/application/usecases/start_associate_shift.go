// Package usecases implements the Workforce Management use cases. Each use
// case is one struct depending only on domain packages and application
// ports — never on a concrete adapter.
package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// StartAssociateShift starts a shift roster entry for an associate.
type StartAssociateShift struct {
	Associates ports.AssociateRepo
	Events     ports.EventPublisher
	Clock      ports.Clock
}

// Execute starts the associate's shift with the given certifications.
func (uc *StartAssociateShift) Execute(ctx context.Context, associateId shared.AssociateId, certifications []shared.Certification) (*associate.AssociateShift, error) {
	shift := associate.NewAssociateShift(associateId, certifications, uc.Clock.Now())

	if err := uc.Associates.Save(ctx, shift); err != nil {
		return nil, err
	}
	if err := uc.Events.Publish(ctx, shift.PullEvents()...); err != nil {
		return nil, err
	}
	return shift, nil
}
