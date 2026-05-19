package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunAdminRemoveAgentAndRoomUseAdminToken(t *testing.T) {
	var requests []string
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		authHeaders = append(authHeaders, request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/v1/agents/stale-agent":
			_ = json.NewEncoder(response).Encode(protocol.RemoveResult{
				Removed: true,
				Kind:    "agent",
				ID:      "stale-agent",
				Mode:    "soft",
			})
		case "/v1/rooms/stale-room":
			_ = json.NewEncoder(response).Encode(protocol.RemoveResult{
				Removed: true,
				Kind:    "room",
				ID:      "stale-room",
				Mode:    "soft",
			})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "agent", "remove",
			"--base-url", server.URL,
			"--agent", "stale-agent",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() admin agent remove error = %v", err)
		}
	})
	if !strings.Contains(output, `"kind": "agent"`) {
		t.Fatalf("unexpected admin agent remove output %q", output)
	}

	output = captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "room", "remove",
			"--base-url", server.URL,
			"--room", "stale-room",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() admin room remove error = %v", err)
		}
	})
	if !strings.Contains(output, `"kind": "room"`) {
		t.Fatalf("unexpected admin room remove output %q", output)
	}
	if len(requests) != 2 ||
		requests[0] != "DELETE /v1/agents/stale-agent" ||
		requests[1] != "DELETE /v1/rooms/stale-room" {
		t.Fatalf("unexpected requests %#v", requests)
	}
	if authHeaders[0] != "Bearer admin-secret" || authHeaders[1] != "Bearer admin-secret" {
		t.Fatalf("unexpected auth headers %#v", authHeaders)
	}
}

func TestRunApplyPostsDeclarativeConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(configPath, []byte(`
version: moltnet.v1
network:
  id: local
auth:
  mode: bearer
  tokens:
    - id: agent-attachments
      scopes: [attach, write]
      agents: [alpha]
rooms:
  - id: floor
    members: [alpha]
    visibility: public
    write_policy: members
`), 0o600); err != nil {
		t.Fatalf("write Moltnet config: %v", err)
	}

	var received protocol.ApplyConfigRequest
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/apply" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		authHeader = request.Header.Get("Authorization")
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode apply body: %v", err)
		}
		_ = json.NewEncoder(response).Encode(protocol.ApplyConfigResult{Applied: true})
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"apply", configPath,
			"--base-url", server.URL,
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() apply error = %v", err)
		}
	})

	if authHeader != "Bearer admin-secret" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}
	if received.NetworkID != "local" ||
		len(received.Agents) != 1 ||
		received.Agents[0].ID != "alpha" ||
		received.Agents[0].CredentialKey != "token:agent-attachments" ||
		len(received.Rooms) != 1 ||
		received.Rooms[0].ID != "floor" ||
		received.Rooms[0].Members[0] != "alpha" {
		t.Fatalf("unexpected apply request %#v", received)
	}
	if !strings.Contains(output, `"applied": true`) {
		t.Fatalf("unexpected apply output %q", output)
	}
}

func TestRunAdminRoomMembersUpdatesMembership(t *testing.T) {
	var received protocol.UpdateRoomMembersRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPatch || request.URL.Path != "/v1/rooms/floor/members" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode members body: %v", err)
		}
		_ = json.NewEncoder(response).Encode(protocol.Room{ID: "floor", Members: received.Add})
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "room", "members", "add",
			"--base-url", server.URL,
			"--room", "floor",
			"--member", "alpha,beta",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() admin room members add error = %v", err)
		}
	})

	if len(received.Add) != 2 || received.Add[0] != "alpha" || received.Add[1] != "beta" {
		t.Fatalf("unexpected room members request %#v", received)
	}
	if !strings.Contains(output, `"id": "floor"`) {
		t.Fatalf("unexpected room members output %q", output)
	}
}
