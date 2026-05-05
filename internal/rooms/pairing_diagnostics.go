package rooms

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const pairingDiagnosticsTTL = 5 * time.Minute

const (
	pairingDiagnosticNetworkMismatch      = "network_id_mismatch"
	pairingDiagnosticUnsupportedHTTP      = "unsupported_http_protocol"
	pairingDiagnosticUnsupportedPair      = "unsupported_pair_protocol"
	pairingDiagnosticMissingCursor        = "missing_cursor_pagination"
	pairingDiagnosticDirectMessagesOff    = "direct_messages_disabled"
	pairingDiagnosticRemoteRequestFailure = "remote_request_failed"
)

type pairingCheckScope string

const (
	pairingCheckNetwork   pairingCheckScope = "network"
	pairingCheckDiscovery pairingCheckScope = "discovery"
	pairingCheckRelayRoom pairingCheckScope = "relay_room"
	pairingCheckRelayDM   pairingCheckScope = "relay_dm"
)

func initialPairingStatus(pairing protocol.Pairing, checkedAt time.Time) pairingStatus {
	status := pairingStatus{
		value:            normalizePairingStatus(pairing.Status),
		updatedAt:        checkedAt,
		diagnostics:      clonePairingDiagnostics(pairing.Diagnostics),
		directMessages:   true,
		cursorPagination: true,
	}
	if status.diagnostics != nil {
		status.checked = true
		status.directMessages = status.diagnostics.Reason != pairingDiagnosticDirectMessagesOff
		status.cursorPagination = status.diagnostics.Reason != pairingDiagnosticMissingCursor
	}
	return status
}

func normalizePairingStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return protocol.PairingStatusUnknown
	}
	return trimmed
}

func (s *Service) refreshPairingDiagnostics(
	ctx context.Context,
	pairing protocol.Pairing,
	scope pairingCheckScope,
) (protocol.Network, pairingStatus, error) {
	network, err := s.pairingClient.FetchNetwork(ctx, pairing)
	if err != nil {
		status := pairingStatus{
			value: protocol.PairingStatusError,
			diagnostics: &protocol.PairingDiagnostics{
				CheckedAt: s.now().UTC(),
				Reason:    pairingDiagnosticRemoteRequestFailure,
				Message:   "Remote network metadata could not be fetched.",
			},
			checked: true,
		}
		s.setPairingRuntime(pairing.ID, status, true)
		return protocol.Network{}, status, err
	}

	status := s.evaluatePairingCompatibility(pairing, network, scope)
	status = s.preserveStrongerPairingDiagnostics(pairing, status, scope)
	s.setPairingRuntime(pairing.ID, status, true)
	return network, status, nil
}

func (s *Service) refreshPairingDiagnosticsIfNeeded(
	ctx context.Context,
	pairing protocol.Pairing,
	scope pairingCheckScope,
) (pairingStatus, error) {
	status := s.currentPairingStatus(pairing)
	if !s.pairingDiagnosticsNeedRefresh(status) {
		return status, nil
	}
	_, refreshed, err := s.refreshPairingDiagnostics(ctx, pairing, scope)
	if err != nil {
		return refreshed, err
	}
	return refreshed, nil
}

func (s *Service) evaluatePairingCompatibility(
	pairing protocol.Pairing,
	network protocol.Network,
	scope pairingCheckScope,
) pairingStatus {
	checkedAt := s.now().UTC()
	status := pairingStatus{
		value:            protocol.PairingStatusConnected,
		updatedAt:        checkedAt,
		directMessages:   network.Capabilities.DirectMessages,
		cursorPagination: strings.TrimSpace(network.Capabilities.MessagePagination) == "cursor",
		checked:          true,
		diagnostics: &protocol.PairingDiagnostics{
			CheckedAt:       checkedAt,
			RemoteVersion:   strings.TrimSpace(network.Version),
			RemoteNetworkID: strings.TrimSpace(network.ID),
			RemoteProtocols: cloneNetworkProtocols(network.Protocols),
		},
	}

	expectedNetworkID := strings.TrimSpace(pairing.RemoteNetworkID)
	reportedNetworkID := strings.TrimSpace(network.ID)
	switch {
	case expectedNetworkID != "" && reportedNetworkID != expectedNetworkID:
		status.value = protocol.PairingStatusIncompatible
		status.diagnostics.Reason = pairingDiagnosticNetworkMismatch
		status.diagnostics.Message = fmt.Sprintf(
			"Remote network_id %q does not match configured remote_network_id %q.",
			reportedNetworkID,
			expectedNetworkID,
		)
	case !pairingHTTPCompatible(network):
		status.value = protocol.PairingStatusIncompatible
		status.diagnostics.Reason = pairingDiagnosticUnsupportedHTTP
		status.diagnostics.Message = "Remote server does not advertise moltnet.http.v1."
	case !pairingProtocolCompatible(network):
		status.value = protocol.PairingStatusIncompatible
		status.diagnostics.Reason = pairingDiagnosticUnsupportedPair
		status.diagnostics.Message = "Remote server does not advertise moltnet.pair.v1."
	case scope == pairingCheckDiscovery && !status.cursorPagination:
		status.value = protocol.PairingStatusDegraded
		status.diagnostics.Reason = pairingDiagnosticMissingCursor
		status.diagnostics.Message = "Remote server does not advertise cursor pagination for pairing discovery."
	case !status.directMessages:
		status.value = protocol.PairingStatusDegraded
		status.diagnostics.Reason = pairingDiagnosticDirectMessagesOff
		status.diagnostics.Message = "Remote server has direct_messages disabled."
	}

	return status
}

func pairingHTTPCompatible(network protocol.Network) bool {
	if hasProtocol(network.Protocols.HTTP, protocol.HTTPProtocolV1) {
		return true
	}
	return legacyNetworkResponse(network) &&
		strings.TrimSpace(network.Capabilities.MessagePagination) == "cursor"
}

func pairingProtocolCompatible(network protocol.Network) bool {
	if hasProtocol(network.Protocols.Pair, protocol.PairProtocolV1) {
		return true
	}
	if len(network.Protocols.Pair) == 0 &&
		(hasProtocol(network.Protocols.HTTP, protocol.HTTPProtocolV1) || legacyNetworkResponse(network)) {
		return true
	}
	return false
}

func (s *Service) preserveStrongerPairingDiagnostics(
	pairing protocol.Pairing,
	next pairingStatus,
	scope pairingCheckScope,
) pairingStatus {
	if scope == pairingCheckDiscovery || strings.TrimSpace(next.value) != protocol.PairingStatusConnected {
		return next
	}

	previous := s.currentPairingStatus(pairing)
	if strings.TrimSpace(previous.value) != protocol.PairingStatusDegraded || previous.diagnostics == nil {
		return next
	}
	if previous.diagnostics.Reason != pairingDiagnosticMissingCursor {
		return next
	}
	return previous
}

func legacyNetworkResponse(network protocol.Network) bool {
	return len(network.Protocols.HTTP) == 0 &&
		len(network.Protocols.Attach) == 0 &&
		len(network.Protocols.Pair) == 0
}

func hasProtocol(protocols []string, want string) bool {
	for _, value := range protocols {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func (s *Service) pairingDiagnosticsNeedRefresh(status pairingStatus) bool {
	switch strings.TrimSpace(status.value) {
	case "", protocol.PairingStatusUnknown:
		return true
	case protocol.PairingStatusError:
		return true
	case protocol.PairingStatusConnected, protocol.PairingStatusDegraded, protocol.PairingStatusIncompatible:
	default:
		return false
	}
	if !status.checked || status.diagnostics == nil || status.updatedAt.IsZero() {
		return true
	}
	return s.now().UTC().Sub(status.updatedAt) >= pairingDiagnosticsTTL
}

func pairingDiscoveryError(status pairingStatus) error {
	if strings.TrimSpace(status.value) == protocol.PairingStatusIncompatible {
		return errors.New(status.diagnosticsMessage("paired network is incompatible"))
	}
	if !status.cursorPagination {
		return errors.New("paired network does not support cursor pagination for discovery")
	}
	return nil
}

func (p pairingStatus) diagnosticsMessage(fallback string) string {
	if p.diagnostics != nil && strings.TrimSpace(p.diagnostics.Message) != "" {
		return p.diagnostics.Message
	}
	return fallback
}

func clonePairingDiagnostics(diagnostics *protocol.PairingDiagnostics) *protocol.PairingDiagnostics {
	if diagnostics == nil {
		return nil
	}
	clone := *diagnostics
	clone.RemoteProtocols = cloneNetworkProtocols(diagnostics.RemoteProtocols)
	return &clone
}

func cloneNetworkProtocols(protocols protocol.NetworkProtocols) protocol.NetworkProtocols {
	return protocol.NetworkProtocols{
		HTTP:   append([]string(nil), protocols.HTTP...),
		Attach: append([]string(nil), protocols.Attach...),
		Pair:   append([]string(nil), protocols.Pair...),
	}
}

func pairingDiagnosticsEqual(left *protocol.PairingDiagnostics, right *protocol.PairingDiagnostics) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.CheckedAt.Equal(right.CheckedAt) &&
		left.RemoteVersion == right.RemoteVersion &&
		left.RemoteNetworkID == right.RemoteNetworkID &&
		slices.Equal(left.RemoteProtocols.HTTP, right.RemoteProtocols.HTTP) &&
		slices.Equal(left.RemoteProtocols.Attach, right.RemoteProtocols.Attach) &&
		slices.Equal(left.RemoteProtocols.Pair, right.RemoteProtocols.Pair) &&
		left.Reason == right.Reason &&
		left.Message == right.Message
}
