package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"

	"github.com/noopolis/moltnet/internal/app"
)

// errConsoleServerDown is reportConsoleServerDown's sentinel wrapped error.
// main.go matches it with errors.Is (the same pattern it already uses for
// context.Canceled) so it can exit nonzero without printing a second,
// context-free "error: ..." line on top of the fact and the exact next
// command reportConsoleServerDown already printed to stdout.
var errConsoleServerDown = errors.New("console: server not running")

// consoleHealthTimeout bounds runConsole's /healthz probe. It must stay
// short: a hung or unreachable server should fail fast rather than making
// an operator wait before seeing the "server not running" guidance.
const consoleHealthTimeout = 1500 * time.Millisecond

// consoleHTTPClient is the client probeConsoleHealth issues its GET
// through. It carries no client-level Timeout of its own — the per-call
// context deadline (consoleHealthTimeout, applied in probeConsoleHealth)
// governs how long any single probe can run.
var consoleHTTPClient = &http.Client{}

// openURLFunc is the browser-launch seam runConsole calls through instead
// of defaultOpenURL directly, so tests can fake it and no test run ever
// spawns a real browser process.
var openURLFunc = defaultOpenURL

// runConsole implements `moltnet console [--id <network-id>] [--config
// <path>] [--print] [--no-open] [--no-restart]`, the last step of the
// "install -> init -> deploy -> share -> console" flow: the server has
// served the built-in web console at <listen_addr>/console/ since it first
// existed
// (internal/transport/ui.go), and `moltnet service install` already starts
// that server, but nothing told the operator the console existed or opened
// it for them.
//
// It resolves the network's server config with the exact same rules
// `moltnet service`/`moltnet pair`/`moltnet relay deploy` use
// (resolvePairConfigPath, pair.go, itself a thin wrapper over
// app.ResolveConfigPath), derives the console URL from the loaded config's
// server.listen_addr (consoleBaseURL below), and health-checks the running
// server's /healthz before ever opening a browser: a browser is only
// launched against a server that is demonstrably answering.
func runConsole(ctx context.Context, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildConsoleUsage())
		return nil
	}

	flags := flag.NewFlagSet("moltnet console", flag.ContinueOnError)
	flags.SetOutput(stdout)
	var (
		configPath = flags.String("config", "", "Moltnet config path")
		id         = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
		printOnly  = flags.Bool("print", false, "print the console URL only; never open a browser")
		noOpen     = flags.Bool("no-open", false, "print the console status line without opening a browser")
		noRestart  = flags.Bool("no-restart", false, "self-heal may write a fresh console token but never restarts the service; print the exact restart command instead")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("console does not accept positional arguments")
	}

	path, err := resolvePairConfigPath(*configPath, *id)
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	cfg, err := app.LoadConfigForPath(absPath, "")
	if err != nil {
		return err
	}

	// explicitConfigPath is empty unless --config itself was given: it
	// tells reportConsoleServerDown which flag to echo in its suggested
	// next command, since a bare --id <network-id> only resolves under
	// ~/.moltnet and would not find a config loaded from an arbitrary
	// --config path.
	var explicitConfigPath string
	if strings.TrimSpace(*configPath) != "" {
		explicitConfigPath = absPath
	}

	base, err := consoleBaseURL(cfg.ListenAddr)
	if err != nil {
		return err
	}
	consoleURL := base + "/console/"

	if !probeConsoleHealth(ctx, base) {
		return reportConsoleServerDown(cfg.NetworkID, explicitConfigPath)
	}

	switch {
	case *printOnly:
		fmt.Fprintln(stdout, consoleURL)
		return nil
	case *noOpen:
		printConsoleReadyLine(consoleURL)
		return nil
	case !isOutputTerminal():
		// A pipe or other non-terminal stdout means a non-interactive
		// caller (a script, a captured test) -- never spawn a GUI browser
		// there; behave exactly like --print instead.
		fmt.Fprintln(stdout, consoleURL)
		return nil
	default:
		// The browser-open path only: internal/transport/auth.go's
		// authorizedConsole 401s an anonymous GET /console/ on a --bearer
		// network with no public_read, so a bare consoleURL would open the
		// browser straight onto raw JSON. Append an observe-ONLY-scoped
		// token as ?access_token=... -- maybeSetConsoleAuthCookie (same
		// file) copies that query parameter verbatim into an HttpOnly
		// cookie with NO scope downgrade, so whatever scopes the token
		// carries become the console session's scopes. consoleObserveToken
		// therefore refuses any token carrying so much as one scope beyond
		// "observe" (P1 fix -- an earlier version accepted the first token
		// whose scopes merely included "observe", which could and did hand
		// the browser session admin-capable scopes). Never printed: only
		// openURLFunc sees openTarget: the stdout ready line below,
		// --print, and --no-open all keep using the bare consoleURL so no
		// script capturing this command's output can ever see the token.
		//
		// A config-file token, self-healed or already present, is not
		// enough on its own: the server only reloads auth.tokens[] on
		// restart, so a token that is genuinely on disk can still be one
		// the running process has never seen (the field bug this fix
		// closes -- a repeat run finding the token in the config and
		// appending it unconditionally, opening a 307-then-401 behind a
		// green checkmark). resolveConsoleAccessToken (console_selfheal.go)
		// is the one place that decides this: it never hands back a token
		// that has not been probe-confirmed live against the actual
		// running server, and it never leaves this command claiming ready
		// when it isn't.
		openTarget := consoleURL
		if cfg.Auth.Mode == authn.ModeBearer {
			token, ok, resolveErr := resolveConsoleAccessToken(ctx, absPath, cfg, base, *noRestart)
			if resolveErr != nil {
				// A restart this command itself attempted did not
				// verifiably come back -- a real command failure (nonzero
				// exit), never a browser open against a server already
				// known not to be answering.
				return resolveErr
			}
			if !ok {
				// Nothing is safe to open yet, but nothing this command
				// tried actually failed (an unloaded existing token, or no
				// managed service to self-heal-restart): the one exact
				// next command is already printed. Opening the browser
				// here would just trade that honest note for a raw 401.
				return nil
			}
			openTarget = consoleURL + "?access_token=" + url.QueryEscape(token)
		}
		if err := openURLFunc(openTarget); err != nil {
			// The opener is missing or failed (headless box, unknown
			// GOOS, ...): fall back to printing the URL rather than
			// failing the command outright -- the operator still gets
			// what they need to open it themselves. The token stays out
			// of this fallback print too, for the same reason.
			fmt.Fprintln(stdout, consoleURL)
			return nil
		}
		printConsoleReadyLine(consoleURL)
		return nil
	}
}

// consoleObserveToken returns the value of the first auth.tokens[] entry in
// cfg (in config-file order) whose scopes are EXACTLY [observe] -- no more,
// no fewer -- for use only on runConsole's browser-open path. ok is false --
// and the caller must not fall back to a more privileged token -- when cfg's
// auth mode is not "bearer" (open and none-mode consoles need no token) or
// when no configured token carries the observe scope and nothing else.
//
// P1 fix: an earlier version selected the first token whose scopes merely
// included "observe", so a full-scope operator token (e.g. the one
// `moltnet init --bearer` writes, [observe, write, admin] -- or an older
// config's [observe, write, admin, pair], from before operator tokens
// stopped minting with "pair") could be handed straight to the browser.
// The server copies ?access_token= verbatim
// into an HttpOnly cookie with no scope downgrade (maybeSetConsoleAuthCookie,
// internal/transport/auth.go), so whatever scopes the token carries become
// the console session's scopes outright -- an admin-capable console session,
// and the token itself visible in `ps` as an argv to the browser opener.
// Selecting only an exact-[observe] token closes both: never fabricate one,
// never widen the search to "observe plus anything else" as a fallback.
func consoleObserveToken(cfg app.Config) (string, bool) {
	if cfg.Auth.Mode != authn.ModeBearer {
		return "", false
	}
	for _, token := range cfg.Auth.Tokens {
		if isObserveOnlyScopes(token.Scopes) {
			return token.Value, true
		}
	}
	return "", false
}

// isObserveOnlyScopes reports whether scopes carries exactly the observe
// scope and nothing else -- the strict subset consoleObserveToken requires
// before it will ever hand a token to the browser.
func isObserveOnlyScopes(scopes []authn.Scope) bool {
	return len(scopes) == 1 && scopes[0] == authn.ScopeObserve
}

// consoleBaseURL derives the plain-HTTP origin this CLI dials directly to
// reach the resolved network's server, from its server.listen_addr
// (app.Config.ListenAddr -- e.g. Moltnet's "server.listen_addr: \":8787\"").
//
// Mapping:
//   - No host (":8787") binds every local interface with no textual host to
//     parse at all, so this substitutes the loopback address 127.0.0.1 --
//     the CLI and the server are assumed to run on the same machine,
//     exactly the case `moltnet service install`/`moltnet start` cover.
//   - A host that parses as an IP and reports net.IP.IsUnspecified() true
//     (the wildcard address, in any of its textual forms -- "0.0.0.0",
//     "::", or the equivalent expanded "0:0:0:0:0:0:0:0") gets the same
//     127.0.0.1 substitution, for the same reason: it binds every local
//     interface, not one single dialable address.
//   - Any other host, including an explicit loopback IP like "::1" or
//     "127.0.0.1", or a non-loopback host ("moltnet.example.com"), is used
//     verbatim: an operator who bound to a specific interface or configured
//     a specific hostname gets that value back untouched.
//   - The scheme is always "http": moltnet never terminates TLS itself
//     (internal/app/app.go's Run calls server.ListenAndServe, never
//     ListenAndServeTLS), so a direct dial to listen_addr is always plain
//     HTTP, whether or not an operator additionally puts a TLS-terminating
//     reverse proxy in front of it. server.trust_forwarded_proto only
//     controls whether the *server* trusts an upstream proxy's
//     X-Forwarded-Proto header when computing its own outbound base URL
//     for humans/agents hitting that proxy
//     (internal/transport/discovery.go's requestBaseURL) -- it has no
//     bearing on this CLI's own direct, unproxied dial to listen_addr, so
//     it plays no part in this mapping.
func consoleBaseURL(listenAddr string) (string, error) {
	addr := strings.TrimSpace(listenAddr)
	if addr == "" {
		addr = ":8787"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid server.listen_addr %q: %w", listenAddr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	} else if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// probeConsoleHealth reports whether base's server answers GET /healthz
// with 200 OK within consoleHealthTimeout. /healthz is always reachable
// without a token (internal/transport/http.go wires it through
// anonymousAllowedInOpen), so this never needs the resolved config's auth
// material -- only its address.
func probeConsoleHealth(ctx context.Context, base string) bool {
	healthCtx, cancel := context.WithTimeout(ctx, consoleHealthTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(healthCtx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return false
	}
	response, err := consoleHTTPClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

// reportConsoleServerDown prints the one actionable next command for a
// network whose server did not answer /healthz, then returns errConsoleServerDown
// (wrapped with the network id) so runCLI/main exit nonzero without a
// browser ever being opened, and so main.go's errors.Is(err,
// errConsoleServerDown) case can skip printing the same fact a second time
// as a bare "error: ..." line. It picks between the two possible next
// commands using internal/service's IsInstalled: `moltnet service install
// ...` when no service unit exists yet for this network (install both
// writes and starts it), or `moltnet service start ...` -- never the
// foreground `moltnet start`, which would race the already-loaded unit --
// when one is already installed but the server still is not answering.
//
// explicitConfigPath is empty unless the operator passed --config
// themselves, in which case the suggested command echoes that same
// absolute --config path instead of --id <network-id>: a bare --id only
// resolves a config under ~/.moltnet, so it would not find one loaded from
// an arbitrary --config path.
func reportConsoleServerDown(networkID string, explicitConfigPath string) error {
	installed, err := newServiceManager().IsInstalled(networkID)
	if err != nil {
		// Can't determine either way (e.g. an unsupported GOOS): default to
		// the install suggestion, the one that also works for an operator
		// who never ran `service install` at all.
		installed = false
	}

	identifier := fmt.Sprintf("--id %s", networkID)
	if explicitConfigPath != "" {
		identifier = fmt.Sprintf("--config %s", explicitConfigPath)
	}

	nextCommand := fmt.Sprintf("moltnet service install %s", identifier)
	if installed {
		nextCommand = fmt.Sprintf("moltnet service start %s", identifier)
	}

	// P2 fix: this used to print to stdout, which poisoned
	// `URL=$(moltnet console --print)` with failure prose instead of
	// leaving stdout empty on a failed run. errConsoleServerDown (wrapped
	// below) is what main.go matches to skip a second, redundant
	// "error: ..." line — that sentinel already prevents this fact from
	// printing twice, so moving it to stderr loses nothing. Printed plain,
	// never through bold/green/yellow/dim: those four gate on stdout's own
	// TTY/NO_COLOR/TERM state (see style.go) and would check the wrong
	// stream's terminal here.
	fmt.Fprintf(stderr, "server not running for %s\n", networkID)
	fmt.Fprintln(stderr, "  "+nextCommand)
	return fmt.Errorf("%w for network %q", errConsoleServerDown, networkID)
}

// printConsoleReadyLine prints the one quiet status line a successful
// (browser-opening or --no-open) `moltnet console` ends with.
func printConsoleReadyLine(consoleURL string) {
	fmt.Fprintln(stdout, "  "+green("✓")+" console  "+consoleURL)
}

// defaultOpenURL launches consoleURL in the operator's default browser via
// the platform opener -- macOS "open", Linux "xdg-open" -- resolved with
// exec.LookPath and run with exec.Command's argv form, never a shell
// string, so consoleURL is never interpreted by a shell.
func defaultOpenURL(consoleURL string) error {
	// Defense in depth: consoleURL is always built by this package (never
	// operator- or network-supplied), but refusing anything that is not a
	// plain "http://host..." URL here means a future bug upstream (a bad
	// join, an unescaped fragment, ...) fails closed instead of handing an
	// unknown scheme or host to the platform opener.
	if parsed, err := url.Parse(consoleURL); err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return fmt.Errorf("refusing to open non-http console URL %q", consoleURL)
	}

	var opener string
	switch runtime.GOOS {
	case "darwin":
		opener = "open"
	case "linux":
		opener = "xdg-open"
	default:
		return fmt.Errorf("no known browser opener for GOOS %q", runtime.GOOS)
	}

	path, err := exec.LookPath(opener)
	if err != nil {
		return err
	}
	return exec.Command(path, consoleURL).Run()
}
