package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAppHTTPIntegrationEndToEnd(t *testing.T) {
	t.Parallel()

	instance, err := New(Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local Moltnet",
		Version:     "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer instance.close()

	server := httptest.NewServer(instance.server.Handler)
	defer server.Close()

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()

	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodGet, server.URL+"/v1/events/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}

	streamResponse, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		t.Fatalf("open sse stream: %v", err)
	}
	defer streamResponse.Body.Close()
	if streamResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stream status %d", streamResponse.StatusCode)
	}

	lines := make(chan string, 32)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(streamResponse.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		readErr <- scanner.Err()
	}()

	waitForSSELine(t, lines, ": stream-open")

	createRoomStatus := postJSON(t, server.URL+"/v1/rooms", protocol.CreateRoomRequest{
		ID:      "research",
		Name:    "Research",
		Members: []string{"orchestrator", "writer"},
	})
	if createRoomStatus != http.StatusCreated {
		t.Fatalf("unexpected room create status %d", createRoomStatus)
	}

	sendMessageStatus := postJSON(t, server.URL+"/v1/messages", protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello world"}},
	})
	if sendMessageStatus != http.StatusAccepted {
		t.Fatalf("unexpected send status %d", sendMessageStatus)
	}

	response, err := http.Get(server.URL + "/v1/rooms/research/messages")
	if err != nil {
		t.Fatalf("get room messages: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected room messages status %d", response.StatusCode)
	}
	var page protocol.MessagePage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode room messages: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Parts[0].Text != "hello world" {
		t.Fatalf("unexpected room messages %#v", page)
	}

	waitForSSELine(t, lines, "event: "+protocol.EventTypeRoomCreated)
	waitForSSELine(t, lines, "event: "+protocol.EventTypeMessageCreated)

	cancelStream()
	select {
	case err := <-readErr:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("unexpected stream read error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sse reader shutdown")
	}
}

func postJSON(t *testing.T, endpoint string, payload any) int {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post %s: %v", endpoint, err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func waitForSSELine(t *testing.T, lines <-chan string, want string) {
	t.Helper()

	timeout := time.After(2 * time.Second)
	for {
		select {
		case line := <-lines:
			if line == want {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for sse line %q", want)
		}
	}
}
