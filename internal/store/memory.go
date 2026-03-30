package store

import (
	"fmt"
	"sort"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type RoomStore interface {
	CreateRoom(room protocol.Room) error
	GetRoom(id string) (protocol.Room, bool)
	ListRooms() []protocol.Room
}

type MemoryStore struct {
	mu             sync.RWMutex
	rooms          map[string]protocol.Room
	roomMessages   map[string][]protocol.Message
	directMessages map[string][]protocol.Message
	directMembers  map[string]map[string]struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rooms:          make(map[string]protocol.Room),
		roomMessages:   make(map[string][]protocol.Message),
		directMessages: make(map[string][]protocol.Message),
		directMembers:  make(map[string]map[string]struct{}),
	}
}

func (s *MemoryStore) CreateRoom(room protocol.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[room.ID]; exists {
		return fmt.Errorf("room %q already exists", room.ID)
	}

	s.rooms[room.ID] = room
	return nil
}

func (s *MemoryStore) GetRoom(id string) (protocol.Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, ok := s.rooms[id]
	return room, ok
}

func (s *MemoryStore) ListRooms() []protocol.Room {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]protocol.Room, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room)
	}

	sort.Slice(rooms, func(i int, j int) bool {
		return rooms[i].ID < rooms[j].ID
	})

	return rooms
}
