package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
)

// This file holds the text-building helpers `relay deploy` (relay_deploy.go)
// prints for its various guidance paths — missing token, workers.dev
// subdomain unclaimed (and its interactive-claim/propagation-lag variants),
// the --print-manual equivalent, and the post-deploy "Next:" block — kept
// separate from runRelayDeploy's orchestration so both files stay under the
// repo's 400-line split threshold.

const cloudflareDashboardTokenURL = "https://dash.cloudflare.com/profile/api-tokens"

// cloudflareTokenTemplateName pre-fills the "Token name" field on the
// Cloudflare API-token-creation deep link below.
const cloudflareTokenTemplateName = "moltnet-relay-deploy"

// cloudflareTokenDeepLinkPermissions is the permissionGroupKeys JSON for
// Account > Workers Scripts > Edit — the one permission group
// `moltnet relay deploy` actually needs (script upload, RELAY_TOKEN secret,
// workers.dev route; see internal/relaydeploy/client.go and deploy.go). It
// matches Cloudflare's own minimal "Workers development" token-template deep
// link verbatim:
// https://developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/
const cloudflareTokenDeepLinkPermissions = `[{"key":"workers_scripts","type":"edit"}]`

// buildCloudflareTokenDeepLink returns a Cloudflare API-token-creation URL
// that pre-selects the Account > Workers Scripts > Edit permission group and
// pre-fills the token name, following Cloudflare's documented user-token
// template URL format (same reference as above):
//
//	https://dash.cloudflare.com/profile/api-tokens?permissionGroupKeys=<url-encoded JSON>&accountId=*&zoneId=all&name=<name>
//
// A pure function, kept separate from buildMissingCloudflareTokenGuidance so
// its exact encoded output can be pinned by a unit test.
func buildCloudflareTokenDeepLink(name string) string {
	return fmt.Sprintf("%s?permissionGroupKeys=%s&accountId=%s&zoneId=%s&name=%s",
		cloudflareDashboardTokenURL,
		url.QueryEscape(cloudflareTokenDeepLinkPermissions),
		url.QueryEscape("*"),
		url.QueryEscape("all"),
		url.QueryEscape(name),
	)
}

// printRelayDeployNextSteps prints the "Next:" block a successful `moltnet
// relay deploy` ends with: a filled-in `pair invite` command for the
// resolved network, or (P1-4) a re-init nudge when the network is still on
// the default id, since `pair invite` refuses to run against it (two
// default installs would collide) and printing it here would hand out a
// command that can never succeed. Split out from runRelayDeploy so the
// branch can be exercised directly without a real Cloudflare deploy.
func printRelayDeployNextSteps(networkID string) {
	if networkID == app.DefaultNetworkID {
		printNextSteps([]nextStep{
			{
				command:     "moltnet init --id <network-id>",
				description: "re-init with a real network id before pairing",
			},
		})
		return
	}

	printNextSteps([]nextStep{
		{
			command:     fmt.Sprintf("moltnet pair invite --network-id %s --room chat", networkID),
			description: "invite a friend over this relay",
		},
	})
}

// buildMissingCloudflareTokenGuidance is what `relay deploy` prints when
// CLOUDFLARE_API_TOKEN is not set: a Cloudflare token-creation deep link
// that pre-selects the one permission group the deploy flow needs (Account >
// Workers Scripts > Edit — see cloudflareTokenDeepLinkPermissions above),
// with the plain dashboard URL kept as a manual fallback. On a TTY, an extra
// dim hint line explains what the pre-filled page looks like; piped output
// (tests, scripts, CI logs) skips it.
func buildMissingCloudflareTokenGuidance(id string) string {
	var b strings.Builder
	b.WriteString("CLOUDFLARE_API_TOKEN is not set.\n\n")
	b.WriteString(cloudflareTokenCreationGuidance())
	b.WriteString("\nOr create one manually at:\n")
	fmt.Fprintf(&b, "  %s\n\n", cloudflareDashboardTokenURL)
	b.WriteString("Required scope:\n")
	b.WriteString("  - Account > Workers Scripts > Edit\n\n")
	b.WriteString("Then export it and retry:\n")
	b.WriteString("  export CLOUDFLARE_API_TOKEN=...\n")
	fmt.Fprintf(&b, "  moltnet relay deploy --id %s\n", id)
	return b.String()
}

// cloudflareTokenCreationGuidance is the token-creation guidance essentials
// shared by buildMissingCloudflareTokenGuidance's non-interactive block and
// maybePromptForCloudflareToken's interactive prompt intro: the deep link
// pre-selecting the one required permission group, plus (on a TTY) a dim
// hint about what the pre-filled page looks like. Kept separate so both
// callers stay byte-identical for this shared portion rather than drifting
// apart over time.
func cloudflareTokenCreationGuidance() string {
	var b strings.Builder
	b.WriteString("Create a Cloudflare API token (pre-filled with the required permission):\n")
	fmt.Fprintf(&b, "  %s\n", buildCloudflareTokenDeepLink(cloudflareTokenTemplateName))
	if isOutputTerminal() {
		fmt.Fprintf(&b, "  %s\n", dim("(opens pre-filled — just Continue → Create Token → copy)"))
	}
	return b.String()
}

// buildWorkersDevSubdomainClaimIntro is printed once, immediately before
// runRelayDeploy's interactive subdomain claim prompt
// (attemptInteractiveWorkersDevSubdomainClaim): it explains why the prompt
// is happening at all before the operator is asked to type a name.
func buildWorkersDevSubdomainClaimIntro() string {
	return "This Cloudflare account has not claimed a workers.dev subdomain yet; claim one now:\n"
}

// buildWorkersDevSubdomainGuidance is what `relay deploy` prints on
// relaydeploy.ErrWorkersDevSubdomainUnclaimed once there is nothing more to
// try automatically: either the run was non-interactive to begin with, or
// it was interactive but the claim prompt itself
// (attemptInteractiveWorkersDevSubdomainClaim) did not succeed within its
// retry budget. The one-time dashboard step is always the correct fallback
// in both cases, so the same text covers both.
func buildWorkersDevSubdomainGuidance(id string) string {
	return fmt.Sprintf(`This Cloudflare account has not claimed a workers.dev subdomain yet. Run
this command interactively (both stdin and stdout attached to a terminal)
and you will be prompted to claim one in place. Otherwise, claim it by hand:

  1. Open https://dash.cloudflare.com and choose this account
  2. Go to Workers & Pages
  3. Claim (or confirm) this account's workers.dev subdomain

Then rerun: moltnet relay deploy --id %s
`, id)
}

// buildWorkersDevSubdomainClaimLagGuidance is what `relay deploy` prints
// instead of buildWorkersDevSubdomainGuidance when the interactive claim
// prompt's PUT already succeeded (name was actually claimed), but the
// redeploy that followed still failed with
// relaydeploy.ErrWorkersDevSubdomainUnclaimed: Cloudflare's account-level
// subdomain claim can take a moment to become visible to a subsequent read
// (read-after-write lag), so this is not the "never claimed anything"
// state buildWorkersDevSubdomainGuidance describes — printing that generic
// guidance here would send the operator back to the dashboard to claim a
// name that is, in fact, already theirs.
func buildWorkersDevSubdomainClaimLagGuidance(name, id string) string {
	return fmt.Sprintf("claimed %q; Cloudflare can take a moment to propagate — rerun `moltnet relay deploy --id %s` in a minute.\n", name, id)
}

func buildRelayDeployManual(scriptName string) string {
	return fmt.Sprintf(`Equivalent manual steps (wrangler), run from relay/:

  npm install
  npx wrangler login
  npx wrangler deploy --name %s
  npx wrangler secret put RELAY_TOKEN --name %s

If this Cloudflare account has never claimed a workers.dev subdomain, claim
one in the dashboard (Workers & Pages) before the *.workers.dev route is
reachable.
`, scriptName, scriptName)
}
