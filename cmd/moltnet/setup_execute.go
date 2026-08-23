package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// setupHealthProbeTimeout bounds probeSetupNetworkHealth's total retry
// window. A freshly installed service needs a moment to bind and start
// answering, so this is generous relative to consoleHealthTimeout's single-
// probe 1.5s (console.go) — this is polling for readiness after a step this
// same command just triggered, not just checking on a network that should
// already be up.
//
// Both this and the interval below are vars, not consts: setup_test.go's
// fake-service-manager tests never actually bind a listener (the same
// fakeServiceRunner service_test.go uses for `service install` tests never
// spawns a real process), so a health probe against them is guaranteed to
// exhaust the full window every time — shrinking both here keeps those
// tests fast without changing production behavior.
var setupHealthProbeTimeout = 5 * time.Second

// setupHealthProbeInterval is the delay between polls within the window
// above.
var setupHealthProbeInterval = 200 * time.Millisecond

// executeSetupPlan runs invocations in order through runChild, then —
// only for scope global with a service requested — health-probes the
// result before handing off to printSetupSummary. Config mutations
// (the init step, and Q7's pairing steps when present) always precede the
// service step, since setupPlanInvocations already fixed that order; this
// function does not reorder anything, only executes and reports.
func executeSetupPlan(ctx context.Context, answers setupAnswers, invocations []childInvocation, configPath string) error {
	var inviteCode string
	for _, invocation := range invocations {
		output, err := runSetupStep(ctx, invocation)
		if err != nil {
			return err
		}
		if invocation.CaptureOutput {
			// The one documented exception to "a step prints its own
			// checkmark line" (SETUP.md's "Exception: the invite code"):
			// `pair invite show`'s output is the invite code itself, which
			// gets its own dedicated presentation in printSetupPairingSummary
			// below, not a generic "✓ invite retrieved" line.
			inviteCode = output
			continue
		}
		printInitConfigCheckLine(stepLabel(invocation), "")
	}

	running := false
	if answers.scope == setupScopeGlobal && answers.service {
		base, err := consoleBaseURL(answers.listenAddr)
		if err != nil {
			return err
		}
		running = probeSetupNetworkHealth(ctx, base, answers.id)
		if !running {
			fmt.Fprintf(stdout, "  %s service installed but %s did not answer /v1/network as %q within %s; check `moltnet service status --id %s`\n",
				yellow("warning:"), base, answers.id, setupHealthProbeTimeout, answers.id)
		}
	}

	printSetupSummary(answers, configPath, running)
	printSetupPairingSummary(answers, inviteCode)
	return nil
}

// runSetupStep runs one invocation through runChild, returning its captured
// stdout (only ever non-empty when invocation.CaptureOutput is set — see
// childInvocation's own doc comment, runner.go; the caller,
// executeSetupPlan, is what decides whether to print a generic checkmark
// line or treat the output specially, so this never prints anything itself
// beyond what runChild's own error path requires). A failure surfaces the
// child's own stderr (runner.go's childResult) appended to the wrapped exit
// error, so the operator sees *why* the step failed instead of a bare exit
// status.
func runSetupStep(ctx context.Context, invocation childInvocation) (string, error) {
	result, err := runChild(ctx, invocation)
	if err != nil {
		if result.Stderr != "" {
			return "", fmt.Errorf("%w\n  %s", err, strings.ReplaceAll(result.Stderr, "\n", "\n  "))
		}
		return "", err
	}
	return result.Stdout, nil
}

// setupNetworkProbeResponse is the one field of GET /v1/network's response
// (protocol.Network, pkg/protocol/types.go) probeSetupNetworkHealth needs:
// proof that *this* network answered, not just that something on the port
// did (SETUP.md: "not just /healthz, so it proves this network answers").
type setupNetworkProbeResponse struct {
	ID string `json:"id"`
}

// probeSetupNetworkHealth polls base's GET /v1/network until it answers 200
// with a body whose id matches wantID, or setupHealthProbeTimeout elapses.
// It is anonymous under the open posture the wizard exclusively authors
// (networkScopes' publicInOpen wiring, internal/transport/http.go), so no
// credential is ever needed here.
func probeSetupNetworkHealth(ctx context.Context, base, wantID string) bool {
	deadline := time.Now().Add(setupHealthProbeTimeout)
	for {
		if probeSetupNetworkOnce(ctx, base, wantID) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(setupHealthProbeInterval):
		}
	}
}

// probeSetupNetworkOnce issues exactly one GET base/v1/network. It reuses
// console.go's consoleHTTPClient rather than a package-private client of
// its own: both are plain, unauthenticated, short-timeout probes against a
// locally resolved base URL, and console.go is not among the files this
// unit is barred from editing.
func probeSetupNetworkOnce(ctx context.Context, base, wantID string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, consoleHealthTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"/v1/network", nil)
	if err != nil {
		return false
	}
	response, err := consoleHTTPClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var body setupNetworkProbeResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return false
	}
	return body.ID == wantID
}

// printSetupSummary prints the wizard's closing screen: "running" only for
// a healthy, service-managed global network; "configured" (plus the exact
// foreground command) otherwise — project scope, a declined service, or a
// service that did not come up healthy — matching SETUP.md's "Configured"
// vs "running"" section. It always points at the network's own
// /install.md, and prints a second, LAN-addressed URL under a widened bind
// (a wildcard listen address is not itself dialable — consoleBaseURL
// already substitutes loopback for it, so a second line is the only way to
// also hand out something reachable from another machine).
func printSetupSummary(answers setupAnswers, configPath string, running bool) {
	fmt.Fprintln(stdout)

	outcome := "configured"
	if running {
		outcome = "running"
	}
	printInitConfigCheckLine(fmt.Sprintf("%s %s", answers.id, outcome), abbreviateHome(configPath))
	printInitConfigCheckLine(fmt.Sprintf("room%s %s ready", pluralSuffix(len(answers.rooms)), strings.Join(answers.rooms, ", ")), "")

	loopbackBase, err := consoleBaseURL(answers.listenAddr)
	if err == nil {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "  Point any agent here:")
		fmt.Fprintf(stdout, "    %s\n", loopbackBase+"/install.md")
		if answers.wideBind {
			if lanURL, ok := setupLANInstallURL(answers.listenAddr); ok {
				fmt.Fprintf(stdout, "    %s  %s\n", lanURL, dim("(from your LAN)"))
			}
		}
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, dim("  on-demand agents (Claude Code, Codex, ...) need no MoltnetNode; persistent/wake runtimes do — see /install.md"))

	if running {
		printNextStep(nextStep{command: fmt.Sprintf("moltnet console --id %s", answers.id), description: "watch it live"})
		return
	}
	printNextStep(nextStep{command: setupForegroundCommand(answers), description: "start it in the foreground"})
}

// setupForegroundCommand names the exact command SETUP.md's "Configured vs
// running" section requires printing whenever the network is not left
// running: bare `moltnet start` for project scope (cwd discovery already
// finds ./Moltnet with no flags, exactly J3's model), `moltnet start --id
// <id>` for a global network with the service declined.
func setupForegroundCommand(answers setupAnswers) string {
	if answers.scope == setupScopeProject {
		return "moltnet start"
	}
	return fmt.Sprintf("moltnet start --id %s", answers.id)
}

// setupLANInstallURL derives a LAN-reachable /install.md URL from
// listenAddr's port and the first non-loopback IPv4 address this machine
// has configured, for printSetupSummary's second URL line under a widened
// bind. ok is false when no such address can be found (offline, IPv6-only,
// ...) — SETUP.md's two-URL requirement is best-effort by nature: there is
// no single canonical "your LAN address" on a multi-homed machine, and
// printing nothing is safer than guessing wrong.
func setupLANInstallURL(listenAddr string) (string, bool) {
	_, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil || port == "" {
		return "", false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil {
			continue
		}
		return fmt.Sprintf("http://%s/install.md", net.JoinHostPort(ip4.String(), port)), true
	}
	return "", false
}

// pluralSuffix returns "" for exactly one, "s" otherwise — just enough
// pluralization for "room"/"rooms" in the summary line above.
func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
