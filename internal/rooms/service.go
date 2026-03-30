package rooms

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type EventBroker interface {
	Publish(event protocol.Event)
	Subscribe(ctx context.Context) <-chan protocol.Event
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
}

type Service struct {
	allowHumanIngress bool
	networkID         string
	networkName       string
	pairings          []protocol.Pairing
	version           string
	store             store.RoomStore
	messages          store.MessageStore
	broker            EventBroker
	counter           atomic.Uint64
}

func NewService(config ServiceConfig) *Service {
	return &Service{
		allowHumanIngress: config.AllowHumanIngress,
		networkID:         config.NetworkID,
		networkName:       config.NetworkName,
		pairings:          append([]protocol.Pairing(nil), config.Pairings...),
		version:           config.Version,
		store:             config.Store,
		messages:          config.Messages,
		broker:            config.Broker,
	}
}

func (s *Service) Network() protocol.Network {
	return protocol.Network{
		ID:      s.networkID,
		Name:    s.networkName,
		Version: s.version,
		Capabilities: protocol.NetworkCapabilities{
			EventStream:       "sse",
			HumanIngress:      s.allowHumanIngress,
			MessagePagination: "cursor",
			Pairings:          len(s.pairings) > 0,
		},
	}
}

func (s *Service) ListRooms() []protocol.Room {
	return s.store.ListRooms()
}

func (s *Service) CreateRoom(request protocol.CreateRoomRequest) (protocol.Room, error) {
	id := strings.TrimSpace(request.ID)
	if id == "" {
		return protocol.Room{}, fmt.Errorf("room id is required")
	}

	room := protocol.Room{
		ID:        id,
		FQID:      protocol.RoomFQID(s.networkID, id),
		Name:      strings.TrimSpace(request.Name),
		Members:   append([]string(nil), request.Members...),
		CreatedAt: time.Now().UTC(),
	}

	if room.Name == "" {
		room.Name = room.ID
	}

	if err := s.store.CreateRoom(room); err != nil {
		return protocol.Room{}, err
	}

	return room, nil
}

func (s *Service) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	if _, ok := s.store.GetRoom(roomID); !ok {
		return protocol.MessagePage{}, fmt.Errorf("unknown room %q", roomID)
	}

	return s.messages.ListRoomMessages(roomID, before, limit), nil
}

func (s *Service) ListDirectConversations() []protocol.DirectConversation {
	return s.messages.ListDirectConversations()
}

func (s *Service) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	if strings.TrimSpace(dmID) == "" {
		return protocol.MessagePage{}, fmt.Errorf("dm id is required")
	}

	return s.messages.ListDMMessages(dmID, before, limit), nil
}

func (s *Service) ListAgents() []protocol.AgentSummary {
	rooms := s.store.ListRooms()
	agentsByID := make(map[string]*protocol.AgentSummary)

	for _, room := range rooms {
		for _, memberID := range room.Members {
			agent, ok := agentsByID[memberID]
			if !ok {
				agent = &protocol.AgentSummary{
					ID:        memberID,
					FQID:      protocol.AgentFQID(s.networkID, memberID),
					NetworkID: s.networkID,
				}
				agentsByID[memberID] = agent
			}

			agent.Rooms = append(agent.Rooms, room.ID)
		}
	}

	agents := make([]protocol.AgentSummary, 0, len(agentsByID))
	for _, agent := range agentsByID {
		slices.Sort(agent.Rooms)
		agents = append(agents, *agent)
	}

	slices.SortFunc(agents, func(left, right protocol.AgentSummary) int {
		return strings.Compare(left.ID, right.ID)
	})

	return agents
}

func (s *Service) ListPairings() []protocol.Pairing {
	pairings := make([]protocol.Pairing, 0, len(s.pairings))
	return append(pairings, s.pairings...)
}

func (s *Service) SendMessage(request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	if request.From.Type == "human" && !s.allowHumanIngress {
		return protocol.MessageAccepted{}, fmt.Errorf("human ingress is disabled for this network")
	}

	messageID := strings.TrimSpace(request.ID)
	if messageID == "" {
		messageID = s.nextID("msg")
	}

	if err := protocol.ValidateTarget(request.Target); err != nil {
		return protocol.MessageAccepted{}, err
	}

	if request.Target.Kind == protocol.TargetKindRoom {
		if _, ok := s.store.GetRoom(request.Target.RoomID); !ok {
			return protocol.MessageAccepted{}, fmt.Errorf("unknown room %q", request.Target.RoomID)
		}
	}

	now := time.Now().UTC()
	message := protocol.Message{
		ID:        messageID,
		NetworkID: s.networkID,
		Target:    request.Target,
		From:      request.From,
		Parts:     append([]protocol.Part(nil), request.Parts...),
		Mentions:  protocol.NormalizeMentions(request.Parts, request.Mentions),
		CreatedAt: now,
	}

	event := protocol.Event{
		ID:        s.nextID("evt"),
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: s.networkID,
		Message:   &message,
		CreatedAt: now,
	}

	if err := s.messages.AppendMessage(message); err != nil {
		return protocol.MessageAccepted{}, err
	}

	s.broker.Publish(event)

	return protocol.MessageAccepted{
		MessageID: message.ID,
		EventID:   event.ID,
		Accepted:  true,
	}, nil
}

func (s *Service) Subscribe(ctx context.Context) <-chan protocol.Event {
	return s.broker.Subscribe(ctx)
}

func (s *Service) nextID(prefix string) string {
	id := s.counter.Add(1)
	return fmt.Sprintf("%s_%d", prefix, id)
}
