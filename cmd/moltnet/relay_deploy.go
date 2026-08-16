package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/relaydeploy"
)

const cloudflareDashboardTokenURL = "https://dash.cloudflare.com/profile/api-tokens"

// newRelayDeployClient constructs the Cloudflare API client `relay deploy`
// deploys through. It's a var, not a direct relaydeploy.NewClient call, so
// tests can point deploys at a net/http/httptest fake Cloudflare server
// (relaydeploy.NewClientForTesting) instead of the real API.
var newRelayDeployClient = func(token string) *relaydeploy.Client {
	return relaydeploy.NewClient(token)
}

// resolveRelayDeployHostname is plumbed through to Deploy's
// Options.ResolveHostname. Nil (the default) leaves Deploy to fall back to
// its own real DNS lookup; it's a var, like newRelayDeployClient above, so
// cmd/moltnet tests can stub post-deploy DNS resolution instead of making a
// live lookup against a hostname (script.acme.workers.dev in the fake
// tests) that was never actually deployed.
var resolveRelayDeployHostname func(ctx context.Context, hostname string) bool

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

// runRelayDeploy implements `moltnet relay deploy`: it deploys the embedded,
// pre-bundled relay Worker (see relay/dist and relay/embed.go) via the
// Cloudflare REST API, using CLOUDFLARE_API_TOKEN for auth (OAuth is
// deferred; see PLAN.md).
func runRelayDeploy(args []string) error {
	flags := flag.NewFlagSet("moltnet relay deploy", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		name        = flags.String("name", relaydeploy.DefaultScriptName, "Cloudflare Worker script name")
		tokenEnv    = flags.String("token-env", "", "environment variable holding an existing RELAY_TOKEN to reuse instead of generating one")
		printManual = flags.Bool("print-manual", false, "print the equivalent wrangler steps and exit without contacting Cloudflare")
		saveToken   = flags.Bool("save-token", false, "save the Cloudflare API token used for this deploy to .moltnet/cloudflare.json for future deploys")
		forgetToken = flags.Bool("forget-token", false, "delete the Cloudflare API token stored at .moltnet/cloudflare.json and exit without deploying")
		configPath  = flags.String("config", "", "Moltnet config path")
		id          = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("relay deploy does not accept positional arguments")
	}
	if *saveToken && *forgetToken {
		return fmt.Errorf("--save-token and --forget-token cannot be used together")
	}

	scriptName := strings.TrimSpace(*name)
	if scriptName == "" {
		scriptName = relaydeploy.DefaultScriptName
	}

	if *printManual {
		fmt.Fprint(stdout, buildRelayDeployManual(scriptName))
		return nil
	}

	path, err := resolvePairConfigPath(*configPath, *id)
	if err != nil {
		return err
	}
	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return err
	}
	credentialsPath := relaydeploy.CredentialsPath(path)
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	if *forgetToken {
		return runRelayDeployForgetToken(tokenPath)
	}

	var existingToken string
	if strings.TrimSpace(*tokenEnv) != "" {
		existingToken = strings.TrimSpace(os.Getenv(*tokenEnv))
		if existingToken == "" {
			return fmt.Errorf("environment variable %q named by --token-env is empty or not set", *tokenEnv)
		}
	}

	envToken := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))

	storedToken, storedTokenOK, loadErr := relaydeploy.LoadCloudflareToken(tokenPath)
	if loadErr != nil {
		// A corrupt/unreadable stored token file must not block a deploy
		// that has a working CLOUDFLARE_API_TOKEN env override, or one that
		// can fall through to the interactive paste prompt below: warn and
		// fall back to treating nothing as stored. Only surface the load
		// error itself when neither of those is available — no env token,
		// and no real terminal to prompt on — since in that case it is the
		// only explanation for why the deploy cannot proceed.
		if envToken == "" && !(isInteractive() && isOutputTerminal()) {
			return fmt.Errorf("%w; run `moltnet relay deploy --forget-token` to remove it", loadErr)
		}
		fmt.Fprintln(stdout, yellow(fmt.Sprintf("warning: %v", loadErr)))
		storedToken, storedTokenOK = "", false
	}
	apiToken, tokenSource := resolveCloudflareAPIToken(envToken, storedToken, storedTokenOK)
	if apiToken == "" {
		pastedToken, promptErr := maybePromptForCloudflareToken()
		// errTerminalEchoUnavailable is not itself fatal — it means the
		// prompt was never even shown (echo could not be reliably disabled,
		// P0 fail-open-echo fix), so this falls through to the same
		// guidance-and-error path a non-interactive run takes, exactly as
		// if no prompt had ever been attempted. Any other error from the
		// prompt itself does propagate.
		if promptErr != nil && !errors.Is(promptErr, errTerminalEchoUnavailable) {
			return promptErr
		}
		if pastedToken == "" {
			if promptErr == nil && isInteractive() && isOutputTerminal() {
				// The guidance was already printed once, right before the
				// prompt (maybePromptForCloudflareToken); reprinting the
				// whole export-and-retry block here would just be noise
				// after the operator already saw it and chose not to paste
				// anything.
				return errors.New("no token pasted")
			}
			fmt.Fprint(stdout, buildMissingCloudflareTokenGuidance(config.NetworkID))
			return errors.New("CLOUDFLARE_API_TOKEN is not set")
		}
		apiToken, tokenSource = pastedToken, cloudflareTokenSourcePasted
	}

	var storedTokenPathForDeploy string
	if tokenSource == cloudflareTokenSourceStored {
		storedTokenPathForDeploy = tokenPath
		fmt.Fprintln(stdout, dim(fmt.Sprintf("  using stored Cloudflare API token from %s", tokenPath)))
	}

	// Only reuse a stored relay token when it belongs to this same --name: a
	// different script name means a different (soon to be deployed) Worker,
	// which must get its own fresh RELAY_TOKEN rather than inheriting
	// whichever relay was deployed most recently.
	var priorToken string
	if existing, ok, loadErr := relaydeploy.LoadCredentials(credentialsPath); loadErr != nil {
		return loadErr
	} else if ok && existing.MatchesScriptName(scriptName) {
		priorToken = existing.Token
	}

	client := newRelayDeployClient(apiToken)
	result, err := relaydeploy.Deploy(context.Background(), client, relaydeploy.Options{
		ScriptName:      scriptName,
		ExistingToken:   existingToken,
		PriorToken:      priorToken,
		ResolveHostname: resolveRelayDeployHostname,
		StoredTokenPath: storedTokenPathForDeploy,
	})
	if err != nil {
		if errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
			fmt.Fprint(stdout, buildWorkersDevSubdomainGuidance(config.NetworkID))
		}
		return err
	}

	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{URL: result.URL, Token: result.Token, ScriptName: result.ScriptName}); err != nil {
		return fmt.Errorf("deployed relay Worker %q but failed to save relay credentials: %w", result.ScriptName, err)
	}

	fmt.Fprintf(stdout, "  deployed relay Worker %q\n", result.ScriptName)
	// P2-3: the relay URL itself must stay at full contrast — it is the
	// value an operator copies out of this line — so only the "relay url:"
	// label is dimmed, not the URL.
	fmt.Fprintf(stdout, "  %s %s\n", dim("relay url:"), result.URL)
	if !result.HostnameResolved {
		fmt.Fprintf(stdout, "  %s %s is not resolving yet; workers.dev DNS can take a few minutes to propagate, retry `moltnet pair invite` shortly if it fails\n", yellow("note:"), result.Hostname)
	}
	fmt.Fprintln(stdout, dim(fmt.Sprintf("  saved relay credentials to %s", credentialsPath)))
	fmt.Fprintf(stdout, "  %s rotating RELAY_TOKEN (redeploying with a new --token-env value) breaks every pairing on this relay at once\n", yellow("warning:"))

	switch tokenSource {
	case cloudflareTokenSourceEnv:
		if err := maybeSaveCloudflareToken(tokenPath, apiToken, *saveToken, storedTokenOK); err != nil {
			return fmt.Errorf("deployed relay Worker %q but failed to save the Cloudflare API token: %w", result.ScriptName, err)
		}
	case cloudflareTokenSourceStored:
		// The token this deploy used already lives at tokenPath (that's what
		// "stored" means); --save-token has nothing new to persist. Say so
		// instead of silently doing nothing, so the flag never looks like it
		// was ignored.
		if *saveToken {
			fmt.Fprintf(stdout, "  token already stored at %s; nothing to save\n", tokenPath)
		}
	case cloudflareTokenSourcePasted:
		if err := maybeSavePastedCloudflareToken(tokenPath, apiToken, *saveToken); err != nil {
			return fmt.Errorf("deployed relay Worker %q but failed to save the Cloudflare API token: %w", result.ScriptName, err)
		}
	}

	printRelayDeployNextSteps(config.NetworkID)
	return nil
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

func buildWorkersDevSubdomainGuidance(id string) string {
	return fmt.Sprintf(`This Cloudflare account has not claimed a workers.dev subdomain yet, and
the API cannot claim one on your behalf. One-time step:

  1. Open https://dash.cloudflare.com and choose this account
  2. Go to Workers & Pages
  3. Claim (or confirm) this account's workers.dev subdomain

Then rerun: moltnet relay deploy --id %s
`, id)
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
