package kafka

import (
	"context"

	segmentio "github.com/segmentio/kafka-go"

	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// Encoded is one wire-ready Kafka message: everything a broker write needs
// except the connection. Topic is carried explicitly (rather than on the
// kafka-go Message) because the two ways of writing an Encoded differ:
//
//   - Publisher / AnalyticsPublisher own a Writer with a FIXED Topic, and
//     kafka-go rejects a Message that also names a topic, so their
//     WriteMessages call must leave Message.Topic empty; and
//   - RelaySink owns a Writer with NO Topic and sets Message.Topic per
//     message from Encoded.Topic, so one relay can drain both the
//     integration and the analytics stream out of one outbox table.
//
// Splitting encoding from sending is what lets the transactional outbox
// (ADR 0016) persist the exact bytes the direct publishers would have
// written: the outbox and the direct path can never disagree on wire
// format because they share the same Encode.
type Encoded struct {
	Topic     string
	EventType string
	Key       []byte
	Value     []byte
	Headers   []segmentio.Header
}

// Encoder turns domain events into wire-ready messages without touching a
// broker. Both Publisher and AnalyticsPublisher implement it, and
// postgres.OutboxPublisher fans every event through each configured Encoder
// so a single outbox row set feeds both topics.
type Encoder interface {
	Encode(ctx context.Context, events ...shared.DomainEvent) ([]Encoded, error)
}

// Writer is the subset of *segmentio.Writer this package depends on, so tests
// can substitute a fake instead of hitting a real broker.
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...segmentio.Message) error
}

// message converts an Encoded into a kafka-go Message. withTopic controls
// whether Encoded.Topic is copied onto the message: true for RelaySink's
// topic-less writer, false for the fixed-topic publishers (see the Encoded
// doc comment for why kafka-go forces this distinction).
func (e Encoded) message(withTopic bool) segmentio.Message {
	msg := segmentio.Message{Key: e.Key, Value: e.Value, Headers: e.Headers}
	if withTopic {
		msg.Topic = e.Topic
	}
	return msg
}
