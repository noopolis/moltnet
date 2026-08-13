package rooms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type participantShapePairingClient struct {
	requests chan protocol.SendMessageRequest
}

func (c *participantShapePairingClient) FetchNetwork(context.Context, protocol.Pairing) (protocol.Network, error) {
	return compatibleRemoteNetwork("remote"), nil
}

func (c *participantShapePairingClient) FetchRooms(context.Context, protocol.Pairing) ([]protocol.Room, error) {
	return nil, nil
}

func (c *participantShapePairingClient) FetchAgents(context.Context, protocol.Pairing) ([]protocol.AgentSummary, error) {
	return nil, nil
}

func (c *participantShapePairingClient) RelayMessage(
	_ context.Context,
	_ protocol.Pairing,
	request protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	c.requests <- request
	return protocol.MessageAccepted{MessageID: request.ID, Accepted: true}, nil
}

func TestDMParticipantByteShapeMatchesStorageEventAndRelay(t *testing.T) {
	tests := []struct {
		name          string
		participants  []string
		expectedJSON  string
		expectRelayed bool
	}{
		{
			name: "all-local aliases",
			participants: []string{
				"molt://local/agents/alpha",
				"local:beta",
			},
			expectedJSON: `["alpha","beta"]`,
		},
		{
			name: "mixed-network participants",
			participants: []string{
				"molt://local/agents/alpha",
				"molt://remote/agents/gamma",
			},
			expectedJSON:  `["local:alpha","remote:gamma"]`,
			expectRelayed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			memory := store.NewMemoryStore()
			pairingClient := &participantShapePairingClient{requests: make(chan protocol.SendMessageRequest, 1)}
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
				Broker:        events.NewBroker(),
				PairingClient: pairingClient,
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream := service.Subscribe(ctx)

			accepted, err := service.SendMessage(protocol.SendMessageRequest{
				ID: "msg-shape",
				Target: protocol.Target{
					Kind:           protocol.TargetKindDM,
					DMID:           "dm-shape",
					ParticipantIDs: test.participants,
				},
				From:  protocol.Actor{Type: "agent", ID: "alpha"},
				Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "shape"}},
			})
			if err != nil || !accepted.Accepted {
				t.Fatalf("SendMessage() = %#v, %v", accepted, err)
			}

			page, err := memory.ListDMMessagesContext(context.Background(), "dm-shape", protocol.PageRequest{Limit: 10})
			if err != nil || len(page.Messages) != 1 {
				t.Fatalf("stored messages = %#v, %v", page, err)
			}
			stored := page.Messages[0]
			eventMessage := waitForParticipantShapeEvent(t, stream, "msg-shape")
			relayRequest, ok := service.relayRequest(service.pairings[0], stored)
			if !ok {
				t.Fatal("canonical relay request was rejected")
			}
			assertParticipantJSON(t, "stored", stored.Target.ParticipantIDs, test.expectedJSON)
			assertParticipantJSON(t, "event", eventMessage.Target.ParticipantIDs, test.expectedJSON)
			assertParticipantJSON(t, "relay", relayRequest.Target.ParticipantIDs, test.expectedJSON)

			if test.expectRelayed {
				select {
				case actual := <-pairingClient.requests:
					assertParticipantJSON(t, "actual relay", actual.Target.ParticipantIDs, test.expectedJSON)
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for relay request")
				}
			} else {
				select {
				case actual := <-pairingClient.requests:
					t.Fatalf("all-local DM was relayed: %#v", actual.Target.ParticipantIDs)
				default:
				}
			}
		})
	}
}

func waitForParticipantShapeEvent(t *testing.T, stream <-chan protocol.Event, messageID string) protocol.Message {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-stream:
			if event.Type == protocol.EventTypeMessageCreated && event.Message != nil && event.Message.ID == messageID {
				return *event.Message
			}
		case <-deadline:
			t.Fatalf("timed out waiting for message event %q", messageID)
		}
	}
}

func assertParticipantJSON(t *testing.T, source string, participants []string, expected string) {
	t.Helper()
	if len(participants) != 2 {
		t.Fatalf("%s participants collapsed: %#v", source, participants)
	}
	for _, participant := range participants {
		if strings.TrimSpace(participant) == "" {
			t.Fatalf("%s contains blank participant: %#v", source, participants)
		}
	}
	encoded, err := json.Marshal(participants)
	if err != nil {
		t.Fatalf("marshal %s participants: %v", source, err)
	}
	if string(encoded) != expected {
		t.Fatalf("%s participant JSON = %s, want %s", source, encoded, expected)
	}
}
