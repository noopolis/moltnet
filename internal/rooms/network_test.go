package rooms

import (
	"slices"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestServiceNetworkAdvertisesCompatibilityProtocols(t *testing.T) {
	t.Parallel()

	network := newTestService().Network()
	if !slices.Equal(network.Protocols.HTTP, []string{protocol.HTTPProtocolV1}) ||
		!slices.Equal(network.Protocols.Attach, []string{protocol.AttachmentProtocolV1}) ||
		!slices.Equal(network.Protocols.Pair, []string{protocol.PairProtocolV1}) {
		t.Fatalf("unexpected protocols %#v", network.Protocols)
	}
	if network.Capabilities.EventStream != "sse" ||
		network.Capabilities.AttachmentProtocol != "websocket" ||
		!network.Capabilities.HumanIngress ||
		!network.Capabilities.DirectMessages ||
		network.Capabilities.MessagePagination != "cursor" {
		t.Fatalf("unexpected network capabilities %#v", network.Capabilities)
	}
}
