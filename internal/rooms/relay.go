package rooms

import (
	"context"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const pairingRelayTimeout = 5 * time.Second
const pairingRelayErrorRetryAfter = 30 * time.Second

func (s *Service) normalizeOrigin(origin protocol.MessageOrigin, messageID string) protocol.MessageOrigin {
	normalized := origin
	if strings.TrimSpace(normalized.NetworkID) == "" {
		normalized.NetworkID = s.networkID
	}
	if strings.TrimSpace(normalized.MessageID) == "" {
		normalized.MessageID = messageID
	}

	return normalized
}

func (s *Service) normalizeTarget(target protocol.Target, from protocol.Actor) protocol.Target {
	if target.Kind != protocol.TargetKindDM {
		return target
	}
	target.ParticipantIDs = canonicalDMParticipantIDs(s.networkID, from, target.ParticipantIDs)
	return target
}

func canonicalDMParticipantIDs(defaultNetworkID string, from protocol.Actor, participantIDs []string) []string {
	type identity struct{ networkID, agentID string }
	identities := make([]identity, 0, len(participantIDs))
	allLocal := true
	for _, participantID := range protocol.UniqueTrimmedStrings(participantIDs) {
		var networkID, agentID string
		if protocol.ActorMatches(from.NetworkID, from.ID, participantID) {
			networkID, agentID = normalizedAgentIdentity(from.NetworkID, from.ID)
		} else {
			networkID, agentID = normalizedAgentIdentity(defaultNetworkID, participantID)
		}
		if networkID != defaultNetworkID {
			allLocal = false
		}
		identities = append(identities, identity{networkID: networkID, agentID: agentID})
	}

	participants := make([]string, 0, len(identities))
	for _, identity := range identities {
		if allLocal {
			participants = append(participants, identity.agentID)
		} else {
			participants = append(participants, protocol.ScopedAgentID(identity.networkID, identity.agentID))
		}
	}
	return protocol.SortedUniqueTrimmedStrings(participants)
}

func (s *Service) relayMessage(message protocol.Message) {
	if !s.shouldRelayMessage(message) {
		return
	}

	for _, pairing := range s.snapshotRelayPairings() {
		if !s.shouldAttemptRelayToPairing(pairing, message) {
			continue
		}

		request, ok := s.relayRequest(pairing, message)
		if !ok {
			continue
		}

		select {
		case s.relaySlots <- struct{}{}:
		default:
			observability.DefaultMetrics.RecordRelay("dropped")
			observability.Logger(context.Background(), "rooms.relay", "message_id", message.ID, "pairing_id", pairing.ID).
				Warn("relay queue full")
			continue
		}

		go func(pairing protocol.Pairing, request protocol.SendMessageRequest, messageID string) {
			defer func() { <-s.relaySlots }()

			ctx, cancel := context.WithTimeout(s.lifecycleCtx, pairingRelayTimeout)
			defer cancel()

			scope := pairingCheckRelayRoom
			if message.Target.Kind == protocol.TargetKindDM {
				scope = pairingCheckRelayDM
			}
			if _, err := s.refreshPairingDiagnosticsIfNeeded(ctx, pairing, scope); err != nil {
				observability.DefaultMetrics.RecordRelay("error")
				observability.Logger(ctx, "rooms.relay", "message_id", messageID, "pairing_id", pairing.ID).
					Error("relay compatibility refresh failed", "error", err)
				return
			}
			if !s.shouldRelayToPairing(pairing, message) {
				observability.DefaultMetrics.RecordRelay("dropped")
				return
			}

			if _, err := s.pairingClient.RelayMessage(ctx, pairing, request); err != nil {
				observability.DefaultMetrics.RecordRelay("error")
				s.setPairingError(pairing.ID, "Remote relay request failed.")
				observability.Logger(ctx, "rooms.relay", "message_id", messageID, "pairing_id", pairing.ID).
					Error("relay message failed", "error", err)
				return
			}
			observability.DefaultMetrics.RecordRelay("success")
			s.setPairingRelaySuccess(pairing.ID)
		}(pairing, request, message.ID)
	}
}

func (s *Service) shouldRelayMessage(message protocol.Message) bool {
	if len(s.snapshotRelayPairings()) == 0 || s.pairingClient == nil {
		return false
	}

	return strings.TrimSpace(message.Origin.NetworkID) == s.networkID
}

func (s *Service) shouldRelayToPairing(pairing protocol.Pairing, message protocol.Message) bool {
	if !pairingHasTransport(pairing) {
		return false
	}
	if !s.pairingRelayAllowed(pairing, message) {
		return false
	}

	if message.Target.Kind != protocol.TargetKindDM {
		return true
	}

	for _, participantID := range message.Target.ParticipantIDs {
		networkID, _, ok := protocol.ParseScopedAgentID(participantID)
		if ok && networkID == pairing.RemoteNetworkID {
			return true
		}
		networkID, _, ok = protocol.ParseAgentFQID(participantID)
		if ok && networkID == pairing.RemoteNetworkID {
			return true
		}
	}

	return false
}

func (s *Service) shouldAttemptRelayToPairing(pairing protocol.Pairing, message protocol.Message) bool {
	if !pairingHasTransport(pairing) {
		return false
	}
	if !s.pairingRelayAttemptReady(pairing, message) {
		return false
	}
	return s.pairingMatchesRelayTarget(pairing, message)
}

func pairingHasTransport(pairing protocol.Pairing) bool {
	return strings.TrimSpace(pairing.RemoteBaseURL) != "" ||
		(pairing.Relay != nil && strings.TrimSpace(pairing.Relay.URL) != "")
}

func (s *Service) currentPairingStatus(pairing protocol.Pairing) pairingStatus {
	if id := strings.TrimSpace(pairing.ID); id != "" {
		s.pairingsMu.RLock()
		status := s.pairingStatusForPairingLocked(pairing)
		s.pairingsMu.RUnlock()
		if strings.TrimSpace(status.value) != "" {
			return status
		}
	}
	return pairingStatus{
		value: normalizePairingStatus(pairing.Status),
	}
}

func (s *Service) pairingRelayAttemptReady(pairing protocol.Pairing, message protocol.Message) bool {
	status := s.currentPairingStatus(pairing)
	value := strings.TrimSpace(status.value)
	if value == protocol.PairingStatusError {
		if status.updatedAt.IsZero() {
			return true
		}
		return s.now().UTC().Sub(status.updatedAt) >= pairingRelayErrorRetryAfter
	}
	if s.pairingDiagnosticsNeedRefresh(status) {
		return true
	}
	if value == protocol.PairingStatusConnected || value == protocol.PairingStatusUnknown {
		return true
	}
	if value == protocol.PairingStatusDegraded {
		return message.Target.Kind != protocol.TargetKindDM || status.directMessages
	}
	return false
}

func (s *Service) pairingRelayAllowed(pairing protocol.Pairing, message protocol.Message) bool {
	status := s.currentPairingStatus(pairing)
	switch strings.TrimSpace(status.value) {
	case protocol.PairingStatusConnected:
		return true
	case protocol.PairingStatusDegraded:
		return message.Target.Kind != protocol.TargetKindDM || status.directMessages
	case protocol.PairingStatusUnknown:
		return false
	default:
		return false
	}
}

func (s *Service) setPairingRelaySuccess(pairingID string) {
	s.pairingsMu.RLock()
	status := s.pairingStatuses[pairingID]
	s.pairingsMu.RUnlock()
	if strings.TrimSpace(status.value) == protocol.PairingStatusDegraded {
		s.setPairingStatus(pairingID, protocol.PairingStatusDegraded)
		return
	}
	s.setPairingStatus(pairingID, protocol.PairingStatusConnected)
}

func (s *Service) pairingMatchesRelayTarget(pairing protocol.Pairing, message protocol.Message) bool {
	if message.Target.Kind != protocol.TargetKindDM {
		return true
	}

	for _, participantID := range message.Target.ParticipantIDs {
		networkID, _, ok := protocol.ParseScopedAgentID(participantID)
		if ok && networkID == pairing.RemoteNetworkID {
			return true
		}
		networkID, _, ok = protocol.ParseAgentFQID(participantID)
		if ok && networkID == pairing.RemoteNetworkID {
			return true
		}
	}

	return false
}

func (s *Service) relayRequest(_ protocol.Pairing, message protocol.Message) (protocol.SendMessageRequest, bool) {
	target := message.Target
	if target.Kind == protocol.TargetKindDM {
		target.ParticipantIDs = canonicalDMParticipantIDs(message.NetworkID, message.From, target.ParticipantIDs)
		if len(target.ParticipantIDs) < 2 {
			return protocol.SendMessageRequest{}, false
		}
	}

	return protocol.SendMessageRequest{
		ID:       message.ID,
		Origin:   message.Origin,
		Target:   target,
		From:     message.From,
		Parts:    append([]protocol.Part(nil), message.Parts...),
		Mentions: append([]string(nil), message.Mentions...),
	}, true
}

func sanitizeIDComponent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "local"
	}

	replacer := strings.NewReplacer(" ", "_", ":", "_", "/", "_")
	return replacer.Replace(trimmed)
}
