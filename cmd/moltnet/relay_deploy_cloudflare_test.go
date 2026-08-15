package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}

func startFakeCloudflareCLIServer(t *testing.T, wantToken string) *fakeCloudflareCLIServer {
	t.Helper()
	fake := &fakeCloudflareCLIServer{wantToken: wantToken}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /user/tokens/verify", fake.authenticated(fake.handleVerify))
	mux.HandleFunc("GET /accounts", fake.authenticated(fake.handleAccounts))
	mux.HandleFunc("PUT /accounts/{account}/workers/scripts/{script}", fake.authenticated(fake.handleOK))
	mux.HandleFunc("PUT /accounts/{account}/workers/scripts/{script}/secrets", fake.authenticated(fake.handleOK))
	mux.HandleFunc("POST /accounts/{account}/workers/scripts/{script}/subdomain", fake.authenticated(fake.handleOK))
	mux.HandleFunc("GET /accounts/{account}/workers/subdomain", fake.authenticated(fake.handleSubdomain))

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

// handleOK backs the worker upload, secret, and route-enable calls: each
// only needs a bare success:true envelope for Deploy to proceed.
func (f *fakeCloudflareCLIServer) handleOK(w http.ResponseWriter, r *http.Request) {
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{}`))
}

func (f *fakeCloudflareCLIServer) handleSubdomain(w http.ResponseWriter, r *http.Request) {
	writeFakeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{"subdomain":"acme"}`))
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
