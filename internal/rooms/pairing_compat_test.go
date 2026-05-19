package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type compatibilityPairingClient struct {
	mu           sync.Mutex
	network      protocol.Network
	networkCalls int
	roomsErr     error
}

func (c *compatibilityPairingClient) FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error) {
	c.mu.Lock()
	c.networkCalls++
	c.mu.Unlock()
	return c.network, nil
}

func (c *compatibilityPairingClient) FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error) {
	return nil, c.roomsErr
}

func (c *compatibilityPairingClient) FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error) {
	return nil, nil
}

func (c *compatibilityPairingClient) RelayMessage(
	ctx context.Context,
	pairing protocol.Pairing,
	request protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	return protocol.MessageAccepted{MessageID: request.ID, Accepted: true}, nil
}

func TestPairingCompatibilityDiagnostics(t *testing.T) {
	t.Parallel()

	missingPair := compatibleRemoteNetwork("remote")
	missingPair.Protocols.Pair = nil
	partialAttachOnly := compatibleRemoteNetwork("remote")
	partialAttachOnly.Protocols.HTTP = nil
	partialAttachOnly.Protocols.Attach = []string{protocol.AttachmentProtocolV1}
	partialAttachOnly.Protocols.Pair = nil
	partialPairOnly := compatibleRemoteNetwork("remote")
	partialPairOnly.Protocols.HTTP = nil
	unsupportedPair := compatibleRemoteNetwork("remote")
	unsupportedPair.Protocols.Pair = []string{"moltnet.pair.v0"}
	dmDisabled := compatibleRemoteNetwork("remote")
	dmDisabled.Capabilities.DirectMessages = false

	tests := []struct {
		name     string
		network  protocol.Network
		status   string
		reason   string
		remoteID string
		wantHTTP []string
		wantPair []string
	}{
		{
			name:     "network mismatch",
			network:  compatibleRemoteNetwork("wrong"),
			status:   protocol.PairingStatusIncompatible,
			reason:   pairingDiagnosticNetworkMismatch,
			remoteID: "wrong",
			wantHTTP: []string{protocol.HTTPProtocolV1},
			wantPair: []string{protocol.PairProtocolV1},
		},
		{
			name:     "missing pair protocol with advertised http v1 candidate",
			network:  missingPair,
			status:   protocol.PairingStatusConnected,
			remoteID: "remote",
			wantHTTP: []string{protocol.HTTPProtocolV1},
		},
		{
			name:     "partial modern attach only response",
			network:  partialAttachOnly,
			status:   protocol.PairingStatusIncompatible,
			reason:   pairingDiagnosticUnsupportedHTTP,
			remoteID: "remote",
		},
		{
			name:     "partial modern pair only response",
			network:  partialPairOnly,
			status:   protocol.PairingStatusIncompatible,
			reason:   pairingDiagnosticUnsupportedHTTP,
			remoteID: "remote",
			wantPair: []string{protocol.PairProtocolV1},
		},
		{
			name:     "unsupported pair protocol",
			network:  unsupportedPair,
			status:   protocol.PairingStatusIncompatible,
			reason:   pairingDiagnosticUnsupportedPair,
			remoteID: "remote",
			wantHTTP: []string{protocol.HTTPProtocolV1},
			wantPair: []string{"moltnet.pair.v0"},
		},
		{
			name: "legacy response",
			network: protocol.Network{
				ID:      "remote",
				Version: "0.1.0",
				Capabilities: protocol.NetworkCapabilities{
					DirectMessages:    true,
					MessagePagination: "cursor",
				},
			},
			status:   protocol.PairingStatusConnected,
			remoteID: "remote",
		},
		{
			name:     "direct messages disabled",
			network:  dmDisabled,
			status:   protocol.PairingStatusDegraded,
			reason:   pairingDiagnosticDirectMessagesOff,
			remoteID: "remote",
			wantHTTP: []string{protocol.HTTPProtocolV1},
			wantPair: []string{protocol.PairProtocolV1},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &compatibilityPairingClient{network: test.network}
			service := newPairingCompatibilityService(client)

			if _, err := service.PairingNetwork(context.Background(), "pair_1"); err != nil {
				t.Fatalf("PairingNetwork() error = %v", err)
			}

			pairings, err := service.ListPairings()
			if err != nil {
				t.Fatalf("ListPairings() error = %v", err)
			}
			if len(pairings) != 1 {
				t.Fatalf("unexpected pairings %#v", pairings)
			}
			pairing := pairings[0]
			if pairing.Token != "" {
				t.Fatalf("expected redacted pairing token, got %#v", pairing)
			}
			if pairing.Status != test.status {
				t.Fatalf("expected status %q, got %#v", test.status, pairing)
			}
			if pairing.Diagnostics == nil {
				t.Fatalf("expected diagnostics for %#v", pairing)
			}
			if pairing.Diagnostics.RemoteNetworkID != test.remoteID || pairing.Diagnostics.Reason != test.reason {
				t.Fatalf("unexpected diagnostics %#v", pairing.Diagnostics)
			}
			if !sameStrings(pairing.Diagnostics.RemoteProtocols.HTTP, test.wantHTTP) ||
				!sameStrings(pairing.Diagnostics.RemoteProtocols.Pair, test.wantPair) {
				t.Fatalf("unexpected remote protocols %#v", pairing.Diagnostics.RemoteProtocols)
			}
			payload, err := json.Marshal(pairing)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if strings.Contains(string(payload), "pair-secret") {
				t.Fatalf("pairing token leaked in diagnostics payload %s", payload)
			}
		})
	}
}

func TestListPairingsUsesCachedDiagnosticsWithoutRemoteCalls(t *testing.T) {
	t.Parallel()

	client := &compatibilityPairingClient{network: compatibleRemoteNetwork("remote")}
	service := newPairingCompatibilityService(client)

	if _, err := service.ListPairingsContext(context.Background(), protocol.PageRequest{}); err != nil {
		t.Fatalf("ListPairingsContext() error = %v", err)
	}
	if calls := client.snapshotNetworkCalls(); calls != 0 {
		t.Fatalf("expected no remote call from list, got %d", calls)
	}

	if _, err := service.PairingNetwork(context.Background(), "pair_1"); err != nil {
		t.Fatalf("PairingNetwork() error = %v", err)
	}
	if calls := client.snapshotNetworkCalls(); calls != 1 {
		t.Fatalf("expected one discovery call, got %d", calls)
	}

	if _, err := service.ListPairingsContext(context.Background(), protocol.PageRequest{}); err != nil {
		t.Fatalf("ListPairingsContext() error = %v", err)
	}
	if calls := client.snapshotNetworkCalls(); calls != 1 {
		t.Fatalf("expected list to reuse cached diagnostics, got %d calls", calls)
	}
}

func TestPairingDiscoveryRequiresCursorPagination(t *testing.T) {
	t.Parallel()

	network := compatibleRemoteNetwork("remote")
	network.Capabilities.MessagePagination = ""
	client := &compatibilityPairingClient{network: network}
	service := newPairingCompatibilityService(client)

	if _, err := service.PairingRoomsContext(context.Background(), "pair_1", protocol.PageRequest{}); err == nil {
		t.Fatal("expected missing cursor pagination to fail discovery")
	}
	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if pairings[0].Status != protocol.PairingStatusDegraded ||
		pairings[0].Diagnostics == nil ||
		pairings[0].Diagnostics.Reason != pairingDiagnosticMissingCursor {
		t.Fatalf("expected cursor pagination degradation, got %#v", pairings[0])
	}
}

func TestPairingNetworkDoesNotClearDiscoveryDegradation(t *testing.T) {
	t.Parallel()

	network := compatibleRemoteNetwork("remote")
	network.Capabilities.MessagePagination = ""
	client := &compatibilityPairingClient{network: network}
	service := newPairingCompatibilityService(client)

	if _, err := service.PairingRoomsContext(context.Background(), "pair_1", protocol.PageRequest{}); err == nil {
		t.Fatal("expected discovery degradation error")
	}
	if _, err := service.PairingNetwork(context.Background(), "pair_1"); err != nil {
		t.Fatalf("PairingNetwork() error = %v", err)
	}

	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if pairings[0].Status != protocol.PairingStatusDegraded ||
		pairings[0].Diagnostics == nil ||
		pairings[0].Diagnostics.Reason != pairingDiagnosticMissingCursor {
		t.Fatalf("expected network refresh to preserve discovery degradation, got %#v", pairings[0])
	}
}

func TestPairingRoomsErrorReplacesStaleDiagnostics(t *testing.T) {
	t.Parallel()

	network := compatibleRemoteNetwork("remote")
	network.Capabilities.DirectMessages = false
	client := &compatibilityPairingClient{
		network:  network,
		roomsErr: errors.New("rooms unavailable"),
	}
	service := newPairingCompatibilityService(client)

	if _, err := service.PairingRoomsContext(context.Background(), "pair_1", protocol.PageRequest{}); err == nil {
		t.Fatal("expected remote rooms error")
	}
	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if pairings[0].Status != protocol.PairingStatusError ||
		pairings[0].Diagnostics == nil ||
		pairings[0].Diagnostics.Reason != pairingDiagnosticRemoteRequestFailure ||
		pairings[0].Diagnostics.Message != "Remote rooms could not be fetched." {
		t.Fatalf("expected current error diagnostics, got %#v", pairings[0])
	}
}

func TestRelaySuppressesIncompatiblePairings(t *testing.T) {
	t.Parallel()

	network := compatibleRemoteNetwork("net_b")
	network.Protocols.Pair = []string{"moltnet.pair.v0"}
	client := &recordingPairingClient{
		network: network,
		notify:  make(chan struct{}, 8),
		done:    make(chan struct{}, 8),
	}
	service := newRelayCompatibilityService(client)

	service.relayMessage(roomRelayMessage())
	waitForNetworkCalls(t, client, 1)
	time.Sleep(20 * time.Millisecond)

	if calls := client.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("expected incompatible pairing to suppress relay, got %#v", calls)
	}
	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if pairings[0].Status != protocol.PairingStatusIncompatible ||
		pairings[0].Diagnostics == nil ||
		pairings[0].Diagnostics.Reason != pairingDiagnosticUnsupportedPair {
		t.Fatalf("unexpected incompatible pairing diagnostics %#v", pairings[0])
	}

	service.relayMessage(roomRelayMessage())
	time.Sleep(20 * time.Millisecond)
	if calls := snapshotRecordingNetworkCalls(client); len(calls) != 1 {
		t.Fatalf("expected incompatible pairing to stay cooled down, got network calls %#v", calls)
	}
}

func TestRelayAllowsRoomAndSuppressesDMWhenRemoteDMsDisabled(t *testing.T) {
	t.Parallel()

	network := compatibleRemoteNetwork("net_b")
	network.Capabilities.DirectMessages = false
	client := &recordingPairingClient{
		network: network,
		notify:  make(chan struct{}, 8),
		done:    make(chan struct{}, 8),
	}
	service := newRelayCompatibilityService(client)

	service.relayMessage(roomRelayMessage())
	waitForRelayCalls(t, client, 1)
	waitForRelayDone(t, client)
	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if pairings[0].Status != protocol.PairingStatusDegraded ||
		pairings[0].Diagnostics == nil ||
		pairings[0].Diagnostics.Reason != pairingDiagnosticDirectMessagesOff {
		t.Fatalf("expected DM-disabled degradation, got %#v", pairings[0])
	}

	client.resetCalls()
	service.relayMessage(dmRelayMessage())
	time.Sleep(20 * time.Millisecond)
	if calls := client.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("expected DM relay to be suppressed, got %#v", calls)
	}
}

func TestRelayRetriesTransientRelayFailureAfterCooldown(t *testing.T) {
	t.Parallel()

	client := &recordingPairingClient{
		network:  compatibleRemoteNetwork("net_b"),
		notify:   make(chan struct{}, 8),
		done:     make(chan struct{}, 8),
		relayErr: errors.New("temporary relay failure"),
	}
	service := newRelayCompatibilityService(client)

	service.relayMessage(roomRelayMessage())
	waitForRelayCalls(t, client, 1)
	waitForRelayDone(t, client)
	pairings := waitForPairingStatus(t, service, "pair_b", protocol.PairingStatusError)
	if pairings[0].Status != protocol.PairingStatusError {
		t.Fatalf("expected transient relay failure to mark pairing error, got %#v", pairings[0])
	}

	client.resetCalls()
	client.relayErr = nil
	service.pairingsMu.Lock()
	status := service.pairingStatuses["pair_b"]
	status.updatedAt = time.Now().UTC().Add(-pairingRelayErrorRetryAfter - time.Second)
	service.pairingStatuses["pair_b"] = status
	service.pairingsMu.Unlock()

	service.relayMessage(roomRelayMessage())
	waitForRelayCalls(t, client, 1)
	waitForRelayDone(t, client)
	pairings = waitForPairingStatus(t, service, "pair_b", protocol.PairingStatusConnected)
	if pairings[0].Status != protocol.PairingStatusConnected {
		t.Fatalf("expected retry to restore pairing, got %#v", pairings[0])
	}
	if calls := snapshotRecordingNetworkCalls(client); len(calls) < 2 {
		t.Fatalf("expected retry to refresh pairing diagnostics, got %#v", calls)
	}
}

func compatibleRemoteNetwork(networkID string) protocol.Network {
	return protocol.Network{
		ID:      networkID,
		Name:    networkID,
		Version: "test",
		Protocols: protocol.NetworkProtocols{
			HTTP: []string{protocol.HTTPProtocolV1},
			Pair: []string{protocol.PairProtocolV1},
		},
		Capabilities: protocol.NetworkCapabilities{
			DirectMessages:    true,
			MessagePagination: "cursor",
		},
	}
}

func newPairingCompatibilityService(client *compatibilityPairingClient) *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		NetworkID: "local",
		Pairings: []protocol.Pairing{{
			ID:              "pair_1",
			RemoteNetworkID: "remote",
			RemoteBaseURL:   "http://remote.example",
			Token:           "pair-secret",
		}},
		Store:         memory,
		Messages:      memory,
		Broker:        events.NewBroker(),
		PairingClient: client,
	})
}

func newRelayCompatibilityService(client *recordingPairingClient) *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		NetworkID: "net_a",
		Pairings: []protocol.Pairing{{
			ID:              "pair_b",
			RemoteNetworkID: "net_b",
			RemoteBaseURL:   "http://remote-b",
		}},
		Store:         memory,
		Messages:      memory,
		Broker:        events.NewBroker(),
		PairingClient: client,
	})
}

func waitForPairingStatus(t *testing.T, service *Service, pairingID string, want string) []protocol.Pairing {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pairings, err := service.ListPairings()
		if err != nil {
			t.Fatalf("ListPairings() error = %v", err)
		}
		for _, pairing := range pairings {
			if pairing.ID == pairingID && pairing.Status == want {
				return pairings
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	t.Fatalf("timed out waiting for pairing %q status %q, got %#v", pairingID, want, pairings)
	return nil
}

func roomRelayMessage() protocol.Message {
	return protocol.Message{
		ID:        "msg_net_a_room",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_room"},
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha", NetworkID: "net_a"},
	}
}

func dmRelayMessage() protocol.Message {
	return protocol.Message{
		ID:        "msg_net_a_dm",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_dm"},
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_alpha_gamma",
			ParticipantIDs: []string{"net_a:alpha", "net_b:gamma"},
		},
		From: protocol.Actor{ID: "alpha", NetworkID: "net_a"},
	}
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (c *compatibilityPairingClient) snapshotNetworkCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.networkCalls
}

func waitForNetworkCalls(t *testing.T, client *recordingPairingClient, want int) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(snapshotRecordingNetworkCalls(client)) >= want {
			return
		}
		select {
		case <-client.notify:
		case <-time.After(10 * time.Millisecond):
		}
	}

	if len(snapshotRecordingNetworkCalls(client)) >= want {
		return
	}
	t.Fatalf("timed out waiting for %d network calls", want)
}

func snapshotRecordingNetworkCalls(client *recordingPairingClient) []protocol.Pairing {
	client.mu.Lock()
	defer client.mu.Unlock()

	return append([]protocol.Pairing(nil), client.networkCalls...)
}
