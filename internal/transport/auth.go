package transport

import (
	"errors"
	"net/http"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func authorized(policy *authn.Policy, scope authn.Scope, next http.HandlerFunc) http.HandlerFunc {
	return authorizedAny(policy, []authn.Scope{scope}, next)
}

func authorizedAny(policy *authn.Policy, scopes []authn.Scope, next http.HandlerFunc) http.HandlerFunc {
	if policy == nil || !policy.Enabled() {
		return next
	}

	return func(response http.ResponseWriter, request *http.Request) {
		claims, err := authenticateAny(policy, request, scopes)
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

func authorizedConsole(policy *authn.Policy, next http.HandlerFunc) http.HandlerFunc {
	if policy == nil || !policy.Enabled() {
		return next
	}

	return func(response http.ResponseWriter, request *http.Request) {
		if maybeSetConsoleAuthCookie(policy, response, request) {
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
	var lastErr error
	for _, scope := range scopes {
		claims, err := policy.AuthenticateRequest(request, scope)
		if err == nil {
			return claims, nil
		}
		lastErr = err
		var authErr *authn.Error
		if errors.As(err, &authErr) && authErr.Status == http.StatusUnauthorized {
			return authn.Claims{}, err
		}
	}
	if lastErr != nil {
		return authn.Claims{}, lastErr
	}
	return authn.Claims{}, &authn.Error{
		Status:  http.StatusForbidden,
		Message: "forbidden",
	}
}
