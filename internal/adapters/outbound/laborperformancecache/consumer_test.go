package laborperformancecache

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// fakeReader replays a fixed sequence of messages, then blocks until ctx
// is cancelled -- mirrors a real Kafka reader that has caught up and is
// now waiting for new messages.
type fakeReader struct {
	messages []kafkago.Message
	pos      int
	closed   bool
}

func (r *fakeReader) ReadMessage(ctx context.Context) (kafkago.Message, error) {
	if r.pos < len(r.messages) {
		m := r.messages[r.pos]
		r.pos++
		return m, nil
	}
	<-ctx.Done()
	return kafkago.Message{}, ctx.Err()
}

func (r *fakeReader) Close() error {
	r.closed = true
	return nil
}

func ptr(f float64) *float64 { return &f }

func envelopeMsg(t *testing.T, partition int, offset int64, eventType string, data any) kafkago.Message {
	t.Helper()
	rawData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	env := envelope{EventType: eventType, Data: rawData}
	rawEnv, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return kafkago.Message{Partition: partition, Offset: offset, Value: rawEnv}
}

func newTestConsumer(reader Reader, target targetOffsets) *Consumer {
	c := &Consumer{
		Reader:  reader,
		totals:  make(map[string]runningMean),
		readyCh: make(chan struct{}),
		target:  target,
	}
	if len(target) == 0 {
		c.markReady()
	}
	return c
}

func TestConsumer_NoTargetOffsets_IsReadyImmediately(t *testing.T) {
	c := newTestConsumer(&fakeReader{}, targetOffsets{})
	if !c.Ready() {
		t.Fatal("expected a consumer with no readiness target to be ready immediately")
	}
}

func TestConsumer_Run_BecomesReadyAfterCatchingUpSinglePartition(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t1", AssociateId: "a1", TaskType: "PICK", EfficiencyPct: ptr(91.2), ActualSeconds: 40, CompletedAt: "2026-09-05T09:30:00Z"}),
			envelopeMsg(t, 0, 1, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t2", AssociateId: "a1", TaskType: "PICK", EfficiencyPct: ptr(88.0), ActualSeconds: 60, CompletedAt: "2026-09-05T10:30:00Z"}),
		},
	}
	// target[0] = 2 means "caught up once offset 1 has been processed"
	// (last is exclusive: 2 messages at offsets 0 and 1).
	c := newTestConsumer(reader, targetOffsets{0: 2})

	if c.Ready() {
		t.Fatal("expected not ready before Run starts")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = c.Run(ctx)
		close(done)
	}()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}
	cancel()
	<-done

	got, err := c.MeanActualSeconds(context.Background(), shared.PathId("pick"))
	if err != nil {
		t.Fatalf("expected MeanActualSeconds to succeed, got: %v", err)
	}
	// mean of 40 and 60 is 50.
	if got != 50 {
		t.Fatalf("mean = %v, want 50", got)
	}
}

func TestConsumer_MultiPartition_ReadyOnlyAfterBothCaughtUp(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t1", TaskType: "PICK", ActualSeconds: 40}),
			// Partition 1 not yet caught up (target[1]=1, need offset 0).
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1, 1: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err == nil {
		t.Fatal("expected WaitReady to time out -- partition 1 never caught up")
	}
}

// TestConsumer_NullEfficiencyPct_StillUpdatesMean is the key contract
// test: a null efficiency_pct means "unscorable", NOT "no duration data"
// -- actual_seconds is always present, so the message must still count
// toward the running mean.
func TestConsumer_NullEfficiencyPct_StillUpdatesMean(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t1", TaskType: "PACK", EfficiencyPct: nil, ActualSeconds: 30}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	got, err := c.MeanActualSeconds(context.Background(), shared.PathId("pack"))
	if err != nil {
		t.Fatalf("expected MeanActualSeconds to succeed despite a null efficiency_pct, got: %v", err)
	}
	if got != 30 {
		t.Fatalf("mean = %v, want 30", got)
	}
}

func TestConsumer_UnknownEventType_IsIgnored(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, "SomeFutureEventType", map[string]any{}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout even for an unrecognized event type, got: %v", err)
	}
}

func TestMeanActualSeconds_PathWithNoTaskTypeCounterpart_ReturnsUnavailable(t *testing.T) {
	c := newTestConsumer(&fakeReader{}, targetOffsets{})
	for _, pathId := range []string{"stow", "hazmat", "unknown-path"} {
		_, err := c.MeanActualSeconds(context.Background(), shared.PathId(pathId))
		if !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
			t.Fatalf("path %q: err = %v, want ErrMeasuredRateUnavailable", pathId, err)
		}
	}
}

func TestMeanActualSeconds_NoDataObservedYet_ReturnsUnavailable(t *testing.T) {
	c := newTestConsumer(&fakeReader{}, targetOffsets{})
	_, err := c.MeanActualSeconds(context.Background(), shared.PathId("pick"))
	if !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
		t.Fatalf("err = %v, want ErrMeasuredRateUnavailable", err)
	}
}

func TestConsumer_MultipleTaskTypes_TrackedIndependently(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t1", TaskType: "PICK", ActualSeconds: 40}),
			envelopeMsg(t, 0, 1, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t2", TaskType: "PACK", ActualSeconds: 20}),
			envelopeMsg(t, 0, 2, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t3", TaskType: "PICK", ActualSeconds: 60}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 3})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	pick, err := c.MeanActualSeconds(context.Background(), shared.PathId("pick"))
	if err != nil {
		t.Fatalf("PICK: %v", err)
	}
	if pick != 50 {
		t.Fatalf("PICK mean = %v, want 50", pick)
	}

	pack, err := c.MeanActualSeconds(context.Background(), shared.PathId("pack"))
	if err != nil {
		t.Fatalf("PACK: %v", err)
	}
	if pack != 20 {
		t.Fatalf("PACK mean = %v, want 20", pack)
	}

	// SLAM has no observed data.
	if _, err := c.MeanActualSeconds(context.Background(), shared.PathId("slam")); !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
		t.Fatalf("SLAM: err = %v, want ErrMeasuredRateUnavailable", err)
	}
}

func TestConsumer_UnattributedTask_EmptyAssociateId_StillUpdatesMean(t *testing.T) {
	// A robot-station task keys on the empty AssociateId (ADR 0013's
	// partition-key rule); this consumer never reads AssociateId at
	// all, so it must fold this message into the mean identically to
	// any attributed task.
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeTaskPerformanceRecorded, taskPerformanceData{TaskId: "t1", AssociateId: "", TaskType: "SLAM", ActualSeconds: 15}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	got, err := c.MeanActualSeconds(context.Background(), shared.PathId("slam"))
	if err != nil {
		t.Fatalf("expected MeanActualSeconds to succeed, got: %v", err)
	}
	if got != 15 {
		t.Fatalf("mean = %v, want 15", got)
	}
}
