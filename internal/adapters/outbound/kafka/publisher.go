// Package kafka provides a Kafka-backed ports.EventPublisher implementation.
// It publishes to the shared cross-service broker described in
// INTEGRATION.md: one ShiftPlanCommitted message per PathPlan line, on topic
// warehouse.workforce.events.
package kafka

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	segmentio "github.com/segmentio/kafka-go"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// Topic is the shared warehouse-systems topic this service publishes to.
const Topic = "warehouse.workforce.events"

// source identifies this service in the published envelope.
const source = "workforce-management"

// messageWriter is the subset of *segmentio.Writer this package depends on,
// so tests can substitute a fake instead of hitting a real broker.
type messageWriter interface {
	WriteMessages(ctx context.Context, msgs ...segmentio.Message) error
}

// envelope is the cross-service message shape shared by every
// warehouse-systems service, as documented in INTEGRATION.md.
type envelope struct {
	EventID    string       `json:"event_id"`
	EventType  string       `json:"event_type"`
	OccurredAt string       `json:"occurred_at"`
	Source     string       `json:"source"`
	Data       envelopeData `json:"data"`
}

type envelopeData struct {
	BuildingId   string  `json:"building_id"`
	ShiftId      string  `json:"shift_id"`
	PathId       string  `json:"path_id"`
	PlannedHeads int     `json:"planned_heads"`
	PlannedRate  float64 `json:"planned_rate"`
	PlannedHours float64 `json:"planned_hours"`
}

// Publisher implements ports.EventPublisher by publishing to Kafka. The
// domain-level ShiftPlanCommitted event carries only buildingId/shiftId (a
// ShiftPlan's identity); ShiftPlans is used to load the committed plan's
// PathPlan lines so one message can be fanned out per line, as
// INTEGRATION.md requires.
type Publisher struct {
	writer     messageWriter
	shiftPlans ports.ShiftPlanRepo
}

// NewPublisher constructs a Publisher writing to brokers on Topic.
func NewPublisher(brokers []string, shiftPlans ports.ShiftPlanRepo) *Publisher {
	return &Publisher{
		writer: &segmentio.Writer{
			Addr:                   segmentio.TCP(brokers...),
			Topic:                  Topic,
			Balancer:               &segmentio.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
		shiftPlans: shiftPlans,
	}
}

// Close releases the underlying Kafka writer's resources.
func (p *Publisher) Close() error {
	if w, ok := p.writer.(*segmentio.Writer); ok {
		return w.Close()
	}
	return nil
}

// Publish fans ShiftPlanCommitted events out into one Kafka message per
// PathPlan line. Other event types are ignored: this round only publishes
// ShiftPlanCommitted, per INTEGRATION.md.
func (p *Publisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	var msgs []segmentio.Message
	for _, e := range events {
		committed, ok := e.(shared.ShiftPlanCommitted)
		if !ok {
			continue
		}
		sp, err := p.shiftPlans.FindByBuildingAndShift(ctx, committed.BuildingId, committed.ShiftId)
		if err != nil {
			return fmt.Errorf("kafka publisher: load committed shift plan: %w", err)
		}
		for _, line := range sp.Lines() {
			env := envelope{
				EventID:    newEventID(),
				EventType:  committed.EventName(),
				OccurredAt: committed.OccurredAt().UTC().Format(time.RFC3339),
				Source:     source,
				Data: envelopeData{
					BuildingId:   committed.BuildingId,
					ShiftId:      committed.ShiftId,
					PathId:       string(line.PathId),
					PlannedHeads: line.PlannedHeads,
					PlannedRate:  line.PlannedRate,
					PlannedHours: line.PlannedHours,
				},
			}
			b, err := json.Marshal(env)
			if err != nil {
				return fmt.Errorf("kafka publisher: marshal envelope: %w", err)
			}
			msgs = append(msgs, segmentio.Message{Value: b})
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return p.writer.WriteMessages(ctx, msgs...)
}

// newEventID generates a random UUID v4 without pulling in a UUID
// dependency beyond what INTEGRATION.md already requires (kafka-go).
func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
