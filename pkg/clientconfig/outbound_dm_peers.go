package clientconfig

import (
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	MaxOutboundDMPeers     = 256
	outboundDMPSentinelDup = "duplicate_peer_id"
)

// MaxOutboundDMPeers is a small, finite, independently owned bound for locally declared
// outbound-DM peers. It is separate from protocol request-participant bounds to keep
// client-config carrier policy decoupled from protocol request payload capacity.

func validateOutboundDMConfigPeers(name string, peers []string, self string) error {
	if len(peers) == 0 {
		return nil
	}
	if len(peers) > MaxOutboundDMPeers {
		return fmt.Errorf("%s must contain %d IDs or fewer", name, MaxOutboundDMPeers)
	}

	seen := make(map[string]struct{}, len(peers))
	for index, peer := range peers {
		trimmed := strings.TrimSpace(peer)
		if peer != trimmed {
			return fmt.Errorf("%s[%d] must not contain surrounding whitespace", name, index)
		}
		if trimmed == "" {
			return fmt.Errorf("%s[%d] is required", name, index)
		}
		if _, _, ok := protocol.ParseScopedAgentID(trimmed); ok {
			return fmt.Errorf("%s[%d] must be an unqualified local member ID", name, index)
		}
		if _, _, ok := protocol.ParseAgentFQID(trimmed); ok {
			return fmt.Errorf("%s[%d] must be an unqualified local member ID", name, index)
		}
		if err := protocol.ValidateMemberID(trimmed); err != nil {
			return fmt.Errorf("%s[%d] %w", name, index, err)
		}
		if self != "" && trimmed == self {
			return fmt.Errorf("%s[%d] cannot reference self", name, index)
		}
		if _, ok := seen[trimmed]; ok {
			return fmt.Errorf("%s[%d] duplicates %q", name, index, outboundDMPSentinelDup)
		}
		seen[trimmed] = struct{}{}
	}

	return nil
}

func (a AttachmentConfig) OutboundDMPeers() []string {
	return copyStrings(a.OutboundDMPEers)
}

func (a AttachmentConfig) clone() AttachmentConfig {
	a.OutboundDMPEers = copyStrings(a.OutboundDMPEers)
	return a
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
