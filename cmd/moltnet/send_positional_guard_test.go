package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestSendPositionalArgsJoinTrailingWordsIntoText is the P3 fix for the most
// natural unquoted typing at a terminal: with --text absent, every
// positional word left over once the target is resolved joins into one
// message, instead of erroring on the first word past the second
// positional.
func TestSendPositionalArgsJoinTrailingWordsIntoText(t *testing.T) {
	target, text, err := resolveSendPositionalArgs([]string{"room:chat", "hola", "mundo"}, "", "")
	if err != nil {
		t.Fatalf("resolveSendPositionalArgs() error = %v, want nil", err)
	}
	if target != "room:chat" || text != "hola mundo" {
		t.Fatalf("resolveSendPositionalArgs() = (%q, %q), want (\"room:chat\", \"hola mundo\")", target, text)
	}
}

// TestSendPositionalArgsExtraArgumentErrorsWhenTextFlagGiven keeps the
// original "extra value" error, but only once --text was already given
// explicitly: joining a stray positional into a --text the caller set on
// purpose would silently change a value they thought they controlled.
func TestSendPositionalArgsExtraArgumentErrorsWhenTextFlagGiven(t *testing.T) {
	_, _, err := resolveSendPositionalArgs([]string{"room:chat", "mundo"}, "", "hola")
	if err == nil || !strings.Contains(err.Error(), `"mundo"`) {
		t.Fatalf("expected an error naming the extra argument, got %v", err)
	}
}

// TestSendPositionalArgsRejectsStrayTargetLikePositionalWhenTargetFlagSet is
// the P2 fix for `send --target room:a room:b` silently posting the literal
// text "room:b": a lone leftover positional that itself looks like a target
// almost certainly means the caller forgot --text, not that "room:b" is the
// intended message.
func TestSendPositionalArgsRejectsStrayTargetLikePositionalWhenTargetFlagSet(t *testing.T) {
	_, _, err := resolveSendPositionalArgs([]string{"room:b"}, "room:a", "")
	if err == nil {
		t.Fatal("expected an error for a stray target-like positional when --target is already set")
	}
	if !strings.Contains(err.Error(), "room:b") || !strings.Contains(err.Error(), "--text") {
		t.Fatalf("expected the error to name %q and --text, got %v", "room:b", err)
	}

	// A dm:<id> positional is refused the same way.
	if _, _, err := resolveSendPositionalArgs([]string{"dm:b"}, "room:a", ""); err == nil {
		t.Fatal("expected an error for a stray dm:<id>-shaped positional too")
	}

	// A non-target-shaped positional is still treated as the message, not
	// refused — the guard only fires on room:/dm: shaped stray positionals.
	target, text, err := resolveSendPositionalArgs([]string{"hola"}, "room:a", "")
	if err != nil {
		t.Fatalf("resolveSendPositionalArgs() error = %v, want nil for a plain message word", err)
	}
	if target != "room:a" || text != "hola" {
		t.Fatalf("resolveSendPositionalArgs() = (%q, %q), want (\"room:a\", \"hola\")", target, text)
	}
}

// TestSendTargetFlagWithStrayTargetLikePositionalErrorsEndToEnd exercises
// the same guard through runSend end-to-end, confirming it never reaches
// the network (no request is sent) and the error names --text.
func TestSendTargetFlagWithStrayTargetLikePositionalErrorsEndToEnd(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	err := run(context.Background(), []string{"send", "--target", "room:a", "room:b"}, "test")
	if err == nil {
		t.Fatal("expected run() to refuse a stray room:b positional when --target is already set")
	}
	if !strings.Contains(err.Error(), "--text") {
		t.Fatalf("expected the error to name --text, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("expected no request to be sent, got %d", requests)
	}
}

// TestSendHelpPrintsUsageInsteadOfTargetError and
// TestReadHelpPrintsUsageInsteadOfTargetError are the P3 fix: a bare "help"
// positional (not "-h"/"--help", already covered by TestFlagHelpNeverErrors
// in help_test.go) used to fall through to parseTarget's "target must be
// room:<id> or dm:<id>" error, treating "help" itself as the target.
func TestSendHelpPrintsUsageInsteadOfTargetError(t *testing.T) {
	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"send", "help"}, "test")
	})
	if err != nil {
		t.Fatalf("run() send help error = %v, want nil", err)
	}
	if !strings.Contains(output, "Usage of moltnet send:") {
		t.Fatalf("run() send help stdout = %q, want send usage", output)
	}
}

func TestReadHelpPrintsUsageInsteadOfTargetError(t *testing.T) {
	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"read", "help"}, "test")
	})
	if err != nil {
		t.Fatalf("run() read help error = %v, want nil", err)
	}
	if !strings.Contains(output, "Usage of moltnet read:") {
		t.Fatalf("run() read help stdout = %q, want read usage", output)
	}
}
