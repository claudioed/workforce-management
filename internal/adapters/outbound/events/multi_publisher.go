package events

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// MultiPublisher fans one Publish call out to several EventPublishers in order,
// so a single composition can emit to both the integration topic and the
// analytics topic without either use case knowing there is more than one sink.
// It stops and returns on the first publisher error.
type MultiPublisher struct {
	publishers []ports.EventPublisher
}

// NewMultiPublisher constructs a MultiPublisher over publishers.
func NewMultiPublisher(publishers ...ports.EventPublisher) *MultiPublisher {
	return &MultiPublisher{publishers: publishers}
}

// Publish forwards evts to every wrapped publisher, in order.
func (m *MultiPublisher) Publish(ctx context.Context, evts ...shared.DomainEvent) error {
	for _, p := range m.publishers {
		if err := p.Publish(ctx, evts...); err != nil {
			return err
		}
	}
	return nil
}

// Compile-time assertion that MultiPublisher satisfies the port.
var _ ports.EventPublisher = (*MultiPublisher)(nil)
