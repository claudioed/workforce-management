//go:build integration

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/assignment"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
	"github.com/claudioed/workforce-management/internal/domain/shiftplan"
)

// testPool sets up a migrated pool against DATABASE_URL, or skips the test
// if it is unset. Build with -tags=integration and DATABASE_URL set to run.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping postgres integration test")
	}

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")

	if err := Migrate(databaseURL, migrationsPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// clean tables between tests
	for _, table := range []string{"labor_assignment_history", "labor_assignment", "path_plan", "shift_plan", "associate_shift", "domain_event"} {
		if _, err := pool.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clean %s: %v", table, err)
		}
	}

	return pool
}

func TestAssociateRepo_SaveAndFindByID(t *testing.T) {
	pool := testPool(t)
	repo := NewAssociateRepo(pool)
	ctx := context.Background()

	shift := associate.NewAssociateShift("assoc-1", []shared.Certification{"pack"}, time.Now())
	shift.PullEvents()

	if err := repo.Save(ctx, shift); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := repo.FindByID(ctx, "assoc-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !loaded.HasCertification("pack") {
		t.Fatal("expected loaded associate to hold certification")
	}
}

func TestAssociateRepo_FindByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewAssociateRepo(pool)

	_, err := repo.FindByID(context.Background(), "ghost")
	if err != ports.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestShiftPlanRepo_SaveAndFind(t *testing.T) {
	pool := testPool(t)
	repo := NewShiftPlanRepo(pool)
	ctx := context.Background()

	lines := []shiftplan.PathPlan{{PathId: "pack", PlannedHeads: 5, PlannedRate: 30, PlannedHours: 40}}
	installed := map[shared.PathId]int{"pack": 10}
	sp, err := shiftplan.CommitShiftPlan("bldg-1", "shift-1", lines, installed, 8, time.Now())
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := repo.Save(ctx, sp); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := repo.FindByBuildingAndShift(ctx, "bldg-1", "shift-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if loaded.PlannedHeadsFor("pack") != 5 {
		t.Fatalf("expected 5 heads, got %d", loaded.PlannedHeadsFor("pack"))
	}
}

func TestAssignmentRepo_SaveAndFindAndCount(t *testing.T) {
	pool := testPool(t)
	repo := NewAssignmentRepo(pool)
	ctx := context.Background()

	la := assignment.NewLaborAssignment("assoc-1")
	if err := la.Assign("pack", true, time.Now()); err != nil {
		t.Fatalf("assign: %v", err)
	}
	la.PullEvents()

	if err := repo.Save(ctx, la); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := repo.FindByAssociateID(ctx, "assoc-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	pathId, active := loaded.ActivePathId()
	if !active || pathId != "pack" {
		t.Fatalf("expected active pack assignment, got %v active=%v", pathId, active)
	}

	count, err := repo.CountActiveByPath(ctx, "pack")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 active assignment, got %d", count)
	}
}
