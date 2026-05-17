package transport

import (
	"context"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func attachmentEventFilter(
	policy *authn.Policy,
	ctx context.Context,
	service Service,
	networkID string,
	agentID string,
) eventFilter {
	if policy == nil || !policy.PublicRead() {
		return nil
	}
	if claims, ok := authn.ClaimsFromContext(ctx); ok &&
		(claims.Allows(authn.ScopeObserve) || claims.Allows(authn.ScopeAdmin)) {
		return nil
	}

	publicFilter := publicOpenEvent(service)
	return func(ctx context.Context, event protocol.Event) bool {
		return publicFilter(ctx, event) || attachedAgentEvent(event, networkID, agentID)
	}
}

func attachedAgentEvent(event protocol.Event, networkID string, agentID string) bool {
	switch event.Type {
	case protocol.EventTypeMessageCreated:
		return event.Message != nil && attachedAgentMessage(event.Message, networkID, agentID)
	case protocol.EventTypeDMCreated:
		return event.DM != nil && participantsIncludeAttachedAgent(event.DM.ParticipantIDs, networkID, agentID)
	default:
		return false
	}
}

func attachedAgentMessage(message *protocol.Message, networkID string, agentID string) bool {
	if message.Target.Kind != protocol.TargetKindDM {
		return false
	}
	return participantsIncludeAttachedAgent(message.Target.ParticipantIDs, networkID, agentID)
}

func attachmentWakeEvent(event protocol.Event, networkID string, agent protocol.Actor) bool {
	if event.Type != protocol.EventTypeMessageCreated || event.Message == nil {
		return false
	}
	message := event.Message
	if message.Target.Kind == protocol.TargetKindDM {
		return participantsIncludeAttachedAgent(message.Target.ParticipantIDs, networkID, agent.ID)
	}
	for _, mention := range message.Mentions {
		if protocol.ActorMatches(networkID, agent.ID, mention) || mention == agent.Name {
			return true
		}
	}
	return false
}

func attachmentRemovedEvent(event protocol.Event, networkID string, agentID string) bool {
	return event.Type == protocol.EventTypeAgentRemoved &&
		event.Agent != nil &&
		protocol.ActorMatches(networkID, agentID, event.Agent.AgentID)
}

func participantsIncludeAttachedAgent(participants []string, networkID string, agentID string) bool {
	for _, participantID := range participants {
		if protocol.ActorMatches(networkID, agentID, participantID) {
			return true
		}
	}
	return false
}
