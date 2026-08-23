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
	rooms = s.filterPairingRooms(ctx, pairing, rooms)

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
	if s.pairingDiscoveryRestricted(ctx) {
		rooms, roomErr := s.pairingClient.FetchRooms(ctx, pairing)
		if roomErr != nil {
			s.setPairingError(pairing.ID, "Remote rooms could not be fetched.")
			return protocol.AgentPage{}, remotePairingError(roomErr)
		}
		agents = s.filterPairingAgents(ctx, pairing, agents, s.filterPairingRooms(ctx, pairing, rooms))
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
	return firstUniqueActivePairing(s.pairings, func(pairing protocol.Pairing) bool {
		return pairing.ID == pairingID
	}, pairingID)
}

// findPairingByID resolves a pairing by its exact id, without depending on a
// configured transport client — unlike the exported findPairing, which
// requires s.pairingClient because it also backs outbound diagnostics
// queries. This is the lookup pairingForPairScopedContext (federation_access.go)
// uses for the 7B.1 placeholder binding: a pair-scoped credential's TokenID
// is minted identical to its pairing's id, so this resolves it even when the
// credential has no confirmed network binding yet.
func (s *Service) findPairingByID(pairingID string) (protocol.Pairing, error) {
	pairingID = strings.TrimSpace(pairingID)
	if pairingID == "" {
		return protocol.Pairing{}, unknownPairingError(pairingID)
	}
	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()
	return firstUniqueActivePairing(s.pairings, func(pairing protocol.Pairing) bool {
		return pairing.ID == pairingID
	}, pairingID)
}

// findPairingByRemoteNetwork resolves an inbound pairing credential without
// depending on a configured transport client.
func (s *Service) findPairingByRemoteNetwork(remoteNetworkID string) (protocol.Pairing, error) {
	remoteNetworkID = strings.TrimSpace(remoteNetworkID)
	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()
	return firstUniqueActivePairing(s.pairings, func(pairing protocol.Pairing) bool {
		return strings.TrimSpace(pairing.RemoteNetworkID) == remoteNetworkID
	}, remoteNetworkID)
}

// firstUniqueActivePairing resolves exactly one pairing matching predicate
// out of pairings, failing closed (unknownPairingError) instead of silently
// trusting the first match, in either of two cases (P2, recorded on review):
//
//   - the match's config-declared status is protocol.PairingStatusRevoked --
//     an operator's own hand-edit (`status: revoked` written directly into
//     pairings[] as a manual stopgap before running the actual `pair
//     revoke`, or a config an older revoke path left in this state) must
//     never be resolved as live, even though the pairing/token entries are
//     still physically present in config.
//   - more than one pairing matches predicate (duplicate ids, or -- for the
//     by-remote-network lookup -- two pairings sharing a remote_network_id,
//     one of them plausibly stale): resolving to "whichever came first" is
//     exactly the silent fallback across a stale credential this guards
//     against, so an ambiguous match is treated as no match at all.
func firstUniqueActivePairing(pairings []protocol.Pairing, predicate func(protocol.Pairing) bool, lookupKey string) (protocol.Pairing, error) {
	var match protocol.Pairing
	found := false
	for _, pairing := range pairings {
		if !predicate(pairing) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(pairing.Status), protocol.PairingStatusRevoked) {
			return protocol.Pairing{}, unknownPairingError(lookupKey)
		}
		if found {
			return protocol.Pairing{}, unknownPairingError(lookupKey)
		}
		match = pairing
		found = true
	}
	if !found {
		return protocol.Pairing{}, unknownPairingError(lookupKey)
	}
	return match, nil
}

func (s *Service) snapshotPairings() []protocol.Pairing {
	return s.snapshotPairingsWithTokenPolicy(false)
}

// snapshotRelayPairings is relayMessage/shouldRelayMessage's (relay.go) view
// of the pairing table: unlike snapshotPairings (which lists every
// configured pairing, revoked or not, for `pair list`/`status` reporting),
// this must never hand back a pairing whose status is revoked. F2 (review
// round 2, confirmed live): firstUniqueActivePairing already refuses a
// revoked pairing for direct lookups, and its own doc comment says a
// hand-edited `status: revoked` pairing "must never be resolved as live" --
// but this function, unfiltered, fed relayMessage's iteration directly, so
// that exact hand-edit (the doc's own recommended manual stopgap before
// `pair revoke` finishes, or a config an older revoke path left in this
// state) kept receiving every message relayed out of any room whose
// federation still listed it.
//
// The exclusion is keyed on revokedPairingIDs' raw, config-declared status
// -- the same field firstUniqueActivePairing checks -- not the live
// diagnostic status snapshotPairingsWithTokenPolicy substitutes into each
// returned Pairing.Status. The two cannot diverge for a pairing this process
// loaded as revoked from the start (nothing ever refreshes diagnostics for
// one, since pairingDiagnosticsNeedRefresh/pairingRelayAttemptReady treat an
// unrecognized status value as not-ready), but this codebase has no
// in-process hot-reload of pairing state at all (pair_revoke.go's own doc
// comment): a pairing that was already connected before an operator
// hand-edited `status: revoked` into a still-running process's on-disk
// config keeps its stale "connected" diagnostic cache regardless. Reading
// the raw field directly means this filter does not depend on that cache
// ever being consistent with the file.
func (s *Service) snapshotRelayPairings() []protocol.Pairing {
	revoked := s.revokedPairingIDs()
	pairings := s.snapshotPairingsWithTokenPolicy(true)
	if len(revoked) == 0 {
		return pairings
	}
	active := make([]protocol.Pairing, 0, len(pairings))
	for _, pairing := range pairings {
		if _, isRevoked := revoked[pairing.ID]; isRevoked {
			continue
		}
		active = append(active, pairing)
	}
	return active
}

// revokedPairingIDs returns the set of pairing ids whose raw, config-declared
// Status (s.pairings, before snapshotPairingsWithTokenPolicy overwrites it
// with the live diagnostic status) is revoked. See snapshotRelayPairings'
// doc comment for why the raw field, not the processed one, is what matters
// here.
func (s *Service) revokedPairingIDs() map[string]struct{} {
	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()

	revoked := make(map[string]struct{})
	for _, pairing := range s.pairings {
		if strings.EqualFold(strings.TrimSpace(pairing.Status), protocol.PairingStatusRevoked) {
			revoked[pairing.ID] = struct{}{}
		}
	}
	return revoked
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
