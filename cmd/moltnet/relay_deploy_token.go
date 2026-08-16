package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// Cloudflare API token resolution order for `relay deploy`: CLOUDFLARE_API_TOKEN
// env always wins (unchanged, so a caller overriding it for one run never
// silently gets a stale stored token instead); a per-network stored token
// (.moltnet/cloudflare.json, saved by --save-token or the save prompt) is
// next; missing both, on an interactive terminal, prompts for a pasted
// token instead of the export-and-retry flow (maybePromptForCloudflareToken
// below); missing both with no interactive prompt available prints the
// token-creation guidance and errors.
const (
	cloudflareTokenSourceEnv    = "env"
	cloudflareTokenSourceStored = "stored"
	cloudflareTokenSourcePasted = "pasted"
)

// maybePromptForCloudflareToken is the interactive replacement for the
// `export CLOUDFLARE_API_TOKEN=...` step: when relay deploy finds no token
// (env or stored) and both stdin and stdout are attached to a real
// terminal, it prints the token-creation guidance essentials and prompts
// for a pasted token (via promptHidden, prompt_hidden.go) instead of
// erroring immediately. It returns "" with a nil error both when the
// terminal is not interactive (the caller's existing missing-token
// guidance-and-error path then runs exactly as before — never a prompt,
// never in CI or a piped run) and when the pasted input was empty or only
// whitespace, which the caller treats identically to no token at all. The
// pasted token itself is never printed by this function.
//
// Echo is disabled here, before the guidance block is printed, rather than
// left to promptHidden's own call right before its read: printing the
// guidance takes several lines, and an operator who starts pasting
// immediately (typeahead, arriving before this function even gets to the
// prompt line) must never have those keystrokes echoed while the guidance
// is still being written. promptHidden's own disableTerminalEcho call
// below is a no-op once echo is already suppressed (see
// prompt_hidden.go) — it does not double-toggle the terminal or register a
// second signal handler — so nesting the two calls this way is safe.
//
// If echo cannot be reliably disabled at all — disableTerminalEcho returns
// ok=false, the P0 fail-open-echo fix — this refuses to print the
// interactive prompt or read anything: printing a warning and returning
// errTerminalEchoUnavailable instead, so the caller (runRelayDeploy) falls
// back to the non-interactive token-guidance-and-error path rather than
// risking a pasted secret echoing to the screen under an
// "(input hidden)" label.
func maybePromptForCloudflareToken() (string, error) {
	if !isInteractive() || !isOutputTerminal() {
		return "", nil
	}

	restore, ok := disableTerminalEcho()
	defer restore()
	if !ok {
		fmt.Fprintln(stdout, yellow("warning: cannot disable terminal echo; falling back to token guidance"))
		return "", errTerminalEchoUnavailable
	}

	fmt.Fprint(stdout, cloudflareTokenCreationGuidance())
	fmt.Fprintln(stdout)
	return promptHidden("  paste token (input hidden): ")
}

// resolveCloudflareAPIToken picks the Cloudflare API token `relay deploy`
// uses and names where it came from, in the resolution order documented on
// the cloudflareTokenSource constants above. A pure function so the
// precedence itself is unit-testable without a Cloudflare client or
// filesystem.
func resolveCloudflareAPIToken(envToken, storedToken string, storedTokenOK bool) (token, source string) {
	envToken = strings.TrimSpace(envToken)
	if envToken != "" {
		return envToken, cloudflareTokenSourceEnv
	}
	if storedTokenOK {
		return storedToken, cloudflareTokenSourceStored
	}
	return "", ""
}

// maybeSaveCloudflareToken persists apiToken to tokenPath after a
// successful deploy that used the CLOUDFLARE_API_TOKEN env token (never
// called for an already-stored token, which needs no re-saving). With
// --save-token it saves unconditionally. Otherwise, only when both stdin
// and stdout are a real terminal (P2 env-path gating fix — matching the
// pasted path's own isInteractive() && isOutputTerminal() gate, rather than
// stdin alone: with stdout piped, the offer text is invisible while still
// blocking on stdin) and only when nothing is stored yet, it offers the
// save once via promptYesNo; declining or running non-interactively leaves
// the token unsaved. Never prints apiToken itself.
func maybeSaveCloudflareToken(tokenPath, apiToken string, saveToken, storedTokenOK bool) error {
	switch {
	case saveToken:
		// fall through to save below
	case !storedTokenOK && isInteractive() && isOutputTerminal():
		// The deploy this save offer follows can take several seconds
		// against the real Cloudflare API; discard anything typed during
		// that window before reading the answer, so mid-deploy typeahead
		// (most commonly a stray Enter) never silently answers this prompt
		// (P0 mid-deploy-typeahead fix).
		flushPendingTerminalInput()
		confirmed, err := promptYesNo(fmt.Sprintf("  save this token to %s (0600) for future deploys? [y/N] ", tokenPath))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	default:
		return nil
	}

	if err := relaydeploy.SaveCloudflareToken(tokenPath, apiToken); err != nil {
		return err
	}
	fmt.Fprintln(stdout, dim(fmt.Sprintf("  saved Cloudflare API token to %s", tokenPath)))
	return nil
}

// maybeSavePastedCloudflareToken persists apiToken — a token the operator
// just hand-pasted into the interactive promptHidden prompt
// (maybePromptForCloudflareToken above) — to tokenPath after a successful
// deploy. Unlike maybeSaveCloudflareToken's env-token save offer (default
// no: that token already lives somewhere, the environment), this defaults
// to yes: saving is the entire point of having prompted for a paste in the
// first place, so a bare Enter accepts. --save-token still saves
// unconditionally, without asking. Never prints apiToken itself. Only
// called when tokenSource is cloudflareTokenSourcePasted, which by
// construction only happens after a successful maybePromptForCloudflareToken
// call, so no further interactivity check is needed here.
func maybeSavePastedCloudflareToken(tokenPath, apiToken string, saveToken bool) error {
	if !saveToken {
		// See maybeSaveCloudflareToken's matching call: the deploy this
		// save offer follows can take several seconds, and anything typed
		// during that window must never silently answer this prompt (P0
		// mid-deploy-typeahead fix).
		flushPendingTerminalInput()
		confirmed, err := promptYesNoDefaultYes(fmt.Sprintf("  save to %s (0600)? [Y/n] ", tokenPath))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(stdout, dim("  not saved"))
			return nil
		}
	}

	if err := relaydeploy.SaveCloudflareToken(tokenPath, apiToken); err != nil {
		return err
	}
	fmt.Fprintln(stdout, dim(fmt.Sprintf("  saved Cloudflare API token to %s", tokenPath)))
	return nil
}

// promptYesNoDefaultYes is promptYesNo's (uninstall.go) default-yes
// counterpart: an empty answer from an actual newline (a bare Enter)
// counts as yes, and ONLY an explicit "y"/"yes" (case insensitive,
// surrounding whitespace trimmed) otherwise counts as yes too — this is a
// whitelist, not a "not an explicit no" blocklist (the P0 consent-parser
// fix: the previous `answer != "n" && answer != "no"` shape treated
// anything at all that wasn't literally "n"/"no" as an affirmative,
// including garbage, a typo like "nope", or a stray escape sequence, which
// meant most ways of *not* actually answering yes still saved the token).
// Every other answer — garbage, a typo, an escape sequence, anything —
// is no. It backs the post-deploy pasted-token save prompt above, where
// the default should be yes (see maybeSavePastedCloudflareToken's
// comment); promptYesNo's default-no shape stays exactly as-is for the
// existing env-token save offer, which is a different decision with a
// different right default.
//
// io.EOF (Ctrl-D, or a closed/exhausted input with no trailing newline) is
// treated as no, not as the bare-Enter default-yes case: EOF means no
// answer was actually given, and silently saving a token to disk on
// nothing more than a closed pipe would be the more surprising and harder
// to undo of the two wrong guesses.
func promptYesNoDefaultYes(question string) (bool, error) {
	fmt.Fprint(stdout, question)
	reader := bufio.NewReader(promptReader)
	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes", nil
}

// runRelayDeployForgetToken implements `relay deploy --forget-token`: delete
// the stored per-network Cloudflare API token and exit without deploying,
// mirroring --print-manual's early-return shape. Missing-file is reported,
// not treated as an error — there is simply nothing to forget.
func runRelayDeployForgetToken(tokenPath string) error {
	removed, err := relaydeploy.DeleteCloudflareToken(tokenPath)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(stdout, "  removed stored Cloudflare API token %s\n", tokenPath)
	} else {
		fmt.Fprintf(stdout, "  no stored Cloudflare API token at %s\n", tokenPath)
	}
	return nil
}
