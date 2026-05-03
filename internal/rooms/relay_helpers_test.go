package rooms

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type recordingPairingClient struct {
	mu     sync.Mutex
	calls  []protocol.Pairing
	err    error
	notify chan struct{}
	block  bool
	done   chan struct{}
}

func (r *recordingPairingClient) FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error) {
	return protocol.Network{}, r.err
}

func (r *recordingPairingClient) FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error) {
	return nil, r.err
}

func (r *recordingPairingClient) FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error) {
	return nil, r.err
}

func (r *recordingPairingClient) RelayMessage(
	ctx context.Context,
	pairing protocol.Pairing,
	request protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	if r.done != nil {
		defer func() {
			select {
			case r.done <- struct{}{}:
			default:
			}
		}()
	}
	r.mu.Lock()
	r.calls = append(r.calls, pairing)
	r.mu.Unlock()
	if r.notify != nil {
		select {
		case r.notify <- struct{}{}:
		default:
		}
	}
	if r.err != nil {
		return protocol.MessageAccepted{}, r.err
	}
	if r.block {
		<-ctx.Done()
		return protocol.MessageAccepted{}, ctx.Err()
	}
	return protocol.MessageAccepted{
		MessageID: request.ID,
		Accepted:  true,
	}, nil
}

func TestRelayHelpers(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{
		NetworkID: "net_a",
		Store:     store.NewMemoryStore(),
		Messages:  store.NewMemoryStore(),
		Broker:    events.NewBroker(),
	})

	origin := service.normalizeOrigin(protocol.MessageOrigin{}, "msg_net_a_1")
	if origin.NetworkID != "net_a" || origin.MessageID != "msg_net_a_1" {
		t.Fatalf("unexpected normalized origin %#v", origin)
	}

	target := service.normalizeTarget(protocol.Target{
		Kind: protocol.TargetKindDM,
		DMID: "dm_1",
		ParticipantIDs: []string{
			"alpha",
			"net_b:gamma",
			"molt://net_b/agents/gamma",
		},
	}, protocol.Actor{ID: "alpha", NetworkID: "net_a"})
	if len(target.ParticipantIDs) != 2 || target.ParticipantIDs[0] != "net_a:alpha" || target.ParticipantIDs[1] != "net_b:gamma" {
		t.Fatalf("unexpected normalized target %#v", target)
	}

	if !hasScopedParticipant(target.ParticipantIDs) {
		t.Fatal("expected scoped participants to be detected")
	}
	if normalizeParticipantID("plain") != "" {
		t.Fatal("expected plain participant to stay unresolved")
	}
	if normalizeParticipantID("molt://net_b/agents/gamma") != "net_b:gamma" {
		t.Fatal("expected fqid participant normalization")
	}
	if sanitizeIDComponent("net a/demo:one") != "net_a_demo_one" {
		t.Fatal("unexpected sanitized id component")
	}
}

func TestRelayMessageSelection(t *testing.T) {
	t.Parallel()

	client := &recordingPairingClient{
		notify: make(chan struct{}, 8),
		done:   make(chan struct{}, 8),
	}
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		NetworkID: "net_a",
		Pairings: []protocol.Pairing{
			{ID: "pair_b", RemoteNetworkID: "net_b", RemoteBaseURL: "http://remote-b", Token: "pair-secret", Status: "connected"},
			{ID: "pair_c", RemoteNetworkID: "net_c", RemoteBaseURL: "http://remote-c", Status: "disconnected"},
			{ID: "pair_d", RemoteNetworkID: "net_d"},
		},
		Store:         memory,
		Messages:      memory,
		Broker:        events.NewBroker(),
		PairingClient: client,
	})

	roomMessage := protocol.Message{
		ID:        "msg_net_a_1",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_1"},
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha", NetworkID: "net_a"},
	}
	if !service.shouldRelayMessage(roomMessage) {
		t.Fatal("expected local-origin room message to relay")
	}
	service.relayMessage(roomMessage)
	waitForRelayCalls(t, client, 1)
	waitForRelayDone(t, client)
	calls := client.snapshotCalls()
	if len(calls) != 1 || calls[0].ID != "pair_b" {
		t.Fatalf("unexpected relay calls %#v", calls)
	}
	if calls[0].Token != "pair-secret" {
		t.Fatalf("expected relay to preserve pairing token, got %#v", calls[0])
	}
	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if len(pairings) == 0 || pairings[0].Token != "" {
		t.Fatalf("expected public pairing list to redact tokens, got %#v", pairings)
	}

	client.resetCalls()
	dmMessage := protocol.Message{
		ID:        "msg_net_a_2",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_2"},
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_alpha_gamma",
			ParticipantIDs: []string{"net_a:alpha", "net_b:gamma"},
		},
		From: protocol.Actor{ID: "alpha", NetworkID: "net_a"},
	}
	if !service.shouldRelayToPairing(service.pairings[0], dmMessage) {
		t.Fatal("expected matching dm pairing")
	}
	service.setPairingStatus("pair_b", "error")
	pairings, err = service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if service.shouldRelayToPairing(pairings[0], dmMessage) {
		t.Fatal("expected live error pairing status to suppress relay")
	}
	service.setPairingStatus("pair_b", "connected")
	if service.shouldRelayToPairing(protocol.Pairing{ID: "pair_c", RemoteNetworkID: "net_c", RemoteBaseURL: "http://remote-c", Status: "connected"}, dmMessage) {
		t.Fatal("expected unrelated dm pairing to be skipped")
	}
	service.pairingsMu.Lock()
	service.pairingStatuses["pair_b"] = pairingStatus{
		value:     "error",
		updatedAt: time.Now().UTC().Add(-pairingRelayErrorRetryAfter - time.Second),
	}
	service.pairingsMu.Unlock()
	if !service.shouldRelayToPairing(protocol.Pairing{ID: "pair_b", RemoteNetworkID: "net_b", RemoteBaseURL: "http://remote-b", Status: "connected"}, dmMessage) {
		t.Fatal("expected cooled-down error pairing status to allow retry")
	}
	service.setPairingStatus("pair_b", "connected")
	service.relayMessage(dmMessage)
	waitForRelayCalls(t, client, 1)
	waitForRelayDone(t, client)
	calls = client.snapshotCalls()
	if len(calls) != 1 || calls[0].ID != "pair_b" {
		t.Fatalf("unexpected dm relay calls %#v", calls)
	}

	if request, ok := service.relayRequest(service.pairings[0], dmMessage); !ok || request.Target.ParticipantIDs[0] != "net_a:alpha" || request.Target.ParticipantIDs[1] != "net_b:gamma" {
		t.Fatalf("unexpected relay request %#v %v", request, ok)
	}

	normalized := relayParticipantIDs(service.pairings[0], protocol.Message{
		From: protocol.Actor{ID: "alpha", NetworkID: "net_a"},
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			ParticipantIDs: []string{"alpha", "gamma", "net_b:gamma", "molt://net_b/agents/gamma"},
		},
	})
	if len(normalized) != 2 || normalized[0] != "net_a:alpha" || normalized[1] != "net_b:gamma" {
		t.Fatalf("unexpected normalized relay participants %#v", normalized)
	}
	if _, ok := service.relayRequest(service.pairings[0], protocol.Message{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			ParticipantIDs: []string{"alpha"},
		},
	}); ok {
		t.Fatal("expected relay request to skip DM with fewer than two participants")
	}

	client.resetCalls()
	service.relayMessage(protocol.Message{
		ID:        "msg_net_a_3",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_b", MessageID: "msg_net_b_3"},
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "beta", NetworkID: "net_b"},
	})
	calls = client.snapshotCalls()
	if len(calls) != 0 {
		t.Fatalf("expected remote-origin message to skip relay, got %#v", calls)
	}
}

func TestRelayMessageDropsWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	client := &recordingPairingClient{notify: make(chan struct{}, 1)}
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		NetworkID: "net_a",
		Pairings: []protocol.Pairing{
			{ID: "pair_b", RemoteNetworkID: "net_b", RemoteBaseURL: "http://remote-b", Status: "connected"},
		},
		Store:         memory,
		Messages:      memory,
		Broker:        events.NewBroker(),
		PairingClient: client,
	})

	for index := 0; index < cap(service.relaySlots); index++ {
		service.relaySlots <- struct{}{}
	}

	service.relayMessage(protocol.Message{
		ID:        "msg_net_a_drop",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_drop"},
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha", NetworkID: "net_a"},
	})

	if calls := client.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("expected full relay queue to skip calls, got %#v", calls)
	}
}

func TestRelayMessageStopsWhenServiceCloses(t *testing.T) {
	t.Parallel()

	client := &recordingPairingClient{
		notify: make(chan struct{}, 1),
		done:   make(chan struct{}, 1),
		block:  true,
	}
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		NetworkID: "net_a",
		Pairings: []protocol.Pairing{
			{ID: "pair_b", RemoteNetworkID: "net_b", RemoteBaseURL: "http://remote-b", Status: "connected"},
		},
		Store:         memory,
		Messages:      memory,
		Broker:        events.NewBroker(),
		PairingClient: client,
	})

	service.relayMessage(protocol.Message{
		ID:        "msg_net_a_close",
		NetworkID: "net_a",
		Origin:    protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_close"},
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha", NetworkID: "net_a"},
	})
	waitForRelayCalls(t, client, 1)

	if err := service.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-client.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay context cancellation")
	}
}

func waitForRelayCalls(t *testing.T, client *recordingPairingClient, want int) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(client.snapshotCalls()) >= want {
			return
		}
		select {
		case <-client.notify:
		case <-time.After(10 * time.Millisecond):
		}
	}

	if len(client.snapshotCalls()) >= want {
		return
	}
	t.Fatalf("timed out waiting for %d relay calls", want)
}

func waitForRelayDone(t *testing.T, client *recordingPairingClient) {
	t.Helper()

	select {
	case <-client.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay completion")
	}
}

func (r *recordingPairingClient) snapshotCalls() []protocol.Pairing {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]protocol.Pairing(nil), r.calls...)
}

func (r *recordingPairingClient) resetCalls() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = nil
}
