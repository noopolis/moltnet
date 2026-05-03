package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

const AgentTokenPrefix = "magt_v1_"

func GenerateAgentToken() (string, error) {
	var randomBytes [32]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", err
	}
	return AgentTokenPrefix + base64.RawURLEncoding.EncodeToString(randomBytes[:]), nil
}

func AgentTokenCredentialKey(token string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return "agent-token:" + hex.EncodeToString(hash[:])
}

func LooksLikeAgentToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), AgentTokenPrefix)
}

func RequestToken(request *http.Request) (string, bool, error) {
	if request == nil {
		return "", false, nil
	}
	if header := strings.TrimSpace(request.Header.Get("Authorization")); header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			return "", false, &Error{Status: http.StatusUnauthorized, Message: "invalid authorization header"}
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if token == "" {
			return "", false, &Error{Status: http.StatusUnauthorized, Message: "authorization token is required"}
		}
		return token, true, nil
	}
	if cookie, err := request.Cookie(CookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value), true, nil
	}
	if allowsQueryAccessToken(request) {
		token := strings.TrimSpace(request.URL.Query().Get("access_token"))
		if token != "" {
			return token, true, nil
		}
	}
	return "", false, nil
}
