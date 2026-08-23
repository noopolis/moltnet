package main

import (
	"reflect"
	"strings"
	"testing"
)

// setupPairOpenUpTestPlan is a fully populated setupPairOpenUpPlan for the
// pure-function invariant tests below — no Cloudflare, no filesystem, no
// prompts, just setupPairInvocations/buildRelayDeployInvocation's own
// argv-shaping rules.
func setupPairOpenUpTestPlan() *setupPairOpenUpPlan {
	return &setupPairOpenUpPlan{
		cloudflareToken: "cf-secret-token",
		subdomain:       "acme",
		room:            "general",
		pairingID:       "friend-net",
	}
}

// TestSetupPairInvocationsCaptureOutputOnlyOnInviteShow guards the single
// documented exception to "child stdout is always discarded" (runner.go's
// childInvocation doc comment, SETUP.md's "Exception: the invite code"):
// exactly one invocation in the "open it up" plan may set CaptureOutput, and
// it must be `pair invite show`, not `relay deploy` (which carries the
// secret-bearing Env) or `pair invite` itself. Setting CaptureOutput: true
// on the wrong invocation would leave the suite green today since nothing
// else asserts this — this is that assertion.
func TestSetupPairInvocationsCaptureOutputOnlyOnInviteShow(t *testing.T) {
	answers := setupAnswers{
		id:         "acme-net",
		configPath: "/home/.moltnet/acme-net/Moltnet",
		connect:    setupConnectOpenUp,
		pairing:    setupPairResolution{openUp: setupPairOpenUpTestPlan()},
	}
	invocations := setupPairInvocations(answers)

	captureCount := 0
	var captured childInvocation
	for _, invocation := range invocations {
		if invocation.CaptureOutput {
			captureCount++
			captured = invocation
		}
	}
	if captureCount != 1 {
		t.Fatalf("setupPairInvocations() has %d invocations with CaptureOutput set, want exactly 1: %+v", captureCount, invocations)
	}
	want := []string{"pair", "invite", "show"}
	if len(captured.Args) < 3 || strings.Join(captured.Args[:3], " ") != strings.Join(want, " ") {
		t.Fatalf("the CaptureOutput invocation's argv = %v, want it to start with %v", captured.Args, want)
	}
}

// TestBuildRelayDeployInvocationAlwaysSavesToken guards SETUP.md
// precondition 3's `--save-token` requirement: a piped, non-interactive
// `relay deploy` can never answer its own save-prompt, so the wizard must
// always pass `--save-token` itself. Dropping the flag from
// buildRelayDeployInvocation left the suite green before this test existed.
func TestBuildRelayDeployInvocationAlwaysSavesToken(t *testing.T) {
	invocation := buildRelayDeployInvocation("/home/.moltnet/acme-net/Moltnet", setupPairOpenUpTestPlan())
	found := false
	for _, arg := range invocation.Args {
		if arg == "--save-token" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("buildRelayDeployInvocation() args = %v, want --save-token", invocation.Args)
	}
}

// TestBuildRelayDeployInvocationUsesExplicitConfigNotID guards the P1 fix:
// every post-init child is threaded the exact resolved --config path, never
// --id/--network-id -- an --id-based child would resolve
// ~/.moltnet/<id>/Moltnet itself (or fall through to MOLTNET_CONFIG in the
// operator's inherited environment, which outranks --id outright), which can
// silently disagree with the config this wizard actually decided on and read
// the Cloudflare token from (resolveSetupCloudflareToken, setup_pair_open.go).
func TestBuildRelayDeployInvocationUsesExplicitConfigNotID(t *testing.T) {
	const wantConfigPath = "/home/.moltnet/acme-net/Moltnet"
	invocation := buildRelayDeployInvocation(wantConfigPath, setupPairOpenUpTestPlan())

	if !argsContainFlagValue(invocation.Args, "--config", wantConfigPath) {
		t.Fatalf("buildRelayDeployInvocation() args = %v, want --config %q", invocation.Args, wantConfigPath)
	}
	if argsContainFlag(invocation.Args, "--id") || argsContainFlag(invocation.Args, "--network-id") {
		t.Fatalf("buildRelayDeployInvocation() args = %v, want no --id/--network-id", invocation.Args)
	}
}

// argsContainFlag reports whether flag appears anywhere in args.
func argsContainFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// argsContainFlagValue reports whether flag is immediately followed by
// value somewhere in args.
func argsContainFlagValue(args []string, flag, value string) bool {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// TestBuildRelayDeployInvocationNeverPutsTokenInArgs guards "the Cloudflare
// token never reaches argv" directly against the *built* invocation, not
// --print-commands' rendering of it. TestSetupPairOpenUpPrintCommandsNeverLeaksToken
// (setup_pair_open_test.go) only proves the printed line is redacted, and it
// went blind to this once --print-commands started leaving
// plan.cloudflareToken == "" (planSetupPairOpenUp skips token resolution
// under that flag): appending "--token", plan.cloudflareToken to Args below
// would still print as "--token ”" and pass that test, and the three
// end-to-end tests would only fail incidentally, because `relay deploy`
// happens to reject an unknown flag — a leak through an *accepted* flag
// would ship silently. This asserts on Args/Env directly, with a plan whose
// token is actually non-empty (setupPairOpenUpTestPlan), so it would catch
// that leak regardless of what --print-commands prints or which flags
// `relay deploy` currently accepts.
func TestBuildRelayDeployInvocationNeverPutsTokenInArgs(t *testing.T) {
	invocation := buildRelayDeployInvocation("/home/.moltnet/acme-net/Moltnet", setupPairOpenUpTestPlan())

	for _, arg := range invocation.Args {
		if strings.Contains(arg, "cf-secret-token") {
			t.Fatalf("buildRelayDeployInvocation() args = %v, want no argument containing the Cloudflare token", invocation.Args)
		}
	}

	wantEnv := []string{"CLOUDFLARE_API_TOKEN=cf-secret-token"}
	if !reflect.DeepEqual(invocation.Env, wantEnv) {
		t.Fatalf("buildRelayDeployInvocation() env = %v, want exactly %v", invocation.Env, wantEnv)
	}
}

// TestBuildPairInviteInvocationUsesExplicitConfigNotNetworkID guards the
// same P1 fix as TestBuildRelayDeployInvocationUsesExplicitConfigNotID:
// `pair invite`'s network-selecting flag (--network-id, not its own --id,
// which is the *pairing* id here) must be replaced by the exact --config
// path this run resolved.
func TestBuildPairInviteInvocationUsesExplicitConfigNotNetworkID(t *testing.T) {
	const wantConfigPath = "/home/.moltnet/acme-net/Moltnet"
	plan := setupPairOpenUpTestPlan()
	invocation := buildPairInviteInvocation(wantConfigPath, plan)

	if !argsContainFlagValue(invocation.Args, "--config", wantConfigPath) {
		t.Fatalf("buildPairInviteInvocation() args = %v, want --config %q", invocation.Args, wantConfigPath)
	}
	if argsContainFlag(invocation.Args, "--network-id") {
		t.Fatalf("buildPairInviteInvocation() args = %v, want no --network-id", invocation.Args)
	}
	// --id here still names the *pairing* id (plan.pairingID) -- that flag
	// must survive, it is not the network-selecting flag being replaced.
	if !argsContainFlagValue(invocation.Args, "--id", plan.pairingID) {
		t.Fatalf("buildPairInviteInvocation() args = %v, want --id %q (the pairing id)", invocation.Args, plan.pairingID)
	}
}

// TestBuildPairInviteShowInvocationUsesExplicitConfigNotID mirrors the above
// for `pair invite show`, whose own --id (unlike `pair invite`'s) already
// means network id -- it must be replaced by --config entirely.
func TestBuildPairInviteShowInvocationUsesExplicitConfigNotID(t *testing.T) {
	const wantConfigPath = "/home/.moltnet/acme-net/Moltnet"
	invocation := buildPairInviteShowInvocation(wantConfigPath, setupPairOpenUpTestPlan())

	if !argsContainFlagValue(invocation.Args, "--config", wantConfigPath) {
		t.Fatalf("buildPairInviteShowInvocation() args = %v, want --config %q", invocation.Args, wantConfigPath)
	}
	if argsContainFlag(invocation.Args, "--id") || argsContainFlag(invocation.Args, "--network-id") {
		t.Fatalf("buildPairInviteShowInvocation() args = %v, want no --id/--network-id", invocation.Args)
	}
}

// TestBuildPairJoinInvocationUsesExplicitConfigNotID mirrors the above for
// `pair <code>`.
func TestBuildPairJoinInvocationUsesExplicitConfigNotID(t *testing.T) {
	const wantConfigPath = "/home/.moltnet/acme-net/Moltnet"
	invocation := buildPairJoinInvocation(wantConfigPath, &setupPairHaveCodePlan{code: "some-code"})

	if !argsContainFlagValue(invocation.Args, "--config", wantConfigPath) {
		t.Fatalf("buildPairJoinInvocation() args = %v, want --config %q", invocation.Args, wantConfigPath)
	}
	if argsContainFlag(invocation.Args, "--id") || argsContainFlag(invocation.Args, "--network-id") {
		t.Fatalf("buildPairJoinInvocation() args = %v, want no --id/--network-id", invocation.Args)
	}
}
