package laborperformance

import (
	"context"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// PermissiveClient is the default MeasuredRateClient: it never contacts
// labor-performance and always returns ports.ErrMeasuredRateUnavailable,
// which ProposePathPlan treats identically to any other unavailable-rate
// case -- fall back to the caller-supplied plannedRate. Unlike
// order-management's inventory-storage PermissiveClient (which fails loud
// because reserving real stock is not optional), this fails quietly by
// design: a measured rate is a soft, optional enrichment, and
// ProposePathPlan must keep working with a caller-supplied rate whether or
// not this adapter is wired to a real service.
type PermissiveClient struct{}

// NewPermissiveClient constructs a PermissiveClient.
func NewPermissiveClient() *PermissiveClient { return &PermissiveClient{} }

func (PermissiveClient) MeanActualSeconds(_ context.Context, _ shared.PathId) (float64, error) {
	return 0, ports.ErrMeasuredRateUnavailable
}
