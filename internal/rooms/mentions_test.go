package rooms

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSendMessageCanonicalizesRoomMentions(t *testing.T) {
	t.Parallel()

	service := newMentionTestService([]protocol.Pairing{{
		ID:                "remote",
		RemoteNetworkID:   "remote_net",
		RemoteNetworkName: "Remote",
	}})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "research",
		Members: []string{
			"director",
			"penny",
			"remote_net:director",
			"molt://remote_net/agents/howard",
		},
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		id       string
		text     string
		explicit []string
		want     []string
	}{
		{
			id:   "msg_short_mention",
			text: "@penny, your turn.",
			want: []string{protocol.AgentFQID("local", "penny")},
		},
		{
			id:   "msg_scoped_mention",
			text: "@remote:director, can you review this?",
			want: []string{protocol.AgentFQID("remote_net", "director")},
		},
		{
			id:   "msg_canonical_mention",
			text: "loop in <@molt://remote_net/agents/howard>",
			want: []string{protocol.AgentFQID("remote_net", "howard")},
		},
		{
			id:       "msg_explicit_duplicate",
			text:     "@penny, still you.",
			explicit: []string{"penny"},
			want:     []string{protocol.AgentFQID("local", "penny")},
		},
	}

	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			message := sendMentionTestMessage(t, service, test.id, test.text, test.explicit)
			if !slices.Equal(message.Mentions, test.want) {
				t.Fatalf("unexpected mentions %#v, want %#v", message.Mentions, test.want)
			}
		})
	}
}

func TestSendMessageIgnoresUnknownAndAmbiguousMentions(t *testing.T) {
	t.Parallel()

	service := newMentionTestService([]protocol.Pairing{{
		ID:              "remote",
		RemoteNetworkID: "remote_net",
	}})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"director", "remote_net:director"},
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "ambiguous short alias", text: "@director, speak."},
		{name: "unknown short alias", text: "@penny, speak."},
		{name: "unknown scoped alias", text: "@missing:director, speak."},
		{name: "canonical outside room", text: "<@molt://remote_net/agents/penny>, speak."},
		{
			name: "mixed unresolved and scoped",
			text: "@director, then @remote:director, then @penny.",
			want: []string{protocol.AgentFQID("remote_net", "director")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := sendMentionTestMessage(
				t,
				service,
				"msg_"+strings.ReplaceAll(test.name, " ", "_"),
				test.text,
				nil,
			)
			if !slices.Equal(message.Mentions, test.want) {
				t.Fatalf("unexpected mentions %#v, want %#v", message.Mentions, test.want)
			}
		})
	}
}

func TestSendMessageDoesNotResolveRemoteMentionToLocalUnscopedMember(t *testing.T) {
	t.Parallel()

	service := newMentionTestService([]protocol.Pairing{{
		ID:              "remote",
		RemoteNetworkID: "remote_net",
	}})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"director"},
	}); err != nil {
		t.Fatal(err)
	}

	message := sendMentionTestMessage(
		t,
		service,
		"msg_remote_director",
		"<@molt://remote_net/agents/director>, speak.",
		nil,
	)
	if len(message.Mentions) != 0 {
		t.Fatalf("unexpected remote mention resolution %#v", message.Mentions)
	}
}

func newMentionTestService(pairings []protocol.Pairing) *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Pairings:          pairings,
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
}

func sendMentionTestMessage(
	t *testing.T,
	service *Service,
	id string,
	text string,
	explicit []string,
) protocol.Message {
	t.Helper()

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		ID:       id,
		Target:   protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:     protocol.Actor{Type: "agent", ID: "writer"},
		Parts:    []protocol.Part{{Kind: protocol.PartKindText, Text: text}},
		Mentions: explicit,
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	page, err := service.ListRoomMessagesContext(context.Background(), "research", protocol.PageRequest{Limit: 20})
	if err != nil {
		t.Fatalf("ListRoomMessagesContext() error = %v", err)
	}
	for _, message := range page.Messages {
		if message.ID == id {
			return message
		}
	}

	t.Fatalf("stored message %q not found in %#v", id, page.Messages)
	return protocol.Message{}
}
