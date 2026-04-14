package rooms

import (
	"context"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) resolveMentions(
	ctx context.Context,
	target protocol.Target,
	candidates []string,
) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	members, err := s.mentionContextMembers(ctx, target)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(candidates))
	mentions := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		resolved, err := s.resolveMentionCandidate(members, candidate)
		if err != nil {
			// Mentions are routing metadata; unresolved @text should not reject chat messages.
			continue
		}
		if resolved == "" {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}

		seen[resolved] = struct{}{}
		mentions = append(mentions, resolved)
	}

	if len(mentions) == 0 {
		return nil, nil
	}
	return mentions, nil
}

func (s *Service) mentionContextMembers(ctx context.Context, target protocol.Target) ([]string, error) {
	switch target.Kind {
	case protocol.TargetKindRoom:
		room, ok, err := s.getRoom(ctx, target.RoomID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, unknownRoomError(target.RoomID)
		}
		return room.Members, nil
	case protocol.TargetKindThread:
		room, ok, err := s.getRoom(ctx, target.RoomID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, unknownRoomError(target.RoomID)
		}
		return room.Members, nil
	case protocol.TargetKindDM:
		return target.ParticipantIDs, nil
	default:
		return nil, nil
	}
}

func (s *Service) resolveMentionCandidate(members []string, candidate string) (string, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return "", nil
	}

	if networkID, agentID, ok := protocol.ParseAgentFQID(trimmed); ok {
		if err := protocol.ValidateMemberID(trimmed); err != nil {
			return "", err
		}
		return resolveScopedMentionFromMembers(s.networkID, members, networkID, agentID)
	}

	if networkAlias, agentID, ok := protocol.ParseScopedAgentID(trimmed); ok {
		networkID, err := s.resolveNetworkAlias(networkAlias)
		if err != nil {
			return "", err
		}
		return resolveScopedMentionFromMembers(s.networkID, members, networkID, agentID)
	}

	return resolveShortMentionFromMembers(s.networkID, members, trimmed)
}

func (s *Service) resolveNetworkAlias(alias string) (string, error) {
	trimmed := strings.TrimSpace(alias)
	if trimmed == "" {
		return "", fmt.Errorf("mention network alias is required")
	}

	matches := make([]string, 0, 1)
	if trimmed == s.networkID || trimmed == s.networkName {
		matches = append(matches, s.networkID)
	}

	s.pairingsMu.RLock()
	for _, pairing := range s.pairings {
		if trimmed == pairing.ID ||
			trimmed == pairing.RemoteNetworkID ||
			(strings.TrimSpace(pairing.RemoteNetworkName) != "" && trimmed == pairing.RemoteNetworkName) {
			matches = append(matches, pairing.RemoteNetworkID)
		}
	}
	s.pairingsMu.RUnlock()

	matches = uniqueNonEmpty(matches)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown mention network alias %q", trimmed)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous mention network alias %q", trimmed)
	}
}

func resolveScopedMentionFromMembers(
	defaultNetworkID string,
	members []string,
	networkID string,
	agentID string,
) (string, error) {
	targetNetwork := strings.TrimSpace(networkID)
	targetAgent := strings.TrimSpace(agentID)
	if targetNetwork == "" || targetAgent == "" {
		return "", fmt.Errorf("scoped mention requires network and agent")
	}

	for _, member := range members {
		memberNetwork, memberAgent := memberIdentity(defaultNetworkID, member)
		if memberNetwork == targetNetwork && memberAgent == targetAgent {
			return protocol.AgentFQID(targetNetwork, targetAgent), nil
		}
	}

	return "", fmt.Errorf("unknown mention @%s:%s", targetNetwork, targetAgent)
}

func resolveShortMentionFromMembers(defaultNetworkID string, members []string, agentID string) (string, error) {
	targetAgent := strings.TrimSpace(agentID)
	if targetAgent == "" {
		return "", fmt.Errorf("mention agent is required")
	}

	matches := make([]string, 0, 1)
	for _, member := range members {
		memberNetwork, memberAgent := memberIdentity(defaultNetworkID, member)
		if memberAgent == targetAgent {
			matches = append(matches, protocol.AgentFQID(memberNetwork, memberAgent))
		}
	}

	matches = uniqueNonEmpty(matches)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown mention @%s", targetAgent)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous mention @%s", targetAgent)
	}
}

func memberIdentity(defaultNetworkID string, member string) (string, string) {
	trimmed := strings.TrimSpace(member)
	if networkID, agentID, ok := protocol.ParseAgentFQID(trimmed); ok {
		return networkID, agentID
	}
	if networkID, agentID, ok := protocol.ParseScopedAgentID(trimmed); ok {
		return networkID, agentID
	}
	return strings.TrimSpace(defaultNetworkID), trimmed
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	return unique
}
