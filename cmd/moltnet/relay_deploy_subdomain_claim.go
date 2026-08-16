package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// workersDevSubdomainClaimPrompt is the interactive prompt shown when a
// deploy hits relaydeploy.ErrWorkersDevSubdomainUnclaimed on a real
// terminal (attemptInteractiveWorkersDevSubdomainClaim below). A normal
// echoed prompt, not promptHidden — the subdomain name is not a secret.
// Byte-matched by tests and docs.
const workersDevSubdomainClaimPrompt = `  choose a workers.dev subdomain (yours forever, e.g. "apresmoi"): `

// maxWorkersDevSubdomainClaimAttempts bounds the interactive claim prompt:
// a rejected name (locally invalid, or a PUT Cloudflare itself refused —
// e.g. already claimed by another account) gets one retry before this gives
// up and lets the caller fall back to the dashboard guidance, rather than
// looping forever on typos.
const maxWorkersDevSubdomainClaimAttempts = 2

// Cloudflare's two documented error codes for PUT
// /accounts/{account_id}/workers/subdomain that attemptInteractiveWorkersDevSubdomainClaim
// branches on, rather than treating every rejection identically (see
// relaydeploy.Client.ClaimWorkersDevSubdomain's doc comment for the same
// two codes from the client's side).
const (
	// cloudflareErrorSubdomainNameTaken (10031) means the name itself is
	// already claimed, by this account or another — retryable with a
	// different name.
	cloudflareErrorSubdomainNameTaken = 10031
	// cloudflareErrorAccountAlreadyHasSubdomain (10036, HTTP 409) means
	// this account has already claimed *a* workers.dev subdomain —
	// Cloudflare allows exactly one per account, ever. Not a failure: the
	// caller re-reads the existing claim and continues as already claimed.
	cloudflareErrorAccountAlreadyHasSubdomain = 10036
)

// cloudflareAPIErrorHasCode reports whether err is a
// *relaydeploy.CloudflareAPIError carrying code among its messages,
// mirroring internal/relaydeploy's own isUnclaimedWorkersDevSubdomainError
// helper (deploy.go) for the same reason: a Cloudflare error envelope can
// carry more than one message, so every one of them is checked rather than
// assuming the code of interest is first.
func cloudflareAPIErrorHasCode(err error, code int) bool {
	var apiErr *relaydeploy.CloudflareAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, message := range apiErr.Messages {
		if message.Code == code {
			return true
		}
	}
	return false
}

// attemptInteractiveWorkersDevSubdomainClaim is runRelayDeploy's response to
// relaydeploy.ErrWorkersDevSubdomainUnclaimed on an interactive terminal
// (both stdin and stdout TTYs, the same gate maybePromptForCloudflareToken
// uses): instead of only printing the dashboard guidance, it prompts for a
// workers.dev subdomain name, normalizes and validates it against
// wrangler's de-facto naming rules
// (relaydeploy.NormalizeWorkersDevSubdomainName /
// relaydeploy.ValidateWorkersDevSubdomainName), and claims it via
// client.ClaimWorkersDevSubdomain (PUT
// /accounts/{account_id}/workers/subdomain) against accountID — already
// resolved by the Deploy call that hit ErrWorkersDevSubdomainUnclaimed
// (relaydeploy.Result.AccountID), so this never re-resolves it itself. It
// returns the name that ended up claimed and whether the claim succeeded.
//
// On success, the caller (runRelayDeploy) re-runs the full Deploy rather
// than this function trying to resume mid-flight: Deploy is documented and
// tested as idempotent (see deploy.go), so retrying it whole is the
// simplest correct way to continue past the step that originally failed.
//
// A rejected name is branched on Cloudflare's own documented error code
// (see the cloudflareError* constants above): code 10031 ("name already
// taken") prints a specific message and lets the operator try a different
// name; code 10036 ("account already has a subdomain") is not treated as a
// rejection at all — the existing claim is re-read via
// client.WorkersDevSubdomain and returned as already claimed, without
// spending one of the two attempts. When that follow-up GET itself still
// reports the account as unclaimed (Cloudflare's own read-after-write lag
// on the claim it just told us about via 10036), retrying is futile — every
// further attempt will just be rejected with the same 10036 — so this prints
// the propagation-lag message once and returns immediately with
// propagationPending=true, rather than burning the remaining attempts on a
// name that was never the problem. Any other rejection — a locally-caught
// invalid shape, or any other Cloudflare error — prints the reason
// verbatim and allows one retry. Exhausting the attempts, or declining by
// closing input (EOF/Ctrl-D, which skips straight to the fallback instead
// of spending both attempts on the same empty-name validation error),
// returns ("", false, false) so the caller falls back to the standard
// dashboard guidance instead of looping forever.
//
// The third return value, propagationPending, is only ever true alongside
// claimed=false: it tells the caller (runRelayDeploy) that this function
// already printed the accurate, specific reason claiming failed, so the
// caller must not additionally print its own generic "has not claimed a
// workers.dev subdomain yet" guidance — that would contradict the message
// already shown, which says the opposite (the account does have one;
// Cloudflare just has not reported it back yet).
func attemptInteractiveWorkersDevSubdomainClaim(ctx context.Context, client *relaydeploy.Client, accountID string, sections *sectionPrinter) (name string, claimed bool, propagationPending bool) {
	// The deploy that hit ErrWorkersDevSubdomainUnclaimed just spent time
	// against the real Cloudflare API; discard anything typed during that
	// window before reading the answer, so mid-deploy typeahead (most
	// commonly a stray Enter) never silently burns the first claim attempt
	// (the same P0 mid-deploy-typeahead guard maybeSaveCloudflareToken uses
	// — relay_deploy_token.go).
	flushPendingTerminalInput()

	for attempt := 1; attempt <= maxWorkersDevSubdomainClaimAttempts; attempt++ {
		name, err := promptLine(workersDevSubdomainClaimPrompt)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Ctrl-D / closed input: the operator declined to answer at
				// all, not an invalid answer. Skip straight to the
				// dashboard-guidance fallback instead of letting the empty
				// name fall through to ValidateWorkersDevSubdomainName and
				// print "must not be empty" on every remaining attempt.
				return "", false, false
			}
			// P2-2: yellow() must wrap only the "warning:" tag, not the whole
			// line — the indent stays outside the color span, matching the
			// note:/warning: convention every other styled line in this
			// package uses.
			fmt.Fprintf(stdout, "  %s %v\n", yellow("warning:"), err)
			return "", false, false
		}
		name = relaydeploy.NormalizeWorkersDevSubdomainName(name)
		if err := relaydeploy.ValidateWorkersDevSubdomainName(name); err != nil {
			fmt.Fprintf(stdout, "  %s %v\n", yellow("note:"), err)
			continue
		}

		claimErr := client.ClaimWorkersDevSubdomain(ctx, accountID, name)
		if claimErr == nil {
			// A blank line ahead of this confirmation, not the retry
			// messages above it: it's the start of the results block the
			// caller (runRelayDeploy) prints next (deployed/saved lines),
			// not a continuation of the prompt loop.
			sections.start()
			printInitConfigCheckLine(fmt.Sprintf("claimed workers.dev subdomain %q", name), "")
			return name, true, false
		}

		if cloudflareAPIErrorHasCode(claimErr, cloudflareErrorAccountAlreadyHasSubdomain) {
			if existing, claimed, readErr := client.WorkersDevSubdomain(ctx, accountID); readErr == nil && claimed {
				sections.start()
				printInitConfigCheckLine(fmt.Sprintf("this account already has a workers.dev subdomain %q; continuing", existing), "")
				return existing, true, false
			}
			// The follow-up GET still reports the account as unclaimed —
			// Cloudflare's own read-after-write lag on the very claim it just
			// told us about via 10036. Retrying is futile: every further
			// attempt in this loop will just be rejected with the same 10036
			// again, so this stops here instead of spending the remaining
			// attempts on names that were never the problem, and returns
			// propagationPending=true so the caller prints this specific
			// reason instead of its generic (and, here, self-contradictory)
			// "has not claimed one yet" guidance.
			sections.start()
			fmt.Fprintf(stdout, "  %s account already has a subdomain; Cloudflare can take a moment to report it — rerun in a minute\n", yellow("note:"))
			return "", false, true
		}
		if cloudflareAPIErrorHasCode(claimErr, cloudflareErrorSubdomainNameTaken) {
			fmt.Fprintf(stdout, "  %s could not claim %q: that name is taken, try another\n", yellow("note:"), name)
			continue
		}
		fmt.Fprintf(stdout, "  %s could not claim %q: %v\n", yellow("warning:"), name, claimErr)
	}
	return "", false, false
}
