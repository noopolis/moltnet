package rooms

import (
	"context"
	"slices"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) pairingDiscoveryRestricted(ctx context.Context) bool {
	claims, ok := authn.ClaimsFromContext(ctx)
	return ok && claims.HasAgentRestriction()
}

func (s *Service) filterPairingRooms(
	ctx context.Context,
	pairing protocol.Pairing,
	rooms []protocol.Room,
) []protocol.Room {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.HasAgentRestriction() {
		return rooms
	}
	allowed := s.allowedPairingViewerIdentities(claims)
	visible := make([]protocol.Room, 0, len(rooms))
	for _, room := range rooms {
		if !pairingRoomAuthorityMatches(pairing, room) {
			continue
		}
		if protocol.NormalizeRoomVisibility(room.Visibility) == protocol.RoomVisibilityPublic ||
			pairingRoomContainsAny(room, pairing.RemoteNetworkID, allowed) {
			visible = append(visible, room)
		}
	}
	return visible
}

func (s *Service) filterPairingAgents(
	ctx context.Context,
	pairing protocol.Pairing,
	agents []protocol.AgentSummary,
	visibleRooms []protocol.Room,
) []protocol.AgentSummary {
	if !s.pairingDiscoveryRestricted(ctx) {
		return agents
	}

	memberships := pairingRoomMemberships(pairing, visibleRooms)
	identities := make([]string, len(agents))
	identityCounts := make(map[string]int, len(agents))
	for index, agent := range agents {
		identity, ok := pairingAgentIdentity(pairing, agent)
		if !ok {
			continue
		}
		identities[index] = identity
		identityCounts[identity]++
	}

	visible := make([]protocol.AgentSummary, 0, len(agents))
	for index, agent := range agents {
		identity := identities[index]
		if identity == "" || identityCounts[identity] != 1 {
			continue
		}
		roomIDs := make([]string, 0)
		for roomID, members := range memberships {
			if _, member := members[identity]; member {
				roomIDs = append(roomIDs, roomID)
			}
		}
		if len(roomIDs) == 0 {
			continue
		}
		slices.Sort(roomIDs)
		agent.Rooms = roomIDs
		visible = append(visible, agent)
	}
	return visible
}

func (s *Service) allowedPairingViewerIdentities(claims authn.Claims) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, agentID := range claims.AgentIDs() {
		if !claims.AllowsAgent(agentID) {
			continue
		}
		networkID, resolvedAgentID, ok := canonicalPairingIdentity(s.networkID, agentID)
		if !ok || networkID != s.networkID {
			continue
		}
		allowed[protocol.ScopedAgentID(networkID, resolvedAgentID)] = struct{}{}
	}
	return allowed
}

func pairingRoomAuthorityMatches(pairing protocol.Pairing, room protocol.Room) bool {
	expectedNetwork := strings.TrimSpace(pairing.RemoteNetworkID)
	roomID := strings.TrimSpace(room.ID)
	if expectedNetwork == "" || protocol.ValidateRoomID(roomID) != nil {
		return false
	}
	if supplied := strings.TrimSpace(room.NetworkID); supplied != "" && supplied != expectedNetwork {
		return false
	}
	if supplied := strings.TrimSpace(room.FQID); supplied != "" && supplied != protocol.RoomFQID(expectedNetwork, roomID) {
		return false
	}
	return true
}

func pairingRoomContainsAny(room protocol.Room, defaultNetworkID string, allowed map[string]struct{}) bool {
	for _, memberID := range room.Members {
		networkID, agentID, ok := canonicalPairingIdentity(defaultNetworkID, memberID)
		if !ok {
			continue
		}
		if _, visible := allowed[protocol.ScopedAgentID(networkID, agentID)]; visible {
			return true
		}
	}
	return false
}

func pairingRoomMemberships(pairing protocol.Pairing, rooms []protocol.Room) map[string]map[string]struct{} {
	memberships := make(map[string]map[string]struct{}, len(rooms))
	for _, room := range rooms {
		if !pairingRoomAuthorityMatches(pairing, room) {
			continue
		}
		members := make(map[string]struct{}, len(room.Members))
		for _, memberID := range room.Members {
			networkID, agentID, ok := canonicalPairingIdentity(pairing.RemoteNetworkID, memberID)
			if ok {
				members[protocol.ScopedAgentID(networkID, agentID)] = struct{}{}
			}
		}
		memberships[room.ID] = members
	}
	return memberships
}

func pairingAgentIdentity(pairing protocol.Pairing, agent protocol.AgentSummary) (string, bool) {
	defaultNetworkID := strings.TrimSpace(pairing.RemoteNetworkID)
	if supplied := strings.TrimSpace(agent.NetworkID); supplied != "" {
		defaultNetworkID = supplied
	}
	networkID, agentID, ok := canonicalPairingIdentity(defaultNetworkID, agent.ID)
	if !ok {
		return "", false
	}
	if supplied := strings.TrimSpace(agent.NetworkID); supplied != "" && supplied != networkID {
		return "", false
	}
	if supplied := strings.TrimSpace(agent.FQID); supplied != "" {
		fqNetwork, fqAgent, parsed := protocol.ParseAgentFQID(supplied)
		if !parsed || strings.TrimSpace(fqNetwork) != networkID || strings.TrimSpace(fqAgent) != agentID {
			return "", false
		}
	}
	return protocol.ScopedAgentID(networkID, agentID), true
}

func canonicalPairingIdentity(defaultNetworkID string, value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if networkID, agentID, ok := protocol.ParseScopedAgentID(value); ok {
		return validPairingIdentity(networkID, agentID)
	}
	if networkID, agentID, ok := protocol.ParseAgentFQID(value); ok {
		return validPairingIdentity(networkID, agentID)
	}
	return validPairingIdentity(defaultNetworkID, value)
}

func validPairingIdentity(networkID string, agentID string) (string, string, bool) {
	networkID = strings.TrimSpace(networkID)
	agentID = strings.TrimSpace(agentID)
	if protocol.ValidateMessageID(networkID) != nil || protocol.ValidateMessageID(agentID) != nil {
		return "", "", false
	}
	return networkID, agentID, true
}
