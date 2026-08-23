package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// setup_adopt_refusal_test.go continues setup_adopt_test.go, split out
// purely to keep that file under this repo's 400-line limit.

// TestSetupNonInteractiveRefusalMatchesInteractiveEnterThrough is P2-5's
// mutation-proven gap closed: refuseSetupNonInteractive's plan must match
// exactly what pressing Enter through every real question produces, for
// both scopes — not a hand-maintained parallel copy of their defaults that
// can silently drift (flipping the refusal's wideBind to true previously
// left the whole suite green).
func TestSetupNonInteractiveRefusalMatchesInteractiveEnterThrough(t *testing.T) {
	globalScope, projectScope := setupScopeGlobal, setupScopeProject
	cases := []struct {
		name   string
		preset *setupScope
		args   []string
	}{
		{"global", &globalScope, []string{"setup", "--global", "--print-commands"}},
		{"project", &projectScope, []string{"setup", "--project", "--print-commands"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTestDefaultPort(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Chdir(t.TempDir()) // outside any git checkout; only matters for project's --dir .

			withPromptAnswers(t, "", "", "", "", "", "") // Enter through every question this scope asks; extras ignored.
			output := captureSetupOutput(t, func() {
				if err := run(context.Background(), tc.args, "test"); err != nil {
					t.Fatalf("setup --print-commands error = %v", err)
				}
			})

			// TODO(setup-p2): this preflights the listen port twice — once
			// inside the interactive run above, once here via
			// buildSetupRefusalInvocations — so anything else grabbing the
			// default port between the two calls makes the rendered
			// --listen value diverge and this comparison flake. Low
			// probability, no action.
			wantInvocations, err := buildSetupRefusalInvocations(tc.preset)
			if err != nil {
				t.Fatalf("buildSetupRefusalInvocations: %v", err)
			}
			if len(wantInvocations) == 0 {
				t.Fatal("buildSetupRefusalInvocations returned no invocations, want at least init")
			}

			// Exact sequence equality against wantInvocations, not just a
			// one-directional Contains check or an order-free multiset
			// comparison: parsing the exact "commands (not run):" block
			// catches a wanted invocation being dropped, a bogus extra line
			// being added, and the two paths disagreeing on order — a bare
			// strings.Contains per wanted line could miss the first two (an
			// interactive line that gained trailing arguments still
			// Contains-matches its own prefix, and a spurious extra
			// interactive line is never checked against at all), and a
			// multiset comparison would miss the third. Order is a real
			// assertion here, not an incidental one: both runSetupInteractive
			// (setup.go) and buildSetupRefusalInvocations (setup.go) call the
			// same setupPlanInvocations, so the two paths can never
			// legitimately order their steps differently, and the printed
			// block is a copy-paste script whose order matters (`init` before
			// `service install`) — reversing printSetupCommands' own loop
			// order must fail this test, not just an order-free one.
			wantLines := make([]string, len(wantInvocations))
			for i, invocation := range wantInvocations {
				wantLines[i] = renderChildInvocation(invocation)
			}
			gotLines := parseSetupPrintedCommandLines(t, output)
			if !reflect.DeepEqual(gotLines, wantLines) {
				t.Fatalf("interactive Enter-through --print-commands output printed %v, want exactly %v in the same order", gotLines, wantLines)
			}
		})
	}
}

// parseSetupPrintedCommandLines extracts each rendered "moltnet ..." command
// line (Env placeholders included) from printSetupCommands' own
// "commands (not run):" block in output, in the order they were printed —
// the parsed counterpart to renderChildInvocation, for exact sequence
// comparisons against a wanted []childInvocation rather than a substring
// search over the raw output.
func parseSetupPrintedCommandLines(t *testing.T, output string) []string {
	t.Helper()

	lines := strings.Split(output, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "commands (not run):" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("output %q missing the \"commands (not run):\" header", output)
	}

	var commands []string
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		commands = append(commands, trimmed)
	}
	return commands
}
