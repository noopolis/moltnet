package machine

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type hostileJSONMarshaler struct{ called *bool }

func (value hostileJSONMarshaler) MarshalJSON() ([]byte, error) {
	*value.called = true
	return []byte(`"unexpected"`), nil
}

func TestDMReadChecksRawProviderTargetBeforeProjection(t *testing.T) {
	dm := protocol.DirectConversation{ID: "dm", NetworkID: "net", ParticipantIDs: []string{"self", "peer"}}
	base := readMessage("dm", "dm", []string{"self", "peer"})
	for _, test := range []struct {
		name   string
		mutate func(*protocol.Message)
	}{
		{"omitted participants", func(message *protocol.Message) { message.Target.ParticipantIDs = nil }},
		{"duplicate participants", func(message *protocol.Message) { message.Target.ParticipantIDs = []string{"self", "self"} }},
		{"extra participants", func(message *protocol.Message) { message.Target.ParticipantIDs = []string{"self", "peer", "other"} }},
		{"wrong peer", func(message *protocol.Message) { message.Target.ParticipantIDs = []string{"self", "other"} }},
		{"wrong dm", func(message *protocol.Message) { message.Target.DMID = "other" }},
		{"cross target", func(message *protocol.Message) { message.Target = protocol.Target{Kind: "room", RoomID: "room"} }},
		{"cross network", func(message *protocol.Message) { message.NetworkID = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := base
			test.mutate(&message)
			fake := &fakeProvider{dms: protocol.DirectConversationPage{DMs: []protocol.DirectConversation{dm}}, dmPage: protocol.MessagePage{Messages: []protocol.Message{message}}}
			response, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), readRequest("dm", "peer"))
			if response.Error == nil || response.Error.Code != protocol.MachineErrorTransport {
				t.Fatalf("accepted raw mismatch: %#v", response)
			}
		})
	}
	roomMessage := readMessage("room", "other", nil)
	fake := &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Messages: []protocol.Message{roomMessage}}}
	response, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), readRequest("room", "room"))
	if response.Error == nil || response.Error.Code != protocol.MachineErrorTransport {
		t.Fatalf("accepted room cross-target: %#v", response)
	}
}

func TestReadCopiesWireValuesExactlyAndDoesNotAliasProvider(t *testing.T) {
	large := json.Number("900719925474099312345678901234567890")
	first := readMessage("room", "room", nil)
	first.ID = "first"
	first.From = protocol.Actor{Type: "agent", ID: "self", Name: "Self", NetworkID: "net", FQID: "molt://net/agents/self", CredentialBound: true}
	first.Parts = []protocol.Part{{Kind: "text", Text: "first", Data: map[string]any{"integer": large, "nested": map[string]any{"value": "original"}}}}
	first.Mentions = []string{"self"}
	second := readMessage("room", "room", nil)
	second.ID = "second"
	second.From = protocol.Actor{Type: "agent", ID: "peer", Name: "Peer", NetworkID: "net", FQID: "molt://net/agents/peer", CredentialBound: true}
	second.Parts = []protocol.Part{{Kind: "text", Text: "second", MediaType: "text/plain", Filename: "second.txt", URL: "https://example.test/second"}}
	second.Mentions = []string{"peer"}
	fake := &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Messages: []protocol.Message{first, second}, Page: protocol.PageInfo{HasMore: true, NextAfter: "after"}}}
	request := readRequest("room", "room")
	request.CorrelationID = strings.Repeat("c", protocol.MachineMaxCorrelationBytes)
	response, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), request)
	if response.Error != nil || response.Read == nil || len(response.Read.Page.Messages) != 2 || response.Read.Page.Messages[0].ID != "first" || response.Read.Page.Messages[1].ID != "second" {
		t.Fatalf("read order/fields lost: %#v", response)
	}
	encoded, err := protocol.EncodeMachineResponseLine(response)
	if err != nil || len(encoded) > protocol.MachineMaxOutputLineBytes || !strings.Contains(string(encoded), large.String()) {
		t.Fatalf("large integer or response budget changed: %q / %v", encoded, err)
	}
	wantFirst, wantSecond := first, second
	wantFirst.From.FQID, wantFirst.From.CredentialBound = "", false
	wantSecond.From.FQID, wantSecond.From.CredentialBound = "", false
	if got := protocol.Message(response.Read.Page.Messages[0]); !reflect.DeepEqual(got, wantFirst) {
		t.Fatalf("first message projection changed: %#v", got)
	}
	if got := protocol.Message(response.Read.Page.Messages[1]); !reflect.DeepEqual(got, wantSecond) {
		t.Fatalf("second message projection changed: %#v", got)
	}
	if strings.Contains(string(encoded), "molt://net/agents/") || strings.Contains(string(encoded), "credential_bound") {
		t.Fatalf("provider authority escaped projection: %s", encoded)
	}
	got := protocol.Message(response.Read.Page.Messages[0])
	fake.roomPage.Messages[0].Parts[0].Data["integer"] = json.Number("2")
	fake.roomPage.Messages[0].Parts[0].Data["nested"].(map[string]any)["value"] = "changed"
	fake.roomPage.Messages[0].Mentions[0] = "peer"
	fake.roomPage.Messages[0].Target.RoomID = "other"
	fake.roomPage.Messages = nil
	if !reflect.DeepEqual(protocol.Message(response.Read.Page.Messages[0]), got) || response.Read.Page.Page.NextAfter != "after" {
		t.Fatalf("response aliases provider page: %#v", response.Read)
	}
	first.Parts[0].Data["integer"] = large
	first.Parts[0].Data["nested"].(map[string]any)["value"] = "original"
	first.Mentions[0], first.Target.RoomID = "self", "room"
	fake.roomPage.Messages = []protocol.Message{first, second}
	response.Read.Page.Messages[0].Parts[0].Data["integer"] = json.Number("3")
	response.Read.Page.Messages[0].Mentions[0] = "peer"
	response.Read.Page.Messages[0].Target.RoomID = "changed"
	if fake.roomPage.Messages[0].Parts[0].Data["integer"] != large || fake.roomPage.Messages[0].Mentions[0] != "self" || fake.roomPage.Messages[0].Target.RoomID != "room" {
		t.Fatalf("provider source aliases returned output: %#v", fake.roomPage.Messages[0])
	}
	fresh, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), request)
	if fresh.Error != nil || !reflect.DeepEqual(protocol.Message(fresh.Read.Page.Messages[0]), wantFirst) || !reflect.DeepEqual(protocol.Message(fresh.Read.Page.Messages[1]), wantSecond) {
		t.Fatalf("fresh projection aliases returned output: %#v", fresh)
	}
}

func TestReadRejectsHostileJSONDataBeforeMarshal(t *testing.T) {
	deep := any(nil)
	for index := 0; index <= maxJSONDataDepth; index++ {
		deep = []any{deep}
	}
	nodes := make([]any, maxJSONDataNodes+1)
	cyclicMap := map[string]any{}
	cyclicMap["self"] = cyclicMap
	cyclicSlice := make([]any, 1)
	cyclicSlice[0] = cyclicSlice
	called := false
	for _, test := range []struct {
		name string
		data map[string]any
	}{
		{"custom marshaler", map[string]any{"x": hostileJSONMarshaler{&called}}},
		{"self-cyclic map", cyclicMap},
		{"self-cyclic slice", map[string]any{"x": cyclicSlice}},
		{"long object key", map[string]any{strings.Repeat("x", maxJSONDataString+1): "x"}},
		{"unsupported value", map[string]any{"x": struct{}{}}},
		{"invalid number", map[string]any{"x": json.Number("01")}},
		{"nonfinite float", map[string]any{"x": math.Inf(1)}},
		{"excessive depth", map[string]any{"x": deep}},
		{"excessive nodes", map[string]any{"x": nodes}},
		{"long string", map[string]any{"x": strings.Repeat("x", maxJSONDataString+1)}},
		{"long number", map[string]any{"x": json.Number(strings.Repeat("1", maxJSONDataNumber+1))}},
		{"oversized encoding", map[string]any{"x": strings.Repeat("<", maxJSONDataString), "y": strings.Repeat("<", maxJSONDataString), "z": strings.Repeat("<", maxJSONDataString)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := readMessage("room", "room", nil)
			message.Parts[0].Data = test.data
			fake := &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Messages: []protocol.Message{message}}}
			response, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), readRequest("room", "room"))
			if response.Error == nil || response.Error.Code != protocol.MachineErrorTransport {
				t.Fatalf("accepted hostile data: %#v", response)
			}
		})
	}
	if called {
		t.Fatal("custom marshaler was invoked")
	}
}

func TestProviderFailuresAndValuesNeverEscapeMachineBoundary(t *testing.T) {
	sentinel := "https://secret.invalid bearer TOKEN header status body"
	assertHidden := func(t *testing.T, response protocol.MachineResponse, err error) {
		t.Helper()
		encoded, encodeErr := protocol.EncodeMachineResponseLine(response)
		if err != nil || encodeErr != nil || response.Error == nil {
			t.Fatalf("unexpected boundary result: %#v / %v / %v", response, err, encodeErr)
		}
		for _, value := range strings.Fields(sentinel) {
			if strings.Contains(errString(err), value) || strings.Contains(string(encoded), value) {
				t.Fatalf("provider sentinel leaked %q in %q", value, encoded)
			}
		}
	}
	for _, test := range []struct {
		name string
		fake *fakeProvider
		req  protocol.MachineRequest
	}{
		{"get room", &fakeProvider{roomErr: errors.New(sentinel)}, sendRequest("room", "room")},
		{"list dms", &fakeProvider{dmErr: errors.New(sentinel)}, sendRequest("dm", "peer")},
		{"read page", &fakeProvider{room: providerRoom(), pageErr: errors.New(sentinel)}, readRequest("room", "room")},
		{"send", &fakeProvider{room: providerRoom(), sendErr: errors.New(sentinel)}, sendRequest("room", "room")},
		{"bad room value", &fakeProvider{room: protocol.Room{ID: sentinel}}, sendRequest("room", "room")},
		{"bad dm value", &fakeProvider{dms: protocol.DirectConversationPage{DMs: []protocol.DirectConversation{{ID: sentinel, NetworkID: "net", ParticipantIDs: []string{"self", "peer"}}}}}, sendRequest("dm", "peer")},
		{"bad page value", &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Messages: []protocol.Message{readMessage("room", sentinel, nil)}}}, readRequest("room", "room")},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := NewProviderExecutor(machineAttachment(), test.fake, nil).Execute(context.Background(), test.req)
			assertHidden(t, response, err)
		})
	}
}

func TestReadForwardsExactBeforeAndAfter(t *testing.T) {
	for _, cursors := range []struct{ before, after string }{{before: "before"}, {after: "after"}} {
		request := readRequest("room", "room")
		request.Read.Before, request.Read.After = cursors.before, cursors.after
		fake := &fakeProvider{room: providerRoom(), roomPage: protocol.MessagePage{Messages: []protocol.Message{readMessage("room", "room", nil)}}}
		response, _ := NewProviderExecutor(machineAttachment(), fake, nil).Execute(context.Background(), request)
		if response.Error != nil || !reflect.DeepEqual(fake.page, protocol.PageRequest{Before: request.Read.Before, After: request.Read.After, Limit: request.Read.Limit}) {
			t.Fatalf("page forwarding changed: %#v / %#v", response, fake.page)
		}
	}
}
