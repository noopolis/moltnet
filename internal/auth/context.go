package auth

import (
	"context"
	"strings"
)

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

func WithPublicRead(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, publicReadContextKey{}, enabled)
}

func PublicReadFromContext(ctx context.Context) bool {
	enabled, ok := ctx.Value(publicReadContextKey{}).(bool)
	return ok && enabled
}

func WithAgentRegistration(ctx context.Context, policy string) context.Context {
	return context.WithValue(ctx, registrationContextKey{}, strings.TrimSpace(policy))
}

func AgentRegistrationFromContext(ctx context.Context) string {
	policy, ok := ctx.Value(registrationContextKey{}).(string)
	if !ok || strings.TrimSpace(policy) == "" {
		return AgentRegistrationDisabled
	}
	return strings.TrimSpace(policy)
}
