package relaydeploy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests cover Options.Subdomain — the non-interactive workers.dev
// subdomain claim path `moltnet relay deploy --subdomain` (cmd/moltnet)
// plumbs through resolveWorkersDevSubdomainClaim (deploy.go) — and the
// claim-state-before-any-mutation ordering it shares with the plain
// ErrWorkersDevSubdomainUnclaimed path covered in deploy_test.go.

// containsCall reports whether calls includes an exact "METHOD /path" entry,
// so a test can assert a specific Cloudflare endpoint was, or was not, ever
// hit — not just that some number of calls happened.
func containsCall(calls []string, methodAndPath string) bool {
	for _, call := range calls {
		if call == methodAndPath {
			return true
		}
	}
	return false
}

// TestDeployOptionsSubdomainClaimsWhenUnclaimed covers the "no claim,
// --subdomain given" branch: Deploy claims the requested name itself,
// before ever uploading the Worker, and the deploy then proceeds and
// succeeds using it.
func TestDeployOptionsSubdomainClaimsWhenUnclaimed(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID:      "acct-claim-flag",
		authOK:         true,
		subdomain:      "", // unclaimed until Deploy's own claim call
		claimSubdomain: "apresmoi",
	})

	result, err := Deploy(context.Background(), fake.client(), Options{
		Subdomain:       "apresmoi",
		ResolveHostname: stubResolveHostname(true),
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Subdomain != "apresmoi" {
		t.Fatalf("Result.Subdomain = %q, want apresmoi", result.Subdomain)
	}
	if result.Hostname != "moltnet-relay.apresmoi.workers.dev" {
		t.Fatalf("Result.Hostname = %q, want moltnet-relay.apresmoi.workers.dev", result.Hostname)
	}
	if !containsCall(fake.calls, "PUT /accounts/acct-claim-flag/workers/subdomain") {
		t.Fatalf("expected the claim PUT to have been made, calls = %v", fake.calls)
	}
	// The claim PUT must precede the upload PUT — the whole point of the
	// reorder this flag depends on.
	claimIndex, uploadIndex := -1, -1
	for i, call := range fake.calls {
		if call == "PUT /accounts/acct-claim-flag/workers/subdomain" {
			claimIndex = i
		}
		if strings.HasPrefix(call, "PUT /accounts/acct-claim-flag/workers/scripts/") && !strings.HasSuffix(call, "/secrets") {
			uploadIndex = i
		}
	}
	if claimIndex == -1 || uploadIndex == -1 || claimIndex > uploadIndex {
		t.Fatalf("expected the claim PUT (index %d) before the upload PUT (index %d), calls = %v", claimIndex, uploadIndex, fake.calls)
	}
}

// TestDeployOptionsSubdomainNormalizesCase covers NormalizeWorkersDevSubdomainName
// being applied inside resolveWorkersDevSubdomainClaim itself, not just by
// the CLI layer: an uppercase Options.Subdomain must still match a
// lowercase claimSubdomain.
func TestDeployOptionsSubdomainNormalizesCase(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		authOK:         true,
		subdomain:      "",
		claimSubdomain: "apresmoi",
	})

	result, err := Deploy(context.Background(), fake.client(), Options{
		Subdomain:       "APresMoi",
		ResolveHostname: stubResolveHostname(true),
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Subdomain != "apresmoi" {
		t.Fatalf("Result.Subdomain = %q, want apresmoi (normalized)", result.Subdomain)
	}
}

// TestDeployOptionsSubdomainInvalidNameRejectedBeforeAnyClaimAttempt covers
// resolveWorkersDevSubdomainClaim's own defense-in-depth validation (the CLI
// already validates --subdomain before calling Deploy, but Deploy is a
// public library entry point other callers can use directly): an invalid
// name is rejected locally, without ever attempting the claim PUT. The
// account-state GET (WorkersDevSubdomain) still runs first regardless of
// opts.Subdomain — it is what determines whether a claim is even needed —
// so this only asserts the mutating PUT never happens, not that the GET
// endpoint is untouched.
func TestDeployOptionsSubdomainInvalidNameRejectedBeforeAnyClaimAttempt(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: ""})

	_, err := Deploy(context.Background(), fake.client(), Options{Subdomain: "-not-valid-"})
	if err == nil {
		t.Fatal("Deploy() error = nil, want a validation error")
	}
	if errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("Deploy() error = %v, want the local validation error, not the generic unclaimed sentinel", err)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "PUT ") && strings.Contains(call, "workers/subdomain") {
			t.Fatalf("expected no claim PUT for a locally-invalid name, calls = %v", fake.calls)
		}
	}
}

// TestDeployOptionsSubdomainNameTakenTranslatesErrorMessage pins the fix for
// a real UX inconsistency: Cloudflare error code 10031 ("subdomain already
// exists" -- a different account already holds the requested name) used to
// leak through Deploy's error raw, while the interactive claim prompt
// (cmd/moltnet/relay_deploy_subdomain_claim.go) already translated the same
// code to a friendly "that name is taken, try another" message. The
// non-interactive --subdomain path deserves the same clarity.
func TestDeployOptionsSubdomainNameTakenTranslatesErrorMessage(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		authOK: true,
		// subdomain left "" (account unclaimed) so resolveWorkersDevSubdomainClaim
		// reaches the claim PUT at all; claimSubdomain names the only value
		// the fake server accepts, so requesting anything else there
		// triggers its simulated 10031 response.
		subdomain:      "",
		claimSubdomain: "apresmoi",
	})

	_, err := Deploy(context.Background(), fake.client(), Options{Subdomain: "someone-else"})
	if err == nil {
		t.Fatal("Deploy() error = nil, want the 10031 name-taken error")
	}
	if errors.Is(err, ErrWorkersDevSubdomainConflict) || errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("Deploy() error = %v, want the name-taken error, not one of the other sentinels", err)
	}
	if !strings.Contains(err.Error(), "that name is taken, try another") {
		t.Fatalf("Deploy() error = %v, want the friendly \"that name is taken, try another\" translation", err)
	}
}

// TestDeployOptionsSubdomainInvalidNameRejectedWhenAlreadyClaimed covers the
// hoisted-validation fix: a malformed Options.Subdomain must be reported as
// an invalid name even when the account already has a (different) claim --
// before this fix, that branch never ran ValidateWorkersDevSubdomainName at
// all, so a malformed name was reported as a conflict against the account's
// real claim instead of as the actual problem (its own shape).
func TestDeployOptionsSubdomainInvalidNameRejectedWhenAlreadyClaimed(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})

	_, err := Deploy(context.Background(), fake.client(), Options{Subdomain: "-not-valid-"})
	if err == nil {
		t.Fatal("Deploy() error = nil, want a validation error")
	}
	if errors.Is(err, ErrWorkersDevSubdomainConflict) {
		t.Fatalf("Deploy() error = %v, want the local validation error, not ErrWorkersDevSubdomainConflict", err)
	}
	if !strings.Contains(err.Error(), "invalid workers.dev subdomain name") {
		t.Fatalf("Deploy() error = %v, want it to name the shape problem, not a conflict", err)
	}
}

// TestDeployOptionsSubdomainSetsClaimedOnlyForAFreshClaim pins Result.Claimed:
// true only when this exact call performed a brand-new, permanent claim,
// false on the adopt-existing-claim path even though Result.Subdomain is
// populated the same way in both cases -- the whole point of the field is
// distinguishing the two, which were otherwise indistinguishable to a
// caller (deploy.go's own TODO this closes).
func TestDeployOptionsSubdomainSetsClaimedOnlyForAFreshClaim(t *testing.T) {
	t.Parallel()

	fresh := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID:      "acct-fresh-claim",
		authOK:         true,
		subdomain:      "",
		claimSubdomain: "apresmoi",
	})
	result, err := Deploy(context.Background(), fresh.client(), Options{
		Subdomain:       "apresmoi",
		ResolveHostname: stubResolveHostname(true),
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if !result.Claimed {
		t.Fatal("Result.Claimed = false, want true for a brand-new claim")
	}

	adopt := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID: "acct-adopt-claimed-flag",
		authOK:    true,
		subdomain: "acme",
	})
	result, err = Deploy(context.Background(), adopt.client(), Options{
		Subdomain:       "acme",
		ResolveHostname: stubResolveHostname(true),
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Claimed {
		t.Fatal("Result.Claimed = true, want false for adopting an existing claim")
	}
}

// TestDeployOptionsSubdomainAdoptsExistingMatchingClaimWithoutReclaiming
// covers the "existing account claim → adopt it, proceed" branch: passing
// --subdomain naming the account's own already-claimed subdomain must not
// re-claim it (no PUT at all — ClaimWorkersDevSubdomain is a one-time
// operation).
func TestDeployOptionsSubdomainAdoptsExistingMatchingClaimWithoutReclaiming(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID: "acct-adopt",
		authOK:    true,
		subdomain: "acme",
	})

	result, err := Deploy(context.Background(), fake.client(), Options{
		Subdomain:       "acme",
		ResolveHostname: stubResolveHostname(true),
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Subdomain != "acme" {
		t.Fatalf("Result.Subdomain = %q, want acme", result.Subdomain)
	}
	if containsCall(fake.calls, "PUT /accounts/acct-adopt/workers/subdomain") {
		t.Fatalf("expected no re-claim PUT for a name matching the existing claim, calls = %v", fake.calls)
	}
}

// TestDeployAdoptsExistingClaimWithNoSubdomainOptionEither covers the same
// adopt branch with no --subdomain at all (the common re-deploy case): an
// already-claimed account just proceeds, exactly as before this fix.
func TestDeployAdoptsExistingClaimWithNoSubdomainOptionEither(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{accountID: "acct-adopt-2", authOK: true, subdomain: "acme"})

	result, err := Deploy(context.Background(), fake.client(), Options{ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Subdomain != "acme" {
		t.Fatalf("Result.Subdomain = %q, want acme", result.Subdomain)
	}
	if containsCall(fake.calls, "PUT /accounts/acct-adopt-2/workers/subdomain") {
		t.Fatalf("expected no claim PUT when no --subdomain was requested, calls = %v", fake.calls)
	}
}

// TestDeployOptionsSubdomainConflictsWithExistingClaim covers "a requested
// name that conflicts with a different existing claim → refuse": Deploy
// must return ErrWorkersDevSubdomainConflict before any remote mutation —
// no claim PUT, and (this being the whole point of the reorder) no worker
// upload either.
func TestDeployOptionsSubdomainConflictsWithExistingClaim(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID: "acct-conflict",
		authOK:    true,
		subdomain: "acme",
	})

	result, err := Deploy(context.Background(), fake.client(), Options{Subdomain: "someone-else"})
	if !errors.Is(err, ErrWorkersDevSubdomainConflict) {
		t.Fatalf("Deploy() error = %v, want ErrWorkersDevSubdomainConflict", err)
	}
	if !strings.Contains(err.Error(), "someone-else") || !strings.Contains(err.Error(), "acme") {
		t.Fatalf("Deploy() error = %v, want it to name both the requested and existing subdomains", err)
	}
	if result.AccountID != "acct-conflict" {
		t.Fatalf("Result.AccountID = %q, want acct-conflict even on ErrWorkersDevSubdomainConflict", result.AccountID)
	}
	if fake.uploadedScript != nil {
		t.Fatal("expected no worker upload on a subdomain-claim conflict")
	}
	if containsCall(fake.calls, "PUT /accounts/acct-conflict/workers/subdomain") {
		t.Fatalf("expected no claim PUT attempt for a name that conflicts with an existing claim, calls = %v", fake.calls)
	}
}
