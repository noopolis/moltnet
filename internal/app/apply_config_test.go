package app

import (
	"path/filepath"
	"testing"
)

func TestLoadApplyFileBuildsDeclarativeRequest(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: local
auth:
  mode: bearer
  tokens:
    - id: attachment-token
      scopes: [attach, write]
      agents: [alpha, beta, alpha]
rooms:
  - id: floor
    name: Floor
    visibility: public
    write_policy: registered_agents
    members: [alpha]
`)

	request, resolvedPath, err := LoadApplyFile(path)
	if err != nil {
		t.Fatalf("LoadApplyFile() error = %v", err)
	}
	if resolvedPath != path {
		t.Fatalf("resolved path = %q, want %q", resolvedPath, path)
	}
	if request.NetworkID != "local" {
		t.Fatalf("network id = %q, want local", request.NetworkID)
	}
	if len(request.Agents) != 2 {
		t.Fatalf("agents len = %d, want 2: %#v", len(request.Agents), request.Agents)
	}
	if request.Agents[0].ID != "alpha" || request.Agents[0].CredentialKey != "token:attachment-token" {
		t.Fatalf("unexpected first agent %#v", request.Agents[0])
	}
	if request.Agents[1].ID != "beta" || request.Agents[1].CredentialKey != "token:attachment-token" {
		t.Fatalf("unexpected second agent %#v", request.Agents[1])
	}
	if len(request.Rooms) != 1 {
		t.Fatalf("rooms len = %d, want 1: %#v", len(request.Rooms), request.Rooms)
	}
	room := request.Rooms[0]
	if room.ID != "floor" || room.Name != "Floor" || room.Visibility != "public" || room.WritePolicy != "registered_agents" {
		t.Fatalf("unexpected room %#v", room)
	}
	if len(room.Members) != 1 || room.Members[0] != "alpha" {
		t.Fatalf("unexpected members %#v", room.Members)
	}
}

func TestLoadApplyFileRejectsAmbiguousAgentCredential(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Moltnet")
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: local
auth:
  mode: bearer
  tokens:
    - id: one
      agents: [alpha]
    - id: two
      agents: [alpha]
`)

	if _, _, err := LoadApplyFile(path); err == nil {
		t.Fatal("expected duplicate agent credential error")
	}
}

func TestValidateApplyAuthRequiresTokenIdentityForDeclaredAgents(t *testing.T) {
	t.Parallel()

	err := validateApplyAuth(rawAuthConfig{
		Mode: "bearer",
		Tokens: []rawAuthTokenConfig{
			{Agents: []string{"alpha"}},
		},
	})
	if err == nil {
		t.Fatal("expected missing token id error")
	}
}

func TestValidateApplyAuthRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	if err := validateApplyAuth(rawAuthConfig{Mode: "wat"}); err == nil {
		t.Fatal("expected invalid auth mode error")
	}
	if err := validateApplyAuth(rawAuthConfig{AgentRegistration: "wat"}); err == nil {
		t.Fatal("expected invalid agent registration error")
	}
	if err := validateApplyAuth(rawAuthConfig{
		Tokens: []rawAuthTokenConfig{
			{ID: "attachments", Agents: []string{"not ok"}},
		},
	}); err == nil {
		t.Fatal("expected invalid agent id error")
	}
}
