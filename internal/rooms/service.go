package rooms

import (
	"context"
	"fmt"
	"github.com/noopolis/moltnet/internal/automation"
	"sync"
	"time"

	"github.com/noopolis/moltnet/internal/follow"
	"github.com/noopolis/moltnet/internal/hubinvites"
	"github.com/noopolis/moltnet/internal/netkeys"
	"github.com/noopolis/moltnet/internal/observability"
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
	Automation *automation.Manager
	// Now stamps domain data only; operational deadlines always use real time.
	Now                       func() (time.Time, error)
	AllowHumanIngress         bool
	DebugEvents               bool
	DisableDirectMessages     bool
	RequirePairNetworkBinding bool
	NetworkID                 string
	NetworkName               string
	Pairings                  []protocol.Pairing
	Version                   string
	Store                     store.RoomStore
	Messages                  store.MessageStore
	Broker                    EventBroker
	PairingClient             PairingClient
	DeliveryRetryMin          time.Duration
	DeliveryRetryMax          time.Duration
	DeliveryPollInterval      time.Duration
	DeliveryTimeout           time.Duration
	// CausalWriter is optional. When set, SendMessageContext stamps a
	// message.accepted causal event (pkg/protocol.CausalEvent) after each
	// durable, non-duplicate message append. When nil, causal stamping is
	// skipped entirely and message send behavior is unchanged.
	CausalWriter *observability.CausalWriter
	// TranscriptWriter is optional. When set, accepted room/DM messages are
	// exported beside causal.jsonl as research data for the memetics programme.
	TranscriptWriter *observability.TranscriptWriter
	// Identity signs locally originated messages relayed over pairings.
	// Nil leaves outbound relays unsigned, exactly as before keys existed.
	Identity *netkeys.Identity
	// KeyPins is the durable trust store for peer network keys. Nil means
	// an in-memory store (tests and keyless servers).
	KeyPins *netkeys.PinStore
	// HubSyncInterval is how often member hub rooms catch up from their
	// home without a gap or reconnect prompting it (default 30s).
	HubSyncInterval time.Duration
	// HubInvites holds shared-room invite grants and pending joins. Nil
	// means an in-memory store (tests and keyless servers).
	HubInvites *hubinvites.Store
}

type Service struct {
	automation                *automation.Manager
	follows                   *follow.Manager
	followApplyMu             sync.Mutex
	allowHumanIngress         bool
	debugEvents               bool
	disableDirectMessages     bool
	requirePairNetworkBinding bool
	networkID                 string
	networkName               string
	pairings                  []protocol.Pairing
	version                   string
	store                     store.RoomStore
	messages                  store.MessageStore
	contextStore              store.ContextRoomStore
	contextMessages           store.ContextMessageStore
	lifecycleMessages         store.ContextLifecycleMessageStore
	deliveryStore             store.AttachmentDeliveryStore
	contextAgents             store.ContextAgentStore
	agentRegistry             store.ContextAgentRegistryStore
	broker                    EventBroker
	pairingClient             PairingClient
	causalWriter              *observability.CausalWriter
	transcriptWriter          *observability.TranscriptWriter
	pairingDeliveries         store.PairingDeliveryStore
	envelopes                 store.MessageEnvelopeStore
	hubs                      store.HubStore
	// hubMu serializes hub control-log appends and receipt placement, so
	// one home assigns each room seq exactly once.
	hubMu                sync.Mutex
	hubRosters           hubRosterCache
	hubRoomList          hubRoomsCache
	hubSyncWake          chan struct{}
	hubSyncMu            sync.Mutex
	hubSyncInterval      time.Duration
	identity             *netkeys.Identity
	keyPins              *netkeys.PinStore
	hubInvites           *hubinvites.Store
	deliveryRetryMin     time.Duration
	deliveryRetryMax     time.Duration
	deliveryPollInterval time.Duration
	deliveryTimeout      time.Duration
	relayWake            chan struct{}
	relayStart           sync.Once
	relayWG              sync.WaitGroup
	pairingsMu           sync.RWMutex
	pairingPublishMu     sync.Mutex
	deliveryMu           sync.Mutex
	pairingStatuses      map[string]pairingStatus
	// learnedPairNetworks pins a pair credential to the origin network it
	// first asserted, for credentials config never bound (the inviting side).
	// Guarded by pairingsMu. See pairing_binding.go.
	learnedPairNetworks map[string]string
	// roomCredentials deliberately stay in memory: protocol.Room is persisted
	// and publicly serialized, while configured credentials are repopulated by
	// ApplyConfigContext at boot and must never enter either surface.
	roomCredentials   map[string]protocol.SecretString
	roomCredentialsMu sync.RWMutex
	agentPresenceMu   sync.RWMutex
	connectedAgents   map[string]bool
	lifecycleCtx      context.Context
	lifecycleCancel   context.CancelFunc
	now               func() (time.Time, error)
}

type pairingStatus struct {
	value            string
	updatedAt        time.Time // Real time: retry and diagnostics-cache age.
	diagnostics      *protocol.PairingDiagnostics
	checked          bool
	everReached      bool
	directMessages   bool
	cursorPagination bool
}

func NewService(config ServiceConfig) *Service {
	now := config.Now
	if now == nil {
		now = func() (time.Time, error) { return time.Now().UTC(), nil }
	}
	readNow := now
	now = func() (time.Time, error) {
		value, err := readNow()
		return value.UTC(), err
	}
	// All dependents are configured before serving requests.
	for _, dependency := range []any{config.Store, config.Messages, config.Broker} {
		if clocked, ok := dependency.(interface {
			SetNow(func() (time.Time, error))
		}); ok {
			clocked.SetNow(now)
		}
	}
	statuses := make(map[string]pairingStatus, len(config.Pairings))
	for _, pairing := range config.Pairings {
		statuses[pairing.ID] = initialPairingStatus(pairing, time.Now().UTC())
	}

	contextStore, _ := config.Store.(store.ContextRoomStore)
	contextMessages, _ := config.Messages.(store.ContextMessageStore)
	lifecycleMessages, _ := config.Messages.(store.ContextLifecycleMessageStore)
	contextAgents, _ := config.Store.(store.ContextAgentStore)
	agentRegistry, _ := config.Store.(store.ContextAgentRegistryStore)
	deliveryStore, _ := config.Messages.(store.AttachmentDeliveryStore)
	if broker, ok := config.Broker.(attachmentDeliveryBroker); !ok || !broker.DurableDeliveryConfigured() {
		deliveryStore = nil
	}
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())

	service := &Service{
		automation:                config.Automation,
		allowHumanIngress:         config.AllowHumanIngress,
		debugEvents:               config.DebugEvents,
		disableDirectMessages:     config.DisableDirectMessages,
		requirePairNetworkBinding: config.RequirePairNetworkBinding,
		networkID:                 config.NetworkID,
		networkName:               config.NetworkName,
		pairings:                  append([]protocol.Pairing(nil), config.Pairings...),
		version:                   config.Version,
		store:                     config.Store,
		messages:                  config.Messages,
		contextStore:              contextStore,
		contextMessages:           contextMessages,
		lifecycleMessages:         lifecycleMessages,
		deliveryStore:             deliveryStore,
		contextAgents:             contextAgents,
		agentRegistry:             agentRegistry,
		broker:                    config.Broker,
		pairingClient:             config.PairingClient,
		causalWriter:              config.CausalWriter,
		transcriptWriter:          config.TranscriptWriter,
		relayWake:                 make(chan struct{}, 1),
		pairingStatuses:           statuses,
		roomCredentials:           make(map[string]protocol.SecretString),
		connectedAgents:           make(map[string]bool),
		lifecycleCtx:              lifecycleCtx,
		lifecycleCancel:           lifecycleCancel,
		now:                       now,
	}
	service.pairingDeliveries, _ = config.Messages.(store.PairingDeliveryStore)
	service.envelopes, _ = config.Messages.(store.MessageEnvelopeStore)
	service.hubs, _ = config.Messages.(store.HubStore)
	service.identity, service.keyPins = config.Identity, config.KeyPins
	if service.keyPins == nil {
		service.keyPins = netkeys.NewMemoryPinStore()
	}
	if service.hubInvites = config.HubInvites; service.hubInvites == nil {
		service.hubInvites = hubinvites.NewMemory()
	}
	service.configureDelivery(config)
	service.configureHubSync(config)
	service.follows = follow.New(service)
	return service
}

func (s *Service) Close() error {
	if s.follows != nil {
		s.follows.Close()
	}
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	s.relayWG.Wait()
	return nil
}

func (s *Service) Network() protocol.Network {
	s.pairingsMu.RLock()
	hasPairings := len(s.pairings) > 0
	warnings := s.networkWarningsLocked()
	s.pairingsMu.RUnlock()

	var identity *protocol.NetworkIdentity
	if public, ok := s.LocalIdentity(); ok {
		identity = &public
	}
	return protocol.Network{
		Identity: identity,
		ID:       s.networkID,
		Name:     s.networkName,
		Version:  s.version,
		Protocols: protocol.NetworkProtocols{
			HTTP:   []string{protocol.HTTPProtocolV1},
			Attach: []string{protocol.AttachmentProtocolV1},
			Pair:   []string{protocol.PairProtocolV1},
		},
		Capabilities: protocol.NetworkCapabilities{
			LocalAutomation:    s.automation != nil,
			EventStream:        "sse",
			AttachmentProtocol: "websocket",
			DebugEvents:        s.debugEvents,
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
		case protocol.PairingStatusPending:
			// expected while waiting for the peer to join; not a fault.
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
