package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/events"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/memory"
	"github.com/claudioed/workforce-management/internal/application/usecases"
)

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
	router := NewRouter(newTestHandler())
	rec := doRequest(t, router, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStartShift(t *testing.T) {
	router := NewRouter(newTestHandler())
	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{"pack"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCertify(t *testing.T) {
	router := NewRouter(newTestHandler())
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/certifications", certifyRequest{Certification: "hazmat"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCertify_NotFound(t *testing.T) {
	router := NewRouter(newTestHandler())
	rec := doRequest(t, router, http.MethodPost, "/associates/ghost/certifications", certifyRequest{Certification: "hazmat"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProposePathPlan(t *testing.T) {
	router := NewRouter(newTestHandler())
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
}

func TestCommitShiftPlan(t *testing.T) {
	router := NewRouter(newTestHandler())
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
}

// TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations is a
// Definition-of-Done named failing-path test at the HTTP layer.
func TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations(t *testing.T) {
	router := NewRouter(newTestHandler())
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
}

func TestAssignLabor(t *testing.T) {
	router := NewRouter(newTestHandler())
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{"pack"}})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/assignments", assignLaborRequest{PathId: "pack"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAssignLabor_RejectsMissingCertification is a Definition-of-Done named
// failing-path test at the HTTP layer.
func TestAssignLabor_RejectsMissingCertification(t *testing.T) {
	router := NewRouter(newTestHandler())
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/assignments", assignLaborRequest{PathId: "hazmat"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAssignLabor_RejectsWhileOnBreak is a Definition-of-Done named
// failing-path test at the HTTP layer.
func TestAssignLabor_RejectsWhileOnBreak(t *testing.T) {
	router := NewRouter(newTestHandler())
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{Certifications: []string{"pack"}})
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/break/start", nil)

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/assignments", assignLaborRequest{PathId: "pack"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartAndEndBreak(t *testing.T) {
	router := NewRouter(newTestHandler())
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
	router := NewRouter(newTestHandler())
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
	router := NewRouter(newTestHandler())
	doRequest(t, router, http.MethodPost, "/associates/assoc-1/start-shift", startShiftRequest{})

	rec := doRequest(t, router, http.MethodPost, "/associates/assoc-1/end-shift", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}
