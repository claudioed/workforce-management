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

// proposePathPlanRequest's plannedRate is OPTIONAL: omit it (or send <= 0)
// to let ProposePathPlan fall back to a measured rate fed back from
// labor-performance when one is available for this path (ADR-0012).
type proposePathPlanRequest struct {
	BuildingId  string  `json:"buildingId"`
	Charge      float64 `json:"charge"`
	PlannedRate float64 `json:"plannedRate,omitempty"`
}

// proposePathPlanResponse's RateSource is one of usecases.RateSourceCaller
// or usecases.RateSourceMeasured, so a human can see WHERE the rate that
// produced ProposedHeads came from -- and isn't left guessing why heads
// came back 0 when no caller rate and no measured rate were available.
type proposePathPlanResponse struct {
	PathId        string  `json:"pathId"`
	ProposedHeads int     `json:"proposedHeads"`
	ResolvedRate  float64 `json:"resolvedRate"`
	RateSource    string  `json:"rateSource"`
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

// problemDetails is the RFC 7807 (Problem Details for HTTP APIs) response
// body used for every error response in this service.
type problemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}
