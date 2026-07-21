package transport

import (
	"context"
	"net/http"
	"slices"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func directConversationForRequest(service Service, ctx context.Context, dmID string) (protocol.DirectConversation, error) {
	return service.GetDirectConversationContext(ctx, dmID)
}

func attachmentEventFilter(
	policy *authn.Policy,
	ctx context.Context,
	service Service,
	networkID string,
	agentID string,
) eventFilter {
	if policy == nil || !policy.Enabled() {
		return nil
	}
	publicCtx := authn.WithPublicRead(authn.WithMode(context.Background(), authn.ModeNone), true)
	publicFilter := publicOpenEvent(service)
	publicOnly := func(event protocol.Event) bool {
		return publicFilter(publicCtx, event) && authoritativeEventNetwork(publicCtx, service, event)
	}
	return func(ctx context.Context, event protocol.Event) bool {
		if event.Type == protocol.EventTypeReplayGap {
			return true
		}
		return (policy.PublicRead() && publicOnly(event)) || attachedAgentEvent(ctx, service, event, networkID, agentID)
	}
}

func attachedAgentEvent(ctx context.Context, service Service, event protocol.Event, networkID string, agentID string) bool {
	switch event.Type {
	case protocol.EventTypeMessageCreated:
		if event.Message == nil {
			return false
		}
		if event.Message.Target.Kind == protocol.TargetKindDM {
			conversation, err := attachedConversation(service, ctx, event.Message.Target.DMID)
			if err == nil {
				parentNetwork := conversation.NetworkID
				if strings.TrimSpace(parentNetwork) == "" {
					parentNetwork = networkID
				}
				return messageNetworksMatchTransport(parentNetwork, networkID, event.NetworkID, event.Message) && (len(event.Message.Target.ParticipantIDs) == 0 || participantsEqualTransport(conversation.ParticipantIDs, event.Message.Target.ParticipantIDs)) && participantsIncludeAttachedAgentInNetwork(conversation.ParticipantIDs, parentNetwork, networkID, agentID)
			}
			// Older compatibility fixtures may not have a DM lookup. Only a
			// genuine not-found result permits the event's participant fallback;
			// authorization and service failures fail closed.
			if statusForError(err) != http.StatusNotFound {
				return false
			}
			participantNetwork := strings.TrimSpace(event.Message.NetworkID)
			if participantNetwork == "" {
				participantNetwork = networkID
			}
			return messageNetworksMatchTransport(networkID, networkID, event.NetworkID, event.Message) && participantsIncludeAttachedAgentInNetwork(event.Message.Target.ParticipantIDs, participantNetwork, networkID, agentID)
		}
		return attachedRoomTarget(ctx, service, event.Message.Target, networkID, agentID, event.NetworkID, event.Message.NetworkID)
	case protocol.EventTypeRoomCreated, protocol.EventTypeRoomRemoved:
		return event.Room != nil && authoritativeRoomEvent(ctx, service, event.Room, networkID, agentID, event.NetworkID)
	case protocol.EventTypeRoomMembersUpdated:
		return event.Room != nil && authoritativeRoomEvent(ctx, service, event.Room, networkID, agentID, event.NetworkID)
	case protocol.EventTypeThreadCreated:
		if event.Thread == nil {
			return false
		}
		thread, err := service.GetThreadContext(ctx, event.Thread.ID)
		if err != nil || thread.FQID != event.Thread.FQID || thread.RoomID != event.Thread.RoomID || thread.ParentMessageID != event.Thread.ParentMessageID {
			return false
		}
		room, err := service.GetRoomContext(ctx, thread.RoomID)
		return err == nil && eventNetworksMatchTransport(room.NetworkID, networkID, event.NetworkID, event.Thread.NetworkID) && attachedRoom(&room, networkID, agentID)
	case protocol.EventTypeDMCreated:
		if event.DM == nil {
			return false
		}
		conversation, err := attachedConversation(service, ctx, event.DM.ID)
		parentNetwork := conversation.NetworkID
		if strings.TrimSpace(parentNetwork) == "" {
			parentNetwork = networkID
		}
		return err == nil && eventNetworksMatchTransport(parentNetwork, networkID, event.NetworkID, event.DM.NetworkID) && sameConversationIdentity(conversation, *event.DM) && participantsIncludeAttachedAgentInNetwork(conversation.ParticipantIDs, parentNetwork, networkID, agentID)
	case protocol.EventTypeAgentConnected, protocol.EventTypeAgentDisconnected:
		return rooms.AgentEventIdentityMatches(networkID, agentID, event)
	case protocol.EventTypeAgentWakeDelivered, protocol.EventTypeAgentWakeFailed:
		return rooms.AgentEventIdentityMatches(networkID, agentID, event)
	default:
		return false
	}
}

func attachedConversation(service Service, ctx context.Context, dmID string) (protocol.DirectConversation, error) {
	return service.GetDirectConversationContext(ctx, dmID)
}

func attachedRoomTarget(ctx context.Context, service Service, target protocol.Target, networkID, agentID string, eventNetworkIDs ...string) bool {
	roomID := target.RoomID
	if target.Kind == protocol.TargetKindThread {
		thread, err := service.GetThreadContext(ctx, target.ThreadID)
		if err != nil {
			return false
		}
		if target.RoomID != "" && target.RoomID != thread.RoomID {
			return false
		}
		roomID = thread.RoomID
	}
	room, err := service.GetRoomContext(ctx, roomID)
	return err == nil && eventNetworksMatchTransport(room.NetworkID, networkID, eventNetworkIDs...) && attachedRoom(&room, networkID, agentID)
}

func attachedRoom(room *protocol.Room, networkID, agentID string) bool {
	if room == nil {
		return false
	}
	if protocol.NormalizeRoomVisibility(room.Visibility) == protocol.RoomVisibilityPublic {
		return true
	}
	parentNetwork := room.NetworkID
	if strings.TrimSpace(parentNetwork) == "" {
		parentNetwork = networkID
	}
	for _, memberID := range room.Members {
		if rooms.AgentIdentityMatches(networkID, agentID, parentNetwork, memberID) {
			return true
		}
	}
	return false
}

func authoritativeRoomEvent(ctx context.Context, service Service, payload *protocol.Room, networkID, agentID, eventNetworkID string) bool {
	room, err := service.GetRoomContext(ctx, payload.ID)
	return err == nil && eventNetworksMatchTransport(room.NetworkID, networkID, eventNetworkID, payload.NetworkID) && sameRoomIdentity(room, *payload) && attachedRoom(&room, networkID, agentID)
}

func sameRoomIdentity(left, right protocol.Room) bool {
	return left.ID == right.ID && networkIdentityEqualTransport(left.NetworkID, right.NetworkID) && left.FQID == right.FQID &&
		left.Name == right.Name && left.Visibility == right.Visibility && left.WritePolicy == right.WritePolicy &&
		participantsEqualTransport(left.Members, right.Members)
}

func sameConversationIdentity(left, right protocol.DirectConversation) bool {
	return left.ID == right.ID && networkIdentityEqualTransport(left.NetworkID, right.NetworkID) && left.FQID == right.FQID &&
		participantsEqualTransport(left.ParticipantIDs, right.ParticipantIDs)
}

func networkIdentityEqualTransport(authoritative, supplied string) bool {
	supplied = strings.TrimSpace(supplied)
	return supplied == "" || strings.TrimSpace(authoritative) == supplied
}

func authoritativeEventNetwork(ctx context.Context, service Service, event protocol.Event) bool {
	switch event.Type {
	case protocol.EventTypeMessageCreated:
		if event.Message == nil {
			return false
		}
		target := event.Message.Target
		if target.Kind == protocol.TargetKindDM {
			conversation, err := service.GetDirectConversationContext(ctx, target.DMID)
			return err == nil && messageNetworksMatchTransport(conversation.NetworkID, service.Network().ID, event.NetworkID, event.Message)
		}
		roomID := target.RoomID
		if target.Kind == protocol.TargetKindThread {
			thread, err := service.GetThreadContext(ctx, target.ThreadID)
			if err != nil || (target.RoomID != "" && target.RoomID != thread.RoomID) {
				return false
			}
			roomID = thread.RoomID
		}
		if target.Kind != protocol.TargetKindRoom && target.Kind != protocol.TargetKindThread {
			return false
		}
		room, err := service.GetRoomContext(ctx, roomID)
		return err == nil && messageNetworksMatchTransport(room.NetworkID, service.Network().ID, event.NetworkID, event.Message)
	case protocol.EventTypeRoomCreated, protocol.EventTypeRoomMembersUpdated, protocol.EventTypeRoomRemoved:
		if event.Room == nil {
			return false
		}
		room, err := service.GetRoomContext(ctx, event.Room.ID)
		return err == nil && eventNetworksMatchTransport(room.NetworkID, service.Network().ID, event.NetworkID, event.Room.NetworkID)
	case protocol.EventTypeThreadCreated:
		if event.Thread == nil {
			return false
		}
		thread, err := service.GetThreadContext(ctx, event.Thread.ID)
		if err != nil {
			return false
		}
		room, err := service.GetRoomContext(ctx, thread.RoomID)
		return err == nil && eventNetworksMatchTransport(room.NetworkID, service.Network().ID, event.NetworkID, event.Thread.NetworkID)
	case protocol.EventTypeDMCreated:
		if event.DM == nil {
			return false
		}
		conversation, err := service.GetDirectConversationContext(ctx, event.DM.ID)
		return err == nil && eventNetworksMatchTransport(conversation.NetworkID, service.Network().ID, event.NetworkID, event.DM.NetworkID)
	default:
		return true
	}
}

func eventNetworksMatchTransport(authoritative, fallback string, supplied ...string) bool {
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

func messageNetworksMatchTransport(authoritative, fallback, outer string, message *protocol.Message) bool {
	if message == nil {
		return false
	}
	return eventNetworksMatchTransport(authoritative, fallback, outer, message.NetworkID)
}

func participantsEqualTransport(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
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
	return event.Type == protocol.EventTypeAgentRemoved && rooms.AgentEventIdentityMatches(networkID, agentID, event)
}

func participantsIncludeAttachedAgent(participants []string, networkID string, agentID string) bool {
	return participantsIncludeAttachedAgentInNetwork(participants, networkID, networkID, agentID)
}

func participantsIncludeAttachedAgentInNetwork(participants []string, parentNetworkID, networkID, agentID string) bool {
	for _, participantID := range participants {
		if rooms.AgentIdentityMatches(networkID, agentID, parentNetworkID, participantID) {
			return true
		}
	}
	return false
}
