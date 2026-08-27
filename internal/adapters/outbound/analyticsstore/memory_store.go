// Package analyticsstore provides the outbound adapters that persist and serve
// the workforce Labor Utilization & Staffing read model: an in-memory
// implementation (MemoryStore) for tests and local runs, and Postgres
// implementations (a writer projection and a read-only reader) for deployment.
// All satisfy the report.ProjectionStore and/or report.ReportStore ports.
package analyticsstore

import (
	"context"
	"sync"
	"time"

	"github.com/claudioed/workforce-management/internal/analytics/report"
)

// MemoryStore is an in-memory implementation of both report.ProjectionStore
// (write) and report.ReportStore (read), backed by maps. It is idempotent per
// eventId via a seen-set, so a duplicate delivery is a no-op. It is safe for
// concurrent use.
type MemoryStore struct {
	// Now supplies the current time for FreshnessLag; defaults to time.Now
	// when nil so lag is deterministic under test.
	Now func() time.Time

	mu     sync.Mutex
	seen   map[string]struct{}
	rows   map[report.RowKey]*rowAcc
	breaks map[string]time.Time // associateId -> break start
	// latest is the OccurredAt of the most recently applied event, used to
	// compute FreshnessLag.
	latest time.Time
}

// rowAcc accumulates the running totals for one report row. AvgBreak is
// derived from the two break-duration fields at query time.
type rowAcc struct {
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

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		seen:   map[string]struct{}{},
		rows:   map[report.RowKey]*rowAcc{},
		breaks: map[string]time.Time{},
	}
}

func hourBucket(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

// firstApply marks eventId as seen and reports whether this is the first time
// (so the caller should apply the effect) or a duplicate (skip). It also
// advances the freshness watermark. The caller must hold s.mu.
func (s *MemoryStore) firstApply(eventId string, at time.Time) bool {
	if _, dup := s.seen[eventId]; dup {
		return false
	}
	s.seen[eventId] = struct{}{}
	if at.After(s.latest) {
		s.latest = at
	}
	return true
}

func (s *MemoryStore) row(k report.RowKey) *rowAcc {
	r, ok := s.rows[k]
	if !ok {
		r = &rowAcc{}
		s.rows[k] = r
	}
	return r
}

// associateRow returns the building-wide (empty PathId) accumulator for at's
// hour bucket, where associate-scoped metrics land.
func (s *MemoryStore) associateRow(at time.Time) *rowAcc {
	return s.row(report.RowKey{PathId: "", HourBucket: hourBucket(at)})
}

// ApplyShiftStarted increments the shift-start counter. Idempotent on eventId.
func (s *MemoryStore) ApplyShiftStarted(_ context.Context, eventId, _ string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.associateRow(at).shiftsStarted++
	return nil
}

// ApplyShiftEnded increments the shift-end counter. Idempotent on eventId.
func (s *MemoryStore) ApplyShiftEnded(_ context.Context, eventId, _ string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.associateRow(at).shiftsEnded++
	return nil
}

// ApplyBreakStarted increments the break count and records the break start so
// a later end can compute duration. Idempotent on eventId.
func (s *MemoryStore) ApplyBreakStarted(_ context.Context, eventId, associateId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.associateRow(at).breaks++
	s.breaks[associateId] = at
	return nil
}

// ApplyBreakEnded folds the paired start-to-end break duration into the bucket
// of the break's start, when the start is known. Idempotent on eventId.
func (s *MemoryStore) ApplyBreakEnded(_ context.Context, eventId, associateId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
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

// ApplyCertified increments the certification counter. Idempotent on eventId.
func (s *MemoryStore) ApplyCertified(_ context.Context, eventId, _ string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.associateRow(at).certifications++
	return nil
}

// ApplyLaborAssigned increments the labor-assigned counter for pathId.
// Idempotent on eventId.
func (s *MemoryStore) ApplyLaborAssigned(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).laborAssigned++
	return nil
}

// ApplyLaborReassigned increments the labor-reassigned counter for toPathId.
// Idempotent on eventId.
func (s *MemoryStore) ApplyLaborReassigned(_ context.Context, eventId, toPathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: toPathId, HourBucket: hourBucket(at)}).laborReassigned++
	return nil
}

// ApplyPathUnderstaffed increments the understaffing-event counter for pathId.
// Idempotent on eventId.
func (s *MemoryStore) ApplyPathUnderstaffed(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).understaffingEvents++
	return nil
}

// Query returns the rows matching q. From is inclusive, To is exclusive, both
// compared against a row's HourBucket; empty PathId means no filter on that
// dimension.
func (s *MemoryStore) Query(_ context.Context, q report.ReportQuery) (report.LaborReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// FreshnessLag returns how far the read model lags real time: now minus the
// OccurredAt of the most recently applied event. Zero when nothing has been
// applied yet, and never negative (a future-dated event clamps to zero).
func (s *MemoryStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.latest.IsZero() {
		return 0, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	lag := now.Sub(s.latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}
