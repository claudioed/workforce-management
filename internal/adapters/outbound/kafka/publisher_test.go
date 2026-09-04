package kafka

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	segmentio "github.com/segmentio/kafka-go"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/memory"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// fakeWriter records WriteMessages calls instead of hitting a broker.
type fakeWriter struct {
	mu   sync.Mutex
	msgs []segmentio.Message
}

func (f *fakeWriter) WriteMessages(ctx context.Context, msgs ...segmentio.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msgs...)
	return nil
}

func newTestPublisher(t *testing.T, sp *shiftplan.ShiftPlan) (*Publisher, *fakeWriter) {
	t.Helper()
	repo := memory.NewShiftPlanRepo()
	if err := repo.Save(context.Background(), sp); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	fw := &fakeWriter{}
	return &Publisher{writer: fw, shiftPlans: repo}, fw
}

func mustCommitPlan(t *testing.T, lines []shiftplan.PathPlan, installed map[shared.PathId]int) *shiftplan.ShiftPlan {
	t.Helper()
	sp, err := shiftplan.CommitShiftPlan("BLD1", "SHIFT1", lines, installed, installed, 8.0, time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("commit shift plan: %v", err)
	}
	return sp
}

func TestPublish_FansOutOneMessagePerPathPlanLine(t *testing.T) {
	lines := []shiftplan.PathPlan{
		{PathId: "pack", PlannedHeads: 3, PlannedRate: 50, PlannedHours: 24},
		{PathId: "pick", PlannedHeads: 2, PlannedRate: 40, PlannedHours: 16},
		{PathId: "stow", PlannedHeads: 1, PlannedRate: 30, PlannedHours: 8},
	}
	installed := map[shared.PathId]int{"pack": 5, "pick": 5, "stow": 5}
	sp := mustCommitPlan(t, lines, installed)
	events := sp.PullEvents()

	pub, fw := newTestPublisher(t, sp)
	if err := pub.Publish(context.Background(), events...); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(fw.msgs) != len(lines) {
		t.Fatalf("expected %d messages (one per path plan line), got %d", len(lines), len(fw.msgs))
	}

	gotPaths := make(map[string]bool)
	for _, msg := range fw.msgs {
		var env envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.EventType != "ShiftPlanCommitted" {
			t.Errorf("event_type = %q, want ShiftPlanCommitted", env.EventType)
		}
		if env.Source != "workforce-management" {
			t.Errorf("source = %q, want workforce-management", env.Source)
		}
		if env.EventID == "" {
			t.Error("event_id must not be empty")
		}
		if env.OccurredAt != "2026-08-21T22:00:00Z" {
			t.Errorf("occurred_at = %q, want 2026-08-21T22:00:00Z", env.OccurredAt)
		}
		if env.Data.BuildingId != "BLD1" {
			t.Errorf("data.building_id = %q, want BLD1", env.Data.BuildingId)
		}
		if env.Data.ShiftId != "SHIFT1" {
			t.Errorf("data.shift_id = %q, want SHIFT1", env.Data.ShiftId)
		}
		gotPaths[env.Data.PathId] = true
	}
	for _, line := range lines {
		if !gotPaths[string(line.PathId)] {
			t.Errorf("missing message for path %q", line.PathId)
		}
	}
}

func TestPublish_EnvelopeCarriesPathPlanValues(t *testing.T) {
	lines := []shiftplan.PathPlan{
		{PathId: "pack", PlannedHeads: 3, PlannedRate: 50, PlannedHours: 24},
	}
	installed := map[shared.PathId]int{"pack": 5}
	sp := mustCommitPlan(t, lines, installed)
	events := sp.PullEvents()

	pub, fw := newTestPublisher(t, sp)
	if err := pub.Publish(context.Background(), events...); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(fw.msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(fw.msgs))
	}

	var env envelope
	if err := json.Unmarshal(fw.msgs[0].Value, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	want := envelopeData{
		BuildingId:   "BLD1",
		ShiftId:      "SHIFT1",
		PathId:       "pack",
		PlannedHeads: 3,
		PlannedRate:  50,
		PlannedHours: 24,
	}
	if env.Data != want {
		t.Errorf("data = %+v, want %+v", env.Data, want)
	}
}

func TestPublish_IgnoresNonShiftPlanCommittedEvents(t *testing.T) {
	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 1, PlannedRate: 10, PlannedHours: 8}}
	installed := map[shared.PathId]int{"pack": 5}
	sp := mustCommitPlan(t, lines, installed)
	sp.PullEvents()

	pub, fw := newTestPublisher(t, sp)
	other := shared.NewAssociateShiftStarted(time.Now(), "A1", nil)
	if err := pub.Publish(context.Background(), other); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(fw.msgs) != 0 {
		t.Fatalf("expected 0 messages for non-ShiftPlanCommitted event, got %d", len(fw.msgs))
	}
}

func TestPublish_NoEvents_NoWrite(t *testing.T) {
	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 1, PlannedRate: 10, PlannedHours: 8}}
	installed := map[shared.PathId]int{"pack": 5}
	sp := mustCommitPlan(t, lines, installed)
	sp.PullEvents()

	pub, fw := newTestPublisher(t, sp)
	if err := pub.Publish(context.Background()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(fw.msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(fw.msgs))
	}
}
