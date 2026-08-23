package main

import (
	"strings"
	"testing"
)

// TestPrintSetupOpenUpSummaryWarnsOnEmptyRetrievedCode pins the fix for a
// real gap: an empty-but-successful `pair invite show` (no error, just "")
// used to early-return silently here, rendering the rest of the completion
// screen with the invite just absent -- no clue to the operator that their
// friend's invite was never actually shown. It must now print an explicit
// warning naming the pairing id and the exact recovery command.
func TestPrintSetupOpenUpSummaryWarnsOnEmptyRetrievedCode(t *testing.T) {
	plan := &setupPairOpenUpPlan{pairingID: "friend-net", room: "general"}

	output := captureStdout(t, func() {
		printSetupOpenUpSummary(plan, "local", "")
	})

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected a warning: line for an empty retrieved code, got %q", output)
	}
	if !strings.Contains(output, "moltnet pair invite show friend-net") {
		t.Fatalf("expected the warning to name the recovery command with the pairing id, got %q", output)
	}
}

// TestPrintSetupOpenUpSummaryNoOpWithoutAPlan is the control case: Q7 never
// having reached "open it up" at all (plan == nil) must stay a silent no-op,
// unaffected by the fix above -- there is no pairing to warn about.
func TestPrintSetupOpenUpSummaryNoOpWithoutAPlan(t *testing.T) {
	output := captureStdout(t, func() {
		printSetupOpenUpSummary(nil, "local", "")
	})
	if output != "" {
		t.Fatalf("expected no output when plan is nil, got %q", output)
	}
}
