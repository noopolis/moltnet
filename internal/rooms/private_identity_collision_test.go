package rooms

import (
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestCanonicalParentIdentityCollisionMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		credentialNetwork string
		credential        string
		parentNetwork     string
		member            string
		want              bool
	}{
		{"local bare room", "local", "luna", "local", "luna", true},
		{"local bare dm", "local", "luna", "local", "luna", true},
		{"remote scoped room", "local", "remote:luna", "remote", "luna", true},
		{"remote fqid dm", "local", "molt://remote/agents/luna", "remote", "luna", true},
		{"remote scoped member", "local", "remote:luna", "remote", "remote:luna", true},
		{"bare local versus remote bare", "local", "luna", "remote", "luna", false},
		{"bare local versus remote scoped", "local", "luna", "remote", "remote:luna", false},
		{"bare local versus remote fqid", "local", "luna", "remote", "molt://remote/agents/luna", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AgentIdentityMatches(test.credentialNetwork, test.credential, test.parentNetwork, test.member); got != test.want {
				t.Fatalf("AgentIdentityMatches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAuthoritativeEventNetworkBindsRoomThreadAndDMMessage(t *testing.T) {
	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "collision-room", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindThread, RoomID: "collision-room", ThreadID: "collision-thread", ParentMessageID: "parent"}, From: protocol.Actor{ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "thread"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "collision-dm", ParticipantIDs: []string{"luna", "mira"}}, From: protocol.Actor{ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "dm"}}}); err != nil {
		t.Fatal(err)
	}
	ctx := privateVisibilityContext("luna", authn.ScopeObserve)
	for _, target := range []protocol.Target{
		{Kind: protocol.TargetKindRoom, RoomID: "collision-room"},
		{Kind: protocol.TargetKindDM, DMID: "collision-dm", ParticipantIDs: []string{"luna", "mira"}},
	} {
		relayed := protocol.Event{Type: protocol.EventTypeMessageCreated, NetworkID: "local", Message: &protocol.Message{
			NetworkID: "local", Origin: protocol.MessageOrigin{NetworkID: "remote"}, From: protocol.Actor{ID: "remote-agent", NetworkID: "remote"}, Target: target,
		}}
		if !service.eventVisible(ctx, relayed) {
			t.Fatalf("valid cross-network relay was hidden for target %s", target.Kind)
		}
	}
	for _, test := range []struct {
		name   string
		target protocol.Target
		outer  string
		nested string
		want   bool
	}{
		{"room matching labels", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "collision-room"}, "local", "local", true},
		{"room remote outer local message", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "collision-room"}, "remote", "local", false},
		{"room local outer remote message", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "collision-room"}, "local", "remote", false},
		{"room missing outer matching nested", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "collision-room"}, "", "local", true},
		{"thread resolved through room", protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "collision-thread", RoomID: "collision-room"}, "local", "local", true},
		{"dm matching labels", protocol.Target{Kind: protocol.TargetKindDM, DMID: "collision-dm", ParticipantIDs: []string{"luna", "mira"}}, "local", "local", true},
		{"dm remote outer local message", protocol.Target{Kind: protocol.TargetKindDM, DMID: "collision-dm", ParticipantIDs: []string{"luna", "mira"}}, "remote", "local", false},
		{"dm local outer remote message", protocol.Target{Kind: protocol.TargetKindDM, DMID: "collision-dm", ParticipantIDs: []string{"luna", "mira"}}, "local", "remote", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := protocol.Event{Type: protocol.EventTypeMessageCreated, NetworkID: test.outer, Message: &protocol.Message{NetworkID: test.nested, Target: test.target, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "private"}}}}
			if got := service.eventVisible(ctx, event); got != test.want {
				t.Fatalf("eventVisible() = %v, want %v", got, test.want)
			}
			if !test.want && event.Message.Parts[0].Text != "private" {
				t.Fatal("test payload was mutated while denying event")
			}
		})
	}
}
