package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// This file holds the client methods PLAN.md 7C.1's `moltnet status`
// composes: network identity/warnings, agents, and pairings, plus the one
// non-JSON endpoint (Metrics). This is new code added alongside api.go, not
// content moved out of it -- api.go's own methods are untouched here, so
// api.go stays well under the repo's 400-line-per-file limit (AGENTS.md) on
// its own; this file exists to keep status's growing client surface out of
// api.go in the first place, rather than to relieve an existing file that
// had grown too large.

// GetNetwork calls GET /v1/network (PLAN.md 7C.1's `moltnet status`): network
// identity, protocol/capability advertisement, and the NetworkWarning codes
// internal/rooms/service.go's networkWarningsLocked already computes —
// service.Network() bakes them into this same response, so status needs no
// separate warnings call.
func (c *Client) GetNetwork(ctx context.Context) (protocol.Network, error) {
	var network protocol.Network
	if err := c.doJSON(ctx, http.MethodGet, "/v1/network", nil, &network); err != nil {
		return protocol.Network{}, err
	}
	return network, nil
}

// ListAgents calls GET /v1/agents (PLAN.md 7C.1's "who is in it" leg).
func (c *Client) ListAgents(ctx context.Context, page protocol.PageRequest) (protocol.AgentPage, error) {
	var result protocol.AgentPage
	path := "/v1/agents" + encodePage(page)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return protocol.AgentPage{}, err
	}
	return result, nil
}

// ListPairings calls GET /v1/pairings (PLAN.md 7C.1's "what are my peers
// doing" leg). Server-side this is authorizedOperatorOrObserver-gated
// (internal/transport/auth.go): it accepts the same observe/admin scopes as
// a read, but additionally refuses a self-registered agent's own token, so a
// caller resolving with authn.ScopeObserve can still get a 403 here even
// though every other status leg succeeded — status must treat that as this
// one leg being unavailable, not the whole command failing.
func (c *Client) ListPairings(ctx context.Context, page protocol.PageRequest) (protocol.PairingPage, error) {
	var result protocol.PairingPage
	path := "/v1/pairings" + encodePage(page)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return protocol.PairingPage{}, err
	}
	return result, nil
}

// Metrics calls GET /metrics, admin-scoped server-side
// (internal/transport/http.go), and returns the raw Prometheus text
// exposition body (observability.Metrics.ServeHTTP) — the one Moltnet
// endpoint this client speaks that is not JSON, so it bypasses doJSON's
// decoder entirely via doText instead. Callers (moltnet status --verbose,
// PLAN.md 7C.1) parse the handful of metric names they care about out of the
// returned text.
func (c *Client) Metrics(ctx context.Context) (string, error) {
	return c.doText(ctx, http.MethodGet, "/metrics")
}

// doText is doJSON's sibling for the one Moltnet response body that is not
// JSON (Metrics's GET /metrics, Prometheus text exposition format): same
// request construction, auth header, and non-2xx error shape as doJSON, but
// the successful body is returned verbatim as a string instead of decoded.
func (c *Client) doText(ctx context.Context, method string, requestPath string) (string, error) {
	endpoint := strings.TrimRight(c.attachment.BaseURL, "/") + "/" + strings.TrimLeft(requestPath, "/")

	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build Moltnet request: %w", err)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request Moltnet %s %s: %w", method, request.URL.Redacted(), err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read Moltnet response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			return "", fmt.Errorf("moltnet %s %s returned %s", method, request.URL.Redacted(), response.Status)
		}
		return "", fmt.Errorf("moltnet %s %s returned %s: %s", method, request.URL.Redacted(), response.Status, trimmed)
	}

	return string(body), nil
}
