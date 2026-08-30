package rooms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/pairings"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestServiceRelaysRoomMessagesAcrossPairings(t *testing.T) {
	t.Parallel()

	serviceA, serverA := newRelayTestService(t, "net_a", "Net A")
	defer serverA.Close()
	serviceB, serverB := newRelayTestService(t, "net_b", "Net B")
	defer serverB.Close()

	serviceA.pairings = []protocol.Pairing{{
		ID:              "pair_b",
		RemoteNetworkID: "net_b",
		RemoteBaseURL:   serverB.URL,
		Status:          "connected",
	}}
	serviceB.pairings = []protocol.Pairing{{
		ID:              "pair_a",
		RemoteNetworkID: "net_a",
		RemoteBaseURL:   serverA.URL,
		Status:          "connected",
	}}

	for _, service := range []*Service{serviceA, serviceB} {
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research", Members: []string{"alpha", "beta"}, Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll}}); err != nil {
			t.Fatalf("CreateRoom() error = %v", err)
		}
	}

	accepted, err := serviceA.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "relay this to net_b"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if !strings.HasPrefix(accepted.MessageID, "msg_") {
		t.Fatalf("unexpected accepted id %#v", accepted)
	}

	pageA, err := serviceA.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(net_a) error = %v", err)
	}
	if len(pageA.Messages) != 1 {
		t.Fatalf("expected one local message, got %#v", pageA)
	}

	pageB := waitForRoomMessage(t, serviceB, "research")

	message := pageB.Messages[0]
	if message.ID != accepted.MessageID {
		t.Fatalf("expected relayed message id %q, got %#v", accepted.MessageID, message)
	}
	if message.Origin.NetworkID != "net_a" || message.Origin.MessageID != accepted.MessageID {
		t.Fatalf("unexpected message origin %#v", message.Origin)
	}
	if message.From.NetworkID != "net_a" || message.From.FQID != protocol.AgentFQID("net_a", "alpha") {
		t.Fatalf("unexpected relayed sender %#v", message.From)
	}

	pageAAfter, err := serviceA.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(net_a second) error = %v", err)
	}
	if len(pageAAfter.Messages) != 1 {
		t.Fatalf("expected relay loop prevention, got %#v", pageAAfter)
	}
}

func TestServiceRelaysRoomMessagesWithPairingToken(t *testing.T) {
	t.Parallel()

	serviceA, serverA := newRelayTestService(t, "net_a", "Net A")
	defer serverA.Close()
	serviceB, serverB := newRelayTestServiceWithBearer(t, "net_b", "Net B", "pair-secret")
	defer serverB.Close()
	if _, err := serviceB.RegisterAgentContext(
		authn.WithClaims(context.Background(), authn.Claims{TokenID: "local-alpha"}),
		protocol.RegisterAgentRequest{RequestedAgentID: "alpha", Name: "Local Alpha"},
	); err != nil {
		t.Fatalf("RegisterAgentContext() local collision setup error = %v", err)
	}

	// P3: pairingsMu guards every production read of Service.pairings
	// (findPairingByID/findPairingByRemoteNetwork/snapshotPairings); taking
	// it here too, even though this setup runs before either service is
	// exercised concurrently, keeps this test from being the one place in
	// the package that assumes the field is safe to touch unguarded.
	serviceA.pairingsMu.Lock()
	serviceA.pairings = []protocol.Pairing{{
		ID:              "pair_b",
		RemoteNetworkID: "net_b",
		RemoteBaseURL:   serverB.URL,
		Token:           "pair-secret",
		Status:          "connected",
	}}
	serviceA.pairingsMu.Unlock()
	// 7B.1: the relayed send below authenticates against serviceB with a
	// pair-scoped credential minted TokenID "pair-relay" (newRelayTestHandler
	// above), so serviceB needs a matching pairing for
	// pairingForPairScopedContext to resolve it -- otherwise "research"'s
	// federation: all is never even reached. RemoteNetworkID mirrors net_a
	// for realism but plays no role in resolution here (TokenID is what
	// matches).
	serviceB.pairingsMu.Lock()
	serviceB.pairings = []protocol.Pairing{{
		ID:              "pair-relay",
		RemoteNetworkID: "net_a",
		RemoteBaseURL:   serverA.URL,
		Status:          "connected",
	}}
	serviceB.pairingsMu.Unlock()

	for _, target := range []struct {
		service *Service
		members []string
	}{
		{service: serviceA, members: []string{"alpha", "beta"}},
		{service: serviceB, members: []string{"alpha", "beta", "net_a:alpha"}},
	} {
		if _, err := target.service.CreateRoom(protocol.CreateRoomRequest{ID: "research", Members: target.members, Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll}}); err != nil {
			t.Fatalf("CreateRoom() error = %v", err)
		}
	}

	accepted, err := serviceA.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "relay with auth"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	pageB := waitForRoomMessage(t, serviceB, "research")
	if pageB.Messages[0].ID != accepted.MessageID {
		t.Fatalf("expected relayed message id %q, got %#v", accepted.MessageID, pageB.Messages[0])
	}
}

func TestServiceRelaysScopedDirectMessagesAcrossPairings(t *testing.T) {
	t.Parallel()

	serviceA, serverA := newRelayTestService(t, "net_a", "Net A")
	defer serverA.Close()
	serviceB, serverB := newRelayTestService(t, "net_b", "Net B")
	defer serverB.Close()

	serviceA.pairings = []protocol.Pairing{{
		ID:              "pair_b",
		RemoteNetworkID: "net_b",
		RemoteBaseURL:   serverB.URL,
		Status:          "connected",
	}}
	serviceB.pairings = []protocol.Pairing{{
		ID:              "pair_a",
		RemoteNetworkID: "net_a",
		RemoteBaseURL:   serverA.URL,
		Status:          "connected",
	}}

	_, err := serviceA.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind: protocol.TargetKindDM,
			DMID: "dm_alpha_gamma",
			ParticipantIDs: []string{
				protocol.ScopedAgentID("net_a", "alpha"),
				protocol.ScopedAgentID("net_b", "gamma"),
			},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: "text", Text: "ping remote gamma"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() dm error = %v", err)
	}

	pageB := waitForDMMessage(t, serviceB, "dm_alpha_gamma")

	message := pageB.Messages[0]
	if len(message.Target.ParticipantIDs) != 2 || message.Target.ParticipantIDs[0] != "net_a:alpha" || message.Target.ParticipantIDs[1] != "net_b:gamma" {
		t.Fatalf("unexpected relayed participants %#v", message.Target.ParticipantIDs)
	}
	if message.From.NetworkID != "net_a" || message.Origin.NetworkID != "net_a" {
		t.Fatalf("unexpected relayed dm metadata %#v", message)
	}

	conversations, err := serviceB.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(conversations.DMs) != 1 || len(conversations.DMs[0].ParticipantIDs) != 2 || conversations.DMs[0].ParticipantIDs[0] != "net_a:alpha" || conversations.DMs[0].ParticipantIDs[1] != "net_b:gamma" {
		t.Fatalf("unexpected relayed conversations %#v", conversations)
	}
}

// TestServiceDoesNotRelayToRevokedPairing is F2's required regression
// (review round 2, confirmed live): a pairing hand-marked `status: revoked`
// -- firstUniqueActivePairing's own doc comment calls this the manual
// stopgap an operator reaches for before `pair revoke` finishes, and says it
// "must never be resolved as live" -- must not keep receiving messages
// relayed out of a room whose federation list still names it.
// snapshotRelayPairings previously applied no status filter at all, so
// relayMessage (relay.go) fed it directly into shouldAttemptRelayToPairing
// regardless of status.
//
// The diagnostic cache (pairingStatuses) is deliberately seeded to
// "connected", not "revoked": this is the realistic shape of the bug, not
// just its simplest repro. This codebase has no in-process hot-reload of
// pairing state (pair_revoke.go's own doc comment) -- an operator hand-edits
// `status: revoked` into the on-disk config of an already-running process
// whose pairing was live and connected moments before, and that edit alone
// never touches the running process's diagnostic cache. A fix that filtered
// on the live cache's status instead of the raw config-declared field would
// pass a same-value repro (both "revoked") while still relaying in this one.
func TestServiceDoesNotRelayToRevokedPairing(t *testing.T) {
	t.Parallel()

	serviceA, serverA := newRelayTestService(t, "net_a", "Net A")
	defer serverA.Close()
	serviceB, serverB := newRelayTestService(t, "net_b", "Net B")
	defer serverB.Close()

	serviceA.pairings = []protocol.Pairing{{
		ID:              "pair_b",
		RemoteNetworkID: "net_b",
		RemoteBaseURL:   serverB.URL,
		Status:          protocol.PairingStatusRevoked,
	}}
	serviceA.pairingStatuses["pair_b"] = pairingStatus{value: protocol.PairingStatusConnected}
	serviceB.pairings = []protocol.Pairing{{
		ID:              "pair_a",
		RemoteNetworkID: "net_a",
		RemoteBaseURL:   serverA.URL,
		Status:          "connected",
	}}

	for _, service := range []*Service{serviceA, serviceB} {
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research", Members: []string{"alpha", "beta"}, Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll}}); err != nil {
			t.Fatalf("CreateRoom() error = %v", err)
		}
	}

	if _, err := serviceA.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "must not reach the revoked pairing"}},
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	// relayMessage's revoked-pairing filter runs synchronously, before any
	// relay goroutine is spawned, so there is no network round trip to race
	// against here -- but give any (incorrect) relay attempt a moment to
	// land before asserting its absence, to keep this test honest if the
	// filter ever moves later in the call chain.
	time.Sleep(100 * time.Millisecond)

	pageB, err := serviceB.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(net_b) error = %v", err)
	}
	if len(pageB.Messages) != 0 {
		t.Fatalf("revoked pairing received a relayed message: %#v", pageB.Messages)
	}
}

// TestSnapshotRelayPairingsExcludesRevokedByRawStatus is
// TestServiceDoesNotRelayToRevokedPairing's unit-level counterpart, isolating
// snapshotRelayPairings itself from pairingRelayAttemptReady/
// pairingRelayAllowed's own (incidental, value-switch-based) refusal to
// treat an unrecognized status as ready -- so this fails on its own if
// snapshotRelayPairings' filter regresses, independent of whether some other
// gate happens to also catch the same case.
func TestSnapshotRelayPairingsExcludesRevokedByRawStatus(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		NetworkID: "local",
		Store:     memory, Messages: memory, Broker: events.NewBroker(),
	})
	service.pairings = []protocol.Pairing{
		{ID: "pair_live", RemoteNetworkID: "remote-live", RemoteBaseURL: "http://example.invalid", Status: protocol.PairingStatusConnected},
		{ID: "pair_revoked", RemoteNetworkID: "remote-revoked", RemoteBaseURL: "http://example.invalid", Status: protocol.PairingStatusRevoked},
	}
	// The diagnostic cache still says "connected" for the revoked pairing --
	// exactly the stale-cache-after-hand-edit shape described above -- so
	// this only passes if the filter reads the raw field, not the cache.
	service.pairingStatuses["pair_revoked"] = pairingStatus{value: protocol.PairingStatusConnected}

	ids := make(map[string]bool)
	for _, pairing := range service.snapshotRelayPairings() {
		ids[pairing.ID] = true
	}
	if !ids["pair_live"] {
		t.Fatalf("snapshotRelayPairings() dropped a live pairing: %v", ids)
	}
	if ids["pair_revoked"] {
		t.Fatalf("snapshotRelayPairings() included a revoked pairing despite its stale-connected diagnostic cache: %v", ids)
	}
}

func newRelayTestService(t *testing.T, networkID string, networkName string) (*Service, *httptest.Server) {
	return newRelayTestServiceWithBearer(t, networkID, networkName, "")
}

func newRelayTestServiceWithBearer(t *testing.T, networkID string, networkName string, expectedBearer string) (*Service, *httptest.Server) {
	t.Helper()

	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         networkID,
		NetworkName:       networkName,
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
		PairingClient:     pairings.NewClient(),
	})

	server := httptest.NewServer(newRelayTestHandler(service, expectedBearer))
	return service, server
}

func newRelayTestHandler(service *Service, expectedBearer string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/v1/network" {
			writeRelayTestJSON(response, compatibleRemoteNetwork(service.networkID))
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
			http.NotFound(response, request)
			return
		}
		if expectedBearer != "" && request.Header.Get("Authorization") != "Bearer "+expectedBearer {
			http.Error(response, "authorization required", http.StatusUnauthorized)
			return
		}

		var payload protocol.SendMessageRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := request.Context()
		if expectedBearer != "" {
			policy, err := authn.NewPolicy(authn.Config{
				Mode: authn.ModeBearer,
				Tokens: []authn.TokenConfig{{
					ID:     "pair-relay",
					Value:  expectedBearer,
					Scopes: []authn.Scope{authn.ScopePair},
				}},
			})
			if err != nil {
				http.Error(response, err.Error(), http.StatusInternalServerError)
				return
			}
			claims, err := policy.AuthenticateRequest(request, authn.ScopePair)
			if err != nil {
				http.Error(response, err.Error(), http.StatusUnauthorized)
				return
			}
			ctx = authn.WithClaims(ctx, claims)
		}

		accepted, err := service.SendMessageContext(ctx, payload)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadGateway)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(response).Encode(accepted)
	})
}

func writeRelayTestJSON(response http.ResponseWriter, payload any) {
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(payload); err != nil {
		http.Error(response, err.Error(), http.StatusInternalServerError)
	}
}

func waitForRoomMessage(t *testing.T, service *Service, roomID string) protocol.MessagePage {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, err := service.ListRoomMessages(roomID, "", 10)
		if err == nil && len(page.Messages) > 0 {
			return page
		}
		time.Sleep(20 * time.Millisecond)
	}

	page, err := service.ListRoomMessages(roomID, "", 10)
	t.Fatalf("timed out waiting for room relay, page=%#v err=%v", page, err)
	return protocol.MessagePage{}
}

func waitForDMMessage(t *testing.T, service *Service, dmID string) protocol.MessagePage {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, err := service.ListDMMessages(dmID, "", 10)
		if err == nil && len(page.Messages) > 0 {
			return page
		}
		time.Sleep(20 * time.Millisecond)
	}

	page, err := service.ListDMMessages(dmID, "", 10)
	t.Fatalf("timed out waiting for dm relay, page=%#v err=%v", page, err)
	return protocol.MessagePage{}
}
