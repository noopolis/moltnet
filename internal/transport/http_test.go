package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type fakeService struct {
	network     protocol.Network
	agents      []protocol.AgentSummary
	pairings    []protocol.Pairing
	rooms       []protocol.Room
	roomPage    protocol.MessagePage
	dms         []protocol.DirectConversation
	dmPage      protocol.MessagePage
	createdRoom protocol.CreateRoomRequest
	sentMessage protocol.SendMessageRequest
	roomID      string
	roomBefore  string
	dmID        string
	dmBefore    string
	limit       int
	createErr   error
	roomError   error
	dmError     error
	sendErr     error
	stream      chan protocol.Event
}

func (f *fakeService) Network() protocol.Network           { return f.network }
func (f *fakeService) ListAgents() []protocol.AgentSummary { return f.agents }
func (f *fakeService) ListPairings() []protocol.Pairing    { return f.pairings }
func (f *fakeService) ListRooms() []protocol.Room          { return f.rooms }
func (f *fakeService) CreateRoom(request protocol.CreateRoomRequest) (protocol.Room, error) {
	f.createdRoom = request
	if f.createErr != nil {
		return protocol.Room{}, f.createErr
	}
	return protocol.Room{ID: request.ID, Name: request.Name}, nil
}
func (f *fakeService) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	f.roomID = roomID
	f.roomBefore = before
	f.limit = limit
	if f.roomError != nil {
		return protocol.MessagePage{}, f.roomError
	}
	return f.roomPage, nil
}
func (f *fakeService) ListDirectConversations() []protocol.DirectConversation { return f.dms }
func (f *fakeService) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	f.dmID = dmID
	f.dmBefore = before
	f.limit = limit
	if f.dmError != nil {
		return protocol.MessagePage{}, f.dmError
	}
	return f.dmPage, nil
}
func (f *fakeService) SendMessage(request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	f.sentMessage = request
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
				EventStream:       "sse",
				HumanIngress:      true,
				MessagePagination: "cursor",
				Pairings:          true,
			},
		},
		agents: []protocol.AgentSummary{
			{ID: "orchestrator", FQID: "molt://local/agents/orchestrator", NetworkID: "local", Rooms: []string{"research"}},
		},
		pairings: []protocol.Pairing{
			{ID: "pair_1", RemoteNetworkID: "remote", RemoteNetworkName: "Remote Lab", Status: "connected"},
		},
		rooms: []protocol.Room{
			{ID: "research", FQID: "molt://local/rooms/research", Name: "Research"},
		},
		roomPage: protocol.MessagePage{
			Messages: []protocol.Message{
				{ID: "msg_room"},
			},
			Page: protocol.PageInfo{HasMore: true, NextBefore: "msg_room"},
		},
		dms: []protocol.DirectConversation{
			{ID: "dm_1", FQID: "molt://local/dms/dm_1", MessageCount: 1},
		},
		dmPage: protocol.MessagePage{
			Messages: []protocol.Message{
				{ID: "msg_dm"},
			},
			Page: protocol.PageInfo{HasMore: false},
		},
		stream: stream,
	}

	handler := NewHTTPHandler(service)

	t.Run("healthz", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", response.Code)
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
		if payload.Capabilities.MessagePagination != "cursor" {
			t.Fatalf("unexpected capabilities %#v", payload.Capabilities)
		}
	})

	t.Run("rooms list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "research") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("agents list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "orchestrator") {
			t.Fatalf("unexpected body %s", response.Body.String())
		}
	})

	t.Run("pairings list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/pairings", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "Remote Lab") {
			t.Fatalf("unexpected body %s", response.Body.String())
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

	t.Run("dms list", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/dms", nil)
		handler.ServeHTTP(response, request)

		if !strings.Contains(response.Body.String(), "dm_1") {
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
		if !strings.Contains(body, ": stream-open") || !strings.Contains(body, "event: message.created") || !strings.Contains(body, "\"id\":\"evt_1\"") {
			t.Fatalf("unexpected stream body %s", body)
		}
	})

	t.Run("ui redirects and serves", func(t *testing.T) {
		redirect := httptest.NewRecorder()
		redirectRequest := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(redirect, redirectRequest)
		if redirect.Code != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected redirect status %d", redirect.Code)
		}

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/ui/", nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected ui status %d", response.Code)
		}
		if !strings.Contains(response.Body.String(), "Moltnet Console") {
			t.Fatalf("unexpected ui body %s", response.Body.String())
		}
	})
}

func TestNewHTTPHandlerErrorsAndHelpers(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network:   protocol.Network{ID: "local", Name: "Local"},
		createErr: errors.New("bad room"),
		roomError: errors.New("bad room messages"),
		dmError:   errors.New("bad dm"),
		sendErr:   errors.New("bad send"),
	}
	handler := NewHTTPHandler(service)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "invalid room json", method: http.MethodPost, path: "/v1/rooms", body: `{"id":1}`, status: http.StatusBadRequest},
		{name: "room create error", method: http.MethodPost, path: "/v1/rooms", body: `{"id":"x"}`, status: http.StatusBadRequest},
		{name: "room list error", method: http.MethodGet, path: "/v1/rooms/x/messages", status: http.StatusBadRequest},
		{name: "dm list error", method: http.MethodGet, path: "/v1/dms/x/messages", status: http.StatusBadRequest},
		{name: "invalid message json", method: http.MethodPost, path: "/v1/messages", body: `{"target":{}}`, status: http.StatusBadRequest},
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
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d", response.Code)
	}

	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=0", nil)); got != 100 {
		t.Fatalf("unexpected default limit %d", got)
	}
	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=600", nil)); got != 500 {
		t.Fatalf("unexpected capped limit %d", got)
	}
	if got := readLimit(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?limit=7", nil)); got != 7 {
		t.Fatalf("unexpected limit %d", got)
	}
	if got := readBefore(httptest.NewRequest(http.MethodGet, "/v1/rooms/x/messages?before=msg_7", nil)); got != "msg_7" {
		t.Fatalf("unexpected before %q", got)
	}

	errorResponse := httptest.NewRecorder()
	writeMethodNotAllowed(errorResponse, http.MethodGet, http.MethodPost)
	if allow := errorResponse.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("unexpected allow header %q", allow)
	}
}
