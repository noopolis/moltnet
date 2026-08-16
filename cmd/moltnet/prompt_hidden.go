package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errTerminalEchoUnavailable is returned by promptHidden — and, before
// promptHidden is ever reached, directly by maybePromptForCloudflareToken
// (relay_deploy_token.go) — when the controlling terminal's local echo
// could not be reliably disabled: the termios get/set ioctl itself failed
// (prompt_hidden_echo_unix.go, darwin/linux), or this GOOS has no known way
// to touch terminal echo at all (prompt_hidden_echo_other.go, every other
// GOOS). Reading a secret in either state would risk echoing it to the
// screen under an "(input hidden)" label, so the read never happens at all
// — a fail-closed error, not the silent no-op this used to be (P0
// fail-open-echo fix). Callers fall back to the non-interactive
// token-guidance path instead of prompting.
var errTerminalEchoUnavailable = errors.New("cannot disable terminal echo")

// promptHidden prints question, then reads one line from promptReader —
// the same seam promptYesNo uses (uninstall.go) — without echoing it to
// the terminal, and returns it trimmed of surrounding whitespace. It backs
// the interactive Cloudflare-token paste prompt (relay_deploy.go): the
// pasted token is never echoed to the screen or returned with its
// characters logged anywhere.
//
// Echo suppression only actually happens when promptReader is still the
// production default (os.Stdin) and stdin is a real TTY (isInteractive());
// the test seam that swaps promptReader for a scripted io.Reader has no
// terminal to touch, so disableTerminalEcho no-ops (ok=true, nothing
// needed) in that case and this just reads the scripted line directly —
// matching promptYesNo's own test-seam behavior. When echo suppression was
// needed but failed (disableTerminalEcho returns ok=false), promptHidden
// refuses to read at all and returns errTerminalEchoUnavailable — it must
// never fall through to a read that would leave a secret echoing on
// screen.
func promptHidden(question string) (string, error) {
	fmt.Fprint(stdout, question)

	restore, ok := disableTerminalEcho()
	defer restore()
	if !ok {
		fmt.Fprintln(stdout)
		return "", errTerminalEchoUnavailable
	}

	reader := bufio.NewReader(promptReader)
	line, err := reader.ReadString('\n')
	// A suppressed-echo terminal never echoed the Enter keystroke either,
	// so the cursor is still sitting at the end of the prompt line; print
	// the newline the user's Enter would otherwise have produced.
	fmt.Fprintln(stdout)

	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimSpace(line), nil
}
