package store

import (
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type snapshot struct {
	Rooms          map[string]protocol.Room                 `json:"rooms"`
	RoomMessages   map[string][]protocol.Message            `json:"room_messages"`
	Threads        map[string]protocol.Thread               `json:"threads"`
	ThreadMessages map[string][]protocol.Message            `json:"thread_messages"`
	DirectMessages map[string][]protocol.Message            `json:"direct_messages"`
	DirectMembers  map[string]map[string]struct{}           `json:"direct_members"`
	Agents         map[string]fileSnapshotAgentRegistration `json:"agents,omitempty"`
}

type fileSnapshotAgentRegistration struct {
	NetworkID     string    `json:"network_id"`
	AgentID       string    `json:"agent_id"`
	ActorUID      string    `json:"actor_uid"`
	ActorURI      string    `json:"actor_uri"`
	DisplayName   string    `json:"display_name,omitempty"`
	CredentialKey string    `json:"credential_key,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
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

func snapshotAgents(values map[string]protocol.AgentRegistration) map[string]fileSnapshotAgentRegistration {
	cloned := make(map[string]fileSnapshotAgentRegistration, len(values))
	for key, value := range values {
		cloned[key] = fileSnapshotAgentRegistration{
			NetworkID:     value.NetworkID,
			AgentID:       value.AgentID,
			ActorUID:      value.ActorUID,
			ActorURI:      value.ActorURI,
			DisplayName:   value.DisplayName,
			CredentialKey: value.CredentialKey,
			CreatedAt:     value.CreatedAt,
			UpdatedAt:     value.UpdatedAt,
		}
	}
	return cloned
}

func protocolAgents(values map[string]fileSnapshotAgentRegistration) map[string]protocol.AgentRegistration {
	cloned := make(map[string]protocol.AgentRegistration, len(values))
	for key, value := range values {
		cloned[key] = protocol.AgentRegistration{
			NetworkID:     value.NetworkID,
			AgentID:       value.AgentID,
			ActorUID:      value.ActorUID,
			ActorURI:      value.ActorURI,
			DisplayName:   value.DisplayName,
			CredentialKey: value.CredentialKey,
			CreatedAt:     value.CreatedAt,
			UpdatedAt:     value.UpdatedAt,
		}
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
		Agents:         snapshotAgents(s.MemoryStore.agents),
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
	s.MemoryStore.agents = protocolAgents(state.Agents)
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
		Agents:         snapshotAgents(memory.agents),
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
	memory.agents = protocolAgents(state.Agents)
	memory.messageIDs = collectMessageIDs(memory.roomMessages, memory.threadMessages, memory.directMessages)
	return memory
}
