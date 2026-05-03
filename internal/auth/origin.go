package auth

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

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
