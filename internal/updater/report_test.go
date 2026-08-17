package updater

import (
	"strings"
	"testing"
)

// TestCheckReportGatesRebuildAvailableOnCleanNonDetachedCheckout is the
// regression test for P2-4: --check/--dry-run must not promise "Rebuild
// available" when the checkout is dirty or HEAD is detached — a real run
// would refuse in both cases — and must instead preview the exact refusal
// a real run would give.
func TestCheckReportGatesRebuildAvailableOnCleanNonDetachedCheckout(t *testing.T) {
	tests := []struct {
		name        string
		plan        SourceUpdatePlan
		wantSubstr  string
		wantAbsence string
	}{
		{
			name:       "clean and on a branch",
			plan:       SourceUpdatePlan{Checkout: "/src/moltnet", Clean: true},
			wantSubstr: "Rebuild available: run `moltnet update`",
		},
		{
			name:        "dirty working tree",
			plan:        SourceUpdatePlan{Checkout: "/src/moltnet", Clean: false},
			wantSubstr:  "uncommitted changes",
			wantAbsence: "Rebuild available",
		},
		{
			name:        "detached HEAD",
			plan:        SourceUpdatePlan{Checkout: "/src/moltnet", Clean: true, Detached: true},
			wantSubstr:  "detached HEAD",
			wantAbsence: "Rebuild available",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Result{
				CheckOnly:       true,
				SourceUpdate:    &test.plan,
				UpdateAvailable: true,
			}
			output := result.String()
			if !strings.Contains(output, test.wantSubstr) {
				t.Fatalf("expected output to contain %q, got %q", test.wantSubstr, output)
			}
			if test.wantAbsence != "" && strings.Contains(output, test.wantAbsence) {
				t.Fatalf("expected output to NOT contain %q, got %q", test.wantAbsence, output)
			}
		})
	}
}

func TestRefusalSummaryUsesRefusalReason(t *testing.T) {
	result := Result{
		MutationRefused: true,
		RefusalReason:   `source checkout "/src/moltnet" has uncommitted changes; commit or stash them yourself, then retry` + " `moltnet update`",
	}
	output := result.String()
	if !strings.Contains(output, "uncommitted changes") {
		t.Fatalf("expected the actual blocker to be named, got %q", output)
	}
}

func TestRefusalSummaryFallsBackWithoutRefusalReason(t *testing.T) {
	result := Result{MutationRefused: true}
	output := result.String()
	if !strings.Contains(output, "Self-update is not available for this install.") {
		t.Fatalf("expected the generic fallback wording, got %q", output)
	}
}

func TestVerboseReportRendersPulledCommit(t *testing.T) {
	plan := SourceUpdatePlan{
		Checkout:     "/src/moltnet",
		Clean:        true,
		PulledCommit: "cafebabecafebabecafebabecafebabecafebabe",
	}
	result := Result{SourceUpdate: &plan, Updated: true}

	quiet := result.string(false)
	if strings.Contains(quiet, "pulled commit") {
		t.Fatalf("expected quiet output to omit pulled commit, got %q", quiet)
	}

	verbose := result.string(true)
	if !strings.Contains(verbose, "pulled commit") || !strings.Contains(verbose, "cafebabecafe") {
		t.Fatalf("expected verbose output to render the pulled commit, got %q", verbose)
	}
}
