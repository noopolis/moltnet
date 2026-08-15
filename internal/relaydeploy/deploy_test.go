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

func TestDeployWorkersDevSubdomainUnclaimed(t *testing.T) {
	t.Parallel()

	// Primary detection path: WorkersDevSubdomain itself reports the
	// account has no claimed subdomain. Deploy must catch this before ever
	// calling EnableWorkersDevRoute (which the real Cloudflare API would
	// fail with error code 10007 for the same reason).
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: ""})
	_, err := Deploy(context.Background(), fake.client(), Options{})
	if !errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("Deploy() error = %v, want ErrWorkersDevSubdomainUnclaimed", err)
	}
	// The worker was still uploaded and the secret still set; only the
	// route/URL step is blocked on the one-time dashboard claim.
	if fake.uploadedScript == nil {
		t.Fatal("expected the worker upload to have happened before the subdomain check")
	}
	if fake.routeEnabled {
		t.Fatal("expected EnableWorkersDevRoute to never be reached once WorkersDevSubdomain reports unclaimed")
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
