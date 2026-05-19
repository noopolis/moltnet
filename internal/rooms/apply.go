package rooms

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) ApplyConfigContext(
	ctx context.Context,
	request protocol.ApplyConfigRequest,
) (protocol.ApplyConfigResult, error) {
	if err := s.validateApplyConfigRequest(request); err != nil {
		return protocol.ApplyConfigResult{}, err
	}

	agents, err := s.applyAgents(ctx, request.Agents)
	if err != nil {
		return protocol.ApplyConfigResult{}, err
	}
	rooms, err := s.applyRooms(ctx, request.Rooms)
	if err != nil {
		return protocol.ApplyConfigResult{}, err
	}

	return protocol.ApplyConfigResult{
		Applied: true,
		Agents:  agents,
		Rooms:   rooms,
	}, nil
}

func (s *Service) validateApplyConfigRequest(request protocol.ApplyConfigRequest) error {
	if networkID := strings.TrimSpace(request.NetworkID); networkID != "" && networkID != s.networkID {
		return invalidMessageRequestError(fmt.Sprintf("network_id %q does not match server network %q", networkID, s.networkID))
	}

	agentCredentials := make(map[string]string)
	for index, agent := range request.Agents {
		agentID := strings.TrimSpace(agent.ID)
		if err := protocol.ValidateMemberID(agentID); err != nil {
			return invalidMessageRequestError(fmt.Sprintf("agents[%d].id %s", index, err.Error()))
		}
		credentialKey := strings.TrimSpace(agent.CredentialKey)
		if credentialKey == "" {
			return invalidMessageRequestError(fmt.Sprintf("agents[%d].credential_key is required", index))
		}
		if existing, ok := agentCredentials[agentID]; ok && existing != credentialKey {
			return invalidMessageRequestError(fmt.Sprintf("agent %q has multiple credential keys", agentID))
		}
		agentCredentials[agentID] = credentialKey
	}
	for index, room := range request.Rooms {
		if err := protocol.ValidateCreateRoomRequest(room); err != nil {
			return invalidRoomRequestReasonError(fmt.Sprintf("rooms[%d]: %s", index, err.Error()))
		}
	}

	return nil
}

func (s *Service) applyAgents(
	ctx context.Context,
	agents []protocol.ApplyAgentRequest,
) ([]protocol.AgentRegistration, error) {
	registrations := make([]protocol.AgentRegistration, 0, len(agents))
	seen := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		agentID := strings.TrimSpace(agent.ID)
		if _, ok := seen[agentID]; ok {
			continue
		}
		seen[agentID] = struct{}{}

		registration, err := s.reconcileAgent(ctx, protocol.AgentRegistration{
			NetworkID:     s.networkID,
			AgentID:       agentID,
			ActorUID:      newPrefixedID("actor"),
			ActorURI:      protocol.AgentFQID(s.networkID, agentID),
			DisplayName:   strings.TrimSpace(agent.Name),
			CredentialKey: strings.TrimSpace(agent.CredentialKey),
			CreatedAt:     s.now().UTC(),
			UpdatedAt:     s.now().UTC(),
		})
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, registration)
	}
	return registrations, nil
}

func (s *Service) reconcileAgent(
	ctx context.Context,
	registration protocol.AgentRegistration,
) (protocol.AgentRegistration, error) {
	if s.agentRegistry == nil {
		return registration, nil
	}
	return s.agentRegistry.ReconcileRegisteredAgentContext(ctx, registration)
}

func (s *Service) applyRooms(
	ctx context.Context,
	rooms []protocol.CreateRoomRequest,
) ([]protocol.Room, error) {
	applied := make([]protocol.Room, 0, len(rooms))
	for _, request := range rooms {
		room, err := s.CreateRoomContext(ctx, request)
		if err != nil {
			if !errors.Is(err, ErrRoomExists) {
				return nil, err
			}
			room, err = s.ReconcileRoomContext(ctx, request)
			if err != nil {
				return nil, err
			}
		}
		applied = append(applied, room)
	}
	return applied, nil
}
