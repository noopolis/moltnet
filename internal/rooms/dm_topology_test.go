package rooms

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type dmTopologyEventBroker struct {
	mu     sync.Mutex
	events []protocol.Event
}

func (b *dmTopologyEventBroker) Publish(event protocol.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

func (b *dmTopologyEventBroker) Subscribe(context.Context) <-chan protocol.Event {
	return make(chan protocol.Event)
}

func (b *dmTopologyEventBroker) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func TestDMTopologyConflictCannotGrantHistoryOrArtifactAccess(t *testing.T) {
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})

	seed, err := service.SendMessage(protocol.SendMessageRequest{
		ID: "msg-seed",
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm-private",
			ParticipantIDs: []string{"alpha", "beta"},
		},
		From:  protocol.Actor{Type: "human", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "private.txt"}},
	})
	if err != nil || !seed.DMCreated {
		t.Fatalf("seed send = %#v, %v", seed, err)
	}

	attack, err := service.SendMessage(protocol.SendMessageRequest{
		ID: "msg-seed",
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm-private",
			ParticipantIDs: []string{"beta", "gamma"},
		},
		From:  protocol.Actor{Type: "human", ID: "gamma"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "reuse"}},
	})
	if attack.Accepted || !errors.Is(err, ErrDMTopologyConflict) || err.Error() != ErrDMTopologyConflict.Error() {
		t.Fatalf("attack send = %#v, %v", attack, err)
	}

	restricted := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:     "gamma-observer",
		Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin},
		Agents: []string{"gamma"},
	}))
	if page, err := service.ListDirectConversationsContext(restricted, protocol.PageRequest{Limit: 10}); err != nil || len(page.DMs) != 0 {
		t.Fatalf("attacker DM list = %#v, %v", page, err)
	}
	if page, err := service.ListDMMessagesContext(restricted, "dm-private", protocol.PageRequest{Limit: 10}); !errors.Is(err, ErrAgentForbidden) || len(page.Messages) != 0 {
		t.Fatalf("attacker history = %#v, %v", page, err)
	}
	if page, err := service.ListArtifactsContext(restricted, protocol.ArtifactFilter{DMID: "dm-private"}, protocol.PageRequest{Limit: 10}); !errors.Is(err, ErrAgentForbidden) || len(page.Artifacts) != 0 {
		t.Fatalf("attacker artifacts = %#v, %v", page, err)
	}

	legitimate := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:     "beta-observer",
		Scopes: []authn.Scope{authn.ScopeObserve},
		Agents: []string{"beta"},
	}))
	if page, err := service.ListDMMessagesContext(legitimate, "dm-private", protocol.PageRequest{Limit: 10}); err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "msg-seed" {
		t.Fatalf("legitimate history = %#v, %v", page, err)
	}
	if page, err := service.ListArtifactsContext(legitimate, protocol.ArtifactFilter{DMID: "dm-private"}, protocol.PageRequest{Limit: 10}); err != nil || len(page.Artifacts) != 1 || page.Artifacts[0].Filename != "private.txt" {
		t.Fatalf("legitimate artifacts = %#v, %v", page, err)
	}
}

func TestDuplicateDMTopologyConflictHasNoServiceSideEffects(t *testing.T) {
	memory := store.NewMemoryStore()
	broker := &dmTopologyEventBroker{}
	relay := &recordingPairingClient{done: make(chan struct{}, 2)}
	causal := &bytes.Buffer{}
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		Pairings: []protocol.Pairing{{
			ID:              "pair_remote",
			RemoteNetworkID: "remote",
			RemoteBaseURL:   "http://remote.example",
			Status:          protocol.PairingStatusConnected,
		}},
		Store:         memory,
		Messages:      memory,
		Broker:        broker,
		PairingClient: relay,
		CausalWriter:  observability.NewCausalWriter(causal),
	})
	defer service.Close()

	seedRequest := protocol.SendMessageRequest{
		ID: "msg-duplicate-topology",
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm-duplicate-topology",
			ParticipantIDs: []string{"local:alpha", "remote:gamma"},
		},
		From:  protocol.Actor{Type: "human", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "private.txt"}},
	}
	seed, err := service.SendMessage(seedRequest)
	if err != nil || !seed.Accepted || !seed.DMCreated {
		t.Fatalf("seed send = %#v, %v", seed, err)
	}
	waitForRelayCalls(t, relay, 1)
	waitForRelayDone(t, relay)
	publishedAfterSeed := broker.count()

	matching := seedRequest
	matching.Target.ParticipantIDs = []string{"remote:gamma", "local:alpha"}
	matching.Parts = []protocol.Part{{Kind: protocol.PartKindText, Text: "retry"}}
	retry, err := service.SendMessage(matching)
	if err != nil || !retry.Accepted || retry.DMCreated || retry.EventID != seed.EventID {
		t.Fatalf("matching duplicate = %#v, %v", retry, err)
	}

	conflict := seedRequest
	conflict.Target.ParticipantIDs = []string{"local:alpha", "remote:delta"}
	conflict.Parts = []protocol.Part{{Kind: protocol.PartKindFile, Filename: "stolen.txt"}}
	rejected, err := service.SendMessage(conflict)
	if rejected.Accepted || !errors.Is(err, ErrDMTopologyConflict) || err.Error() != ErrDMTopologyConflict.Error() {
		t.Fatalf("conflicting duplicate = %#v, %v", rejected, err)
	}

	conversation, ok, err := memory.GetDirectConversationContext(context.Background(), seedRequest.Target.DMID)
	if err != nil || !ok || conversation.MessageCount != 1 || !sameStrings(conversation.ParticipantIDs, []string{"local:alpha", "remote:gamma"}) {
		t.Fatalf("conversation changed = %#v, %v, %v", conversation, ok, err)
	}
	messages, err := memory.ListDMMessagesContext(context.Background(), seedRequest.Target.DMID, protocol.PageRequest{Limit: 10})
	if err != nil || len(messages.Messages) != 1 || messages.Messages[0].ID != seedRequest.ID || messages.Messages[0].Parts[0].Filename != "private.txt" {
		t.Fatalf("messages changed = %#v, %v", messages, err)
	}
	artifacts, err := memory.ListArtifactsContext(context.Background(), protocol.ArtifactFilter{DMID: seedRequest.Target.DMID}, protocol.PageRequest{Limit: 10})
	if err != nil || len(artifacts.Artifacts) != 1 || artifacts.Artifacts[0].Filename != "private.txt" {
		t.Fatalf("artifacts changed = %#v, %v", artifacts, err)
	}
	if broker.count() != publishedAfterSeed {
		t.Fatalf("published event count changed from %d to %d", publishedAfterSeed, broker.count())
	}
	recorded := causalLines(t, causal)
	if len(recorded) != 1 || recorded[0].Type != protocol.EventTypeMessageAccepted {
		t.Fatalf("causal stamps = %#v, want one seed acceptance and no duplicate acceptance/denial", recorded)
	}
	if calls := relay.snapshotCalls(); len(calls) != 1 {
		t.Fatalf("relay calls = %#v, want seed only", calls)
	}
}

func TestDMSenderMustBeInCanonicalParticipantSet(t *testing.T) {
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})

	for _, participants := range [][]string{
		{"alpha", "beta"},
		{"gamma", "local:gamma"},
		{"gamma", "local:gamma", "beta"},
	} {
		accepted, err := service.SendMessage(protocol.SendMessageRequest{
			Target: protocol.Target{
				Kind:           protocol.TargetKindDM,
				DMID:           "dm-invalid",
				ParticipantIDs: participants,
			},
			From:  protocol.Actor{Type: "human", ID: "gamma"},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "invalid"}},
		})
		if accepted.Accepted || !errors.Is(err, ErrDMTopologyConflict) {
			t.Fatalf("participants %#v accepted=%#v err=%v", participants, accepted, err)
		}
	}
	if _, ok, err := memory.GetDirectConversationContext(context.Background(), "dm-invalid"); err != nil || ok {
		t.Fatalf("invalid topology created a conversation: ok=%v err=%v", ok, err)
	}
}
