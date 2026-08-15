package main

import (
	"fmt"
	"strings"
)

// nextStepColumn is the column (0-indexed) description text aligns to in a
// "Next:" block; a command longer than that wraps its description onto its
// own continuation line indented to the same column. Shared by every
// command that ends its output with a "Next:" block (init, relay deploy)
// so the CLI's aftercare output reads as one consistent design.
const nextStepColumn = 51

// nextStep is one line of a "Next:" block: a runnable command and a short
// description of what it does.
type nextStep struct {
	command     string
	description string
}

// printNextSteps prints the two-space "Next:" header (preceded by a blank
// line) followed by each step, column-aligned via formatNextStep. It is a
// no-op for an empty steps slice, so callers can build the slice
// conditionally without an extra guard at the call site.
func printNextSteps(steps []nextStep) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  Next:")
	for _, step := range steps {
		fmt.Fprintln(stdout, formatNextStep(step.command, step.description))
	}
}

// formatNextStep column-aligns one "Next:" line's description to
// nextStepColumn, wrapping the description onto its own indented
// continuation line when command itself runs past that column. Width is
// computed from the plain (unstyled) command, then the command is bolded
// and the description dimmed for display — so column alignment stays
// correct whether or not styling is active, and is byte-identical to plain
// text when it is not (bold/dim are no-ops off a terminal or under
// NO_COLOR).
func formatNextStep(command, description string) string {
	line := "    " + command
	styledLine := "    " + bold(command)
	if len(line) < nextStepColumn {
		return styledLine + strings.Repeat(" ", nextStepColumn-len(line)) + dim(description)
	}
	return styledLine + "\n" + strings.Repeat(" ", nextStepColumn) + dim(description)
}
