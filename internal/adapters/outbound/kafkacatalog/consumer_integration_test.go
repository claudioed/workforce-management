//go:build integration

package kafkacatalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
)

// TestNewConsumerForTopic_TwoInstancesInARow_BothReplayFully verifies the
// real replay/readiness path against an isolated Kafka broker. The second
// consumer models a process restart and must replay the complete topic under
// its own consumer group instead of resuming the first consumer's offsets.
func TestNewConsumerForTopic_TwoInstancesInARow_BothReplayFully(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	container, err := tckafka.Run(ctx, "confluentinc/confluent-local:7.6.1", tckafka.WithClusterID("workforce-kafkacatalog-itest"))
	if err != nil {
		t.Fatalf("start Kafka container: %v", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate Kafka container: %v", err)
		}
	}()

	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("Kafka brokers: %v", err)
	}
	topic := fmt.Sprintf("workforce-kafkacatalog-itest-%d", time.Now().UnixNano())
	createTopicAndWaitForLeader(t, ctx, brokers, topic)

	writer := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: topic}
	defer func() { _ = writer.Close() }()
	if err := writer.WriteMessages(ctx, kafkago.Message{Value: []byte(`{"event_type":"ProcessPathCreated","data":{"path_id":"ITEST","match_prefix":"itest","direct":true,"required_capabilities":["itest"]}}`)}); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	for instance := 1; instance <= 2; instance++ {
		consumer, err := NewConsumerForTopic(ctx, brokers, topic, nil)
		if err != nil {
			t.Fatalf("construct consumer %d: %v", instance, err)
		}
		runCtx, runCancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- consumer.Run(runCtx) }()

		if err := consumer.WaitReady(ctx); err != nil {
			runCancel()
			_ = consumer.Close()
			t.Fatalf("consumer %d did not become ready: %v", instance, err)
		}
		if _, err := consumer.Lookup("itest-order"); err != nil {
			runCancel()
			_ = consumer.Close()
			t.Fatalf("consumer %d did not replay the path: %v", instance, err)
		}

		runCancel()
		if err := consumer.Close(); err != nil {
			t.Fatalf("close consumer %d: %v", instance, err)
		}
		if err := <-done; err != nil {
			t.Fatalf("run consumer %d: %v", instance, err)
		}
	}
}

func createTopicAndWaitForLeader(t *testing.T, ctx context.Context, brokers []string, topic string) {
	t.Helper()
	conn, err := kafkago.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatalf("dial Kafka controller: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.CreateTopics(kafkago.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatalf("create topic %q: %v", topic, err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		partitions, err := conn.ReadPartitions(topic)
		if err == nil && len(partitions) == 1 && partitions[0].Leader.ID >= 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("topic %q did not obtain a partition leader", topic)
}
