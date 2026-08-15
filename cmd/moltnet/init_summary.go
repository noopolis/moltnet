package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/noopolis/moltnet/internal/app"
)

// initSummary bundles what printInitSummary needs to render the `moltnet
// init` aftercare block: what got created versus what already existed, and
// the resolved network id it fills into the "Next:" commands.
type initSummary struct {
	id            string
	root          string
	dirExisted    bool
	serverPath    string
	serverCreated bool
	nodeCreated   bool
	bearer        bool
	bearerAdded   bool
	bearerAddErr  error
}

// configLineColumn is the column the per-file "network: ... · auth: ..."
// annotation on the "wrote Moltnet" line starts at.
const configLineColumn = 24

// printInitSummary renders the two-space-indented, checkmark aftercare
// block `moltnet init` ends with: what was written (or already existed),
// where the operator token landed (or a nudge toward --bearer when none was
// requested), and a "Next:" block naming the real follow-up commands for
// this network.
func printInitSummary(summary initSummary) {
	dirVerb := "created"
	if summary.dirExisted {
		dirVerb = "using"
	}
	fmt.Fprintf(stdout, "  ✓ %s %s%s\n", dirVerb, abbreviateHome(summary.root), string(filepath.Separator))

	authLabel := "none"
	if summary.bearer {
		authLabel = "bearer"
	}
	if summary.bearerAdded {
		// N3: the config already existed (serverCreated is false), so the
		// generic "already exists (unchanged)" line would contradict the
		// "operator token added" line printed just below it. Say what
		// actually happened to the file instead.
		printInitConfigCheckLine("updated Moltnet", fmt.Sprintf("added operator token · auth: %s", authLabel))
	} else {
		printInitConfigLine("Moltnet", summary.serverCreated, fmt.Sprintf("network: %s · auth: %s", summary.id, authLabel))
	}
	printInitConfigLine("MoltnetNode", summary.nodeCreated, "")

	switch {
	case summary.bearer && summary.serverCreated:
		fmt.Fprintln(stdout, "  ✓ operator token stored in Moltnet (0600) — local admin")
		fmt.Fprintln(stdout, "    commands pick it up automatically")
	case summary.bearer && summary.bearerAdded:
		fmt.Fprintf(stdout, "  ✓ operator token added to %s (0600) — local admin\n", abbreviateHome(summary.serverPath))
		fmt.Fprintln(stdout, "    commands pick it up automatically")
	case summary.bearer && summary.bearerAddErr != nil:
		// The reason varies (auth.tokens already has entries, or the config
		// is a symlink init refuses to write through), so the note leads
		// with the actual error rather than assuming which one it was.
		fmt.Fprintf(stdout, "    note: --bearer could not add an operator token to %s (%v); edit auth: there to add one manually\n", abbreviateHome(summary.serverPath), summary.bearerAddErr)
	default:
		fmt.Fprintln(stdout, "    tip: rerun with --bearer to generate an operator token for admin access")
	}

	steps := []nextStep{
		{command: fmt.Sprintf("moltnet service install --id %s", summary.id), description: "run it as a service"},
		{command: fmt.Sprintf("moltnet relay deploy --id %s", summary.id), description: "relay on Cloudflare (pair across NAT)"},
	}
	if summary.id == app.DefaultNetworkID {
		// P1-4: `pair invite` refuses to run against the default network id
		// (two default installs would collide), so printing it here would
		// hand out a command that can never succeed.
		steps = append(steps, nextStep{
			command:     "moltnet init --id <network-id>",
			description: "re-init with a real network id before pairing",
		})
	} else {
		steps = append(steps, nextStep{
			command:     fmt.Sprintf("moltnet pair invite --network-id %s --room chat", summary.id),
			description: "invite a friend",
		})
	}
	printNextSteps(steps)
}

// printInitConfigLine prints the "wrote <label>" checkmark line when
// created is true (with extra appended, column-aligned, when non-empty), or
// an "already exists" line otherwise.
func printInitConfigLine(label string, created bool, extra string) {
	if !created {
		fmt.Fprintf(stdout, "  · %s already exists (unchanged)\n", label)
		return
	}
	printInitConfigCheckLine("wrote "+label, extra)
}

// printInitConfigCheckLine prints a column-aligned "  ✓ <what>" line, with
// extra appended (starting at configLineColumn) when non-empty. It backs
// both printInitConfigLine's "wrote <label>" case and the init --bearer
// rerun's "updated Moltnet" case, so a config that was amended in place
// gets its own truthful checkmark line instead of reusing "wrote" (which
// would claim the file was freshly written) or "already exists (unchanged)"
// (which would contradict the operator-token-added line printed right
// after it — N3).
func printInitConfigCheckLine(what string, extra string) {
	prefix := "  ✓ " + what
	if extra == "" {
		fmt.Fprintln(stdout, prefix)
		return
	}
	if width := utf8.RuneCountInString(prefix); width < configLineColumn {
		fmt.Fprintln(stdout, prefix+strings.Repeat(" ", configLineColumn-width)+extra)
		return
	}
	fmt.Fprintln(stdout, prefix+" "+extra)
}

// abbreviateHome replaces the user's home directory prefix in path with
// "~", matching how a human would read the path back; it returns path
// unchanged when the home directory can't be resolved or doesn't prefix it.
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
