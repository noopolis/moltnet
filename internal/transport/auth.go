package transport

import (
	"context"
	"errors"
	"net/http"

	authn "github.com/noopolis/moltnet/internal/auth"
)

type agentTokenVerifier interface {
	AuthenticateAgentTokenContext(ctx context.Context, token string) (authn.Claims, bool, error)
}

func authorizedAny(policy *authn.Policy, scopes []authn.Scope, next http.HandlerFunc) http.HandlerFunc {
	return authorizedAnyWithVerifier(policy, nil, scopes, next)
}

func authorizedWithVerifier(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	scope authn.Scope,
	next http.HandlerFunc,
) http.HandlerFunc {
	return authorizedAnyWithVerifier(policy, verifier, []authn.Scope{scope}, next)
}

func authorizedAnyWithVerifier(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	scopes []authn.Scope,
	next http.HandlerFunc,
) http.HandlerFunc {
	if policy == nil || !policy.Enabled() {
		return next
	}

	return func(response http.ResponseWriter, request *http.Request) {
		request = requestWithAuthMode(policy, request)
		claims, err := authenticateAnyWithVerifier(policy, verifier, request, scopes)
		if err != nil {
			var authErr *authn.Error
			if errors.As(err, &authErr) {
				writeError(response, authErr.Status, authErr)
				return
			}
			writeError(response, http.StatusUnauthorized, err)
			return
		}

		next(response, request.WithContext(authn.WithClaims(request.Context(), claims)))
	}
}

func publicInOpen(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	scopes []authn.Scope,
	next http.HandlerFunc,
) http.HandlerFunc {
	if policy == nil || !policy.Enabled() {
		return next
	}
	if !policy.Open() {
		return authorizedAnyWithVerifier(policy, verifier, scopes, next)
	}

	return optionalAuthInOpen(policy, verifier, scopes, next)
}

func anonymousAllowedInOpen(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	next http.HandlerFunc,
) http.HandlerFunc {
	if policy == nil || !policy.Open() {
		return next
	}
	return optionalAuthInOpen(policy, verifier, nil, next)
}

func authorizedAttach(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	next http.HandlerFunc,
) http.HandlerFunc {
	if policy == nil || !policy.Enabled() {
		return next
	}
	if !policy.Open() {
		return authorizedWithVerifier(policy, verifier, authn.ScopeAttach, next)
	}

	return func(response http.ResponseWriter, request *http.Request) {
		request = requestWithAuthMode(policy, request)
		token, ok, err := authn.RequestToken(request)
		if err != nil {
			writeAuthError(response, err)
			return
		}
		if !ok {
			next(response, request)
			return
		}
		claims, ok, err := authenticateBearerToken(policy, verifier, request.Context(), token)
		if err != nil {
			writeAuthError(response, err)
			return
		}
		if !ok {
			writeError(response, http.StatusUnauthorized, &authn.Error{
				Status:  http.StatusUnauthorized,
				Message: "invalid token",
			})
			return
		}
		if !claims.Allows(authn.ScopeAttach) {
			writeError(response, http.StatusForbidden, &authn.Error{
				Status:  http.StatusForbidden,
				Message: "forbidden",
			})
			return
		}
		next(response, request.WithContext(authn.WithClaims(request.Context(), claims)))
	}
}

func authorizedEventStream(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	service Service,
	limiter *streamLimiter,
) http.HandlerFunc {
	fullStream := handleEventStream(service, limiter)
	if policy == nil || !policy.Enabled() {
		return fullStream
	}
	if !policy.Open() {
		return authorizedWithVerifier(policy, verifier, authn.ScopeObserve, fullStream)
	}

	publicStream := handlePublicOpenEventStream(service, limiter)
	return func(response http.ResponseWriter, request *http.Request) {
		request = requestWithAuthMode(policy, request)
		claims, ok, err := authenticateOptionalOpen(policy, verifier, request, []authn.Scope{authn.ScopeObserve})
		if err != nil {
			writeAuthError(response, err)
			return
		}
		if ok {
			fullStream(response, request.WithContext(authn.WithClaims(request.Context(), claims)))
			return
		}
		publicStream(response, request)
	}
}

func optionalAuthInOpen(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	scopes []authn.Scope,
	next http.HandlerFunc,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request = requestWithAuthMode(policy, request)
		claims, ok, err := authenticateOptionalOpen(policy, verifier, request, scopes)
		if err != nil {
			writeAuthError(response, err)
			return
		}
		if ok {
			request = request.WithContext(authn.WithClaims(request.Context(), claims))
		}
		next(response, request)
	}
}

func writeAuthError(response http.ResponseWriter, err error) {
	var authErr *authn.Error
	if errors.As(err, &authErr) {
		writeError(response, authErr.Status, authErr)
		return
	}
	writeError(response, http.StatusUnauthorized, err)
}

func authorizedConsole(policy *authn.Policy, verifier agentTokenVerifier, next http.HandlerFunc) http.HandlerFunc {
	if policy == nil || !policy.Enabled() {
		return next
	}

	return func(response http.ResponseWriter, request *http.Request) {
		if maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}

		request = requestWithAuthMode(policy, request)
		if policy.Open() {
			claims, ok, err := authenticateOptionalOpen(policy, verifier, request, []authn.Scope{authn.ScopeObserve})
			if err != nil {
				writeAuthError(response, err)
				return
			}
			if ok {
				request = request.WithContext(authn.WithClaims(request.Context(), claims))
			}
			next(response, request)
			return
		}

		claims, err := policy.AuthenticateRequest(request, authn.ScopeObserve)
		if err != nil {
			var authErr *authn.Error
			if errors.As(err, &authErr) {
				writeError(response, authErr.Status, authErr)
				return
			}
			writeError(response, http.StatusUnauthorized, err)
			return
		}

		next(response, request.WithContext(authn.WithClaims(request.Context(), claims)))
	}
}

func maybeSetConsoleAuthCookie(policy *authn.Policy, response http.ResponseWriter, request *http.Request) bool {
	if policy == nil || !policy.Enabled() {
		return false
	}

	token := request.URL.Query().Get("access_token")
	if token == "" {
		return false
	}

	updated := *request.URL
	query := updated.Query()
	query.Del("access_token")
	updated.RawQuery = query.Encode()

	http.SetCookie(response, &http.Cookie{
		Name:     authn.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   policy.IsSecureRequest(request),
	})
	http.Redirect(response, request, updated.String(), http.StatusTemporaryRedirect)
	return true
}

func authenticateAny(policy *authn.Policy, request *http.Request, scopes []authn.Scope) (authn.Claims, error) {
	return authenticateAnyWithVerifier(policy, nil, request, scopes)
}

func authenticateAnyWithVerifier(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	request *http.Request,
	scopes []authn.Scope,
) (authn.Claims, error) {
	token, ok, err := authn.RequestToken(request)
	if err != nil {
		return authn.Claims{}, err
	}
	if !ok {
		return authn.Claims{}, &authn.Error{
			Status:  http.StatusUnauthorized,
			Message: "authorization required",
		}
	}

	claims, ok, err := authenticateBearerToken(policy, verifier, request.Context(), token)
	if err != nil {
		return authn.Claims{}, err
	}
	if !ok {
		return authn.Claims{}, &authn.Error{
			Status:  http.StatusUnauthorized,
			Message: "invalid token",
		}
	}
	if !claims.AllowsAny(scopes) {
		return authn.Claims{}, &authn.Error{
			Status:  http.StatusForbidden,
			Message: "forbidden",
		}
	}
	return claims, nil
}

func authenticateOptionalOpen(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	request *http.Request,
	scopes []authn.Scope,
) (authn.Claims, bool, error) {
	token, ok, err := authn.RequestToken(request)
	if err != nil || !ok {
		return authn.Claims{}, false, err
	}

	claims, ok, err := authenticateBearerToken(policy, verifier, request.Context(), token)
	if err != nil {
		return authn.Claims{}, false, err
	}
	if !ok {
		return authn.Claims{}, false, &authn.Error{
			Status:  http.StatusUnauthorized,
			Message: "invalid token",
		}
	}
	if len(scopes) > 0 && !claims.AllowsAny(scopes) {
		return authn.Claims{}, false, nil
	}
	return claims, true, nil
}

func authenticateBearerToken(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	ctx context.Context,
	token string,
) (authn.Claims, bool, error) {
	if claims, ok := policy.StaticClaimsForToken(token); ok {
		return claims, true, nil
	}
	if policy.Open() && verifier != nil {
		return verifier.AuthenticateAgentTokenContext(ctx, token)
	}
	return authn.Claims{}, false, nil
}

func requestWithAuthMode(policy *authn.Policy, request *http.Request) *http.Request {
	if policy == nil {
		return request
	}
	return request.WithContext(authn.WithMode(request.Context(), policy.Mode()))
}
