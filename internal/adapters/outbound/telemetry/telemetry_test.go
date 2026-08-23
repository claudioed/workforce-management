package telemetry

import (
	"context"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// unreachableEndpoint is a loopback port nothing listens on. Binding a
// listener and closing it immediately gives us a port that is almost
// certainly free, so these tests exercise the "no Collector running" path
// rather than accidentally talking to a real one.
func unreachableEndpoint(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// TestSetupDoesNotBlockWithoutACollector is the concrete form of the
// non-blocking-export requirement: with nothing listening at the endpoint,
// Setup must succeed and return promptly, and shutdown must come back within
// its own context deadline rather than hanging. A missing Collector degrades
// to "telemetry dropped", never to "service won't start" or "shutdown
// wedges".
//
// Shutdown's *error* is deliberately not asserted: flushing to a dead
// endpoint legitimately reports "deadline exceeded", and main logs that at
// Warn rather than treating it as a failure.
func TestSetupDoesNotBlockWithoutACollector(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "explicit unreachable endpoint", endpoint: unreachableEndpoint(t)},
		{name: "empty endpoint falls back to the default", endpoint: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupDone := make(chan error, 1)
			var shutdown func(context.Context) error
			go func() {
				var err error
				shutdown, err = Setup(context.Background(), "workforce-management-test", "test", tc.endpoint)
				setupDone <- err
			}()

			select {
			case err := <-setupDone:
				if err != nil {
					t.Fatalf("Setup with no collector listening: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Setup blocked waiting for a collector to become reachable")
			}

			shutdownDone := make(chan struct{})
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = shutdown(ctx)
				close(shutdownDone)
			}()

			select {
			case <-shutdownDone:
			case <-time.After(10 * time.Second):
				t.Fatal("shutdown ignored its context deadline and hung on an unreachable collector")
			}
		})
	}
}

// TestSetupInstallsGlobalProviders checks Setup actually takes effect
// globally — otherwise instruments created against otel.Meter/otel.Tracer
// elsewhere in the codebase would stay no-ops.
func TestSetupInstallsGlobalProviders(t *testing.T) {
	shutdown, err := Setup(context.Background(), "workforce-management-test", "test", unreachableEndpoint(t))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Error ignored: no collector is listening, so the final flush
		// legitimately times out. See TestSetupDoesNotBlockWithoutACollector.
		_ = shutdown(ctx)
	})

	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Error("expected a recording span from the installed TracerProvider, got an invalid span context")
	}

	if _, err := otel.Meter("test").Int64Counter("test.counter"); err != nil {
		t.Errorf("create counter from the installed MeterProvider: %v", err)
	}

	// The W3C propagator has to be installed too, or Kafka header injection
	// silently writes nothing.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if carrier.Get("traceparent") == "" {
		t.Error("expected traceparent to be injected, got none — is the propagator installed?")
	}
}

func TestEnvironmentDefaultsToLocal(t *testing.T) {
	tests := []struct {
		name string
		set  string
		want string
	}{
		{name: "unset", set: "", want: "local"},
		{name: "explicit", set: "production", want: "production"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", tc.set)
			if got := Environment(); got != tc.want {
				t.Errorf("Environment() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewResourceCarriesServiceIdentity(t *testing.T) {
	t.Setenv("ENVIRONMENT", "staging")

	res, err := newResource("workforce-management", "1.2.3")
	if err != nil {
		t.Fatalf("newResource: %v", err)
	}

	want := map[string]string{
		"service.name":                "workforce-management",
		"service.version":             "1.2.3",
		"deployment.environment.name": "staging",
	}
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		if _, ok := want[string(kv.Key)]; ok {
			got[string(kv.Key)] = kv.Value.AsString()
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("resource attribute %s = %q, want %q", k, got[k], v)
		}
	}
}
