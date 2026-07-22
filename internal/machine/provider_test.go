package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type fakeProvider struct {
	room      protocol.Room
	roomErr   error
	dms       protocol.DirectConversationPage
	dmErr     error
	roomPage  protocol.MessagePage
	dmPage    protocol.MessagePage
	pageErr   error
	accepted  protocol.MessageAccepted
	sendErr   error
	sendCalls int
	roomCalls int
	dmCalls   int
	request   protocol.SendMessageRequest
	page      protocol.PageRequest
}

func (fake *fakeProvider) GetRoom(context.Context, string) (protocol.Room, error) {
	fake.roomCalls++
	return fake.room, fake.roomErr
}
func (fake *fakeProvider) ListDMs(context.Context) (protocol.DirectConversationPage, error) {
	fake.dmCalls++
	return fake.dms, fake.dmErr
}
func (fake *fakeProvider) ListRoomMessages(_ context.Context, _ string, page protocol.PageRequest) (protocol.MessagePage, error) {
	fake.page = page
	return fake.roomPage, fake.pageErr
}
func (fake *fakeProvider) ListDMMessages(_ context.Context, _ string, page protocol.PageRequest) (protocol.MessagePage, error) {
	fake.page = page
	return fake.dmPage, fake.pageErr
}
func (fake *fakeProvider) SendMessage(_ context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	fake.sendCalls++
	fake.request = request
	return fake.accepted, fake.sendErr
}

func machineAttachment() clientconfig.AttachmentConfig {
	readWrite := &bridgeconfig.RoomAccess{CanRead: true, CanWrite: true}
	return clientconfig.AttachmentConfig{AgentName: "attached", MemberID: "self", NetworkID: "net", Rooms: []bridgeconfig.RoomBinding{{ID: "room", Access: readWrite}}, DMs: &bridgeconfig.DMConfig{Enabled: true}, OutboundDMPEers: []string{"peer"}}
}

func providerRoom() protocol.Room {
	return protocol.Room{ID: "room", NetworkID: "net", Members: []string{"self"}, Access: &protocol.RoomAccess{CanRead: true, CanWrite: true}}
}

func sendRequest(kind, id string) protocol.MachineRequest {
	return protocol.MachineRequest{Version: protocol.MachineProtocolV1, CorrelationID: "corr", Operation: protocol.MachineOpSendNudge, SendNudge: &protocol.MachineSendNudgeRequest{DeliveryID: "delivery", Target: protocol.MachineTarget{Kind: kind, ID: id}, Body: " exact ", OriginMessageID: "origin", CauseEventIDs: []string{"cause2", "cause1"}}}
}

func accepted() protocol.MessageAccepted {
	return protocol.MessageAccepted{Accepted: true, MessageID: "message", EventID: "event"}
}

func TestProviderExecutorRoomAuthorityAndSendMapping(t *testing.T) {
	attachment := machineAttachment()
	fake := &fakeProvider{room: providerRoom(), accepted: accepted()}
	executor := NewProviderExecutor(attachment, fake, NewDeliveryRegistry(8))
	response, err := executor.Execute(context.Background(), sendRequest(protocol.MachineTargetKindRoom, "room"))
	if err != nil || response.Error != nil || fake.sendCalls != 1 {
		t.Fatalf("send = %#v, %v; calls=%d", response, err, fake.sendCalls)
	}
	want := protocol.SendMessageRequest{ID: "delivery", Origin: protocol.MessageOrigin{NetworkID: "net", MessageID: "origin"}, Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "room"}, From: protocol.Actor{Type: "agent", ID: "self", Name: "attached", NetworkID: "net"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: " exact ", Data: map[string]any{"control_marker": protocol.MachineExportMarker}}}, CauseEventIDs: []string{"cause2", "cause1"}}
	if !reflect.DeepEqual(fake.request, want) {
		t.Fatalf("provider request mismatch\n got: %#v\nwant: %#v", fake.request, want)
	}
	if response.SendNudge == nil || !*response.SendNudge.Accepted || *response.SendNudge.ThreadCreated || *response.SendNudge.DMCreated || response.SendNudge.MessageID != "message" || response.SendNudge.EventID != "event" {
		t.Fatalf("unexpected accepted mapping: %#v", response.SendNudge)
	}
}

func TestProviderExecutorRejectsRoomAuthorityBeforeSend(t *testing.T) {
	for name, mutate := range map[string]func(*clientconfig.AttachmentConfig, *fakeProvider){
		"unattached":    func(a *clientconfig.AttachmentConfig, _ *fakeProvider) { a.Rooms = nil },
		"wrongNetwork":  func(_ *clientconfig.AttachmentConfig, f *fakeProvider) { f.room.NetworkID = "other" },
		"missingMember": func(_ *clientconfig.AttachmentConfig, f *fakeProvider) { f.room.Members = []string{"other"} },
		"malformedMember": func(_ *clientconfig.AttachmentConfig, f *fakeProvider) {
			f.room.Members = []string{"self", "bad member"}
		},
		"providerReadOnly":   func(_ *clientconfig.AttachmentConfig, f *fakeProvider) { f.room.Access.CanWrite = false },
		"attachmentReadOnly": func(a *clientconfig.AttachmentConfig, _ *fakeProvider) { a.Rooms[0].Access.CanWrite = false },
	} {
		t.Run(name, func(t *testing.T) {
			a, f := machineAttachment(), &fakeProvider{room: providerRoom(), accepted: accepted()}
			mutate(&a, f)
			response, _ := NewProviderExecutor(a, f, NewDeliveryRegistry(8)).Execute(context.Background(), sendRequest(protocol.MachineTargetKindRoom, "room"))
			if response.Error == nil || response.Error.Code != protocol.MachineErrorNotFound || f.sendCalls != 0 {
				t.Fatalf("expected pre-send denial, got %#v / calls=%d", response, f.sendCalls)
			}
		})
	}
}

func TestProviderExecutorReadAuthority(t *testing.T) {
	request := protocol.MachineRequest{Version: protocol.MachineProtocolV1, CorrelationID: "read", Operation: protocol.MachineOpRead, Read: &protocol.MachineReadRequest{Target: protocol.MachineTarget{Kind: "room", ID: "room"}, Limit: 1}}
	for name, mutate := range map[string]func(*clientconfig.AttachmentConfig, *fakeProvider){
		"attachmentUnreadable": func(a *clientconfig.AttachmentConfig, _ *fakeProvider) { a.Rooms[0].Access.CanRead = false },
		"providerUnreadable":   func(_ *clientconfig.AttachmentConfig, f *fakeProvider) { f.room.Access.CanRead = false },
	} {
		t.Run(name, func(t *testing.T) {
			a, f := machineAttachment(), &fakeProvider{room: providerRoom()}
			mutate(&a, f)
			response, _ := NewProviderExecutor(a, f, nil).Execute(context.Background(), request)
			if response.Error == nil || response.Error.Code != protocol.MachineErrorNotFound {
				t.Fatalf("expected inaccessible read %#v", response)
			}
		})
	}
}

func TestProviderExecutorDMTopology(t *testing.T) {
	valid := protocol.DirectConversation{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "peer"}}
	for name, mutate := range map[string]func(*clientconfig.AttachmentConfig, *protocol.DirectConversationPage){
		"disabled":   func(a *clientconfig.AttachmentConfig, _ *protocol.DirectConversationPage) { a.DMs.Enabled = false },
		"undeclared": func(a *clientconfig.AttachmentConfig, _ *protocol.DirectConversationPage) { a.OutboundDMPEers = nil },
		"declaredTwice": func(a *clientconfig.AttachmentConfig, _ *protocol.DirectConversationPage) {
			a.OutboundDMPEers = []string{"peer", "peer"}
		},
		"self": func(a *clientconfig.AttachmentConfig, _ *protocol.DirectConversationPage) {
			a.OutboundDMPEers = []string{"self"}
		},
		"partial": func(_ *clientconfig.AttachmentConfig, p *protocol.DirectConversationPage) { p.Page.HasMore = true },
		"network": func(_ *clientconfig.AttachmentConfig, p *protocol.DirectConversationPage) {
			p.DMs[0].NetworkID = "other"
		},
		"emptyNetwork": func(_ *clientconfig.AttachmentConfig, p *protocol.DirectConversationPage) {
			p.DMs[0].NetworkID = ""
		},
		"duplicate": func(_ *clientconfig.AttachmentConfig, p *protocol.DirectConversationPage) {
			p.DMs[0].ParticipantIDs = []string{"self", "self"}
		},
		"extra": func(_ *clientconfig.AttachmentConfig, p *protocol.DirectConversationPage) {
			p.DMs[0].ParticipantIDs = []string{"self", "peer", "other"}
		},
		"missing": func(_ *clientconfig.AttachmentConfig, p *protocol.DirectConversationPage) {
			p.DMs[0].ParticipantIDs = []string{"self", "other"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			a, page := machineAttachment(), protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid}}
			mutate(&a, &page)
			f := &fakeProvider{dms: page, accepted: accepted()}
			response, _ := NewProviderExecutor(a, f, NewDeliveryRegistry(8)).Execute(context.Background(), sendRequest(protocol.MachineTargetKindDM, "peer"))
			if response.Error == nil || f.sendCalls != 0 {
				t.Fatalf("expected topology rejection, got %#v / calls=%d", response, f.sendCalls)
			}
		})
	}
	for _, page := range []protocol.DirectConversationPage{{}, {DMs: []protocol.DirectConversation{valid, valid}}} {
		f := &fakeProvider{dms: page, accepted: accepted()}
		response, _ := NewProviderExecutor(machineAttachment(), f, NewDeliveryRegistry(8)).Execute(context.Background(), sendRequest(protocol.MachineTargetKindDM, "peer"))
		if response.Error == nil || f.sendCalls != 0 {
			t.Fatalf("expected zero/multiple topology rejection %#v calls=%d", response, f.sendCalls)
		}
	}
	f := &fakeProvider{dms: protocol.DirectConversationPage{DMs: []protocol.DirectConversation{valid}}, accepted: accepted()}
	response, _ := NewProviderExecutor(machineAttachment(), f, NewDeliveryRegistry(8)).Execute(context.Background(), sendRequest(protocol.MachineTargetKindDM, "peer"))
	if response.Error != nil || f.sendCalls != 1 || !reflect.DeepEqual(f.request.Target.ParticipantIDs, []string{"self", "peer"}) || f.request.Target.DMID != "dm" {
		t.Fatalf("expected exact unique dm selection, got %#v / %#v", response, f.request.Target)
	}
}

func TestProviderExecutorRejectsMalformedAcceptanceAndReadPage(t *testing.T) {
	ids := []protocol.MessageAccepted{{}, {MessageID: "message"}, {EventID: "event"}, {MessageID: "message", EventID: "event"}}
	flags := []protocol.MessageAccepted{{}, {ThreadCreated: true}, {DMCreated: true}, {ThreadCreated: true, DMCreated: true}}
	for _, accepted := range []bool{true, false} {
		for idIndex, id := range ids {
			for flagIndex, flag := range flags {
				value := protocol.MessageAccepted{Accepted: accepted, MessageID: id.MessageID, EventID: id.EventID, ThreadCreated: flag.ThreadCreated, DMCreated: flag.DMCreated}
				name := fmt.Sprintf("accepted=%t_ids=%d_flags=%d", accepted, idIndex, flagIndex)
				t.Run(name, func(t *testing.T) {
					registry, fake := NewDeliveryRegistry(8), &fakeProvider{room: providerRoom(), accepted: value}
					executor, request := NewProviderExecutor(machineAttachment(), fake, registry), sendRequest("room", "room")
					response := runProviderSend(t, executor, registry, request)
					safeRejection := !accepted && idIndex == 0 && flagIndex == 0
					validAcceptance := accepted && idIndex == 3 && flagIndex == 0
					if validAcceptance {
						if response.Error != nil || response.SendNudge == nil {
							t.Fatalf("rejected valid acceptance %#v", response)
						}
					} else if safeRejection {
						if response.Error == nil || response.Error.Code != protocol.MachineErrorNotFound {
							t.Fatalf("did not map definite rejection %#v", response)
						}
					} else if response.Error == nil || response.Error.Code != protocol.MachineErrorTransport {
						t.Fatalf("did not retain ambiguous acceptance %#v", response)
					}
					request.CorrelationID = "retry"
					_ = runProviderSend(t, executor, registry, request)
					wantCalls := 1
					if safeRejection {
						wantCalls = 2
					}
					if fake.sendCalls != wantCalls {
						t.Fatalf("calls=%d want=%d for %#v", fake.sendCalls, wantCalls, value)
					}
				})
			}
		}
	}
	request := protocol.MachineRequest{Version: protocol.MachineProtocolV1, CorrelationID: "read", Operation: protocol.MachineOpRead, Read: &protocol.MachineReadRequest{Target: protocol.MachineTarget{Kind: "room", ID: "room"}, Limit: 1}}
	f := &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Page: protocol.PageInfo{HasMore: true}}}
	response, _ := NewProviderExecutor(machineAttachment(), f, nil).Execute(context.Background(), request)
	if response.Error == nil || response.Error.Code != protocol.MachineErrorTransport {
		t.Fatalf("expected malformed page rejection %#v", response)
	}
}

func TestProviderExecutorReadPreservesValidatedPage(t *testing.T) {
	created := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	message := protocol.Message{ID: "message", NetworkID: "net", Origin: protocol.MessageOrigin{NetworkID: "net", MessageID: "origin"}, Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "room"}, From: protocol.Actor{Type: "agent", ID: "self", Name: "attached", NetworkID: "net"}, Parts: []protocol.Part{{Kind: "text", Text: "one", Data: map[string]any{"n": float64(1)}}}, Mentions: []string{"self"}, CreatedAt: created}
	f := &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Messages: []protocol.Message{message}, Page: protocol.PageInfo{HasMore: true, NextAfter: "after"}}}
	request := protocol.MachineRequest{Version: protocol.MachineProtocolV1, CorrelationID: "read", Operation: protocol.MachineOpRead, Read: &protocol.MachineReadRequest{Target: protocol.MachineTarget{Kind: "room", ID: "room"}, Limit: 2, After: "cursor"}}
	response, _ := NewProviderExecutor(machineAttachment(), f, nil).Execute(context.Background(), request)
	if response.Error != nil || response.Read == nil || !reflect.DeepEqual(f.page, protocol.PageRequest{After: "cursor", Limit: 2}) {
		t.Fatalf("read mismatch %#v page=%#v", response, f.page)
	}
	wantMessage := message
	wantMessage.Parts = []protocol.Part{{Kind: "text", Text: "one", Data: map[string]any{"n": json.Number("1")}}}
	if got := protocol.Message(response.Read.Page.Messages[0]); !reflect.DeepEqual(got, wantMessage) {
		t.Fatalf("message not preserved: %#v", got)
	}
	f.roomPage.Messages[0].NetworkID = "other"
	bad, _ := NewProviderExecutor(machineAttachment(), f, nil).Execute(context.Background(), request)
	if bad.Error == nil || bad.Error.Code != protocol.MachineErrorTransport {
		t.Fatalf("expected cross-network page rejection, got %#v", bad)
	}
}

func TestProviderExecutorDMReadForwardsBefore(t *testing.T) {
	created := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	message := protocol.Message{ID: "dm_message", NetworkID: "net", Origin: protocol.MessageOrigin{NetworkID: "net", MessageID: "origin"}, Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm", ParticipantIDs: []string{"self", "peer"}}, From: protocol.Actor{Type: "agent", ID: "peer", NetworkID: "net"}, Parts: []protocol.Part{{Kind: "text", Text: "dm"}}, CreatedAt: created}
	f := &fakeProvider{dms: protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "peer"}}}}, dmPage: protocol.MessagePage{Messages: []protocol.Message{message}, Page: protocol.PageInfo{HasMore: false}}}
	request := protocol.MachineRequest{Version: protocol.MachineProtocolV1, CorrelationID: "dmread", Operation: protocol.MachineOpRead, Read: &protocol.MachineReadRequest{Target: protocol.MachineTarget{Kind: "dm", ID: "peer"}, Limit: 1, Before: "before"}}
	response, _ := NewProviderExecutor(machineAttachment(), f, nil).Execute(context.Background(), request)
	wantMessage := message
	wantMessage.Target.ParticipantIDs = nil
	if response.Error != nil || response.Read == nil || !reflect.DeepEqual(f.page, protocol.PageRequest{Before: "before", Limit: 1}) || !reflect.DeepEqual(protocol.Message(response.Read.Page.Messages[0]), wantMessage) {
		t.Fatalf("dm read mismatch response=%#v page=%#v", response, f.page)
	}
}

func TestProviderExecutorNeverLeaksProviderError(t *testing.T) {
	sentinel := "https://secret.invalid/path bearer TOKEN header status body provider-error"
	f := &fakeProvider{room: providerRoom(), sendErr: errors.New(sentinel)}
	response, err := NewProviderExecutor(machineAttachment(), f, nil).Execute(context.Background(), sendRequest("room", "room"))
	if err != nil || response.Error == nil || response.Error.Code != protocol.MachineErrorTransport {
		t.Fatalf("unexpected error mapping %#v %v", response, err)
	}
	encoded, encodeErr := protocol.EncodeMachineResponseLine(response)
	if encodeErr != nil {
		t.Fatalf("could not inspect response")
	}
	for _, forbidden := range []string{"https://secret.invalid/path", "bearer", "TOKEN", "header", "status", "body", "provider-error"} {
		if strings.Contains(string(encoded), forbidden) || strings.Contains(errString(err), forbidden) {
			t.Fatalf("sentinel %q leaked: %q", forbidden, encoded)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
