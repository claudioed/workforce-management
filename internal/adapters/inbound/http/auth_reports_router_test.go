package http_test

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/adapters/inbound/auth"
	"github.com/claudioed/workforce-management/internal/adapters/inbound/http"
	"github.com/claudioed/workforce-management/internal/analytics/report"
)

func newAuthedReportsServer(mode auth.Mode) stdhttp.Handler {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{
		"reports-read-key": auth.ScopeRead,
		"reports-rw-key":   auth.ScopeReadWrite,
	})
	store := &fakeReportStore{report: report.LaborReport{}, lag: 3 * time.Second}
	return http.NewReportsRouter(&http.ReportsHandlers{Store: store}, nil, "", http.WithAuth(authn, mode))
}

func doReports(t *testing.T, h stdhttp.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The reports reader requires the read scope on every /reports/* route and
// leaves /healthz open (fleet ADR 0005 §2).
func TestReportsRouterAuth_EnforceTable(t *testing.T) {
	h := newAuthedReportsServer(auth.ModeEnforce)
	const labor = "/reports/labor?from=2026-06-01T00:00:00Z&to=2026-06-02T00:00:00Z"

	t.Run("no token -> 401 problem+json with WWW-Authenticate", func(t *testing.T) {
		rec := doReports(t, h, labor, "")
		if rec.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Fatalf("Content-Type = %q, want application/problem+json", ct)
		}
		if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer ") {
			t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", got)
		}
		var p map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if p["type"] != "https://errors.workforce-management.warehouse-systems.dev/unauthenticated" {
			t.Fatalf("type = %v", p["type"])
		}
	})
	t.Run("read key -> 200", func(t *testing.T) {
		rec := doReports(t, h, labor, "reports-read-key")
		if rec.Code != stdhttp.StatusOK {
			t.Fatalf("got %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("rw key -> 200", func(t *testing.T) {
		rec := doReports(t, h, "/reports/labor/freshness", "reports-rw-key")
		if rec.Code != stdhttp.StatusOK {
			t.Fatalf("got %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("freshness without token -> 401", func(t *testing.T) {
		rec := doReports(t, h, "/reports/labor/freshness", "")
		if rec.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rec.Code)
		}
	})
	t.Run("/healthz with no token -> 200", func(t *testing.T) {
		rec := doReports(t, h, "/healthz", "")
		if rec.Code != stdhttp.StatusOK {
			t.Fatalf("got %d, want 200", rec.Code)
		}
	})
}

func TestReportsRouterAuth_OffModeIsANoop(t *testing.T) {
	h := newAuthedReportsServer(auth.ModeOff)
	rec := doReports(t, h, "/reports/labor/freshness", "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("off mode without a token: got %d, want 200", rec.Code)
	}
}
