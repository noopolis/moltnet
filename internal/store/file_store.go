package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type FileStore struct {
	*MemoryStore
	path      string
	persistMu sync.Mutex
}

type snapshot struct {
	Rooms          map[string]protocol.Room       `json:"rooms"`
	RoomMessages   map[string][]protocol.Message  `json:"room_messages"`
	Threads        map[string]protocol.Thread     `json:"threads"`
	ThreadMessages map[string][]protocol.Message  `json:"thread_messages"`
	DirectMessages map[string][]protocol.Message  `json:"direct_messages"`
	DirectMembers  map[string]map[string]struct{} `json:"direct_members"`
}

func NewFileStore(path string) (*FileStore, error) {
	memory := NewMemoryStore()
	store := &FileStore{
		MemoryStore: memory,
		path:        path,
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *FileStore) Health(context.Context) error {
	return nil
}

func (s *FileStore) GetRoom(id string) (protocol.Room, bool, error) {
	return s.MemoryStore.GetRoom(id)
}

func (s *FileStore) GetRoomContext(ctx context.Context, id string) (protocol.Room, bool, error) {
	return s.MemoryStore.GetRoomContext(ctx, id)
}

func (s *FileStore) GetThread(id string) (protocol.Thread, bool, error) {
	return s.MemoryStore.GetThread(id)
}

func (s *FileStore) GetThreadContext(ctx context.Context, id string) (protocol.Thread, bool, error) {
	return s.MemoryStore.GetThreadContext(ctx, id)
}

func (s *FileStore) ListRooms() ([]protocol.Room, error) {
	return s.MemoryStore.ListRooms()
}

func (s *FileStore) ListRoomsContext(ctx context.Context) ([]protocol.Room, error) {
	return s.MemoryStore.ListRoomsContext(ctx)
}

func (s *FileStore) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	return s.MemoryStore.ListRoomMessages(roomID, before, limit)
}

func (s *FileStore) ListRoomMessagesContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	return s.MemoryStore.ListRoomMessagesContext(ctx, roomID, page)
}

func (s *FileStore) ListThreads(roomID string) ([]protocol.Thread, error) {
	return s.MemoryStore.ListThreads(roomID)
}

func (s *FileStore) ListThreadsContext(ctx context.Context, roomID string) ([]protocol.Thread, error) {
	return s.MemoryStore.ListThreadsContext(ctx, roomID)
}

func (s *FileStore) ListThreadMessages(threadID string, before string, limit int) (protocol.MessagePage, error) {
	return s.MemoryStore.ListThreadMessages(threadID, before, limit)
}

func (s *FileStore) ListThreadMessagesContext(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	return s.MemoryStore.ListThreadMessagesContext(ctx, threadID, page)
}

func (s *FileStore) ListDirectConversations() ([]protocol.DirectConversation, error) {
	return s.MemoryStore.ListDirectConversations()
}

func (s *FileStore) ListDirectConversationsContext(ctx context.Context) ([]protocol.DirectConversation, error) {
	return s.MemoryStore.ListDirectConversationsContext(ctx)
}

func (s *FileStore) GetDirectConversationContext(ctx context.Context, dmID string) (protocol.DirectConversation, bool, error) {
	return s.MemoryStore.GetDirectConversationContext(ctx, dmID)
}

func (s *FileStore) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	return s.MemoryStore.ListDMMessages(dmID, before, limit)
}

func (s *FileStore) ListDMMessagesContext(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	return s.MemoryStore.ListDMMessagesContext(ctx, dmID, page)
}

func (s *FileStore) ListArtifacts(filter protocol.ArtifactFilter, before string, limit int) (protocol.ArtifactPage, error) {
	return s.MemoryStore.ListArtifacts(filter, before, limit)
}

func (s *FileStore) ListArtifactsContext(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error) {
	return s.MemoryStore.ListArtifactsContext(ctx, filter, page)
}

func (s *FileStore) CreateRoom(room protocol.Room) error {
	return s.CreateRoomContext(context.Background(), room)
}

func (s *FileStore) CreateRoomContext(_ context.Context, room protocol.Room) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	working := memoryStoreFromSnapshot(s.snapshot())
	if err := working.CreateRoomContext(context.Background(), room); err != nil {
		return err
	}

	next := snapshotFromMemoryStore(working)
	if err := s.persistSnapshot(next); err != nil {
		return err
	}

	s.restore(next)
	return nil
}

func (s *FileStore) AppendMessage(message protocol.Message) error {
	return s.AppendMessageContext(context.Background(), message)
}

func (s *FileStore) AppendMessageContext(_ context.Context, message protocol.Message) error {
	_, err := s.AppendMessageWithLifecycleContext(context.Background(), message)
	return err
}

func (s *FileStore) AppendMessageWithLifecycleContext(_ context.Context, message protocol.Message) (AppendLifecycle, error) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	working := memoryStoreFromSnapshot(s.snapshot())
	lifecycle, err := working.AppendMessageWithLifecycleContext(context.Background(), message)
	if err != nil {
		return AppendLifecycle{}, err
	}

	next := snapshotFromMemoryStore(working)
	if err := s.persistSnapshot(next); err != nil {
		return AppendLifecycle{}, err
	}

	s.restore(next)
	return lifecycle, nil
}

func (s *FileStore) UpdateRoomMembers(roomID string, add []string, remove []string) (protocol.Room, error) {
	return s.UpdateRoomMembersContext(context.Background(), roomID, add, remove)
}

func (s *FileStore) UpdateRoomMembersContext(
	_ context.Context,
	roomID string,
	add []string,
	remove []string,
) (protocol.Room, error) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	working := memoryStoreFromSnapshot(s.snapshot())
	room, err := working.UpdateRoomMembersContext(context.Background(), roomID, add, remove)
	if err != nil {
		return protocol.Room{}, err
	}

	next := snapshotFromMemoryStore(working)
	if err := s.persistSnapshot(next); err != nil {
		return protocol.Room{}, err
	}

	s.restore(next)
	return room, nil
}

func (s *FileStore) load() error {
	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read Moltnet store %q: %w", s.path, err)
	}

	var snapshot snapshot
	if err := json.Unmarshal(bytes, &snapshot); err != nil {
		return fmt.Errorf("decode Moltnet store %q: %w", s.path, err)
	}

	s.MemoryStore.rooms = cloneRooms(snapshot.Rooms)
	s.MemoryStore.roomMessages = cloneMessages(snapshot.RoomMessages)
	s.MemoryStore.threads = cloneThreads(snapshot.Threads)
	s.MemoryStore.roomThreads = collectRoomThreads(s.MemoryStore.threads)
	s.MemoryStore.threadMessages = cloneMessages(snapshot.ThreadMessages)
	s.MemoryStore.directMessages = cloneMessages(snapshot.DirectMessages)
	s.MemoryStore.directMembers = cloneMembers(snapshot.DirectMembers)
	s.MemoryStore.messageIDs = collectMessageIDs(
		s.MemoryStore.roomMessages,
		s.MemoryStore.threadMessages,
		s.MemoryStore.directMessages,
	)

	return nil
}

func (s *FileStore) persistSnapshot(snapshot snapshot) error {
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Moltnet store dir: %w", err)
	}
	if directory != "." && directory != "" {
		if err := os.Chmod(directory, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("secure Moltnet store dir: %w", err)
		}
	}

	bytes, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode Moltnet store: %w", err)
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, bytes, 0o600); err != nil {
		return fmt.Errorf("write Moltnet store: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace Moltnet store: %w", err)
	}

	return nil
}

func cloneRooms(values map[string]protocol.Room) map[string]protocol.Room {
	cloned := make(map[string]protocol.Room, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneThreads(values map[string]protocol.Thread) map[string]protocol.Thread {
	cloned := make(map[string]protocol.Thread, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func collectRoomThreads(values map[string]protocol.Thread) map[string]map[string]struct{} {
	grouped := make(map[string]map[string]struct{})
	for _, thread := range values {
		if _, ok := grouped[thread.RoomID]; !ok {
			grouped[thread.RoomID] = make(map[string]struct{})
		}
		grouped[thread.RoomID][thread.ID] = struct{}{}
	}
	return grouped
}

func cloneMessages(values map[string][]protocol.Message) map[string][]protocol.Message {
	cloned := make(map[string][]protocol.Message, len(values))
	for key, value := range values {
		cloned[key] = append([]protocol.Message(nil), value...)
	}
	return cloned
}

func cloneMembers(values map[string]map[string]struct{}) map[string]map[string]struct{} {
	cloned := make(map[string]map[string]struct{}, len(values))
	for key, value := range values {
		members := make(map[string]struct{}, len(value))
		for memberID := range value {
			members[memberID] = struct{}{}
		}
		cloned[key] = members
	}
	return cloned
}

func collectMessageIDs(groups ...map[string][]protocol.Message) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, group := range groups {
		for _, messages := range group {
			for _, message := range messages {
				if message.ID != "" {
					ids[message.ID] = struct{}{}
				}
			}
		}
	}

	return ids
}

func (s *FileStore) snapshot() snapshot {
	s.MemoryStore.mu.RLock()
	defer s.MemoryStore.mu.RUnlock()

	return snapshot{
		Rooms:          cloneRooms(s.MemoryStore.rooms),
		RoomMessages:   cloneMessages(s.MemoryStore.roomMessages),
		Threads:        cloneThreads(s.MemoryStore.threads),
		ThreadMessages: cloneMessages(s.MemoryStore.threadMessages),
		DirectMessages: cloneMessages(s.MemoryStore.directMessages),
		DirectMembers:  cloneMembers(s.MemoryStore.directMembers),
	}
}

func (s *FileStore) restore(state snapshot) {
	s.MemoryStore.mu.Lock()
	defer s.MemoryStore.mu.Unlock()

	s.MemoryStore.rooms = cloneRooms(state.Rooms)
	s.MemoryStore.roomMessages = cloneMessages(state.RoomMessages)
	s.MemoryStore.threads = cloneThreads(state.Threads)
	s.MemoryStore.roomThreads = collectRoomThreads(s.MemoryStore.threads)
	s.MemoryStore.threadMessages = cloneMessages(state.ThreadMessages)
	s.MemoryStore.directMessages = cloneMessages(state.DirectMessages)
	s.MemoryStore.directMembers = cloneMembers(state.DirectMembers)
	s.MemoryStore.messageIDs = collectMessageIDs(
		s.MemoryStore.roomMessages,
		s.MemoryStore.threadMessages,
		s.MemoryStore.directMessages,
	)
}

func snapshotFromMemoryStore(memory *MemoryStore) snapshot {
	memory.mu.RLock()
	defer memory.mu.RUnlock()

	return snapshot{
		Rooms:          cloneRooms(memory.rooms),
		RoomMessages:   cloneMessages(memory.roomMessages),
		Threads:        cloneThreads(memory.threads),
		ThreadMessages: cloneMessages(memory.threadMessages),
		DirectMessages: cloneMessages(memory.directMessages),
		DirectMembers:  cloneMembers(memory.directMembers),
	}
}

func memoryStoreFromSnapshot(state snapshot) *MemoryStore {
	memory := NewMemoryStore()
	memory.rooms = cloneRooms(state.Rooms)
	memory.roomMessages = cloneMessages(state.RoomMessages)
	memory.threads = cloneThreads(state.Threads)
	memory.roomThreads = collectRoomThreads(memory.threads)
	memory.threadMessages = cloneMessages(state.ThreadMessages)
	memory.directMessages = cloneMessages(state.DirectMessages)
	memory.directMembers = cloneMembers(state.DirectMembers)
	memory.messageIDs = collectMessageIDs(memory.roomMessages, memory.threadMessages, memory.directMessages)
	return memory
}
