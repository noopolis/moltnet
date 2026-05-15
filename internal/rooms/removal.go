package rooms

import (
	"context"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const removeModeSoft = "soft"

func (s *Service) RemoveAgentContext(ctx context.Context, agentID string) (protocol.RemoveResult, error) {
	id := strings.TrimSpace(agentID)
	if err := protocol.ValidateMemberID(id); err != nil {
		return protocol.RemoveResult{}, unknownAgentError(id)
	}

	agent, err := s.GetAgent(id)
	if err != nil {
		return protocol.RemoveResult{}, err
	}
	if err := s.removeAgent(ctx, id); err != nil {
		return protocol.RemoveResult{}, err
	}
	if err := s.removeRegisteredAgent(ctx, id); err != nil {
		return protocol.RemoveResult{}, err
	}

	s.agentPresenceMu.Lock()
	delete(s.connectedAgents, id)
	s.agentPresenceMu.Unlock()

	now := s.now().UTC()
	s.publishEvent(protocol.Event{
		ID:        newPrefixedID("evt"),
		Type:      protocol.EventTypeAgentRemoved,
		NetworkID: s.networkID,
		Agent: &protocol.AgentEvent{
			AgentID:   id,
			NetworkID: s.networkID,
			FQID:      agent.FQID,
			Name:      agent.Name,
			Reason:    "removed",
		},
		CreatedAt: now,
	})

	return protocol.RemoveResult{Removed: true, Kind: "agent", ID: id, Mode: removeModeSoft}, nil
}

func (s *Service) RemoveRoomContext(ctx context.Context, roomID string) (protocol.RemoveResult, error) {
	id := strings.TrimSpace(roomID)
	if err := protocol.ValidateRoomID(id); err != nil {
		return protocol.RemoveResult{}, invalidRoomIDError()
	}

	room, ok, err := s.getRoom(ctx, id)
	if err != nil {
		return protocol.RemoveResult{}, err
	}
	if !ok {
		return protocol.RemoveResult{}, unknownRoomError(id)
	}
	if err := s.removeRoom(ctx, id); err != nil {
		return protocol.RemoveResult{}, err
	}

	now := s.now().UTC()
	s.publishEvent(protocol.Event{
		ID:        newPrefixedID("evt"),
		Type:      protocol.EventTypeRoomRemoved,
		NetworkID: s.networkID,
		Room:      &room,
		CreatedAt: now,
	})

	return protocol.RemoveResult{Removed: true, Kind: "room", ID: id, Mode: removeModeSoft}, nil
}
