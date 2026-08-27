package http_test

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/adapters/inbound/http"
	"github.com/claudioed/workforce-management/internal/analytics/report"
)

// fakeReportStore is a test double for report.ReportStore.
type fakeReportStore struct {
	report    report.LaborReport
	lag       time.Duration
	queryErr  error
	freshErr  error
	lastQuery report.ReportQuery
}

func (f *fakeReportStore) Query(_ context.Context, q report.ReportQuery) (report.LaborReport, error) {
	f.lastQuery = q
	return f.report, f.queryErr
}

func (f *fakeReportStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	return f.lag, f.freshErr
}

func newReportsServer(store report.ReportStore) stdhttp.Handler {
	return http.NewReportsRouter(&http.ReportsHandlers{Store: store}, nil, "")
}

func TestReportsLabor_OK(t *testing.T) {
	bucket := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	store := &fakeReportStore{
		report: report.LaborReport{Rows: []report.Row{
			{
				Key:             report.RowKey{PathId: "pack", HourBucket: bucket},
				LaborAssigned:   5,
				LaborReassigned: 1,
			},
			{
				Key:             report.RowKey{PathId: "", HourBucket: bucket},
				ShiftsStarted:   3,
				Breaks:          2,
				AvgBreakSeconds: 300,
				Certifications:  1,
			},
		}},
	}
	srv := newReportsServer(store)

	req := httptest.NewRequest(stdhttp.MethodGet,
		"/reports/labor?from=2026-06-01T00:00:00Z&to=2026-06-02T00:00:00Z&pathId=pack&granularity=hour", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.lastQuery.PathId != "pack" {
		t.Errorf("pathId filter not forwarded: %+v", store.lastQuery)
	}
	if store.lastQuery.Granularity != report.GranularityHour {
		t.Errorf("granularity = %q, want hour", store.lastQuery.Granularity)
	}

	var body struct {
		Rows []struct {
			PathId          string  `json:"pathId"`
			HourBucket      string  `json:"hourBucket"`
			LaborAssigned   int     `json:"laborAssigned"`
			ShiftsStarted   int     `json:"shiftsStarted"`
			AvgBreakSeconds float64 `json:"avgBreakSeconds"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(body.Rows))
	}
	if body.Rows[0].HourBucket != "2026-06-01T10:00:00Z" {
		t.Errorf("hourBucket = %q", body.Rows[0].HourBucket)
	}
}

func TestReportsLabor_MissingFromTo(t *testing.T) {
	srv := newReportsServer(&fakeReportStore{})
	tests := []struct {
		name string
		url  string
	}{
		{"no from", "/reports/labor?to=2026-06-02T00:00:00Z"},
		{"no to", "/reports/labor?from=2026-06-01T00:00:00Z"},
		{"bad from", "/reports/labor?from=nope&to=2026-06-02T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, tt.url, nil))
			if rec.Code != stdhttp.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("content-type = %q, want application/problem+json", ct)
			}
		})
	}
}

func TestReportsLabor_DefaultGranularity(t *testing.T) {
	store := &fakeReportStore{}
	srv := newReportsServer(store)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet,
		"/reports/labor?from=2026-06-01T00:00:00Z&to=2026-06-02T00:00:00Z", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.lastQuery.Granularity != report.GranularityHour {
		t.Errorf("default granularity = %q, want hour", store.lastQuery.Granularity)
	}
}

func TestReportsFreshness_OK(t *testing.T) {
	store := &fakeReportStore{lag: 90 * time.Second}
	srv := newReportsServer(store)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/reports/labor/freshness", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		LagSeconds float64 `json:"lagSeconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.LagSeconds != 90 {
		t.Errorf("lagSeconds = %v, want 90", body.LagSeconds)
	}
}

func TestReportsHealthz(t *testing.T) {
	srv := newReportsServer(&fakeReportStore{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
