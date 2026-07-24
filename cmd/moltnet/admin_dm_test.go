package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunAdminEnsureDMInstallsControlOnlyTopology(t *testing.T) {
	t.Setenv("MOLTNET_ADMIN_TOKEN", "operator-secret")
	var received protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer operator-secret" {
			t.Fatalf("unexpected authorization header")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(response).Encode(protocol.MessageAccepted{
			MessageID: received.ID,
			EventID:   "evt_topology",
			Accepted:  true,
			DMCreated: true,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	if err := runAdminEnsureDM([]string{
		"--sender", "world",
		"--member", "world",
		"--member", "red",
		"--base-url", server.URL,
		"--token-env", "MOLTNET_ADMIN_TOKEN",
	}); err != nil {
		t.Fatalf("runAdminEnsureDM() error = %v", err)
	}
	if received.ID != "dm_topology_6238b3bbafa0ba450338df5edec3dede" {
		t.Fatalf("unexpected message id %q", received.ID)
	}
	if received.Target.DMID != "dm_6238b3bbafa0ba450338df5edec3dede" ||
		!reflect.DeepEqual(received.Target.ParticipantIDs, []string{"red", "world"}) {
		t.Fatalf("unexpected target %#v", received.Target)
	}
	if received.From.ID != "world" || received.From.Type != "agent" {
		t.Fatalf("unexpected sender %#v", received.From)
	}
	if len(received.Parts) != 1 || received.Parts[0].Kind != protocol.PartKindData ||
		received.Parts[0].Data["control_marker"] != dmTopologyControlMarker {
		t.Fatalf("unexpected control part %#v", received.Parts)
	}
}

func TestCanonicalDMTopologyMembersRejectsInvalidTopology(t *testing.T) {
	for _, test := range []struct {
		name    string
		members []string
	}{
		{name: "missing", members: nil},
		{name: "one", members: []string{"red"}},
		{name: "three", members: []string{"red", "world", "blue"}},
		{name: "duplicate", members: []string{"red", "red"}},
		{name: "invalid", members: []string{"red", "bad member"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := canonicalDMTopologyMembers(test.members); err == nil {
				t.Fatal("expected invalid topology to fail")
			}
		})
	}
}
