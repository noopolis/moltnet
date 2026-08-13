package machine

import (
	"context"

	"github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// Provider is the narrow, credential-free dependency used by the machine
// executor. It deliberately mirrors only the client operations machine needs.
type Provider interface {
	GetRoom(context.Context, string) (protocol.Room, error)
	ListDMs(context.Context) (protocol.DirectConversationPage, error)
	ListRoomMessages(context.Context, string, protocol.PageRequest) (protocol.MessagePage, error)
	ListDMMessages(context.Context, string, protocol.PageRequest) (protocol.MessagePage, error)
	SendMessage(context.Context, protocol.SendMessageRequest) (protocol.MessageAccepted, error)
}

// Keep the concrete HTTP client usable without exposing its transport details
// through this package's public API.
var _ Provider = (*client.Client)(nil)
