package shared

import (
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

func TestNewShiftPlanProposed(t *testing.T) {
	e := NewShiftPlanProposed(fixedTime, "bldg-1", PathId("pack"), 5, 42.5)

	if e.EventName() != "ShiftPlanProposed" {
		t.Fatalf("expected EventName ShiftPlanProposed, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.BuildingId != "bldg-1" {
		t.Fatalf("expected BuildingId bldg-1, got %s", e.BuildingId)
	}
	if e.PathId != PathId("pack") {
		t.Fatalf("expected PathId pack, got %s", e.PathId)
	}
	if e.PlannedHeads != 5 {
		t.Fatalf("expected PlannedHeads 5, got %d", e.PlannedHeads)
	}
	if e.PlannedRate != 42.5 {
		t.Fatalf("expected PlannedRate 42.5, got %v", e.PlannedRate)
	}
}

func TestNewShiftPlanCommitted(t *testing.T) {
	e := NewShiftPlanCommitted(fixedTime, "bldg-1", "shift-1")

	if e.EventName() != "ShiftPlanCommitted" {
		t.Fatalf("expected EventName ShiftPlanCommitted, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.BuildingId != "bldg-1" {
		t.Fatalf("expected BuildingId bldg-1, got %s", e.BuildingId)
	}
	if e.ShiftId != "shift-1" {
		t.Fatalf("expected ShiftId shift-1, got %s", e.ShiftId)
	}
}

func TestNewAssociateShiftStarted(t *testing.T) {
	certs := []Certification{"pack", "hazmat"}
	e := NewAssociateShiftStarted(fixedTime, AssociateId("assoc-1"), certs)

	if e.EventName() != "AssociateShiftStarted" {
		t.Fatalf("expected EventName AssociateShiftStarted, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
	if len(e.Certifications) != 2 || e.Certifications[0] != "pack" || e.Certifications[1] != "hazmat" {
		t.Fatalf("expected Certifications [pack hazmat], got %v", e.Certifications)
	}
}

func TestNewAssociateCertified(t *testing.T) {
	e := NewAssociateCertified(fixedTime, AssociateId("assoc-1"), Certification("hazmat"))

	if e.EventName() != "AssociateCertified" {
		t.Fatalf("expected EventName AssociateCertified, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
	if e.Certification != Certification("hazmat") {
		t.Fatalf("expected Certification hazmat, got %s", e.Certification)
	}
}

func TestNewAssociateBreakStarted(t *testing.T) {
	e := NewAssociateBreakStarted(fixedTime, AssociateId("assoc-1"))

	if e.EventName() != "AssociateBreakStarted" {
		t.Fatalf("expected EventName AssociateBreakStarted, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
}

func TestNewAssociateBreakEnded(t *testing.T) {
	e := NewAssociateBreakEnded(fixedTime, AssociateId("assoc-1"))

	if e.EventName() != "AssociateBreakEnded" {
		t.Fatalf("expected EventName AssociateBreakEnded, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
}

func TestNewLaborAssigned(t *testing.T) {
	e := NewLaborAssigned(fixedTime, AssociateId("assoc-1"), PathId("pack"))

	if e.EventName() != "LaborAssigned" {
		t.Fatalf("expected EventName LaborAssigned, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
	if e.PathId != PathId("pack") {
		t.Fatalf("expected PathId pack, got %s", e.PathId)
	}
}

func TestNewLaborReassigned(t *testing.T) {
	e := NewLaborReassigned(fixedTime, AssociateId("assoc-1"), PathId("pack"), PathId("stow"))

	if e.EventName() != "LaborReassigned" {
		t.Fatalf("expected EventName LaborReassigned, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
	if e.FromPathId != PathId("pack") {
		t.Fatalf("expected FromPathId pack, got %s", e.FromPathId)
	}
	if e.ToPathId != PathId("stow") {
		t.Fatalf("expected ToPathId stow, got %s", e.ToPathId)
	}
}

func TestNewPathUnderstaffed(t *testing.T) {
	e := NewPathUnderstaffed(fixedTime, PathId("pack"), 10, 7)

	if e.EventName() != "PathUnderstaffed" {
		t.Fatalf("expected EventName PathUnderstaffed, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.PathId != PathId("pack") {
		t.Fatalf("expected PathId pack, got %s", e.PathId)
	}
	if e.PlannedHeads != 10 {
		t.Fatalf("expected PlannedHeads 10, got %d", e.PlannedHeads)
	}
	if e.ActiveHeads != 7 {
		t.Fatalf("expected ActiveHeads 7, got %d", e.ActiveHeads)
	}
}

func TestNewAssociateShiftEnded(t *testing.T) {
	e := NewAssociateShiftEnded(fixedTime, AssociateId("assoc-1"))

	if e.EventName() != "AssociateShiftEnded" {
		t.Fatalf("expected EventName AssociateShiftEnded, got %s", e.EventName())
	}
	if !e.OccurredAt().Equal(fixedTime) {
		t.Fatalf("expected OccurredAt %v, got %v", fixedTime, e.OccurredAt())
	}
	if e.AssociateId != AssociateId("assoc-1") {
		t.Fatalf("expected AssociateId assoc-1, got %s", e.AssociateId)
	}
}

// TestAllEvents_ImplementDomainEvent ensures every event type satisfies the
// DomainEvent interface, as a compile-time-checked sanity test.
func TestAllEvents_ImplementDomainEvent(t *testing.T) {
	events := []DomainEvent{
		NewShiftPlanProposed(fixedTime, "bldg-1", PathId("pack"), 5, 42.5),
		NewShiftPlanCommitted(fixedTime, "bldg-1", "shift-1"),
		NewAssociateShiftStarted(fixedTime, AssociateId("assoc-1"), nil),
		NewAssociateCertified(fixedTime, AssociateId("assoc-1"), Certification("hazmat")),
		NewAssociateBreakStarted(fixedTime, AssociateId("assoc-1")),
		NewAssociateBreakEnded(fixedTime, AssociateId("assoc-1")),
		NewLaborAssigned(fixedTime, AssociateId("assoc-1"), PathId("pack")),
		NewLaborReassigned(fixedTime, AssociateId("assoc-1"), PathId("pack"), PathId("stow")),
		NewPathUnderstaffed(fixedTime, PathId("pack"), 10, 7),
		NewAssociateShiftEnded(fixedTime, AssociateId("assoc-1")),
	}

	for _, e := range events {
		if e.EventName() == "" {
			t.Fatalf("expected non-empty EventName for %#v", e)
		}
		if !e.OccurredAt().Equal(fixedTime) {
			t.Fatalf("expected OccurredAt %v for %#v, got %v", fixedTime, e, e.OccurredAt())
		}
	}
}
