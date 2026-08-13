package machine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func readRequest(kind, id string) protocol.MachineRequest {
	return protocol.MachineRequest{Version: protocol.MachineProtocolV1, CorrelationID: "read", Operation: protocol.MachineOpRead,
		Read: &protocol.MachineReadRequest{Target: protocol.MachineTarget{Kind: kind, ID: id}, Limit: 2}}
}

func readMessage(kind, id string, participants []string) protocol.Message {
	target := protocol.Target{Kind: kind, ParticipantIDs: participants}
	if kind == protocol.TargetKindRoom {
		target.RoomID = id
	} else {
		target.DMID = id
	}
	return protocol.Message{ID: "message", NetworkID: "net", Origin: protocol.MessageOrigin{NetworkID: "net", MessageID: "origin"},
		Target: target,
		From:   protocol.Actor{Type: "agent", ID: "self", NetworkID: "net"}, Parts: []protocol.Part{{Kind: "text", Text: "body"}},
		CreatedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)}
}

func TestRoomMembershipIsSemanticAndOnlyRequiredByPolicy(t *testing.T) {
	for _, test := range []struct {
		name  string
		write bool
		room  protocol.Room
		allow bool
	}{
		{"public nonmember read", false, protocol.Room{Visibility: "public", Members: []string{"other"}}, true},
		{"registered nonmember write", true, protocol.Room{WritePolicy: "registered_agents", Members: []string{"other"}}, true},
		{"operator nonmember write", true, protocol.Room{WritePolicy: "operators", Members: []string{"other"}}, true},
		{"private missing self", false, protocol.Room{Visibility: "private", Members: []string{"other"}}, false},
		{"members missing self", true, protocol.Room{WritePolicy: "members", Members: []string{"other"}}, false},
		{"semantic self alias duplicate", false, protocol.Room{Visibility: "private", Members: []string{"self", "net:self"}}, false},
		{"fqid self alias duplicate", true, protocol.Room{WritePolicy: "members", Members: []string{"self", "molt://net/agents/self"}}, false},
		{"local non-self alias duplicate", false, protocol.Room{Visibility: "public", Members: []string{"other", "net:other"}}, false},
		{"fqid non-self alias duplicate", false, protocol.Room{Visibility: "public", Members: []string{"other", "molt://net/agents/other"}}, false},
		{"remote non-self alias duplicate", false, protocol.Room{Visibility: "public", Members: []string{"remote:other", "molt://remote/agents/other"}}, false},
		{"distinct remote networks", false, protocol.Room{Visibility: "public", Members: []string{"other", "remote:other", "othernet:other"}}, true},
		{"unique scoped self", true, protocol.Room{WritePolicy: "members", Members: []string{"net:self", "remote:other"}}, true},
		{"malformed scoped", false, protocol.Room{Visibility: "public", Members: []string{"net: bad"}}, false},
		{"malformed fqid", false, protocol.Room{Visibility: "public", Members: []string{"molt://net/agents/bad/member"}}, false},
		{"raw malformed visibility", false, protocol.Room{Visibility: " public ", Members: []string{"self"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			room := providerRoom()
			room.Visibility, room.WritePolicy, room.Members = test.room.Visibility, test.room.WritePolicy, test.room.Members
			fake := &fakeProvider{room: room, accepted: accepted()}
			var response protocol.MachineResponse
			if test.write {
				response, _ = NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), sendRequest("room", "room"))
			} else {
				response, _ = NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), readRequest("room", "room"))
			}
			if (response.Error == nil) != test.allow {
				t.Fatalf("allow=%v got %#v", test.allow, response)
			}
		})
	}
}

func TestDMPageIsWholeAuthoritySnapshot(t *testing.T) {
	valid := protocol.DirectConversation{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "peer"}}
	for _, test := range []struct {
		name  string
		page  protocol.DirectConversationPage
		allow bool
	}{
		{"valid plus unrelated", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid, {ID: "other", NetworkID: "net", ParticipantIDs: []string{"self", "other"}}}}, true},
		{"has more", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid}, Page: protocol.PageInfo{HasMore: true}}, false},
		{"cursor without more", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid}, Page: protocol.PageInfo{NextBefore: "cursor"}}, false},
		{"wrong network", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", NetworkID: "other", ParticipantIDs: []string{"self", "peer"}}}}, false},
		{"empty network", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", ParticipantIDs: []string{"self", "peer"}}}}, false},
		{"raw whitespace", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", " peer"}}}}, false},
		{"raw duplicate", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "self"}}}}, false},
		{"raw missing", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "other"}}}}, false},
		{"raw extra", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "peer", "other"}}}}, false},
		{"valid plus malformed", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid, {ID: "bad", NetworkID: "other", ParticipantIDs: []string{"self", "other"}}}}, false},
		{"zero target", protocol.DirectConversationPage{}, false},
		{"multiple target", protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid, valid}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeProvider{dms: test.page, accepted: accepted()}
			response, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), sendRequest("dm", "peer"))
			if (response.Error == nil) != test.allow || fake.sendCalls != map[bool]int{true: 1, false: 0}[test.allow] {
				t.Fatalf("allow=%v response=%#v calls=%d", test.allow, response, fake.sendCalls)
			}
		})
	}
}

func TestProviderExecutorProjectsIndependentSecretFreeAuthority(t *testing.T) {
	attachment := machineAttachment()
	canWrite := true
	attachment.Rooms[0].CanWrite = &canWrite
	attachment.BaseURL = "https://authority-secret.invalid"
	attachment.Auth = clientconfig.AuthConfig{Token: "TOKEN-SENTINEL", TokenEnv: "TOKEN_ENV", TokenPath: "/secret"}
	attachment.Runtime = "runtime-sentinel"
	attachment.DMs.AllowedWakeSenders = []string{"wake-sentinel"}
	dm := protocol.DirectConversation{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "peer"}}
	fake := &fakeProvider{room: providerRoom(), accepted: accepted(), dms: protocol.DirectConversationPage{DMs: []protocol.DirectConversation{dm}},
		roomPage: protocol.MessagePage{Messages: []protocol.Message{readMessage("room", "room", nil)}},
		dmPage:   protocol.MessagePage{Messages: []protocol.Message{readMessage("dm", "dm", []string{"self", "peer"})}}}
	executor := NewProviderExecutor(attachment, fake, NewDeliveryRegistry(8))
	attachment.Rooms[0].ID = "mutated"
	attachment.Rooms[0].Access.CanRead, attachment.Rooms[0].Access.CanWrite = false, false
	*attachment.Rooms[0].CanWrite = false
	attachment.DMs.Enabled = false
	attachment.OutboundDMPEers[0] = "mutated"
	for _, request := range []protocol.MachineRequest{sendRequest("room", "room"), readRequest("room", "room"), sendRequest("dm", "peer"), readRequest("dm", "peer")} {
		response, _ := executor.Execute(context.Background(), request)
		if response.Error != nil {
			t.Fatalf("authority changed after attachment mutation: %#v", response)
		}
	}
	state := fmt.Sprintf("%#v", executor.resolver.authority)
	for _, forbidden := range []string{"authority-secret", "TOKEN-SENTINEL", "TOKEN_ENV", "/secret", "runtime-sentinel", "wake-sentinel"} {
		if strings.Contains(state, forbidden) {
			t.Fatalf("projected authority retained %q: %s", forbidden, state)
		}
	}
	if concrete, err := client.New(machineAttachment()); err != nil || any(concrete).(Provider) == nil {
		t.Fatalf("concrete client provider compatibility: %v", err)
	}
}
