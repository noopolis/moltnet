package store

import (
	"context"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type ContextRoomStore interface {
	CreateRoomContext(ctx context.Context, room protocol.Room) error
	GetRoomContext(ctx context.Context, id string) (protocol.Room, bool, error)
	GetThreadContext(ctx context.Context, id string) (protocol.Thread, bool, error)
	ListRoomsContext(ctx context.Context) ([]protocol.Room, error)
	UpdateRoomMembersContext(ctx context.Context, roomID string, add []string, remove []string) (protocol.Room, error)
}

type ContextMessageStore interface {
	AppendMessageContext(ctx context.Context, message protocol.Message) error
	ListRoomMessagesContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListThreadsContext(ctx context.Context, roomID string) ([]protocol.Thread, error)
	ListThreadMessagesContext(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListDirectConversationsContext(ctx context.Context) ([]protocol.DirectConversation, error)
	GetDirectConversationContext(ctx context.Context, dmID string) (protocol.DirectConversation, bool, error)
	ListDMMessagesContext(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListArtifactsContext(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error)
}

type AppendLifecycle struct {
	Thread *protocol.Thread
	DM     *protocol.DirectConversation
}

type ContextLifecycleMessageStore interface {
	AppendMessageWithLifecycleContext(ctx context.Context, message protocol.Message) (AppendLifecycle, error)
}

type ContextAgentStore interface {
	ListAgentsContext(ctx context.Context) ([]protocol.AgentSummary, error)
	GetAgentContext(ctx context.Context, agentID string) (protocol.AgentSummary, bool, error)
}

type ContextAgentRegistryStore interface {
	RegisterAgentContext(ctx context.Context, registration protocol.AgentRegistration) (protocol.AgentRegistration, error)
	ListRegisteredAgentsContext(ctx context.Context) ([]protocol.AgentRegistration, error)
	GetRegisteredAgentContext(ctx context.Context, agentID string) (protocol.AgentRegistration, bool, error)
}
