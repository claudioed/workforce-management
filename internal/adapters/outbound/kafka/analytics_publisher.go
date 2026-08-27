package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// AnalyticsTopic is the dedicated topic the analytics data product consumes.
// It is separate from the integration topic (Topic) so the OLTP integration
// contract and the analytical read-model stream evolve independently
// (ADR-0010).
const AnalyticsTopic = "warehouse.workforce.analytics"

// analyticsSchemaVersion is the schema version stamped onto every analytics
// envelope this publisher emits.
const analyticsSchemaVersion = 1

// AnalyticsEnvelope is the shared Envelope v1 wrapper for the analytics stream.
// Unlike the integration envelope it carries the payload as a json.RawMessage
// so a single publisher can emit the event_type-specific data object for every
// workforce domain event without a bespoke struct per type.
type AnalyticsEnvelope struct {
	EventId       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Source        string          `json:"source"`
	SchemaVersion int             `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
}

// AnalyticsPublisher publishes every workforce-management domain event onto
// AnalyticsTopic as an AnalyticsEnvelope. It satisfies ports.EventPublisher and
// is a SEPARATE adapter from Publisher: the integration publisher (publisher.go)
// forwards only ShiftPlanCommitted and is left untouched.
//
// The message key is the aggregate id — AssociateId for associate-scoped events
// and PathId for path-scoped events — so per-aggregate ordering is preserved on
// the topic.
type AnalyticsPublisher struct {
	Writer messageWriter
	NewId  func() string
}

// NewAnalyticsPublisher constructs an AnalyticsPublisher writing to
// AnalyticsTopic on brokers. newId mints the envelope event_id.
func NewAnalyticsPublisher(brokers []string, newId func() string) *AnalyticsPublisher {
	return &AnalyticsPublisher{
		Writer: &segmentio.Writer{
			Addr:                   segmentio.TCP(brokers...),
			Topic:                  AnalyticsTopic,
			Balancer:               &segmentio.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
		NewId: newId,
	}
}

// Publish emits every event in evts onto AnalyticsTopic. Events with no
// analytics payload (an unrecognised type) are skipped rather than erroring, so
// the caller can hand it the full event stream indiscriminately.
func (p *AnalyticsPublisher) Publish(ctx context.Context, evts ...shared.DomainEvent) error {
	for _, e := range evts {
		eventType, key, data, ok := marshalAnalyticsData(e)
		if !ok {
			continue
		}
		env := AnalyticsEnvelope{
			EventId:       p.NewId(),
			EventType:     eventType,
			OccurredAt:    e.OccurredAt(),
			Source:        source,
			SchemaVersion: analyticsSchemaVersion,
			Data:          data,
		}
		payload, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("kafka: marshal analytics envelope: %w", err)
		}
		if err := p.write(ctx, eventType, key, payload); err != nil {
			return err
		}
	}
	return nil
}

// marshalAnalyticsData maps a domain event to its analytics event_type,
// aggregate-id message key, and snake_case JSON payload. The bool return is
// false for an event type outside the analytics contract, so Publish can skip
// it.
func marshalAnalyticsData(e shared.DomainEvent) (eventType, key string, data json.RawMessage, ok bool) {
	switch ev := e.(type) {
	case shared.AssociateShiftStarted:
		return "AssociateShiftStarted", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id": string(ev.AssociateId),
		}), true
	case shared.AssociateShiftEnded:
		return "AssociateShiftEnded", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id": string(ev.AssociateId),
		}), true
	case shared.AssociateBreakStarted:
		return "AssociateBreakStarted", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id": string(ev.AssociateId),
		}), true
	case shared.AssociateBreakEnded:
		return "AssociateBreakEnded", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id": string(ev.AssociateId),
		}), true
	case shared.AssociateCertified:
		return "AssociateCertified", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id":  string(ev.AssociateId),
			"certification": string(ev.Certification),
		}), true
	case shared.LaborAssigned:
		return "LaborAssigned", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id": string(ev.AssociateId),
			"path_id":      string(ev.PathId),
		}), true
	case shared.LaborReassigned:
		return "LaborReassigned", string(ev.AssociateId), mustMarshal(map[string]any{
			"associate_id": string(ev.AssociateId),
			"from_path_id": string(ev.FromPathId),
			"to_path_id":   string(ev.ToPathId),
		}), true
	case shared.PathUnderstaffed:
		return "PathUnderstaffed", string(ev.PathId), mustMarshal(map[string]any{
			"path_id":       string(ev.PathId),
			"planned_heads": ev.PlannedHeads,
			"active_heads":  ev.ActiveHeads,
		}), true
	case shared.ShiftPlanProposed:
		return "ShiftPlanProposed", string(ev.PathId), mustMarshal(map[string]any{
			"building_id":   ev.BuildingId,
			"path_id":       string(ev.PathId),
			"planned_heads": ev.PlannedHeads,
			"planned_rate":  ev.PlannedRate,
		}), true
	case shared.ShiftPlanCommitted:
		return "ShiftPlanCommitted", ev.BuildingId, mustMarshal(map[string]any{
			"building_id": ev.BuildingId,
			"shift_id":    ev.ShiftId,
		}), true
	default:
		return "", "", nil, false
	}
}

// mustMarshal marshals a map whose shape is fully controlled by
// marshalAnalyticsData, so an error here is a programming mistake rather than a
// runtime condition.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("kafka: marshal analytics data: %v", err))
	}
	return b
}

// write publishes one already-marshalled envelope inside a
// "kafka.publish <topic>" producer span, injecting that span's context into the
// message headers (via the shared headerCarrier) so the projector's consume
// span becomes its child.
func (p *AnalyticsPublisher) write(ctx context.Context, eventType, key string, payload []byte) error {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "kafka.publish "+AnalyticsTopic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingOperationName("publish"),
			semconv.MessagingDestinationName(AnalyticsTopic),
		),
	)
	defer span.End()

	msg := segmentio.Message{Key: []byte(key), Value: payload}
	otel.GetTextMapPropagator().Inject(ctx, propagation.TextMapCarrier(headerCarrier{headers: &msg.Headers}))

	if err := p.Writer.WriteMessages(ctx, msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("kafka: publish %s analytics event: %w", eventType, err)
	}
	return nil
}

// Close releases the underlying Kafka writer.
func (p *AnalyticsPublisher) Close() error {
	if w, ok := p.Writer.(*segmentio.Writer); ok {
		return w.Close()
	}
	return nil
}

// Compile-time assertion that AnalyticsPublisher satisfies the outbound
// event-publishing port.
var _ ports.EventPublisher = (*AnalyticsPublisher)(nil)
