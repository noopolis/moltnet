package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// operatorSenderID is the fixed CLI identity `moltnet send` uses in the
// zero-setup operator fallback (resolveClientOrOperator) when --member is
// not given. See send.go's runSend for why the sender Actor built for this
// path always has Type "human", not "agent": it is a human operator typing
// at this terminal, and that Type sidesteps the entire agent
// registration/collision path in internal/rooms/messaging.go's
// validateSenderIdentity outright (it branches straight to
// validateHumanSender for Type=="human"), so this fallback never registers,
// mints, or persists any new identity as a side effect of sending — it only
// ever needs a static bearer token with the write scope, which is exactly
// what selectLeastPrivilegedToken below hands it.
const operatorSenderID = "operator"

// operatorOverrideOptions carries the explicit low-level override flags
// send/read/conversations/participants expose alongside their existing
// agent-config flags (--config/--network/--member): --base-url and
// --token-env. When baseURL is non-empty it always wins outright, exactly
// like admin's --base-url (resolveAdminClient) — neither the client config
// nor the local-server fallback is ever consulted.
//
// There is deliberately no bare --token flag here, unlike admin's
// adminClientOptions: an operator bearer token passed on argv sits in shell
// history and in `ps` output for the process's lifetime. --token-env keeps
// the token out of both; an operator who only has a plain token value can
// export it into a variable first.
type operatorOverrideOptions struct {
	baseURL  string
	tokenEnv string
}

func bindOperatorOverrideFlags(flags *flag.FlagSet) *operatorOverrideOptions {
	options := &operatorOverrideOptions{}
	flags.StringVar(&options.baseURL, "base-url", "", "explicit Moltnet base URL; skips client config and the local-server fallback entirely")
	flags.StringVar(&options.tokenEnv, "token-env", "", "environment variable holding the bearer token for --base-url")
	return options
}

// operatorSenderActor builds the "From" actor `moltnet send` uses in the
// zero-setup fallback: --member if given, else operatorSenderID. Type is
// always "human" — see operatorSenderID's doc comment for why. networkID is
// carried through only for display/wire completeness; the human path never
// consults Actor.NetworkID server-side (validateSenderIdentity branches to
// validateHumanSender for Type=="human" before agentCollisionID, the only
// reader of that field, is ever called).
func operatorSenderActor(networkID string, memberID string) protocol.Actor {
	id := strings.TrimSpace(memberID)
	if id == "" {
		id = operatorSenderID
	}
	return protocol.Actor{Type: "human", ID: id, Name: id, NetworkID: strings.TrimSpace(networkID)}
}

func (o operatorOverrideOptions) attachment() clientconfig.AttachmentConfig {
	auth := clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeNone}
	if env := strings.TrimSpace(o.tokenEnv); env != "" {
		auth = clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, TokenEnv: env}
	}
	return clientconfig.AttachmentConfig{BaseURL: strings.TrimSpace(o.baseURL), Auth: auth}
}

// resolveClientOrOperator is the shared resolution send/read/conversations/
// participants funnel through: an explicit --base-url always wins outright;
// otherwise try the existing agent-config path unchanged
// (resolveClientForMember, backed by `moltnet connect`'s
// .moltnet/config.json); only when that fails with errClientConfigNotFound
// — no client config exists anywhere, not a malformed or ambiguous one —
// fall back to the local Moltnet *server* config every other command
// (admin, service, pair, console) already resolves, so an operator who has
// only ever run `init`/`service install`/`relay deploy`/`pair invite` can
// send/read without ever running `moltnet connect` first.
//
// usingFallback tells callers whether attachment carries a real agent-side
// rooms/DMs allowlist (client-config path, false) or none at all (either
// fallback path, true) — ensureTargetAllowed's callers must skip it in the
// true case, deferring entirely to the server's own write-policy/access
// checks, which are authoritative regardless.
func resolveClientOrOperator(
	configPath string,
	networkID string,
	memberID string,
	override operatorOverrideOptions,
	requiredScope authn.Scope,
) (clientconfig.AttachmentConfig, *moltnetclient.Client, bool, error) {
	if strings.TrimSpace(override.baseURL) != "" {
		attachment := override.attachment()
		client, err := moltnetclient.New(attachment)
		if err != nil {
			return clientconfig.AttachmentConfig{}, nil, false, err
		}
		return attachment, client, true, nil
	}

	_, attachment, client, err := resolveClientForMember(configPath, networkID, memberID)
	if err == nil {
		return attachment, client, false, nil
	}
	if !errors.Is(err, errClientConfigNotFound) {
		return clientconfig.AttachmentConfig{}, nil, false, err
	}

	attachment, client, fallbackErr := resolveOperatorClient(networkID, requiredScope)
	if fallbackErr != nil {
		return clientconfig.AttachmentConfig{}, nil, false, fallbackErr
	}
	return attachment, client, true, nil
}

// resolveOperatorClient is the local-server half of resolveClientOrOperator:
// it resolves the local Moltnet *server* config the same way admin commands
// do (resolveAdminFromServerConfig, app.ResolveConfigPath — --network
// doubles as the id disambiguator under ~/.moltnet/, exactly like admin's
// --network and start/pair/relay/service's --id), then applies two
// guardrails an admin-scoped fallback does not need:
//
//   - loopback only: the derived base URL (consoleBaseURL's own
//     wildcard-to-127.0.0.1 mapping) must resolve to a loopback address. A
//     server bound to a LAN or public host never gets its operator token
//     silently sent there by this fallback — the caller gets an actionable
//     error naming --base-url/--token-env instead.
//   - least privilege: the selected token is the narrowest configured token
//     that actually satisfies requiredScope (selectLeastPrivilegedToken),
//     never a broader one when a narrower one exists, and never an
//     admin-only token for an operation that does not accept admin in its
//     place (send's write requirement has no such admin bypass — see
//     tokenAllowsScope).
func resolveOperatorClient(networkID string, requiredScope authn.Scope) (clientconfig.AttachmentConfig, *moltnetclient.Client, error) {
	path, found, err := app.ResolveConfigPath("", strings.TrimSpace(networkID))
	if err != nil {
		return clientconfig.AttachmentConfig{}, nil, err
	}
	if !found {
		return clientconfig.AttachmentConfig{}, nil, fmt.Errorf("%w; run `moltnet init` first", errClientConfigNotFound)
	}

	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return clientconfig.AttachmentConfig{}, nil, err
	}

	baseURL, loopback, err := operatorBaseURL(config.ListenAddr)
	if err != nil {
		return clientconfig.AttachmentConfig{}, nil, err
	}
	if !loopback {
		return clientconfig.AttachmentConfig{}, nil, fmt.Errorf(
			"server.listen_addr %q in %s is not a loopback address; the zero-setup fallback only ever dials 127.0.0.1, so pass --base-url %s and --token-env <VAR> explicitly (VAR holding a %s-scoped token) instead",
			config.ListenAddr, path, baseURL, requiredScope,
		)
	}

	auth, ok := operatorAuthForScope(config, requiredScope)
	if !ok {
		return clientconfig.AttachmentConfig{}, nil, fmt.Errorf(
			"no %s-scoped token found in %s; run `moltnet init --bearer` on a fresh network, or add one to auth.tokens with `moltnet apply`",
			requiredScope, path,
		)
	}

	attachment := clientconfig.AttachmentConfig{
		BaseURL:   baseURL,
		NetworkID: config.NetworkID,
		Auth:      auth,
	}
	// NewLoopbackOnly, not New: this is the path whose documented invariant
	// is "never leaves the machine" (operatorBaseURL's loopback check
	// above) — that invariant must hold even if the local server itself
	// issues a redirect (moltnetclient.NewLoopbackOnly's doc comment).
	client, err := moltnetclient.NewLoopbackOnly(attachment)
	if err != nil {
		return clientconfig.AttachmentConfig{}, nil, err
	}
	return attachment, client, nil
}

// operatorAuthForScope picks the local-server auth this fallback hands to
// moltnetclient: none at all when the resolved config's auth.mode is
// neither "bearer" nor "open" (an unauthenticated server needs no token),
// or — for either mode — the least-privileged configured token that
// satisfies requiredScope. "open" mode is not "no auth needed": its
// PublicRead widening only ever covers reads (internal/transport/auth.go's
// publicInOpen/optionalAuthInOpen), never POST /v1/messages, which
// authorizes on authorizedAnyWithVerifier regardless of mode — so an open
// server still 401s a tokenless send exactly like a bearer one does. The
// only case that still falls back to no auth is an open server with zero
// configured tokens at all (bearer mode can never reach here with none:
// NewPolicy refuses to construct a bearer policy without at least one).
// ok is false only when a token requirement applies and no token satisfies
// it, so callers can fail with an exact, actionable error rather than
// silently downgrading to no auth and letting the server return a bare 401.
func operatorAuthForScope(config app.Config, requiredScope authn.Scope) (clientconfig.AuthConfig, bool) {
	if config.Auth.Mode != authn.ModeBearer && config.Auth.Mode != authn.ModeOpen {
		return clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeNone}, true
	}
	if len(config.Auth.Tokens) == 0 {
		return clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeNone}, true
	}
	token, ok := selectLeastPrivilegedToken(config.Auth.Tokens, requiredScope)
	if !ok {
		return clientconfig.AuthConfig{}, false
	}
	return clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: token.Value}, true
}

// selectLeastPrivilegedToken returns the least-privileged token in tokens
// that satisfies requiredScope (tokenAllowsScope), skipping any token with
// no resolvable plaintext value and any token restricted to specific agents
// (TokenConfig.Agents): an agent-restricted token is a specific registered
// agent's own credential, never a general operator identity, and picking
// one here could either 403 (wrong agent) or silently act as an agent the
// operator never asked to be.
//
// Privilege ranks by tokenLessPrivileged: a token that does not carry
// ScopeAdmin always outranks one that does, regardless of how many scopes
// either has — an [admin]-only token must never beat an [observe]-only
// token for a read command just because config order happened to list it
// first. Only among tokens tied on that first axis does scope count break
// the tie, and only among tokens tied on both does document order (the
// first one found) win, since the loop below only replaces best on a
// strict improvement.
func selectLeastPrivilegedToken(tokens []authn.TokenConfig, requiredScope authn.Scope) (authn.TokenConfig, bool) {
	var best authn.TokenConfig
	found := false
	for _, token := range tokens {
		if strings.TrimSpace(token.Value) == "" {
			continue
		}
		if len(token.Agents) > 0 {
			continue
		}
		if !tokenAllowsScope(token, requiredScope) {
			continue
		}
		if !found || tokenLessPrivileged(token, best) {
			best = token
			found = true
		}
	}
	return best, found
}

// tokenLessPrivileged reports whether a is strictly less privileged than b
// for selectLeastPrivilegedToken's ranking: non-admin-bearing beats
// admin-bearing first, then fewer total scopes, in that order. A token that
// ties b on both axes is not "less privileged" (returns false), so the
// document-order token already selected is kept rather than replaced.
func tokenLessPrivileged(a, b authn.TokenConfig) bool {
	aAdmin, bAdmin := tokenCarriesAdmin(a), tokenCarriesAdmin(b)
	if aAdmin != bAdmin {
		return !aAdmin
	}
	return len(a.Scopes) < len(b.Scopes)
}

// tokenCarriesAdmin reports whether token's scopes include ScopeAdmin.
func tokenCarriesAdmin(token authn.TokenConfig) bool {
	for _, scope := range token.Scopes {
		if scope == authn.ScopeAdmin {
			return true
		}
	}
	return false
}

// tokenAllowsScope reports whether token satisfies requiredScope for the
// purposes of this fallback. It is a literal scope match, with exactly one
// widening: an admin-scoped token stands in for ScopeObserve, mirroring
// internal/transport/scopes.go's readScopes ([observe, admin]) — every read
// endpoint this fallback calls (GET /v1/rooms, /v1/dms, .../messages)
// already accepts admin in place of observe server-side. There is no such
// widening for ScopeWrite: POST /v1/messages authorizes on literally
// [write, pair] (internal/transport/http.go), so an admin-only token
// without write would 403 there regardless — never select one and claim it
// satisfies a write requirement it cannot actually meet.
func tokenAllowsScope(token authn.TokenConfig, requiredScope authn.Scope) bool {
	for _, scope := range token.Scopes {
		if scope == requiredScope {
			return true
		}
	}
	if requiredScope != authn.ScopeObserve {
		return false
	}
	for _, scope := range token.Scopes {
		if scope == authn.ScopeAdmin {
			return true
		}
	}
	return false
}

// operatorBaseURL derives the origin this fallback would dial for
// listenAddr (reusing consoleBaseURL's own wildcard/host mapping, so the two
// commands never disagree about what a given server.listen_addr means) and
// reports whether that origin is loopback-safe: an explicit loopback IP or
// "localhost" host. consoleBaseURL already substitutes the loopback address
// for an empty or wildcard-bind host, so any other host (a LAN IP, a public
// hostname) survives untouched here and correctly reports loopback=false.
func operatorBaseURL(listenAddr string) (baseURL string, loopback bool, err error) {
	base, err := consoleBaseURL(listenAddr)
	if err != nil {
		return "", false, err
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", false, err
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return base, true, nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return base, true, nil
	}
	return base, false, nil
}
