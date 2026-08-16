package relaydeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	relay "github.com/noopolis/moltnet/relay"
)

// DefaultScriptName is the Cloudflare Worker script name used when
// `moltnet relay deploy` is run without --name.
const DefaultScriptName = "moltnet-relay"

const (
	relayTokenSecretName = "RELAY_TOKEN"
	relayTokenBits       = 256
)

// ErrWorkersDevSubdomainUnclaimed indicates the Cloudflare account has never
// claimed a workers.dev subdomain. The API cannot claim one on the caller's
// behalf (PLAN.md review finding), so the caller must do the one-time
// dashboard step before a *.workers.dev URL exists to deploy to.
var ErrWorkersDevSubdomainUnclaimed = errors.New("workers.dev subdomain not yet claimed for this Cloudflare account")

// Options configures a single Deploy call.
type Options struct {
	// ScriptName is the Cloudflare Worker script name. Empty defaults to
	// DefaultScriptName.
	ScriptName string
	// ExistingToken, when non-empty, is set as the RELAY_TOKEN secret
	// instead of generating or reusing one (the CLI --token-env path).
	ExistingToken string
	// PriorToken, used only when ExistingToken is empty, is reused instead
	// of generating a new RELAY_TOKEN, so a re-deploy stays idempotent and
	// does not rotate every pairing's shared secret out from under it.
	PriorToken string
	// ResolveHostname resolves the deployed workers.dev hostname's DNS
	// availability. Nil defaults to the package's real DNS lookup; tests
	// inject their own stub here instead of mutating shared package state,
	// which keeps parallel tests independent of each other.
	ResolveHostname func(ctx context.Context, hostname string) bool
	// StoredTokenPath, when non-empty, names the per-network stored
	// Cloudflare API token file (CloudflareTokenPath) that supplied the
	// token this deploy is using. It carries no auth behavior of its own —
	// it only lets Deploy recognize a rejected/insufficiently-scoped token
	// (CloudflareAPIError.Unauthorized()) as coming from that stored file,
	// so the returned error can name it and suggest --forget-token or the
	// CLOUDFLARE_API_TOKEN env override instead of a generic auth failure.
	// Leave empty when the token came from CLOUDFLARE_API_TOKEN or
	// --token-env.
	StoredTokenPath string
}

// Result is what a successful Deploy learned, for the CLI to print and the
// caller to persist via SaveCredentials. On an ErrWorkersDevSubdomainUnclaimed
// error specifically, Result is not fully zeroed: AccountID is still
// populated (account resolution always happens before any step that can
// return that sentinel), so a caller driving the interactive claim prompt
// (cmd/moltnet's attemptInteractiveWorkersDevSubdomainClaim) can reuse it
// instead of resolving the account a second time.
type Result struct {
	ScriptName       string
	AccountID        string
	Token            string
	Hostname         string
	URL              string
	HostnameResolved bool
}

// Deploy uploads the embedded relay Worker via client, sets its RELAY_TOKEN
// secret, enables its workers.dev route, and resolves the resulting
// wss:// URL. It is safe to call repeatedly: re-uploading an existing
// script name updates the code, and the RELAY_TOKEN secret is only
// regenerated when neither opts.ExistingToken nor opts.PriorToken is set.
//
// A rejected/insufficiently-scoped Cloudflare API token error is enriched
// with opts.StoredTokenPath guidance, when set, via wrapStoredTokenError.
// See Result's doc comment for what is still populated on an
// ErrWorkersDevSubdomainUnclaimed error specifically.
func Deploy(ctx context.Context, client *Client, opts Options) (Result, error) {
	result, err := deploy(ctx, client, opts)
	if err != nil {
		return result, wrapStoredTokenError(err, opts.StoredTokenPath)
	}
	return result, nil
}

// wrapStoredTokenError appends guidance naming storedTokenPath and
// suggesting `relay deploy --forget-token` or the CLOUDFLARE_API_TOKEN env
// override, when err is a rejected/insufficiently-scoped Cloudflare API
// token (CloudflareAPIError.Unauthorized()) and storedTokenPath is
// non-empty (the deploy was using a stored per-network token, not one from
// the environment). Any other error, or a deploy that was not using a
// stored token, passes through unchanged.
func wrapStoredTokenError(err error, storedTokenPath string) error {
	if storedTokenPath == "" {
		return err
	}
	var apiErr *CloudflareAPIError
	if !errors.As(err, &apiErr) || !apiErr.Unauthorized() {
		return err
	}
	return fmt.Errorf("%w\n\nstored Cloudflare API token %s was rejected; run `moltnet relay deploy --forget-token` and re-export CLOUDFLARE_API_TOKEN, or set CLOUDFLARE_API_TOKEN to override it", err, storedTokenPath)
}

// deploy is Deploy's unwrapped implementation.
func deploy(ctx context.Context, client *Client, opts Options) (Result, error) {
	scriptName := strings.TrimSpace(opts.ScriptName)
	if scriptName == "" {
		scriptName = DefaultScriptName
	}

	if err := client.VerifyToken(ctx); err != nil {
		return Result{}, fmt.Errorf("verify Cloudflare API token: %w", err)
	}

	accountID, err := client.ResolveAccountID(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("resolve Cloudflare account: %w", err)
	}

	mainModule := workerMainModule(relay.WorkerMetadataJSON)

	uploadMetadata, err := prepareUploadMetadata(ctx, client, accountID, scriptName, relay.WorkerMetadataJSON)
	if err != nil {
		return Result{}, fmt.Errorf("resolve relay worker migration state: %w", err)
	}

	if err := client.UploadWorkerModule(ctx, accountID, scriptName, mainModule, relay.WorkerScript, uploadMetadata); err != nil {
		// A field-observed failure mode this belt-and-braces check exists
		// for: on a minimal-scope token that never even reaches the
		// WorkersDevSubdomain check below (auth itself succeeds), Cloudflare
		// can reject the upload itself with error code 10063 ("You need a
		// workers.dev subdomain in order to proceed") when the account has
		// never claimed one — not just the EnableWorkersDevRoute step this
		// code originally only checked around.
		if isUnclaimedWorkersDevSubdomainError(err) {
			return Result{AccountID: accountID}, ErrWorkersDevSubdomainUnclaimed
		}
		return Result{}, fmt.Errorf("upload relay worker: %w", err)
	}

	token := strings.TrimSpace(opts.ExistingToken)
	if token == "" {
		token = strings.TrimSpace(opts.PriorToken)
	}
	if token == "" {
		token, err = app.GenerateRandomToken(relayTokenBits)
		if err != nil {
			return Result{}, fmt.Errorf("generate relay token: %w", err)
		}
	}

	if err := client.SetSecret(ctx, accountID, scriptName, relayTokenSecretName, token); err != nil {
		if isUnclaimedWorkersDevSubdomainError(err) {
			return Result{AccountID: accountID}, ErrWorkersDevSubdomainUnclaimed
		}
		return Result{}, fmt.Errorf("set %s secret: %w", relayTokenSecretName, err)
	}

	// Check the account-level workers.dev subdomain claim before enabling the
	// route: on an account with no claimed subdomain, EnableWorkersDevRoute
	// itself fails (Cloudflare error code 10007), so checking claim status
	// first is what lets the ErrWorkersDevSubdomainUnclaimed branch (and the
	// CLI's dashboard guidance / interactive claim prompt) actually trigger
	// instead of a raw API error. In practice the upload and secret steps
	// above can already have caught this (see their own checks), so this is
	// usually a no-op confirmation rather than the first detection.
	subdomain, claimed, err := client.WorkersDevSubdomain(ctx, accountID)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workers.dev subdomain: %w", err)
	}
	if !claimed {
		return Result{AccountID: accountID}, ErrWorkersDevSubdomainUnclaimed
	}

	if err := client.EnableWorkersDevRoute(ctx, accountID, scriptName); err != nil {
		// Belt and braces: treat 10007/10063 from EnableWorkersDevRoute
		// itself as the same sentinel, in case the subdomain claim state
		// changes between the check above and this call.
		if isUnclaimedWorkersDevSubdomainError(err) {
			return Result{AccountID: accountID}, ErrWorkersDevSubdomainUnclaimed
		}
		return Result{}, fmt.Errorf("enable workers.dev route: %w", err)
	}

	hostname := fmt.Sprintf("%s.%s.workers.dev", scriptName, subdomain)

	resolve := opts.ResolveHostname
	if resolve == nil {
		resolve = resolveHostname
	}

	return Result{
		ScriptName:       scriptName,
		AccountID:        accountID,
		Token:            token,
		Hostname:         hostname,
		URL:              "wss://" + hostname,
		HostnameResolved: resolve(ctx, hostname),
	}, nil
}

// isUnclaimedWorkersDevSubdomainError reports whether err is a Cloudflare
// API error carrying code 10007 ("You do not have a workers.dev
// subdomain.", the code EnableWorkersDevRoute returns) or code 10063 ("You
// need a workers.dev subdomain in order to proceed.", observed in the field
// from the worker-upload step on a minimal-scope token, before
// WorkersDevSubdomain or EnableWorkersDevRoute are ever reached). Cloudflare
// does not document which of its steps can return which of these two codes
// for an unclaimed account, so every step that touches a script on an
// account with no claimed subdomain (upload, secret, route) checks for
// both, rather than assuming only one code is possible at each call site.
func isUnclaimedWorkersDevSubdomainError(err error) bool {
	var apiErr *CloudflareAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, message := range apiErr.Messages {
		if message.Code == 10007 || message.Code == 10063 {
			return true
		}
	}
	return false
}

// workerMainModule extracts main_module from the embedded worker metadata,
// falling back to the conventional name if it is ever missing.
func workerMainModule(metadataJSON []byte) string {
	var parsed struct {
		MainModule string `json:"main_module"`
	}
	if err := json.Unmarshal(metadataJSON, &parsed); err != nil || strings.TrimSpace(parsed.MainModule) == "" {
		return "worker.js"
	}
	return parsed.MainModule
}
