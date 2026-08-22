package shiftplan

import (
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/domain/shared"
)

func TestProposedHeads_RoundsUp(t *testing.T) {
	got := ProposedHeads(100, 30)
	if got != 4 {
		t.Fatalf("expected 4 heads (ceil(100/30)), got %d", got)
	}
}

func TestProposedHeads_ZeroRateYieldsZero(t *testing.T) {
	if got := ProposedHeads(100, 0); got != 0 {
		t.Fatalf("expected 0 heads for zero rate, got %d", got)
	}
}

func TestCommitShiftPlan_Succeeds(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40},
	}
	installed := map[shared.PathId]int{"pack": 10}

	sp, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	events := sp.PullEvents()
	if len(events) != 1 || events[0].EventName() != "ShiftPlanCommitted" {
		t.Fatalf("expected ShiftPlanCommitted event, got %+v", events)
	}
	if sp.PlannedHeadsFor("pack") != 5 {
		t.Fatalf("expected 5 planned heads for pack, got %d", sp.PlannedHeadsFor("pack"))
	}
}

// TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations is a
// Definition-of-Done named failing-path test: plannedHeads > installedStations
// must be rejected on ShiftPlan commit.
func TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 11, PlannedRate: 30, PlannedHours: 40},
	}
	installed := map[shared.PathId]int{"pack": 10}

	_, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != ErrPlannedHeadsExceedInstalled {
		t.Fatalf("expected ErrPlannedHeadsExceedInstalled, got %v", err)
	}
}

// TestCommitShiftPlan_AllowsPlannedHeadsExactlyEqualToInstalledStations is a
// boundary test: the invariant is plannedHeads <= installedStations, so the
// exact-equal case must be ACCEPTED, not rejected.
func TestCommitShiftPlan_AllowsPlannedHeadsExactlyEqualToInstalledStations(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 10, PlannedRate: 30, PlannedHours: 80},
	}
	installed := map[shared.PathId]int{"pack": 10}

	sp, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != nil {
		t.Fatalf("expected plannedHeads == installedStations to be accepted, got error: %v", err)
	}
	if sp.PlannedHeadsFor("pack") != 10 {
		t.Fatalf("expected 10 planned heads, got %d", sp.PlannedHeadsFor("pack"))
	}
}

func TestCommitShiftPlan_RejectsPlannedHoursExceedingCapacity(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 2, PlannedRate: 30, PlannedHours: 20},
	}
	installed := map[shared.PathId]int{"pack": 10}

	_, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != ErrPlannedHoursExceedCapacity {
		t.Fatalf("expected ErrPlannedHoursExceedCapacity, got %v", err)
	}
}

func TestCommitShiftPlan_RejectsMissingInstalledStations(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "stow", PlannedHeads: 2, PlannedRate: 30, PlannedHours: 10},
	}
	installed := map[shared.PathId]int{"pack": 10}

	_, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != ErrMissingInstalledStations {
		t.Fatalf("expected ErrMissingInstalledStations, got %v", err)
	}
}

func TestCommitShiftPlan_RejectsEmptyLines(t *testing.T) {
	now := time.Now()
	_, err := CommitShiftPlan("building-1", "shift-1", nil, map[shared.PathId]int{}, 8, now)
	if err != ErrNoPathPlans {
		t.Fatalf("expected ErrNoPathPlans, got %v", err)
	}
}

// TestRehydrate_ReconstructsWithoutRaisingEvents covers Rehydrate,
// BuildingId, and ShiftId: reconstructing a ShiftPlan from persisted state
// must reflect the given identifiers and lines, and must not raise events
// (unlike CommitShiftPlan).
func TestRehydrate_ReconstructsWithoutRaisingEvents(t *testing.T) {
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40},
		{PathId: "stow", PlannedHeads: 3, PlannedRate: 25, PlannedHours: 24},
	}

	sp := Rehydrate("building-1", "shift-1", lines)

	if sp.BuildingId() != "building-1" {
		t.Fatalf("expected BuildingId building-1, got %q", sp.BuildingId())
	}
	if sp.ShiftId() != "shift-1" {
		t.Fatalf("expected ShiftId shift-1, got %q", sp.ShiftId())
	}
	got := sp.Lines()
	if len(got) != len(lines) {
		t.Fatalf("expected %d lines, got %d", len(lines), len(got))
	}
	for i, want := range lines {
		if got[i] != want {
			t.Fatalf("line %d: expected %+v, got %+v", i, want, got[i])
		}
	}
	if events := sp.PullEvents(); len(events) != 0 {
		t.Fatalf("expected Rehydrate to raise no events, got %+v", events)
	}
}

// TestLines_ReturnsCopy covers Lines() returning all committed lines with
// correct fields, and that the returned slice is a copy: mutating it must
// not affect the aggregate's internal state.
func TestLines_ReturnsCopy(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40},
		{PathId: "stow", PlannedHeads: 3, PlannedRate: 25, PlannedHours: 24},
	}
	installed := map[shared.PathId]int{"pack": 10, "stow": 10}

	sp, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sp.Lines()
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got))
	}
	if got[0].PathId != "pack" || got[0].PlannedHeads != 5 || got[0].PlannedRate != 30 || got[0].PlannedHours != 40 {
		t.Fatalf("unexpected line 0: %+v", got[0])
	}
	if got[1].PathId != "stow" || got[1].PlannedHeads != 3 || got[1].PlannedRate != 25 || got[1].PlannedHours != 24 {
		t.Fatalf("unexpected line 1: %+v", got[1])
	}

	// Mutate the returned slice; the aggregate's internal state must be
	// unaffected since Lines() returns a copy.
	got[0].PlannedHeads = 999
	_ = append(got, PathPlan{PathId: "pick", PlannedHeads: 1})

	again := sp.Lines()
	if len(again) != 2 {
		t.Fatalf("expected internal state to still have 2 lines, got %d", len(again))
	}
	if again[0].PlannedHeads != 5 {
		t.Fatalf("expected internal PlannedHeads to remain 5, got %d", again[0].PlannedHeads)
	}
}

// TestPlannedHeadsFor_ReturnsZeroForUnknownPath covers the not-found branch
// of PlannedHeadsFor: a path with no line in the plan yields 0.
func TestPlannedHeadsFor_ReturnsZeroForUnknownPath(t *testing.T) {
	now := time.Now()
	lines := []PathPlan{
		{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40},
	}
	installed := map[shared.PathId]int{"pack": 10}

	sp, err := CommitShiftPlan("building-1", "shift-1", lines, installed, 8, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := sp.PlannedHeadsFor("unknown-path"); got != 0 {
		t.Fatalf("expected 0 planned heads for unknown path, got %d", got)
	}
}
