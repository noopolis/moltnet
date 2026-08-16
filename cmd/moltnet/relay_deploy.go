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
		verboseFlag = flags.Bool("verbose", false, "print full detail: per-step checkmarks, stored-token source, credential paths, the rotation warning")
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
	verbose := *verboseFlag

	// Validated before any config load or output, including the "Deploying
	// relay for <id>" header below: --token-env naming an empty/unset
	// variable is an immediate, config-independent usage error, and printing
	// a header ahead of it would suggest a deploy attempt actually started
	// (P3 no-header-on-immediate-error fix).
	var existingToken string
	if strings.TrimSpace(*tokenEnv) != "" {
		existingToken = strings.TrimSpace(os.Getenv(*tokenEnv))
		if existingToken == "" {
			return fmt.Errorf("environment variable %q named by --token-env is empty or not set", *tokenEnv)
		}
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

	// Everything from here on is an actual attempt to deploy (never
	// --print-manual or --forget-token, both already returned above), so
	// this is the one place the "Deploying relay for <id>" header prints.
	// sections then tracks blank-line separation between whichever of this
	// run's phases — corrupt-token warning, token prompt, subdomain claim,
	// results, relay url, token save — actually produce output.
	fmt.Fprintf(stdout, "  Deploying relay for %s\n\n", config.NetworkID)
	sections := &sectionPrinter{}

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
		sections.start()
		// P2-3: loadErr's message embeds tokenPath as a raw absolute path
		// (relaydeploy.LoadCloudflareToken's own decode error) — abbreviate
		// it for display here, at the presentation layer, rather than in
		// internal/relaydeploy: that package's own tests pin the raw path in
		// what it returns to its callers.
		fmt.Fprintf(stdout, "  %s %s\n", yellow("warning:"), abbreviatePathInMessage(loadErr, tokenPath))
		storedToken, storedTokenOK = "", false
	}
	apiToken, tokenSource := resolveCloudflareAPIToken(envToken, storedToken, storedTokenOK)
	if apiToken == "" {
		pastedToken, promptErr := maybePromptForCloudflareToken(sections)
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
			sections.start()
			fmt.Fprint(stdout, buildMissingCloudflareTokenGuidance(config.NetworkID))
			return errors.New("CLOUDFLARE_API_TOKEN is not set")
		}
		apiToken, tokenSource = pastedToken, cloudflareTokenSourcePasted
	}

	var storedTokenPathForDeploy string
	if tokenSource == cloudflareTokenSourceStored {
		storedTokenPathForDeploy = tokenPath
		if verbose {
			sections.start()
			fmt.Fprintln(stdout, dim(fmt.Sprintf("  using stored Cloudflare API token from %s", abbreviateHome(tokenPath))))
		}
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
	deploySpinner := startSpinner("deploying relay…")
	result, err := relaydeploy.Deploy(ctx, client, deployOpts)
	deploySpinner.Stop()
	var claimedSubdomainName string
	var claimPropagationPending bool
	if err != nil && errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) && isInteractive() && isOutputTerminal() {
		sections.start()
		fmt.Fprint(stdout, buildWorkersDevSubdomainClaimIntro())
		// result.AccountID is already resolved here even though this Deploy
		// call itself failed: every step that can return
		// ErrWorkersDevSubdomainUnclaimed runs after account resolution
		// (see Result's doc comment, deploy.go) — attemptInteractiveWorkersDevSubdomainClaim
		// reuses it instead of resolving the account a second time.
		name, ok, pending := attemptInteractiveWorkersDevSubdomainClaim(ctx, client, result.AccountID, sections)
		if ok {
			claimedSubdomainName = name
			// The claim succeeded; re-run Deploy in full rather than trying
			// to resume mid-flight — see attemptInteractiveWorkersDevSubdomainClaim's
			// doc comment for why that's the simplest correct choice here.
			redeploySpinner := startSpinner("deploying relay…")
			result, err = relaydeploy.Deploy(ctx, client, deployOpts)
			redeploySpinner.Stop()
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
				sections.start()
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
				sections.start()
				fmt.Fprint(stdout, buildWorkersDevSubdomainGuidance(config.NetworkID))
			}
		}
		// P2-3: when this deploy used a stored per-network token
		// (storedTokenPathForDeploy set) and Cloudflare rejected it,
		// wrapStoredTokenError (internal/relaydeploy/deploy.go) already named
		// the file in err's message using its raw absolute path — abbreviate
		// that here, at the presentation layer, rather than in
		// internal/relaydeploy itself: that package's own tests pin the raw
		// path in what it returns to its callers, so the returned error
		// value only gets rewritten when it actually mentions the path (a
		// no-op, and err returned unchanged, for every other error shape,
		// including relaydeploy.ErrWorkersDevSubdomainUnclaimed above).
		return abbreviatePathError(err, storedTokenPathForDeploy)
	}

	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{URL: result.URL, Token: result.Token, ScriptName: result.ScriptName}); err != nil {
		return fmt.Errorf("deployed relay Worker %q but failed to save relay credentials: %w", result.ScriptName, err)
	}

	// When a claim just happened, attemptInteractiveWorkersDevSubdomainClaim
	// already opened this results block with its own "✓ claimed ..." line
	// (and its own leading blank line via sections.start()); the checkmarks
	// below (verbose only) continue that same block, not a new one.
	if claimedSubdomainName == "" {
		sections.start()
	}
	printedStepLines := false
	if verbose {
		printInitConfigCheckLine(fmt.Sprintf("deployed relay Worker %q", result.ScriptName), "")
		printInitConfigCheckLine("saved relay credentials", abbreviateHome(credentialsPath))
		printedStepLines = true
	}
	if claimedSubdomainName != "" || printedStepLines {
		sections.start()
	}
	// P2-3: the relay URL itself must stay at full contrast — it is the
	// value an operator copies out of this line — so it is never dimmed or
	// abbreviated, unlike the "extra" column printInitConfigCheckLine dims.
	fmt.Fprintf(stdout, "  %s relay live   %s\n", green("✓"), result.URL)
	if !result.HostnameResolved {
		// P2-5: name the detected hostname itself, not just the generic
		// "DNS can take a few minutes" reason — the guide's own prose
		// ("it says so") promises this note names what isn't resolving yet.
		// A detected condition, so it always prints, --verbose or not.
		fmt.Fprintf(stdout, "    %s %s is not resolving yet — workers.dev DNS can take a few minutes; retry `moltnet pair invite` shortly if it fails\n", yellow("note:"), result.Hostname)
	}
	if verbose {
		fmt.Fprintf(stdout, "  %s rotating RELAY_TOKEN (redeploying with a new --token-env value) breaks every pairing on this relay at once\n", yellow("warning:"))
	}

	switch tokenSource {
	case cloudflareTokenSourceEnv:
		if err := maybeSaveCloudflareToken(sections, tokenPath, apiToken, *saveToken, storedTokenOK); err != nil {
			return fmt.Errorf("deployed relay Worker %q but failed to save the Cloudflare API token: %w", result.ScriptName, err)
		}
	case cloudflareTokenSourceStored:
		// The token this deploy used already lives at tokenPath (that's what
		// "stored" means); --save-token has nothing new to persist. Say so
		// instead of silently doing nothing, so the flag never looks like it
		// was ignored — a --verbose-only note, since it is informational,
		// not actionable.
		if *saveToken && verbose {
			sections.start()
			fmt.Fprintf(stdout, "  %s token already stored at %s; nothing to save\n", yellow("note:"), abbreviateHome(tokenPath))
		}
	case cloudflareTokenSourcePasted:
		if err := maybeSavePastedCloudflareToken(sections, tokenPath, apiToken, *saveToken); err != nil {
			return fmt.Errorf("deployed relay Worker %q but failed to save the Cloudflare API token: %w", result.ScriptName, err)
		}
	}

	printRelayDeployNextSteps(config.NetworkID)
	return nil
}

// abbreviatePathInMessage returns err's message with every occurrence of
// rawPath rewritten to its ~-abbreviated form (abbreviateHome), for display
// only (P2-3). This is a presentation-layer transform: it never changes what
// relaydeploy itself returns to its own callers — internal/relaydeploy's own
// tests pin the raw absolute path in the errors it returns — it only cleans
// up the copy this CLI prints or returns as its own top-level error, so a
// path shown here reads consistently with every other ~-abbreviated path in
// this output. A targeted string replace, not a general parser: err or an
// empty rawPath returns err's message (or "") unchanged, and a message that
// never mentions rawPath comes back byte-identical.
func abbreviatePathInMessage(err error, rawPath string) string {
	if err == nil {
		return ""
	}
	if rawPath == "" {
		return err.Error()
	}
	abbreviated := abbreviateHome(rawPath)
	if abbreviated == rawPath {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), rawPath, abbreviated)
}

// abbreviatePathError is abbreviatePathInMessage's error-returning
// counterpart, for a returned (rather than printed) error: the rejected-
// stored-token error wrapStoredTokenError (internal/relaydeploy/deploy.go)
// produces, which this CLI returns rather than printing directly, so
// whatever eventually prints it (main.go's top-level error path) shows the
// abbreviated path too. Returns err itself, unchanged, whenever rawPath is
// empty (no stored token was in play) or never appears in err's message —
// which also means every unrelated error (including
// relaydeploy.ErrWorkersDevSubdomainUnclaimed, handled just above every call
// site of this function) passes through with its errors.Is/errors.As chain
// intact; only the one shape that actually names rawPath gets rebuilt as a
// plain error carrying the rewritten text.
func abbreviatePathError(err error, rawPath string) error {
	if err == nil || rawPath == "" {
		return err
	}
	rewritten := abbreviatePathInMessage(err, rawPath)
	if rewritten == err.Error() {
		return err
	}
	return errors.New(rewritten)
}
