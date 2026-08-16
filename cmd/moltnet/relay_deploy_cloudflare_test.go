package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// fakeCloudflareCLIServer is a minimal net/http/httptest stand-in for the
// Cloudflare REST API, covering exactly the calls a full relaydeploy.Deploy
// happy path makes. internal/relaydeploy has its own richer fake (used to
// unit test the client and Deploy orchestration in isolation); this one
// exists only so cmd/moltnet's CLI-layer tests can drive `relay deploy`
// end to end — flags, token resolution, save/prompt, output — without a
// real Cloudflare account, and cannot reuse relaydeploy's unexported fake
// since it lives in a different package.
type fakeCloudflareCLIServer struct {
	Server *httptest.Server
	// wantToken, when non-empty, makes every call require this exact
	// bearer token, returning 401 otherwise — how tests prove which token
	// (env vs. stored) a deploy actually used.
	wantToken string

	// acceptClaimName, when non-empty, makes handleClaimSubdomain (PUT
	// /workers/subdomain) accept only this exact name, rejecting any other
	// with Cloudflare's documented error code 10031 ("name already taken";
	// see internal/relaydeploy's ClaimWorkersDevSubdomain doc comment).
	acceptClaimName string

	// rejectClaimAsAccountAlreadyHasSubdomain, when true, makes
	// handleClaimSubdomain always reject with Cloudflare's documented error
	// code 10036 ("account already has a subdomain", HTTP 409) instead of
	// ever consulting acceptClaimName. Pairs with subdomain already being
	// set (see startFakeCloudflareCLIServerAccountAlreadyHasSubdomain): the
	// account genuinely already has a claim, so the post-rejection GET
	// attemptInteractiveWorkersDevSubdomainClaim makes on that code finds
	// it via the normal handleGetSubdomain path, unchanged.
	rejectClaimAsAccountAlreadyHasSubdomain bool

	// simulateClaimLag, when true, makes a successful handleClaimSubdomain
	// PUT accept the name (so the claim itself is reported as succeeded)
	// without actually updating f.subdomain — standing in for Cloudflare's
	// real read-after-write lag on the account-level subdomain claim: a
	// redeploy immediately afterward still finds the account unclaimed.
	simulateClaimLag bool

	// uploadAndSecretSucceedWhenUnclaimed, when true, makes handleOK ignore
	// f.subdomain entirely and always succeed — standing in for an account
	// where Cloudflare's upload/secret steps never surface error code 10063
	// (unlike startFakeCloudflareCLIServerUnclaimed's default, the
	// field-observed 10063-at-upload failure mode), so
	// relaydeploy.Deploy's *own* WorkersDevSubdomain GET check (deploy.go)
	// is what first detects the account as unclaimed — the P1 regression
	// this fake exists to cover (that primary detection path must still
	// populate Result.AccountID, not just the upload-10063 belt-and-braces
	// path already covered elsewhere).
	uploadAndSecretSucceedWhenUnclaimed bool

	mu sync.Mutex
	// subdomain is this account's claimed workers.dev subdomain: "acme" for
	// every existing test (startFakeCloudflareCLIServer's default), or ""
	// (unclaimed) for the interactive-claim-flow tests
	// (startFakeCloudflareCLIServerUnclaimed), in which case the upload,
	// secret, and route-enable steps mirror the field failure this fix
	// covers — Cloudflare error code 10063 — until a PUT
	// /workers/subdomain claims one.
	subdomain string
}

func startFakeCloudflareCLIServer(t *testing.T, wantToken string) *fakeCloudflareCLIServer {
	t.Helper()
	return newFakeCloudflareCLIServer(t, wantToken, "acme", "")
}

// startFakeCloudflareCLIServerUnclaimed is startFakeCloudflareCLIServer's
// counterpart for the interactive workers.dev subdomain claim tests
// (relay_deploy_subdomain_claim_test.go): the account starts with no
// claimed subdomain, so upload/secret/route all fail with Cloudflare error
// code 10063 until a PUT /workers/subdomain claims acceptClaimName.
func startFakeCloudflareCLIServerUnclaimed(t *testing.T, wantToken, acceptClaimName string) *fakeCloudflareCLIServer {
	t.Helper()
	return newFakeCloudflareCLIServer(t, wantToken, "", acceptClaimName)
}

// startFakeCloudflareCLIServerUnclaimedViaSubdomainCheck is
// startFakeCloudflareCLIServerUnclaimed's counterpart for the P1 regression
// test covering the *primary* ErrWorkersDevSubdomainUnclaimed detection
// path: unlike that fake, upload and secret both succeed even though the
// account has no claimed subdomain (uploadAndSecretSucceedWhenUnclaimed), so
// it is relaydeploy.Deploy's own WorkersDevSubdomain GET check — not a
// 10063 upload/secret rejection — that first detects the account as
// unclaimed and returns relaydeploy.ErrWorkersDevSubdomainUnclaimed.
func startFakeCloudflareCLIServerUnclaimedViaSubdomainCheck(t *testing.T, wantToken, acceptClaimName string) *fakeCloudflareCLIServer {
	t.Helper()
	fake := newFakeCloudflareCLIServer(t, wantToken, "", acceptClaimName)
	fake.uploadAndSecretSucceedWhenUnclaimed = true
	return fake
}

// startFakeCloudflareCLIServerClaimLag is startFakeCloudflareCLIServerUnclaimed's
// counterpart for the P2 post-claim propagation-lag messaging test: a PUT
// /workers/subdomain claiming acceptClaimName is reported as succeeding,
// but f.subdomain (backing GET /workers/subdomain and the upload/secret/
// route steps) is deliberately never actually updated — see
// fakeCloudflareCLIServer.simulateClaimLag — modeling Cloudflare's real
// read-after-write lag: a redeploy immediately after a successful claim
// can still see the account as unclaimed for a moment.
func startFakeCloudflareCLIServerClaimLag(t *testing.T, wantToken, acceptClaimName string) *fakeCloudflareCLIServer {
	t.Helper()
	fake := newFakeCloudflareCLIServer(t, wantToken, "", acceptClaimName)
	fake.simulateClaimLag = true
	return fake
}

// startFakeCloudflareCLIServerAccountAlreadyHasSubdomain is a fake whose
// account already has claimedName as its workers.dev subdomain, but whose
// PUT /workers/subdomain always rejects any claim attempt with Cloudflare's
// documented error code 10036 ("account already has a subdomain") rather
// than ever accepting one — the P2 real-error-codes fix's "continue as
// claimed" path: attemptInteractiveWorkersDevSubdomainClaim must treat this
// as success, not a rejection to retry, discovering claimedName via a
// follow-up GET.
func startFakeCloudflareCLIServerAccountAlreadyHasSubdomain(t *testing.T, wantToken, claimedName string) *fakeCloudflareCLIServer {
	t.Helper()
	fake := newFakeCloudflareCLIServer(t, wantToken, claimedName, "")
	fake.rejectClaimAsAccountAlreadyHasSubdomain = true
	return fake
}

// startFakeCloudflareCLIServerAccountAlreadyHasSubdomainPropagationLag is
// startFakeCloudflareCLIServerAccountAlreadyHasSubdomain's counterpart for
// the P3 copy fix (relay_deploy_subdomain_claim.go): PUT /workers/subdomain
// still always rejects with Cloudflare's documented error code 10036
// ("account already has a subdomain"), but unlike that fake, the account's
// claim is never visible via the follow-up GET either — f.subdomain stays
// "" — modeling Cloudflare's own read-after-write lag on the very claim it
// just told us about via 10036. attemptInteractiveWorkersDevSubdomainClaim
// must treat this as a terminal, non-retryable case (propagationPending)
// rather than burning its remaining attempts on names that were never the
// problem.
func startFakeCloudflareCLIServerAccountAlreadyHasSubdomainPropagationLag(t *testing.T, wantToken string) *fakeCloudflareCLIServer {
	t.Helper()
	fake := newFakeCloudflareCLIServer(t, wantToken, "", "")
	fake.rejectClaimAsAccountAlreadyHasSubdomain = true
	return fake
}

func newFakeCloudflareCLIServer(t *testing.T, wantToken, initialSubdomain, acceptClaimName string) *fakeCloudflareCLIServer {
	t.Helper()
	fake := &fakeCloudflareCLIServer{wantToken: wantToken, subdomain: initialSubdomain, acceptClaimName: acceptClaimName}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /user/tokens/verify", fake.authenticated(fake.handleVerify))
	mux.HandleFunc("GET /accounts", fake.authenticated(fake.handleAccounts))
	mux.HandleFunc("PUT /accounts/{account}/workers/scripts/{script}", fake.authenticated(fake.handleOK))
	mux.HandleFunc("PUT /accounts/{account}/workers/scripts/{script}/secrets", fake.authenticated(fake.handleOK))
	mux.HandleFunc("POST /accounts/{account}/workers/scripts/{script}/subdomain", fake.authenticated(fake.handleOK))
	mux.HandleFunc("GET /accounts/{account}/workers/subdomain", fake.authenticated(fake.handleGetSubdomain))
	mux.HandleFunc("PUT /accounts/{account}/workers/subdomain", fake.authenticated(fake.handleClaimSubdomain))

	fake.Server = httptest.NewServer(mux)
	t.Cleanup(fake.Server.Close)
	return fake
}

func (f *fakeCloudflareCLIServer) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if f.wantToken != "" && r.Header.Get("Authorization") != "Bearer "+f.wantToken {
			writeFakeCloudflareEnvelope(w, http.StatusUnauthorized, false, nil)
			return
		}
		next(w, r)
	}
}

func (f *fakeCloudflareCLIServer) handleVerify(w http.ResponseWriter, r *http.Request) {
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{"id":"test-token","status":"active"}`))
}

func (f *fakeCloudflareCLIServer) handleAccounts(w http.ResponseWriter, r *http.Request) {
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`[{"id":"account-1","name":"acme"}]`))
}

// handleOK backs the worker upload, secret, and route-enable calls. Once
// this account has a claimed subdomain (the common case: every existing
// test's default "acme"), each just needs a bare success:true envelope for
// Deploy to proceed. Before a claim (startFakeCloudflareCLIServerUnclaimed),
// it instead returns Cloudflare error code 10063 — the field-observed
// failure this fix maps to relaydeploy.ErrWorkersDevSubdomainUnclaimed at
// every step that can hit it, not just route-enable.
func (f *fakeCloudflareCLIServer) handleOK(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	claimed := f.subdomain != "" || f.uploadAndSecretSucceedWhenUnclaimed
	f.mu.Unlock()
	if !claimed {
		writeFakeCloudflareEnvelopeError(w, http.StatusBadRequest, 10063, "You need a workers.dev subdomain in order to proceed.")
		return
	}
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{}`))
}

func (f *fakeCloudflareCLIServer) handleGetSubdomain(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	subdomain := f.subdomain
	f.mu.Unlock()
	if subdomain == "" {
		writeFakeCloudflareEnvelopeError(w, http.StatusNotFound, 10007, "workers.dev subdomain not found")
		return
	}
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(fmt.Sprintf(`{"subdomain":%q}`, subdomain)))
}

// handleClaimSubdomain backs the interactive claim prompt's PUT
// (relaydeploy.Client.ClaimWorkersDevSubdomain). Precedence: (1)
// rejectClaimAsAccountAlreadyHasSubdomain, when set, always rejects with
// Cloudflare's documented error code 10036 ("account already has a
// subdomain", HTTP 409), regardless of the name submitted; otherwise (2)
// only acceptClaimName (when set) is accepted, and anything else —
// including a resubmission of a name already rejected — is refused with
// Cloudflare's documented error code 10031 ("name already taken"); a
// successful claim updates f.subdomain unless simulateClaimLag is set (see
// its doc comment).
func (f *fakeCloudflareCLIServer) handleClaimSubdomain(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Subdomain string `json:"subdomain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.rejectClaimAsAccountAlreadyHasSubdomain {
		writeFakeCloudflareEnvelopeError(w, http.StatusConflict, 10036, "account already has a subdomain")
		return
	}
	if f.acceptClaimName == "" || payload.Subdomain != f.acceptClaimName {
		writeFakeCloudflareEnvelopeError(w, http.StatusBadRequest, 10031, "subdomain already exists")
		return
	}
	f.mu.Lock()
	if !f.simulateClaimLag {
		f.subdomain = payload.Subdomain
	}
	f.mu.Unlock()
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(fmt.Sprintf(`{"subdomain":%q}`, payload.Subdomain)))
}

func writeFakeCloudflareEnvelope(w http.ResponseWriter, status int, success bool, result json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	envelope := struct {
		Success bool             `json:"success"`
		Errors  []map[string]any `json:"errors"`
		Result  json.RawMessage  `json:"result"`
	}{Success: success, Result: result}
	if !success {
		envelope.Errors = []map[string]any{{"code": 9109, "message": "invalid token"}}
	}
	_ = json.NewEncoder(w).Encode(envelope)
}

// writeFakeCloudflareEnvelopeError writes a failure envelope carrying a
// specific error code and message, unlike writeFakeCloudflareEnvelope's
// hardcoded auth-failure shape — used by handleOK, handleGetSubdomain, and
// handleClaimSubdomain above to return the specific Cloudflare error codes
// their scenarios need (10063, 10007, 10036).
func writeFakeCloudflareEnvelopeError(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	envelope := struct {
		Success bool             `json:"success"`
		Errors  []map[string]any `json:"errors"`
		Result  json.RawMessage  `json:"result"`
	}{Success: false, Errors: []map[string]any{{"code": code, "message": message}}}
	_ = json.NewEncoder(w).Encode(envelope)
}

// withRelayDeployFakeCloudflare points newRelayDeployClient at fake, and
// resolveRelayDeployHostname at a stub reporting the deployed hostname as
// resolved, for the test's duration, restoring both afterward. Without the
// stub, a deploy through the fake server would fall through to Deploy's
// real DNS lookup on the fake's script.acme.workers.dev hostname — a live
// network call in every test, and one that is not actually reliable: the
// workers.dev wildcard DNS record resolves for essentially any subdomain
// whether or not a script was ever deployed there, so the "not resolving
// yet" note this stub avoids is not something these tests could count on
// either way.
func withRelayDeployFakeCloudflare(t *testing.T, fake *fakeCloudflareCLIServer) {
	t.Helper()
	previous := newRelayDeployClient
	newRelayDeployClient = func(token string) *relaydeploy.Client {
		return relaydeploy.NewClientForTesting(token, fake.Server.URL, fake.Server.Client())
	}
	t.Cleanup(func() { newRelayDeployClient = previous })

	previousResolve := resolveRelayDeployHostname
	resolveRelayDeployHostname = func(ctx context.Context, hostname string) bool { return true }
	t.Cleanup(func() { resolveRelayDeployHostname = previousResolve })
}

// TestRunRelayDeployNotResolvingNoteNamesHostname covers the P2-5 fix: when
// the freshly deployed hostname is not resolving yet
// (Result.HostnameResolved false), the printed note must name the specific
// hostname that isn't resolving, not just the generic "DNS can take a few
// minutes" reason — restoring the detected fact the guide's own prose
// ("it says so") promises. Unlike withRelayDeployFakeCloudflare's default
// stub (always resolved, so most tests never hit a live DNS lookup), this
// overrides resolveRelayDeployHostname to report unresolved instead.
func TestRunRelayDeployNotResolvingNoteNamesHostname(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	previousResolve := resolveRelayDeployHostname
	resolveRelayDeployHostname = func(ctx context.Context, hostname string) bool { return false }
	t.Cleanup(func() { resolveRelayDeployHostname = previousResolve })

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	// startFakeCloudflareCLIServer's default subdomain is "acme" and the
	// default script name is moltnet-relay, so the deployed hostname is
	// deterministic.
	const wantHostname = "moltnet-relay.acme.workers.dev"
	if !strings.Contains(output, wantHostname+" is not resolving yet") {
		t.Fatalf("expected the note to name %q as not resolving yet, got %q", wantHostname, output)
	}
	if !strings.Contains(output, "workers.dev DNS can take a few minutes") {
		t.Fatalf("expected the note to keep the DNS-propagation reason, got %q", output)
	}
}

// cloudflareTokenFileContents reads back the token saved at path,
// t.Fatal-ing if nothing was saved there.
func cloudflareTokenFileContents(t *testing.T, path string) string {
	t.Helper()
	token, ok, err := relaydeploy.LoadCloudflareToken(path)
	if err != nil {
		t.Fatalf("LoadCloudflareToken(%q) error = %v", path, err)
	}
	if !ok {
		t.Fatalf("LoadCloudflareToken(%q) ok = false, want a saved token", path)
	}
	return token
}
