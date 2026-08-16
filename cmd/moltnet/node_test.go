package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestRunNodeFlagsAfterPositionalErrClearly covers item 5: the flag package
// stops parsing at the first non-flag argument, so `moltnet node start
// <path> --id x` never reaches --id as a flag at all — it lands as three
// unparsed positional args (path, --id, x), *id stays at its zero value,
// and the pre-existing "flags.NArg() > 1" guard used to reject it with the
// same bare os.ErrInvalid a genuinely bogus arg list gets, giving no hint
// that the fix is just "put --id before the path" (already how
// buildNodeUsage documents it: "moltnet node start [--id <network-id>]
// [path]"). This pins the specific, actionable error message instead.
func TestRunNodeFlagsAfterPositionalErrClearly(t *testing.T) {
	err := runNode(context.Background(), []string{"/some/path", "--id", "acme"})
	if err == nil {
		t.Fatal("expected an error when a flag follows the positional path")
	}
	if errors.Is(err, os.ErrInvalid) {
		t.Fatalf("runNode() error = %v (bare os.ErrInvalid), want a specific flags-must-precede-the-path message", err)
	}
	if !strings.Contains(err.Error(), "flags must precede the path") {
		t.Fatalf("runNode() error = %q, want it to mention flags must precede the path", err.Error())
	}
	if !strings.Contains(err.Error(), `"--id"`) {
		t.Fatalf("runNode() error = %q, want it to name the offending flag", err.Error())
	}
}

// TestRunNodeTooManyPlainArgsKeepsGenericError is the companion case: extra
// positional args that don't look like a flag at all (no leading "-") still
// get the original, generic os.ErrInvalid — there's no specific "put this
// first" advice to give when nothing after the path looks like a flag.
func TestRunNodeTooManyPlainArgsKeepsGenericError(t *testing.T) {
	err := runNode(context.Background(), []string{"/some/path", "extra"})
	if !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("runNode() error = %v, want os.ErrInvalid for extra non-flag-looking args", err)
	}
}

// The already-documented, already-working order (buildNodeUsage: "moltnet
// node start [--id <network-id>] [path]") is covered by
// TestRunNodeResolvesNetworkByID (start_node_id_test.go) — this fix only
// adds a clearer error for the reversed order and must not regress that
// one, which the existing test already pins.
