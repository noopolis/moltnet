package rooms

import (
	"context"
	"slices"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestOwnerCanAddressDisconnectedRegisteredAgent(t *testing.T) {
	service := newMentionTestService(nil)
	ctx := context.Background()
	if _, err := service.RegisterAgentContext(ctx, protocol.RegisterAgentRequest{RequestedAgentID: "grok-bot"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, visibility, policy string
		want                   bool
	}{
		{"public-open", protocol.RoomVisibilityPublic, protocol.RoomWritePolicyRegisteredAgents, true},
		{"private-open", protocol.RoomVisibilityPrivate, protocol.RoomWritePolicyRegisteredAgents, false},
		{"public-members", protocol.RoomVisibilityPublic, protocol.RoomWritePolicyMembers, false},
	} {
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: tc.id, Visibility: tc.visibility, WritePolicy: tc.policy, Members: []string{"writer"}}); err != nil {
			t.Fatal(err)
		}
		target := protocol.Target{Kind: protocol.TargetKindRoom, RoomID: tc.id}
		mentions, err := service.resolveMentions(ctx, target, []string{"grok-bot"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string(nil)
		if tc.want {
			want = []string{protocol.AgentFQID("local", "grok-bot")}
		}
		if !slices.Equal(mentions, want) {
			t.Fatalf("%s mentions=%v want=%v", tc.id, mentions, want)
		}
		// A caller without private-roster access cannot use this as a directory.
		limited := authn.WithClaims(authn.WithMode(ctx, authn.ModeOpen), authn.NewAgentTokenClaims("writer", "agent-test"))
		mentions, err = service.resolveMentions(limited, target, []string{"grok-bot"})
		if err != nil {
			t.Fatal(err)
		}
		if len(mentions) != 0 {
			t.Fatal("registered outsider was exposed to agent caller")
		}
	}
}
