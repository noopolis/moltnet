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

	items := make([]pairingItem, 0, len(pairings))
	for _, pairing := range pairings {
		items = append(items, pairingItem{Pairing: pairing})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.PairingPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.PairingPage{}, err
	}
	values := make([]protocol.Pairing, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.Pairing)
	}

	return protocol.PairingPage{
		Pairings: values,
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

	network, _, err := s.refreshPairingDiagnostics(ctx, pairing, pairingCheckNetwork)
	if err != nil {
		return protocol.Network{}, remotePairingError(err)
	}
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

	_, status, err := s.refreshPairingDiagnostics(ctx, pairing, pairingCheckDiscovery)
	if err != nil {
		return protocol.RoomPage{}, remotePairingError(err)
	}
	if err := pairingDiscoveryError(status); err != nil {
		return protocol.RoomPage{}, remotePairingError(err)
	}

	rooms, err := s.pairingClient.FetchRooms(ctx, pairing)
	if err != nil {
		s.setPairingError(pairing.ID, "Remote rooms could not be fetched.")
		return protocol.RoomPage{}, remotePairingError(err)
	}

	items := make([]roomItem, 0, len(rooms))
	for _, room := range rooms {
		items = append(items, roomItem{Room: room})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.RoomPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.RoomPage{}, err
	}
	values := make([]protocol.Room, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.Room)
	}

	return protocol.RoomPage{
		Rooms: values,
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

	_, status, err := s.refreshPairingDiagnostics(ctx, pairing, pairingCheckDiscovery)
	if err != nil {
		return protocol.AgentPage{}, remotePairingError(err)
	}
	if err := pairingDiscoveryError(status); err != nil {
		return protocol.AgentPage{}, remotePairingError(err)
	}

	agents, err := s.pairingClient.FetchAgents(ctx, pairing)
	if err != nil {
		s.setPairingError(pairing.ID, "Remote agents could not be fetched.")
		return protocol.AgentPage{}, remotePairingError(err)
	}

	items := make([]agentItem, 0, len(agents))
	for _, agent := range agents {
		items = append(items, agentItem{AgentSummary: agent})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.AgentPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.AgentPage{}, err
	}
	values := make([]protocol.AgentSummary, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.AgentSummary)
	}

	return protocol.AgentPage{
		Agents: values,
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
	return s.snapshotPairingsWithTokenPolicy(false)
}

func (s *Service) snapshotRelayPairings() []protocol.Pairing {
	return s.snapshotPairingsWithTokenPolicy(true)
}

func (s *Service) snapshotPairingsWithTokenPolicy(includeTokens bool) []protocol.Pairing {
	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()

	pairings := make([]protocol.Pairing, 0, len(s.pairings))
	for _, pairing := range s.pairings {
		if !includeTokens {
			pairing.Token = ""
		}
		status := s.pairingStatusForPairingLocked(pairing)
		pairing.Status = status.value
		pairing.Diagnostics = clonePairingDiagnostics(status.diagnostics)
		pairings = append(pairings, pairing)
	}

	return pairings
}

func (s *Service) pairingStatusForPairingLocked(pairing protocol.Pairing) pairingStatus {
	status := s.pairingStatuses[pairing.ID]
	if strings.TrimSpace(status.value) != "" {
		return status
	}
	return pairingStatus{
		value: normalizePairingStatus(pairing.Status),
	}
}
