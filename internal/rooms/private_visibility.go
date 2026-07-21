package rooms

import (
	"context"
	"slices"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// AgentEventIdentityMatches resolves every identity authority on an agent
// event before comparing it with an attachment or restricted claim. Empty
// authorities inherit defaultNetworkID; explicit authorities must agree.
func AgentEventIdentityMatches(defaultNetworkID, candidate string, event protocol.Event) bool {
	if event.Agent == nil {
		return false
	}
	eventNetwork, eventAgent, ok := resolvedAgentEventIdentity(defaultNetworkID, event.NetworkID, *event.Agent)
	if !ok {
		return false
	}
	candidateNetwork, candidateAgent := normalizedAgentIdentity(defaultNetworkID, candidate)
	return eventNetwork != "" && eventNetwork == candidateNetwork && eventAgent == candidateAgent
}

func resolvedAgentEventIdentity(defaultNetworkID, outerNetwork string, agent protocol.AgentEvent) (string, string, bool) {
	agentID := strings.TrimSpace(agent.AgentID)
	if agentID == "" {
		return "", "", false
	}
	networkID := strings.TrimSpace(defaultNetworkID)
	explicitNetworkSeen := false
	for _, supplied := range []string{outerNetwork, agent.NetworkID} {
		if supplied = strings.TrimSpace(supplied); supplied != "" {
			if explicitNetworkSeen && networkID != supplied {
				return "", "", false
			}
			networkID = supplied
			explicitNetworkSeen = true
		}
	}
	if fqid := strings.TrimSpace(agent.FQID); fqid != "" {
		fqNetwork, fqAgent, parsed := protocol.ParseAgentFQID(fqid)
		if !parsed || strings.TrimSpace(fqAgent) != agentID {
			return "", "", false
		}
		fqNetwork = strings.TrimSpace(fqNetwork)
		if explicitNetworkSeen && networkID != fqNetwork {
			return "", "", false
		}
		networkID = fqNetwork
		explicitNetworkSeen = true
	}
	return networkID, agentID, networkID != ""
}

func (s *Service) validateArtifactScope(ctx context.Context, filter protocol.ArtifactFilter) error {
	parentKinds := 0
	if filter.RoomID != "" {
		parentKinds++
	}
	if filter.ThreadID != "" {
		parentKinds++
	}
	if filter.DMID != "" {
		parentKinds++
	}
	if parentKinds != 1 {
		return invalidRoomRequestReasonError("exactly one artifact parent is required")
	}
	if filter.RoomID != "" {
		room, ok, err := s.getRoom(ctx, filter.RoomID)
		if err != nil {
			return err
		}
		if !ok {
			return unknownRoomError(filter.RoomID)
		}
		if _, readable := s.readableRoom(ctx, room); !readable {
			return readForbiddenError(filter.RoomID)
		}
	}
	if filter.ThreadID != "" {
		thread, ok, err := s.getThread(ctx, filter.ThreadID)
		if err != nil {
			return err
		}
		if !ok {
			return unknownThreadError(filter.ThreadID)
		}
		room, roomOK, err := s.getRoom(ctx, thread.RoomID)
		if err != nil {
			return err
		}
		if !roomOK {
			return unknownRoomError(thread.RoomID)
		}
		if _, readable := s.readableRoom(ctx, room); !readable {
			return readForbiddenError(thread.RoomID)
		}
	}
	if filter.DMID != "" {
		conversation, ok, err := s.getDirectConversation(ctx, filter.DMID)
		if err != nil {
			return err
		}
		if !ok {
			return unknownDirectConversationError(filter.DMID)
		}
		if !s.canReadDirectConversation(ctx, conversation) {
			return agentForbiddenError("direct conversation is not readable")
		}
	}
	return nil
}

// eventVisible is deliberately fail-closed: a restricted stream must not
// receive an event whose parent cannot be resolved authoritatively.
func (s *Service) eventVisible(ctx context.Context, event protocol.Event) bool {
	if event.Type == protocol.EventTypeReplayGap {
		return true
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.HasAgentRestriction() {
		return true
	}
	allowed := func(event protocol.Event) bool {
		for _, agentID := range claims.AgentIDs() {
			if !claims.AllowsAgent(agentID) {
				continue
			}
			if AgentEventIdentityMatches(s.networkID, agentID, event) {
				return true
			}
		}
		return false
	}
	roomVisible := func(roomID string) bool {
		room, found, err := s.getRoom(ctx, roomID)
		return err == nil && found && s.canReadRoom(ctx, room)
	}
	roomEventVisible := func(payload *protocol.Room) bool {
		if payload == nil {
			return false
		}
		authoritative, found, err := s.getRoom(ctx, payload.ID)
		return err == nil && found && eventNetworksMatch(authoritative.NetworkID, s.networkID, event.NetworkID, payload.NetworkID) && roomsSecurityEqual(authoritative, *payload) && s.canReadRoom(ctx, authoritative)
	}
	threadEventVisible := func(payload *protocol.Thread) bool {
		if payload == nil {
			return false
		}
		authoritative, found, err := s.getThread(ctx, payload.ID)
		if err != nil || !found || !threadsSecurityEqual(authoritative, *payload) {
			return false
		}
		room, found, err := s.getRoom(ctx, authoritative.RoomID)
		return err == nil && found && eventNetworksMatch(room.NetworkID, s.networkID, event.NetworkID, payload.NetworkID) && roomVisible(authoritative.RoomID)
	}
	dmEventVisible := func(payload *protocol.DirectConversation) bool {
		if payload == nil {
			return false
		}
		authoritative, found, err := s.getDirectConversation(ctx, payload.ID)
		return err == nil && found && eventNetworksMatch(authoritative.NetworkID, s.networkID, event.NetworkID, payload.NetworkID) && directConversationsSecurityEqual(authoritative, *payload) && s.canReadDirectConversation(ctx, authoritative)
	}
	switch event.Type {
	case protocol.EventTypeRoomCreated, protocol.EventTypeRoomMembersUpdated, protocol.EventTypeRoomRemoved:
		return roomEventVisible(event.Room)
	case protocol.EventTypeThreadCreated:
		return threadEventVisible(event.Thread)
	case protocol.EventTypeMessageCreated:
		if event.Message == nil {
			return false
		}
		message := event.Message
		target := event.Message.Target
		if target.Kind == protocol.TargetKindDM {
			conversation, found, err := s.getDirectConversation(ctx, target.DMID)
			return err == nil && found && messageNetworksMatch(conversation.NetworkID, s.networkID, event.NetworkID, message) && participantsEqual(conversation.ParticipantIDs, target.ParticipantIDs) && s.canReadDirectConversation(ctx, conversation)
		}
		if target.Kind == protocol.TargetKindThread {
			thread, found, err := s.getThread(ctx, target.ThreadID)
			if err != nil || !found {
				return false
			}
			room, found, err := s.getRoom(ctx, thread.RoomID)
			return err == nil && found && messageNetworksMatch(room.NetworkID, s.networkID, event.NetworkID, message) && (target.RoomID == "" || target.RoomID == thread.RoomID) && roomVisible(thread.RoomID)
		}
		if target.Kind != protocol.TargetKindRoom {
			return false
		}
		room, found, err := s.getRoom(ctx, target.RoomID)
		return err == nil && found && messageNetworksMatch(room.NetworkID, s.networkID, event.NetworkID, message) && s.canReadRoom(ctx, room)
	case protocol.EventTypeDMCreated:
		return dmEventVisible(event.DM)
	case protocol.EventTypeAgentConnected, protocol.EventTypeAgentDisconnected, protocol.EventTypeAgentRemoved,
		protocol.EventTypeAgentWakeDelivered, protocol.EventTypeAgentWakeFailed:
		return allowed(event)
	default:
		return false
	}
}

func roomsSecurityEqual(left, right protocol.Room) bool {
	return left.ID == right.ID && networkIdentityEqual(left.NetworkID, right.NetworkID) && left.FQID == right.FQID &&
		left.Name == right.Name && left.Visibility == right.Visibility && left.WritePolicy == right.WritePolicy &&
		participantsEqual(left.Members, right.Members)
}

func threadsSecurityEqual(left, right protocol.Thread) bool {
	return left.ID == right.ID && networkIdentityEqual(left.NetworkID, right.NetworkID) && left.FQID == right.FQID &&
		left.RoomID == right.RoomID && left.ParentMessageID == right.ParentMessageID
}

func directConversationsSecurityEqual(left, right protocol.DirectConversation) bool {
	return left.ID == right.ID && networkIdentityEqual(left.NetworkID, right.NetworkID) && left.FQID == right.FQID &&
		participantsEqual(left.ParticipantIDs, right.ParticipantIDs)
}

func networkIdentityEqual(authoritative, supplied string) bool {
	supplied = strings.TrimSpace(supplied)
	return supplied == "" || strings.TrimSpace(authoritative) == supplied
}

func eventNetworksMatch(authoritative, fallback string, supplied ...string) bool {
	expected := strings.TrimSpace(authoritative)
	if expected == "" {
		expected = strings.TrimSpace(fallback)
	}
	for _, networkID := range supplied {
		if networkID = strings.TrimSpace(networkID); networkID != "" && networkID != expected {
			return false
		}
	}
	return expected != "" || len(supplied) == 0
}

func messageNetworksMatch(authoritative, fallback, outer string, message *protocol.Message) bool {
	if message == nil {
		return false
	}
	return eventNetworksMatch(authoritative, fallback, outer, message.NetworkID)
}

func participantsEqual(left, right []string) bool {
	return slices.Equal(protocol.SortedUniqueTrimmedStrings(left), protocol.SortedUniqueTrimmedStrings(right))
}
