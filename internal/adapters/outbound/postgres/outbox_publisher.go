package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	segmentio "github.com/segmentio/kafka-go"

	outboundkafka "github.com/claudioed/workforce-management/internal/adapters/outbound/kafka"
	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// OutboxPublisher implements ports.EventPublisher by writing each event's
// Kafka wire form into outbox_events instead of the broker (ADR 0016).
//
// It fans out: every event is run through EVERY configured Encoder (the
// integration Publisher and the AnalyticsPublisher in production), and one
// row is inserted per Encoded message — so a single ShiftPlanCommitted
// yields N integration rows (one per PathPlan line) plus one analytics row,
// all in the same transaction as the aggregate when called inside
// UnitOfWork.Execute. Encoding happens here, inside that transaction,
// because the integration Encoder re-reads the ShiftPlan the use case just
// saved. OutboxRelay later drains the table onto Kafka.
type OutboxPublisher struct {
	pool     *pgxpool.Pool
	encoders []outboundkafka.Encoder
}

// NewOutboxPublisher constructs an OutboxPublisher over pool feeding
// encoders. It never touches the broker.
func NewOutboxPublisher(pool *pgxpool.Pool, encoders ...outboundkafka.Encoder) *OutboxPublisher {
	return &OutboxPublisher{pool: pool, encoders: encoders}
}

// headerJSON is the persisted shape of one Kafka header.
type headerJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func marshalHeaders(hs []segmentio.Header) ([]byte, error) {
	out := make([]headerJSON, len(hs))
	for i, h := range hs {
		out[i] = headerJSON{Key: h.Key, Value: string(h.Value)}
	}
	return json.Marshal(out)
}

func unmarshalHeaders(b []byte) ([]segmentio.Header, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var in []headerJSON
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]segmentio.Header, len(in))
	for i, h := range in {
		out[i] = segmentio.Header{Key: h.Key, Value: []byte(h.Value)}
	}
	return out, nil
}

// Publish stores the wire form of events for every configured topic in the
// outbox, through the transaction bound to ctx when there is one.
func (p *OutboxPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	if len(events) == 0 {
		return nil
	}
	q := querierFrom(ctx, p.pool)
	for _, enc := range p.encoders {
		msgs, err := enc.Encode(ctx, events...)
		if err != nil {
			return fmt.Errorf("postgres: encode outbox events: %w", err)
		}
		for _, m := range msgs {
			headers, err := marshalHeaders(m.Headers)
			if err != nil {
				return fmt.Errorf("postgres: marshal outbox headers for %s: %w", m.EventType, err)
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO outbox_events (topic, event_type, key, value, headers)
				VALUES ($1, $2, $3, $4, $5)
			`, m.Topic, m.EventType, m.Key, m.Value, headers); err != nil {
				return fmt.Errorf("postgres: enqueue outbox event %s for %s: %w", m.EventType, m.Topic, err)
			}
		}
	}
	return nil
}

// Compile-time assertion that OutboxPublisher satisfies the port.
var _ ports.EventPublisher = (*OutboxPublisher)(nil)
