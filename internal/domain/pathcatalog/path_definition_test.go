package pathcatalog_test

import (
	"errors"
	"testing"

	"github.com/claudioed/workforce-management/internal/domain/pathcatalog"
)

func fleetCatalogue() *pathcatalog.Catalogue {
	return pathcatalog.New([]pathcatalog.PathDefinition{
		{Id: "PICK", MatchPrefix: "pick", RequiredCapabilities: []string{"pick"}},
		{Id: "PACK", MatchPrefix: "pack", RequiredCapabilities: []string{"pack"}},
		{Id: "REBIN", MatchPrefix: "rebin", RequiredCapabilities: []string{"rebin"}},
		{Id: "SLAM", MatchPrefix: "slam", RequiredCapabilities: []string{"slam"}},
	})
}

func TestCatalogue_Lookup_UnknownPathId_ReturnsError(t *testing.T) {
	cat := pathcatalog.New([]pathcatalog.PathDefinition{
		{Id: "PICK", MatchPrefix: "pick", RequiredCapabilities: []string{"pick"}},
	})
	if _, err := cat.Lookup("rebin"); !errors.Is(err, pathcatalog.ErrUnknownPath) {
		t.Fatalf("want ErrUnknownPath, got %v", err)
	}
}

func TestCatalogue_Lookup_KnownPathId_ReturnsDefinition(t *testing.T) {
	cat := pathcatalog.New([]pathcatalog.PathDefinition{
		{Id: "PICK", MatchPrefix: "pick", RequiredCapabilities: []string{"pick"}},
	})
	def, err := cat.Lookup("PICK")
	if err != nil || def.Id != "PICK" {
		t.Fatalf("unexpected lookup result: %+v, err=%v", def, err)
	}
}

func TestCatalogue_Lookup_AllFourDeclaredPaths(t *testing.T) {
	cat := fleetCatalogue()
	for _, want := range []string{"PICK", "PACK", "REBIN", "SLAM"} {
		def, err := cat.Lookup(want)
		if err != nil {
			t.Fatalf("Lookup(%q): unexpected error %v", want, err)
		}
		if def.Id != want {
			t.Fatalf("Lookup(%q): got id %q", want, def.Id)
		}
	}
}

// The real regression class this test exists to prevent (see
// fulfillment-execution's ADR-0017 addendum): actual path_id values in
// this fleet are not the bare canonical id. This service's own
// ProposePathPlan/CommitShiftPlan/AssignLabor/staffing-gap endpoints all
// accept caller-supplied path_id values that look like this.
func TestCatalogue_Lookup_RealFleetPathIdVariants(t *testing.T) {
	cat := fleetCatalogue()

	cases := []struct{ pathId, wantId string }{
		{"pick", "PICK"},
		{"pick-zone-a", "PICK"},
		{"pick-soak", "PICK"},
		{"pack-station-3", "PACK"},
		{"PICK", "PICK"},
		{"Pick-Zone-B", "PICK"},
	}
	for _, tc := range cases {
		def, err := cat.Lookup(tc.pathId)
		if err != nil {
			t.Fatalf("Lookup(%q): unexpected error %v", tc.pathId, err)
		}
		if def.Id != tc.wantId {
			t.Fatalf("Lookup(%q): got id %q, want %q", tc.pathId, def.Id, tc.wantId)
		}
	}
}

func TestCatalogue_Lookup_DoesNotMatchBareSubstringPrefix(t *testing.T) {
	cat := fleetCatalogue()
	if _, err := cat.Lookup("picking-station"); !errors.Is(err, pathcatalog.ErrUnknownPath) {
		t.Fatalf("want ErrUnknownPath, got %v", err)
	}
}

func TestCatalogue_Ids_ReturnsEveryDeclaredId(t *testing.T) {
	cat := pathcatalog.New([]pathcatalog.PathDefinition{
		{Id: "PICK", MatchPrefix: "pick", RequiredCapabilities: []string{"pick"}},
		{Id: "PACK", MatchPrefix: "pack", RequiredCapabilities: []string{"pack"}},
	})
	if len(cat.Ids()) != 2 {
		t.Fatalf("expected 2 ids, got %v", cat.Ids())
	}
}

func TestCatalogue_EmptyCatalogue_EveryLookupFails(t *testing.T) {
	cat := pathcatalog.New(nil)
	if _, err := cat.Lookup("pick"); !errors.Is(err, pathcatalog.ErrUnknownPath) {
		t.Fatalf("want ErrUnknownPath, got %v", err)
	}
}

// Deterministic tie-break, mirroring wes-work-planning's own hardened
// test for this rule.
func TestCatalogue_Lookup_TiedPrefixLength_FirstDeclaredWins(t *testing.T) {
	cat := pathcatalog.New([]pathcatalog.PathDefinition{
		{Id: "FIRST", MatchPrefix: "pick", RequiredCapabilities: []string{"pick"}},
		{Id: "SECOND", MatchPrefix: "pick", RequiredCapabilities: []string{"pick"}},
	})
	def, err := cat.Lookup("pick")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Id != "FIRST" {
		t.Fatalf("expected the first-declared definition to win a length tie, got %q", def.Id)
	}
}
