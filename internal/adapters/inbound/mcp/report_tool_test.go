package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	inboundmcp "github.com/claudioed/workforce-management/internal/adapters/inbound/mcp"
)

// fakeReportsClient is a test double for the reports REST client the MCP tool
// delegates to.
type fakeReportsClient struct {
	report    inboundmcp.LaborReportView
	freshness inboundmcp.FreshnessView
	err       error
	lastQuery inboundmcp.LaborQuery
}

func (f *fakeReportsClient) GetLabor(_ context.Context, q inboundmcp.LaborQuery) (inboundmcp.LaborReportView, error) {
	f.lastQuery = q
	return f.report, f.err
}

func (f *fakeReportsClient) GetFreshness(_ context.Context) (inboundmcp.FreshnessView, error) {
	return f.freshness, f.err
}

func TestReportTool_ForwardsFiltersAndReturnsRows(t *testing.T) {
	client := &fakeReportsClient{
		report: inboundmcp.LaborReportView{
			Rows: []inboundmcp.LaborRowView{
				{PathId: "pack", HourBucket: "2026-06-01T10:00:00Z", LaborAssigned: 3, LaborReassigned: 1},
			},
		},
	}

	out, err := inboundmcp.GetLaborReportForTest(context.Background(), client, inboundmcp.LaborToolInput{
		From:        "2026-06-01T00:00:00Z",
		To:          "2026-06-02T00:00:00Z",
		PathId:      "pack",
		Granularity: "hour",
	})
	if err != nil {
		t.Fatalf("tool: %v", err)
	}

	if client.lastQuery.From != "2026-06-01T00:00:00Z" || client.lastQuery.PathId != "pack" {
		t.Errorf("filters not forwarded: %+v", client.lastQuery)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(out.Rows))
	}
	if out.Rows[0].LaborAssigned != 3 || out.Rows[0].PathId != "pack" {
		t.Errorf("row = %+v", out.Rows[0])
	}
}

func TestReportTool_RequiresFromTo(t *testing.T) {
	client := &fakeReportsClient{}
	tests := []inboundmcp.LaborToolInput{
		{To: "2026-06-02T00:00:00Z"},
		{From: "2026-06-01T00:00:00Z"},
	}
	for _, in := range tests {
		if _, err := inboundmcp.GetLaborReportForTest(context.Background(), client, in); err == nil {
			t.Errorf("expected error for missing from/to, input=%+v", in)
		}
	}
}

// TestReportsRESTClient_CallsEndpoints verifies the real HTTP client hits the
// expected reports paths and decodes the responses.
func TestReportsRESTClient_CallsEndpoints(t *testing.T) {
	var gotPath, gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/reports/labor":
			_ = json.NewEncoder(w).Encode(inboundmcp.LaborReportView{
				Rows: []inboundmcp.LaborRowView{{PathId: "pack", LaborAssigned: 7}},
			})
		case "/reports/labor/freshness":
			_ = json.NewEncoder(w).Encode(inboundmcp.FreshnessView{LagSeconds: 12})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := inboundmcp.NewReportsRESTClient(ts.URL, ts.Client())

	rep, err := c.GetLabor(context.Background(), inboundmcp.LaborQuery{
		From: "2026-06-01T00:00:00Z", To: "2026-06-02T00:00:00Z", PathId: "pack", Granularity: "hour",
	})
	if err != nil {
		t.Fatalf("GetLabor: %v", err)
	}
	if gotPath != "/reports/labor" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery == "" {
		t.Error("expected query string with filters")
	}
	if len(rep.Rows) != 1 || rep.Rows[0].LaborAssigned != 7 {
		t.Errorf("report = %+v", rep)
	}

	fr, err := c.GetFreshness(context.Background())
	if err != nil {
		t.Fatalf("GetFreshness: %v", err)
	}
	if fr.LagSeconds != 12 {
		t.Errorf("lag = %v, want 12", fr.LagSeconds)
	}
}

func TestReportsRESTClient_Non2xxIsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := inboundmcp.NewReportsRESTClient(ts.URL, ts.Client())
	if _, err := c.GetLabor(context.Background(), inboundmcp.LaborQuery{From: "a", To: "b"}); err == nil {
		t.Error("expected error on 500 response")
	}
}
