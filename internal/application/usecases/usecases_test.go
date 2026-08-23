package usecases

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/claudioed/workforce-management/internal/adapters/outbound/events"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/memory"
	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/assignment"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// fixedClock lets tests advance time deterministically.
type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type fixtures struct {
	associates  *memory.AssociateRepo
	shiftPlans  *memory.ShiftPlanRepo
	assignments *memory.AssignmentRepo
	pub         *events.LogPublisher
	clock       *fixedClock
}

func newFixtures() *fixtures {
	return &fixtures{
		associates:  memory.NewAssociateRepo(),
		shiftPlans:  memory.NewShiftPlanRepo(),
		assignments: memory.NewAssignmentRepo(),
		pub:         events.NewLogPublisher(slog.New(slog.NewTextHandler(io.Discard, nil))),
		clock:       &fixedClock{now: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)},
	}
}

// errBoom is a sentinel error injected by the failing* fakes below to
// exercise use-case error-handling branches the in-memory adapters can
// never trigger on their own: they never return an error from Save, and
// only ever return ports.ErrNotFound from a lookup.

var errBoom = errors.New("boom")

// failingAssociateRepo wraps a real *memory.AssociateRepo so tests can force
// Save or FindByID to fail while otherwise delegating to the real repo.
type failingAssociateRepo struct {
	*memory.AssociateRepo
	saveErr error
	findErr error
}

func (r *failingAssociateRepo) Save(ctx context.Context, a *associate.AssociateShift) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.AssociateRepo.Save(ctx, a)
}

func (r *failingAssociateRepo) FindByID(ctx context.Context, id shared.AssociateId) (*associate.AssociateShift, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.AssociateRepo.FindByID(ctx, id)
}

// failingShiftPlanRepo wraps a real *memory.ShiftPlanRepo so tests can force
// Save or FindByBuildingAndShift to fail while otherwise delegating to the
// real repo.
type failingShiftPlanRepo struct {
	*memory.ShiftPlanRepo
	saveErr error
	findErr error
}

func (r *failingShiftPlanRepo) Save(ctx context.Context, sp *shiftplan.ShiftPlan) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.ShiftPlanRepo.Save(ctx, sp)
}

func (r *failingShiftPlanRepo) FindByBuildingAndShift(ctx context.Context, buildingId, shiftId string) (*shiftplan.ShiftPlan, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.ShiftPlanRepo.FindByBuildingAndShift(ctx, buildingId, shiftId)
}

// failingAssignmentRepo wraps a real *memory.AssignmentRepo so tests can
// force Save, FindByAssociateID, or CountActiveByPath to fail while
// otherwise delegating to the real repo.
type failingAssignmentRepo struct {
	*memory.AssignmentRepo
	saveErr  error
	findErr  error
	countErr error
}

func (r *failingAssignmentRepo) Save(ctx context.Context, la *assignment.LaborAssignment) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.AssignmentRepo.Save(ctx, la)
}

func (r *failingAssignmentRepo) FindByAssociateID(ctx context.Context, id shared.AssociateId) (*assignment.LaborAssignment, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.AssignmentRepo.FindByAssociateID(ctx, id)
}

func (r *failingAssignmentRepo) CountActiveByPath(ctx context.Context, pathId shared.PathId) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.AssignmentRepo.CountActiveByPath(ctx, pathId)
}

// failingPublisher wraps a real *events.LogPublisher so tests can force
// Publish to fail while otherwise delegating to the real publisher.
type failingPublisher struct {
	*events.LogPublisher
	err error
}

func (p *failingPublisher) Publish(ctx context.Context, evts ...shared.DomainEvent) error {
	if p.err != nil {
		return p.err
	}
	return p.LogPublisher.Publish(ctx, evts...)
}

func TestStartAssociateShift_PersistsAndPublishes(t *testing.T) {
	f := newFixtures()
	uc := &StartAssociateShift{Associates: f.associates, Events: f.pub, Clock: f.clock}

	shift, err := uc.Execute(context.Background(), "assoc-1", []shared.Certification{"pack"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shift.AssociateId() != "assoc-1" {
		t.Fatalf("unexpected associate id: %v", shift.AssociateId())
	}

	stored, err := f.associates.FindByID(context.Background(), "assoc-1")
	if err != nil {
		t.Fatalf("expected associate to be persisted: %v", err)
	}
	if !stored.HasCertification("pack") {
		t.Fatal("expected persisted associate to hold certification")
	}

	found := false
	for _, e := range f.pub.Events() {
		if e.EventName() == "AssociateShiftStarted" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected AssociateShiftStarted to be published")
	}
}

func TestCertifyAssociate_AddsCertification(t *testing.T) {
	f := newFixtures()
	start := &StartAssociateShift{Associates: f.associates, Events: f.pub, Clock: f.clock}
	if _, err := start.Execute(context.Background(), "assoc-1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	certify := &CertifyAssociate{Associates: f.associates, Events: f.pub, Clock: f.clock}
	if err := certify.Execute(context.Background(), "assoc-1", "hazmat"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, _ := f.associates.FindByID(context.Background(), "assoc-1")
	if !stored.HasCertification("hazmat") {
		t.Fatal("expected certification to persist")
	}
}

func TestCertifyAssociate_NotFound(t *testing.T) {
	f := newFixtures()
	certify := &CertifyAssociate{Associates: f.associates, Events: f.pub, Clock: f.clock}
	err := certify.Execute(context.Background(), "ghost", "hazmat")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProposePathPlan_ComputesCeil(t *testing.T) {
	f := newFixtures()
	uc := &ProposePathPlan{Events: f.pub, Clock: f.clock}
	heads, err := uc.Execute(context.Background(), "bldg-1", "pack", 100, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if heads != 4 {
		t.Fatalf("expected 4 heads, got %d", heads)
	}
}

func TestCommitShiftPlan_PersistsAndPublishes(t *testing.T) {
	f := newFixtures()
	uc := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}

	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40}}
	installed := map[shared.PathId]int{"pack": 10}

	sp, err := uc.Execute(context.Background(), "bldg-1", "shift-1", lines, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sp.PlannedHeadsFor("pack") != 5 {
		t.Fatalf("expected 5 heads, got %d", sp.PlannedHeadsFor("pack"))
	}

	stored, err := f.shiftPlans.FindByBuildingAndShift(context.Background(), "bldg-1", "shift-1")
	if err != nil {
		t.Fatalf("expected plan to be persisted: %v", err)
	}
	if stored.PlannedHeadsFor("pack") != 5 {
		t.Fatalf("expected persisted plan to have 5 heads, got %d", stored.PlannedHeadsFor("pack"))
	}
}

// TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations is a
// Definition-of-Done named failing-path test at the application layer.
func TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations(t *testing.T) {
	f := newFixtures()
	uc := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}

	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 11, PlannedRate: 30, PlannedHours: 40}}
	installed := map[shared.PathId]int{"pack": 10}

	_, err := uc.Execute(context.Background(), "bldg-1", "shift-1", lines, installed)
	if !errors.Is(err, shiftplan.ErrPlannedHeadsExceedInstalled) {
		t.Fatalf("expected ErrPlannedHeadsExceedInstalled, got %v", err)
	}
}

func setupCertifiedAssociate(t *testing.T, f *fixtures, id shared.AssociateId, certs ...shared.Certification) {
	t.Helper()
	start := &StartAssociateShift{Associates: f.associates, Events: f.pub, Clock: f.clock}
	if _, err := start.Execute(context.Background(), id, certs); err != nil {
		t.Fatalf("unexpected error setting up associate: %v", err)
	}
}

func TestAssignLabor_Succeeds(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")

	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	la, err := uc.Execute(context.Background(), "assoc-1", "pack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pathId, active := la.ActivePathId()
	if !active || pathId != "pack" {
		t.Fatalf("expected active assignment to pack, got %v active=%v", pathId, active)
	}

	count, err := f.assignments.CountActiveByPath(context.Background(), "pack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 active assignment on pack, got %d", count)
	}
}

// TestAssignLabor_RejectsMissingCertification is a Definition-of-Done named
// failing-path test at the application layer.
func TestAssignLabor_RejectsMissingCertification(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")

	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "assoc-1", "hazmat")
	if !errors.Is(err, assignment.ErrCertificationRequired) {
		t.Fatalf("expected ErrCertificationRequired, got %v", err)
	}
}

// TestAssignLabor_RejectsWhileOnBreak is a Definition-of-Done named
// failing-path test at the application layer.
func TestAssignLabor_RejectsWhileOnBreak(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")

	startBreak := &StartBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}
	if err := startBreak.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "assoc-1", "pack")
	if !errors.Is(err, associate.ErrOnBreak) {
		t.Fatalf("expected ErrOnBreak, got %v", err)
	}
}

func TestAssignLabor_SecondAssignmentEndsPriorAndLogsHours(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack", "stow")

	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := uc.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(3 * time.Hour)
	la, err := uc.Execute(context.Background(), "assoc-1", "stow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pathId, active := la.ActivePathId()
	if !active || pathId != "stow" {
		t.Fatalf("expected single active assignment to stow, got %v active=%v", pathId, active)
	}

	countPack, _ := f.assignments.CountActiveByPath(context.Background(), "pack")
	if countPack != 0 {
		t.Fatalf("expected 0 active assignments on pack after reassignment, got %d", countPack)
	}

	stored, _ := f.associates.FindByID(context.Background(), "assoc-1")
	if stored.HoursLogged() != 3 {
		t.Fatalf("expected 3 hours logged from closed pack interval, got %v", stored.HoursLogged())
	}
}

func TestStartBreak_And_EndBreak(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")

	startBreak := &StartBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}
	endBreak := &EndBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}

	if err := startBreak.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, _ := f.associates.FindByID(context.Background(), "assoc-1")
	if !stored.IsOnBreak() {
		t.Fatal("expected associate to be on break")
	}

	if err := endBreak.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, _ = f.associates.FindByID(context.Background(), "assoc-1")
	if stored.IsOnBreak() {
		t.Fatal("expected associate to no longer be on break")
	}
}

func TestGetStaffingGap_RaisesPathUnderstaffed(t *testing.T) {
	f := newFixtures()
	commit := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 3, PlannedRate: 30, PlannedHours: 24}}
	if _, err := commit.Execute(context.Background(), "bldg-1", "shift-1", lines, map[shared.PathId]int{"pack": 5}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uc := &GetStaffingGap{ShiftPlans: f.shiftPlans, Assignments: f.assignments, Events: f.pub, Clock: f.clock}
	gap, err := uc.Execute(context.Background(), "bldg-1", "shift-1", "pack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gap.Understaffed || gap.PlannedHeads != 3 || gap.ActiveHeads != 0 {
		t.Fatalf("unexpected gap: %+v", gap)
	}

	found := false
	for _, e := range f.pub.Events() {
		if e.EventName() == "PathUnderstaffed" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected PathUnderstaffed to be published")
	}
}

func TestEndAssociateShift_ClosesActiveAssignmentAndShift(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")

	assign := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := assign.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(5 * time.Hour)
	end := &EndAssociateShift{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if err := end.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, _ := f.associates.FindByID(context.Background(), "assoc-1")
	if !stored.Ended() {
		t.Fatal("expected shift to be ended")
	}
	if stored.HoursLogged() != 5 {
		t.Fatalf("expected 5 hours logged, got %v", stored.HoursLogged())
	}

	count, _ := f.assignments.CountActiveByPath(context.Background(), "pack")
	if count != 0 {
		t.Fatalf("expected 0 active assignments after shift end, got %d", count)
	}
}

func TestStartAssociateShift_SaveError(t *testing.T) {
	f := newFixtures()
	repo := &failingAssociateRepo{AssociateRepo: f.associates, saveErr: errBoom}
	uc := &StartAssociateShift{Associates: repo, Events: f.pub, Clock: f.clock}
	_, err := uc.Execute(context.Background(), "assoc-1", nil)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestStartAssociateShift_PublishError(t *testing.T) {
	f := newFixtures()
	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &StartAssociateShift{Associates: f.associates, Events: pub, Clock: f.clock}
	_, err := uc.Execute(context.Background(), "assoc-1", nil)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestCertifyAssociate_SaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")
	repo := &failingAssociateRepo{AssociateRepo: f.associates, saveErr: errBoom}
	uc := &CertifyAssociate{Associates: repo, Events: f.pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "assoc-1", "hazmat")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestCertifyAssociate_PublishError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")
	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &CertifyAssociate{Associates: f.associates, Events: pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "assoc-1", "hazmat")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestProposePathPlan_PublishError(t *testing.T) {
	f := newFixtures()
	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &ProposePathPlan{Events: pub, Clock: f.clock}
	_, err := uc.Execute(context.Background(), "bldg-1", "pack", 100, 30)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestCommitShiftPlan_SaveError(t *testing.T) {
	f := newFixtures()
	repo := &failingShiftPlanRepo{ShiftPlanRepo: f.shiftPlans, saveErr: errBoom}
	uc := &CommitShiftPlan{ShiftPlans: repo, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}

	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40}}
	installed := map[shared.PathId]int{"pack": 10}

	_, err := uc.Execute(context.Background(), "bldg-1", "shift-1", lines, installed)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestCommitShiftPlan_PublishError(t *testing.T) {
	f := newFixtures()
	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: pub, Clock: f.clock, MaxHoursPerShift: 8}

	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40}}
	installed := map[shared.PathId]int{"pack": 10}

	_, err := uc.Execute(context.Background(), "bldg-1", "shift-1", lines, installed)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestAssignLabor_AssociateNotFound(t *testing.T) {
	f := newFixtures()
	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "ghost", "pack")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAssignLabor_FindAssignmentError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")
	repo := &failingAssignmentRepo{AssignmentRepo: f.assignments, findErr: errBoom}
	uc := &AssignLabor{Associates: f.associates, Assignments: repo, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "assoc-1", "pack")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

// TestAssignLabor_LogHoursExceedsMax exercises shift.LogHours failing from
// inside AssignLabor.Execute (a reassignment closing a too-long interval) —
// distinct from the domain-level LogHours tests in associate_shift_test.go.
func TestAssignLabor_LogHoursExceedsMax(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack", "stow")

	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 2}
	if _, err := uc.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(3 * time.Hour)
	_, err := uc.Execute(context.Background(), "assoc-1", "stow")
	if !errors.Is(err, associate.ErrMaxHoursExceeded) {
		t.Fatalf("expected ErrMaxHoursExceeded, got %v", err)
	}
}

func TestAssignLabor_AssociatesSaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack", "stow")

	first := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := first.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(1 * time.Hour)
	repo := &failingAssociateRepo{AssociateRepo: f.associates, saveErr: errBoom}
	uc := &AssignLabor{Associates: repo, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "assoc-1", "stow")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestAssignLabor_AssignmentsSaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")
	repo := &failingAssignmentRepo{AssignmentRepo: f.assignments, saveErr: errBoom}
	uc := &AssignLabor{Associates: f.associates, Assignments: repo, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "assoc-1", "pack")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestAssignLabor_PublishError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")
	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: pub, Clock: f.clock, MaxHoursPerShift: 8}
	_, err := uc.Execute(context.Background(), "assoc-1", "pack")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestStartBreak_NotFound(t *testing.T) {
	f := newFixtures()
	uc := &StartBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "ghost")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStartBreak_SaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")
	repo := &failingAssociateRepo{AssociateRepo: f.associates, saveErr: errBoom}
	uc := &StartBreak{Associates: repo, Events: f.pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestStartBreak_PublishError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")
	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &StartBreak{Associates: f.associates, Events: pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestEndBreak_NotFound(t *testing.T) {
	f := newFixtures()
	uc := &EndBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "ghost")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEndBreak_SaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")
	startBreak := &StartBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}
	if err := startBreak.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo := &failingAssociateRepo{AssociateRepo: f.associates, saveErr: errBoom}
	uc := &EndBreak{Associates: repo, Events: f.pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestEndBreak_PublishError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")
	startBreak := &StartBreak{Associates: f.associates, Events: f.pub, Clock: f.clock}
	if err := startBreak.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &EndBreak{Associates: f.associates, Events: pub, Clock: f.clock}
	err := uc.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestGetStaffingGap_PlanNotFound(t *testing.T) {
	f := newFixtures()
	uc := &GetStaffingGap{ShiftPlans: f.shiftPlans, Assignments: f.assignments, Events: f.pub, Clock: f.clock}
	_, err := uc.Execute(context.Background(), "bldg-1", "shift-1", "pack")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetStaffingGap_CountActiveError(t *testing.T) {
	f := newFixtures()
	commit := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 3, PlannedRate: 30, PlannedHours: 24}}
	if _, err := commit.Execute(context.Background(), "bldg-1", "shift-1", lines, map[shared.PathId]int{"pack": 5}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo := &failingAssignmentRepo{AssignmentRepo: f.assignments, countErr: errBoom}
	uc := &GetStaffingGap{ShiftPlans: f.shiftPlans, Assignments: repo, Events: f.pub, Clock: f.clock}
	_, err := uc.Execute(context.Background(), "bldg-1", "shift-1", "pack")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestGetStaffingGap_PublishErrorWhenUnderstaffed(t *testing.T) {
	f := newFixtures()
	commit := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 3, PlannedRate: 30, PlannedHours: 24}}
	if _, err := commit.Execute(context.Background(), "bldg-1", "shift-1", lines, map[shared.PathId]int{"pack": 5}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	uc := &GetStaffingGap{ShiftPlans: f.shiftPlans, Assignments: f.assignments, Events: pub, Clock: f.clock}
	_, err := uc.Execute(context.Background(), "bldg-1", "shift-1", "pack")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestGetStaffingGap_NotUnderstaffed(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")
	assign := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := assign.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commit := &CommitShiftPlan{ShiftPlans: f.shiftPlans, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 1, PlannedRate: 30, PlannedHours: 8}}
	if _, err := commit.Execute(context.Background(), "bldg-1", "shift-1", lines, map[shared.PathId]int{"pack": 5}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uc := &GetStaffingGap{ShiftPlans: f.shiftPlans, Assignments: f.assignments, Events: f.pub, Clock: f.clock}
	gap, err := uc.Execute(context.Background(), "bldg-1", "shift-1", "pack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gap.Understaffed {
		t.Fatalf("expected not understaffed, got %+v", gap)
	}

	for _, e := range f.pub.Events() {
		if e.EventName() == "PathUnderstaffed" {
			t.Fatal("did not expect PathUnderstaffed to be published")
		}
	}
}

func TestEndAssociateShift_AssociateNotFound(t *testing.T) {
	f := newFixtures()
	uc := &EndAssociateShift{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	err := uc.Execute(context.Background(), "ghost")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEndAssociateShift_FindAssignmentError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")
	repo := &failingAssignmentRepo{AssignmentRepo: f.assignments, findErr: errBoom}
	uc := &EndAssociateShift{Associates: f.associates, Assignments: repo, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	err := uc.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

// TestEndAssociateShift_LogHoursExceedsMax exercises shift.LogHours failing
// from inside EndAssociateShift.Execute when closing the active assignment.
func TestEndAssociateShift_LogHoursExceedsMax(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")

	assign := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := assign.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(3 * time.Hour)
	end := &EndAssociateShift{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 2}
	err := end.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, associate.ErrMaxHoursExceeded) {
		t.Fatalf("expected ErrMaxHoursExceeded, got %v", err)
	}
}

func TestEndAssociateShift_AssignmentsSaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")

	assign := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := assign.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(1 * time.Hour)
	repo := &failingAssignmentRepo{AssignmentRepo: f.assignments, saveErr: errBoom}
	end := &EndAssociateShift{Associates: f.associates, Assignments: repo, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	err := end.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestEndAssociateShift_AssociatesSaveError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1", "pack")

	assign := &AssignLabor{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if _, err := assign.Execute(context.Background(), "assoc-1", "pack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.clock.now = f.clock.now.Add(1 * time.Hour)
	repo := &failingAssociateRepo{AssociateRepo: f.associates, saveErr: errBoom}
	end := &EndAssociateShift{Associates: repo, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	err := end.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestEndAssociateShift_PublishError(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")

	pub := &failingPublisher{LogPublisher: f.pub, err: errBoom}
	end := &EndAssociateShift{Associates: f.associates, Assignments: f.assignments, Events: pub, Clock: f.clock, MaxHoursPerShift: 8}
	err := end.Execute(context.Background(), "assoc-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

// TestEndAssociateShift_NoActiveAssignment covers an associate who was
// started but never assigned to a path: Assignments.FindByAssociateID
// returns ports.ErrNotFound, la stays nil, and the shift still ends cleanly.
func TestEndAssociateShift_NoActiveAssignment(t *testing.T) {
	f := newFixtures()
	setupCertifiedAssociate(t, f, "assoc-1")

	end := &EndAssociateShift{Associates: f.associates, Assignments: f.assignments, Events: f.pub, Clock: f.clock, MaxHoursPerShift: 8}
	if err := end.Execute(context.Background(), "assoc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, _ := f.associates.FindByID(context.Background(), "assoc-1")
	if !stored.Ended() {
		t.Fatal("expected shift to be ended")
	}
}
