package loop

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunControlLoop(t *testing.T) {
	t.Parallel()

	var controlBodies []string
	var published int
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	event := protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: "room", RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions:  []string{"researcher"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello @reviewer"}},
			CreatedAt: time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
		},
		CreatedAt: time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
	}
	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			connection, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Fatalf("upgrade websocket: %v", err)
			}
			defer connection.Close()

			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:                  protocol.AttachmentOpHello,
				Version:             protocol.AttachmentProtocolV1,
				HeartbeatIntervalMS: 30000,
			}); err != nil {
				t.Fatalf("write hello: %v", err)
			}

			var identify protocol.AttachmentFrame
			if err := connection.ReadJSON(&identify); err != nil {
				t.Fatalf("read identify: %v", err)
			}
			if identify.Op != protocol.AttachmentOpIdentify || identify.Agent == nil || identify.Agent.ID != "researcher" {
				t.Fatalf("unexpected identify %#v", identify)
			}

			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpReady,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				AgentID:   "researcher",
			}); err != nil {
				t.Fatalf("write ready: %v", err)
			}

			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				Cursor:    "evt_1",
				Event:     &event,
			}); err != nil {
				t.Fatalf("write event: %v", err)
			}

			var ack protocol.AttachmentFrame
			if err := connection.ReadJSON(&ack); err != nil {
				t.Fatalf("read ack: %v", err)
			}
			if ack.Op != protocol.AttachmentOpAck || ack.Cursor != "evt_1" {
				t.Fatalf("unexpected ack %#v", ack)
			}

			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				time.Now().Add(time.Second),
			)
		case "/v1/messages":
			published++
			response.WriteHeader(http.StatusBadGateway)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moltnetServer.Close()

	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read control body: %v", err)
		}
		controlBodies = append(controlBodies, string(body))
		response.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), `"from":"Moltnet Bootstrap"`) {
			_, _ = response.Write([]byte(`{"from":"researcher","message":""}`))
			return
		}
		_, _ = response.Write([]byte(`{"from":"researcher","message":"@reviewer done"}`))
	}))
	defer controlServer.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   moltnetServer.URL,
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			ControlURL: controlServer.URL,
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto},
		},
	}

	if err := RunControlLoop(context.Background(), config); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}

	if len(controlBodies) != 2 {
		t.Fatalf("unexpected control requests %#v", controlBodies)
	}

	var sawBootstrap bool
	var sawInbound bool
	for _, body := range controlBodies {
		if !strings.Contains(body, "\"to\":\"researcher\"") {
			t.Fatalf("unexpected control target in %#v", controlBodies)
		}
		if !strings.Contains(body, "\"context_id\":\"moltnet:local:room:research\"") {
			t.Fatalf("expected stable room context id, got %#v", controlBodies)
		}
		if strings.Contains(body, "\"from\":\"Moltnet Bootstrap\"") {
			sawBootstrap = true
		}
		if strings.Contains(body, "\"from\":\"Writer\"") {
			sawInbound = true
		}
	}
	if !sawBootstrap || !sawInbound {
		t.Fatalf("expected bootstrap and inbound control requests, got %#v", controlBodies)
	}
	if published != 0 {
		t.Fatalf("expected control loop responses to stay off Moltnet, got %d sends", published)
	}
}

func TestRunControlLoopReconnectsAfterAttachFailure(t *testing.T) {
	t.Parallel()

	var attachRequests int
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			attachRequests++
			if attachRequests == 1 {
				response.WriteHeader(http.StatusBadGateway)
				return
			}

			connection, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Fatalf("upgrade websocket: %v", err)
			}
			defer connection.Close()

			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:                  protocol.AttachmentOpHello,
				Version:             protocol.AttachmentProtocolV1,
				HeartbeatIntervalMS: 30000,
			}); err != nil {
				t.Fatalf("write hello: %v", err)
			}
			var identify protocol.AttachmentFrame
			if err := connection.ReadJSON(&identify); err != nil {
				t.Fatalf("read identify: %v", err)
			}
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpReady,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				AgentID:   "researcher",
			}); err != nil {
				t.Fatalf("write ready: %v", err)
			}
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				Cursor:    "evt_1",
				Event: &protocol.Event{
					ID:        "evt_1",
					Type:      protocol.EventTypeMessageCreated,
					NetworkID: "local",
					Message: &protocol.Message{
						ID:        "msg_1",
						NetworkID: "local",
						Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
						From:      protocol.Actor{Type: "agent", ID: "writer"},
						Mentions:  []string{"researcher"},
						Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
						CreatedAt: time.Now().UTC(),
					},
					CreatedAt: time.Now().UTC(),
				},
			}); err != nil {
				t.Fatalf("write event: %v", err)
			}

			var ack protocol.AttachmentFrame
			if err := connection.ReadJSON(&ack); err != nil {
				t.Fatalf("read ack: %v", err)
			}
			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				time.Now().Add(time.Second),
			)
		case "/v1/messages":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_2","event_id":"evt_2","accepted":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moltnetServer.Close()

	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"from":"researcher","message":"done"}`))
	}))
	defer controlServer.Close()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: moltnetServer.URL, NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: controlServer.URL},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto}},
	}

	if err := RunControlLoop(context.Background(), config); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}
	if attachRequests < 2 {
		t.Fatalf("expected reconnect after attach failure, got %d requests", attachRequests)
	}
}

func TestRunControlLoopCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := RunControlLoop(ctx, bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: "http://control"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet", NetworkID: "local"},
	}); err != nil {
		t.Fatalf("expected nil on cancelled context, got %v", err)
	}
}

func TestSendControlMessageErrors(t *testing.T) {
	t.Parallel()

	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer controlServer.Close()

	_, err := sendControlMessage(context.Background(), &http.Client{Timeout: time.Second}, bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: controlServer.URL},
	}, protocol.Event{
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"},
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			CreatedAt: time.Now().UTC(),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "control url returned") {
		t.Fatalf("expected control url error, got %v", err)
	}

	_, err = sendControlMessage(context.Background(), &http.Client{Timeout: time.Second}, bridgeconfig.Config{}, protocol.Event{})
	if err == nil || !strings.Contains(err.Error(), "event has no message") {
		t.Fatalf("expected missing message error, got %v", err)
	}

	invalidResponseServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"from":`))
	}))
	defer invalidResponseServer.Close()

	_, err = sendControlMessage(context.Background(), &http.Client{Timeout: time.Second}, bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: invalidResponseServer.URL},
	}, protocol.Event{
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Now().UTC(),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "decode control response") {
		t.Fatalf("expected invalid response decode error, got %v", err)
	}
}

func TestSendControlMessageUsesStableDMContextID(t *testing.T) {
	t.Parallel()

	var requestBody string
	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read control body: %v", err)
		}
		requestBody = string(body)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"from":"researcher","message":"ok"}`))
	}))
	defer controlServer.Close()

	_, err := sendControlMessage(context.Background(), &http.Client{Timeout: time.Second}, bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: controlServer.URL},
	}, protocol.Event{
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target: protocol.Target{
				Kind:           protocol.TargetKindDM,
				DMID:           "dm-orchestrator-researcher",
				ParticipantIDs: []string{"orchestrator", "researcher"},
			},
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("sendControlMessage() error = %v", err)
	}

	if !strings.Contains(requestBody, "\"context_id\":\"moltnet:local:dm:dm-orchestrator-researcher\"") {
		t.Fatalf("expected stable dm context id, got %q", requestBody)
	}
}

func TestSendControlMessageIncludesRuntimeAuthToken(t *testing.T) {
	t.Parallel()

	var authorization string
	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"from":"researcher","message":"ok"}`))
	}))
	defer controlServer.Close()

	_, err := sendControlMessage(context.Background(), &http.Client{Timeout: time.Second}, bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: controlServer.URL, Token: "runtime-secret"},
	}, protocol.Event{
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("sendControlMessage() error = %v", err)
	}
	if authorization != "Bearer runtime-secret" {
		t.Fatalf("unexpected authorization header %q", authorization)
	}
}

func TestBuildBootstrapControlMessage(t *testing.T) {
	t.Parallel()

	message := buildBootstrapControlMessage(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
	}, protocol.Target{
		Kind:   protocol.TargetKindRoom,
		RoomID: "research",
	})

	if !strings.Contains(message, "Moltnet bootstrap delivery.") {
		t.Fatalf("expected bootstrap preamble, got %q", message)
	}
	if !strings.Contains(message, "execute that startup action now") {
		t.Fatalf("expected explicit startup instruction, got %q", message)
	}
	if !strings.Contains(message, `"kind":"bootstrap"`) {
		t.Fatalf("expected bootstrap payload marker, got %q", message)
	}
	if !strings.Contains(message, `"conversation":"moltnet:local:room:research"`) {
		t.Fatalf("expected stable bootstrap conversation id, got %q", message)
	}
}
