package loop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetClientPersistsReadyAgentToken(t *testing.T) {
	var sendAuth string
	tokenPath := filepath.Join(t.TempDir(), ".moltnet", "alpha.token")
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			if request.Header.Get("Authorization") != "" {
				t.Fatalf("expected anonymous first attach, got %q", request.Header.Get("Authorization"))
			}
			connection, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Fatalf("upgrade websocket: %v", err)
			}
			defer connection.Close()
			writeHello(t, connection)
			readIdentify(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:         protocol.AttachmentOpReady,
				Version:    protocol.AttachmentProtocolV1,
				NetworkID:  "local",
				AgentID:    "alpha",
				AgentToken: "magt_v1_alpha",
			}); err != nil {
				t.Fatalf("write ready: %v", err)
			}
			_ = connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), timeNow())
		case "/v1/messages":
			sendAuth = request.Header.Get("Authorization")
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{Accepted: true})
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:  bridgeconfig.AuthModeOpen,
			BaseURL:   server.URL,
			NetworkID: "local",
			TokenPath: tokenPath,
		},
	}
	client := NewMoltnetClient(config)
	if err := client.StreamEvents(context.Background(), config, func(protocol.Event) error { return nil }); err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
	contents, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	if string(contents) != "magt_v1_alpha\n" {
		t.Fatalf("unexpected persisted token %q", contents)
	}

	_, err = client.SendMessage(context.Background(), protocol.SendMessageRequest{
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if sendAuth != "Bearer magt_v1_alpha" {
		t.Fatalf("unexpected send auth %q", sendAuth)
	}
}

func TestMoltnetClientRejectsReadyTokenWithoutTokenPath(t *testing.T) {
	server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
		writeHello(t, connection)
		readIdentify(t, connection)
		if err := connection.WriteJSON(protocol.AttachmentFrame{
			Op:         protocol.AttachmentOpReady,
			AgentToken: "magt_v1_alpha",
		}); err != nil {
			t.Fatalf("write ready: %v", err)
		}
	})
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:  bridgeconfig.AuthModeOpen,
			BaseURL:   server.URL,
			NetworkID: "local",
			Token:     "operator-secret",
		},
	}
	client := NewMoltnetClient(config)
	err := client.StreamEvents(context.Background(), config, func(protocol.Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "token_path is not configured") {
		t.Fatalf("expected token_path error, got %v", err)
	}
}

func TestMoltnetClientPreflightsOpenClaimTokenPath(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called = true
		response.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:  bridgeconfig.AuthModeOpen,
			BaseURL:   server.URL,
			NetworkID: "local",
			TokenPath: filepath.Join(blocker, "alpha.token"),
		},
	}

	client := NewMoltnetClient(config)
	err := client.StreamEvents(context.Background(), config, func(protocol.Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "token_path") {
		t.Fatalf("expected token_path preflight error, got %v", err)
	}
	if called {
		t.Fatal("server was contacted before token_path preflight succeeded")
	}
}
