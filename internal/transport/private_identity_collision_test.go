package transport

import (
	"context"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAttachmentCanonicalParentIdentityCollisionMatrix(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		network: protocol.Network{ID: "local"},
		rooms:   []protocol.Room{{ID: "local-room", NetworkID: "local", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}, {ID: "remote-room", NetworkID: "remote", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}},
		dms:     []protocol.DirectConversation{{ID: "local-dm", NetworkID: "local", ParticipantIDs: []string{"luna", "mira"}}, {ID: "remote-dm", NetworkID: "remote", ParticipantIDs: []string{"luna", "mira"}}},
	}
	ctx := context.Background()
	if !attachedRoom(&service.rooms[0], "local", "luna") {
		t.Fatal("local bare attachment missed local authoritative room")
	}
	if !participantsIncludeAttachedAgentInNetwork(service.dms[0].ParticipantIDs, "local", "local", "luna") {
		t.Fatal("local bare attachment missed local authoritative DM")
	}
	tests := []struct {
		name       string
		credential string
		member     string
		want       bool
	}{
		{"local bare room", "luna", "luna", false},
		{"remote scoped room", "remote:luna", "luna", true},
		{"remote fqid room", "molt://remote/agents/luna", "luna", true},
		{"bare local against remote scoped room", "luna", "remote:luna", false},
		{"bare local against remote bare room", "luna", "luna", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			room := service.rooms[1]
			room.Members = []string{test.member}
			if got := rooms.AgentIdentityMatches("local", test.credential, room.NetworkID, room.Members[0]); got != test.want {
				t.Fatalf("room identity match = %v, want %v", got, test.want)
			}
			if got := attachedRoom(&room, "local", test.credential); got != test.want {
				t.Fatalf("attachedRoom() = %v, want %v", got, test.want)
			}
		})
	}
	dmTests := []struct {
		name       string
		credential string
		want       bool
	}{
		{"remote scoped dm", "remote:luna", true},
		{"remote fqid dm", "molt://remote/agents/luna", true},
		{"bare local dm collision", "luna", false},
	}
	for _, test := range dmTests {
		t.Run(test.name, func(t *testing.T) {
			event := protocol.Event{Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{
				NetworkID: "remote",
				Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "remote-dm", ParticipantIDs: []string{"luna", "mira"}},
			}}
			if got := attachedAgentEvent(ctx, service, event, "local", test.credential); got != test.want {
				t.Fatalf("attached DM event = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAttachmentEventNetworkBindsEveryMessageParent(t *testing.T) {
	service := &fakeService{
		network: protocol.Network{ID: "local"},
		rooms:   []protocol.Room{{ID: "room", NetworkID: "local", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}, {ID: "public", NetworkID: "local", Visibility: protocol.RoomVisibilityPublic}},
		threads: []protocol.Thread{{ID: "thread", NetworkID: "local", RoomID: "room", ParentMessageID: "parent"}},
		dms:     []protocol.DirectConversation{{ID: "dm", NetworkID: "local", ParticipantIDs: []string{"luna", "mira"}}},
	}
	ctx := context.Background()
	for _, eventType := range []string{protocol.EventTypeRoomCreated, protocol.EventTypeRoomRemoved, protocol.EventTypeRoomMembersUpdated} {
		payload := service.rooms[0]
		event := protocol.Event{Type: eventType, NetworkID: "remote", Room: &payload}
		if attachedAgentEvent(ctx, service, event, "local", "luna") {
			t.Fatalf("%s accepted a relabelled outer network", eventType)
		}
		event.NetworkID = "local"
		if !attachedAgentEvent(ctx, service, event, "local", "luna") {
			t.Fatalf("%s rejected its authoritative outer network", eventType)
		}
	}
	for _, target := range []protocol.Target{
		{Kind: protocol.TargetKindRoom, RoomID: "room"},
		{Kind: protocol.TargetKindDM, DMID: "dm", ParticipantIDs: []string{"luna", "mira"}},
	} {
		relayed := protocol.Event{Type: protocol.EventTypeMessageCreated, NetworkID: "local", Message: &protocol.Message{
			NetworkID: "local", Origin: protocol.MessageOrigin{NetworkID: "remote"}, From: protocol.Actor{ID: "remote-agent", NetworkID: "remote"}, Target: target,
		}}
		if !attachedAgentEvent(ctx, service, relayed, "local", "luna") {
			t.Fatalf("valid cross-network relay was hidden for target %s", target.Kind)
		}
	}
	policy := mustBearerPolicy(t, authn.TokenConfig{ID: "attach", Value: "secret", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}})
	filter := attachmentEventFilter(policy, ctx, service, "local", "luna")
	publicRelabelled := protocol.Event{Type: protocol.EventTypeMessageCreated, NetworkID: "remote", Message: &protocol.Message{NetworkID: "local", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}}}
	if filter(ctx, publicRelabelled) {
		t.Fatal("public attachment path accepted a relabelled room message")
	}
	for _, test := range []struct {
		name   string
		target protocol.Target
		outer  string
		nested string
		want   bool
	}{
		{"room matching", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "room"}, "local", "local", true},
		{"room remote outer", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "room"}, "remote", "local", false},
		{"room remote nested", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "room"}, "local", "remote", false},
		{"room empty outer legacy", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "room"}, "", "local", true},
		{"thread resolved through room", protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thread", RoomID: "room"}, "local", "local", true},
		{"dm matching", protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm", ParticipantIDs: []string{"luna", "mira"}}, "local", "local", true},
		{"dm conflicting labels", protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm", ParticipantIDs: []string{"luna", "mira"}}, "remote", "local", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := protocol.Event{Type: protocol.EventTypeMessageCreated, NetworkID: test.outer, Message: &protocol.Message{NetworkID: test.nested, Target: test.target}}
			if got := attachedAgentEvent(ctx, service, event, "local", "luna"); got != test.want {
				t.Fatalf("attachedAgentEvent() = %v, want %v", got, test.want)
			}
		})
	}
}
