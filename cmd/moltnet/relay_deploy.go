package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/relaydeploy"
)

const cloudflareDashboardTokenURL = "https://dash.cloudflare.com/profile/api-tokens"

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
		configPath  = flags.String("config", "", "Moltnet config path")
		id          = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("relay deploy does not accept positional arguments")
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

	apiToken := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if apiToken == "" {
		fmt.Fprint(stdout, buildMissingCloudflareTokenGuidance(config.NetworkID))
		return errors.New("CLOUDFLARE_API_TOKEN is not set")
	}

	var existingToken string
	if strings.TrimSpace(*tokenEnv) != "" {
		existingToken = strings.TrimSpace(os.Getenv(*tokenEnv))
		if existingToken == "" {
			return fmt.Errorf("environment variable %q named by --token-env is empty or not set", *tokenEnv)
		}
	}

	// Only reuse a stored token when it belongs to this same --name: a
	// different script name means a different (soon to be deployed) Worker,
	// which must get its own fresh RELAY_TOKEN rather than inheriting
	// whichever relay was deployed most recently.
	var priorToken string
	if existing, ok, loadErr := relaydeploy.LoadCredentials(credentialsPath); loadErr != nil {
		return loadErr
	} else if ok && existing.MatchesScriptName(scriptName) {
		priorToken = existing.Token
	}

	client := relaydeploy.NewClient(apiToken)
	result, err := relaydeploy.Deploy(context.Background(), client, relaydeploy.Options{
		ScriptName:    scriptName,
		ExistingToken: existingToken,
		PriorToken:    priorToken,
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
	fmt.Fprintf(stdout, "  relay url: %s\n", result.URL)
	if !result.HostnameResolved {
		fmt.Fprintf(stdout, "  note: %s is not resolving yet; workers.dev DNS can take a few minutes to propagate, retry `moltnet pair invite` shortly if it fails\n", result.Hostname)
	}
	fmt.Fprintf(stdout, "  saved relay credentials to %s\n", credentialsPath)
	fmt.Fprintln(stdout, "  warning: rotating RELAY_TOKEN (redeploying with a new --token-env value) breaks every pairing on this relay at once")

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

func buildMissingCloudflareTokenGuidance(id string) string {
	return fmt.Sprintf(`CLOUDFLARE_API_TOKEN is not set.

Create a Cloudflare API token at:
  %s

Required scopes:
  - Account > Workers Scripts > Edit
  - User > User Details > Read

Then export it and retry:
  export CLOUDFLARE_API_TOKEN=...
  moltnet relay deploy --id %s
`, cloudflareDashboardTokenURL, id)
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
