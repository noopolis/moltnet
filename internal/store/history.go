package store

import (
	"sort"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type MessageStore interface {
	AppendMessage(message protocol.Message) error
	ListRoomMessages(roomID string, before string, limit int) protocol.MessagePage
	ListDirectConversations() []protocol.DirectConversation
	ListDMMessages(dmID string, before string, limit int) protocol.MessagePage
}

func (s *MemoryStore) AppendMessage(message protocol.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		s.roomMessages[message.Target.RoomID] = append(s.roomMessages[message.Target.RoomID], message)
	case protocol.TargetKindDM:
		s.directMessages[message.Target.DMID] = append(s.directMessages[message.Target.DMID], message)
		if _, ok := s.directMembers[message.Target.DMID]; !ok {
			s.directMembers[message.Target.DMID] = make(map[string]struct{})
		}
		for _, participantID := range message.Target.ParticipantIDs {
			s.directMembers[message.Target.DMID][participantID] = struct{}{}
		}
		s.directMembers[message.Target.DMID][message.From.ID] = struct{}{}
	}

	return nil
}

func (s *MemoryStore) ListRoomMessages(roomID string, before string, limit int) protocol.MessagePage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return pageMessages(s.roomMessages[roomID], before, limit)
}

func (s *MemoryStore) ListDirectConversations() []protocol.DirectConversation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conversations := make([]protocol.DirectConversation, 0, len(s.directMessages))
	for dmID, messages := range s.directMessages {
		conversation := protocol.DirectConversation{
			ID:           dmID,
			MessageCount: len(messages),
		}

		if len(messages) > 0 {
			conversation.LastMessageAt = messages[len(messages)-1].CreatedAt
			conversation.FQID = protocol.DMFQID(messages[len(messages)-1].NetworkID, dmID)
		}

		members := make([]string, 0, len(s.directMembers[dmID]))
		for memberID := range s.directMembers[dmID] {
			members = append(members, memberID)
		}
		sort.Strings(members)
		conversation.ParticipantIDs = members
		conversations = append(conversations, conversation)
	}

	sort.Slice(conversations, func(i int, j int) bool {
		return conversations[i].ID < conversations[j].ID
	})

	return conversations
}

func (s *MemoryStore) ListDMMessages(dmID string, before string, limit int) protocol.MessagePage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return pageMessages(s.directMessages[dmID], before, limit)
}
