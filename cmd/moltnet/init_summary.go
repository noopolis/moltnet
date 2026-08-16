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
// the resolved network id it fills into the "next:" command.
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
	// verbose, when true, restores the per-file breakdown (what was
	// created/updated, exact paths, the --bearer tip) behind the quiet
	// single-outcome-line default (--verbose, see runInit).
	verbose bool
}

// configLineColumn is the column the per-file "network: ... · auth: ..."
// annotation on the "wrote Moltnet" line starts at (--verbose only).
const configLineColumn = 24

// printInitSummary renders `moltnet init`'s aftercare: by default, one
// checkmark line naming the network ready — with the resolved directory
// abbreviated alongside it, and " (existing)" appended when that directory
// was already there before this run rather than freshly created (P3: quiet
// mode used to say only "<id> ready", with no way to tell the two apart) —
// plus a real --bearer failure note, which is never hidden (an actionable
// problem, not decoration), and a single "next:" line. With --verbose, the
// full per-file breakdown this command used to always print (what was
// written or already existed, where the operator token landed, a nudge
// toward --bearer when none was requested) prints in addition to that same
// outcome line (P3: --verbose superset parity — it must only ever add
// detail on top of the quiet essentials, never replace them outright).
func printInitSummary(summary initSummary) {
	printInitConfigCheckLine(fmt.Sprintf("%s ready", summary.id), initReadyExtra(summary))

	if summary.verbose {
		printInitSummaryVerbose(summary)
	} else if summary.bearerAddErr != nil {
		fmt.Fprintf(stdout, "    %s --bearer could not add an operator token to %s (%v); edit auth: there to add one manually\n",
			yellow("note:"), abbreviateHome(summary.serverPath), summary.bearerAddErr)
	}

	printNextStep(initNextStep(summary.id))
}

// initReadyExtra renders printInitSummary's checkmark-line extra: the
// abbreviated resolved directory, with " (existing)" appended when
// summary.dirExisted — i.e. this run reused a directory that was already
// there rather than creating a fresh one.
func initReadyExtra(summary initSummary) string {
	extra := abbreviateHome(summary.root) + string(filepath.Separator)
	if summary.dirExisted {
		extra += " (existing)"
	}
	return extra
}

// printInitSummaryVerbose is printInitSummary's --verbose body: the
// original per-file checkmark breakdown, unchanged from before the
// quiet-by-default redesign.
func printInitSummaryVerbose(summary initSummary) {
	dirVerb := "created"
	if summary.dirExisted {
		dirVerb = "using"
	}
	fmt.Fprintf(stdout, "  %s %s %s%s\n", green("✓"), dirVerb, abbreviateHome(summary.root), string(filepath.Separator))

	authLabel := "none"
	if summary.bearer {
		authLabel = "bearer"
	}
	if summary.bearerAdded {
		// The config already existed (serverCreated is false), so the
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
		fmt.Fprintf(stdout, "  %s operator + console tokens stored in Moltnet (0600) — full access + read-only console\n", green("✓"))
		fmt.Fprintln(stdout, "    commands pick it up automatically")
	case summary.bearer && summary.bearerAdded:
		fmt.Fprintf(stdout, "  %s operator + console tokens added to %s (0600) — full access + read-only console\n", green("✓"), abbreviateHome(summary.serverPath))
		fmt.Fprintln(stdout, "    commands pick it up automatically")
	case summary.bearer && summary.bearerAddErr != nil:
		// The reason varies (auth.tokens already has entries, or the config
		// is a symlink init refuses to write through), so the note leads
		// with the actual error rather than assuming which one it was.
		fmt.Fprintf(stdout, "    %s --bearer could not add an operator token to %s (%v); edit auth: there to add one manually\n", yellow("note:"), abbreviateHome(summary.serverPath), summary.bearerAddErr)
	default:
		fmt.Fprintf(stdout, "    %s rerun with --bearer to generate an operator token for admin access\n", yellow("tip:"))
	}
}

// initNextStep names the one real follow-up command for a just-initialized
// network: `moltnet service install` for a real network id, or a re-init
// nudge for the still-default id (`pair invite` refuses to run against it —
// two default installs would collide — so suggesting service install/relay
// deploy first would just lead to a dead end).
func initNextStep(id string) nextStep {
	if id == app.DefaultNetworkID {
		return nextStep{command: "moltnet init --id <network-id>", description: "re-init with a real network id before pairing"}
	}
	return nextStep{command: fmt.Sprintf("moltnet service install --id %s", id), description: "run it as a service"}
}

// printInitConfigLine prints the "wrote <label>" checkmark line when
// created is true (with extra appended, column-aligned, when non-empty), or
// an "already exists" line otherwise. --verbose only.
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
// after it) — and, more generally, is the shared single-outcome-line
// building block every command's quiet-by-default checkmark
// (`relay deploy`'s "relay live", `service install`'s "service running",
// `pair invite`'s "pairing ready", ...) is printed through.
//
// Column width is computed from the plain (unstyled) prefix so alignment
// stays correct whether or not the ✓ and extra get styled for display.
func printInitConfigCheckLine(what string, extra string) {
	plainPrefix := "  ✓ " + what
	displayPrefix := "  " + green("✓") + " " + what
	if extra == "" {
		fmt.Fprintln(stdout, displayPrefix)
		return
	}
	styledExtra := dim(extra)
	if width := utf8.RuneCountInString(plainPrefix); width < configLineColumn {
		fmt.Fprintln(stdout, displayPrefix+strings.Repeat(" ", configLineColumn-width)+styledExtra)
		return
	}
	fmt.Fprintln(stdout, displayPrefix+" "+styledExtra)
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
