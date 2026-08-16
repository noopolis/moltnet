package relaydeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeCloudflareConfig configures the scenarios client_test.go and
// deploy_test.go exercise against newFakeCloudflareServer: the happy path,
// an unclaimed workers.dev subdomain, and an auth failure.
type fakeCloudflareConfig struct {
	accountID string

	authOK     bool
	authStatus int // used when authOK is false; defaults to 403

	subdomain       string // "" means unclaimed
	subdomainStatus int    // used when subdomain == ""; defaults to 404

	// forceEnableRouteUnclaimed makes handleEnableRoute return the same
	// 10007 "workers.dev subdomain required" error Cloudflare returns for an
	// unclaimed account, even when subdomain is claimed. It exercises
	// Deploy's belt-and-braces detection path (EnableWorkersDevRoute itself
	// returning 10007), as opposed to the primary path where
	// WorkersDevSubdomain already reports the account unclaimed.
	forceEnableRouteUnclaimed bool

	// forceUpload10063 makes handleUploadWorker fail with Cloudflare error
	// code 10063 ("You need a workers.dev subdomain in order to proceed."),
	// the field-observed failure mode: a minimal-scope token whose auth
	// itself succeeds, but where Cloudflare rejects the worker upload
	// itself — before Deploy ever reaches the WorkersDevSubdomain check —
	// on an account with no claimed subdomain.
	forceUpload10063 bool

	// claimSubdomain, when set, is the only subdomain name
	// handleClaimSubdomain (PUT) accepts; any other name (including empty)
	// is rejected with Cloudflare's documented error code 10031 ("name
	// already taken"). Once claimed, subsequent GET /workers/subdomain
	// calls report it via claimedSubdomain (set on a successful PUT),
	// regardless of the static cfg.subdomain configured above — mirroring
	// the real account-level claim's effect on every later call.
	claimSubdomain string
}

// fakeCloudflareServer is a stdlib net/http/httptest stand-in for the
// Cloudflare REST API, exercising Client without any real network access.
type fakeCloudflareServer struct {
	Server *httptest.Server
	cfg    fakeCloudflareConfig

	mu               sync.Mutex
	calls            []string
	uploadedScript   []byte
	uploadedMetadata []byte
	secrets          map[string]string
	routeEnabled     bool
	claimedSubdomain string // set by a successful handleClaimSubdomain PUT
}

func newFakeCloudflareServer(t *testing.T, cfg fakeCloudflareConfig) *fakeCloudflareServer {
	t.Helper()
	if cfg.accountID == "" {
		cfg.accountID = "account-1"
	}

	fake := &fakeCloudflareServer{cfg: cfg, secrets: map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /user/tokens/verify", fake.handleVerify)
	mux.HandleFunc("GET /accounts", fake.handleAccounts)
	mux.HandleFunc("PUT /accounts/{account}/workers/scripts/{script}", fake.handleUploadWorker)
	mux.HandleFunc("PUT /accounts/{account}/workers/scripts/{script}/secrets", fake.handleSetSecret)
	mux.HandleFunc("POST /accounts/{account}/workers/scripts/{script}/subdomain", fake.handleEnableRoute)
	mux.HandleFunc("GET /accounts/{account}/workers/subdomain", fake.handleWorkersDevSubdomain)
	mux.HandleFunc("PUT /accounts/{account}/workers/subdomain", fake.handleClaimSubdomain)

	fake.Server = httptest.NewServer(mux)
	t.Cleanup(fake.Server.Close)
	return fake
}

// client returns a Client pointed at this fake server. Tests are in the
// same package, so unexported Client fields can be set directly rather than
// going through NewClient (which always targets the real API host).
func (f *fakeCloudflareServer) client() *Client {
	return &Client{baseURL: f.Server.URL, token: "test-cloudflare-token", http: f.Server.Client()}
}

func (f *fakeCloudflareServer) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
}

func (f *fakeCloudflareServer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeCloudflareServer) secretValue(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.secrets[name]
	return value, ok
}

func (f *fakeCloudflareServer) handleVerify(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if !f.cfg.authOK {
		status := f.cfg.authStatus
		if status == 0 {
			status = http.StatusForbidden
		}
		writeCloudflareEnvelope(w, status, false, nil, []CloudflareMessage{{Code: 9109, Message: "invalid token"}})
		return
	}
	writeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{"id":"test-token","status":"active"}`), nil)
}

func (f *fakeCloudflareServer) handleAccounts(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	result := json.RawMessage(fmt.Sprintf(`[{"id":%q,"name":"acme"}]`, f.cfg.accountID))
	writeCloudflareEnvelope(w, http.StatusOK, true, result, nil)
}

func (f *fakeCloudflareServer) handleUploadWorker(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if f.cfg.forceUpload10063 {
		writeCloudflareEnvelope(w, http.StatusBadRequest, false, nil, []CloudflareMessage{{Code: 10063, Message: "You need a workers.dev subdomain in order to proceed."}})
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	metadata := []byte(r.FormValue("metadata"))
	if err := validateUploadWorkerMetadataShape(metadata); err != nil {
		writeCloudflareEnvelope(w, http.StatusBadRequest, false, nil, []CloudflareMessage{{Code: 10021, Message: err.Error()}})
		return
	}

	f.mu.Lock()
	f.uploadedMetadata = metadata
	for fieldName, files := range r.MultipartForm.File {
		if fieldName == "metadata" || len(files) == 0 {
			continue
		}
		file, err := files[0].Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(file)
		file.Close()
		f.uploadedScript = data
	}
	f.mu.Unlock()

	writeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{"id":"script"}`), nil)
}

// validateUploadWorkerMetadataShape enforces the Cloudflare Script Upload
// API's module-worker metadata shape, catching regressions back toward
// wrangler.jsonc's config-file conventions (a nested `durable_objects`
// wrapper key, or a `migrations` array keyed by `tag` instead of a single
// `new_tag` object): the API wants a flat, typed top-level `bindings` array
// and a single-step `migrations` object.
func validateUploadWorkerMetadataShape(metadata []byte) error {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &parsed); err != nil {
		return fmt.Errorf("invalid worker upload metadata JSON: %w", err)
	}

	if _, present := parsed["durable_objects"]; present {
		return errors.New(`worker upload metadata must not have a top-level "durable_objects" key`)
	}

	rawBindings, present := parsed["bindings"]
	if !present {
		return errors.New(`worker upload metadata missing top-level "bindings" array`)
	}
	var bindings []struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		ClassName string `json:"class_name"`
	}
	if err := json.Unmarshal(rawBindings, &bindings); err != nil {
		return fmt.Errorf(`worker upload metadata "bindings" is not an array: %w`, err)
	}
	hasDurableObjectBinding := false
	for _, binding := range bindings {
		if binding.Type == "durable_object_namespace" && binding.Name != "" && binding.ClassName != "" {
			hasDurableObjectBinding = true
			break
		}
	}
	if !hasDurableObjectBinding {
		return errors.New(`worker upload metadata "bindings" has no durable_object_namespace entry with a name and class_name`)
	}

	rawMigrations, present := parsed["migrations"]
	if !present {
		return errors.New(`worker upload metadata missing "migrations"`)
	}
	trimmedMigrations := strings.TrimSpace(string(rawMigrations))
	if !strings.HasPrefix(trimmedMigrations, "{") {
		return errors.New(`worker upload metadata "migrations" must be a single-step object, not an array`)
	}
	var migrations struct {
		NewTag string `json:"new_tag"`
	}
	if err := json.Unmarshal(rawMigrations, &migrations); err != nil {
		return fmt.Errorf(`worker upload metadata "migrations" is not a valid object: %w`, err)
	}
	if migrations.NewTag == "" {
		return errors.New(`worker upload metadata "migrations" is missing "new_tag"`)
	}

	return nil
}

func (f *fakeCloudflareServer) handleSetSecret(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	var payload struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.secrets[payload.Name] = payload.Text
	f.mu.Unlock()
	writeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{}`), nil)
}

func (f *fakeCloudflareServer) handleEnableRoute(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	f.mu.Lock()
	claimed := f.claimedSubdomain
	f.mu.Unlock()
	// Mirrors the real Cloudflare API: enabling a script's workers.dev route
	// fails with error code 10007 when the account has never claimed a
	// workers.dev subdomain, so the P1 fix (checking WorkersDevSubdomain
	// first) and its belt-and-braces 10007 handling both have a scenario to
	// exercise. claimed (set by a prior handleClaimSubdomain PUT, exercising
	// the interactive-claim-then-redeploy path) counts as claimed even when
	// cfg.subdomain was configured empty.
	if (f.cfg.subdomain == "" && claimed == "") || f.cfg.forceEnableRouteUnclaimed {
		writeCloudflareEnvelope(w, http.StatusBadRequest, false, nil, []CloudflareMessage{{Code: 10007, Message: "workers.dev subdomain required"}})
		return
	}
	f.mu.Lock()
	f.routeEnabled = true
	f.mu.Unlock()
	writeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`{"enabled":true}`), nil)
}

func (f *fakeCloudflareServer) handleWorkersDevSubdomain(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	f.mu.Lock()
	claimed := f.claimedSubdomain
	f.mu.Unlock()
	if claimed != "" {
		writeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(fmt.Sprintf(`{"subdomain":%q}`, claimed)), nil)
		return
	}
	if f.cfg.subdomain == "" {
		status := f.cfg.subdomainStatus
		if status == 0 {
			status = http.StatusNotFound
		}
		writeCloudflareEnvelope(w, status, false, nil, []CloudflareMessage{{Code: 10007, Message: "workers.dev subdomain not found"}})
		return
	}
	result := json.RawMessage(fmt.Sprintf(`{"subdomain":%q}`, f.cfg.subdomain))
	writeCloudflareEnvelope(w, http.StatusOK, true, result, nil)
}

// handleClaimSubdomain backs Client.ClaimWorkersDevSubdomain (PUT). Only
// f.cfg.claimSubdomain — when set — is accepted; anything else is rejected
// with Cloudflare's documented error code 10031 ("name already taken" —
// see fakeCloudflareConfig.claimSubdomain). Cloudflare's own request body
// shape ({"subdomain": "..."}) is decoded and validated, matching
// Client.ClaimWorkersDevSubdomain's payload exactly.
func (f *fakeCloudflareServer) handleClaimSubdomain(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	var payload struct {
		Subdomain string `json:"subdomain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.cfg.claimSubdomain == "" || payload.Subdomain != f.cfg.claimSubdomain {
		writeCloudflareEnvelope(w, http.StatusBadRequest, false, nil, []CloudflareMessage{{Code: 10031, Message: "subdomain already exists"}})
		return
	}
	f.mu.Lock()
	f.claimedSubdomain = payload.Subdomain
	f.mu.Unlock()
	result := json.RawMessage(fmt.Sprintf(`{"subdomain":%q}`, payload.Subdomain))
	writeCloudflareEnvelope(w, http.StatusOK, true, result, nil)
}

func writeCloudflareEnvelope(w http.ResponseWriter, status int, success bool, result json.RawMessage, errs []CloudflareMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	envelope := struct {
		Success bool                `json:"success"`
		Errors  []CloudflareMessage `json:"errors"`
		Result  json.RawMessage     `json:"result"`
	}{Success: success, Errors: errs, Result: result}
	_ = json.NewEncoder(w).Encode(envelope)
}

// newEmptyAccountsCloudflareServer is a minimal fake exercising
// ResolveAccountID's zero-accounts error path, distinct from the shared
// fakeCloudflareServer scenarios.
func newEmptyAccountsCloudflareServer(t *testing.T) *fakeCloudflareServer {
	t.Helper()
	fake := &fakeCloudflareServer{secrets: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		writeCloudflareEnvelope(w, http.StatusOK, true, json.RawMessage(`[]`), nil)
	})
	fake.Server = httptest.NewServer(mux)
	t.Cleanup(fake.Server.Close)
	return fake
}

// stubResolveHostname returns an Options.ResolveHostname stub reporting
// resolved, so Deploy's post-deploy verification never touches the network.
// Each test passes its own closure via Options rather than mutating shared
// package state, so tests stay safe to run with t.Parallel().
func stubResolveHostname(resolved bool) func(context.Context, string) bool {
	return func(context.Context, string) bool { return resolved }
}
