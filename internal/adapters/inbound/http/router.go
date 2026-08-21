package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/claudioed/workforce-management/internal/application/usecases"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// Handler wires the Workforce Management use cases to chi routes.
type Handler struct {
	StartAssociateShift *usecases.StartAssociateShift
	CertifyAssociate    *usecases.CertifyAssociate
	ProposePathPlan     *usecases.ProposePathPlan
	CommitShiftPlan     *usecases.CommitShiftPlan
	AssignLabor         *usecases.AssignLabor
	StartBreak          *usecases.StartBreak
	EndBreak            *usecases.EndBreak
	GetStaffingGap      *usecases.GetStaffingGap
	EndAssociateShift   *usecases.EndAssociateShift
}

// NewRouter builds the chi router for the Workforce Management REST API.
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", h.healthz)

	r.Post("/associates/{id}/start-shift", h.startShift)
	r.Post("/associates/{id}/certifications", h.certify)
	r.Post("/paths/{pathId}/plan/propose", h.proposePathPlan)
	r.Post("/shift-plans", h.commitShiftPlan)
	r.Post("/associates/{id}/assignments", h.assignLabor)
	r.Post("/associates/{id}/break/start", h.startBreak)
	r.Post("/associates/{id}/break/end", h.endBreak)
	r.Get("/paths/{pathId}/staffing-gap", h.staffingGap)
	r.Post("/associates/{id}/end-shift", h.endShift)

	return r
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) startShift(w http.ResponseWriter, r *http.Request) {
	associateId := chi.URLParam(r, "id")

	var req startShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	certs := make([]shared.Certification, len(req.Certifications))
	for i, c := range req.Certifications {
		certs[i] = shared.Certification(c)
	}

	shift, err := h.StartAssociateShift.Execute(r.Context(), shared.AssociateId(associateId), certs)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"associateId": string(shift.AssociateId()),
	})
}

func (h *Handler) certify(w http.ResponseWriter, r *http.Request) {
	associateId := chi.URLParam(r, "id")

	var req certifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if err := h.CertifyAssociate.Execute(r.Context(), shared.AssociateId(associateId), shared.Certification(req.Certification)); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) proposePathPlan(w http.ResponseWriter, r *http.Request) {
	pathId := chi.URLParam(r, "pathId")

	var req proposePathPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	heads, err := h.ProposePathPlan.Execute(r.Context(), req.BuildingId, shared.PathId(pathId), req.Charge, req.PlannedRate)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, proposePathPlanResponse{PathId: pathId, ProposedHeads: heads})
}

func (h *Handler) commitShiftPlan(w http.ResponseWriter, r *http.Request) {
	var req commitShiftPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	lines := make([]shiftplan.PathPlan, len(req.Lines))
	installed := make(map[shared.PathId]int, len(req.Lines))
	for i, l := range req.Lines {
		lines[i] = shiftplan.PathPlan{
			PathId:       shared.PathId(l.PathId),
			PlannedHeads: l.PlannedHeads,
			PlannedRate:  l.PlannedRate,
			PlannedHours: l.PlannedHours,
		}
		installed[shared.PathId(l.PathId)] = l.InstalledStations
	}

	sp, err := h.CommitShiftPlan.Execute(r.Context(), req.BuildingId, req.ShiftId, lines, installed)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}

	respLines := make([]pathPlanLineResp, 0, len(sp.Lines()))
	for _, l := range sp.Lines() {
		respLines = append(respLines, pathPlanLineResp{
			PathId:       string(l.PathId),
			PlannedHeads: l.PlannedHeads,
			PlannedRate:  l.PlannedRate,
			PlannedHours: l.PlannedHours,
		})
	}
	writeJSON(w, http.StatusCreated, shiftPlanResponse{
		BuildingId: sp.BuildingId(),
		ShiftId:    sp.ShiftId(),
		Lines:      respLines,
	})
}

func (h *Handler) assignLabor(w http.ResponseWriter, r *http.Request) {
	associateId := chi.URLParam(r, "id")

	var req assignLaborRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	la, err := h.AssignLabor.Execute(r.Context(), shared.AssociateId(associateId), shared.PathId(req.PathId))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}

	pathId, active := la.ActivePathId()
	writeJSON(w, http.StatusCreated, assignmentResponse{
		AssociateId:  string(la.AssociateId()),
		ActivePathId: string(pathId),
		Active:       active,
	})
}

func (h *Handler) startBreak(w http.ResponseWriter, r *http.Request) {
	associateId := chi.URLParam(r, "id")
	if err := h.StartBreak.Execute(r.Context(), shared.AssociateId(associateId)); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) endBreak(w http.ResponseWriter, r *http.Request) {
	associateId := chi.URLParam(r, "id")
	if err := h.EndBreak.Execute(r.Context(), shared.AssociateId(associateId)); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// staffingGap answers GET /paths/{pathId}/staffing-gap?buildingId=&shiftId=.
// ShiftPlan is keyed by building+shift, so this context requires those as
// query parameters alongside the path in the URL.
func (h *Handler) staffingGap(w http.ResponseWriter, r *http.Request) {
	pathId := chi.URLParam(r, "pathId")
	buildingId := r.URL.Query().Get("buildingId")
	shiftId := r.URL.Query().Get("shiftId")

	gap, err := h.GetStaffingGap.Execute(r.Context(), buildingId, shiftId, shared.PathId(pathId))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffingGapResponse(gap))
}

func (h *Handler) endShift(w http.ResponseWriter, r *http.Request) {
	associateId := chi.URLParam(r, "id")
	if err := h.EndAssociateShift.Execute(r.Context(), shared.AssociateId(associateId)); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{Error: err.Error()})
}
