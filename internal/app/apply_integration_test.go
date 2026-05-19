package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAppHTTPApplyReconcilesStaticAgentsAndMembershipDrift(t *testing.T) {
	t.Parallel()

	sqlitePath := filepath.Join(t.TempDir(), "moltnet.db")
	openInstance, err := New(Config{
		Auth:        authn.Config{Mode: authn.ModeOpen},
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Rooms: []RoomConfig{{
			ID:          "floor",
			Members:     []string{"alpha"},
			WritePolicy: protocol.RoomWritePolicyMembers,
		}},
		Storage: StorageConfig{
			Kind:   storageKindSQLite,
			SQLite: SQLiteStorageConfig{Path: sqlitePath},
		},
		Version: "test",
	})
	if err != nil {
		t.Fatalf("New(open) error = %v", err)
	}
	openServer := httptest.NewServer(openInstance.server.Handler)

	var openRegistration protocol.AgentRegistration
	status := postJSONWithToken(t, openServer.URL+"/v1/agents/register", "", protocol.RegisterAgentRequest{
		RequestedAgentID: "alpha",
	}, &openRegistration)
	if status != http.StatusCreated || openRegistration.AgentToken == "" {
		t.Fatalf("unexpected open registration status=%d value=%#v", status, openRegistration)
	}
	openServer.Close()
	openInstance.close()

	bearerInstance, err := New(Config{
		Auth: authn.Config{
			Mode: authn.ModeBearer,
			Tokens: []authn.TokenConfig{
				{
					ID:     "admin",
					Value:  "admin-secret",
					Scopes: []authn.Scope{authn.ScopeAdmin, authn.ScopeWrite, authn.ScopeObserve},
				},
				{
					ID:     "agent-attachments",
					Value:  "agent-secret",
					Scopes: []authn.Scope{authn.ScopeAttach, authn.ScopeWrite, authn.ScopeObserve},
					Agents: []string{"alpha"},
				},
			},
		},
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Rooms: []RoomConfig{{
			ID:          "floor",
			Members:     []string{"alpha"},
			WritePolicy: protocol.RoomWritePolicyMembers,
		}},
		Storage: StorageConfig{
			Kind:   storageKindSQLite,
			SQLite: SQLiteStorageConfig{Path: sqlitePath},
		},
		Version: "test",
	})
	if err != nil {
		t.Fatalf("New(bearer) error = %v", err)
	}
	defer bearerInstance.close()
	server := httptest.NewServer(bearerInstance.server.Handler)
	defer server.Close()

	status = postAgentMessage(t, server.URL, "agent-secret", "alpha", "after startup apply")
	if status != http.StatusAccepted {
		t.Fatalf("expected startup apply to rebind credential, send status %d", status)
	}

	status = deleteWithToken(t, server.URL+"/v1/agents/alpha", "admin-secret")
	if status != http.StatusOK {
		t.Fatalf("unexpected agent delete status %d", status)
	}
	status = postAgentMessage(t, server.URL, "agent-secret", "alpha", "missing room membership")
	if status == http.StatusAccepted {
		t.Fatalf("expected removed registration or membership to fail")
	}

	status = postJSONWithToken(t, server.URL+"/v1/apply", "admin-secret", protocol.ApplyConfigRequest{
		NetworkID: "local",
		Agents: []protocol.ApplyAgentRequest{{
			ID:            "alpha",
			CredentialKey: authn.StaticCredentialKey("agent-attachments"),
		}},
		Rooms: []protocol.CreateRoomRequest{{
			ID:          "floor",
			Members:     []string{"alpha"},
			WritePolicy: protocol.RoomWritePolicyMembers,
		}},
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("unexpected apply status %d", status)
	}
	status = postAgentMessage(t, server.URL, "agent-secret", "alpha", "membership restored")
	if status != http.StatusAccepted {
		t.Fatalf("expected apply to restore membership, send status %d", status)
	}
}

func postAgentMessage(t *testing.T, baseURL string, token string, agentID string, text string) int {
	t.Helper()

	return postJSONWithToken(t, baseURL+"/v1/messages", token, protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "floor"},
		From:   protocol.Actor{Type: "agent", ID: agentID},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: text}},
	}, nil)
}

func deleteWithToken(t *testing.T, endpoint string, token string) int {
	t.Helper()

	request, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("delete %s: %v", endpoint, err)
	}
	defer response.Body.Close()
	return response.StatusCode
}
