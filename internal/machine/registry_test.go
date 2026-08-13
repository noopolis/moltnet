package machine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestLifecycleRegistryRejectsDuplicateAndCapacity(t *testing.T) {
	req := newRequestLifecycleRegistry(2, 2)
	state, err := req.register("corr_1", protocol.MachineOpRead)
	if err != nil || state != registerStateAccepted {
		t.Fatalf("expected accepted registration, got state=%v err=%v", state, err)
	}
	if _, err := req.register("corr_1", protocol.MachineOpRead); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	if _, err := req.register("corr_2", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if _, err := req.register("corr_3", protocol.MachineOpRead); err == nil {
		t.Fatal("expected lifetime capacity error")
	}
}

func TestLifecycleRegistryActiveCapacity(t *testing.T) {
	req := newRequestLifecycleRegistry(1, 3)
	state, err := req.register("corr_1", protocol.MachineOpRead)
	if err != nil || state != registerStateAccepted {
		t.Fatalf("unexpected register: state=%v err=%v", state, err)
	}
	if err := req.activate("corr_1", func() {}); err != nil {
		t.Fatalf("unexpected activate error: %v", err)
	}
	if _, err := req.register("corr_2", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if err := req.activate("corr_2", func() {}); err == nil {
		t.Fatal("expected active capacity error")
	}
}

func TestLifecycleRegistryDuplicateClaimDeterministic(t *testing.T) {
	req := newRequestLifecycleRegistry(2, 3)
	if _, err := req.register("corr_1", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if err := req.activate("corr_1", func() {}); err != nil {
		t.Fatalf("unexpected activate error: %v", err)
	}

	emit, target := req.claimDuplicate("corr_1")
	if !emit {
		t.Fatal("expected duplicate claim to emit")
	}
	if target.correlation != "corr_1" {
		t.Fatalf("unexpected target %q", target.correlation)
	}

	if emit, _ := req.claimDuplicate("corr_1"); emit {
		t.Fatal("expected duplicate suppression after terminal")
	}

	_, active, terminal := req.status("corr_1")
	if active || !terminal {
		t.Fatalf("expected active=false terminal=true after duplicate")
	}
}

func TestLifecycleRegistryClaimTerminalAndCancel(t *testing.T) {
	req := newRequestLifecycleRegistry(2, 3)
	if _, err := req.register("corr_1", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if err := req.activate("corr_1", func() {}); err != nil {
		t.Fatalf("unexpected activate error: %v", err)
	}

	emit, target := req.claimTerminal("corr_1")
	if !emit {
		t.Fatal("expected terminal to emit")
	}
	if target.operation != protocol.MachineOpRead {
		t.Fatalf("wrong operation %q", target.operation)
	}

	if emit, _ := req.claimTerminal("corr_1"); emit {
		t.Fatal("expected second terminal claim suppressed")
	}
}

func TestLifecycleRegistryCancelWinsAndAlreadyFinal(t *testing.T) {
	req := newRequestLifecycleRegistry(2, 2)
	if _, err := req.register("corr_1", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if err := req.activate("corr_1", func() {}); err != nil {
		t.Fatalf("unexpected activate error: %v", err)
	}

	state, target := req.claimCancel("corr_1")
	if state != protocol.MachineCancelStateCanceled || target.correlation != "corr_1" {
		t.Fatalf("expected canceled state target, got state=%q target=%q", state, target.correlation)
	}
	state, _ = req.claimCancel("corr_1")
	if state != protocol.MachineCancelStateAlreadyFinal {
		t.Fatalf("expected already_final after cancel terminalized, got %q", state)
	}
}

func TestLifecycleRegistrySnapshotActiveKeepsBoundedState(t *testing.T) {
	req := newRequestLifecycleRegistry(2, 2)
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("corr_%d", i)
		if _, err := req.register(name, protocol.MachineOpRead); err != nil {
			t.Fatalf("unexpected register error: %v", err)
		}
		if err := req.activate(name, func() {}); err != nil {
			t.Fatalf("unexpected activate error: %v", err)
		}
	}
	targets, ok := req.closeAdmission()
	if !ok {
		t.Fatal("expected admission close to take")
	}
	if len(targets) != 2 {
		t.Fatalf("expected two active snapshot targets, got %d", len(targets))
	}
	if req.size() != 2 {
		t.Fatalf("expected bounded lifetime map to retain 2 entries, got %d", req.size())
	}
	if !req.isShuttingDown() {
		t.Fatal("expected lifecycle to be shutting down after closeAdmission")
	}
}

func TestLifecycleRegistryCloseAdmissionReturnsStableOrder(t *testing.T) {
	req := newRequestLifecycleRegistry(2, 3)
	if _, err := req.register("corr_b", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if _, err := req.register("corr_a", protocol.MachineOpRead); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if _, err := req.register("corr_c", protocol.MachineOpSubscribe); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	targets, ok := req.closeAdmission()
	if !ok {
		t.Fatal("expected closeAdmission to close")
	}
	if len(targets) != 3 {
		t.Fatalf("expected three targets, got %d", len(targets))
	}

	got := make([]string, 0, len(targets))
	for _, target := range targets {
		got = append(got, target.correlation)
	}
	want := []string{"corr_a", "corr_b", "corr_c"}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("expected sorted correlations, got %v", got)
	}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestDeliveryRegistryClaimStateAndResolve(t *testing.T) {
	delivery := newMemoryDeliveryRegistry(2)
	identity := DeliveryIdentity{Identity: "del_1", Fingerprint: "fp_1"}

	claim, err := delivery.Claim(identity)
	if err != nil || claim.State != DeliveryClaimStateNew {
		t.Fatalf("expected new claim, got state=%v err=%v", claim.State, err)
	}

	identical, err := delivery.Claim(identity)
	if err != nil || identical.State != DeliveryClaimStateIdenticalPending {
		t.Fatalf("expected identical pending, got state=%v err=%v", identical.State, err)
	}

	response := protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: "corr_1", Operation: protocol.MachineOpSendNudge}
	if !delivery.Resolve(identity, response, false) {
		t.Fatal("expected resolve to succeed")
	}

	resolved, err := delivery.Claim(identity)
	resolved = mustClaimState(t, resolved, err)
	if resolved.State != DeliveryClaimStateIdenticalResolved {
		t.Fatalf("expected identical resolved, got %v", resolved.State)
	}
	if resolved.ExistingResponse.CorrelationID != "corr_1" {
		t.Fatalf("expected cached response correlation, got %q", resolved.ExistingResponse.CorrelationID)
	}
	if resolved.ExistingResolved != true {
		t.Fatal("expected existing resolved marker")
	}
}

func TestDeliveryRegistryCapacity(t *testing.T) {
	delivery := newMemoryDeliveryRegistry(1)
	if _, err := delivery.Claim(DeliveryIdentity{Identity: "del_1", Fingerprint: "fp_1"}); err != nil {
		t.Fatal("expected first claim to succeed")
	}
	if _, err := delivery.Claim(DeliveryIdentity{Identity: "del_2", Fingerprint: "fp_2"}); err == nil {
		t.Fatal("expected capacity error")
	}
}

func TestDeliveryRegistryConflictFingerprint(t *testing.T) {
	delivery := newMemoryDeliveryRegistry(4)
	if _, err := delivery.Claim(DeliveryIdentity{Identity: "delivery", Fingerprint: "fingerprint-a"}); err != nil {
		t.Fatalf("unexpected claim: %v", err)
	}
	claim, err := delivery.Claim(DeliveryIdentity{Identity: "delivery", Fingerprint: "fingerprint-b"})
	if err != nil {
		t.Fatalf("unexpected claim: %v", err)
	}
	if claim.State != DeliveryClaimStateChangedConflict {
		t.Fatalf("expected changed conflict, got %v", claim.State)
	}
}

func mustClaimState(t *testing.T, claim DeliveryClaim, err error) DeliveryClaim {
	t.Helper()
	if err != nil {
		t.Fatalf("claim should not error: %v", err)
	}
	return claim
}
