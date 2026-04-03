package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type fakeStatusError struct {
	status int
	msg    string
}

func (e fakeStatusError) Error() string {
	return e.msg
}

func (e fakeStatusError) StatusCode() int {
	return e.status
}

type fakeService struct {
	network       protocol.Network
	pairNetwork   protocol.Network
	agents        []protocol.AgentSummary
	pairAgents    []protocol.AgentSummary
	pairings      []protocol.Pairing
	pairingsPage  protocol.PairingPage
	pairListErr   error
	rooms         []protocol.Room
	pairRooms     []protocol.Room
	roomPage      protocol.MessagePage
	threads       []protocol.Thread
	threadPage    protocol.MessagePage
	threadsPage   protocol.ThreadPage
	dms           []protocol.DirectConversation
	dmListPage    protocol.DirectConversationPage
	dmPage        protocol.MessagePage
	artifactPage  protocol.ArtifactPage
	createdRoom   protocol.CreateRoomRequest
	updatedRoom   protocol.UpdateRoomMembersRequest
	sentMessage   protocol.SendMessageRequest
	roomID        string
	roomBefore    string
	roomAfter     string
	threadID      string
	threadBefore  string
	threadAfter   string
	dmID          string
	dmBefore      string
	dmAfter       string
	agentBefore   string
	agentAfter    string
	pairBefore    string
	pairAfter     string
	pairRoomPage  protocol.RoomPage
	pairAgentPage protocol.AgentPage
	limit         int
	createErr     error
	pairingErr    error
	roomError     error
	threadError   error
	dmError       error
	artifactErr   error
	sendErr       error
	healthErr     error
	stream        chan protocol.Event
	replayStream  chan protocol.Event
}

func (f *fakeService) Health(ctx context.Context) error { return f.healthErr }
func (f *fakeService) Network() protocol.Network        { return f.network }
func (f *fakeService) GetAgent(agentID string) (protocol.AgentSummary, error) {
	for _, agent := range f.agents {
		if agent.ID == agentID {
			return agent, nil
		}
	}
	return protocol.AgentSummary{}, fakeStatusError{status: http.StatusNotFound, msg: "unknown agent"}
}
func (f *fakeService) GetDirectConversation(dmID string) (protocol.DirectConversation, error) {
	for _, dm := range f.dms {
		if dm.ID == dmID {
			return dm, nil
		}
	}
	return protocol.DirectConversation{}, fakeStatusError{status: http.StatusNotFound, msg: "unknown dm"}
}
func (f *fakeService) GetRoom(roomID string) (protocol.Room, error) {
	for _, room := range f.rooms {
		if room.ID == roomID {
			return room, nil
		}
	}
	return protocol.Room{}, fakeStatusError{status: http.StatusNotFound, msg: "unknown room"}
}
func (f *fakeService) GetThread(threadID string) (protocol.Thread, error) {
	for _, thread := range f.threads {
		if thread.ID == threadID {
			return thread, nil
		}
	}
	return protocol.Thread{}, fakeStatusError{status: http.StatusNotFound, msg: "unknown thread"}
}
func (f *fakeService) ListAgentsContext(ctx context.Context, page protocol.PageRequest) (protocol.AgentPage, error) {
	f.agentBefore = page.Before
	f.agentAfter = page.After
	f.limit = page.Limit
	return protocol.AgentPage{Agents: f.agents}, nil
}
func (f *fakeService) ListPairingsContext(ctx context.Context, page protocol.PageRequest) (protocol.PairingPage, error) {
	f.pairBefore = page.Before
	f.pairAfter = page.After
	f.limit = page.Limit
	if f.pairListErr != nil {
		return protocol.PairingPage{}, f.pairListErr
	}
	if len(f.pairingsPage.Pairings) > 0 || f.pairingsPage.Page != (protocol.PageInfo{}) {
		return f.pairingsPage, nil
	}
	return protocol.PairingPage{Pairings: f.pairings}, nil
}
func (f *fakeService) PairingNetwork(ctx context.Context, pairingID string) (protocol.Network, error) {
	if f.pairingErr != nil {
		return protocol.Network{}, f.pairingErr
	}
	return f.pairNetwork, nil
}
func (f *fakeService) PairingRoomsContext(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.RoomPage, error) {
	f.roomBefore = page.Before
	f.roomAfter = page.After
	f.limit = page.Limit
	if f.pairingErr != nil {
		return protocol.RoomPage{}, f.pairingErr
	}
	if len(f.pairRoomPage.Rooms) > 0 || f.pairRoomPage.Page != (protocol.PageInfo{}) {
		return f.pairRoomPage, nil
	}
	return protocol.RoomPage{Rooms: f.pairRooms}, nil
}
func (f *fakeService) PairingAgentsContext(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.AgentPage, error) {
	f.agentBefore = page.Before
	f.agentAfter = page.After
	f.limit = page.Limit
	if f.pairingErr != nil {
		return protocol.AgentPage{}, f.pairingErr
	}
	if len(f.pairAgentPage.Agents) > 0 || f.pairAgentPage.Page != (protocol.PageInfo{}) {
		return f.pairAgentPage, nil
	}
	return protocol.AgentPage{Agents: f.pairAgents}, nil
}
func (f *fakeService) ListRoomsContext(ctx context.Context, page protocol.PageRequest) (protocol.RoomPage, error) {
	f.roomBefore = page.Before
	f.roomAfter = page.After
	f.limit = page.Limit
	return protocol.RoomPage{Rooms: f.rooms}, nil
}
func (f *fakeService) CreateRoomContext(ctx context.Context, request protocol.CreateRoomRequest) (protocol.Room, error) {
	f.createdRoom = request
	if f.createErr != nil {
		return protocol.Room{}, f.createErr
	}
	return protocol.Room{ID: request.ID, Name: request.Name}, nil
}
func (f *fakeService) UpdateRoomMembers(ctx context.Context, roomID string, request protocol.UpdateRoomMembersRequest) (protocol.Room, error) {
	f.roomID = roomID
	f.updatedRoom = request
	return protocol.Room{ID: roomID, Members: append(append([]string(nil), request.Add...), request.Remove...)}, nil
}
func (f *fakeService) ListRoomMessagesContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	f.roomID = roomID
	f.roomBefore = page.Before
	f.limit = page.Limit
	if f.roomError != nil {
		return protocol.MessagePage{}, f.roomError
	}
	return f.roomPage, nil
}
func (f *fakeService) ListThreadsContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.ThreadPage, error) {
	f.roomID = roomID
	f.threadBefore = page.Before
	f.threadAfter = page.After
	f.limit = page.Limit
	if f.threadError != nil {
		return protocol.ThreadPage{}, f.threadError
	}
	if len(f.threadsPage.Threads) > 0 || f.threadsPage.Page != (protocol.PageInfo{}) {
		return f.threadsPage, nil
	}
	return protocol.ThreadPage{Threads: f.threads}, nil
}
func (f *fakeService) ListThreadMessagesContext(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	f.threadID = threadID
	f.threadBefore = page.Before
	f.limit = page.Limit
	if f.threadError != nil {
		return protocol.MessagePage{}, f.threadError
	}
	return f.threadPage, nil
}
func (f *fakeService) ListDirectConversationsContext(ctx context.Context, page protocol.PageRequest) (protocol.DirectConversationPage, error) {
	f.dmBefore = page.Before
	f.dmAfter = page.After
	f.limit = page.Limit
	if len(f.dmListPage.DMs) > 0 || f.dmListPage.Page != (protocol.PageInfo{}) {
		return f.dmListPage, nil
	}
	return protocol.DirectConversationPage{DMs: f.dms}, nil
}
func (f *fakeService) ListDMMessagesContext(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	f.dmID = dmID
	f.dmBefore = page.Before
	f.limit = page.Limit
	if f.dmError != nil {
		return protocol.MessagePage{}, f.dmError
	}
	return f.dmPage, nil
}
func (f *fakeService) ListArtifactsContext(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error) {
	f.roomID = filter.RoomID
	f.threadID = filter.ThreadID
	f.dmID = filter.DMID
	f.roomBefore = page.Before
	f.limit = page.Limit
	if f.artifactErr != nil {
		return protocol.ArtifactPage{}, f.artifactErr
	}
	return f.artifactPage, nil
}
func (f *fakeService) SendMessageContext(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	f.sentMessage = request
	if err := protocol.ValidateSendMessageRequest(request); err != nil {
		return protocol.MessageAccepted{}, fakeStatusError{
			status: http.StatusUnprocessableEntity,
			msg:    err.Error(),
		}
	}
	if f.sendErr != nil {
		return protocol.MessageAccepted{}, f.sendErr
	}
	return protocol.MessageAccepted{MessageID: "msg_1", EventID: "evt_1", Accepted: true}, nil
}
func (f *fakeService) Subscribe(ctx context.Context) <-chan protocol.Event {
	if f.stream == nil {
		ch := make(chan protocol.Event)
		close(ch)
		return ch
	}
	return f.stream
}
func (f *fakeService) SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	if f.replayStream != nil {
		return f.replayStream
	}
	return f.Subscribe(ctx)
}

func TestNewHTTPHandlerRoutes(t *testing.T) {
	t.Parallel()

	stream := make(chan protocol.Event, 1)
	stream <- protocol.Event{
		ID:   "evt_1",
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID: "msg_1",
		},
	}
	close(stream)

	service := &fakeService{
		network: protocol.Network{
			ID:   "local",
			Name: "Local",
			Capabilities: protocol.NetworkCapabilities{
				EventStream:        "sse",
				AttachmentProtocol: "websocket",
				HumanIngress:       true,
				MessagePagination:  "cursor",
				Pairings:           true,
			},
		},
		agents: []protocol.AgentSummary{
			{ID: "orchestrator", FQID: "molt://local/agents/orchestrator", NetworkID: "local", Rooms: []string{"research"}},
		},
		pairings: []protocol.Pairing{
			{ID: "pair_1", RemoteNetworkID: "remote", RemoteNetworkName: "Remote Lab", Status: "connected"},
		},
		pairNetwork: protocol.Network{
			ID:   "remote",
			Name: "Remote Lab",
		},
		pairRooms: []protocol.Room{
			{ID: "remote-research", NetworkID: "remote", FQID: "molt://remote/rooms/remote-research", Name: "Remote Research"},
		},
		pairAgents: []protocol.AgentSummary{
			{ID: "remote-writer", NetworkID: "remote", FQID: "molt://remote/agents/remote-writer"},
		},
		rooms: []protocol.Room{
			{ID: "research", NetworkID: "local", FQID: "molt://local/rooms/research", Name: "Research"},
		},
		roomPage: protocol.MessagePage{
			Messages: []protocol.Message{
				{ID: "msg_room"},
			},
			Page: protocol.PageInfo{HasMore: true, NextBefore: "msg_room"},
		},
		threads: []protocol.Thread{
			{ID: "thread_1", NetworkID: "local", FQID: "molt://local/threads/thread_1", RoomID: "research", ParentMessageID: "msg_room", MessageCount: 1},
		},
		threadPage: protocol.MessagePage{
			Messages: []protocol.Message{
				{ID: "msg_thread"},
			},
			Page: protocol.PageInfo{HasMore: false},
		},
		dms: []protocol.DirectConversation{
			{ID: "dm_1", NetworkID: "local", FQID: "molt://local/dms/dm_1", MessageCount: 1},
		},
		dmPage: protocol.MessagePage{
			Messages: []protocol.Message{
				{ID: "msg_dm"},
			},
			Page: protocol.PageInfo{HasMore: false},
		},
		artifactPage: protocol.ArtifactPage{
			Artifacts: []protocol.Artifact{
				{ID: "art_1", NetworkID: "local", FQID: "molt://local/artifacts/art_1", MessageID: "msg_thread", PartIndex: 0, Kind: "image"},
			},
			Page: protocol.PageInfo{HasMore: false},
		},
		stream: stream,
	}

	handler := NewHTTPHandler(service, nil)

	t.Run("healthz", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", response.Code)
		}
	})

	t.Run("readyz", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if !strings.Contains(response.Body.String(), `"ready"`) {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("network", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/network", nil)
		handler.ServeHTTP(response, request)

		var payload protocol.Network
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ID != "local" {
			t.Fatalf("unexpected network %#v", payload)
		}
		if payload.Capabilities.MessagePagination != "cursor" || payload.Capabilities.AttachmentProtocol != "websocket" {
			t.Fatalf("unexpected capabilities %#v", payload.Capabilities)
		}
	})

	t.Run("rooms list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/rooms?limit=3&after=room_prev", nil)
		handler.ServeHTTP(response, request)

		if service.roomAfter != "room_prev" || service.limit != 3 {
			t.Fatalf("unexpected rooms list after %q limit %d", service.roomAfter, service.limit)
		}
		if !strings.Contains(response.Body.String(), "research") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("room get", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/rooms/research", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), `"id":"research"`) {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("agents list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/agents?limit=4&after=agent_prev", nil)
		handler.ServeHTTP(response, request)

		if service.agentAfter != "agent_prev" || service.limit != 4 {
			t.Fatalf("unexpected agents list after %q limit %d", service.agentAfter, service.limit)
		}
		if !strings.Contains(response.Body.String(), "orchestrator") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("agent get", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/agents/orchestrator", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), `"id":"orchestrator"`) {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("pairings list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings?limit=2&after=pair_prev", nil)
		handler.ServeHTTP(response, request)

		if service.pairAfter != "pair_prev" || service.limit != 2 {
			t.Fatalf("unexpected pairings list after %q limit %d", service.pairAfter, service.limit)
		}
		if !strings.Contains(response.Body.String(), "Remote Lab") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("pairings invalid cursor error", func(t *testing.T) {
		service.pairListErr = fakeStatusError{status: http.StatusUnprocessableEntity, msg: `invalid cursor "missing"`}
		defer func() { service.pairListErr = nil }()

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings?after=missing", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("unexpected invalid cursor status %d", response.Code)
		}
	})

	t.Run("pairing network", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings/pair_1/network", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "\"id\":\"remote\"") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("pairing rooms", func(t *testing.T) {
		service.pairRoomPage = protocol.RoomPage{
			Rooms: []protocol.Room{
				{ID: "remote-research", NetworkID: "remote", FQID: "molt://remote/rooms/remote-research", Name: "Remote Research"},
			},
			Page: protocol.PageInfo{HasMore: true, NextAfter: "remote-research"},
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings/pair_1/rooms?after=remote-seed&limit=2", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "remote-research") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
		if service.roomAfter != "remote-seed" || service.limit != 2 {
			t.Fatalf("unexpected room pagination capture after=%q limit=%d", service.roomAfter, service.limit)
		}
	})

	t.Run("pairing agents", func(t *testing.T) {
		service.pairAgentPage = protocol.AgentPage{
			Agents: []protocol.AgentSummary{
				{ID: "remote-writer", NetworkID: "remote", FQID: "molt://remote/agents/remote-writer"},
			},
			Page: protocol.PageInfo{HasMore: true, NextBefore: "remote-writer"},
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings/pair_1/agents?before=remote-zzz&limit=3", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "remote-writer") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
		if service.agentBefore != "remote-zzz" || service.limit != 3 {
			t.Fatalf("unexpected agent pagination capture before=%q limit=%d", service.agentBefore, service.limit)
		}
	})

	t.Run("pairing rooms invalid cursor", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings/pair_1/rooms?before=room_1&after=room_2", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("unexpected invalid cursor status %d", response.Code)
		}
	})

	t.Run("create room", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(`{"id":"review","name":"Review"}`))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusCreated {
			t.Fatalf("unexpected status %d", response.Code)
		}

		if service.createdRoom.ID != "review" {
			t.Fatalf("unexpected room request %#v", service.createdRoom)
		}
	})

	t.Run("update room members", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/v1/rooms/research/members", strings.NewReader(`{"add":["writer"],"remove":["reviewer"]}`))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if service.roomID != "research" || len(service.updatedRoom.Add) != 1 || service.updatedRoom.Add[0] != "writer" {
			t.Fatalf("unexpected room member update %#v", service.updatedRoom)
		}
		if !strings.Contains(response.Body.String(), "writer") || !strings.Contains(response.Body.String(), "reviewer") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("room messages", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/rooms/research/messages?limit=12&before=msg_room_older", nil)
		handler.ServeHTTP(response, request)

		if service.roomID != "research" || service.roomBefore != "msg_room_older" || service.limit != 12 {
			t.Fatalf("unexpected room lookup %q before %q limit %d", service.roomID, service.roomBefore, service.limit)
		}
		if !strings.Contains(response.Body.String(), "msg_room") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
		if !strings.Contains(response.Body.String(), "\"page\"") {
			t.Fatalf("expected page metadata in body %s", response.Body.String())
		}
	})

	t.Run("threads list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/rooms/research/threads?limit=2&after=thread_prev", nil)
		handler.ServeHTTP(response, request)

		if service.roomID != "research" || service.threadAfter != "thread_prev" || service.limit != 2 {
			t.Fatalf("unexpected thread lookup %q after %q limit %d", service.roomID, service.threadAfter, service.limit)
		}
		if !strings.Contains(response.Body.String(), "thread_1") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
		if !strings.Contains(response.Body.String(), "\"page\"") {
			t.Fatalf("expected page metadata in body %s", response.Body.String())
		}
	})

	t.Run("thread messages", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/threads/thread_1/messages?limit=4&before=msg_thread_prev", nil)
		handler.ServeHTTP(response, request)

		if service.threadID != "thread_1" || service.threadBefore != "msg_thread_prev" || service.limit != 4 {
			t.Fatalf("unexpected thread lookup %q before %q limit %d", service.threadID, service.threadBefore, service.limit)
		}
		if !strings.Contains(response.Body.String(), "msg_thread") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("thread get", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/threads/thread_1", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), `"id":"thread_1"`) {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("dms list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/dms?limit=2&after=dm_prev", nil)
		handler.ServeHTTP(response, request)

		if service.dmAfter != "dm_prev" || service.limit != 2 {
			t.Fatalf("unexpected dm list after %q limit %d", service.dmAfter, service.limit)
		}
		if !strings.Contains(response.Body.String(), "dm_1") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
		if !strings.Contains(response.Body.String(), "\"page\"") {
			t.Fatalf("expected page metadata in body %s", response.Body.String())
		}
	})

	t.Run("dm get", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/dms/dm_1", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), `"id":"dm_1"`) {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("dm messages", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/dms/dm_1/messages?limit=20&before=msg_dm_older", nil)
		handler.ServeHTTP(response, request)

		if service.dmID != "dm_1" || service.dmBefore != "msg_dm_older" || service.limit != 20 {
			t.Fatalf("unexpected dm lookup %q before %q limit %d", service.dmID, service.dmBefore, service.limit)
		}
		if !strings.Contains(response.Body.String(), "msg_dm") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("artifacts", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/artifacts?thread_id=thread_1&before=art_prev&limit=10", nil)
		handler.ServeHTTP(response, request)

		if service.threadID != "thread_1" || service.roomBefore != "art_prev" || service.limit != 10 {
			t.Fatalf("unexpected artifacts lookup thread=%q before=%q limit=%d", service.threadID, service.roomBefore, service.limit)
		}
		if !strings.Contains(response.Body.String(), "art_1") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("send message", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
			"target":{"kind":"room","room_id":"research"},
			"from":{"type":"agent","id":"orchestrator"},
			"parts":[{"kind":"text","text":"hello"}]
		}`))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusAccepted {
			t.Fatalf("unexpected status %d", response.Code)
		}

		if service.sentMessage.Target.RoomID != "research" {
			t.Fatalf("unexpected sent message %#v", service.sentMessage)
		}
	})

	t.Run("stream events", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
		handler.ServeHTTP(response, request)

		body := response.Body.String()
		if !strings.Contains(body, ": stream-open") || !strings.Contains(body, "id: evt_1") || !strings.Contains(body, "event: message.created") || !strings.Contains(body, "\"id\":\"evt_1\"") {
			t.Fatalf("unexpected stream body %s", body)
		}
	})

	t.Run("stream replay from last event id", func(t *testing.T) {
		replay := make(chan protocol.Event, 1)
		replay <- protocol.Event{
			ID:   "evt_2",
			Type: protocol.EventTypeMessageCreated,
			Message: &protocol.Message{
				ID: "msg_2",
			},
		}
		close(replay)
		service.replayStream = replay
		defer func() { service.replayStream = nil }()

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
		request.Header.Set("Last-Event-ID", "evt_1")
		handler.ServeHTTP(response, request)

		body := response.Body.String()
		if !strings.Contains(body, "id: evt_2") || !strings.Contains(body, "\"id\":\"evt_2\"") {
			t.Fatalf("unexpected replay stream body %s", body)
		}
	})

	t.Run("stream skips unsafe event type", func(t *testing.T) {
		stream := make(chan protocol.Event, 1)
		stream <- protocol.Event{ID: "evt_bad", Type: "bad\nevent"}
		close(stream)
		service.stream = stream

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
		handler.ServeHTTP(response, request)

		body := response.Body.String()
		if strings.Contains(body, "event: bad") || strings.Contains(body, "id: evt_bad") {
			t.Fatalf("expected unsafe event to be skipped, got %s", body)
		}
	})

	t.Run("console redirects and serves", func(t *testing.T) {
		redirect := httptest.NewRecorder()
		redirectRequest := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(redirect, redirectRequest)
		if redirect.Code != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected redirect status %d", redirect.Code)
		}
		if location := redirect.Header().Get("Location"); location != "/console/" {
			t.Fatalf("unexpected redirect location %q", location)
		}

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/console/", nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected console status %d", response.Code)
		}
		if !strings.Contains(response.Body.String(), "Moltnet Console") {
			t.Fatalf("unexpected console body %s", response.Body.String())
		}
	})

	t.Run("metrics", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected metrics status %d", response.Code)
		}
		if !strings.Contains(response.Body.String(), "moltnet_http_requests_total") {
			t.Fatalf("unexpected metrics body %s", response.Body.String())
		}
	})
}

func TestNewHTTPHandlerErrorsAndHelpers(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network:     protocol.Network{ID: "local", Name: "Local"},
		createErr:   fakeStatusError{status: http.StatusConflict, msg: "room already exists"},
		pairingErr:  fakeStatusError{status: http.StatusBadGateway, msg: "paired network request failed"},
		roomError:   fakeStatusError{status: http.StatusNotFound, msg: fmt.Sprintf("unknown room %q", "x")},
		threadError: fakeStatusError{status: http.StatusNotFound, msg: fmt.Sprintf("unknown thread %q", "x")},
		dmError:     fakeStatusError{status: http.StatusUnprocessableEntity, msg: "dm id is required"},
		artifactErr: fakeStatusError{status: http.StatusNotFound, msg: fmt.Sprintf("unknown room %q", "x")},
		sendErr:     errors.New("bad send"),
	}
	handler := NewHTTPHandler(service, nil)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "invalid room json", method: http.MethodPost, path: "/v1/rooms", body: `{"id":1}`, status: http.StatusBadRequest},
		{name: "room create error", method: http.MethodPost, path: "/v1/rooms", body: `{"id":"x"}`, status: http.StatusConflict},
		{name: "invalid room members json", method: http.MethodPatch, path: "/v1/rooms/x/members", body: `{"add":1}`, status: http.StatusBadRequest},
		{name: "room list error", method: http.MethodGet, path: "/v1/rooms/x/messages", status: http.StatusNotFound},
		{name: "pairing network error", method: http.MethodGet, path: "/v1/pairings/pair_1/network", status: http.StatusBadGateway},
		{name: "pairing rooms error", method: http.MethodGet, path: "/v1/pairings/pair_1/rooms", status: http.StatusBadGateway},
		{name: "pairing agents error", method: http.MethodGet, path: "/v1/pairings/pair_1/agents", status: http.StatusBadGateway},
		{name: "thread list error", method: http.MethodGet, path: "/v1/rooms/x/threads", status: http.StatusNotFound},
		{name: "thread messages error", method: http.MethodGet, path: "/v1/threads/x/messages", status: http.StatusNotFound},
		{name: "dm list error", method: http.MethodGet, path: "/v1/dms/x/messages", status: http.StatusUnprocessableEntity},
		{name: "artifacts error", method: http.MethodGet, path: "/v1/artifacts?room_id=x", status: http.StatusNotFound},
		{name: "invalid message json", method: http.MethodPost, path: "/v1/messages", body: `{"target":{}}`, status: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("unexpected status %d", response.Code)
			}
		})
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"target":{"kind":"room","room_id":"research"},
		"from":{"type":"agent","id":"orchestrator"},
		"parts":[{"kind":"text","text":"hello"}]
	}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", response.Code)
	}

	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=0", nil)); got != 100 {
		t.Fatalf("unexpected default limit %d", got)
	}
	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=wat", nil)); got != 100 {
		t.Fatalf("unexpected invalid limit %d", got)
	}
	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=600", nil)); got != 500 {
		t.Fatalf("unexpected capped limit %d", got)
	}
	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=7", nil)); got != 7 {
		t.Fatalf("unexpected limit %d", got)
	}
	page, err := readPageRequest(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?before=msg_7", nil))
	if err != nil {
		t.Fatalf("readPageRequest() error = %v", err)
	}
	if got := page.Before; got != "msg_7" {
		t.Fatalf("unexpected before %q", got)
	}

}
