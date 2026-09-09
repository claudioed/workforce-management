package fulfillmentexecution_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/fulfillmentexecution"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// Fleet ADR 0005: the outbound client presents "Authorization: Bearer <key>"
// exactly when FULFILLMENT_EXECUTION_API_KEY is configured, and sends no
// Authorization header at all otherwise.
func TestInstalledCapacityBearerHeaderPresentIffConfigured(t *testing.T) {
	tests := []struct {
		name      string
		opts      []fulfillmentexecution.Option
		wantAuthz string
	}{
		{"no token configured", nil, ""},
		{"empty token configured", []fulfillmentexecution.Option{fulfillmentexecution.WithBearerToken("")}, ""},
		{"token configured", []fulfillmentexecution.Option{fulfillmentexecution.WithBearerToken("fe-read-key")}, "Bearer fe-read-key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuthz string
			var sawHeader bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuthz = r.Header.Get("Authorization")
				_, sawHeader = r.Header["Authorization"]
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"capability":"pack","installed":2}`))
			}))
			t.Cleanup(srv.Close)

			client := fulfillmentexecution.NewClient(srv.URL, nil, tc.opts...)
			if _, err := client.InstalledCapacity(context.Background(), shared.PathId("pack")); err != nil {
				t.Fatalf("InstalledCapacity: %v", err)
			}
			if gotAuthz != tc.wantAuthz {
				t.Fatalf("Authorization = %q, want %q", gotAuthz, tc.wantAuthz)
			}
			if tc.wantAuthz == "" && sawHeader {
				t.Fatal("Authorization header must be absent when no token is configured")
			}
		})
	}
}
