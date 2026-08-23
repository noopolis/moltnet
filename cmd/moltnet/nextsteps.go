package main

import (
	"fmt"
	"strings"
)

// nextStepColumn is the column (0-indexed) description text aligns to on a
// "next:" line; a command longer than that wraps its description onto its
// own continuation line indented to the same column.
const nextStepColumn = 51

// nextStep is one recommended follow-up: a runnable command and a short
// description of what it does.
type nextStep struct {
	command     string
	description string
}

// printNextStep prints the CLI's single-recommended-action line: a blank
// line, then "  next: <command>    <description>", column-aligned via
// formatNextStepWithPrefix. This is the shape every command in this CLI
// that ends with a follow-up suggestion uses — the redesigned quiet-by-
// default UX chains commands single-file (init -> service install -> relay
// deploy -> pair invite -> the membership command), so there is never more
// than one genuinely next action to name; a numbered menu of alternatives
// only made the eye wander over choices that were never really optional.
func printNextStep(step nextStep) {
	printNextStepList([]nextStep{step})
}

// printNextStepList is printNextStep's multi-line counterpart, used in two
// shapes: more than one command genuinely required next — the same action
// repeated once per declared shared room (`pair invite --room a --room b`)
// — or, since PLAN.md 7A.4, genuine alternatives the operator picks between
// (`service install`'s three-intent fork: local agents only, open up to a
// friend, or join an existing network — printServiceInstallNextSteps,
// service.go). Either way each line gets its own "next: " lead-in rather
// than a shared header, since there is no single header that reads
// correctly above more than one command here — for the fork case that also
// means never implying a ranked or default choice among the three. A no-op
// for an empty slice, so callers can build it conditionally without an
// extra guard at the call site.
func printNextStepList(steps []nextStep) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(stdout)
	for _, step := range steps {
		fmt.Fprintln(stdout, formatNextStepWithPrefix("  next: ", step.command, step.description))
	}
}

// formatNextStepWithPrefix column-aligns description to nextStepColumn
// given an arbitrary line-start prefix, wrapping onto an indented
// continuation line at nextStepColumn when prefix+command runs past it.
// Width is computed from the plain (unstyled) prefix+command, then the
// command is bolded and the description dimmed for display — so column
// alignment stays correct whether or not styling is active, and is
// byte-identical to plain text when it is not (bold/dim are no-ops off a
// terminal or under NO_COLOR).
func formatNextStepWithPrefix(prefix, command, description string) string {
	line := prefix + command
	styledLine := prefix + bold(command)
	if len(line) < nextStepColumn {
		return styledLine + strings.Repeat(" ", nextStepColumn-len(line)) + dim(description)
	}
	return styledLine + "\n" + strings.Repeat(" ", nextStepColumn) + dim(description)
}
