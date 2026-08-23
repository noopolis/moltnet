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
		subdomain   = flags.String("subdomain", "", "claim this workers.dev subdomain for the Cloudflare account non-interactively, instead of the interactive prompt (required when stdin/stdout are not both terminals and the account has no existing claim); claiming a workers.dev subdomain is PERMANENT for the account and Cloudflare allows exactly one, ever — passing this flag is your explicit confirmation of that claim. Ignored (adopted silently) if it matches an existing claim; refused if it names a different one")
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

	// Same rationale as --token-env above: an invalid --subdomain name is a
	// config-independent usage error, caught here rather than surfaced from
	// deep inside relaydeploy.Deploy after a header already printed.
	// Normalizing (lowercasing) here, once, is what lets deployOpts.Subdomain
	// below and the equality checks inside relaydeploy.Deploy assume a
	// canonical form — the same rule
	// attemptInteractiveWorkersDevSubdomainClaim applies to a typed name
	// (relay_deploy_subdomain_claim.go).
	//
	// A whitespace-only --subdomain "   " (given but blank after trimming)
	// is rejected outright rather than silently treated as though the flag
	// were absent -- before this fix, that value trimmed to "" here, the
	// run then behaved exactly as if --subdomain had never been passed, and
	// died on the generic unclaimed-subdomain error with no hint the flag
	// itself had been ignored. NormalizeWorkersDevSubdomainName only ever
	// lowercases (never blanks a non-whitespace value), so an empty result
	// here is unambiguously "trimmed to nothing," never a name the
	// subdomain-shape validation below would have rejected for some other
	// reason.
	rawSubdomain := *subdomain
	subdomainName := relaydeploy.NormalizeWorkersDevSubdomainName(strings.TrimSpace(rawSubdomain))
	if subdomainName == "" && rawSubdomain != "" {
		return fmt.Errorf("--subdomain: must not be blank")
	}
	if subdomainName != "" {
		if err := relaydeploy.ValidateWorkersDevSubdomainName(subdomainName); err != nil {
			return fmt.Errorf("--subdomain: %w", err)
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
		Subdomain:       subdomainName,
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
	// result.Claimed is the non-interactive --subdomain counterpart: a
	// first-ever, permanent, account-level workers.dev claim performed by
	// this exact call, made unconditionally (not --verbose only, matching
	// the interactive path's own always-on receipt above) since adopting an
	// existing claim is otherwise indistinguishable from making a new,
	// irreversible one. Never true at the same time as claimedSubdomainName
	// being non-empty: that variable is only ever set by the interactive
	// claim prompt, whose own redeploy call passes no --subdomain and so
	// only ever adopts (relaydeploy.Deploy's own Claimed doc comment).
	if result.Claimed {
		printInitConfigCheckLine(fmt.Sprintf("claimed workers.dev subdomain %q", result.Subdomain), "")
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

// abbreviatePathInMessage and abbreviatePathError live in
// relay_deploy_path_abbrev.go -- split out to keep this file under the
// repo's 400-line limit.
