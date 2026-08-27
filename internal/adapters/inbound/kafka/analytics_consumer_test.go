package kafka_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	inboundkafka "github.com/claudioed/workforce-management/internal/adapters/inbound/kafka"
)

// call captures one projection-store method invocation.
type call struct {
	method  string
	eventId string
	id      string // associateId or pathId, depending on the event
	at      time.Time
}

// fakeProjection records the calls the consumer makes so a test can assert the
// envelope was routed to the right method with the right fields.
type fakeProjection struct {
	calls []call
}

func (f *fakeProjection) ApplyShiftStarted(_ context.Context, eventId, associateId string, at time.Time) error {
	f.calls = append(f.calls, call{"shiftStarted", eventId, associateId, at})
	return nil
}
func (f *fakeProjection) ApplyShiftEnded(_ context.Context, eventId, associateId string, at time.Time) error {
	f.calls = append(f.calls, call{"shiftEnded", eventId, associateId, at})
	return nil
}
func (f *fakeProjection) ApplyBreakStarted(_ context.Context, eventId, associateId string, at time.Time) error {
	f.calls = append(f.calls, call{"breakStarted", eventId, associateId, at})
	return nil
}
func (f *fakeProjection) ApplyBreakEnded(_ context.Context, eventId, associateId string, at time.Time) error {
	f.calls = append(f.calls, call{"breakEnded", eventId, associateId, at})
	return nil
}
func (f *fakeProjection) ApplyCertified(_ context.Context, eventId, associateId string, at time.Time) error {
	f.calls = append(f.calls, call{"certified", eventId, associateId, at})
	return nil
}
func (f *fakeProjection) ApplyLaborAssigned(_ context.Context, eventId, pathId string, at time.Time) error {
	f.calls = append(f.calls, call{"laborAssigned", eventId, pathId, at})
	return nil
}
func (f *fakeProjection) ApplyLaborReassigned(_ context.Context, eventId, toPathId string, at time.Time) error {
	f.calls = append(f.calls, call{"laborReassigned", eventId, toPathId, at})
	return nil
}
func (f *fakeProjection) ApplyPathUnderstaffed(_ context.Context, eventId, pathId string, at time.Time) error {
	f.calls = append(f.calls, call{"pathUnderstaffed", eventId, pathId, at})
	return nil
}

// fakeProcessed is an in-memory ports.ProcessedEvents.
type fakeProcessed struct {
	seen map[string]bool
}

func newFakeProcessed() *fakeProcessed { return &fakeProcessed{seen: map[string]bool{}} }

func (p *fakeProcessed) MarkProcessed(_ context.Context, eventId string) (bool, error) {
	if p.seen[eventId] {
		return false, nil
	}
	p.seen[eventId] = true
	return true, nil
}

func envelope(t *testing.T, eventId, eventType string, at time.Time, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	env := map[string]any{
		"event_id":       eventId,
		"event_type":     eventType,
		"occurred_at":    at.Format(time.RFC3339Nano),
		"source":         "workforce-management",
		"schema_version": 1,
		"data":           json.RawMessage(raw),
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

func TestAnalyticsConsumer_RoutesEachEventType(t *testing.T) {
	at := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		eventType  string
		data       map[string]any
		wantMethod string
		wantId     string
	}{
		{"shiftStarted", "AssociateShiftStarted", map[string]any{"associate_id": "a1"}, "shiftStarted", "a1"},
		{"shiftEnded", "AssociateShiftEnded", map[string]any{"associate_id": "a1"}, "shiftEnded", "a1"},
		{"breakStarted", "AssociateBreakStarted", map[string]any{"associate_id": "a1"}, "breakStarted", "a1"},
		{"breakEnded", "AssociateBreakEnded", map[string]any{"associate_id": "a1"}, "breakEnded", "a1"},
		{"certified", "AssociateCertified", map[string]any{"associate_id": "a1", "certification": "hazmat"}, "certified", "a1"},
		{"laborAssigned", "LaborAssigned", map[string]any{"associate_id": "a1", "path_id": "pack"}, "laborAssigned", "pack"},
		{"laborReassigned", "LaborReassigned", map[string]any{"associate_id": "a1", "from_path_id": "pick", "to_path_id": "pack"}, "laborReassigned", "pack"},
		{"pathUnderstaffed", "PathUnderstaffed", map[string]any{"path_id": "pack", "planned_heads": 5, "active_heads": 3}, "pathUnderstaffed", "pack"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj := &fakeProjection{}
			processed := newFakeProcessed()
			c := &inboundkafka.AnalyticsConsumer{Projection: proj, Processed: processed, Logger: slog.Default()}

			raw := envelope(t, "e-"+tt.name, tt.eventType, at, tt.data)
			if err := c.HandleMessage(context.Background(), raw); err != nil {
				t.Fatalf("HandleMessage: %v", err)
			}
			if len(proj.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(proj.calls))
			}
			got := proj.calls[0]
			if got.method != tt.wantMethod {
				t.Errorf("method = %q, want %q", got.method, tt.wantMethod)
			}
			if got.id != tt.wantId {
				t.Errorf("id = %q, want %q", got.id, tt.wantId)
			}
			if !got.at.Equal(at) {
				t.Errorf("at = %v, want %v", got.at, at)
			}
		})
	}
}

func TestAnalyticsConsumer_Idempotent(t *testing.T) {
	at := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	proj := &fakeProjection{}
	processed := newFakeProcessed()
	c := &inboundkafka.AnalyticsConsumer{Projection: proj, Processed: processed, Logger: slog.Default()}

	raw := envelope(t, "dup", "LaborAssigned", at, map[string]any{"associate_id": "a1", "path_id": "pack"})
	for range 2 {
		if err := c.HandleMessage(context.Background(), raw); err != nil {
			t.Fatalf("HandleMessage: %v", err)
		}
	}
	if len(proj.calls) != 1 {
		t.Fatalf("expected 1 apply for duplicate delivery, got %d", len(proj.calls))
	}
}

func TestAnalyticsConsumer_IgnoresNonProjectingEventType(t *testing.T) {
	proj := &fakeProjection{}
	processed := newFakeProcessed()
	c := &inboundkafka.AnalyticsConsumer{Projection: proj, Processed: processed, Logger: slog.Default()}

	for _, et := range []string{"ShiftPlanProposed", "ShiftPlanCommitted", "SomethingUnknown"} {
		raw := envelope(t, "e-"+et, et, time.Now(), map[string]any{"path_id": "pack"})
		if err := c.HandleMessage(context.Background(), raw); err != nil {
			t.Fatalf("HandleMessage(%s): %v", et, err)
		}
	}
	if len(proj.calls) != 0 {
		t.Fatalf("expected non-projecting events to make no call, got %d", len(proj.calls))
	}
	// A non-projecting event must NOT be marked processed, so a later contract
	// change could reprocess it.
	if processed.seen["e-ShiftPlanCommitted"] {
		t.Error("non-projecting event should not be marked processed")
	}
}
