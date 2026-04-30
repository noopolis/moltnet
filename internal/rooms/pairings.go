package rooms

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) ListPairings() ([]protocol.Pairing, error) {
	page, err := s.ListPairingsContext(context.Background(), protocol.PageRequest{})
	if err != nil {
		return nil, err
	}
	return page.Pairings, nil
}

func (s *Service) ListPairingsContext(_ context.Context, page protocol.PageRequest) (protocol.PairingPage, error) {
	pairings := s.snapshotPairings()
	slices.SortFunc(pairings, func(left, right protocol.Pairing) int {
		return strings.Compare(left.ID, right.ID)
	})

	selected, info, err := paginatePairings(pairings, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.PairingPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.PairingPage{}, err
	}

	return protocol.PairingPage{
		Pairings: selected,
		Page:     info,
	}, nil
}

func (s *Service) PairingNetwork(ctx context.Context, pairingID string) (protocol.Network, error) {
	pairing, err := s.findPairing(pairingID)
	if err != nil {
		return protocol.Network{}, err
	}
	if s.pairingClient == nil {
		return protocol.Network{}, pairingClientMissingError()
	}

	network, err := s.pairingClient.FetchNetwork(ctx, pairing)
	if err != nil {
		s.setPairingStatus(pairing.ID, "error")
		return protocol.Network{}, remotePairingError(err)
	}
	s.setPairingStatus(pairing.ID, "connected")
	return network, nil
}

func (s *Service) PairingRooms(ctx context.Context, pairingID string) ([]protocol.Room, error) {
	page, err := s.PairingRoomsContext(ctx, pairingID, protocol.PageRequest{})
	if err != nil {
		return nil, err
	}
	return page.Rooms, nil
}

func (s *Service) PairingRoomsContext(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.RoomPage, error) {
	pairing, err := s.findPairing(pairingID)
	if err != nil {
		return protocol.RoomPage{}, err
	}
	if s.pairingClient == nil {
		return protocol.RoomPage{}, pairingClientMissingError()
	}

	rooms, err := s.pairingClient.FetchRooms(ctx, pairing)
	if err != nil {
		s.setPairingStatus(pairing.ID, "error")
		return protocol.RoomPage{}, remotePairingError(err)
	}
	s.setPairingStatus(pairing.ID, "connected")

	selected, info, err := paginateRooms(rooms, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.RoomPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.RoomPage{}, err
	}

	return protocol.RoomPage{
		Rooms: selected,
		Page:  info,
	}, nil
}

func (s *Service) PairingAgents(ctx context.Context, pairingID string) ([]protocol.AgentSummary, error) {
	page, err := s.PairingAgentsContext(ctx, pairingID, protocol.PageRequest{})
	if err != nil {
		return nil, err
	}
	return page.Agents, nil
}

func (s *Service) PairingAgentsContext(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.AgentPage, error) {
	pairing, err := s.findPairing(pairingID)
	if err != nil {
		return protocol.AgentPage{}, err
	}
	if s.pairingClient == nil {
		return protocol.AgentPage{}, pairingClientMissingError()
	}

	agents, err := s.pairingClient.FetchAgents(ctx, pairing)
	if err != nil {
		s.setPairingStatus(pairing.ID, "error")
		return protocol.AgentPage{}, remotePairingError(err)
	}
	s.setPairingStatus(pairing.ID, "connected")

	selected, info, err := paginateAgentValues(agents, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.AgentPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.AgentPage{}, err
	}

	return protocol.AgentPage{
		Agents: selected,
		Page:   info,
	}, nil
}

func (s *Service) findPairing(pairingID string) (protocol.Pairing, error) {
	if s.pairingClient == nil {
		return protocol.Pairing{}, pairingClientMissingError()
	}

	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()
	for _, pairing := range s.pairings {
		if pairing.ID == pairingID {
			return pairing, nil
		}
	}

	return protocol.Pairing{}, unknownPairingError(pairingID)
}

func (s *Service) snapshotPairings() []protocol.Pairing {
	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()

	pairings := make([]protocol.Pairing, 0, len(s.pairings))
	for _, pairing := range s.pairings {
		pairing.Token = ""
		pairing.Status = s.pairingStatuses[pairing.ID].value
		pairings = append(pairings, pairing)
	}

	return pairings
}
