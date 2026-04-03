package rooms

import (
	"context"
	"strings"
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
	AllowHumanIngress bool
	NetworkID         string
	NetworkName       string
	Pairings          []protocol.Pairing
	Version           string
	Store             store.RoomStore
	Messages          store.MessageStore
	Broker            EventBroker
	PairingClient     PairingClient
}

type Service struct {
	allowHumanIngress bool
	networkID         string
	networkName       string
	pairings          []protocol.Pairing
	version           string
	store             store.RoomStore
	messages          store.MessageStore
	contextStore      store.ContextRoomStore
	contextMessages   store.ContextMessageStore
	lifecycleMessages store.ContextLifecycleMessageStore
	contextAgents     store.ContextAgentStore
	broker            EventBroker
	pairingClient     PairingClient
	relaySlots        chan struct{}
	pairingsMu        sync.RWMutex
	pairingPublishMu  sync.Mutex
	pairingStatuses   map[string]pairingStatus
	counter           atomic.Uint64
	lifecycleCtx      context.Context
	lifecycleCancel   context.CancelFunc
	now               func() time.Time
}

type pairingStatus struct {
	value     string
	updatedAt time.Time
}

func NewService(config ServiceConfig) *Service {
	now := time.Now
	statuses := make(map[string]pairingStatus, len(config.Pairings))
	for _, pairing := range config.Pairings {
		statuses[pairing.ID] = pairingStatus{
			value:     strings.TrimSpace(pairing.Status),
			updatedAt: now().UTC(),
		}
	}

	contextStore, _ := config.Store.(store.ContextRoomStore)
	contextMessages, _ := config.Messages.(store.ContextMessageStore)
	lifecycleMessages, _ := config.Messages.(store.ContextLifecycleMessageStore)
	contextAgents, _ := config.Store.(store.ContextAgentStore)
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())

	return &Service{
		allowHumanIngress: config.AllowHumanIngress,
		networkID:         config.NetworkID,
		networkName:       config.NetworkName,
		pairings:          append([]protocol.Pairing(nil), config.Pairings...),
		version:           config.Version,
		store:             config.Store,
		messages:          config.Messages,
		contextStore:      contextStore,
		contextMessages:   contextMessages,
		lifecycleMessages: lifecycleMessages,
		contextAgents:     contextAgents,
		broker:            config.Broker,
		pairingClient:     config.PairingClient,
		relaySlots:        make(chan struct{}, 8),
		pairingStatuses:   statuses,
		lifecycleCtx:      lifecycleCtx,
		lifecycleCancel:   lifecycleCancel,
		now:               now,
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
	s.pairingsMu.RUnlock()

	return protocol.Network{
		ID:      s.networkID,
		Name:    s.networkName,
		Version: s.version,
		Capabilities: protocol.NetworkCapabilities{
			EventStream:        "sse",
			AttachmentProtocol: "websocket",
			HumanIngress:       s.allowHumanIngress,
			MessagePagination:  "cursor",
			Pairings:           hasPairings,
		},
	}
}
