package store

import (
	"context"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// AttachmentDeliveryStore makes message delivery resumable independently of
// the in-memory event broker. Preparing an agent with no cursor establishes a
// baseline at the current durable head; only later unacknowledged events replay.
type AttachmentDeliveryStore interface {
	AppendMessageEventWithLifecycleContext(context.Context, protocol.Message, protocol.Event) (AppendLifecycle, error)
	PrepareAttachmentDeliveryContext(context.Context, string) ([]protocol.Event, error)
	AcknowledgeAttachmentDeliveryContext(context.Context, string, string) error
}
