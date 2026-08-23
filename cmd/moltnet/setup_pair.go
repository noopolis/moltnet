package main

import "context"

// setupPairResolution is what asking Q7's two connected branches resolves
// to (planSetupPairing, below): enough for setupPlanInvocations
// (setup_plan.go) to build the exact child commands, and for
// executeSetupPlan (setup_execute.go) to know whether an invite code needs
// retrieving afterward and what the completion screen should say. Zero
// value is Q7 = "not now" (setupConnectNotNow), which needs neither field.
type setupPairResolution struct {
	// openUp is non-nil only when connect == setupConnectOpenUp.
	openUp *setupPairOpenUpPlan
	// haveCode is non-nil only when connect == setupConnectHaveCode.
	haveCode *setupPairHaveCodePlan
}

// planSetupPairing is Q7's dispatcher, called once askSetupConnect has an
// answer (runSetupInteractive, setup.go). rooms is Q5's final room list
// (needed for Q7.1's "only when more than one room" gate); configPath is
// resolveSetupConfigPath(scope, id) — the exact path a fresh `init` for this
// network would write to, which may not exist on disk yet (a brand-new
// network's .moltnet/ directory is created lazily by whichever step first
// writes into it, and reading a stored Cloudflare token or relay credential
// file that does not exist yet is a normal, silent "nothing stored" case,
// not an error — see relaydeploy.LoadCloudflareToken/LoadCredentials). id is
// Q2's own answer, threaded into both branches so each can refuse a
// pre-mutation collision with the local network id — the default id itself
// for "open it up" (P1, planSetupPairOpenUp), or the invite's own NetworkID
// for "I have a code" (P2-1, planSetupPairHaveCode) — before init or any
// Cloudflare call ever runs. printCommandsOnly is threaded only to the
// "open it up" branch (P2-2): it is the one branch that would otherwise
// contact Cloudflare, or prompt for a secret, while only building a preview.
func planSetupPairing(ctx context.Context, connect setupConnectChoice, rooms []string, configPath, id string, printCommandsOnly bool) (setupPairResolution, error) {
	switch connect {
	case setupConnectOpenUp:
		plan, err := planSetupPairOpenUp(ctx, rooms, configPath, id, printCommandsOnly)
		if err != nil {
			return setupPairResolution{}, err
		}
		return setupPairResolution{openUp: &plan}, nil
	case setupConnectHaveCode:
		plan, err := planSetupPairHaveCode(ctx, id)
		if err != nil {
			return setupPairResolution{}, err
		}
		return setupPairResolution{haveCode: &plan}, nil
	default:
		return setupPairResolution{}, nil
	}
}

// setupPairInvocations renders Q7's resolved plan into the child commands
// setupPlanInvocations splices between `init` and `service install`. Both
// branches build config-mutating steps only when their plan pointer is
// actually set (setupPlanInvocations always calls this, regardless of
// answers.connect, so a zero-value setupPairResolution — Q7 = "not now" —
// must produce nothing).
func setupPairInvocations(answers setupAnswers) []childInvocation {
	switch {
	case answers.connect == setupConnectOpenUp && answers.pairing.openUp != nil:
		plan := answers.pairing.openUp
		return []childInvocation{
			buildRelayDeployInvocation(answers.configPath, plan),
			buildPairInviteInvocation(answers.configPath, plan),
			buildPairInviteShowInvocation(answers.configPath, plan),
		}
	case answers.connect == setupConnectHaveCode && answers.pairing.haveCode != nil:
		return []childInvocation{buildPairJoinInvocation(answers.configPath, answers.pairing.haveCode)}
	default:
		return nil
	}
}

// buildRelayDeployInvocation renders `moltnet relay deploy` for the "open it
// up" branch. --save-token is always passed (SETUP.md: "since the child's
// own save-prompt cannot fire under piped stdio" — a piped, non-interactive
// relay deploy never gets to ask), and --subdomain is passed only when
// plan.subdomain is non-empty: an empty subdomain means the Cloudflare
// account already had a claim (planSetupPairOpenUp adopts it silently,
// asking Q7.2 only when there was none), and relay deploy's own claim
// resolution already adopts an unrequested existing claim on its own — see
// internal/relaydeploy/deploy.go's resolveWorkersDevSubdomainClaim.
//
// --config names the exact path this run resolved (setupAnswers.configPath),
// never --id: relay deploy's own --id-based discovery
// (resolvePairConfigPath -> app.ResolveConfigPath) resolves
// ~/.moltnet/<id>/Moltnet ahead of a project-scope cwd config sharing that
// same id, and MOLTNET_CONFIG in the operator's inherited environment
// outranks --id outright — either one can silently deploy against, and save
// the Cloudflare token for (relaydeploy.CloudflareTokenPath), a different
// network than the one resolveSetupCloudflareToken just read the token from.
//
// The Cloudflare API token travels only in Env, never Args: it is the one
// thing this wizard must never let reach argv (visible via `ps`, and
// exactly what --print-commands would otherwise echo back verbatim) — see
// childInvocation's own doc comment (runner.go) and renderChildInvocation's
// placeholder rendering (setup_plan.go).
func buildRelayDeployInvocation(configPath string, plan *setupPairOpenUpPlan) childInvocation {
	args := []string{"relay", "deploy", "--config", configPath, "--save-token"}
	if plan.subdomain != "" {
		args = append(args, "--subdomain", plan.subdomain)
	}
	return childInvocation{
		Args: args,
		Env:  []string{"CLOUDFLARE_API_TOKEN=" + plan.cloudflareToken},
	}
}

// buildPairInviteInvocation renders `moltnet pair invite`. --id here is the
// *pairing* id (pair_invite.go's own flag), minted once by planSetupPairOpenUp
// so the same id names both this invocation and buildPairInviteShowInvocation
// below — required for --print-commands to print a `pair invite show`
// command that actually names a real pairing, and for the real run to
// retrieve the exact invite it just wrote. --config disambiguates which
// local network this pairing belongs to by the exact path this run resolved
// (setupAnswers.configPath), rather than --network-id: `pair invite`'s own
// --network-id-based discovery (resolvePairConfigPath) resolves
// ~/.moltnet/<id>/Moltnet ahead of a project-scope cwd config sharing that
// id, and MOLTNET_CONFIG in the operator's inherited environment outranks it
// outright — either one can silently write this pairing into a different
// network's config than the one setup just decided on (see
// buildPairJoinInvocation for the join-side mirror of this).
func buildPairInviteInvocation(configPath string, plan *setupPairOpenUpPlan) childInvocation {
	return childInvocation{Args: []string{
		"pair", "invite",
		"--id", plan.pairingID,
		"--room", plan.room,
		"--config", configPath,
	}}
}

// buildPairInviteShowInvocation renders `moltnet pair invite show
// <pairing-id>`: the wizard's only way to retrieve the code (child stdout is
// suppressed everywhere else — SETUP.md's "Exception: the invite code").
// CaptureOutput is set so runSetupStep (setup_execute.go) keeps this one
// step's stdout instead of discarding it. --config names the exact path this
// run resolved, for the same reason buildPairInviteInvocation above passes
// it instead of --id/--network-id.
func buildPairInviteShowInvocation(configPath string, plan *setupPairOpenUpPlan) childInvocation {
	return childInvocation{
		Args:          []string{"pair", "invite", "show", plan.pairingID, "--config", configPath},
		CaptureOutput: true,
	}
}

// buildPairJoinInvocation renders `moltnet pair '<code>'` for the "I have an
// invite code" branch. The code travels in Args, not Env: unlike the
// Cloudflare API token, it is the exact same shape `pair <code>` already
// accepts as a positional argument everywhere else in this CLI
// (pair_join.go) — it is a one-time, already-scoped bearer credential for a
// pairing that does not exist until this very call creates it, not an
// ambient account-wide secret like the Cloudflare token, and printing it
// back via --print-commands is exactly as safe (or unsafe) as printing the
// `pair invite` command that produced it in the first place. --config names
// the exact path this run resolved rather than --id, for the same
// ambient-environment/--id-precedence reasons buildPairInviteInvocation's
// doc comment gives.
func buildPairJoinInvocation(configPath string, plan *setupPairHaveCodePlan) childInvocation {
	return childInvocation{Args: []string{"pair", plan.code, "--config", configPath}}
}
