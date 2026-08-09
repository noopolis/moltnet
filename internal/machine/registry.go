package machine

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type requestLifecycle struct {
	operation string
	active    bool
	terminal  bool
	cancel    context.CancelFunc
}

type requestLifecycleRegistry struct {
	mu             sync.Mutex
	entries        map[string]*requestLifecycle
	activeCount    int
	maxActive      int
	maxCorrelation int
	admitting      bool
}

type cancelTarget struct {
	correlation string
	operation   string
	cancel      context.CancelFunc
}

func newRequestLifecycleRegistry(maxActive, maxCorrelation int) *requestLifecycleRegistry {
	return &requestLifecycleRegistry{
		entries:        make(map[string]*requestLifecycle),
		maxActive:      maxActive,
		maxCorrelation: maxCorrelation,
		admitting:      true,
	}
}

var errLifetimeDuplicate = fmt.Errorf("duplicate correlation_id")
var errLifetimeCapacity = fmt.Errorf("lifetime capacity exceeded")
var errLifetimeClosed = fmt.Errorf("lifecycle is closing")
var errActiveCapacity = fmt.Errorf("active capacity exceeded")

// register claims a correlation in the bounded lifetime registry.
// The same correlation may reappear:
// - duplicateActive: correlation exists and is still mutable.
// - duplicateTerminal: correlation exists and is already terminal.
type registerState int

const (
	registerStateAccepted registerState = iota
	registerStateDuplicateActive
	registerStateDuplicateTerminal
	registerStateCapacity
	registerStateClosed
)

func (registry *requestLifecycleRegistry) register(correlationID, operation string) (registerState, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.admitting {
		return registerStateClosed, errLifetimeClosed
	}
	entry, exists := registry.entries[correlationID]
	if exists {
		if entry.terminal {
			return registerStateDuplicateTerminal, errLifetimeDuplicate
		}
		return registerStateDuplicateActive, errLifetimeDuplicate
	}
	if len(registry.entries) >= registry.maxCorrelation {
		return registerStateCapacity, errLifetimeCapacity
	}
	registry.entries[correlationID] = &requestLifecycle{operation: operation}
	return registerStateAccepted, nil
}

func (registry *requestLifecycleRegistry) activate(correlationID string, cancel context.CancelFunc) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.admitting {
		return errLifetimeClosed
	}
	entry, ok := registry.entries[correlationID]
	if !ok {
		return errLifetimeDuplicate
	}
	if entry.terminal {
		return errLifetimeDuplicate
	}
	if entry.active {
		return nil
	}
	if registry.activeCount >= registry.maxActive {
		return errActiveCapacity
	}
	entry.active = true
	entry.cancel = cancel
	registry.activeCount++
	return nil
}

func (registry *requestLifecycleRegistry) claimTerminal(correlationID string) (emit bool, target cancelTarget) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.entries[correlationID]
	if !ok || entry.terminal {
		return false, cancelTarget{}
	}
	entry.terminal = true
	if entry.active {
		entry.active = false
		registry.activeCount--
	}
	cancel := entry.cancel
	entry.cancel = nil
	return true, cancelTarget{
		correlation: correlationID,
		operation:   entry.operation,
		cancel:      cancel,
	}
}

func (registry *requestLifecycleRegistry) claimDuplicate(correlationID string) (emit bool, target cancelTarget) {
	// Duplicate must always terminalize the original correlation and prevent late success.
	return registry.claimTerminal(correlationID)
}

func (registry *requestLifecycleRegistry) claimCancel(correlationID string) (state string, target cancelTarget) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.entries[correlationID]
	if !ok {
		return protocol.MachineCancelStateNotFound, cancelTarget{}
	}
	if entry.terminal {
		return protocol.MachineCancelStateAlreadyFinal, cancelTarget{}
	}
	entry.terminal = true
	if entry.active {
		entry.active = false
		registry.activeCount--
	}
	cancel := entry.cancel
	entry.cancel = nil
	return protocol.MachineCancelStateCanceled, cancelTarget{
		correlation: correlationID,
		operation:   entry.operation,
		cancel:      cancel,
	}
}

func (registry *requestLifecycleRegistry) closeAdmission() ([]cancelTarget, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.admitting {
		return nil, false
	}
	registry.admitting = false

	targets := make([]cancelTarget, 0, registry.activeCount)
	type snapshot struct {
		correlation string
		operation   string
		cancel      context.CancelFunc
	}
	snapshots := make([]snapshot, 0, len(registry.entries))
	for correlationID, entry := range registry.entries {
		if entry.terminal {
			continue
		}
		snapshots = append(snapshots, snapshot{
			correlation: correlationID,
			operation:   entry.operation,
			cancel:      entry.cancel,
		})
		entry.active = false
		entry.terminal = true
		entry.cancel = nil
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].correlation != snapshots[j].correlation {
			return snapshots[i].correlation < snapshots[j].correlation
		}
		return snapshots[i].operation < snapshots[j].operation
	})
	for _, snapshot := range snapshots {
		targets = append(targets, cancelTarget(snapshot))
	}
	registry.activeCount = 0
	return targets, true
}

func (registry *requestLifecycleRegistry) isShuttingDown() bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return !registry.admitting
}

func (registry *requestLifecycleRegistry) status(correlationID string) (known bool, active bool, terminal bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.entries[correlationID]
	if !ok {
		return false, false, false
	}
	return true, entry.active, entry.terminal
}

// size returns bounded count of known lifetime entries.
func (registry *requestLifecycleRegistry) size() int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.entries)
}

type memoryDeliveryRecord struct {
	fingerprint string
	resolved    bool
	ambiguous   bool
	response    protocol.MachineResponse
}

type memoryDeliveryRegistry struct {
	mu      sync.Mutex
	entries map[string]memoryDeliveryRecord
	max     int
}

func newMemoryDeliveryRegistry(max int) *memoryDeliveryRegistry {
	return &memoryDeliveryRegistry{
		entries: make(map[string]memoryDeliveryRecord),
		max:     max,
	}
}

func (registry *memoryDeliveryRegistry) Size() int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.entries)
}

var errDeliveryCapacity = fmt.Errorf("delivery capacity exceeded")

func (registry *memoryDeliveryRegistry) Claim(identity DeliveryIdentity) (DeliveryClaim, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if identity.Identity == "" {
		return DeliveryClaim{State: DeliveryClaimStateNew}, nil
	}

	record, ok := registry.entries[identity.Identity]
	if !ok {
		if len(registry.entries) >= registry.max {
			return DeliveryClaim{State: DeliveryClaimStateCapacity}, errDeliveryCapacity
		}
		registry.entries[identity.Identity] = memoryDeliveryRecord{
			fingerprint: identity.Fingerprint,
		}
		return DeliveryClaim{State: DeliveryClaimStateNew}, nil
	}

	if record.resolved {
		if record.fingerprint == identity.Fingerprint {
			return DeliveryClaim{
				State:             DeliveryClaimStateIdenticalResolved,
				ExistingResponse:  record.response,
				ExistingResolved:  true,
				ExistingAmbiguous: record.ambiguous,
			}, nil
		}
		return DeliveryClaim{
			State:               DeliveryClaimStateChangedConflict,
			ExistingFingerprint: record.fingerprint,
		}, nil
	}

	if record.fingerprint == identity.Fingerprint {
		return DeliveryClaim{State: DeliveryClaimStateIdenticalPending}, nil
	}
	return DeliveryClaim{
		State:               DeliveryClaimStateChangedConflict,
		ExistingFingerprint: record.fingerprint,
	}, nil
}

func (registry *memoryDeliveryRegistry) Resolve(identity DeliveryIdentity, response protocol.MachineResponse, ambiguous bool) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	record, ok := registry.entries[identity.Identity]
	if !ok || record.resolved {
		return false
	}
	record.resolved = true
	record.response = response
	record.ambiguous = ambiguous
	registry.entries[identity.Identity] = record
	return true
}

func (registry *memoryDeliveryRegistry) Release(identity DeliveryIdentity) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	record, ok := registry.entries[identity.Identity]
	if !ok || record.resolved || record.fingerprint != identity.Fingerprint {
		return false
	}
	delete(registry.entries, identity.Identity)
	return true
}

func (registry *memoryDeliveryRegistry) Lookup(identity DeliveryIdentity) (protocol.MachineResponse, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	record, ok := registry.entries[identity.Identity]
	if !ok || !record.resolved {
		return protocol.MachineResponse{}, false
	}
	return record.response, true
}
