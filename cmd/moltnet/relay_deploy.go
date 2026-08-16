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

	ctx := context.Background()
	client := newRelayDeployClient(apiToken)
	deployOpts := relaydeploy.Options{
		ScriptName:      scriptName,
		ExistingToken:   existingToken,
		PriorToken:      priorToken,
		ResolveHostname: resolveRelayDeployHostname,
		StoredTokenPath: storedTokenPathForDeploy,
	}
	result, err := relaydeploy.Deploy(ctx, client, deployOpts)
	var claimedSubdomainName string
	var claimPropagationPending bool
	if err != nil && errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) && isInteractive() && isOutputTerminal() {
		fmt.Fprint(stdout, buildWorkersDevSubdomainClaimIntro())
		// result.AccountID is already resolved here even though this Deploy
		// call itself failed: every step that can return
		// ErrWorkersDevSubdomainUnclaimed runs after account resolution
		// (see Result's doc comment, deploy.go) — attemptInteractiveWorkersDevSubdomainClaim
		// reuses it instead of resolving the account a second time.
		name, ok, pending := attemptInteractiveWorkersDevSubdomainClaim(ctx, client, result.AccountID)
		if ok {
			claimedSubdomainName = name
			// The claim succeeded; re-run Deploy in full rather than trying
			// to resume mid-flight — see attemptInteractiveWorkersDevSubdomainClaim's
			// doc comment for why that's the simplest correct choice here.
			result, err = relaydeploy.Deploy(ctx, client, deployOpts)
		} else {
			claimPropagationPending = pending
		}
	}
	if err != nil {
		if errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
			switch {
			case claimedSubdomainName != "":
				// The claim PUT itself already succeeded (claimedSubdomainName
				// was only set on a successful attemptInteractiveWorkersDevSubdomainClaim
				// call); this redeploy failing again with the same sentinel
				// means Cloudflare's own read-after-write lag, not an
				// unclaimed account — printing the generic "has not claimed"
				// guidance here would be actively misleading (P2 post-claim
				// lag messaging fix).
				fmt.Fprint(stdout, buildWorkersDevSubdomainClaimLagGuidance(claimedSubdomainName, config.NetworkID))
			case claimPropagationPending:
				// attemptInteractiveWorkersDevSubdomainClaim already printed
				// its own specific propagation-lag reason (the 10036 PUT
				// rejection whose follow-up GET recheck still reported the
				// account unclaimed); printing the generic "has not claimed
				// one yet" guidance on top of that would directly contradict
				// it, since Cloudflare has already told us the account does
				// have a subdomain.
			default:
				fmt.Fprint(stdout, buildWorkersDevSubdomainGuidance(config.NetworkID))
			}
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
