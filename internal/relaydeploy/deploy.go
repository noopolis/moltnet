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
// dashboard step before a *.workers.dev URL exists to deploy to, or supply
// Options.Subdomain to claim it non-interactively.
var ErrWorkersDevSubdomainUnclaimed = errors.New("workers.dev subdomain not yet claimed for this Cloudflare account")

// ErrWorkersDevSubdomainConflict indicates opts.Subdomain named a
// workers.dev subdomain that does not match the name this Cloudflare
// account has already claimed. Claiming a workers.dev subdomain is
// permanent and limited to exactly one per account, ever (see
// Client.ClaimWorkersDevSubdomain's doc comment on Cloudflare error code
// 10036), so a request naming a different one can never be honored: Deploy
// refuses before any remote mutation rather than silently adopting the
// account's existing claim (ignoring what the caller actually asked for) or
// attempting a claim Cloudflare will only reject.
var ErrWorkersDevSubdomainConflict = errors.New("requested workers.dev subdomain conflicts with an existing claim for this account")

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
	// Subdomain, when non-empty, is a workers.dev subdomain name to claim
	// for this Cloudflare account non-interactively (the CLI's --subdomain
	// flag) instead of erroring with ErrWorkersDevSubdomainUnclaimed. It is
	// resolved and validated before any remote mutation — see deploy's own
	// doc comment for the adopt/claim/conflict/fail decision table. Ignored
	// (no claim attempted) when the account already has a claim matching
	// this name; ErrWorkersDevSubdomainConflict when it has a different
	// one. Passing this is the caller's explicit confirmation that claiming
	// a workers.dev subdomain is permanent for the account — see
	// NormalizeWorkersDevSubdomainName / ValidateWorkersDevSubdomainName for
	// the rules it is checked against.
	Subdomain string
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
	ScriptName string
	AccountID  string
	Token      string
	// Subdomain is the account's workers.dev subdomain name (e.g. "acme" in
	// "acme.workers.dev") that this deploy resolved — whether it was
	// already claimed, adopted from an existing claim matching
	// opts.Subdomain, or freshly claimed via opts.Subdomain. Left empty on
	// both ErrWorkersDevSubdomainUnclaimed (there is, by definition, none
	// yet) and ErrWorkersDevSubdomainConflict (deploy wraps that error as
	// Result{AccountID: accountID}, deliberately not re-deriving Subdomain
	// from resolveWorkersDevSubdomainClaim's own conflict-path locals) --
	// the conflict error message itself already names both the requested
	// and the account's actual subdomain, so no caller has needed this
	// field for that case.
	//
	// Claimed is true only when this exact Deploy call is the one that
	// performed a fresh, brand-new, permanent account-level workers.dev
	// claim (opts.Subdomain set, account previously unclaimed) -- never on
	// the already-claimed/adopted paths, where Subdomain is populated but
	// nothing was newly committed. A first-ever permanent claim is
	// otherwise indistinguishable from adopting an existing one to a
	// caller, with no acknowledgement at all; see the CLI's own use of this
	// field (cmd/moltnet/relay_deploy.go) for the receipt it prints.
	Claimed          bool
	Subdomain        string
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
//
// Claim state is resolved and validated before any remote mutation
// (UploadWorkerModule, SetSecret, EnableWorkersDevRoute) — the fix for a bug
// where those three ran first and only then checked the workers.dev
// subdomain claim, so a deploy that failed on an unclaimed subdomain had
// already uploaded a Worker and set a secret it could not roll back.
// resolveWorkersDevSubdomainClaim implements the decision table:
//
//   - Already claimed, opts.Subdomain unset or matching → adopt, proceed.
//   - Already claimed, opts.Subdomain names a different one →
//     ErrWorkersDevSubdomainConflict, before any mutation.
//   - Not claimed, opts.Subdomain set → validate and claim it now, before
//     any mutation.
//   - Not claimed, opts.Subdomain unset → ErrWorkersDevSubdomainUnclaimed,
//     before any mutation (the caller's interactive claim prompt, or
//     dashboard guidance, is what used to run only after the partial
//     upload/secret work above).
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

	subdomain, claimedNow, err := resolveWorkersDevSubdomainClaim(ctx, client, accountID, opts.Subdomain)
	if err != nil {
		return Result{AccountID: accountID}, err
	}

	mainModule := workerMainModule(relay.WorkerMetadataJSON)

	uploadMetadata, err := prepareUploadMetadata(ctx, client, accountID, scriptName, relay.WorkerMetadataJSON)
	if err != nil {
		return Result{}, fmt.Errorf("resolve relay worker migration state: %w", err)
	}

	if err := client.UploadWorkerModule(ctx, accountID, scriptName, mainModule, relay.WorkerScript, uploadMetadata); err != nil {
		// Belt and braces: on a minimal-scope token, Cloudflare can reject
		// the upload itself with error code 10063 ("You need a workers.dev
		// subdomain in order to proceed") even though
		// resolveWorkersDevSubdomainClaim above just reported the account as
		// claimed — a race between that check and this call. Field-observed;
		// see isUnclaimedWorkersDevSubdomainError's doc comment.
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
		Claimed:          claimedNow,
		Subdomain:        subdomain,
		Hostname:         hostname,
		URL:              "wss://" + hostname,
		HostnameResolved: resolve(ctx, hostname),
	}, nil
}

// cloudflareAccountAlreadyHasSubdomainCode is Cloudflare's documented error
// code for PUT .../workers/subdomain when this account has already claimed
// a subdomain (10036, HTTP 409) — see Client.ClaimWorkersDevSubdomain's doc
// comment. resolveWorkersDevSubdomainClaim's own claim attempt treats it as
// a race to recover from (re-read and adopt), not a failure, mirroring
// cmd/moltnet's interactive claim prompt
// (attemptInteractiveWorkersDevSubdomainClaim).
const cloudflareAccountAlreadyHasSubdomainCode = 10036

// cloudflareSubdomainNameTakenCode is Cloudflare's documented error code for
// PUT .../workers/subdomain when the requested name itself is already taken
// by a different account (10031, distinct from 10036's "this account
// already has one" case above). Mirrors cmd/moltnet's own
// cloudflareErrorSubdomainNameTaken constant (relay_deploy_subdomain_claim.go)
// for the same reason cloudflareErrorHasCode itself is duplicated rather
// than shared: the two packages do not import each other's unexported
// symbols.
const cloudflareSubdomainNameTakenCode = 10031

// cloudflareErrorHasCode reports whether err is a *CloudflareAPIError
// carrying code among its messages. A Cloudflare error envelope can carry
// more than one message, so every one of them is checked rather than
// assuming the code of interest is first — mirroring cmd/moltnet's own
// cloudflareAPIErrorHasCode helper for the same reason (a separate copy: the
// two packages do not import each other's unexported helpers).
func cloudflareErrorHasCode(err error, code int) bool {
	var apiErr *CloudflareAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, message := range apiErr.Messages {
		if message.Code == code {
			return true
		}
	}
	return false
}

// resolveWorkersDevSubdomainClaim resolves and validates this account's
// workers.dev subdomain claim state — deploy's own doc comment has the
// decision table this implements. requested is opts.Subdomain, normalized
// and validated here (callers do not need to pre-normalize); empty means no
// non-interactive claim was requested. Returns the account's resulting
// subdomain name, and whether this call is the one that just performed a
// brand-new, permanent claim (false on every already-claimed/adopted path,
// including the 10036 race-adoption branch below — nothing new was
// committed there, an earlier caller's claim already had been).
//
// requested's shape is validated before the WorkersDevSubdomain GET, not
// just before the claim PUT further down: a malformed name must be reported
// as invalid regardless of whether the account already has a claim, rather
// than as a conflict against the account's real one on the claimed branch
// (which never otherwise looks at requested's shape at all).
func resolveWorkersDevSubdomainClaim(ctx context.Context, client *Client, accountID, requested string) (subdomain string, claimedNow bool, err error) {
	requested = NormalizeWorkersDevSubdomainName(strings.TrimSpace(requested))
	if requested != "" {
		if err := ValidateWorkersDevSubdomainName(requested); err != nil {
			return "", false, fmt.Errorf("invalid workers.dev subdomain name %q: %w", requested, err)
		}
	}

	existingSubdomain, claimed, err := client.WorkersDevSubdomain(ctx, accountID)
	if err != nil {
		return "", false, fmt.Errorf("resolve workers.dev subdomain: %w", err)
	}

	if claimed {
		if requested != "" && requested != existingSubdomain {
			return "", false, fmt.Errorf("%w: requested %q, this account already has %q", ErrWorkersDevSubdomainConflict, requested, existingSubdomain)
		}
		return existingSubdomain, false, nil
	}

	if requested == "" {
		return "", false, ErrWorkersDevSubdomainUnclaimed
	}

	if err := client.ClaimWorkersDevSubdomain(ctx, accountID, requested); err != nil {
		if cloudflareErrorHasCode(err, cloudflareAccountAlreadyHasSubdomainCode) {
			// Race: the account was claimed (by this same call racing another
			// deploy, or by hand in the dashboard) between the GET above and
			// this PUT. Re-read and adopt rather than treat this as a
			// failure -- nothing new was claimed by this call either way.
			existing, ok, readErr := client.WorkersDevSubdomain(ctx, accountID)
			if readErr != nil || !ok {
				return "", false, fmt.Errorf("claim workers.dev subdomain %q: account already has one, but Cloudflare has not reported it back yet — retry shortly: %w", requested, err)
			}
			if requested != existing {
				return "", false, fmt.Errorf("%w: requested %q, this account already has %q", ErrWorkersDevSubdomainConflict, requested, existing)
			}
			return existing, false, nil
		}
		if cloudflareErrorHasCode(err, cloudflareSubdomainNameTakenCode) {
			// Mirrors cmd/moltnet's own interactive claim prompt
			// (relay_deploy_subdomain_claim.go), which translates this exact
			// code to the same friendly wording rather than a raw Cloudflare
			// error message -- the non-interactive --subdomain path deserves
			// the same clarity, not just the flag's own existence.
			return "", false, fmt.Errorf("claim workers.dev subdomain %q: that name is taken, try another: %w", requested, err)
		}
		return "", false, fmt.Errorf("claim workers.dev subdomain %q: %w", requested, err)
	}

	return requested, true, nil
}

// isUnclaimedWorkersDevSubdomainError reports whether err is a Cloudflare
// API error carrying code 10007 ("You do not have a workers.dev
// subdomain.", the code EnableWorkersDevRoute returns) or code 10063 ("You
// need a workers.dev subdomain in order to proceed.", observed in the field
// from the worker-upload step on a minimal-scope token racing
// resolveWorkersDevSubdomainClaim's own check above). Cloudflare does not
// document which of its steps can return which of these two codes for an
// unclaimed account, so every step that touches a script on an account with
// no claimed subdomain (upload, secret, route) checks for both, rather than
// assuming only one code is possible at each call site.
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
