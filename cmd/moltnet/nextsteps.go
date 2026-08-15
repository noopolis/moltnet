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
// continuation line when command itself runs past that column.
func formatNextStep(command, description string) string {
	line := "    " + command
	if len(line) < nextStepColumn {
		return line + strings.Repeat(" ", nextStepColumn-len(line)) + description
	}
	return line + "\n" + strings.Repeat(" ", nextStepColumn) + description
}
