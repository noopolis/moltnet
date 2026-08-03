package rooms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const joinTestCredential = "room-join-secret"

func TestJoinRoomGrantsMembershipForWriteAndRead(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:          "research",
		Visibility:  protocol.RoomVisibilityPrivate,
		WritePolicy: protocol.RoomWritePolicyMembers,
		Credential:  protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	ctx := bearerClaimsContext(claims)
	message := roomSend("research", "writer")

	if _, err := service.SendMessageContext(ctx, message); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("non-member message error = %v, want write forbidden", err)
	}
	assertStoredRoomMessageCount(t, service, "research", 0)

	joined, err := service.JoinRoomContext(ctx, "research", protocol.JoinRoomRequest{
		From:       message.From,
		Credential: protocol.NewSecretString(joinTestCredential),
	})
	if err != nil {
		t.Fatalf("JoinRoomContext() error = %v", err)
	}
	if !actorIsRoomMember(joined, message.From) || joined.Access == nil || !joined.Access.CanRead {
		t.Fatalf("joined room = %#v, want readable writer membership", joined)
	}

	if _, err := service.SendMessageContext(ctx, message); err != nil {
		t.Fatalf("joined member message error = %v", err)
	}
	assertStoredRoomMessageCount(t, service, "research", 1)

	page, err := service.ListRoomsContext(ctx, protocol.PageRequest{Limit: 10})
	if err != nil || len(page.Rooms) != 1 || page.Rooms[0].ID != "research" {
		t.Fatalf("joined actor room list = %#v, %v", page, err)
	}
}

func TestJoinRoomRejectsWrongCredentialWithoutWriting(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	ctx := bearerClaimsContext(claims)
	message := roomSend("research", "writer")

	if _, err := service.JoinRoomContext(ctx, "research", protocol.JoinRoomRequest{
		From:       message.From,
		Credential: protocol.NewSecretString("wrong-secret"),
	}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("wrong credential error = %v, want generic forbidden", err)
	}
	if _, err := service.SendMessageContext(ctx, message); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("write after rejected join error = %v, want write forbidden", err)
	}
	assertStoredRoomMessageCount(t, service, "research", 0)
}

func TestJoinRoomMembershipDoesNotBypassOperatorsPolicy(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:          "operators",
		WritePolicy: protocol.RoomWritePolicyOperators,
		Credential:  protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	ctx := bearerClaimsContext(claims)
	message := roomSend("operators", "writer")
	if _, err := service.JoinRoomContext(ctx, "operators", protocol.JoinRoomRequest{
		From:       message.From,
		Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatalf("JoinRoomContext() error = %v", err)
	}
	if _, err := service.SendMessageContext(ctx, message); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("operator room write after join error = %v, want write forbidden", err)
	}
	assertStoredRoomMessageCount(t, service, "operators", 0)
}

func TestJoinRoomBindsCredentialToAuthenticatedAgent(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{ID: "research", Credential: protocol.NewSecretString(joinTestCredential)}); err != nil {
		t.Fatal(err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	ctx := bearerClaimsContext(claims)
	if _, err := service.JoinRoomContext(ctx, "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "other"},
		Credential: protocol.NewSecretString(joinTestCredential),
	}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("cross-agent join error = %v, want forbidden", err)
	}
}

func TestJoinRoomRejectsPairTokenClaimingLocalAgentWithoutMutation(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Members:    []string{"founder"},
		Visibility: protocol.RoomVisibilityPrivate,
		Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	claims := authn.NewStaticClaims(authn.TokenConfig{ID: "remote-b", Scopes: []authn.Scope{authn.ScopePair}})
	ctx := bearerClaimsContext(claims)

	_, err := service.JoinRoomContext(ctx, "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "victim"},
		Credential: protocol.NewSecretString(joinTestCredential),
	})
	if !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("pair-token join error = %v, want forbidden", err)
	}
	assertRoomDoesNotContainMember(t, service, "research", "victim")
}

func TestJoinRoomRejectsUnscopedStaticWriteTokenWithoutMutation(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Members:    []string{"founder"},
		Visibility: protocol.RoomVisibilityPrivate,
		Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	claims := authn.NewStaticClaims(authn.TokenConfig{ID: "operator", Scopes: []authn.Scope{authn.ScopeWrite}})

	_, err := service.JoinRoomContext(bearerClaimsContext(claims), "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "victim"},
		Credential: protocol.NewSecretString(joinTestCredential),
	})
	if !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("static-token join error = %v, want forbidden", err)
	}
	assertRoomDoesNotContainMember(t, service, "research", "victim")
}

func TestReconcileRoomRevokesOldCredentialBeforeAcceptingNewCredential(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString("credential-a"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcileRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString("credential-b"),
	}); err != nil {
		t.Fatalf("ReconcileRoomContext() error = %v", err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	ctx := bearerClaimsContext(claims)

	if _, err := service.JoinRoomContext(ctx, "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "writer"},
		Credential: protocol.NewSecretString("credential-a"),
	}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("old credential join error = %v, want forbidden", err)
	}
	assertRoomDoesNotContainMember(t, service, "research", "writer")
	if _, err := service.JoinRoomContext(ctx, "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "writer"},
		Credential: protocol.NewSecretString("credential-b"),
	}); err != nil {
		t.Fatalf("new credential join error = %v", err)
	}
}

func TestReconcileRoomRestoresCredentialWhenStoreUpdateFails(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString("credential-a"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.store.RemoveRoom("research"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcileRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString("credential-b"),
	}); !errors.Is(err, ErrUnknownRoom) {
		t.Fatalf("ReconcileRoomContext() error = %v, want unknown room", err)
	}
	credential, ok := service.roomCredential("research")
	if !ok || credential.Reveal() != "credential-a" {
		t.Fatalf("credential after failed reconcile = %q, configured %v", credential.Reveal(), ok)
	}
}

func TestCreateRoomRejectsCredentialWhenAuthIsNone(t *testing.T) {
	service := newAgentRegistryTestService()
	ctx := authn.WithMode(context.Background(), authn.ModeNone)
	_, err := service.CreateRoomContext(ctx, protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString(joinTestCredential),
	})
	if !errors.Is(err, ErrInvalidRoomRequest) {
		t.Fatalf("CreateRoomContext() error = %v, want invalid room request", err)
	}
	if _, ok, err := service.store.GetRoom("research"); err != nil || ok {
		t.Fatalf("stored room after rejected create = ok %v, err %v", ok, err)
	}
}

func TestApplyConfigRepopulatesRoomCredentialAfterReconcile(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyConfigContext(credentialModeContext(), protocol.ApplyConfigRequest{Rooms: []protocol.CreateRoomRequest{{
		ID:         "research",
		Credential: protocol.NewSecretString(joinTestCredential),
	}}}); err != nil {
		t.Fatal(err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	if _, err := service.JoinRoomContext(bearerClaimsContext(claims), "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "writer"},
		Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatalf("join after reconciled config error = %v", err)
	}
	if _, err := service.RemoveRoomContext(context.Background(), "research"); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.roomCredential("research"); ok {
		t.Fatal("removed room credential remained in memory")
	}
}

func TestRoomCredentialNeverAppearsInRoomEventsOrStores(t *testing.T) {
	service := newCredentialTestService(store.NewMemoryStore())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := service.Subscribe(ctx)
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{ID: "research", Credential: protocol.NewSecretString(joinTestCredential)}); err != nil {
		t.Fatal(err)
	}
	assertSecretAbsentFromJSON(t, <-stream)

	claims := authn.NewAgentTokenClaims("writer", "agent-token:writer")
	joinCtx := bearerClaimsContext(claims)
	if _, err := service.JoinRoomContext(joinCtx, "research", protocol.JoinRoomRequest{
		From:       protocol.Actor{Type: "agent", ID: "writer"},
		Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	assertSecretAbsentFromJSON(t, <-stream)

	room, err := service.GetRoomContext(context.Background(), "research")
	if err != nil {
		t.Fatal(err)
	}
	assertSecretAbsentFromJSON(t, room)

	sqlStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlStore.Close()
	sqlService := newCredentialTestService(sqlStore)
	if _, err := sqlService.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID: "sql-room", Credential: protocol.NewSecretString(joinTestCredential),
	}); err != nil {
		t.Fatal(err)
	}
	sqlRoom, ok, err := sqlStore.GetRoomContext(context.Background(), "sql-room")
	if err != nil || !ok {
		t.Fatalf("SQL GetRoomContext() = %#v, %v, %v", sqlRoom, ok, err)
	}
	assertSecretAbsentFromJSON(t, sqlRoom)
}

func newCredentialTestService(roomStore store.RoomStore) *Service {
	return NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             roomStore,
		Messages:          roomStore.(store.MessageStore),
		Broker:            events.NewBroker(),
	})
}

func assertStoredRoomMessageCount(t *testing.T, service *Service, roomID string, want int) {
	t.Helper()
	page, err := service.listRoomMessages(context.Background(), roomID, protocol.PageRequest{Limit: 10})
	if err != nil || len(page.Messages) != want {
		t.Fatalf("stored messages = %#v, %v; want %d", page, err, want)
	}
}

func credentialModeContext() context.Context {
	return authn.WithMode(context.Background(), authn.ModeBearer)
}

func assertRoomDoesNotContainMember(t *testing.T, service *Service, roomID string, memberID string) {
	t.Helper()
	room, ok, err := service.store.GetRoom(roomID)
	if err != nil || !ok {
		t.Fatalf("stored room %q = %#v, ok %v, err %v", roomID, room, ok, err)
	}
	for _, member := range room.Members {
		if member == memberID {
			t.Fatalf("room %q unexpectedly contains member %q: %#v", roomID, memberID, room.Members)
		}
	}
}

func assertSecretAbsentFromJSON(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if bytes.Contains(encoded, []byte(joinTestCredential)) {
		t.Fatalf("credential leaked in serialized bytes %s", encoded)
	}
}

func TestJoinRoomIsIdempotentWithoutExtraMembershipEvent(t *testing.T) {
	service := newAgentRegistryTestService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{ID: "research", Credential: protocol.NewSecretString(joinTestCredential)}); err != nil {
		t.Fatal(err)
	}
	claims := registerPolicyAgent(t, service, "writer")
	joinCtx := bearerClaimsContext(claims)
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := service.Subscribe(streamCtx)
	request := protocol.JoinRoomRequest{From: protocol.Actor{Type: "agent", ID: "writer"}, Credential: protocol.NewSecretString(joinTestCredential)}
	if _, err := service.JoinRoomContext(joinCtx, "research", request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stream:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for membership event")
	}
	if _, err := service.JoinRoomContext(joinCtx, "research", request); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-stream:
		t.Fatalf("unexpected idempotent join event %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}
