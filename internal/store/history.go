package store

import (
	"context"
	"slices"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type MessageStore interface {
	AppendMessage(message protocol.Message) error
	ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error)
	ListThreads(roomID string) ([]protocol.Thread, error)
	ListThreadMessages(threadID string, before string, limit int) (protocol.MessagePage, error)
	ListDirectConversations() ([]protocol.DirectConversation, error)
	ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error)
	ListArtifacts(filter protocol.ArtifactFilter, before string, limit int) (protocol.ArtifactPage, error)
}

func (s *MemoryStore) AppendMessage(message protocol.Message) error {
	return s.AppendMessageContext(context.Background(), message)
}

func (s *MemoryStore) AppendMessageContext(_ context.Context, message protocol.Message) error {
	_, err := s.AppendMessageWithLifecycleContext(context.Background(), message)
	return err
}

func (s *MemoryStore) AppendMessageWithLifecycleContext(_ context.Context, message protocol.Message) (AppendLifecycle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.messageIDs[message.ID]; exists {
		return AppendLifecycle{}, ErrDuplicateMessage
	}

	lifecycle := AppendLifecycle{}

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		s.roomMessages[message.Target.RoomID] = append(s.roomMessages[message.Target.RoomID], message)
	case protocol.TargetKindThread:
		s.threadMessages[message.Target.ThreadID] = append(s.threadMessages[message.Target.ThreadID], message)
		thread, ok := s.threads[message.Target.ThreadID]
		if !ok {
			thread = protocol.Thread{
				ID:              message.Target.ThreadID,
				NetworkID:       message.NetworkID,
				FQID:            protocol.ThreadFQID(message.NetworkID, message.Target.ThreadID),
				RoomID:          message.Target.RoomID,
				ParentMessageID: message.Target.ParentMessageID,
			}
		}
		thread.MessageCount = len(s.threadMessages[message.Target.ThreadID])
		thread.LastMessageAt = message.CreatedAt
		s.threads[message.Target.ThreadID] = thread
		if _, ok := s.roomThreads[thread.RoomID]; !ok {
			s.roomThreads[thread.RoomID] = make(map[string]struct{})
		}
		s.roomThreads[thread.RoomID][thread.ID] = struct{}{}
		if thread.MessageCount == 1 {
			copyThread := thread
			lifecycle.Thread = &copyThread
		}
	case protocol.TargetKindDM:
		s.directMessages[message.Target.DMID] = append(s.directMessages[message.Target.DMID], message)
		if _, ok := s.directMembers[message.Target.DMID]; !ok {
			s.directMembers[message.Target.DMID] = make(map[string]struct{})
		}
		for _, participantID := range message.Target.ParticipantIDs {
			s.directMembers[message.Target.DMID][participantID] = struct{}{}
		}
		s.directMembers[message.Target.DMID][protocol.RemoteParticipantID(message.NetworkID, message.From)] = struct{}{}
		if conversation, ok := s.directConversationLocked(message.Target.DMID); ok && conversation.MessageCount == 1 {
			copyConversation := conversation
			lifecycle.DM = &copyConversation
		}
	}
	s.messageIDs[message.ID] = struct{}{}

	return lifecycle, nil
}

func (s *MemoryStore) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListRoomMessagesContext(context.Background(), roomID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *MemoryStore) ListRoomMessagesContext(
	_ context.Context,
	roomID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return pageMessagesResult(s.roomMessages[roomID], page)
}

func (s *MemoryStore) ListThreads(roomID string) ([]protocol.Thread, error) {
	return s.ListThreadsContext(context.Background(), roomID)
}

func (s *MemoryStore) ListThreadsContext(_ context.Context, roomID string) ([]protocol.Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	threadIDs := s.roomThreads[roomID]
	threads := make([]protocol.Thread, 0, len(threadIDs))
	for threadID := range threadIDs {
		thread, ok := s.threads[threadID]
		if !ok {
			continue
		}
		threads = append(threads, thread)
	}

	slices.SortFunc(threads, func(left, right protocol.Thread) int {
		return strings.Compare(left.ID, right.ID)
	})

	return threads, nil
}

func (s *MemoryStore) ListThreadMessages(threadID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListThreadMessagesContext(context.Background(), threadID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *MemoryStore) ListThreadMessagesContext(
	_ context.Context,
	threadID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return pageMessagesResult(s.threadMessages[threadID], page)
}

func (s *MemoryStore) ListDirectConversations() ([]protocol.DirectConversation, error) {
	return s.ListDirectConversationsContext(context.Background())
}

func (s *MemoryStore) ListDirectConversationsContext(_ context.Context) ([]protocol.DirectConversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conversations := make([]protocol.DirectConversation, 0, len(s.directMessages))
	for dmID := range s.directMessages {
		conversation, ok := s.directConversationLocked(dmID)
		if !ok {
			continue
		}
		conversations = append(conversations, conversation)
	}

	slices.SortFunc(conversations, func(left, right protocol.DirectConversation) int {
		return strings.Compare(left.ID, right.ID)
	})

	return conversations, nil
}

func (s *MemoryStore) GetDirectConversationContext(
	_ context.Context,
	dmID string,
) (protocol.DirectConversation, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conversation, ok := s.directConversationLocked(dmID)
	return conversation, ok, nil
}

func (s *MemoryStore) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListDMMessagesContext(context.Background(), dmID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *MemoryStore) ListDMMessagesContext(
	_ context.Context,
	dmID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return pageMessagesResult(s.directMessages[dmID], page)
}

func (s *MemoryStore) ListArtifacts(
	filter protocol.ArtifactFilter,
	before string,
	limit int,
) (protocol.ArtifactPage, error) {
	return s.ListArtifactsContext(context.Background(), filter, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *MemoryStore) ListArtifactsContext(
	_ context.Context,
	filter protocol.ArtifactFilter,
	page protocol.PageRequest,
) (protocol.ArtifactPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages := s.artifactMessages(filter)
	artifacts := collectArtifacts(messages)

	return pageArtifactsResult(artifacts, page)
}

func (s *MemoryStore) artifactMessages(filter protocol.ArtifactFilter) []protocol.Message {
	switch {
	case filter.ThreadID != "":
		return append([]protocol.Message(nil), s.threadMessages[filter.ThreadID]...)
	case filter.DMID != "":
		return append([]protocol.Message(nil), s.directMessages[filter.DMID]...)
	case filter.RoomID != "":
		messages := append([]protocol.Message(nil), s.roomMessages[filter.RoomID]...)
		for threadID := range s.roomThreads[filter.RoomID] {
			messages = append(messages, s.threadMessages[threadID]...)
		}
		slices.SortFunc(messages, func(left, right protocol.Message) int {
			if left.CreatedAt.Before(right.CreatedAt) {
				return -1
			}
			if left.CreatedAt.After(right.CreatedAt) {
				return 1
			}
			return strings.Compare(left.ID, right.ID)
		})
		return messages
	default:
		messages := make([]protocol.Message, 0)
		for _, roomMessages := range s.roomMessages {
			messages = append(messages, roomMessages...)
		}
		for _, threadMessages := range s.threadMessages {
			messages = append(messages, threadMessages...)
		}
		for _, dmMessages := range s.directMessages {
			messages = append(messages, dmMessages...)
		}
		slices.SortFunc(messages, func(left, right protocol.Message) int {
			if left.CreatedAt.Before(right.CreatedAt) {
				return -1
			}
			if left.CreatedAt.After(right.CreatedAt) {
				return 1
			}
			return strings.Compare(left.ID, right.ID)
		})
		return messages
	}
}

func (s *MemoryStore) directConversationLocked(dmID string) (protocol.DirectConversation, bool) {
	messages, ok := s.directMessages[dmID]
	if !ok {
		return protocol.DirectConversation{}, false
	}

	conversation := protocol.DirectConversation{
		ID:           dmID,
		MessageCount: len(messages),
	}
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		conversation.NetworkID = last.NetworkID
		conversation.LastMessageAt = last.CreatedAt
		conversation.FQID = protocol.DMFQID(last.NetworkID, dmID)
	}

	members := make([]string, 0, len(s.directMembers[dmID]))
	for memberID := range s.directMembers[dmID] {
		members = append(members, memberID)
	}
	slices.Sort(members)
	conversation.ParticipantIDs = members

	return conversation, true
}
