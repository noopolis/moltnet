package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPairingDiagnosticsJSON(t *testing.T) {
	t.Parallel()

	pairing := Pairing{
		ID:              "pair_1",
		RemoteNetworkID: "remote",
		Status:          PairingStatusIncompatible,
		Diagnostics: &PairingDiagnostics{
			CheckedAt:       time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
			RemoteVersion:   "0.1.4",
			RemoteNetworkID: "remote-beta",
			RemoteProtocols: NetworkProtocols{
				HTTP: []string{HTTPProtocolV1},
				Pair: []string{},
			},
			Reason:  "unsupported_pair_protocol",
			Message: "Remote server does not advertise moltnet.pair.v1.",
		},
	}

	payload, err := json.Marshal(pairing)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(payload)
	for _, want := range []string{
		`"status":"incompatible"`,
		`"diagnostics"`,
		`"remote_version":"0.1.4"`,
		`"remote_network_id":"remote-beta"`,
		`"http":["moltnet.http.v1"]`,
		`"reason":"unsupported_pair_protocol"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("pairing JSON missing %q: %s", want, text)
		}
	}

	var decoded Pairing
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.Diagnostics == nil ||
		decoded.Diagnostics.RemoteVersion != "0.1.4" ||
		decoded.Diagnostics.Reason != "unsupported_pair_protocol" {
		t.Fatalf("unexpected decoded diagnostics %#v", decoded.Diagnostics)
	}
}
