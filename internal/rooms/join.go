package rooms

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) JoinRoomContext(
	ctx context.Context,
	roomID string,
	request protocol.JoinRoomRequest,
) (protocol.Room, error) {
	id := strings.TrimSpace(roomID)
	room, ok, err := s.getRoom(ctx, id)
	if err != nil {
		return protocol.Room{}, err
	}
	if !ok {
		return protocol.Room{}, unknownRoomError(id)
	}

	credential, configured := s.roomCredential(id)
	if !configured || !credentialsEqual(credential, request.Credential) {
		return protocol.Room{}, joinForbiddenError()
	}

	actor := protocol.NormalizeActor(s.networkID, request.From)
	agentID, authorized := s.joinActorAuthorized(ctx, actor)
	if !authorized {
		return protocol.Room{}, joinForbiddenError()
	}

	updated, err := s.updateRoomMembers(ctx, id, []string{agentID}, nil)
	if err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			return protocol.Room{}, unknownRoomError(id)
		}
		return protocol.Room{}, err
	}
	if !membersEqual(room.Members, updated.Members) {
		s.publishEvent(protocol.Event{
			ID:        newPrefixedID("evt"),
			Type:      protocol.EventTypeRoomMembersUpdated,
			NetworkID: s.networkID,
			Room:      &updated,
			CreatedAt: updated.CreatedAt,
		})
	}

	readable, ok := s.readableRoom(ctx, updated)
	if !ok {
		return protocol.Room{}, joinForbiddenError()
	}
	return readable, nil
}

func (s *Service) setRoomCredential(roomID string, credential protocol.SecretString) {
	id := strings.TrimSpace(roomID)
	value := strings.TrimSpace(credential.Reveal())
	s.roomCredentialsMu.Lock()
	defer s.roomCredentialsMu.Unlock()
	if value == "" {
		delete(s.roomCredentials, id)
		return
	}
	s.roomCredentials[id] = protocol.NewSecretString(value)
}

func (s *Service) clearRoomCredential(roomID string) {
	s.roomCredentialsMu.Lock()
	delete(s.roomCredentials, strings.TrimSpace(roomID))
	s.roomCredentialsMu.Unlock()
}

func (s *Service) roomCredential(roomID string) (protocol.SecretString, bool) {
	s.roomCredentialsMu.RLock()
	credential, ok := s.roomCredentials[strings.TrimSpace(roomID)]
	s.roomCredentialsMu.RUnlock()
	return credential, ok
}

func credentialsEqual(left, right protocol.SecretString) bool {
	leftValue := []byte(strings.TrimSpace(left.Reveal()))
	rightValue := []byte(strings.TrimSpace(right.Reveal()))
	if len(leftValue) != len(rightValue) {
		return false
	}
	return subtle.ConstantTimeCompare(leftValue, rightValue) == 1
}

func (s *Service) joinActorAuthorized(ctx context.Context, actor protocol.Actor) (string, bool) {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok {
		return "", false
	}
	agentID, local := s.agentCollisionID(actor)
	if strings.TrimSpace(actor.Type) != "agent" || !local ||
		strings.TrimSpace(agentID) == "" || !claims.AgentToken() ||
		!claims.HasAgentRestriction() || !claims.AllowsAgent(agentID) {
		return "", false
	}
	return agentID, true
}
