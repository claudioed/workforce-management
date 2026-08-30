package http

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/events"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/memory"
	"github.com/claudioed/workforce-management/internal/application/usecases"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

func newTestHandler() *Handler {
	associates := memory.NewAssociateRepo()
	shiftPlans := memory.NewShiftPlanRepo()
	assignments := memory.NewAssignmentRepo()
	pub := events.NewLogPublisher(nil)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)}
	const maxHoursPerShift = 8.0

	return &Handler{
		StartAssociateShift: &usecases.StartAssociateShift{Associates: associates, Events: pub, Clock: clock},
		CertifyAssociate:    &usecases.CertifyAssociate{Associates: associates, Events: pub, Clock: clock},
		ProposePathPlan:     &usecases.ProposePathPlan{Events: pub, Clock: clock},
		CommitShiftPlan:     &usecases.CommitShiftPlan{ShiftPlans: shiftPlans, Events: pub, Clock: clock, MaxHoursPerShift: maxHoursPerShift},
		AssignLabor:         &usecases.AssignLabor{Associates: associates, Assignments: assignments, Events: pub, Clock: clock, MaxHoursPerShift: maxHoursPerShift},
		StartBreak:          &usecases.StartBreak{Associates: associates, Events: pub, Clock: clock},
		EndBreak:            &usecases.EndBreak{Associates: associates, Events: pub, Clock: clock},
		GetStaffingGap:      &usecases.GetStaffingGap{ShiftPlans: shiftPlans, Assignments: assignments, Events: pub, Clock: clock},
		EndAssociateShift:   &usecases.EndAssociateShift{Associates: associates, Assignments: assignments, Events: pub, Clock: clock, MaxHoursPerShift: maxHoursPerShift},
	}
}

func doRequest(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	rec := doRequest(t, router, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStartShift(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{"pack"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/associates/assoc-1" {
		t.Fatalf("expected Location /associates/assoc-1, got %q", loc)
	}
}

// TestStartShift_RejectsEmptyCertification is an HTTP-layer validation test:
// the decoded DTO is validated (via shared.NewCertification) before the use
// case runs, so an empty certification string is rejected as 400 RFC 7807
// rather than reaching the domain/use-case layer.
func TestStartShift_RejectsEmptyCertification(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{""}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusBadRequest, "empty-certification", "/associates/assoc-1/start-shift")
}

func TestCertify(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/certifications", certifyRequest{Certification: "hazmat"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCertify_NotFound(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	rec := doRequest(t, router, http.MethodPost, "/associates/ghost/certifications", certifyRequest{Certification: "hazmat"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusNotFound, "resource-not-found", "/associates/ghost/certifications")
}

func TestProposePathPlan(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	rec := doRequest(t, router, http.MethodPost, "/paths/pack/plan/propose", proposePathPlanRequest{BuildingId: "bldg-1", Charge: 100, PlannedRate: 30})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp proposePathPlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ProposedHeads != 4 {
		t.Fatalf("expected 4 proposed heads, got %d", resp.ProposedHeads)
	}
	if resp.RateSource != "caller" {
		t.Fatalf("expected rateSource caller, got %q", resp.RateSource)
	}
	if resp.ResolvedRate != 30 {
		t.Fatalf("expected resolvedRate 30, got %v", resp.ResolvedRate)
	}
}

// TestProposePathPlan_OmittedRateFallsBackToZeroHeadsWithoutMeasuredRate
// covers the wire-level contract when plannedRate is omitted and no
// MeasuredRateClient is wired (newTestHandler leaves it nil): the request
// must still succeed with 0 proposed heads, never a 4xx/5xx.
func TestProposePathPlan_OmittedRateFallsBackToZeroHeadsWithoutMeasuredRate(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	rec := doRequest(t, router, http.MethodPost, "/paths/pack/plan/propose", proposePathPlanRequest{BuildingId: "bldg-1", Charge: 100})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp proposePathPlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ProposedHeads != 0 {
		t.Fatalf("expected 0 proposed heads with no rate available, got %d", resp.ProposedHeads)
	}
}

func TestCommitShiftPlan(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	req := commitShiftPlanRequest{
		BuildingId: "bldg-1",
		ShiftId:    "shift-1",
		Lines: []pathPlanLineRequest{
			{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40, InstalledStations: 10},
		},
	}
	rec := doRequest(t, router, http.MethodPost, "/shift-plans", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/shift-plans/bldg-1/shift-1" {
		t.Fatalf("expected Location /shift-plans/bldg-1/shift-1, got %q", loc)
	}
}

// TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations is a
// Definition-of-Done named failing-path test at the HTTP layer.
func TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	req := commitShiftPlanRequest{
		BuildingId: "bldg-1",
		ShiftId:    "shift-1",
		Lines: []pathPlanLineRequest{
			{PathId: "pack", PlannedHeads: 11, PlannedRate: 30, PlannedHours: 40, InstalledStations: 10},
		},
	}
	rec := doRequest(t, router, http.MethodPost, "/shift-plans", req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusConflict, "planned-heads-exceed-installed", "/shift-plans")
}

// TestCommitShiftPlan_RejectsMissingBuildingId is an HTTP-layer validation
// test: buildingId/shiftId have no domain value object (ShiftPlan stores
// them as plain strings), so the adapter itself must reject an empty
// buildingId as 400 before calling the use case.
func TestCommitShiftPlan_RejectsMissingBuildingId(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	req := commitShiftPlanRequest{
		ShiftId: "shift-1",
		Lines: []pathPlanLineRequest{
			{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40, InstalledStations: 10},
		},
	}
	rec := doRequest(t, router, http.MethodPost, "/shift-plans", req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusBadRequest, "missing-building-id", "/shift-plans")
}

func TestAssignLabor(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{"pack"}})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/assignments", assignLaborRequest{PathId: "pack"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/associates/assoc-1/assignments" {
		t.Fatalf("expected Location /associates/assoc-1/assignments, got %q", loc)
	}
}

// TestAssignLabor_RejectsMissingCertification is a Definition-of-Done named
// failing-path test at the HTTP layer.
func TestAssignLabor_RejectsMissingCertification(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/assignments", assignLaborRequest{PathId: "hazmat"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusConflict, "certification-required", "/associates/assoc-1/assignments")
}

// TestAssignLabor_RejectsWhileOnBreak is a Definition-of-Done named
// failing-path test at the HTTP layer.
func TestAssignLabor_RejectsWhileOnBreak(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{"pack"}})
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/break/start", nil)

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/assignments", assignLaborRequest{PathId: "pack"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartAndEndBreak(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/break/start", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, router, http.MethodPost, "/associates/assoc-1/break/end", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStaffingGap(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	req := commitShiftPlanRequest{
		BuildingId: "bldg-1",
		ShiftId:    "shift-1",
		Lines: []pathPlanLineRequest{
			{PathId: "pack", PlannedHeads: 3, PlannedRate: 30, PlannedHours: 24, InstalledStations: 10},
		},
	}
	doRequest(t, router, http.MethodPost, "/shift-plans", req)

	rec := doRequest(t, router, http.MethodGet, "/paths/pack/staffing-gap?buildingId=bldg-1&shiftId=shift-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp staffingGapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Understaffed || resp.PlannedHeads != 3 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestEndShift(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/end-shift", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMalformedRequestBody is an HTTP-layer validation test covering the
// generic (non-domain-sentinel) RFC 7807 category: malformed JSON falls
// back to the status-keyed "malformed-request-body" category.
func TestMalformedRequestBody(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "")
	req := httptest.NewRequest(http.MethodPost, "/associates/assoc-1/start-shift", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusBadRequest, "malformed-request-body", "/associates/assoc-1/start-shift")
}

// assertProblemDetails asserts rec's body is a well-formed RFC 7807 problem
// document (Content-Type: application/problem+json) matching status and
// wantSlug (the last path segment of "type"), with instance == wantInstance.
func assertProblemDetails(t *testing.T, rec *httptest.ResponseRecorder, status int, wantSlug, wantInstance string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected Content-Type application/problem+json, got %q", ct)
	}
	var pd problemDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &pd); err != nil {
		t.Fatalf("unmarshal problem details: %v", err)
	}
	wantType := problemErrorsURIBase + "/" + wantSlug
	if pd.Type != wantType {
		t.Fatalf("expected type %q, got %q", wantType, pd.Type)
	}
	if pd.Status != status {
		t.Fatalf("expected status %d, got %d", status, pd.Status)
	}
	if pd.Title == "" {
		t.Fatalf("expected non-empty title")
	}
	if pd.Detail == "" {
		t.Fatalf("expected non-empty detail")
	}
	if pd.Instance != wantInstance {
		t.Fatalf("expected instance %q, got %q", wantInstance, pd.Instance)
	}
}
