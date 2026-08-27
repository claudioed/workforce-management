package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/analytics/report"
)

// fakeStore is an in-memory implementation of both report ports used to
// exercise report derivation from a synthetic event sequence. It is a test
// double local to this package: the production stores live in the
// analyticsstore outbound adapter.
type fakeStore struct {
	seen   map[string]bool
	rows   map[report.RowKey]*acc
	breaks map[string]time.Time // associateId -> break start
}

// acc is the fake store's per-row accumulator, kept separate from the public
// report.Row so the running-total intermediate state never leaks into the
// read-model type.
type acc struct {
	shiftsStarted       int
	shiftsEnded         int
	breaks              int
	totalBreakSeconds   float64
	breaksWithDuration  int
	certifications      int
	laborAssigned       int
	laborReassigned     int
	understaffingEvents int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		seen:   map[string]bool{},
		rows:   map[report.RowKey]*acc{},
		breaks: map[string]time.Time{},
	}
}

func (s *fakeStore) row(k report.RowKey) *acc {
	r, ok := s.rows[k]
	if !ok {
		r = &acc{}
		s.rows[k] = r
	}
	return r
}

func (s *fakeStore) dup(eventId string) bool {
	if s.seen[eventId] {
		return true
	}
	s.seen[eventId] = true
	return false
}

func hourBucket(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }

func (s *fakeStore) associateRow(at time.Time) *acc {
	return s.row(report.RowKey{PathId: "", HourBucket: hourBucket(at)})
}

func (s *fakeStore) ApplyShiftStarted(_ context.Context, eventId, _ string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.associateRow(at).shiftsStarted++
	return nil
}

func (s *fakeStore) ApplyShiftEnded(_ context.Context, eventId, _ string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.associateRow(at).shiftsEnded++
	return nil
}

func (s *fakeStore) ApplyBreakStarted(_ context.Context, eventId, associateId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.associateRow(at).breaks++
	s.breaks[associateId] = at
	return nil
}

func (s *fakeStore) ApplyBreakEnded(_ context.Context, eventId, associateId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	if started, ok := s.breaks[associateId]; ok {
		r := s.row(report.RowKey{PathId: "", HourBucket: hourBucket(started)})
		r.totalBreakSeconds += at.Sub(started).Seconds()
		r.breaksWithDuration++
		delete(s.breaks, associateId)
	}
	return nil
}

func (s *fakeStore) ApplyCertified(_ context.Context, eventId, _ string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.associateRow(at).certifications++
	return nil
}

func (s *fakeStore) ApplyLaborAssigned(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).laborAssigned++
	return nil
}

func (s *fakeStore) ApplyLaborReassigned(_ context.Context, eventId, toPathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: toPathId, HourBucket: hourBucket(at)}).laborReassigned++
	return nil
}

func (s *fakeStore) ApplyPathUnderstaffed(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).understaffingEvents++
	return nil
}

func (s *fakeStore) Query(_ context.Context, q report.ReportQuery) (report.LaborReport, error) {
	out := report.LaborReport{}
	for k, r := range s.rows {
		if k.HourBucket.Before(q.From) || !k.HourBucket.Before(q.To) {
			continue
		}
		if q.PathId != "" && k.PathId != q.PathId {
			continue
		}
		row := report.Row{
			Key:                 k,
			ShiftsStarted:       r.shiftsStarted,
			ShiftsEnded:         r.shiftsEnded,
			Breaks:              r.breaks,
			Certifications:      r.certifications,
			LaborAssigned:       r.laborAssigned,
			LaborReassigned:     r.laborReassigned,
			UnderstaffingEvents: r.understaffingEvents,
		}
		if r.breaksWithDuration > 0 {
			row.AvgBreakSeconds = r.totalBreakSeconds / float64(r.breaksWithDuration)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func (s *fakeStore) FreshnessLag(_ context.Context) (time.Duration, error) { return 0, nil }

func TestLaborReport_DerivesFromEventSequence(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s := newFakeStore()
	ctx := context.Background()

	// Associate-scoped lifecycle in one hour bucket:
	//  - two shifts start, one ends
	//  - associate a1 takes a 300s break (start then end)
	//  - one certification
	// Path-scoped in the same bucket for path "pack":
	//  - two labor assigned, one reassigned, one understaffed
	must(t, s.ApplyShiftStarted(ctx, "e1", "a1", base))
	must(t, s.ApplyShiftStarted(ctx, "e2", "a2", base))
	must(t, s.ApplyShiftEnded(ctx, "e3", "a2", base.Add(5*time.Minute)))
	must(t, s.ApplyBreakStarted(ctx, "e4", "a1", base.Add(time.Minute)))
	must(t, s.ApplyBreakEnded(ctx, "e5", "a1", base.Add(time.Minute+300*time.Second)))
	must(t, s.ApplyCertified(ctx, "e6", "a1", base.Add(2*time.Minute)))
	must(t, s.ApplyLaborAssigned(ctx, "e7", "pack", base))
	must(t, s.ApplyLaborAssigned(ctx, "e8", "pack", base.Add(time.Minute)))
	must(t, s.ApplyLaborReassigned(ctx, "e9", "pack", base.Add(2*time.Minute)))
	must(t, s.ApplyPathUnderstaffed(ctx, "e10", "pack", base.Add(3*time.Minute)))

	rep, err := s.Query(ctx, report.ReportQuery{
		From:        base.Add(-time.Hour),
		To:          base.Add(2 * time.Hour),
		Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	bucket := base.Truncate(time.Hour)
	assoc := findRow(rep, report.RowKey{PathId: "", HourBucket: bucket})
	if assoc == nil {
		t.Fatal("no associate-scoped row")
	}
	if assoc.ShiftsStarted != 2 {
		t.Errorf("ShiftsStarted = %d, want 2", assoc.ShiftsStarted)
	}
	if assoc.ShiftsEnded != 1 {
		t.Errorf("ShiftsEnded = %d, want 1", assoc.ShiftsEnded)
	}
	if assoc.Breaks != 1 {
		t.Errorf("Breaks = %d, want 1", assoc.Breaks)
	}
	if assoc.AvgBreakSeconds != 300 {
		t.Errorf("AvgBreakSeconds = %v, want 300", assoc.AvgBreakSeconds)
	}
	if assoc.Certifications != 1 {
		t.Errorf("Certifications = %d, want 1", assoc.Certifications)
	}

	pack := findRow(rep, report.RowKey{PathId: "pack", HourBucket: bucket})
	if pack == nil {
		t.Fatal("no pack row")
	}
	if pack.LaborAssigned != 2 {
		t.Errorf("LaborAssigned = %d, want 2", pack.LaborAssigned)
	}
	if pack.LaborReassigned != 1 {
		t.Errorf("LaborReassigned = %d, want 1", pack.LaborReassigned)
	}
	if pack.UnderstaffingEvents != 1 {
		t.Errorf("UnderstaffingEvents = %d, want 1", pack.UnderstaffingEvents)
	}
}

func TestLaborReport_FiltersAndIdempotency(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	tests := []struct {
		name  string
		query report.ReportQuery
		want  int // number of rows expected
	}{
		{"no filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), Granularity: report.GranularityHour}, 2},
		{"path filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), PathId: "pack", Granularity: report.GranularityHour}, 1},
		{"window excludes all", report.ReportQuery{From: base.Add(24 * time.Hour), To: base.Add(48 * time.Hour), Granularity: report.GranularityHour}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newFakeStore()
			// Apply the same assign twice with the same eventId → counts once.
			must(t, s.ApplyLaborAssigned(ctx, "dup", "pack", base))
			must(t, s.ApplyLaborAssigned(ctx, "dup", "pack", base))
			must(t, s.ApplyShiftStarted(ctx, "s1", "a1", base))

			rep, err := s.Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(rep.Rows) != tt.want {
				t.Errorf("rows = %d, want %d", len(rep.Rows), tt.want)
			}
			if tt.name == "no filter" {
				pack := findRow(rep, report.RowKey{PathId: "pack", HourBucket: base.Truncate(time.Hour)})
				if pack == nil || pack.LaborAssigned != 1 {
					t.Errorf("dedupe failed: pack labor assigned = %v", pack)
				}
			}
		})
	}
}

func findRow(rep report.LaborReport, k report.RowKey) *report.Row {
	for i := range rep.Rows {
		if rep.Rows[i].Key == k {
			return &rep.Rows[i]
		}
	}
	return nil
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
}
