package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type dmTopologyTestStore interface {
	ContextLifecycleMessageStore
	ContextMessageStore
}

func TestDMTopologyIsImmutableAcrossStoreBackends(t *testing.T) {
	for _, backend := range dmTopologyStoreFactories() {
		t.Run(backend.name, func(t *testing.T) {
			messages, cleanup := backend.open(t)
			defer cleanup()

			first := dmTopologyMessage("msg-first", "dm-fixed", "local", []string{"beta", "alpha"}, protocol.Actor{ID: "alpha"})
			first.Parts = []protocol.Part{{Kind: protocol.PartKindFile, Filename: "private.txt"}}
			lifecycle, err := messages.AppendMessageWithLifecycleContext(context.Background(), first)
			if err != nil || lifecycle.DM == nil {
				t.Fatalf("first append = %#v, %v", lifecycle, err)
			}

			reordered := dmTopologyMessage("msg-reordered", "dm-fixed", "local", []string{"alpha", "beta"}, protocol.Actor{ID: "beta"})
			if _, err := messages.AppendMessageWithLifecycleContext(context.Background(), reordered); err != nil {
				t.Fatalf("same-set reordered append: %v", err)
			}

			for _, test := range []struct {
				name    string
				message protocol.Message
			}{
				{"missing sender", dmTopologyMessage("msg-missing", "dm-fixed", "local", []string{"beta", "gamma"}, protocol.Actor{ID: "alpha"})},
				{"different set", dmTopologyMessage("msg-set", "dm-fixed", "local", []string{"alpha", "gamma"}, protocol.Actor{ID: "alpha"})},
				{"different network", dmTopologyMessage("msg-network", "dm-fixed", "remote", []string{"alpha", "beta"}, protocol.Actor{ID: "alpha"})},
				{"self only", dmTopologyMessage("msg-self", "dm-self", "local", []string{"alpha"}, protocol.Actor{ID: "alpha"})},
				{"two aliases collapse", dmTopologyMessage("msg-collapse", "dm-collapse", "local", []string{"alpha", "local:alpha"}, protocol.Actor{ID: "alpha"})},
				{"alias duplicate among peers", dmTopologyMessage("msg-alias", "dm-alias", "local", []string{"alpha", "local:alpha", "beta"}, protocol.Actor{ID: "alpha"})},
				{"forged actor network", dmTopologyMessage("msg-forged-network", "dm-forged-network", "local", []string{"local:alpha", "remote:beta"}, protocol.Actor{ID: "local:alpha", NetworkID: "remote"})},
				{"forged actor fqid", dmTopologyMessage("msg-forged-fqid", "dm-forged-fqid", "local", []string{"local:alpha", "remote:beta"}, protocol.Actor{ID: "alpha", NetworkID: "local", FQID: protocol.AgentFQID("remote", "alpha")})},
			} {
				t.Run(test.name, func(t *testing.T) {
					if _, err := messages.AppendMessageWithLifecycleContext(context.Background(), test.message); !errors.Is(err, ErrDMTopologyConflict) {
						t.Fatalf("append error = %v, want ErrDMTopologyConflict", err)
					}
				})
			}

			duplicateConflict := dmTopologyMessage("msg-first", "dm-fixed", "local", []string{"alpha", "gamma"}, protocol.Actor{ID: "alpha"})
			if _, err := messages.AppendMessageWithLifecycleContext(context.Background(), duplicateConflict); !errors.Is(err, ErrDuplicateMessage) {
				t.Fatalf("duplicate error = %v, want ErrDuplicateMessage", err)
			}

			assertDMTopologyState(t, messages, "dm-fixed", []string{"alpha", "beta"}, 2, 1)
		})
	}
}

func TestDMTopologyPreservesMixedNetworkRelaySenders(t *testing.T) {
	for _, backend := range dmTopologyStoreFactories() {
		t.Run(backend.name, func(t *testing.T) {
			messages, cleanup := backend.open(t)
			defer cleanup()

			message := dmTopologyMessage(
				"msg-relay",
				"dm-relay",
				"local",
				[]string{"local:alpha", "remote:gamma"},
				protocol.Actor{ID: "gamma", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "gamma")},
			)
			if _, err := messages.AppendMessageWithLifecycleContext(context.Background(), message); err != nil {
				t.Fatalf("mixed-network relay append: %v", err)
			}
			assertDMTopologyState(t, messages, "dm-relay", []string{"local:alpha", "remote:gamma"}, 1, 0)
		})
	}
}

func TestConcurrentDMTopologyClaimsNeverUnionParticipants(t *testing.T) {
	for _, backend := range dmTopologyStoreFactories() {
		t.Run(backend.name, func(t *testing.T) {
			messages, cleanup := backend.open(t)
			defer cleanup()

			const attempts = 20
			start := make(chan struct{})
			errorsByAttempt := make([]error, attempts)
			var wait sync.WaitGroup
			for index := 0; index < attempts; index++ {
				index := index
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					participants := []string{"alpha", "beta"}
					if index%2 == 1 {
						participants = []string{"alpha", "gamma"}
					}
					_, errorsByAttempt[index] = messages.AppendMessageWithLifecycleContext(
						context.Background(),
						dmTopologyMessage(fmt.Sprintf("msg-%02d", index), "dm-race", "local", participants, protocol.Actor{ID: "alpha"}),
					)
				}()
			}
			close(start)
			wait.Wait()

			successes := 0
			conflicts := 0
			for _, err := range errorsByAttempt {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, ErrDMTopologyConflict):
					conflicts++
				default:
					t.Fatalf("unexpected concurrent append error: %v", err)
				}
			}
			if successes == 0 || conflicts == 0 || successes+conflicts != attempts {
				t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
			}

			conversation, ok, err := messages.GetDirectConversationContext(context.Background(), "dm-race")
			if err != nil || !ok || len(conversation.ParticipantIDs) != 2 {
				t.Fatalf("conversation = %#v, %v, %v", conversation, ok, err)
			}
			page, err := messages.ListDMMessagesContext(context.Background(), "dm-race", protocol.PageRequest{Limit: attempts + 1})
			if err != nil || len(page.Messages) != successes {
				t.Fatalf("messages = %#v, %v", page, err)
			}
			for _, message := range page.Messages {
				if !sameStrings(message.Target.ParticipantIDs, conversation.ParticipantIDs) {
					t.Fatalf("message topology %#v differs from frozen %#v", message.Target.ParticipantIDs, conversation.ParticipantIDs)
				}
			}
		})
	}
}

type dmTopologyStoreFactory struct {
	name string
	open func(*testing.T) (dmTopologyTestStore, func())
}

func dmTopologyStoreFactories() []dmTopologyStoreFactory {
	return []dmTopologyStoreFactory{
		{name: "memory", open: func(t *testing.T) (dmTopologyTestStore, func()) {
			return NewMemoryStore(), func() {}
		}},
		{name: "sqlite", open: func(t *testing.T) (dmTopologyTestStore, func()) {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
			if err != nil {
				t.Fatalf("NewSQLiteStore(): %v", err)
			}
			return store, func() { _ = store.Close() }
		}},
	}
}

func dmTopologyMessage(id, dmID, networkID string, participants []string, from protocol.Actor) protocol.Message {
	return protocol.Message{
		ID:        id,
		NetworkID: networkID,
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           dmID,
			ParticipantIDs: participants,
		},
		From:      from,
		Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: id}},
		CreatedAt: time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
	}
}

func assertDMTopologyState(
	t *testing.T,
	messages dmTopologyTestStore,
	dmID string,
	participants []string,
	messageCount int,
	artifactCount int,
) {
	t.Helper()
	conversation, ok, err := messages.GetDirectConversationContext(context.Background(), dmID)
	if err != nil || !ok || !sameStrings(conversation.ParticipantIDs, participants) || conversation.MessageCount != messageCount {
		t.Fatalf("conversation = %#v, %v, %v", conversation, ok, err)
	}
	page, err := messages.ListDMMessagesContext(context.Background(), dmID, protocol.PageRequest{Limit: 100})
	if err != nil || len(page.Messages) != messageCount {
		t.Fatalf("message page = %#v, %v", page, err)
	}
	artifacts, err := messages.ListArtifactsContext(context.Background(), protocol.ArtifactFilter{DMID: dmID}, protocol.PageRequest{Limit: 100})
	if err != nil || len(artifacts.Artifacts) != artifactCount {
		t.Fatalf("artifact page = %#v, %v", artifacts, err)
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}
