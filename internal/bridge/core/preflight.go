package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	attachmentProtocolWebSocket = "websocket"
	messagePaginationCursor     = "cursor"
	preflightTimeout            = 15 * time.Second
)

var defaultPreflightClient = &http.Client{Timeout: preflightTimeout}

type PreflightCacheKey struct {
	BaseURL          string
	NetworkID        string
	TokenFingerprint string
}

type PreflightRequest struct {
	baseURL string
	token   string
	key     PreflightCacheKey
}

type CompatibilityError struct {
	BaseURL  string
	Network  protocol.Network
	Expected string
	Issues   []string
}

func (e *CompatibilityError) Error() string {
	if len(e.Issues) == 0 {
		return "Moltnet compatibility check failed"
	}

	return fmt.Sprintf(
		"Moltnet compatibility check failed for %s: %s",
		observability.RedactURL(e.BaseURL),
		strings.Join(e.Issues, "; "),
	)
}

func Preflight(ctx context.Context, config bridgeconfig.Config) error {
	request, err := NewPreflightRequest(config)
	if err != nil {
		return err
	}

	network, err := FetchPreflightNetwork(ctx, request)
	if err != nil {
		return err
	}

	return ValidateNetworkCompatibility(config, network)
}

func NewPreflightRequest(config bridgeconfig.Config) (PreflightRequest, error) {
	config = config.Normalized()

	token, _, err := config.Moltnet.ResolveToken()
	if err != nil {
		return PreflightRequest{}, fmt.Errorf("resolve Moltnet token for compatibility preflight: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(config.Moltnet.BaseURL), "/")
	return PreflightRequest{
		baseURL: baseURL,
		token:   strings.TrimSpace(token),
		key: PreflightCacheKey{
			BaseURL:          baseURL,
			NetworkID:        strings.TrimSpace(config.Moltnet.NetworkID),
			TokenFingerprint: tokenFingerprint(token),
		},
	}, nil
}

func (r PreflightRequest) CacheKey() PreflightCacheKey {
	return r.key
}

func FetchPreflightNetwork(ctx context.Context, request PreflightRequest) (protocol.Network, error) {
	endpoint := request.baseURL + "/v1/network"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return protocol.Network{}, fmt.Errorf("build Moltnet compatibility preflight request: %w", err)
	}
	if request.token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+request.token)
	}

	response, err := defaultPreflightClient.Do(httpRequest)
	if err != nil {
		return protocol.Network{}, fmt.Errorf(
			"request Moltnet compatibility metadata from %s: %w",
			observability.RedactURL(endpoint),
			err,
		)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return protocol.Network{}, fmt.Errorf(
			"Moltnet compatibility preflight %s returned %s: configured credentials cannot read network compatibility metadata; required scopes: observe, pair, or attach",
			observability.RedactURL(endpoint),
			response.Status,
		)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := readPreflightBody(response.Body)
		if message != "" {
			return protocol.Network{}, fmt.Errorf(
				"Moltnet compatibility preflight %s returned %s: %s",
				observability.RedactURL(endpoint),
				response.Status,
				message,
			)
		}
		return protocol.Network{}, fmt.Errorf(
			"Moltnet compatibility preflight %s returned %s",
			observability.RedactURL(endpoint),
			response.Status,
		)
	}

	var network protocol.Network
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&network); err != nil {
		return protocol.Network{}, fmt.Errorf("decode Moltnet compatibility metadata: %w", err)
	}

	return network, nil
}

func ValidateNetworkCompatibility(config bridgeconfig.Config, network protocol.Network) error {
	config = config.Normalized()

	var issues []string
	legacyProtocols := len(network.Protocols.HTTP) == 0 &&
		len(network.Protocols.Attach) == 0 &&
		len(network.Protocols.Pair) == 0
	expectedNetworkID := strings.TrimSpace(config.Moltnet.NetworkID)
	reportedNetworkID := strings.TrimSpace(network.ID)
	if reportedNetworkID != expectedNetworkID {
		issues = append(issues, fmt.Sprintf("network_id mismatch: expected %q, reported %q", expectedNetworkID, reportedNetworkID))
	}

	if issue := requiredProtocolIssue(
		"protocols.http",
		network.Protocols.HTTP,
		protocol.HTTPProtocolV1,
		network.Capabilities.MessagePagination,
		messagePaginationCursor,
		legacyProtocols,
	); issue != "" {
		issues = append(issues, issue)
	}

	if issue := requiredProtocolIssue(
		"protocols.attach",
		network.Protocols.Attach,
		protocol.AttachmentProtocolV1,
		network.Capabilities.AttachmentProtocol,
		attachmentProtocolWebSocket,
		legacyProtocols,
	); issue != "" {
		issues = append(issues, issue)
	}

	if strings.TrimSpace(network.Capabilities.AttachmentProtocol) != attachmentProtocolWebSocket {
		issues = append(
			issues,
			fmt.Sprintf(
				"required capability attachment_protocol=%s, reported %q",
				attachmentProtocolWebSocket,
				strings.TrimSpace(network.Capabilities.AttachmentProtocol),
			),
		)
	}

	if config.DMs != nil && config.DMs.Enabled && !network.Capabilities.DirectMessages {
		issues = append(issues, "required capability direct_messages=true, reported false")
	}

	if len(issues) == 0 {
		return nil
	}

	return &CompatibilityError{
		BaseURL:  config.Moltnet.BaseURL,
		Network:  network,
		Expected: expectedNetworkID,
		Issues:   issues,
	}
}

func requiredProtocolIssue(
	name string,
	reported []string,
	required string,
	legacyValue string,
	legacyRequired string,
	allowLegacy bool,
) string {
	if len(reported) > 0 {
		if containsProtocol(reported, required) {
			return ""
		}
		return fmt.Sprintf("required protocol %s in %s, reported %s", required, name, formatProtocols(reported))
	}

	if !allowLegacy {
		return fmt.Sprintf("required protocol %s in %s, reported none", required, name)
	}

	if strings.TrimSpace(legacyValue) == legacyRequired {
		return ""
	}

	return fmt.Sprintf(
		"required protocol %s in %s, reported none; legacy hint %s=%s is missing",
		required,
		name,
		capabilityNameForProtocol(name),
		legacyRequired,
	)
}

func containsProtocol(values []string, required string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == required {
			return true
		}
	}
	return false
}

func formatProtocols(values []string) string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		if value := strings.TrimSpace(value); value != "" {
			trimmed = append(trimmed, value)
		}
	}
	if len(trimmed) == 0 {
		return "none"
	}
	return strings.Join(trimmed, ",")
}

func capabilityNameForProtocol(name string) string {
	switch name {
	case "protocols.http":
		return "message_pagination"
	case "protocols.attach":
		return "attachment_protocol"
	default:
		return "capability"
	}
}

func tokenFingerprint(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func readPreflightBody(body io.Reader) string {
	message, _ := io.ReadAll(io.LimitReader(body, 1<<20))
	return strings.TrimSpace(string(message))
}
