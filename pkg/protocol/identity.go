package protocol

import "strings"

func ScopedAgentID(networkID string, agentID string) string {
	trimmedNetwork := strings.TrimSpace(networkID)
	trimmedAgent := strings.TrimSpace(agentID)
	if trimmedNetwork == "" || trimmedAgent == "" {
		return trimmedAgent
	}

	return trimmedNetwork + ":" + trimmedAgent
}

func ParseScopedAgentID(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "molt://") {
		return "", "", false
	}

	left, right, ok := strings.Cut(trimmed, ":")
	if !ok || strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return "", "", false
	}

	return left, right, true
}

func ParseAgentFQID(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	prefix := "molt://"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", "", false
	}

	withoutPrefix := strings.TrimPrefix(trimmed, prefix)
	networkID, remainder, ok := strings.Cut(withoutPrefix, "/agents/")
	if !ok || strings.TrimSpace(networkID) == "" || strings.TrimSpace(remainder) == "" {
		return "", "", false
	}

	return networkID, remainder, true
}

func NormalizeActor(defaultNetworkID string, actor Actor) Actor {
	if strings.TrimSpace(actor.NetworkID) == "" {
		actor.NetworkID = strings.TrimSpace(defaultNetworkID)
	}
	if strings.TrimSpace(actor.FQID) == "" && strings.TrimSpace(actor.ID) != "" && strings.TrimSpace(actor.NetworkID) != "" {
		actor.FQID = AgentFQID(actor.NetworkID, actor.ID)
	}

	return actor
}

func ActorMatches(networkID string, actorID string, candidate string) bool {
	trimmedActorID := strings.TrimSpace(actorID)
	trimmedCandidate := strings.TrimSpace(candidate)
	if trimmedActorID == "" || trimmedCandidate == "" {
		return false
	}

	if trimmedCandidate == trimmedActorID {
		return true
	}
	if trimmedCandidate == ScopedAgentID(networkID, trimmedActorID) {
		return true
	}
	if trimmedCandidate == AgentFQID(networkID, trimmedActorID) {
		return true
	}

	return false
}

func ActorMatchesIdentity(defaultNetworkID string, actor Actor, candidate string) bool {
	normalized := NormalizeActor(defaultNetworkID, actor)
	actorNetworkID := strings.TrimSpace(normalized.NetworkID)
	actorID := strings.TrimSpace(normalized.ID)
	if actorNetworkID == "" || actorID == "" {
		return false
	}

	candidateNetworkID := strings.TrimSpace(defaultNetworkID)
	candidateID := strings.TrimSpace(candidate)
	if networkID, agentID, ok := ParseScopedAgentID(candidateID); ok {
		candidateNetworkID, candidateID = networkID, agentID
	} else if networkID, agentID, ok := ParseAgentFQID(candidateID); ok {
		candidateNetworkID, candidateID = networkID, agentID
	}

	return actorNetworkID == strings.TrimSpace(candidateNetworkID) && actorID == strings.TrimSpace(candidateID)
}

func RemoteParticipantID(currentNetworkID string, actor Actor) string {
	normalized := NormalizeActor(currentNetworkID, actor)
	if normalized.NetworkID != "" && normalized.NetworkID != currentNetworkID {
		return ScopedAgentID(normalized.NetworkID, normalized.ID)
	}

	return normalized.ID
}
