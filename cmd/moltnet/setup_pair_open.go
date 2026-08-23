package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// setupPairOpenUpPlaceholderSubdomain is what
// buildRelayDeployInvocation's printed `--subdomain` argument reads as under
// --print-commands: planSetupPairOpenUp never contacts Cloudflare in that
// mode (P2-2 below), so whether the account already has a claim -- the one
// fact that decides whether a real run would pass --subdomain at all -- is
// simply unknown, and this placeholder says so rather than guessing either
// way.
const setupPairOpenUpPlaceholderSubdomain = "<name>"

// errSetupPairDefaultNetworkID is Q7 = "open it up"'s own pre-mutation
// refusal (P1): `pair invite` itself refuses outright against the default
// network id (pair_invite.go), but only *after* `relay deploy` has already
// run -- and relay deploy's own claim step is exactly the one workers.dev
// claim a Cloudflare account ever gets, permanently. Q2's own default answer
// (askSetupNetworkID, app.DefaultNetworkID) is that exact id, so an
// Enter-through-everything run reaching this branch must refuse here, before
// resolveSetupCloudflareToken or any Cloudflare call, rather than let the
// claim get burned only to fail one step later.
var errSetupPairDefaultNetworkID = errors.New(`setup: "invite a friend" needs a distinct network id`)

// TODO(setup-p2): no PTY coverage exists for any Q7 prompt in this file --
// every test drives Q7.1/Q7.2 through withPromptAnswers, so the raw-mode
// setupPromptSelect -> promptYesNo -> promptHidden interleaving this branch
// actually issues on a real terminal (Cloudflare token prompt, subdomain
// name, permanence confirm) is never exercised the way SETUP.md's
// interruption section wants for the pre-mutation secret prompt.

// setupPairOpenUpPlan is Q7 = "open it up for a friend"'s fully resolved
// plan: everything setupPairInvocations (setup_pair.go) needs to build the
// `relay deploy` / `pair invite` / `pair invite show` child commands.
type setupPairOpenUpPlan struct {
	// cloudflareToken is never printed and never placed in argv — see
	// buildRelayDeployInvocation's doc comment. It is carried here, in
	// memory, only long enough to reach that one invocation's Env.
	cloudflareToken string
	// subdomain is the workers.dev name to pass as `relay deploy
	// --subdomain`, or "" when the Cloudflare account already had a claim
	// (Q7.2 is then skipped entirely — SETUP.md: "Existing claim → adopt
	// silently").
	subdomain string
	// room is Q7.1's resolved answer: the one room `pair invite --room`
	// shares.
	room string
	// pairingID is minted once, up front, so the same id names both the
	// `pair invite --id` and `pair invite show` invocations.
	pairingID string
}

// planSetupPairOpenUp implements Q7 = "open it up"'s full flow: refuse
// outright against the default network id (P1, before anything else —
// errSetupPairDefaultNetworkID above), resolve a Cloudflare API token
// (hosting the secret entry itself, since a piped child can never prompt for
// one — SETUP.md's "Driving the terminal" precondition 3), check whether the
// account already has a workers.dev claim, ask Q7.2 only when it does not,
// then ask Q7.1 (only when rooms has more than one entry), and mint a
// pairing id up front.
//
// printCommandsOnly (P2-2) skips the token resolution and the live
// VerifyToken/ResolveAccountID/WorkersDevSubdomain calls entirely — a
// --print-commands preview must never contact Cloudflare or demand a secret
// the operator has no way to supply mid-preview — substituting
// setupPairOpenUpPlaceholderSubdomain for the unknown claim state instead of
// guessing it.
func planSetupPairOpenUp(ctx context.Context, rooms []string, configPath, id string, printCommandsOnly bool) (setupPairOpenUpPlan, error) {
	if id == app.DefaultNetworkID {
		return setupPairOpenUpPlan{}, fmt.Errorf(
			"%w — the default %q would let `relay deploy` burn this Cloudflare account's one-ever workers.dev claim before `pair invite` ever gets a chance to refuse it; rerun and give this network a distinct name at \"Name it\"",
			errSetupPairDefaultNetworkID, app.DefaultNetworkID,
		)
	}

	var token string
	var claimed bool
	if !printCommandsOnly {
		var err error
		token, err = resolveSetupCloudflareToken(configPath)
		if err != nil {
			return setupPairOpenUpPlan{}, err
		}
		claimed, err = checkSetupWorkersDevClaimed(ctx, token)
		if err != nil {
			return setupPairOpenUpPlan{}, err
		}
	}

	subdomain := ""
	switch {
	case printCommandsOnly:
		subdomain = setupPairOpenUpPlaceholderSubdomain
	case !claimed:
		// Flushed here, right before Q7.2's name prompt, and again inside
		// askSetupWorkersDevSubdomain itself right before its permanence
		// confirm (setup_pair_open.go's own call site there) -- input queued
		// behind a fast Enter-through name answer could otherwise still be
		// sitting in the terminal's input buffer when that confirm reads,
		// exactly the class of bug flushPendingTerminalInput exists to
		// prevent for the first prompt in this sequence.
		flushPendingTerminalInput()
		var err error
		subdomain, err = askSetupWorkersDevSubdomain(ctx)
		if err != nil {
			return setupPairOpenUpPlan{}, err
		}
	}

	room, err := askSetupShareRoom(ctx, rooms)
	if err != nil {
		return setupPairOpenUpPlan{}, err
	}

	// TODO(setup-p2): pairingID is always freshly minted here, even when this
	// is a re-run recovering from an interruption between a prior run's
	// `pair invite` and `pair invite show` steps (executeSetupPlan,
	// setup_execute.go) — SETUP.md's whole rationale for the invite receipt
	// (`pair invite show <id>`) is recovering exactly that interrupted
	// window, but this plan never looks for an already-committed, unshown
	// pairing to resume; it always generates a second one instead. Fixing
	// this needs Q7-specific interruption state this wizard does not
	// otherwise keep (existing.found only tracks the network config itself),
	// so it is out of scope here.
	pairingID, err := defaultPairingID()
	if err != nil {
		return setupPairOpenUpPlan{}, err
	}

	return setupPairOpenUpPlan{
		cloudflareToken: token,
		subdomain:       subdomain,
		room:            room,
		pairingID:       pairingID,
	}, nil
}

// resolveSetupCloudflareToken resolves the Cloudflare API token "open it
// up" needs, in the same precedence `relay deploy` itself uses
// (resolveCloudflareAPIToken, relay_deploy_token.go): CLOUDFLARE_API_TOKEN
// env first, then a previously stored per-network token
// (relaydeploy.CloudflareTokenPath), and only then an interactive prompt —
// via promptHidden directly, per SETUP.md ("the wizard hosts all secret
// entry itself"), not relay deploy's own maybePromptForCloudflareToken:
// that helper additionally gates on isOutputTerminal(), which setup itself
// does not require (only stdin needs to be a real terminal — runSetup
// already refused otherwise before any question was ever asked), and it
// would print relay-deploy-specific guidance framed around that command
// rather than this wizard's own question flow.
func resolveSetupCloudflareToken(configPath string) (string, error) {
	tokenPath := relaydeploy.CloudflareTokenPath(configPath)
	envToken := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))

	storedToken, storedTokenOK, err := relaydeploy.LoadCloudflareToken(tokenPath)
	if err != nil {
		// A corrupt/unreadable stored token file must not block this
		// question when a working env token or the interactive prompt below
		// can still supply one — mirrors relay deploy's own tolerance for
		// exactly this failure (relay_deploy.go).
		fmt.Fprintf(stdout, "  %s %v\n", yellow("warning:"), err)
		storedToken, storedTokenOK = "", false
	}
	if token, _ := resolveCloudflareAPIToken(envToken, storedToken, storedTokenOK); token != "" {
		return token, nil
	}

	// Uses relay deploy's own shared guidance (relay_deploy_guidance.go)
	// rather than a second, hand-written line: that one prints the deep link
	// with the required permission group already selected and the token name
	// pre-filled, so the operator clicks Continue → Create Token → copy
	// instead of hunting for the right template by hand. Setup previously
	// printed a bare dashboard URL here, which sent the operator to an empty
	// token form and left them to find the permission themselves.
	fmt.Fprint(stdout, cloudflareTokenCreationGuidance())
	pasted, err := promptHidden("  paste token (input hidden): ")
	if err != nil && !errors.Is(err, errTerminalEchoUnavailable) {
		return "", err
	}
	if pasted == "" {
		return "", fmt.Errorf(`"invite a friend" needs a Cloudflare API token to deploy the relay; rerun once one is ready, or choose "not now"`)
	}
	return pasted, nil
}

// checkSetupWorkersDevClaimed resolves whether the Cloudflare account behind
// token already has a workers.dev subdomain claim — Q7.2's own gate
// (SETUP.md: "asked only when the Cloudflare account has no existing
// claim"). It contacts Cloudflare directly (VerifyToken, then
// ResolveAccountID, then WorkersDevSubdomain) through the same injectable
// newRelayDeployClient seam `relay deploy` itself uses (relay_deploy.go),
// so tests point this at a fake server exactly the way relay deploy's own
// tests do, never a real Cloudflare account. This is a read-only check: the
// actual claim, when one is needed, is left entirely to the `relay deploy
// --subdomain` child invocation (internal/relaydeploy/deploy.go's own
// atomic validate-then-claim, landed as this phase's Unit 0) rather than
// duplicated here.
func checkSetupWorkersDevClaimed(ctx context.Context, token string) (bool, error) {
	client := newRelayDeployClient(token)
	if err := client.VerifyToken(ctx); err != nil {
		return false, fmt.Errorf("verify Cloudflare API token: %w", err)
	}
	accountID, err := client.ResolveAccountID(ctx)
	if err != nil {
		return false, fmt.Errorf("resolve Cloudflare account: %w", err)
	}
	_, claimed, err := client.WorkersDevSubdomain(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("resolve workers.dev subdomain: %w", err)
	}
	return claimed, nil
}

// askSetupWorkersDevSubdomain implements Q7.2: typed, validated locally
// (relaydeploy.ValidateWorkersDevSubdomainName — the exact rule `relay
// deploy`'s own interactive claim prompt uses, relay_deploy_subdomain_claim.go),
// with an explicit permanence confirmation before the name is accepted
// (SETUP.md: "state that the claim is permanent for the account"). The
// actual claim PUT is left to the `relay deploy --subdomain` child
// invocation this plans toward — never issued here — so a name confirmed at
// this prompt and a name actually claimed later are always the same single
// code path (internal/relaydeploy/deploy.go), not two.
//
// maxWorkersDevSubdomainClaimAttempts is relay_deploy_subdomain_claim.go's
// own constant, reused rather than duplicated: the two prompts are
// different questions (this one never touches the network), but bounding
// both the same way keeps a mistyped or declined name from looping forever.
//
// TODO(setup-p2): a name confirmed here is not re-validated against
// Cloudflare's own registry (ValidateWorkersDevSubdomainName is a local
// shape check only) until the `relay deploy --subdomain` step actually
// runs, which can be well after this prompt (init, and possibly other
// questions, still lie between the two). If Cloudflare rejects it then as
// already taken (error 10031), the failure surfaces as deploy.go's raw
// "claim workers.dev subdomain %q: %w" — this function never loops back to
// offer a different name the way relay deploy's own interactive claim
// prompt does (relay_deploy_subdomain_claim.go), and `init` has already run
// by then, unlike every other refusal in this wizard.
func askSetupWorkersDevSubdomain(ctx context.Context) (string, error) {
	for attempt := 1; attempt <= maxWorkersDevSubdomainClaimAttempts; attempt++ {
		name, err := setupPromptLineCtx(ctx, "  choose a workers.dev subdomain (yours forever, e.g. \"acme\"): ")
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		name = relaydeploy.NormalizeWorkersDevSubdomainName(strings.TrimSpace(name))
		if name == "" {
			return "", fmt.Errorf("a workers.dev subdomain name is required to open this network up")
		}
		if err := relaydeploy.ValidateWorkersDevSubdomainName(name); err != nil {
			fmt.Fprintf(stdout, "  %s %v\n", yellow("note:"), err)
			continue
		}

		// Flushed again here, right before this permanence confirm reads --
		// see planSetupPairOpenUp's own flushPendingTerminalInput call site
		// (right before this function's name prompt above) for why: input
		// queued behind a fast Enter-through name answer could otherwise
		// still be sitting in the terminal's buffer by the time this second
		// prompt reads, silently answering "proceed?" with whatever was
		// typed ahead rather than a deliberate answer to this specific,
		// PERMANENT confirmation.
		flushPendingTerminalInput()
		confirmed, err := promptYesNo(fmt.Sprintf(
			"  %s claiming %q is PERMANENT for this Cloudflare account — Cloudflare allows exactly one, ever. proceed? [y/N] ",
			yellow("warning:"), name,
		))
		if err != nil {
			return "", err
		}
		if !confirmed {
			continue
		}
		printInitConfigCheckLine(fmt.Sprintf("workers.dev subdomain %q", name), "")
		return name, nil
	}
	return "", fmt.Errorf("no workers.dev subdomain name confirmed after %d attempts", maxWorkersDevSubdomainClaimAttempts)
}

// printSetupPairOpenUpPrintCommandsNote documents --print-commands' one
// known inaccuracy for the "open it up" branch: the printed `relay deploy`
// line always carries `--subdomain <name>` — setupPairOpenUpPlaceholderSubdomain
// substitutes for the unknown claim state, since --print-commands never
// contacts Cloudflare to find out (P2-2 above) — but a real run against an
// already-claimed Cloudflare account omits the flag entirely
// (buildRelayDeployInvocation, setup_pair.go). Deliberate and harmless to
// paste verbatim (the placeholder name fails local validation rather than
// claiming anything), but silently misleading without this caveat printed
// directly under the commands block.
func printSetupPairOpenUpPrintCommandsNote(answers setupAnswers) {
	if answers.connect != setupConnectOpenUp || answers.pairing.openUp == nil {
		return
	}
	fmt.Fprintln(stdout, dim("  --subdomain is only needed if this Cloudflare account has never claimed a workers.dev subdomain"))
}

// askSetupShareRoom implements Q7.1, asked only when rooms has more than one
// entry (SETUP.md: "asked only when Q5 produced more than one room"); a
// single room is used without asking, and an empty rooms slice (should not
// happen — Q5 always yields at least one — but defended anyway) falls back
// to the starter room id.
func askSetupShareRoom(ctx context.Context, rooms []string) (string, error) {
	switch len(rooms) {
	case 0:
		return starterRoomID, nil
	case 1:
		return rooms[0], nil
	}

	options := make([]selectOption, len(rooms))
	for i, room := range rooms {
		options[i] = selectOption{label: room}
	}
	index, err := setupPromptSelect(ctx, "Which room to share?", options, 0)
	if err != nil {
		return "", err
	}
	return rooms[index], nil
}
