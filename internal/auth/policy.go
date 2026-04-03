package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const CookieName = "moltnet_auth"

const (
	ModeNone   = "none"
	ModeBearer = "bearer"
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
	TokenID string
	scopes  map[Scope]struct{}
	agents  map[string]struct{}
}

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

type claimsContextKey struct{}

func NewPolicy(config Config) (*Policy, error) {
	mode := strings.TrimSpace(config.Mode)
	if mode == "" {
		mode = ModeNone
	}

	switch mode {
	case ModeNone, ModeBearer:
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

	if len(config.Tokens) == 0 {
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
	return p != nil && p.mode == ModeBearer
}

func (p *Policy) AuthenticateRequest(request *http.Request, scope Scope) (Claims, error) {
	if !p.Enabled() {
		return Claims{}, nil
	}

	token := bearerToken(request)
	if token == "" {
		return Claims{}, &Error{
			Status:  http.StatusUnauthorized,
			Message: "authorization required",
		}
	}

	config, ok := p.lookupToken(token)
	if !ok {
		return Claims{}, &Error{
			Status:  http.StatusUnauthorized,
			Message: "invalid token",
		}
	}

	claims := newClaims(config)
	if !claims.Allows(scope) {
		return Claims{}, &Error{
			Status:  http.StatusForbidden,
			Message: "forbidden",
		}
	}

	return claims, nil
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

func (p *Policy) CheckOrigin(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	normalized, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}

	_, allowed := p.allowedOrigins[normalized]
	return allowed
}

func (p *Policy) IsSecureRequest(request *http.Request) bool {
	if request != nil && request.TLS != nil {
		return true
	}
	if p == nil || !p.trustForwardedProto || request == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https")
}

func (c Claims) Allows(scope Scope) bool {
	if len(c.scopes) == 0 {
		return false
	}

	_, ok := c.scopes[scope]
	return ok
}

func (c Claims) AllowsAgent(agentID string) bool {
	if len(c.agents) == 0 {
		return true
	}

	_, ok := c.agents[strings.TrimSpace(agentID)]
	return ok
}

func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(Claims)
	return claims, ok
}

func newClaims(config TokenConfig) Claims {
	claims := Claims{
		TokenID: strings.TrimSpace(config.ID),
		scopes:  make(map[Scope]struct{}),
		agents:  make(map[string]struct{}),
	}

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

func bearerToken(request *http.Request) string {
	if header := strings.TrimSpace(request.Header.Get("Authorization")); header != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(header, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
	}
	if cookie, err := request.Cookie(CookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}
	if allowsQueryAccessToken(request) {
		return strings.TrimSpace(request.URL.Query().Get("access_token"))
	}
	return ""
}

func allowsQueryAccessToken(request *http.Request) bool {
	path := strings.TrimSpace(request.URL.Path)
	return path == "/console" || strings.HasPrefix(path, "/console/")
}

func normalizeOrigins(origins []string, listenAddr string) []string {
	var normalized []string
	for _, origin := range origins {
		if value, ok := normalizeOrigin(origin); ok {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) > 0 {
		return normalized
	}
	return defaultOrigins(listenAddr)
}

func normalizeOrigin(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false
	}
	return parsed.Scheme + "://" + parsed.Host, true
}

func defaultOrigins(listenAddr string) []string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return nil
	}

	hosts := []string{"localhost", "127.0.0.1"}
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost != "" && trimmedHost != "0.0.0.0" && trimmedHost != "::" {
		hosts = append(hosts, strings.Trim(trimmedHost, "[]"))
	}

	var origins []string
	for _, item := range hosts {
		origins = append(origins, "http://"+net.JoinHostPort(item, port))
		origins = append(origins, "https://"+net.JoinHostPort(item, port))
	}
	return origins
}

func isSupportedScope(scope Scope) bool {
	switch scope {
	case ScopeObserve, ScopeWrite, ScopeAdmin, ScopeAttach, ScopePair:
		return true
	default:
		return false
	}
}
