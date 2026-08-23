package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

const CookieName = "moltnet_auth"

const (
	ModeNone   = "none"
	ModeBearer = "bearer"
	ModeOpen   = "open"
)

const (
	AgentRegistrationDisabled = "disabled"
	AgentRegistrationToken    = "token"
	AgentRegistrationOpen     = "open"
)

type Scope string

const (
	ScopeObserve Scope = "observe"
	ScopeWrite   Scope = "write"
	ScopeAdmin   Scope = "admin"
	ScopeAttach  Scope = "attach"
	ScopePair    Scope = "pair"
)

type TokenConfig struct {
	ID      string
	Value   string
	Network string
	Scopes  []Scope
	Agents  []string
}

type Config struct {
	Mode                string
	PublicRead          bool
	AgentRegistration   string
	ListenAddr          string
	AllowedOrigins      []string
	TrustForwardedProto bool
	// RequirePairNetworkBinding rejects remote-origin pairing messages when
	// their pair credential has no bound network (internal/rooms's
	// pairCredentialMatchesOrigin). Defaults to false (internal/app's
	// defaultConfig, config_load.go) — an inbound pair-scoped message that
	// claims a remote origin but carries a credential with no confirmed
	// network binding is accepted with a logged warning rather than refused
	// outright. F3: 7B.2 shipped this defaulting to true on the assumption
	// that the inviting side's credential gets bound "once the peer has made
	// contact" — no code does that persistence today (see config_load.go's
	// field comment), so a strict default left every `pair invite` inviter
	// permanently unable to receive from the peer it invited. Set
	// `auth.require_pair_network_binding: true` in the config file, or
	// MOLTNET_REQUIRE_PAIR_NETWORK_BINDING=true, to opt into the stricter
	// posture once you have confirmed (e.g. via `pair show`) that this
	// pairing's credential is actually bound. This is a distinct, narrower
	// gate than 7B.1's federation-list enforcement
	// (internal/rooms/federation_access.go): that one is keyed on credential
	// identity and cannot be disabled, this one is keyed on the credential's
	// bound network and is operator-configurable.
	RequirePairNetworkBinding bool
	Tokens                    []TokenConfig
}

type Policy struct {
	mode                string
	publicRead          bool
	agentRegistration   string
	allowedOrigins      map[string]struct{}
	trustForwardedProto bool
	tokens              []tokenRecord
}

type tokenRecord struct {
	hash   [32]byte
	config TokenConfig
}

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

type claimsContextKey struct{}
type modeContextKey struct{}
type publicReadContextKey struct{}
type registrationContextKey struct{}

func NewPolicy(config Config) (*Policy, error) {
	mode := strings.TrimSpace(config.Mode)
	if mode == "" {
		mode = ModeNone
	}

	switch mode {
	case ModeNone, ModeBearer, ModeOpen:
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", mode)
	}

	agentRegistration := strings.TrimSpace(config.AgentRegistration)
	if agentRegistration == "" {
		agentRegistration = AgentRegistrationDisabled
	}
	if mode == ModeOpen {
		config.PublicRead = true
		agentRegistration = AgentRegistrationOpen
	}
	switch agentRegistration {
	case AgentRegistrationDisabled, AgentRegistrationToken, AgentRegistrationOpen:
	default:
		return nil, fmt.Errorf("unsupported auth.agent_registration %q", agentRegistration)
	}

	policy := &Policy{
		mode:                mode,
		publicRead:          config.PublicRead,
		agentRegistration:   agentRegistration,
		allowedOrigins:      make(map[string]struct{}),
		trustForwardedProto: config.TrustForwardedProto,
		tokens:              make([]tokenRecord, 0, len(config.Tokens)),
	}

	for _, origin := range normalizeOrigins(config.AllowedOrigins, config.ListenAddr) {
		policy.allowedOrigins[origin] = struct{}{}
	}

	if mode == ModeNone {
		return policy, nil
	}

	if mode == ModeBearer && len(config.Tokens) == 0 {
		return nil, fmt.Errorf("bearer auth requires at least one token")
	}

	for index, token := range config.Tokens {
		value := strings.TrimSpace(token.Value)
		if value == "" {
			return nil, fmt.Errorf("auth.tokens[%d].value is required", index)
		}
		if len(token.Scopes) == 0 {
			return nil, fmt.Errorf("auth.tokens[%d].scopes is required", index)
		}
		for _, scope := range token.Scopes {
			if !isSupportedScope(scope) {
				return nil, fmt.Errorf("auth.tokens[%d].scope %q is unsupported", index, scope)
			}
		}
		hash := sha256.Sum256([]byte(value))
		token.ID = TokenConfigID(token)
		policy.tokens = append(policy.tokens, tokenRecord{
			hash:   hash,
			config: token,
		})
	}

	return policy, nil
}

func (p *Policy) Enabled() bool {
	return p != nil && p.mode != ModeNone
}

func (p *Policy) Mode() string {
	if p == nil || strings.TrimSpace(p.mode) == "" {
		return ModeNone
	}
	return p.mode
}

func (p *Policy) None() bool {
	return p == nil || p.mode == ModeNone
}

func (p *Policy) Bearer() bool {
	return p != nil && p.mode == ModeBearer
}

func (p *Policy) Open() bool {
	return p != nil && p.mode == ModeOpen
}

func (p *Policy) PublicRead() bool {
	return p != nil && p.publicRead
}

func (p *Policy) AgentRegistration() string {
	if p == nil || strings.TrimSpace(p.agentRegistration) == "" {
		return AgentRegistrationDisabled
	}
	return p.agentRegistration
}

// TrustForwardedProto reports whether the operator has told Moltnet
// (server.trust_forwarded_proto) that a reverse proxy sits in front of it.
// Once true, a loopback RemoteAddr no longer proves a request originated on
// this machine — it just means the proxy connected from here.
func (p *Policy) TrustForwardedProto() bool {
	return p != nil && p.trustForwardedProto
}

func (p *Policy) AuthenticateRequest(request *http.Request, scope Scope) (Claims, error) {
	if p == nil || p.mode == ModeNone {
		return Claims{}, nil
	}

	token, ok, err := RequestToken(request)
	if err != nil {
		return Claims{}, err
	}
	if !ok {
		return Claims{}, &Error{
			Status:  http.StatusUnauthorized,
			Message: "authorization required",
		}
	}

	return p.AuthenticateToken(token, scope)
}

func (p *Policy) AuthenticateToken(value string, scope Scope) (Claims, error) {
	claims, ok := p.StaticClaimsForToken(value)
	if !ok {
		return Claims{}, &Error{
			Status:  http.StatusUnauthorized,
			Message: "invalid token",
		}
	}

	if !claims.Allows(scope) {
		return Claims{}, &Error{
			Status:  http.StatusForbidden,
			Message: "forbidden",
		}
	}

	return claims, nil
}

func (p *Policy) StaticClaimsForToken(value string) (Claims, bool) {
	if p == nil || p.mode == ModeNone {
		return Claims{}, false
	}
	config, ok := p.lookupToken(value)
	if !ok {
		return Claims{}, false
	}
	return NewStaticClaims(config), true
}

func (p *Policy) lookupToken(value string) (TokenConfig, bool) {
	candidateHash := sha256.Sum256([]byte(strings.TrimSpace(value)))
	for _, token := range p.tokens {
		if subtle.ConstantTimeCompare(candidateHash[:], token.hash[:]) == 1 {
			return token.config, true
		}
	}
	return TokenConfig{}, false
}

func isSupportedScope(scope Scope) bool {
	switch scope {
	case ScopeObserve, ScopeWrite, ScopeAdmin, ScopeAttach, ScopePair:
		return true
	default:
		return false
	}
}
