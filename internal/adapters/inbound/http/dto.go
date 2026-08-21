// Package http implements the inbound chi-based REST adapter: DTOs, routing,
// and domain-error-to-HTTP-status mapping. Domain and application structs
// never leak across this boundary.
package http

import "github.com/claudioed/workforce-management/internal/application/usecases"

type startShiftRequest struct {
	Certifications []string `json:"certifications"`
}

type certifyRequest struct {
	Certification string `json:"certification"`
}

type proposePathPlanRequest struct {
	BuildingId  string  `json:"buildingId"`
	Charge      float64 `json:"charge"`
	PlannedRate float64 `json:"plannedRate"`
}

type proposePathPlanResponse struct {
	PathId        string `json:"pathId"`
	ProposedHeads int    `json:"proposedHeads"`
}

type pathPlanLineRequest struct {
	PathId            string  `json:"pathId"`
	PlannedHeads      int     `json:"plannedHeads"`
	PlannedRate       float64 `json:"plannedRate"`
	PlannedHours      float64 `json:"plannedHours"`
	InstalledStations int     `json:"installedStations"`
}

type commitShiftPlanRequest struct {
	BuildingId string                `json:"buildingId"`
	ShiftId    string                `json:"shiftId"`
	Lines      []pathPlanLineRequest `json:"lines"`
}

type shiftPlanResponse struct {
	BuildingId string             `json:"buildingId"`
	ShiftId    string             `json:"shiftId"`
	Lines      []pathPlanLineResp `json:"lines"`
}

type pathPlanLineResp struct {
	PathId       string  `json:"pathId"`
	PlannedHeads int     `json:"plannedHeads"`
	PlannedRate  float64 `json:"plannedRate"`
	PlannedHours float64 `json:"plannedHours"`
}

type assignLaborRequest struct {
	PathId string `json:"pathId"`
}

type assignmentResponse struct {
	AssociateId  string `json:"associateId"`
	ActivePathId string `json:"activePathId,omitempty"`
	Active       bool   `json:"active"`
}

type staffingGapResponse struct {
	PathId       string `json:"pathId"`
	PlannedHeads int    `json:"plannedHeads"`
	ActiveHeads  int    `json:"activeHeads"`
	Understaffed bool   `json:"understaffed"`
}

func toStaffingGapResponse(g usecases.StaffingGap) staffingGapResponse {
	return staffingGapResponse{
		PathId:       string(g.PathId),
		PlannedHeads: g.PlannedHeads,
		ActiveHeads:  g.ActiveHeads,
		Understaffed: g.Understaffed,
	}
}

type errorResponse struct {
	Error string `json:"error"`
}
