package relaydeploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/relay"
)

func TestDeployHappyPath(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID: "acct-deploy",
		authOK:    true,
		subdomain: "acme",
	})

	result, err := Deploy(context.Background(), fake.client(), Options{ScriptName: "moltnet-relay", ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.ScriptName != "moltnet-relay" {
		t.Fatalf("ScriptName = %q, want moltnet-relay", result.ScriptName)
	}
	if result.AccountID != "acct-deploy" {
		t.Fatalf("AccountID = %q, want acct-deploy", result.AccountID)
	}
	if result.Hostname != "moltnet-relay.acme.workers.dev" {
		t.Fatalf("Hostname = %q, want moltnet-relay.acme.workers.dev", result.Hostname)
	}
	if result.URL != "wss://moltnet-relay.acme.workers.dev" {
		t.Fatalf("URL = %q, want wss://moltnet-relay.acme.workers.dev", result.URL)
	}
	if !result.HostnameResolved {
		t.Fatal("HostnameResolved = false, want true (stubbed)")
	}
	if result.Token == "" {
		t.Fatal("Token is empty, want a generated RELAY_TOKEN")
	}

	if got := string(fake.uploadedScript); got != string(relay.WorkerScript) {
		t.Fatalf("uploaded script did not match the embedded relay.WorkerScript (len %d vs %d)", len(got), len(relay.WorkerScript))
	}
	if got := string(fake.uploadedMetadata); got != string(relay.WorkerMetadataJSON) {
		t.Fatalf("uploaded metadata did not match the embedded relay.WorkerMetadataJSON")
	}
	if value, ok := fake.secretValue(relayTokenSecretName); !ok || value != result.Token {
		t.Fatalf("secret %s = (%q, %v), want (%q, true)", relayTokenSecretName, value, ok, result.Token)
	}
	if !fake.routeEnabled {
		t.Fatal("expected the workers.dev route to be enabled")
	}
}

func TestDeployDefaultsScriptName(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	result, err := Deploy(context.Background(), fake.client(), Options{ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.ScriptName != DefaultScriptName {
		t.Fatalf("ScriptName = %q, want default %q", result.ScriptName, DefaultScriptName)
	}
}

func TestDeployReusesPriorTokenOnRedeploy(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	first, err := Deploy(context.Background(), fake.client(), Options{ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("first Deploy() error = %v", err)
	}

	second, err := Deploy(context.Background(), fake.client(), Options{PriorToken: first.Token, ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("second Deploy() error = %v", err)
	}
	if second.Token != first.Token {
		t.Fatalf("redeploy token = %q, want unchanged %q (idempotent redeploy keeps the secret)", second.Token, first.Token)
	}
}

func TestDeployExistingTokenOverridesPriorToken(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	result, err := Deploy(context.Background(), fake.client(), Options{
		ExistingToken:   "rotated-token-value",
		PriorToken:      "stale-token-value",
		ResolveHostname: stubResolveHostname(true),
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Token != "rotated-token-value" {
		t.Fatalf("Token = %q, want the --token-env value to win over the stored prior token", result.Token)
	}
}

// TestDeployWorkersDevSubdomainUnclaimedFailsBeforeAnyUpload covers the
// bug fix: claim state is resolved before any remote mutation, so a deploy
// that fails on an unclaimed subdomain (no Options.Subdomain given) must
// never have uploaded a Worker or set a secret in the first place — unlike
// the old order, which called UploadWorkerModule and SetSecret first and
// only then checked WorkersDevSubdomain, leaving partial external state
// behind on exactly this failure.
func TestDeployWorkersDevSubdomainUnclaimedFailsBeforeAnyUpload(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: ""})
	result, err := Deploy(context.Background(), fake.client(), Options{})
	if !errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("Deploy() error = %v, want ErrWorkersDevSubdomainUnclaimed", err)
	}
	// P1 regression: this is the *primary* detection path (WorkersDevSubdomain
	// itself reports unclaimed, not a belt-and-braces 10007/10063 rejection
	// from another step), so Result's doc comment's guarantee that AccountID
	// stays populated on this sentinel must hold here specifically — a
	// caller driving the interactive claim (cmd/moltnet's
	// attemptInteractiveWorkersDevSubdomainClaim) reuses exactly this value
	// instead of resolving the account a second time, and a zeroed AccountID
	// here would send its claim PUT to /accounts//workers/subdomain.
	if result.AccountID != fake.cfg.accountID {
		t.Fatalf("Result.AccountID = %q, want %q even on ErrWorkersDevSubdomainUnclaimed from the primary GET-check path", result.AccountID, fake.cfg.accountID)
	}
	if fake.uploadedScript != nil {
		t.Fatal("expected no worker upload before the subdomain claim check (the bug this fix closes)")
	}
	if _, ok := fake.secretValue(relayTokenSecretName); ok {
		t.Fatal("expected no RELAY_TOKEN secret to be set before the subdomain claim check")
	}
	if fake.routeEnabled {
		t.Fatal("expected EnableWorkersDevRoute to never be reached once WorkersDevSubdomain reports unclaimed")
	}
	if want := 3; fake.callCount() != want {
		t.Fatalf("expected exactly VerifyToken + ResolveAccountID + the one WorkersDevSubdomain check before failing (%d calls), got %d: %v", want, fake.callCount(), fake.calls)
	}
}

func TestDeployWorkersDevSubdomainUnclaimedViaEnableRoute10007(t *testing.T) {
	t.Parallel()

	// Belt-and-braces detection path: WorkersDevSubdomain reports the
	// account as claimed, but EnableWorkersDevRoute itself still fails with
	// Cloudflare error code 10007 (e.g. the claim state changed between the
	// two calls). Deploy must still surface ErrWorkersDevSubdomainUnclaimed
	// rather than a raw wrapped API error.
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		authOK:                    true,
		subdomain:                 "acme",
		forceEnableRouteUnclaimed: true,
	})
	_, err := Deploy(context.Background(), fake.client(), Options{})
	if !errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("Deploy() error = %v, want ErrWorkersDevSubdomainUnclaimed", err)
	}
	if fake.routeEnabled {
		t.Fatal("expected EnableWorkersDevRoute's 10007 response to leave the route disabled")
	}
}

func TestDeployWorkersDevSubdomainUnclaimedViaUpload10063(t *testing.T) {
	t.Parallel()

	// Belt-and-braces detection path, now that the primary
	// resolveWorkersDevSubdomainClaim check runs before UploadWorkerModule:
	// the account is claimed ("acme", so the check adopts it and Deploy
	// proceeds), but the upload call itself still fails with Cloudflare
	// error code 10063 ("You need a workers.dev subdomain in order to
	// proceed.") — e.g. a minimal-scope token whose write path independently
	// rejects, a race the check above cannot observe. The raw 403/10063
	// error must not leak past Deploy; it must map to the same
	// ErrWorkersDevSubdomainUnclaimed sentinel as the 10007 path.
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		authOK:           true,
		subdomain:        "acme",
		forceUpload10063: true,
	})
	_, err := Deploy(context.Background(), fake.client(), Options{})
	if !errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("Deploy() error = %v, want ErrWorkersDevSubdomainUnclaimed", err)
	}
	if strings.Contains(err.Error(), "10063") {
		t.Fatalf("Deploy() error = %v, want the raw Cloudflare error code never to leak past the sentinel", err)
	}
	if fake.routeEnabled {
		t.Fatal("expected Deploy to stop at the upload step, never reaching EnableWorkersDevRoute")
	}
}

// TestIsUnclaimedWorkersDevSubdomainErrorMatchesBothCodes covers the
// detection function directly for all three deploy steps (upload, secret,
// route) that call it, rather than standing up a fake-server scenario for
// each: TestDeployWorkersDevSubdomainUnclaimedViaUpload10063 and
// TestDeployWorkersDevSubdomainUnclaimedViaEnableRoute10007 already cover
// two of those call sites end to end, and SetSecret's own check is the same
// three lines as UploadWorkerModule's — this is what actually backs all of
// them.
func TestIsUnclaimedWorkersDevSubdomainErrorMatchesBothCodes(t *testing.T) {
	t.Parallel()

	for _, code := range []int{10007, 10063} {
		err := &CloudflareAPIError{StatusCode: 400, Messages: []CloudflareMessage{{Code: code, Message: "workers.dev subdomain required"}}}
		if !isUnclaimedWorkersDevSubdomainError(err) {
			t.Fatalf("isUnclaimedWorkersDevSubdomainError() = false for code %d, want true", code)
		}
	}

	other := &CloudflareAPIError{StatusCode: 400, Messages: []CloudflareMessage{{Code: 9109, Message: "invalid token"}}}
	if isUnclaimedWorkersDevSubdomainError(other) {
		t.Fatal("isUnclaimedWorkersDevSubdomainError() = true for an unrelated error code, want false")
	}

	if isUnclaimedWorkersDevSubdomainError(errors.New("not a cloudflare api error")) {
		t.Fatal("isUnclaimedWorkersDevSubdomainError() = true for a non-CloudflareAPIError, want false")
	}
}

func TestDeployAuthFailureShortCircuits(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: false, authStatus: 403})
	_, err := Deploy(context.Background(), fake.client(), Options{})
	if err == nil {
		t.Fatal("Deploy() error = nil, want auth failure")
	}
	if !strings.Contains(err.Error(), "verify Cloudflare API token") {
		t.Fatalf("Deploy() error = %v, want it to name the failing step", err)
	}
	if fake.uploadedScript != nil {
		t.Fatal("expected Deploy to stop before uploading the worker on auth failure")
	}
	if fake.callCount() != 1 {
		t.Fatalf("expected exactly one Cloudflare API call before short-circuiting, got %d", fake.callCount())
	}
}

func TestDeployAuthFailureWithStoredTokenNamesFileAndSuggestsForgetToken(t *testing.T) {
	t.Parallel()

	// A 401/403 while deploying with a stored per-network token
	// (Options.StoredTokenPath set) must name the stored file and suggest
	// --forget-token or the env override, not just a raw Cloudflare error.
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: false, authStatus: 401})
	_, err := Deploy(context.Background(), fake.client(), Options{StoredTokenPath: "/home/alice/.moltnet/acme-net/.moltnet/cloudflare.json"})
	if err == nil {
		t.Fatal("Deploy() error = nil, want auth failure")
	}
	if !strings.Contains(err.Error(), "/home/alice/.moltnet/acme-net/.moltnet/cloudflare.json") {
		t.Fatalf("Deploy() error = %v, want it to name the stored token file", err)
	}
	if !strings.Contains(err.Error(), "--forget-token") {
		t.Fatalf("Deploy() error = %v, want it to suggest --forget-token", err)
	}
	if !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
		t.Fatalf("Deploy() error = %v, want it to suggest the CLOUDFLARE_API_TOKEN env override", err)
	}
	var apiErr *CloudflareAPIError
	if !errors.As(err, &apiErr) {
		t.Fatal("expected the wrapped error to still unwrap to a *CloudflareAPIError via errors.As")
	}
}

func TestDeployAuthFailureWithoutStoredTokenPathIsUnwrapped(t *testing.T) {
	t.Parallel()

	// Options.StoredTokenPath empty (the common case: env/--token-env
	// supplied the token) must leave the error exactly as the unwrapped
	// deploy would have returned it — no stored-file guidance appended.
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: false, authStatus: 403})
	_, err := Deploy(context.Background(), fake.client(), Options{})
	if err == nil {
		t.Fatal("Deploy() error = nil, want auth failure")
	}
	if strings.Contains(err.Error(), "--forget-token") {
		t.Fatalf("Deploy() error = %v, want no --forget-token guidance when no stored token was used", err)
	}
}

func TestWrapStoredTokenErrorPassesThroughNonAuthErrors(t *testing.T) {
	t.Parallel()

	original := errors.New("some other cloudflare failure")
	got := wrapStoredTokenError(original, "/home/alice/.moltnet/acme-net/.moltnet/cloudflare.json")
	if got != original {
		t.Fatalf("wrapStoredTokenError() = %v, want the original error unchanged for a non-auth error", got)
	}
}

func TestWorkerMainModuleFallsBackWhenMissing(t *testing.T) {
	t.Parallel()
	if got := workerMainModule([]byte(`{}`)); got != "worker.js" {
		t.Fatalf("workerMainModule({}) = %q, want worker.js", got)
	}
	if got := workerMainModule([]byte(`not json`)); got != "worker.js" {
		t.Fatalf("workerMainModule(invalid) = %q, want worker.js", got)
	}
	if got := workerMainModule([]byte(`{"main_module":"index.mjs"}`)); got != "index.mjs" {
		t.Fatalf("workerMainModule() = %q, want index.mjs", got)
	}
}
