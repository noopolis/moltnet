package transport

import (
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func registerConsoleSessionRoutes(mux *http.ServeMux, policy *authn.Policy, verifier agentTokenVerifier, config ConsoleConfig) {
	mux.HandleFunc("GET /v1/console/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]bool{
			"local_owner_available": config.LocalOwnerToken.Reveal() != "" && localConsoleRequest(r),
		})
	})
	mux.HandleFunc("POST /v1/console/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !sameOriginConsolePost(r) {
			writeError(w, http.StatusForbidden, errors.New("console login requires a same-origin request"))
			return
		}
		var payload struct {
			Local    bool   `json:"local"`
			Token    string `json:"token"`
			Remember bool   `json:"remember"`
		}
		if err := decodeJSON(w, r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid login request"))
			return
		}
		token := strings.TrimSpace(payload.Token)
		if payload.Local {
			if !localConsoleRequest(r) || config.LocalOwnerToken.Reveal() == "" {
				writeError(w, http.StatusForbidden, errors.New("local owner login is unavailable"))
				return
			}
			token = config.LocalOwnerToken.Reveal()
		}
		claims, ok, err := authenticateBearerToken(policy, verifier, r.Context(), token)
		if err != nil || !ok || !claims.StaticToken() || !claims.Allows(authn.ScopeObserve) {
			writeError(w, http.StatusUnauthorized, errors.New("invalid console token"))
			return
		}
		cookie := &http.Cookie{Name: authn.CookieName, Value: token, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteStrictMode, Secure: policy.IsSecureRequest(r)}
		if payload.Remember {
			cookie.MaxAge = 30 * 24 * 60 * 60
			cookie.Expires = time.Now().Add(30 * 24 * time.Hour)
		}
		http.SetCookie(w, cookie)
		writeJSON(w, http.StatusOK, map[string]bool{"signed_in": true})
	})
	mux.HandleFunc("DELETE /v1/console/session", func(w http.ResponseWriter, r *http.Request) {
		if !sameOriginConsolePost(r) {
			writeError(w, http.StatusForbidden, errors.New("console logout requires a same-origin request"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.SetCookie(w, &http.Cookie{Name: authn.CookieName, Path: "/", Value: "", MaxAge: -1,
			HttpOnly: true, Secure: policy.IsSecureRequest(r), SameSite: http.SameSiteStrictMode})
		w.WriteHeader(http.StatusNoContent)
	})
}

func sameOriginConsolePost(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin, err := url.Parse(r.Header.Get("Origin"))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && origin.Scheme == scheme && origin.Host == r.Host &&
		origin.User == nil && origin.RawQuery == "" && origin.Fragment == "" && origin.Path == ""
}

func localConsoleRequest(r *http.Request) bool {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	host := r.Host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if host != "localhost" && !net.ParseIP(strings.Trim(host, "[]")).IsLoopback() {
		return false
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host {
			return false
		}
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site"
}
