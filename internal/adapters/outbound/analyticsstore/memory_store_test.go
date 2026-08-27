package analyticsstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/analyticsstore"
	"github.com/claudioed/workforce-management/internal/analytics/report"
)

func TestMemoryStore_BreakDurationIdempotent(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	s := analyticsstore.NewMemoryStore()

	apply := func() {
		if err := s.ApplyBreakStarted(ctx, "break-1", "a1", base); err != nil {
			t.Fatalf("break started: %v", err)
		}
		if err := s.ApplyBreakEnded(ctx, "break-end-1", "a1", base.Add(45*time.Second)); err != nil {
			t.Fatalf("break ended: %v", err)
		}
	}

	// Apply the full sequence twice with the SAME event ids (duplicate
	// delivery): the counters must reflect one logical occurrence.
	apply()
	apply()

	rep, err := s.Query(ctx, report.ReportQuery{
		From:        base.Add(-time.Hour),
		To:          base.Add(time.Hour),
		Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rep.Rows))
	}
	row := rep.Rows[0]
	if row.Breaks != 1 {
		t.Errorf("Breaks = %d, want 1 (idempotent)", row.Breaks)
	}
	if row.AvgBreakSeconds != 45 {
		t.Errorf("AvgBreakSeconds = %v, want 45", row.AvgBreakSeconds)
	}
}

func TestMemoryStore_PathAndAssociateMetrics(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)
	ctx := context.Background()
	s := analyticsstore.NewMemoryStore()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	must(s.ApplyShiftStarted(ctx, "ss1", "a1", base))
	must(s.ApplyShiftEnded(ctx, "se1", "a1", base))
	must(s.ApplyCertified(ctx, "c1", "a1", base))
	must(s.ApplyLaborAssigned(ctx, "la1", "pack", base))
	must(s.ApplyLaborReassigned(ctx, "lr1", "pack", base))
	must(s.ApplyPathUnderstaffed(ctx, "pu1", "pack", base))
	// Duplicate understaffed ignored.
	must(s.ApplyPathUnderstaffed(ctx, "pu1", "pack", base))

	rep, err := s.Query(ctx, report.ReportQuery{
		From:        base.Add(-time.Hour),
		To:          base.Add(time.Hour),
		Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	var assocRow, packRow *report.Row
	for i := range rep.Rows {
		switch rep.Rows[i].Key.PathId {
		case "":
			assocRow = &rep.Rows[i]
		case "pack":
			packRow = &rep.Rows[i]
		}
	}
	if assocRow == nil || assocRow.ShiftsStarted != 1 || assocRow.ShiftsEnded != 1 || assocRow.Certifications != 1 {
		t.Errorf("associate row = %+v", assocRow)
	}
	if packRow == nil || packRow.LaborAssigned != 1 || packRow.LaborReassigned != 1 || packRow.UnderstaffingEvents != 1 {
		t.Errorf("pack row = %+v", packRow)
	}
}

func TestMemoryStore_FreshnessLag(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	s := analyticsstore.NewMemoryStore()
	s.Now = func() time.Time { return now }

	// No events yet: lag is zero.
	lag, err := s.FreshnessLag(ctx)
	if err != nil {
		t.Fatalf("FreshnessLag: %v", err)
	}
	if lag != 0 {
		t.Errorf("empty lag = %v, want 0", lag)
	}

	// An event 10 minutes old makes the lag 10 minutes.
	if err := s.ApplyShiftStarted(ctx, "e", "a1", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	lag, err = s.FreshnessLag(ctx)
	if err != nil {
		t.Fatalf("FreshnessLag: %v", err)
	}
	if lag != 10*time.Minute {
		t.Errorf("lag = %v, want 10m", lag)
	}
}

// Compile-time assertions that MemoryStore satisfies both ports.
var (
	_ report.ProjectionStore = (*analyticsstore.MemoryStore)(nil)
	_ report.ReportStore     = (*analyticsstore.MemoryStore)(nil)
)
