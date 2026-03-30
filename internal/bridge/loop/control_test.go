package loop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunControlLoop(t *testing.T) {
	t.Parallel()

	var controlBodies []string
	var published []protocol.SendMessageRequest
	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/events/stream":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: message.created\n"))
			_, _ = response.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"message.created\",\"network_id\":\"local\",\"message\":{\"id\":\"msg_1\",\"network_id\":\"local\",\"target\":{\"kind\":\"room\",\"room_id\":\"research\"},\"from\":{\"type\":\"agent\",\"id\":\"writer\",\"name\":\"Writer\"},\"mentions\":[\"researcher\"],\"parts\":[{\"kind\":\"text\",\"text\":\"hello @reviewer\"}],\"created_at\":\"2026-03-30T12:00:00Z\"},\"created_at\":\"2026-03-30T12:00:00Z\"}\n\n"))
		case "/v1/messages":
			var payload protocol.SendMessageRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode published message: %v", err)
			}
			published = append(published, payload)
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_2","event_id":"evt_2","accepted":true}`))
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

	if len(controlBodies) != 1 || !strings.Contains(controlBodies[0], "\"to\":\"researcher\"") {
		t.Fatalf("unexpected control requests %#v", controlBodies)
	}
	if len(published) != 1 || len(published[0].Mentions) != 1 || published[0].Mentions[0] != "reviewer" {
		t.Fatalf("unexpected published messages %#v", published)
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

	_, err := sendControlMessage(context.Background(), bridgeconfig.Config{
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
}

func TestMoltnetClientStreamAndSendErrors(t *testing.T) {
	t.Parallel()

	badStream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("expected auth header, got %q", request.Header.Get("Authorization"))
		}
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer badStream.Close()

	client := &moltnetClient{
		baseURL: badStream.URL,
		client:  &http.Client{},
		token:   "secret",
	}

	if err := client.streamEvents(context.Background(), func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "moltnet stream returned") {
		t.Fatalf("expected stream request error, got %v", err)
	}

	invalidStream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("event: message.created\n"))
		_, _ = response.Write([]byte("data: {bad json}\n\n"))
	}))
	defer invalidStream.Close()

	client.baseURL = invalidStream.URL
	client.token = ""
	if err := client.streamEvents(context.Background(), func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "decode moltnet event") {
		t.Fatalf("expected stream decode error, got %v", err)
	}

	failingSend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("expected auth header on send, got %q", request.Header.Get("Authorization"))
		}
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer failingSend.Close()

	client.baseURL = failingSend.URL
	client.token = "secret"
	_, err := client.sendMessage(context.Background(), protocol.SendMessageRequest{
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
	})
	if err == nil || !strings.Contains(err.Error(), "moltnet message send returned") {
		t.Fatalf("expected send error, got %v", err)
	}

	invalidSend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"message_id":`))
	}))
	defer invalidSend.Close()

	client.baseURL = invalidSend.URL
	client.token = ""
	_, err = client.sendMessage(context.Background(), protocol.SendMessageRequest{
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
	})
	if err == nil || !strings.Contains(err.Error(), "decode moltnet message response") {
		t.Fatalf("expected send decode error, got %v", err)
	}
}
