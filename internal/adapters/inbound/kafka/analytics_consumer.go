// Package kafka provides the inbound Kafka adapter for the workforce analytics
// data product: a consumer that reads the analytics topic and applies each
// event to the Labor Utilization & Staffing projection, exactly once per
// event_id despite Kafka's at-least-once delivery.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/workforce-management/internal/analytics/report"
	"github.com/claudioed/workforce-management/internal/application/ports"
)

// tracerName identifies this adapter's instrumentation scope.
const tracerName = "github.com/claudioed/workforce-management/internal/adapters/inbound/kafka"

// AnalyticsConsumerGroup is the Kafka consumer group the analytics projector
// reads under. It is distinct from any OLTP consumer group so the two pipelines
// track their offsets independently.
const AnalyticsConsumerGroup = "workforce-analytics"

// headerCarrier adapts a kafka-go header slice to OTel's
// propagation.TextMapCarrier so the producer's trace context can be read off a
// consumed message. It mirrors the outbound adapter's carrier (kept local so
// this inbound adapter does not depend on the outbound one).
type headerCarrier struct {
	headers []segmentio.Header
}

func (c headerCarrier) Get(key string) string {
	for _, h := range c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c headerCarrier) Set(string, string) {}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for _, h := range c.headers {
		keys = append(keys, h.Key)
	}
	return keys
}

// analyticsEnvelope is the inbound decode shape of the Envelope v1 wrapper on
// the analytics topic. The data payload is left as a RawMessage and decoded per
// event_type. It is declared here (rather than imported from the outbound
// publisher) so this inbound adapter does not depend on an outbound adapter.
type analyticsEnvelope struct {
	EventId       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Source        string          `json:"source"`
	SchemaVersion int             `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
}

// analyticsData is the union of fields the projecting event payloads carry.
// Each event_type populates the subset it needs.
type analyticsData struct {
	AssociateId string `json:"associate_id"`
	PathId      string `json:"path_id"`
	ToPathId    string `json:"to_path_id"`
}

// AnalyticsConsumer reads analytics events off the analytics topic and applies
// each to the labor ProjectionStore, exactly once per event_id despite Kafka's
// at-least-once delivery.
type AnalyticsConsumer struct {
	Reader     *segmentio.Reader
	Projection report.ProjectionStore
	Processed  ports.ProcessedEvents
	Logger     *slog.Logger
}

// NewAnalyticsConsumer constructs an AnalyticsConsumer reading topic from
// brokers under AnalyticsConsumerGroup.
func NewAnalyticsConsumer(brokers []string, topic string, projection report.ProjectionStore, processed ports.ProcessedEvents, logger *slog.Logger) *AnalyticsConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	reader := segmentio.NewReader(segmentio.ReaderConfig{
		Brokers: brokers,
		Topic:   topic,
		GroupID: AnalyticsConsumerGroup,
		// Start a brand-new consumer group at the EARLIEST offset. The analytics
		// projection must see the full history of the topic (it is a replayable
		// read model, not a live integration reaction), so a fresh projector — or
		// a backfill into a new group — reads from the beginning rather than
		// kafka-go's default of the latest offset, which would silently drop
		// every event produced before the group first committed an offset. Once
		// the group has committed offsets, those take precedence and this only
		// affects the first join.
		StartOffset: segmentio.FirstOffset,
	})
	return &AnalyticsConsumer{Reader: reader, Projection: projection, Processed: processed, Logger: logger}
}

// Run reads and handles messages until ctx is cancelled or the reader returns a
// fatal error. A handling error is logged and the loop continues so one bad
// message cannot wedge the projector.
func (c *AnalyticsConsumer) Run(ctx context.Context) error {
	for {
		msg, err := c.Reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if err := c.Handle(ctx, msg); err != nil {
			c.Logger.ErrorContext(ctx, "analytics message handling failed", "error", err)
		}
	}
}

// Close releases the underlying Kafka reader.
func (c *AnalyticsConsumer) Close() error {
	return c.Reader.Close()
}

// Handle processes one consumed message inside a "kafka.consume <topic>" span
// whose parent is the producer's span, read from the message headers. It is
// exported separately from Run so the propagation can be tested without a live
// broker.
func (c *AnalyticsConsumer) Handle(ctx context.Context, msg segmentio.Message) error {
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.TextMapCarrier(headerCarrier{headers: msg.Headers}))

	ctx, span := otel.Tracer(tracerName).Start(ctx,
		"kafka.consume "+msg.Topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(msg.Topic),
			semconv.MessagingOperationName("process"),
		),
	)
	defer span.End()

	if err := c.HandleMessage(ctx, msg.Value); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// HandleMessage decodes raw as an analyticsEnvelope and applies the matching
// projection method for its event_type. Event types outside the projection
// contract are ignored (and not marked processed). For a projecting event it
// dedupes on event_id via ProcessedEvents before applying, so a redelivery is a
// no-op. It is exported separately from Run so tests can feed raw envelopes
// without a live broker.
func (c *AnalyticsConsumer) HandleMessage(ctx context.Context, raw []byte) error {
	var env analyticsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("analytics: decode envelope: %w", err)
	}

	// Only the utilization/staffing-moving events project; the rest
	// (ShiftPlanProposed, ShiftPlanCommitted) are acknowledged without touching
	// the read model or the processed set.
	switch env.EventType {
	case "AssociateShiftStarted", "AssociateShiftEnded",
		"AssociateBreakStarted", "AssociateBreakEnded", "AssociateCertified",
		"LaborAssigned", "LaborReassigned", "PathUnderstaffed":
	default:
		return nil
	}

	isNew, err := c.Processed.MarkProcessed(ctx, env.EventId)
	if err != nil {
		return fmt.Errorf("analytics: mark processed: %w", err)
	}
	if !isNew {
		return nil
	}

	var data analyticsData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return fmt.Errorf("analytics: decode data: %w", err)
	}

	switch env.EventType {
	case "AssociateShiftStarted":
		return c.Projection.ApplyShiftStarted(ctx, env.EventId, data.AssociateId, env.OccurredAt)
	case "AssociateShiftEnded":
		return c.Projection.ApplyShiftEnded(ctx, env.EventId, data.AssociateId, env.OccurredAt)
	case "AssociateBreakStarted":
		return c.Projection.ApplyBreakStarted(ctx, env.EventId, data.AssociateId, env.OccurredAt)
	case "AssociateBreakEnded":
		return c.Projection.ApplyBreakEnded(ctx, env.EventId, data.AssociateId, env.OccurredAt)
	case "AssociateCertified":
		return c.Projection.ApplyCertified(ctx, env.EventId, data.AssociateId, env.OccurredAt)
	case "LaborAssigned":
		return c.Projection.ApplyLaborAssigned(ctx, env.EventId, data.PathId, env.OccurredAt)
	case "LaborReassigned":
		return c.Projection.ApplyLaborReassigned(ctx, env.EventId, data.ToPathId, env.OccurredAt)
	case "PathUnderstaffed":
		return c.Projection.ApplyPathUnderstaffed(ctx, env.EventId, data.PathId, env.OccurredAt)
	default:
		return nil
	}
}
