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
