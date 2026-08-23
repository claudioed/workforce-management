package kafka

import (
	segmentio "github.com/segmentio/kafka-go"
)

// headerCarrier adapts a kafka-go header slice to OTel's
// propagation.TextMapCarrier so trace context can ride along with a message.
// kafka-go has no built-in instrumentation, so this hand-written carrier is
// what makes a consumer's span a child of this producer's span.
//
// It holds a pointer to the slice because Set appends.
type headerCarrier struct {
	headers *[]segmentio.Header
}

// Get returns the value of the first header with the given key, or "" if
// absent.
func (c headerCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set replaces any existing header with the given key, then appends the new
// value — a carrier must be idempotent per key, and Kafka permits duplicate
// header keys, so overwriting has to be explicit.
func (c headerCarrier) Set(key, value string) {
	headers := (*c.headers)[:0]
	for _, h := range *c.headers {
		if h.Key != key {
			headers = append(headers, h)
		}
	}
	headers = append(headers, segmentio.Header{Key: key, Value: []byte(value)})
	*c.headers = headers
}

// Keys lists every header key present on the message.
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, h.Key)
	}
	return keys
}
