package bridge

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestOwnerWakeRouting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from     protocol.Actor
		mentions []string
		origin   string
		want     bool
	}{
		{"human broadcast", protocol.Actor{Type: "human", ID: "operator"}, nil, "", true},
		{"human addressed here", protocol.Actor{Type: "human", ID: "operator"}, []string{"molt://local/agents/grok-bot"}, "", true},
		{"human addressed elsewhere", protocol.Actor{Type: "human", ID: "operator"}, []string{"other"}, "", false},
		{"agent reply", protocol.Actor{Type: "agent", ID: "other"}, nil, "", false},
		{"agent mention", protocol.Actor{Type: "agent", ID: "other"}, []string{"grok-bot"}, "", false},
		{"agent with owner name", protocol.Actor{Type: "agent", ID: "operator", Name: "我"}, nil, "", false},
		{"another human", protocol.Actor{Type: "human", ID: "other"}, nil, "", false},
		{"remote human", protocol.Actor{Type: "human", ID: "operator", NetworkID: "remote"}, nil, "remote", false},
		{"remote origin", protocol.Actor{Type: "human", ID: "operator"}, nil, "remote", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := &protocol.Message{NetworkID: "local", From: tc.from, Mentions: tc.mentions, Origin: protocol.MessageOrigin{NetworkID: tc.origin}}
			if got := ShouldWakeMessage(bridgeconfig.WakeOwner, message, "local", bridgeconfig.AgentConfig{ID: "grok-bot"}); got != tc.want {
				t.Fatalf("wake=%v want %v", got, tc.want)
			}
		})
	}
}
