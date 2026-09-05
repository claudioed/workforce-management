package laborperformance_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/laborperformance"
	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

type captured struct {
	method string
	path   string
}

func newServer(t *testing.T, status int, responseBody string, got *captured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if responseBody != "" {
			_, _ = w.Write([]byte(responseBody))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMeanActualSecondsCallsPublishedContract(t *testing.T) {
	var got captured
	srv := newServer(t, http.StatusOK, `{"taskType":"PACK","taskCount":10,"meanEfficiencyPct":92.5,"meanActualSeconds":47.3}`, &got)

	client := laborperformance.NewClient(srv.URL+"/", nil)
	seconds, err := client.MeanActualSeconds(context.Background(), shared.PathId("pack"))
	if err != nil {
		t.Fatalf("MeanActualSeconds: %v", err)
	}
	if seconds != 47.3 {
		t.Fatalf("seconds = %v, want 47.3", seconds)
	}
	if got.method != http.MethodGet || got.path != "/task-types/PACK/performance" {
		t.Fatalf("called %s %s, want GET /task-types/PACK/performance", got.method, got.path)
	}
}

func TestMeanActualSecondsUppercasesPathId(t *testing.T) {
	tests := []struct {
		pathId       string
		wantTaskType string
	}{
		{"pick", "PICK"},
		{"PACK", "PACK"},
		{"slam", "SLAM"},
	}
	for _, tt := range tests {
		t.Run(tt.pathId, func(t *testing.T) {
			var got captured
			srv := newServer(t, http.StatusOK, `{"meanActualSeconds":10}`, &got)
			client := laborperformance.NewClient(srv.URL, nil)
			if _, err := client.MeanActualSeconds(context.Background(), shared.PathId(tt.pathId)); err != nil {
				t.Fatalf("MeanActualSeconds: %v", err)
			}
			if got.path != "/task-types/"+tt.wantTaskType+"/performance" {
				t.Fatalf("path = %q, want /task-types/%s/performance", got.path, tt.wantTaskType)
			}
		})
	}
}

// TestMeanActualSecondsRejectsPathsWithNoTaskType is the key mapping test:
// paths with no labor-performance TaskType counterpart (e.g. "stow",
// "hazmat") must fail fast with ErrMeasuredRateUnavailable and make NO
// HTTP call at all.
func TestMeanActualSecondsRejectsPathsWithNoTaskType(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := laborperformance.NewClient(srv.URL, nil)
	for _, pathId := range []string{"stow", "hazmat", "unknown-path"} {
		_, err := client.MeanActualSeconds(context.Background(), shared.PathId(pathId))
		if !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
			t.Fatalf("path %q: err = %v, want ErrMeasuredRateUnavailable", pathId, err)
		}
	}
	if called {
		t.Fatal("expected no HTTP call for a path with no TaskType counterpart")
	}
}

func TestMeanActualSecondsMapsFailureModesToErrMeasuredRateUnavailable(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		responseBody string
	}{
		{name: "non-200 status", status: http.StatusInternalServerError},
		{name: "malformed body", status: http.StatusOK, responseBody: "not json"},
		{name: "null meanActualSeconds means no data yet", status: http.StatusOK, responseBody: `{"taskType":"PACK","taskCount":0,"meanEfficiencyPct":null,"meanActualSeconds":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got captured
			srv := newServer(t, tt.status, tt.responseBody, &got)
			client := laborperformance.NewClient(srv.URL, nil)
			_, err := client.MeanActualSeconds(context.Background(), shared.PathId("pack"))
			if !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
				t.Fatalf("err = %v, want ErrMeasuredRateUnavailable", err)
			}
		})
	}
}

type errDoer struct{ err error }

func (d errDoer) Do(*http.Request) (*http.Response, error) { return nil, d.err }

func TestMeanActualSecondsMapsTransportFailure(t *testing.T) {
	boom := errors.New("connection refused")
	client := laborperformance.NewClient("http://example.invalid", errDoer{err: boom})

	_, err := client.MeanActualSeconds(context.Background(), shared.PathId("pack"))
	if !errors.Is(err, ports.ErrMeasuredRateUnavailable) {
		t.Fatalf("err = %v, want ErrMeasuredRateUnavailable", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to also wrap the transport error", err)
	}
}

func TestNewClientDefaultsItsDoer(t *testing.T) {
	if laborperformance.NewClient("http://example.invalid", nil) == nil {
		t.Fatal("NewClient returned nil")
	}
}
