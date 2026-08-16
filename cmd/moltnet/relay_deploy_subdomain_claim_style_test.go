package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// This file covers the two P2-2 restyled claim-loop messages the existing
// end-to-end fixtures (relay_deploy_subdomain_claim_test.go) never exercise:
// a plain (non-EOF) promptLine read error, and a Cloudflare claim rejection
// carrying neither of the two documented codes (10031/10036) this package
// branches on. Both used to print whole-line-yellow with no note:/warning:
// prefix; both are now yellow("warning:") + a plain body.

// erroringReader is an io.Reader whose every Read call fails with a fixed,
// non-io.EOF error — standing in for a real (if rare) terminal read failure,
// something withPromptAnswers's strings.Reader-backed stubs (which only
// ever produce a clean io.EOF) cannot model.
type erroringReader struct{ err error }

func (r erroringReader) Read([]byte) (int, error) { return 0, r.err }

// TestAttemptInteractiveWorkersDevSubdomainClaimPromptReadErrorWarns covers
// promptLine returning a plain (non-io.EOF) error: attemptInteractiveWorkersDevSubdomainClaim
// prints it with a warning: prefix and gives up immediately, the same as an
// EOF decline, rather than looping.
func TestAttemptInteractiveWorkersDevSubdomainClaimPromptReadErrorWarns(t *testing.T) {
	previousReader := promptReader
	promptReader = erroringReader{err: errors.New("device not configured")}
	t.Cleanup(func() { promptReader = previousReader })
	previousInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = previousInteractive })

	// The read fails before any network call, so a client pointed at an
	// unreachable address is fine here — it must never actually be dialed.
	client := relaydeploy.NewClientForTesting("test-token", "http://127.0.0.1:0", http.DefaultClient)

	var name string
	var ok bool
	output := captureStdout(t, func() {
		name, ok, _ = attemptInteractiveWorkersDevSubdomainClaim(context.Background(), client, "account-1", &sectionPrinter{})
	})
	if ok || name != "" {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() = (%q, %v), want (\"\", false) on a read error", name, ok)
	}
	if !strings.Contains(output, yellow("warning:")+" read input: device not configured") {
		t.Fatalf("expected a warning:-prefixed read error, got %q", output)
	}
}

// startFakeCloudflareClaimSubdomainGenericErrorServer is a minimal fake
// backing only PUT /accounts/{account}/workers/subdomain, always rejecting
// with a Cloudflare error code that is neither 10031 (name taken) nor 10036
// (account already has a subdomain) — the "any other rejection" branch
// attemptInteractiveWorkersDevSubdomainClaim falls through to.
func startFakeCloudflareClaimSubdomainGenericErrorServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /accounts/{account}/workers/subdomain", func(w http.ResponseWriter, r *http.Request) {
		writeFakeCloudflareEnvelopeError(w, http.StatusInternalServerError, 10000, "internal error")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// TestAttemptInteractiveWorkersDevSubdomainClaimGenericRejectionWarns covers
// a Cloudflare claim rejection carrying neither documented code: it prints
// the API's own reason with a warning: prefix (unlike the 10031 name-taken
// branch, which uses note: — that one is an expected, retryable outcome;
// an unrecognized rejection is not) and, on attempt exhaustion, falls back
// to the standard guidance exactly like any other repeated failure.
func TestAttemptInteractiveWorkersDevSubdomainClaimGenericRejectionWarns(t *testing.T) {
	server := startFakeCloudflareClaimSubdomainGenericErrorServer(t)
	client := relaydeploy.NewClientForTesting("test-token", server.URL, server.Client())
	withPromptAnswers(t, "apresmoi", "apresmoi")

	var name string
	var ok bool
	output := captureStdout(t, func() {
		name, ok, _ = attemptInteractiveWorkersDevSubdomainClaim(context.Background(), client, "account-1", &sectionPrinter{})
	})
	if ok || name != "" {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() = (%q, %v), want (\"\", false) once both attempts are rejected", name, ok)
	}
	if !strings.Contains(output, yellow("warning:")+` could not claim "apresmoi": `) {
		t.Fatalf("expected a warning:-prefixed generic rejection, got %q", output)
	}
	if !strings.Contains(output, "internal error") {
		t.Fatalf("expected the API's own rejection reason, got %q", output)
	}
}
