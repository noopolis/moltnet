package rooms

import (
	"testing"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func newPendingTestService() *Service {
	return NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Pairings: []protocol.Pairing{
			{ID: "pair_1", RemoteNetworkID: "remote", RemoteBaseURL: "https://remote.example.com"},
		},
		Store:    nil,
		Messages: nil,
		Broker:   events.NewBroker(),
	})
}

func TestSetPairingErrorNeverReachedReportsPendingWithoutWarning(t *testing.T) {
	t.Parallel()

	service := newPendingTestService()
	service.setPairingError("pair_1", "dial tcp: connection refused")

	service.pairingsMu.RLock()
	status := service.pairingStatuses["pair_1"]
	warnings := service.networkWarningsLocked()
	service.pairingsMu.RUnlock()

	if status.value != protocol.PairingStatusPending {
		t.Fatalf("expected pending status for a never-reached pairing, got %q", status.value)
	}
	if status.everReached {
		t.Fatalf("expected everReached to stay false for a never-reached pairing")
	}
	for _, warning := range warnings {
		if warning.Code == "pairings.error" {
			t.Fatalf("expected no operator warning for a pending pairing, got %#v", warnings)
		}
	}
}

func TestSetPairingErrorAfterConnectionReportsErrorAndWarns(t *testing.T) {
	t.Parallel()

	service := newPendingTestService()
	service.setPairingStatus("pair_1", protocol.PairingStatusConnected)
	service.setPairingError("pair_1", "dial tcp: connection refused")

	service.pairingsMu.RLock()
	status := service.pairingStatuses["pair_1"]
	warnings := service.networkWarningsLocked()
	service.pairingsMu.RUnlock()

	if status.value != protocol.PairingStatusError {
		t.Fatalf("expected error status once a pairing has previously connected, got %q", status.value)
	}
	if !status.everReached {
		t.Fatalf("expected everReached to stay true once a pairing has previously connected")
	}
	found := false
	for _, warning := range warnings {
		if warning.Code == "pairings.error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an operator warning for an errored pairing that previously connected, got %#v", warnings)
	}
}

func TestEverReachedSurvivesNoChangeShortCircuit(t *testing.T) {
	t.Parallel()

	service := newPendingTestService()
	service.setPairingStatus("pair_1", protocol.PairingStatusConnected)

	// Repeating the same status hits the no-change short-circuit in
	// setPairingRuntime, which must not drop everReached along the way.
	service.setPairingStatus("pair_1", protocol.PairingStatusConnected)

	service.pairingsMu.RLock()
	afterShortCircuit := service.pairingStatuses["pair_1"]
	service.pairingsMu.RUnlock()
	if !afterShortCircuit.everReached {
		t.Fatalf("expected everReached to survive the no-change short-circuit")
	}

	service.setPairingError("pair_1", "dial tcp: connection refused")

	service.pairingsMu.RLock()
	status := service.pairingStatuses["pair_1"]
	service.pairingsMu.RUnlock()
	if status.value != protocol.PairingStatusError {
		t.Fatalf("expected error status after everReached survived the short-circuit, got %q", status.value)
	}
}
