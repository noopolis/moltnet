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
}

// Result is what a successful Deploy learned, for the CLI to print and the
// caller to persist via SaveCredentials.
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
func Deploy(ctx context.Context, client *Client, opts Options) (Result, error) {
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
	if err := client.UploadWorkerModule(ctx, accountID, scriptName, mainModule, relay.WorkerScript, relay.WorkerMetadataJSON); err != nil {
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
		return Result{}, fmt.Errorf("set %s secret: %w", relayTokenSecretName, err)
	}

	// Check the account-level workers.dev subdomain claim before enabling the
	// route: on an account with no claimed subdomain, EnableWorkersDevRoute
	// itself fails (Cloudflare error code 10007), so checking claim status
	// first is what lets the ErrWorkersDevSubdomainUnclaimed branch (and the
	// CLI's dashboard guidance) actually trigger instead of a raw API error.
	subdomain, claimed, err := client.WorkersDevSubdomain(ctx, accountID)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workers.dev subdomain: %w", err)
	}
	if !claimed {
		return Result{}, ErrWorkersDevSubdomainUnclaimed
	}

	if err := client.EnableWorkersDevRoute(ctx, accountID, scriptName); err != nil {
		// Belt and braces: treat a 10007 from EnableWorkersDevRoute itself as
		// the same sentinel, in case the subdomain claim state changes
		// between the check above and this call.
		if isUnclaimedWorkersDevSubdomainError(err) {
			return Result{}, ErrWorkersDevSubdomainUnclaimed
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

// isUnclaimedWorkersDevSubdomainError reports whether err is Cloudflare API
// error code 10007, the code EnableWorkersDevRoute returns when the account
// has never claimed a workers.dev subdomain. WorkersDevSubdomain is checked
// before that call and normally catches the unclaimed case first; this is
// belt and braces so the same sentinel still surfaces if that ordering ever
// slips.
func isUnclaimedWorkersDevSubdomainError(err error) bool {
	var apiErr *CloudflareAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, message := range apiErr.Messages {
		if message.Code == 10007 {
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
