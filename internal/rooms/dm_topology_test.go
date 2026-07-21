package rooms

import (
	"context"
	"errors"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

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
		ID: "msg-attack",
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
