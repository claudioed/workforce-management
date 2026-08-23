package kafka

import (
	"context"
	"testing"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

func TestHeaderCarrierGetSetKeys(t *testing.T) {
	var headers []segmentio.Header
	c := headerCarrier{headers: &headers}

	if got := c.Get("absent"); got != "" {
		t.Errorf("Get on an empty carrier = %q, want \"\"", got)
	}

	c.Set("traceparent", "first")
	c.Set("baggage", "b=1")
	if got := c.Get("traceparent"); got != "first" {
		t.Errorf("Get(traceparent) = %q, want %q", got, "first")
	}

	// A carrier must be idempotent per key: re-setting replaces rather than
	// appending a duplicate, even though Kafka itself allows duplicate keys.
	c.Set("traceparent", "second")
	if got := c.Get("traceparent"); got != "second" {
		t.Errorf("Get(traceparent) after overwrite = %q, want %q", got, "second")
	}
	if len(headers) != 2 {
		t.Errorf("headers = %v, want 2 entries after an overwrite", headers)
	}

	keys := c.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys() = %v, want 2", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	if !seen["traceparent"] || !seen["baggage"] {
		t.Errorf("Keys() = %v, want traceparent and baggage", keys)
	}
}

// TestHeaderCarrierRoundTripsTraceContext is the propagation contract that
// makes a consumer's span a child of this producer's span: whatever the W3C
// propagator injects into Kafka headers must extract back to the same
// trace/span IDs on the other side of the broker.
func TestHeaderCarrierRoundTripsTraceContext(t *testing.T) {
	propagator := propagation.TraceContext{}

	tid, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("parse trace id: %v", err)
	}
	sid, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("parse span id: %v", err)
	}
	want := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	var headers []segmentio.Header
	propagator.Inject(
		trace.ContextWithSpanContext(context.Background(), want),
		headerCarrier{headers: &headers},
	)
	if len(headers) == 0 {
		t.Fatal("Inject wrote no headers")
	}

	got := trace.SpanContextFromContext(
		propagator.Extract(context.Background(), headerCarrier{headers: &headers}),
	)
	if !got.Equal(want) {
		t.Errorf("extracted span context = %+v, want %+v", got, want)
	}
}

// TestPublishInjectsTraceContextIntoEveryMessage proves the publish path —
// not just the carrier — carries trace context onto the wire, on every
// fanned-out message.
func TestPublishInjectsTraceContextIntoEveryMessage(t *testing.T) {
	lines := []shiftplan.PathPlan{
		{PathId: "pack", PlannedHeads: 3, PlannedRate: 50, PlannedHours: 24},
		{PathId: "pick", PlannedHeads: 2, PlannedRate: 40, PlannedHours: 16},
	}
	sp, err := shiftplan.CommitShiftPlan("BLD1", "SHIFT1", lines,
		map[shared.PathId]int{"pack": 5, "pick": 5}, 8.0,
		time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("commit shift plan: %v", err)
	}
	events := sp.PullEvents()

	pub, fw := newTestPublisher(t, sp)

	// telemetry.Setup installs the W3C propagator in production; install the
	// same one here so this test exercises real injection instead of the
	// default no-op, without depending on test ordering.
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })

	tid, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("parse trace id: %v", err)
	}
	sid, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("parse span id: %v", err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	}))

	if err := pub.Publish(ctx, events...); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(fw.msgs) != len(lines) {
		t.Fatalf("published %d messages, want %d", len(fw.msgs), len(lines))
	}

	for i, msg := range fw.msgs {
		headers := msg.Headers
		got := trace.SpanContextFromContext(
			propagation.TraceContext{}.Extract(context.Background(), headerCarrier{headers: &headers}),
		)
		if !got.IsValid() {
			t.Errorf("message %d carried no usable trace context (headers: %v)", i, headers)
			continue
		}
		if got.TraceID() != tid {
			t.Errorf("message %d carried trace id %s, want %s", i, got.TraceID(), tid)
		}
	}
}
