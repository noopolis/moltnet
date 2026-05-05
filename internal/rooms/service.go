package rooms

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type EventBroker interface {
	Publish(event protocol.Event)
	Subscribe(ctx context.Context) <-chan protocol.Event
}

type PairingClient interface {
	FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error)
	FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error)
	FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error)
	RelayMessage(ctx context.Context, pairing protocol.Pairing, request protocol.SendMessageRequest) (protocol.MessageAccepted, error)
}

type ServiceConfig struct {
	AllowHumanIngress     bool
	DisableDirectMessages bool
	NetworkID             string
	NetworkName           string
	Pairings              []protocol.Pairing
	Version               string
	Store                 store.RoomStore
	Messages              store.MessageStore
	Broker                EventBroker
	PairingClient         PairingClient
}

type Service struct {
	allowHumanIngress     bool
	disableDirectMessages bool
	networkID             string
	networkName           string
	pairings              []protocol.Pairing
	version               string
	store                 store.RoomStore
	messages              store.MessageStore
	contextStore          store.ContextRoomStore
	contextMessages       store.ContextMessageStore
	lifecycleMessages     store.ContextLifecycleMessageStore
	contextAgents         store.ContextAgentStore
	agentRegistry         store.ContextAgentRegistryStore
	broker                EventBroker
	pairingClient         PairingClient
	relaySlots            chan struct{}
	pairingsMu            sync.RWMutex
	pairingPublishMu      sync.Mutex
	pairingStatuses       map[string]pairingStatus
	counter               atomic.Uint64
	lifecycleCtx          context.Context
	lifecycleCancel       context.CancelFunc
	now                   func() time.Time
}

type pairingStatus struct {
	value            string
	updatedAt        time.Time
	diagnostics      *protocol.PairingDiagnostics
	checked          bool
	directMessages   bool
	cursorPagination bool
}

func NewService(config ServiceConfig) *Service {
	now := time.Now
	statuses := make(map[string]pairingStatus, len(config.Pairings))
	for _, pairing := range config.Pairings {
		statuses[pairing.ID] = initialPairingStatus(pairing, now().UTC())
	}

	contextStore, _ := config.Store.(store.ContextRoomStore)
	contextMessages, _ := config.Messages.(store.ContextMessageStore)
	lifecycleMessages, _ := config.Messages.(store.ContextLifecycleMessageStore)
	contextAgents, _ := config.Store.(store.ContextAgentStore)
	agentRegistry, _ := config.Store.(store.ContextAgentRegistryStore)
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())

	return &Service{
		allowHumanIngress:     config.AllowHumanIngress,
		disableDirectMessages: config.DisableDirectMessages,
		networkID:             config.NetworkID,
		networkName:           config.NetworkName,
		pairings:              append([]protocol.Pairing(nil), config.Pairings...),
		version:               config.Version,
		store:                 config.Store,
		messages:              config.Messages,
		contextStore:          contextStore,
		contextMessages:       contextMessages,
		lifecycleMessages:     lifecycleMessages,
		contextAgents:         contextAgents,
		agentRegistry:         agentRegistry,
		broker:                config.Broker,
		pairingClient:         config.PairingClient,
		relaySlots:            make(chan struct{}, 8),
		pairingStatuses:       statuses,
		lifecycleCtx:          lifecycleCtx,
		lifecycleCancel:       lifecycleCancel,
		now:                   now,
	}
}

func (s *Service) Close() error {
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	return nil
}

func (s *Service) Network() protocol.Network {
	s.pairingsMu.RLock()
	hasPairings := len(s.pairings) > 0
	warnings := s.networkWarningsLocked()
	s.pairingsMu.RUnlock()

	return protocol.Network{
		ID:      s.networkID,
		Name:    s.networkName,
		Version: s.version,
		Protocols: protocol.NetworkProtocols{
			HTTP:   []string{protocol.HTTPProtocolV1},
			Attach: []string{protocol.AttachmentProtocolV1},
			Pair:   []string{protocol.PairProtocolV1},
		},
		Capabilities: protocol.NetworkCapabilities{
			EventStream:        "sse",
			AttachmentProtocol: "websocket",
			HumanIngress:       s.allowHumanIngress,
			DirectMessages:     !s.disableDirectMessages,
			MessagePagination:  "cursor",
			Pairings:           hasPairings,
		},
		Warnings: warnings,
	}
}

func (s *Service) networkWarningsLocked() []protocol.NetworkWarning {
	var incompatible int
	var degraded int
	var errored int
	for _, status := range s.pairingStatuses {
		switch status.value {
		case protocol.PairingStatusIncompatible:
			incompatible++
		case protocol.PairingStatusDegraded:
			degraded++
		case protocol.PairingStatusError:
			errored++
		}
	}

	warnings := make([]protocol.NetworkWarning, 0, 3)
	if incompatible > 0 {
		warnings = append(warnings, protocol.NetworkWarning{
			Severity: "error",
			Code:     "pairings.incompatible",
			Message:  fmt.Sprintf("%s incompatible.", pairingCountText(incompatible)),
			Action:   "Open the Pairings panel for diagnostics.",
		})
	}
	if errored > 0 {
		warnings = append(warnings, protocol.NetworkWarning{
			Severity: "warning",
			Code:     "pairings.error",
			Message:  fmt.Sprintf("%s in error.", pairingCountText(errored)),
			Action:   "Check remote network connectivity and pairing credentials.",
		})
	}
	if degraded > 0 {
		warnings = append(warnings, protocol.NetworkWarning{
			Severity: "warning",
			Code:     "pairings.degraded",
			Message:  fmt.Sprintf("%s degraded.", pairingCountText(degraded)),
			Action:   "Open the Pairings panel for diagnostics.",
		})
	}
	return warnings
}

func pairingCountText(count int) string {
	if count == 1 {
		return "1 pairing"
	}
	return fmt.Sprintf("%d pairings", count)
}
