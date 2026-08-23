package main

import (
	"fmt"
	"strings"
)

// setupPlanInvocations turns a fully answered setupAnswers into the exact
// ordered child commands runSetup drives through runChild — or, under
// --print-commands, only prints. adopted is true when an existing network
// was found and passed both adoption gates (setup_adopt.go): SETUP.md's
// adoption table requires skipping `init` entirely in that case ("Skip
// init, adopt, continue with whatever durable steps are missing"), never
// re-running it as a no-op just to be thorough.
//
// Order matters and is fixed, matching SETUP.md's "Execution order ≠
// question order": every config mutation completes before the service step,
// which is always last. Q7's two pairing branches (setup_pair.go) sit
// between `init` and `service install` for exactly that reason — `relay
// deploy` and `pair invite`/`pair join` both mutate config (or config-
// adjacent .moltnet/ state), and `pair invite` in particular tells the
// operator to restart when it is not itself allowed to (pair.go), so
// installing the service ahead of it would leave it running stale config.
func setupPlanInvocations(answers setupAnswers, adopted bool) []childInvocation {
	var invocations []childInvocation
	if !adopted {
		invocations = append(invocations, buildSetupInitInvocation(answers))
	}
	invocations = append(invocations, setupPairInvocations(answers)...)
	if answers.scope == setupScopeGlobal && answers.service {
		invocations = append(invocations, buildSetupServiceInstallInvocation(answers))
	}
	return invocations
}

// buildSetupInitInvocation renders the `moltnet init` argv for answers.
// Never --bearer (the wizard only ever authors the open posture — see
// SETUP.md's Q3 section) and never --name (init already derives a display
// name from --id when one is not given, exactly the single-typed-answer
// model Q2 uses — see askSetupNetworkID's own doc comment).
func buildSetupInitInvocation(answers setupAnswers) childInvocation {
	args := []string{"init"}
	if answers.scope == setupScopeProject {
		args = append(args, "--dir", ".")
	}
	args = append(args, "--id", answers.id, "--listen", answers.listenAddr)
	for _, room := range answers.rooms {
		args = append(args, "--room", room)
	}
	return childInvocation{Args: args}
}

// buildSetupServiceInstallInvocation renders the `moltnet service install`
// argv. Project scope never reaches this (setupPlanInvocations only calls
// it for setupScopeGlobal), matching J3's "foreground-only in v1". --config
// names the exact path this run resolved (answers.configPath) rather than
// --id: `service install`'s own --id-based discovery
// (app.ResolveConfigPath) would otherwise resolve ~/.moltnet/<id>/Moltnet
// ahead of a project-scope cwd config of the same id, and MOLTNET_CONFIG in
// the operator's inherited environment outranks --id outright -- either one
// can silently install a service pointed at a different network than the
// one setup just decided on.
func buildSetupServiceInstallInvocation(answers setupAnswers) childInvocation {
	return childInvocation{Args: []string{"service", "install", "--config", answers.configPath}}
}

// stepLabel names one planned invocation for the wizard's own one-line-per-
// step transcript (SETUP.md: "The wizard renders its own one-line-per-step
// output," since a piped child's own "✓ ready"/"next:" lines are suppressed
// outright — runner.go's doc comment). Falls back to the raw argv, joined,
// for any shape this does not specifically recognize, so a future third
// invocation type never prints as a blank line.
func stepLabel(invocation childInvocation) string {
	args := invocation.Args
	switch {
	case len(args) >= 1 && args[0] == "init":
		return "network initialized"
	case len(args) >= 2 && args[0] == "service" && args[1] == "install":
		return "service installed"
	case len(args) >= 2 && args[0] == "relay" && args[1] == "deploy":
		return "relay deployed"
	case len(args) >= 3 && args[0] == "pair" && args[1] == "invite" && args[2] == "show":
		return "invite retrieved"
	case len(args) >= 2 && args[0] == "pair" && args[1] == "invite":
		return "invite generated"
	case len(args) >= 1 && args[0] == "pair":
		return "paired"
	default:
		return strings.Join(args, " ")
	}
}

// renderChildInvocation formats invocation as a copy-pasteable shell
// command line for --print-commands and the non-TTY refusal one-liner:
// "moltnet <args...>", each argument shell-quoted only when it actually
// needs it (so the common case reads as plainly as typing it by hand), with
// any Env entries rendered as a leading "KEY=<value from $KEY>" placeholder
// rather than the literal secret — SETUP.md's explicit rule: a literal
// value would leak into a transcript or a copied script; a caller-supplied
// env-var name lets the reader (and the equivalence test) know exactly what
// to substitute. Q7 = "open it up" is the one invocation that actually
// carries Env today — buildRelayDeployInvocation's CLOUDFLARE_API_TOKEN
// (setup_pair.go) — so this path is exercised by both setup_plan_test.go's
// generic unit test and setup_pair_open_test.go's --print-commands
// end-to-end coverage.
func renderChildInvocation(invocation childInvocation) string {
	var b strings.Builder
	for _, kv := range invocation.Env {
		key, _, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "%s=<value from $%s> ", key, key)
	}
	b.WriteString("moltnet")
	for _, arg := range invocation.Args {
		b.WriteString(" ")
		b.WriteString(shellQuoteIfNeeded(arg))
	}
	return b.String()
}

// shellQuoteIfNeeded single-quotes s (POSIX-sh style, escaping an embedded
// single quote by closing the quoted string, emitting an escaped literal
// quote, then reopening it) only when it contains a character a shell would
// otherwise treat specially, so a plain argument like "acme-net" or
// "127.0.0.1:8787" prints unquoted and legible.
func shellQuoteIfNeeded(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"$`\\*?[]{}()<>|&;~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// printSetupCommands is --print-commands' entire non-execution output: the
// exact commands the interactive run would have driven through runChild,
// one per line, in the same fixed order setupPlanInvocations returns them.
func printSetupCommands(invocations []childInvocation) {
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  commands (not run):")
	for _, invocation := range invocations {
		fmt.Fprintf(stdout, "    %s\n", renderChildInvocation(invocation))
	}
}
