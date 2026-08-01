package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestPairingRelayJSONAndYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	want := Pairing{ID: "pair", Relay: &PairingRelay{URL: "wss://relay.example.com", Room: "lobby"}}
	jsonBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var gotJSON Pairing
	if err := json.Unmarshal(jsonBytes, &gotJSON); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if gotJSON.Relay == nil || *gotJSON.Relay != *want.Relay {
		t.Fatalf("JSON relay = %#v, want %#v", gotJSON.Relay, want.Relay)
	}

	yamlBytes, err := yaml.Marshal(want)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	var gotYAML Pairing
	if err := yaml.Unmarshal(yamlBytes, &gotYAML); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if gotYAML.Relay == nil || *gotYAML.Relay != *want.Relay {
		t.Fatalf("YAML relay = %#v, want %#v", gotYAML.Relay, want.Relay)
	}
}

func TestValidateTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target Target
		ok     bool
	}{
		{
			name:   "room ok",
			target: Target{Kind: TargetKindRoom, RoomID: "research"},
			ok:     true,
		},
		{
			name:   "thread ok",
			target: Target{Kind: TargetKindThread, ThreadID: "thr_1", RoomID: "research", ParentMessageID: "msg_1"},
			ok:     true,
		},
		{
			name:   "dm ok",
			target: Target{Kind: TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"orchestrator", "molt://remote/agents/researcher"}},
			ok:     true,
		},
		{
			name:   "room missing id",
			target: Target{Kind: TargetKindRoom},
		},
		{
			name:   "thread missing id",
			target: Target{Kind: TargetKindThread},
		},
		{
			name:   "thread missing room",
			target: Target{Kind: TargetKindThread, ThreadID: "thr_1"},
		},
		{
			name:   "thread invalid id",
			target: Target{Kind: TargetKindThread, ThreadID: "bad thread", RoomID: "research"},
		},
		{
			name:   "dm missing id",
			target: Target{Kind: TargetKindDM, ParticipantIDs: []string{"orchestrator", "researcher"}},
		},
		{
			name:   "dm missing participants",
			target: Target{Kind: TargetKindDM, DMID: "dm_1"},
		},
		{
			name:   "dm too few participants",
			target: Target{Kind: TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"orchestrator"}},
		},
		{
			name:   "dm invalid participant",
			target: Target{Kind: TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"orchestrator", "bad\nparticipant"}},
		},
		{
			name:   "unsupported kind",
			target: Target{Kind: "weird"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateTarget(test.target)
			if test.ok && err != nil {
				t.Fatalf("expected success, got %v", err)
			}

			if !test.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestUniqueParticipantIDs(t *testing.T) {
	t.Parallel()

	participants := UniqueTrimmedStrings([]string{" writer ", "researcher", "writer", "", "researcher"})
	if len(participants) != 2 || participants[0] != "writer" || participants[1] != "researcher" {
		t.Fatalf("unexpected participants %#v", participants)
	}
}

func TestAgentRegistrationAgentTokenJSON(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(AgentRegistration{
		NetworkID:     "local",
		AgentID:       "luna",
		ActorUID:      "actor_1",
		ActorURI:      AgentFQID("local", "luna"),
		AgentToken:    "magt_v1_secret",
		CredentialKey: "agent-token:hash",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, `"agent_token":"magt_v1_secret"`) {
		t.Fatalf("expected agent_token in JSON %s", text)
	}
	if strings.Contains(text, "credential") || strings.Contains(text, "agent-token:hash") {
		t.Fatalf("credential key leaked in JSON %s", text)
	}
}

func TestActorCredentialBoundJSONIsAdditive(t *testing.T) {
	t.Parallel()

	legacy, err := json.Marshal(Actor{Type: "agent", ID: "world"})
	if err != nil {
		t.Fatalf("Marshal() legacy error = %v", err)
	}
	if strings.Contains(string(legacy), "credential_bound") {
		t.Fatalf("zero-value provenance should stay omitted: %s", legacy)
	}
	stamped, err := json.Marshal(Actor{Type: "agent", ID: "world", CredentialBound: true})
	if err != nil {
		t.Fatalf("Marshal() stamped error = %v", err)
	}
	if !strings.Contains(string(stamped), `"credential_bound":true`) {
		t.Fatalf("stamped provenance missing from JSON: %s", stamped)
	}
	var decoded Actor
	if err := json.Unmarshal([]byte(`{"type":"agent","id":"world"}`), &decoded); err != nil {
		t.Fatalf("Unmarshal() legacy error = %v", err)
	}
	if decoded.CredentialBound {
		t.Fatalf("legacy actor unexpectedly bound: %#v", decoded)
	}
}
