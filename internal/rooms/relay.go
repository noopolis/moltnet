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

	if !hasScopedParticipant(target.ParticipantIDs) {
		return target
	}

	participants := make([]string, 0, len(target.ParticipantIDs))
	for _, participantID := range target.ParticipantIDs {
		if protocol.ActorMatches(from.NetworkID, from.ID, participantID) {
			participants = append(participants, protocol.ScopedAgentID(from.NetworkID, from.ID))
			continue
		}

		participants = append(participants, normalizeParticipantID(participantID))
	}
	target.ParticipantIDs = protocol.UniqueTrimmedStrings(participants)

	return target
}

func (s *Service) relayMessage(message protocol.Message) {
	if !s.shouldRelayMessage(message) {
		return
	}

	for _, pairing := range s.snapshotRelayPairings() {
		if !s.shouldRelayToPairing(pairing, message) {
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

			if _, err := s.pairingClient.RelayMessage(ctx, pairing, request); err != nil {
				observability.DefaultMetrics.RecordRelay("error")
				s.setPairingStatus(pairing.ID, "error")
				observability.Logger(ctx, "rooms.relay", "message_id", messageID, "pairing_id", pairing.ID).
					Error("relay message failed", "error", err)
				return
			}
			observability.DefaultMetrics.RecordRelay("success")
			s.setPairingStatus(pairing.ID, "connected")
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
	if strings.TrimSpace(pairing.RemoteBaseURL) == "" {
		return false
	}
	if !s.pairingRelayReady(pairing) {
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

func (s *Service) currentPairingStatus(pairing protocol.Pairing) pairingStatus {
	if id := strings.TrimSpace(pairing.ID); id != "" {
		s.pairingsMu.RLock()
		status := s.pairingStatuses[id]
		s.pairingsMu.RUnlock()
		if strings.TrimSpace(status.value) != "" {
			return status
		}
	}
	return pairingStatus{
		value: strings.TrimSpace(pairing.Status),
	}
}

func (s *Service) pairingRelayReady(pairing protocol.Pairing) bool {
	status := s.currentPairingStatus(pairing)
	value := strings.TrimSpace(status.value)
	if value == "" || value == "connected" {
		return true
	}
	if value != "error" {
		return false
	}
	if status.updatedAt.IsZero() {
		return true
	}
	return s.now().UTC().Sub(status.updatedAt) >= pairingRelayErrorRetryAfter
}

func (s *Service) relayRequest(pairing protocol.Pairing, message protocol.Message) (protocol.SendMessageRequest, bool) {
	target := message.Target
	if target.Kind == protocol.TargetKindDM {
		target.ParticipantIDs = relayParticipantIDs(pairing, message)
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

func relayParticipantIDs(pairing protocol.Pairing, message protocol.Message) []string {
	participants := make([]string, 0, len(message.Target.ParticipantIDs))
	for _, participantID := range message.Target.ParticipantIDs {
		normalized := normalizeParticipantID(participantID)
		if normalized != "" {
			participants = append(participants, normalized)
			continue
		}

		if protocol.ActorMatches(message.From.NetworkID, message.From.ID, participantID) {
			participants = append(participants, protocol.ScopedAgentID(message.From.NetworkID, message.From.ID))
			continue
		}

		if strings.TrimSpace(pairing.RemoteNetworkID) != "" && strings.TrimSpace(participantID) != "" {
			participants = append(participants, protocol.ScopedAgentID(pairing.RemoteNetworkID, participantID))
		}
	}

	return protocol.UniqueTrimmedStrings(participants)
}

func hasScopedParticipant(participants []string) bool {
	for _, participantID := range participants {
		if normalizeParticipantID(participantID) != "" {
			return true
		}
	}

	return false
}

func normalizeParticipantID(value string) string {
	if networkID, agentID, ok := protocol.ParseScopedAgentID(value); ok {
		return protocol.ScopedAgentID(networkID, agentID)
	}
	if networkID, agentID, ok := protocol.ParseAgentFQID(value); ok {
		return protocol.ScopedAgentID(networkID, agentID)
	}

	return ""
}

func sanitizeIDComponent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "local"
	}

	replacer := strings.NewReplacer(" ", "_", ":", "_", "/", "_")
	return replacer.Replace(trimmed)
}
