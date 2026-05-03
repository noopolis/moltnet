package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type RoomStore interface {
	CreateRoom(room protocol.Room) error
	GetRoom(id string) (protocol.Room, bool, error)
	GetThread(id string) (protocol.Thread, bool, error)
	ListRooms() ([]protocol.Room, error)
	UpdateRoomMembers(roomID string, add []string, remove []string) (protocol.Room, error)
}

type MemoryStore struct {
	mu             sync.RWMutex
	messageIDs     map[string]struct{}
	rooms          map[string]protocol.Room
	roomMessages   map[string][]protocol.Message
	threads        map[string]protocol.Thread
	roomThreads    map[string]map[string]struct{}
	threadMessages map[string][]protocol.Message
	directMessages map[string][]protocol.Message
	directMembers  map[string]map[string]struct{}
	agents         map[string]protocol.AgentRegistration
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		messageIDs:     make(map[string]struct{}),
		rooms:          make(map[string]protocol.Room),
		roomMessages:   make(map[string][]protocol.Message),
		threads:        make(map[string]protocol.Thread),
		roomThreads:    make(map[string]map[string]struct{}),
		threadMessages: make(map[string][]protocol.Message),
		directMessages: make(map[string][]protocol.Message),
		directMembers:  make(map[string]map[string]struct{}),
		agents:         make(map[string]protocol.AgentRegistration),
	}
}

func (s *MemoryStore) Health(context.Context) error {
	return nil
}

func (s *MemoryStore) CreateRoom(room protocol.Room) error {
	return s.CreateRoomContext(context.Background(), room)
}

func (s *MemoryStore) CreateRoomContext(_ context.Context, room protocol.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[room.ID]; exists {
		return fmt.Errorf("%w: %q", ErrRoomExists, room.ID)
	}

	s.rooms[room.ID] = room
	return nil
}

func (s *MemoryStore) GetRoom(id string) (protocol.Room, bool, error) {
	return s.GetRoomContext(context.Background(), id)
}

func (s *MemoryStore) GetRoomContext(_ context.Context, id string) (protocol.Room, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, ok := s.rooms[id]
	return room, ok, nil
}

func (s *MemoryStore) GetThread(id string) (protocol.Thread, bool, error) {
	return s.GetThreadContext(context.Background(), id)
}

func (s *MemoryStore) GetThreadContext(_ context.Context, id string) (protocol.Thread, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	thread, ok := s.threads[id]
	return thread, ok, nil
}

func (s *MemoryStore) ListRooms() ([]protocol.Room, error) {
	return s.ListRoomsContext(context.Background())
}

func (s *MemoryStore) ListRoomsContext(_ context.Context) ([]protocol.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]protocol.Room, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room)
	}

	slices.SortFunc(rooms, func(left, right protocol.Room) int {
		return strings.Compare(left.ID, right.ID)
	})

	return rooms, nil
}

func (s *MemoryStore) ListAgentsContext(_ context.Context) ([]protocol.AgentSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return agentSummariesLocked(s.rooms), nil
}

func (s *MemoryStore) GetAgentContext(_ context.Context, agentID string) (protocol.AgentSummary, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return agentSummaryLocked(s.rooms, agentID)
}

func (s *MemoryStore) RegisterAgentContext(
	_ context.Context,
	registration protocol.AgentRegistration,
) (protocol.AgentRegistration, error) {
	registration.AgentToken = ""

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.agents[registration.AgentID]
	if ok {
		existing.AgentToken = ""
		if existing.CredentialKey != registration.CredentialKey {
			return protocol.AgentRegistration{}, ErrAgentCredential
		}
		if registration.DisplayName != "" {
			existing.DisplayName = registration.DisplayName
		}
		existing.UpdatedAt = registration.UpdatedAt
		s.agents[registration.AgentID] = existing
		return existing, nil
	}

	s.agents[registration.AgentID] = registration
	return registration, nil
}

func (s *MemoryStore) ListRegisteredAgentsContext(_ context.Context) ([]protocol.AgentRegistration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]protocol.AgentRegistration, 0, len(s.agents))
	for _, agent := range s.agents {
		agents = append(agents, agent)
	}
	slices.SortFunc(agents, func(left, right protocol.AgentRegistration) int {
		return strings.Compare(left.AgentID, right.AgentID)
	})
	return agents, nil
}

func (s *MemoryStore) GetRegisteredAgentContext(
	_ context.Context,
	agentID string,
) (protocol.AgentRegistration, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, ok := s.agents[strings.TrimSpace(agentID)]
	return agent, ok, nil
}

func (s *MemoryStore) GetRegisteredAgentByCredentialKeyContext(
	_ context.Context,
	credentialKey string,
) (protocol.AgentRegistration, bool, error) {
	trimmed := strings.TrimSpace(credentialKey)
	if trimmed == "" {
		return protocol.AgentRegistration{}, false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, agent := range s.agents {
		if agent.CredentialKey == trimmed {
			return agent, true, nil
		}
	}
	return protocol.AgentRegistration{}, false, nil
}

func (s *MemoryStore) UpdateRoomMembers(roomID string, add []string, remove []string) (protocol.Room, error) {
	return s.UpdateRoomMembersContext(context.Background(), roomID, add, remove)
}

func (s *MemoryStore) UpdateRoomMembersContext(
	_ context.Context,
	roomID string,
	add []string,
	remove []string,
) (protocol.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, ok := s.rooms[roomID]
	if !ok {
		return protocol.Room{}, fmt.Errorf("%w: %q", ErrRoomNotFound, roomID)
	}

	memberSet := make(map[string]struct{}, len(room.Members))
	for _, memberID := range protocol.UniqueTrimmedStrings(room.Members) {
		memberSet[memberID] = struct{}{}
	}
	for _, memberID := range protocol.UniqueTrimmedStrings(add) {
		memberSet[memberID] = struct{}{}
	}
	for _, memberID := range protocol.UniqueTrimmedStrings(remove) {
		delete(memberSet, memberID)
	}

	members := make([]string, 0, len(memberSet))
	for memberID := range memberSet {
		members = append(members, memberID)
	}
	slices.Sort(members)
	room.Members = members
	s.rooms[roomID] = room
	return room, nil
}

func agentSummariesLocked(rooms map[string]protocol.Room) []protocol.AgentSummary {
	agentsByID := make(map[string]*protocol.AgentSummary)
	for _, room := range rooms {
		for _, memberID := range room.Members {
			agent, ok := agentsByID[memberID]
			if !ok {
				agent = &protocol.AgentSummary{
					ID:        memberID,
					NetworkID: room.NetworkID,
					FQID:      protocol.AgentFQID(room.NetworkID, memberID),
				}
				agentsByID[memberID] = agent
			}
			agent.Rooms = append(agent.Rooms, room.ID)
		}
	}

	agents := make([]protocol.AgentSummary, 0, len(agentsByID))
	for _, agent := range agentsByID {
		slices.Sort(agent.Rooms)
		agents = append(agents, *agent)
	}
	slices.SortFunc(agents, func(left, right protocol.AgentSummary) int {
		return strings.Compare(left.ID, right.ID)
	})

	return agents
}

func agentSummaryLocked(rooms map[string]protocol.Room, agentID string) (protocol.AgentSummary, bool, error) {
	trimmed := strings.TrimSpace(agentID)
	if trimmed == "" {
		return protocol.AgentSummary{}, false, nil
	}

	for _, agent := range agentSummariesLocked(rooms) {
		if agent.ID == trimmed {
			return agent, true, nil
		}
	}

	return protocol.AgentSummary{}, false, nil
}
