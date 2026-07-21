package transport

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type contextDMService struct {
	*fakeService
	seenClaims bool
}

type publicRoomService struct{ *fakeService }

func (s *publicRoomService) GetRoomContext(_ context.Context, roomID string) (protocol.Room, error) {
	room, err := s.fakeService.GetRoomContext(context.Background(), roomID)
	if err != nil || protocol.NormalizeRoomVisibility(room.Visibility) != protocol.RoomVisibilityPublic {
		return protocol.Room{}, fakeStatusError{status: http.StatusForbidden, msg: "private room"}
	}
	return room, nil
}

func (s *contextDMService) GetDirectConversationContext(ctx context.Context, dmID string) (protocol.DirectConversation, error) {
	claims, ok := authn.ClaimsFromContext(ctx)
	s.seenClaims = ok
	if ok && claims.HasAgentRestriction() {
		conversation, err := s.fakeService.GetDirectConversation(dmID)
		if err != nil {
			return protocol.DirectConversation{}, err
		}
		for _, participant := range conversation.ParticipantIDs {
			if claims.AllowsAgent(participant) {
				return conversation, nil
			}
		}
		return protocol.DirectConversation{}, fakeStatusError{status: http.StatusForbidden, msg: "unrelated DM"}
	}
	return s.fakeService.GetDirectConversation(dmID)
}

func TestDMGetterPreservesAuthenticatedRequestClaims(t *testing.T) {
	service := &contextDMService{fakeService: &fakeService{
		network: protocol.Network{ID: "local"},
		dms:     []protocol.DirectConversation{{ID: "dm_luna", ParticipantIDs: []string{"luna", "mira"}}, {ID: "dm_atlas", ParticipantIDs: []string{"atlas", "mira"}}},
	}}
	policy, err := authn.NewPolicy(authn.Config{
		Mode:   authn.ModeBearer,
		Tokens: []authn.TokenConfig{{ID: "observe", Value: "secret", Scopes: []authn.Scope{authn.ScopeObserve}, Agents: []string{"luna"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(service, policy)
	request := httptest.NewRequest(http.MethodGet, "/v1/dms/dm_luna", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !service.seenClaims {
		t.Fatalf("DM response status=%d claims-preserved=%v body=%s", response.Code, service.seenClaims, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/dms/dm_atlas", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unrelated DM status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEnabledAttachmentNarrowsPrivateRoomsDMsAndAdminEvents(t *testing.T) {
	service := &fakeService{
		network: protocol.Network{ID: "local"},
		rooms: []protocol.Room{
			{ID: "own", Visibility: protocol.RoomVisibilityPrivate, Members: []string{"luna"}},
			{ID: "other", Visibility: protocol.RoomVisibilityPrivate, Members: []string{"atlas"}},
		},
		dms: []protocol.DirectConversation{{ID: "dm_luna", ParticipantIDs: []string{"luna", "mira"}}, {ID: "dm_atlas", ParticipantIDs: []string{"atlas", "mira"}}},
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{ID: "observe", Value: "secret", Scopes: []authn.Scope{authn.ScopeObserve}, Agents: []string{"luna"}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{ID: "observe", Scopes: []authn.Scope{authn.ScopeObserve}, Agents: []string{"luna"}}))
	filter := attachmentEventFilter(policy, ctx, service, "local", "luna")
	if !filter(ctx, protocol.Event{ID: "gap", Type: protocol.EventTypeReplayGap, ReplayGap: &protocol.ReplayGap{RequestedEventID: "old"}}) {
		t.Fatal("private-read attachment suppressed replay-gap recovery control")
	}
	roomEvent := func(id string) protocol.Event {
		return protocol.Event{ID: id, Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: id}}}
	}
	dmEvent := func(id, dmID string) protocol.Event {
		return protocol.Event{ID: id, Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: dmID}}}
	}
	if !filter(ctx, roomEvent("own")) || !filter(ctx, dmEvent("dm-own", "dm_luna")) {
		t.Fatal("allowed private room/DM was not delivered")
	}
	for _, event := range []protocol.Event{roomEvent("other"), dmEvent("dm-other", "dm_atlas"), {ID: "pair", Type: protocol.EventTypePairingUpdated}} {
		if filter(ctx, event) {
			t.Fatalf("attachment delivered blocked event %#v", event)
		}
	}
	if filter(ctx, protocol.Event{ID: "inconsistent-dm", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_luna", ParticipantIDs: []string{"atlas"}}}}) {
		t.Fatal("inconsistent DM participant event was delivered")
	}
	if filter(ctx, protocol.Event{ID: "inconsistent-room", Type: protocol.EventTypeRoomCreated, Room: &protocol.Room{ID: "own", Visibility: protocol.RoomVisibilityPrivate, Members: []string{"atlas"}}}) {
		t.Fatal("inconsistent room event was delivered")
	}
}

func TestAttachmentVisibilityKeepsNoAuthAndPublicReadBoundaries(t *testing.T) {
	service := &publicRoomService{fakeService: &fakeService{
		network: protocol.Network{ID: "local"},
		rooms: []protocol.Room{
			{ID: "public", Visibility: protocol.RoomVisibilityPublic},
			{ID: "private", Visibility: protocol.RoomVisibilityPrivate, Members: []string{"atlas"}},
		},
	}}
	privateEvent := protocol.Event{ID: "private", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "private"}}}
	publicEvent := protocol.Event{ID: "public", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}}}
	if filter := attachmentEventFilter(nil, context.Background(), service, "local", "luna"); filter != nil {
		t.Fatal("no-auth attachment unexpectedly became filtered")
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatal(err)
	}
	claims := authn.NewStaticClaims(authn.TokenConfig{ID: "admin", Scopes: []authn.Scope{authn.ScopeAdmin}})
	requestContext := authn.WithClaims(context.Background(), claims)
	filter := attachmentEventFilter(policy, requestContext, service, "local", "luna")
	if filter(requestContext, publicEvent) != true {
		t.Fatal("public room was not delivered")
	}
	if filter(requestContext, privateEvent) {
		t.Fatal("authenticated public-read attachment widened to private room")
	}
}

type attachmentDMErrorService struct {
	*fakeService
	err error
}

func (s *attachmentDMErrorService) GetDirectConversationContext(context.Context, string) (protocol.DirectConversation, error) {
	return protocol.DirectConversation{}, s.err
}

func TestAttachmentDMFallbackFailsClosedExceptNotFound(t *testing.T) {
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{ID: "attach", Value: "secret", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}}}})
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.Event{Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "legacy", ParticipantIDs: []string{"luna", "mira"}}}}
	ctx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{ID: "attach", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}}))
	for _, test := range []struct {
		name   string
		status int
		want   bool
	}{{"not found compatibility", http.StatusNotFound, true}, {"authorization failure", http.StatusForbidden, false}, {"store failure", http.StatusInternalServerError, false}} {
		service := &attachmentDMErrorService{fakeService: &fakeService{network: protocol.Network{ID: "local"}}, err: fakeStatusError{status: test.status, msg: test.name}}
		filter := attachmentEventFilter(policy, ctx, service, "local", "luna")
		if got := filter(ctx, event); got != test.want {
			t.Fatalf("%s fallback = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestAttachmentLifecycleIdentityIsNetworkQualified(t *testing.T) {
	policy := mustBearerPolicy(t, authn.TokenConfig{ID: "attach", Value: "secret", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}})
	ctx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{ID: "attach", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}}))
	service := &fakeService{network: protocol.Network{ID: "local"}}
	filter := attachmentEventFilter(policy, ctx, service, "local", "luna")
	for _, eventType := range []string{protocol.EventTypeAgentConnected, protocol.EventTypeAgentDisconnected, protocol.EventTypeAgentWakeDelivered, protocol.EventTypeAgentWakeFailed} {
		for _, test := range []struct {
			name string
			net  string
			want bool
		}{
			{"local", "local", true}, {"remote same name", "remote", false}, {"unrelated", "local", false},
		} {
			agentID := "luna"
			if test.name == "unrelated" {
				agentID = "atlas"
			}
			event := protocol.Event{NetworkID: test.net, Type: eventType, Agent: &protocol.AgentEvent{AgentID: agentID, NetworkID: test.net, FQID: protocol.AgentFQID(test.net, agentID)}}
			if got := filter(ctx, event); got != test.want {
				t.Fatalf("%s %s = %v, want %v", test.name, eventType, got, test.want)
			}
		}
		malformed := protocol.Event{NetworkID: "local", Type: eventType, Agent: &protocol.AgentEvent{AgentID: "luna", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "luna")}}
		if filter(ctx, malformed) {
			t.Fatalf("inconsistent %s lifecycle event passed", eventType)
		}
	}
	removed := func(network, agentID string) protocol.Event {
		return protocol.Event{NetworkID: network, Type: protocol.EventTypeAgentRemoved, Agent: &protocol.AgentEvent{AgentID: agentID, NetworkID: network, FQID: protocol.AgentFQID(network, agentID)}}
	}
	if !attachmentRemovedEvent(removed("local", "luna"), "local", "luna") || attachmentRemovedEvent(removed("remote", "luna"), "local", "luna") || attachmentRemovedEvent(removed("local", "atlas"), "local", "luna") {
		t.Fatal("network-qualified removal decision admitted the wrong identity")
	}
}

func realVisibilityService() *rooms.Service {
	memory := store.NewMemoryStore()
	return rooms.NewService(rooms.ServiceConfig{AllowHumanIngress: true, NetworkID: "local", NetworkName: "Local", Version: "test", Store: memory, Messages: memory, Broker: events.NewBroker()})
}

func TestHTTPEventStreamFiltersRestrictedReplayAndLiveEvents(t *testing.T) {
	service := realVisibilityService()
	for _, room := range []protocol.CreateRoomRequest{{ID: "public", Visibility: protocol.RoomVisibilityPublic}, {ID: "own", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}, {ID: "other", Members: []string{"atlas"}, Visibility: protocol.RoomVisibilityPrivate}} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatal(err)
		}
	}
	first, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "cursor"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hidden-room"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-own", ParticipantIDs: []string{"luna", "mira"}}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "own-dm"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-other", ParticipantIDs: []string{"atlas", "mira"}}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hidden-dm"}}}); err != nil {
		t.Fatal(err)
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, PublicRead: true, Tokens: []authn.TokenConfig{{ID: "luna", Value: "luna-secret", Scopes: []authn.Scope{authn.ScopeObserve}, Agents: []string{"luna"}}, {ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeAdmin}}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHTTPHandler(service, policy))
	defer server.Close()
	requestContext, requestCancel := context.WithCancel(context.Background())
	defer requestCancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, server.URL+"/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer luna-secret")
	request.Header.Set("Last-Event-ID", first.EventID)
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status %d", response.StatusCode)
	}
	lines := make(chan string, 1)
	go readSSEUntil(lines, response.Body, "allowed-sentinel")
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "allowed-sentinel"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-lines:
		for _, blocked := range []string{"hidden-room", "hidden-dm"} {
			if strings.Contains(body, blocked) {
				t.Fatalf("restricted SSE leaked %q: %s", blocked, body)
			}
		}
		if !strings.Contains(body, "own-dm") || !strings.Contains(body, "allowed-sentinel") {
			t.Fatalf("restricted SSE missed replay/live positive: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out reading SSE replay/live sentinel")
	}
	operatorContext, operatorCancel := context.WithCancel(context.Background())
	defer operatorCancel()
	operatorRequest, _ := http.NewRequestWithContext(operatorContext, http.MethodGet, server.URL+"/v1/events/stream", nil)
	operatorRequest.Header.Set("Authorization", "Bearer operator-secret")
	operatorResponse, err := (&http.Client{}).Do(operatorRequest)
	if err != nil {
		t.Fatal(err)
	}
	operatorLines := make(chan string, 1)
	go readSSEUntil(operatorLines, operatorResponse.Body, "operator-sentinel")
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "operator-sentinel"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-operatorLines:
		if !strings.Contains(body, "operator-sentinel") {
			t.Fatalf("operator SSE missed unrelated private event: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out reading operator SSE sentinel")
	}
	operatorResponse.Body.Close()
	openPolicy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, PublicRead: true})
	if err != nil {
		t.Fatal(err)
	}
	openServer := httptest.NewServer(NewHTTPHandler(service, openPolicy))
	defer openServer.Close()
	openContext, openCancel := context.WithCancel(context.Background())
	defer openCancel()
	openRequest, _ := http.NewRequestWithContext(openContext, http.MethodGet, openServer.URL+"/v1/events/stream", nil)
	openResponse, err := (&http.Client{}).Do(openRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer openResponse.Body.Close()
	if openResponse.StatusCode != http.StatusOK {
		t.Fatalf("anonymous open SSE status %d", openResponse.StatusCode)
	}
	openLines := make(chan string, 1)
	go readSSEUntil(openLines, openResponse.Body, "public-sentinel")
	for _, message := range []protocol.SendMessageRequest{
		{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "public-content"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "open-hidden-room"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "open-hidden-dm", ParticipantIDs: []string{"atlas", "mira"}}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "open-hidden-dm"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "public-sentinel"}}},
	} {
		if _, err := service.SendMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case body := <-openLines:
		if !strings.Contains(body, "public-content") || !strings.Contains(body, "public-sentinel") || strings.Contains(body, "open-hidden-room") || strings.Contains(body, "open-hidden-dm") {
			t.Fatalf("anonymous open SSE visibility = %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out reading public SSE sentinel")
	}
}

func readSSEUntil(output chan<- string, body io.ReadCloser, sentinel string) {
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 256*1024)
	var content strings.Builder
	for scanner.Scan() {
		content.WriteString(scanner.Text())
		content.WriteByte('\n')
		if strings.Contains(content.String(), sentinel) {
			break
		}
	}
	output <- content.String()
}
