// Package report holds the workforce Labor Utilization & Staffing read model:
// the shapes of the analytical report the data product serves, the query that
// selects it, and the outbound ports the writer and reader adapters implement.
// It is a read-model region that depends on nothing else in this module — the
// OLTP domain and application layers must not import it, and it must not import
// them (ADR-0010).
package report

import "time"

// Granularity is the time-bucket resolution a report is rolled up to. Only
// hourly buckets are modelled for this round.
type Granularity string

const (
	// GranularityHour rolls rows up into UTC hour buckets.
	GranularityHour Granularity = "hour"
)

// RowKey identifies a single labor row: the process path and the UTC hour
// bucket the row aggregates. HourBucket is the bucket start, truncated to the
// hour in UTC.
//
// Associate-scoped events (shift start/end, breaks, certifications) do not
// carry a PathId — this context deliberately stops at the path boundary and
// never links an associate to a task. Their metrics are aggregated under the
// empty PathId "" (a building-wide "unassigned" bucket) so the report still
// captures workforce utilization without inventing a path attribution the
// domain does not record.
type RowKey struct {
	PathId     string
	HourBucket time.Time
}

// Row is one aggregated Labor Utilization & Staffing row for a
// (pathId, hourBucket) key.
type Row struct {
	Key RowKey
	// ShiftsStarted is the number of AssociateShiftStarted events in this
	// bucket (PathId is "" for these — see RowKey).
	ShiftsStarted int
	// ShiftsEnded is the number of AssociateShiftEnded events in this bucket
	// (PathId is "" for these).
	ShiftsEnded int
	// Breaks is the number of AssociateBreakStarted events in this bucket
	// (PathId is "" for these).
	Breaks int
	// AvgBreakSeconds is the mean elapsed seconds from a break's start to its
	// end, over the breaks in this bucket whose end was observed. Zero when no
	// break in the bucket had a paired end.
	AvgBreakSeconds float64
	// Certifications is the number of AssociateCertified events in this bucket
	// (PathId is "" for these).
	Certifications int
	// LaborAssigned is the number of LaborAssigned events for this path in the
	// bucket.
	LaborAssigned int
	// LaborReassigned is the number of LaborReassigned events into this path in
	// the bucket.
	LaborReassigned int
	// UnderstaffingEvents is the number of PathUnderstaffed events for this
	// path in the bucket.
	UnderstaffingEvents int
}

// LaborReport is the full result of a report query: the matching rows.
type LaborReport struct {
	Rows []Row
}

// ReportQuery selects and filters the rows a report covers. From is inclusive
// and To is exclusive, both compared against a row's HourBucket. PathId is an
// optional exact-match filter (empty means "no filter on this dimension").
type ReportQuery struct {
	From        time.Time
	To          time.Time
	PathId      string
	Granularity Granularity
}
