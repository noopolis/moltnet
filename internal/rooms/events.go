package rooms

import (
	"context"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) publishEvent(event protocol.Event) {
	s.broker.Publish(event)
}

func (s *Service) AgentConnected(ctx context.Context, agent protocol.Actor) {
	eventAgent := s.setAgentConnected(agent, true)
	s.publishAgentEvent(ctx, protocol.EventTypeAgentConnected, eventAgent)
}

func (s *Service) AgentDisconnected(ctx context.Context, agent protocol.Actor) {
	eventAgent := s.setAgentConnected(agent, false)
	s.publishAgentEvent(ctx, protocol.EventTypeAgentDisconnected, eventAgent)
}

func (s *Service) AgentWakeDelivered(ctx context.Context, agent protocol.Actor, event protocol.Event) {
	if event.Message == nil {
		return
	}
	s.publishAgentEvent(ctx, protocol.EventTypeAgentWakeDelivered, protocol.AgentEvent{
		AgentID:   strings.TrimSpace(agent.ID),
		NetworkID: s.networkID,
		FQID:      protocol.AgentFQID(s.networkID, strings.TrimSpace(agent.ID)),
		Name:      strings.TrimSpace(agent.Name),
		MessageID: event.Message.ID,
		Reason:    agentWakeReason(s.networkID, agent, event),
		Target:    &event.Message.Target,
	})
}

func (s *Service) AgentWakeFailed(ctx context.Context, agent protocol.Actor, event protocol.Event, err error) {
	if event.Message == nil {
		return
	}
	failure := protocol.AgentEvent{
		AgentID:   strings.TrimSpace(agent.ID),
		NetworkID: s.networkID,
		FQID:      protocol.AgentFQID(s.networkID, strings.TrimSpace(agent.ID)),
		Name:      strings.TrimSpace(agent.Name),
		MessageID: event.Message.ID,
		Reason:    agentWakeReason(s.networkID, agent, event),
		Target:    &event.Message.Target,
	}
	if err != nil {
		failure.Error = strings.TrimSpace(err.Error())
	}
	s.publishAgentEvent(ctx, protocol.EventTypeAgentWakeFailed, failure)
}

func (s *Service) setAgentConnected(agent protocol.Actor, connected bool) protocol.AgentEvent {
	agentID := strings.TrimSpace(agent.ID)
	s.agentPresenceMu.Lock()
	if connected {
		s.connectedAgents[agentID] = true
	} else {
		delete(s.connectedAgents, agentID)
	}
	s.agentPresenceMu.Unlock()

	return protocol.AgentEvent{
		AgentID:   agentID,
		NetworkID: s.networkID,
		FQID:      protocol.AgentFQID(s.networkID, agentID),
		Name:      strings.TrimSpace(agent.Name),
	}
}

func (s *Service) publishAgentEvent(ctx context.Context, eventType string, agent protocol.AgentEvent) {
	if strings.TrimSpace(agent.AgentID) == "" {
		return
	}
	observability.Logger(ctx, "rooms.agent", "agent_id", agent.AgentID, "event_type", eventType).
		Info("agent lifecycle event")
	s.publishEvent(protocol.Event{
		ID:        newPrefixedID("evt"),
		Type:      eventType,
		NetworkID: s.networkID,
		Agent:     &agent,
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Service) agentConnected(agentID string) bool {
	s.agentPresenceMu.RLock()
	defer s.agentPresenceMu.RUnlock()
	return s.connectedAgents[strings.TrimSpace(agentID)]
}

func agentWakeReason(networkID string, agent protocol.Actor, event protocol.Event) string {
	if event.Message == nil {
		return ""
	}
	if event.Message.Target.Kind == protocol.TargetKindDM {
		return "dm"
	}
	for _, mention := range event.Message.Mentions {
		if protocol.ActorMatches(networkID, agent.ID, mention) || mention == agent.Name {
			return "mention"
		}
	}
	return "targeted"
}

func eventIDForMessage(messageID string) string {
	return deterministicPrefixedID("evt", messageID)
}

func (s *Service) conversationLifecycle(
	ctx context.Context,
	message protocol.Message,
) (store.AppendLifecycle, error) {
	switch message.Target.Kind {
	case protocol.TargetKindThread:
		thread, ok, err := s.getThread(ctx, message.Target.ThreadID)
		if err != nil {
			return store.AppendLifecycle{}, err
		}
		if ok && thread.MessageCount == 1 {
			return store.AppendLifecycle{Thread: &thread}, nil
		}
	case protocol.TargetKindDM:
		conversation, ok, err := s.getDirectConversation(ctx, message.Target.DMID)
		if err != nil {
			return store.AppendLifecycle{}, err
		}
		if ok && conversation.MessageCount == 1 {
			return store.AppendLifecycle{DM: &conversation}, nil
		}
	}

	return store.AppendLifecycle{}, nil
}

func (s *Service) setPairingStatus(pairingID string, status string) {
	s.setPairingRuntime(pairingID, pairingStatus{value: status}, false)
}

func (s *Service) setPairingError(pairingID string, message string) {
	s.setPairingRuntime(pairingID, pairingStatus{
		value: protocol.PairingStatusError,
		diagnostics: &protocol.PairingDiagnostics{
			CheckedAt: s.now().UTC(),
			Reason:    pairingDiagnosticRemoteRequestFailure,
			Message:   strings.TrimSpace(message),
		},
		checked: true,
	}, true)
}

func (s *Service) setPairingRuntime(pairingID string, next pairingStatus, replaceDiagnostics bool) {
	next.value = normalizePairingStatus(next.value)
	updatedAt := s.now().UTC()

	s.pairingsMu.Lock()
	previous := s.pairingStatuses[pairingID]
	if !replaceDiagnostics {
		next.diagnostics = clonePairingDiagnostics(previous.diagnostics)
		next.checked = previous.checked
		next.directMessages = previous.directMessages
		next.cursorPagination = previous.cursorPagination
	} else {
		next.diagnostics = clonePairingDiagnostics(next.diagnostics)
	}
	if previous.value == next.value &&
		previous.checked == next.checked &&
		previous.directMessages == next.directMessages &&
		previous.cursorPagination == next.cursorPagination &&
		pairingDiagnosticsEqual(previous.diagnostics, next.diagnostics) {
		previous.updatedAt = updatedAt
		s.pairingStatuses[pairingID] = previous
		s.pairingsMu.Unlock()
		return
	}
	next.updatedAt = updatedAt
	s.pairingStatuses[pairingID] = next

	var eventPairing *protocol.Pairing
	for _, pairing := range s.pairings {
		if pairing.ID == pairingID {
			pairing.Token = ""
			pairing.Status = next.value
			pairing.Diagnostics = clonePairingDiagnostics(next.diagnostics)
			copyPairing := pairing
			eventPairing = &copyPairing
			break
		}
	}
	s.pairingPublishMu.Lock()
	s.pairingsMu.Unlock()
	defer s.pairingPublishMu.Unlock()

	if eventPairing == nil {
		return
	}

	observability.Logger(s.lifecycleCtx, "rooms.pairing", "pairing_id", pairingID, "status", next.value).
		Info("pairing status updated")
	s.publishEvent(protocol.Event{
		ID:        newPrefixedID("evt"),
		Type:      protocol.EventTypePairingUpdated,
		NetworkID: s.networkID,
		Pairing:   eventPairing,
		CreatedAt: time.Now().UTC(),
	})
}
