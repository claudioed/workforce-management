package laborperformance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/laborperformance"
	"github.com/claudioed/workforce-management/internal/application/ports"
)

func TestPermissiveClientAlwaysReportsUnavailable(t *testing.T) {
	client := laborperformance.NewPermissiveClient()
	_, err := client.MeanActualSeconds(context.Background(), "pack")
	if !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
		t.Fatalf("err = %v, want ErrMeasuredRateUnavailable", err)
	}
}
