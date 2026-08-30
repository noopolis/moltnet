package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// This file holds Claims (the resolved shape of an authenticated
// credential) and its constructors, split out of policy.go (F4, review
// round 2) to keep policy.go under the repo's 400-line limit. Policy itself
// -- token storage, lookup, and request-level authentication -- stays in
// policy.go; no behavior changed in the split.

// Claims is the resolved shape of an authenticated credential: which scopes
// it carries, which agent ids (if any) it is restricted to, which remote
// network (if any) it is bound to, and the credential key identifying it
// for logging and comparison. Every gate in this codebase that decides
// "can this caller do X" reads a Claims, never the raw token — see
// authn.ClaimsFromContext (context.go) for how one lands on a request
// context.
type Claims struct {
	// TokenID is the auth.tokens[] id this credential was minted from
	// (NewStaticClaims) or, for a pair-scoped credential written by `pair
	// invite`/`pair <code>`, the pairing id it is minted identical to (see
	// rooms.pairingForPairScopedContext's doc comment,
	// internal/rooms/federation_access.go, for why that identity binding
	// matters).
	TokenID string
	// CredentialKey identifies this credential's shape and origin for
	// logging and equality checks: StaticToken/AgentToken below classify it
	// by prefix, and StaticCredentialKey/TokenConfigCredentialKey construct
	// the static-token form of it.
	CredentialKey string
	network       string
	scopes        map[Scope]struct{}
	agents        map[string]struct{}
}

// Allows reports whether these claims carry scope. Claims with no scopes at
// all (the zero value) allow nothing.
func (c Claims) Allows(scope Scope) bool {
	if len(c.scopes) == 0 {
		return false
	}

	_, ok := c.scopes[scope]
	return ok
}

// AllowsAny reports whether these claims carry at least one of scopes.
func (c Claims) AllowsAny(scopes []Scope) bool {
	for _, scope := range scopes {
		if c.Allows(scope) {
			return true
		}
	}
	return false
}

// Operator reports whether these claims describe an operator credential --
// this network's own owner -- rather than a peer/pairing credential or a
// per-agent one. An operator is defined, everywhere in this codebase, as a
// credential carrying both `admin` and `write`, regardless of which other
// scopes it also carries.
//
// The "regardless" matters: `--bearer` init used to mint (and configs in the
// wild still hold) an operator token scoped [observe, write, admin, pair].
// Every gate that inspects the `pair` scope must treat that token as an
// operator and not as a peer, or it looks for a pairing named "operator",
// finds none, and denies the owner every write and hides every room from
// them (F1). Defining "operator" by scope-counting, or by the mere absence
// of `pair`, is what broke.
//
// This predicate lives here, on Claims, because it was previously copied
// verbatim into internal/rooms/federation_access.go and
// internal/transport/federation_discovery.go and open-coded inline in three
// more gates (rooms/access_policy.go canWriteRoom, rooms/sender_identity.go,
// transport/skill_markdown.go). Five independent spellings of one
// trust-boundary rule meant loosening any single one of them still passed
// every test. There is now exactly one definition, in the package that owns
// caller-identity policy, and every gate calls it.
func (c Claims) Operator() bool {
	return c.Allows(ScopeAdmin) && c.Allows(ScopeWrite)
}

// AllowsAgent reports whether these claims may act as agentID. An
// unrestricted credential (HasAgentRestriction false) allows every agent
// id; an agent-restricted one (e.g. NewAgentTokenClaims) allows only the
// ids it was minted for.
func (c Claims) AllowsAgent(agentID string) bool {
	if len(c.agents) == 0 {
		return true
	}

	_, ok := c.agents[strings.TrimSpace(agentID)]
	return ok
}

// HasAgentRestriction reports whether these claims are scoped to a
// specific, non-empty set of agent ids rather than every agent.
func (c Claims) HasAgentRestriction() bool {
	return len(c.agents) > 0
}

// StaticToken reports whether this credential's key is the static
// auth.tokens[]-derived shape StaticCredentialKey/TokenConfigCredentialKey
// produce, as opposed to a runtime-issued agent token.
func (c Claims) StaticToken() bool {
	return strings.HasPrefix(strings.TrimSpace(c.CredentialKey), "token:")
}

// AgentToken reports whether this credential is a runtime-issued agent
// token (NewAgentTokenClaims), as opposed to a static auth.tokens[] entry.
func (c Claims) AgentToken() bool {
	return strings.HasPrefix(strings.TrimSpace(c.CredentialKey), "agent-token:")
}

// Network returns the remote network id this credential is bound to, or ""
// if it carries no confirmed binding. A pair-scoped credential's binding is
// only ever set once the peer's real remote_network_id is known -- see
// rooms.pairingForPairScopedContext's doc comment
// (internal/rooms/federation_access.go) for which pairing directions have
// one and which do not.
func (c Claims) Network() string {
	return c.network
}

// AgentIDs returns every agent id these claims are restricted to, in no
// particular order. Empty when HasAgentRestriction is false.
func (c Claims) AgentIDs() []string {
	agents := make([]string, 0, len(c.agents))
	for agentID := range c.agents {
		if strings.TrimSpace(agentID) != "" {
			agents = append(agents, agentID)
		}
	}
	return agents
}

// StaticCredentialKey builds the CredentialKey shape a static auth.tokens[]
// entry with the given id resolves to.
func StaticCredentialKey(tokenID string) string {
	return "token:" + strings.TrimSpace(tokenID)
}

// TokenConfigID returns token's configured id, or -- for a hand-written
// token with no id set -- a stable id derived from a truncated SHA-256 hash
// of its value, so every static token has a non-empty, deterministic id to
// key claims and config writebacks by.
func TokenConfigID(token TokenConfig) string {
	id := strings.TrimSpace(token.ID)
	if id != "" {
		return id
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(token.Value)))
	return "tok_" + hex.EncodeToString(hash[:8])
}

// TokenConfigCredentialKey is StaticCredentialKey(TokenConfigID(token)) --
// the CredentialKey a static claims built from token resolves to.
func TokenConfigCredentialKey(token TokenConfig) string {
	return StaticCredentialKey(TokenConfigID(token))
}

// NewStaticClaims builds the Claims for a static auth.tokens[] entry:
// TokenID and CredentialKey from the token's (possibly derived) id, and its
// configured network/scopes/agent restrictions carried straight across.
func NewStaticClaims(config TokenConfig) Claims {
	claims := Claims{
		TokenID: strings.TrimSpace(config.ID),
		network: strings.TrimSpace(config.Network),
		scopes:  make(map[Scope]struct{}),
		agents:  make(map[string]struct{}),
	}
	claims.CredentialKey = StaticCredentialKey(claims.TokenID)

	for _, scope := range config.Scopes {
		claims.scopes[scope] = struct{}{}
	}
	for _, agent := range config.Agents {
		trimmed := strings.TrimSpace(agent)
		if trimmed != "" {
			claims.agents[trimmed] = struct{}{}
		}
	}

	return claims
}

// NewAgentTokenClaims builds the Claims for a runtime-issued agent token:
// restricted to agentID (when non-empty) and scoped to exactly
// [observe, write, attach] -- never admin or pair, so an agent token can
// never create rooms or read /metrics.
func NewAgentTokenClaims(agentID string, credentialKey string) Claims {
	trimmedAgentID := strings.TrimSpace(agentID)
	claims := Claims{
		CredentialKey: strings.TrimSpace(credentialKey),
		// P2-2: observe too, not just write+attach — without it this token
		// can't read on a bearer network with public_read: false, so the
		// join guide's own next command 403s. Still agent-scoped (agents
		// below), never admin or pair: no room creation, no /metrics.
		scopes: map[Scope]struct{}{
			ScopeObserve: {},
			ScopeWrite:   {},
			ScopeAttach:  {},
		},
		agents: make(map[string]struct{}),
	}
	if trimmedAgentID != "" {
		claims.agents[trimmedAgentID] = struct{}{}
	}
	return claims
}

// NewCredentialClaims builds a bare Claims carrying only credentialKey --
// no scopes, no network, no agent restriction. Used where a caller's
// identity needs recording (e.g. for logging) but no authorization scopes
// apply.
func NewCredentialClaims(credentialKey string) Claims {
	return Claims{
		CredentialKey: strings.TrimSpace(credentialKey),
	}
}

func newClaims(config TokenConfig) Claims {
	return NewStaticClaims(config)
}
