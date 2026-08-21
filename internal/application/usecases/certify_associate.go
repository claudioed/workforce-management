package usecases

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// CertifyAssociate adds a certification to an associate's shift.
type CertifyAssociate struct {
	Associates ports.AssociateRepo
	Events     ports.EventPublisher
	Clock      ports.Clock
}

// Execute adds certification to associateId.
func (uc *CertifyAssociate) Execute(ctx context.Context, associateId shared.AssociateId, certification shared.Certification) error {
	shift, err := uc.Associates.FindByID(ctx, associateId)
	if err != nil {
		return err
	}
	if err := shift.Certify(certification, uc.Clock.Now()); err != nil {
		return err
	}
	if err := uc.Associates.Save(ctx, shift); err != nil {
		return err
	}
	return uc.Events.Publish(ctx, shift.PullEvents()...)
}
