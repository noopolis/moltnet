package protocol

import (
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestRoomFederationWireStancesAndPairingAccess(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		input   string
		pairing string
		allowed bool
	}{
		{name: "none", input: `"none"`, pairing: "pair_a"},
		{name: "all", input: `"all"`, pairing: "pair_a", allowed: true},
		{name: "list", input: `["pair_a","pair_b"]`, pairing: "pair_a", allowed: true},
		{name: "list rejects another pairing", input: `["pair_a"]`, pairing: "pair_b"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var federation RoomFederation
			if err := json.Unmarshal([]byte(test.input), &federation); err != nil {
				t.Fatalf("json.Unmarshal(%s): %v", test.input, err)
			}
			if got := RoomFederationAllows(&federation, test.pairing); got != test.allowed {
				t.Fatalf("RoomFederationAllows(%#v, %q) = %v, want %v", federation, test.pairing, got, test.allowed)
			}
			encoded, err := yaml.Marshal(federation)
			if err != nil {
				t.Fatalf("yaml.Marshal(): %v", err)
			}
			var roundTripped RoomFederation
			if err := yaml.Unmarshal(encoded, &roundTripped); err != nil {
				t.Fatalf("yaml.Unmarshal(): %v", err)
			}
			if got := RoomFederationAllows(&roundTripped, test.pairing); got != test.allowed {
				t.Fatalf("YAML round trip access = %v, want %v", got, test.allowed)
			}
		})
	}
}
