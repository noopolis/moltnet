package rooms

import (
	"slices"
	"strings"
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

func TestServiceNetworkIncludesPairingWarnings(t *testing.T) {
	t.Parallel()

	service := newTestService()
	service.pairingStatuses["pair_a"] = pairingStatus{value: protocol.PairingStatusIncompatible}
	service.pairingStatuses["pair_b"] = pairingStatus{value: protocol.PairingStatusDegraded}

	network := service.Network()
	if len(network.Warnings) != 2 {
		t.Fatalf("expected two warnings, got %#v", network.Warnings)
	}
	if network.Warnings[0].Code != "pairings.incompatible" ||
		network.Warnings[0].Severity != "error" ||
		!strings.Contains(network.Warnings[0].Message, "1 pairing") {
		t.Fatalf("unexpected incompatible warning %#v", network.Warnings[0])
	}
	if network.Warnings[1].Code != "pairings.degraded" ||
		network.Warnings[1].Severity != "warning" {
		t.Fatalf("unexpected degraded warning %#v", network.Warnings[1])
	}
}
