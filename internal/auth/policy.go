package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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

type Scope string

const (
	ScopeObserve Scope = "observe"
	ScopeWrite   Scope = "write"
	ScopeAdmin   Scope = "admin"
	ScopeAttach  Scope = "attach"
	ScopePair    Scope = "pair"
)

type TokenConfig struct {
	ID     string
	Value  string
	Scopes []Scope
	Agents []string
}

type Config struct {
	Mode                string
	ListenAddr          string
	AllowedOrigins      []string
	TrustForwardedProto bool
	Tokens              []TokenConfig
}

type Policy struct {
	mode                string
	allowedOrigins      map[string]struct{}
	trustForwardedProto bool
	tokens              []tokenRecord
}

type tokenRecord struct {
	hash   [32]byte
	config TokenConfig
}

type Claims struct {
	TokenID       string
	CredentialKey string
	scopes        map[Scope]struct{}
	agents        map[string]struct{}
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

	policy := &Policy{
		mode:                mode,
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
		if strings.TrimSpace(token.ID) == "" {
			token.ID = "tok_" + hex.EncodeToString(hash[:8])
		}
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

func (c Claims) Allows(scope Scope) bool {
	if len(c.scopes) == 0 {
		return false
	}

	_, ok := c.scopes[scope]
	return ok
}

func (c Claims) AllowsAny(scopes []Scope) bool {
	for _, scope := range scopes {
		if c.Allows(scope) {
			return true
		}
	}
	return false
}

func (c Claims) AllowsAgent(agentID string) bool {
	if len(c.agents) == 0 {
		return true
	}

	_, ok := c.agents[strings.TrimSpace(agentID)]
	return ok
}

func (c Claims) HasAgentRestriction() bool {
	return len(c.agents) > 0
}

func (c Claims) StaticToken() bool {
	return strings.HasPrefix(strings.TrimSpace(c.CredentialKey), "token:")
}

func (c Claims) AgentToken() bool {
	return strings.HasPrefix(strings.TrimSpace(c.CredentialKey), "agent-token:")
}

func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(Claims)
	return claims, ok
}

func WithMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, modeContextKey{}, strings.TrimSpace(mode))
}

func ModeFromContext(ctx context.Context) string {
	mode, ok := ctx.Value(modeContextKey{}).(string)
	if !ok || strings.TrimSpace(mode) == "" {
		return ModeNone
	}
	return strings.TrimSpace(mode)
}

func StaticCredentialKey(tokenID string) string {
	return "token:" + strings.TrimSpace(tokenID)
}

func NewStaticClaims(config TokenConfig) Claims {
	claims := Claims{
		TokenID: strings.TrimSpace(config.ID),
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

func NewAgentTokenClaims(agentID string, credentialKey string) Claims {
	trimmedAgentID := strings.TrimSpace(agentID)
	claims := Claims{
		CredentialKey: strings.TrimSpace(credentialKey),
		scopes: map[Scope]struct{}{
			ScopeWrite:  {},
			ScopeAttach: {},
		},
		agents: make(map[string]struct{}),
	}
	if trimmedAgentID != "" {
		claims.agents[trimmedAgentID] = struct{}{}
	}
	return claims
}

func NewCredentialClaims(credentialKey string) Claims {
	return Claims{
		CredentialKey: strings.TrimSpace(credentialKey),
	}
}

func newClaims(config TokenConfig) Claims {
	return NewStaticClaims(config)
}

func isSupportedScope(scope Scope) bool {
	switch scope {
	case ScopeObserve, ScopeWrite, ScopeAdmin, ScopeAttach, ScopePair:
		return true
	default:
		return false
	}
}
