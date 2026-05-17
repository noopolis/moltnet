package rooms

import (
	"context"
	"net/http"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) enforceTargetWritePolicy(ctx context.Context, target protocol.Target, actor protocol.Actor) error {
	room, err := s.roomForWrite(ctx, target)
	if err != nil {
		return err
	}
	if room.ID == "" {
		return nil
	}
	if s.canWriteRoom(ctx, room, actor) {
		return nil
	}
	return writeForbiddenError(room.ID)
}

func (s *Service) roomForWrite(ctx context.Context, target protocol.Target) (protocol.Room, error) {
	roomID := strings.TrimSpace(target.RoomID)
	if target.Kind == protocol.TargetKindThread {
		if thread, ok, err := s.getThread(ctx, target.ThreadID); err != nil {
			return protocol.Room{}, err
		} else if ok {
			roomID = thread.RoomID
		}
	}

	room, ok, err := s.getRoom(ctx, roomID)
	if err != nil {
		return protocol.Room{}, err
	}
	if !ok {
		return protocol.Room{}, unknownRoomError(roomID)
	}
	return normalizeRoomAccess(room), nil
}

func (s *Service) canWriteRoom(ctx context.Context, room protocol.Room, actor protocol.Actor) bool {
	mode := authn.ModeFromContext(ctx)
	claims, hasClaims := authn.ClaimsFromContext(ctx)
	if mode == authn.ModeNone && !hasClaims {
		return true
	}
	if hasClaims && claims.Allows(authn.ScopeAdmin) && claims.Allows(authn.ScopeWrite) {
		return true
	}

	policy := protocol.NormalizeRoomWritePolicy(room.WritePolicy)
	if policy == protocol.RoomWritePolicyOperators {
		return hasClaims && claims.StaticToken() && claims.Allows(authn.ScopeWrite)
	}
	if actorIsRoomMember(room, actor) {
		return true
	}
	if policy == protocol.RoomWritePolicyRegisteredAgents {
		return s.registeredAgentActorCanWrite(ctx, actor, claims, hasClaims)
	}
	return false
}

func actorIsRoomMember(room protocol.Room, actor protocol.Actor) bool {
	normalized := protocol.NormalizeActor(room.NetworkID, actor)
	for _, memberID := range room.Members {
		if protocol.ActorMatches(normalized.NetworkID, normalized.ID, memberID) {
			return true
		}
	}
	return false
}

func (s *Service) registeredAgentActorCanWrite(
	ctx context.Context,
	actor protocol.Actor,
	claims authn.Claims,
	hasClaims bool,
) bool {
	if !hasClaims || !claims.AgentToken() {
		return false
	}
	agentID, local := s.agentCollisionID(actor)
	if !local || strings.TrimSpace(agentID) == "" || !claims.AllowsAgent(agentID) {
		return false
	}
	registration, ok, err := s.registeredAgent(ctx, agentID)
	if err != nil || !ok {
		return false
	}
	return registration.CredentialKey == strings.TrimSpace(claims.CredentialKey)
}

func normalizeRoomAccess(room protocol.Room) protocol.Room {
	room.Visibility = protocol.NormalizeRoomVisibility(room.Visibility)
	room.WritePolicy = protocol.NormalizeRoomWritePolicy(room.WritePolicy)
	room.Members = protocol.SortedUniqueTrimmedStrings(room.Members)
	return room
}

func (s *Service) readableRoom(ctx context.Context, room protocol.Room) (protocol.Room, bool) {
	room = normalizeRoomAccess(room)
	if s.canReadRoom(ctx, room) {
		room.Access = &protocol.RoomAccess{
			CanRead:  true,
			CanWrite: s.canWriteRoom(ctx, room, actorFromClaims(ctx)),
			Reason:   roomAccessReason(ctx, room),
		}
		return room, true
	}
	return protocol.Room{}, false
}

func (s *Service) canReadRoom(ctx context.Context, room protocol.Room) bool {
	mode := authn.ModeFromContext(ctx)
	claims, hasClaims := authn.ClaimsFromContext(ctx)
	if mode == authn.ModeNone && !authn.PublicReadFromContext(ctx) {
		return true
	}
	if hasClaims && claims.AllowsAny([]authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}) {
		return true
	}
	return authn.PublicReadFromContext(ctx) &&
		protocol.NormalizeRoomVisibility(room.Visibility) == protocol.RoomVisibilityPublic
}

func actorFromClaims(ctx context.Context) protocol.Actor {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.AgentToken() {
		return protocol.Actor{}
	}
	for _, agentID := range claims.AgentIDs() {
		return protocol.Actor{Type: "agent", ID: agentID}
	}
	return protocol.Actor{}
}

func roomAccessReason(ctx context.Context, room protocol.Room) string {
	readPrefix := "private"
	if authn.PublicReadFromContext(ctx) &&
		protocol.NormalizeRoomVisibility(room.Visibility) == protocol.RoomVisibilityPublic {
		readPrefix = "public-read"
	}
	return readPrefix + "/" + protocol.NormalizeRoomWritePolicy(room.WritePolicy) + "-write"
}

func readForbiddenError(roomID string) error {
	return &Error{
		status: http.StatusForbidden,
		msg:    "room " + strings.TrimSpace(roomID) + " is not readable",
		cause:  ErrAgentForbidden,
	}
}
