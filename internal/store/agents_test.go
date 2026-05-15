package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAgentRegistryStoresCredentialOwnership(t *testing.T) {
	t.Parallel()

	registry := NewMemoryStore()
	first := protocol.AgentRegistration{
		NetworkID:     "local",
		AgentID:       "director",
		ActorUID:      "actor_1",
		ActorURI:      protocol.AgentFQID("local", "director"),
		DisplayName:   "Director",
		CredentialKey: "token:one",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	registered, err := registry.RegisterAgentContext(context.Background(), first)
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if registered.ActorUID != "actor_1" {
		t.Fatalf("unexpected registration %#v", registered)
	}

	second := first
	second.DisplayName = "Director Prime"
	second.CredentialKey = "token:one"
	registered, err = registry.RegisterAgentContext(context.Background(), second)
	if err != nil {
		t.Fatalf("idempotent RegisterAgentContext() error = %v", err)
	}
	if registered.ActorUID != "actor_1" || registered.DisplayName != "Director Prime" {
		t.Fatalf("unexpected idempotent registration %#v", registered)
	}

	conflict := first
	conflict.CredentialKey = "token:two"
	if _, err := registry.RegisterAgentContext(context.Background(), conflict); !errors.Is(err, ErrAgentCredential) {
		t.Fatalf("expected credential conflict, got %v", err)
	}

	byCredential, ok, err := registry.GetRegisteredAgentByCredentialKeyContext(context.Background(), "token:one")
	if err != nil {
		t.Fatalf("GetRegisteredAgentByCredentialKeyContext() error = %v", err)
	}
	if !ok || byCredential.AgentID != "director" || byCredential.AgentToken != "" {
		t.Fatalf("unexpected credential lookup ok=%v value=%#v", ok, byCredential)
	}
}

func TestSQLAgentRegistryPersists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "moltnet.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	registration := protocol.AgentRegistration{
		NetworkID:     "local",
		AgentID:       "director",
		ActorUID:      "actor_1",
		ActorURI:      protocol.AgentFQID("local", "director"),
		DisplayName:   "Director",
		CredentialKey: "token:one",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if _, err := store.RegisterAgentContext(context.Background(), registration); err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore() error = %v", err)
	}
	defer reopened.Close()

	registered, ok, err := reopened.GetRegisteredAgentContext(context.Background(), "director")
	if err != nil {
		t.Fatalf("GetRegisteredAgentContext() error = %v", err)
	}
	if !ok || registered.ActorUID != "actor_1" || registered.CredentialKey != "token:one" {
		t.Fatalf("unexpected persisted registration ok=%v value=%#v", ok, registered)
	}

	byCredential, ok, err := reopened.GetRegisteredAgentByCredentialKeyContext(context.Background(), "token:one")
	if err != nil {
		t.Fatalf("GetRegisteredAgentByCredentialKeyContext() error = %v", err)
	}
	if !ok || byCredential.AgentID != "director" {
		t.Fatalf("unexpected credential lookup ok=%v value=%#v", ok, byCredential)
	}
}

func TestAgentRegistryDoesNotStorePlaintextAgentToken(t *testing.T) {
	t.Parallel()

	registry := NewMemoryStore()
	registration := protocol.AgentRegistration{
		NetworkID:     "local",
		AgentID:       "luna",
		ActorUID:      "actor_1",
		ActorURI:      protocol.AgentFQID("local", "luna"),
		AgentToken:    "magt_v1_plaintext",
		CredentialKey: "agent-token:hash",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	registered, err := registry.RegisterAgentContext(context.Background(), registration)
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if registered.AgentToken != "" {
		t.Fatalf("store returned plaintext token %#v", registered)
	}
	fetched, ok, err := registry.GetRegisteredAgentContext(context.Background(), "luna")
	if err != nil || !ok {
		t.Fatalf("GetRegisteredAgentContext() ok=%v err=%v", ok, err)
	}
	if fetched.AgentToken != "" {
		t.Fatalf("store retained plaintext token %#v", fetched)
	}
}

func TestRemoveRegisteredAgentAndMemberships(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if _, err := store.RegisterAgentContext(context.Background(), protocol.AgentRegistration{
		NetworkID:     "local",
		AgentID:       "luna",
		ActorUID:      "actor_1",
		ActorURI:      protocol.AgentFQID("local", "luna"),
		CredentialKey: "token:one",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if err := store.CreateRoom(protocol.Room{
		ID:        "agora",
		NetworkID: "local",
		FQID:      protocol.RoomFQID("local", "agora"),
		Name:      "Agora",
		Members:   []string{"luna", "socrates"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	if err := store.RemoveAgentContext(context.Background(), "luna"); err != nil {
		t.Fatalf("RemoveAgentContext() error = %v", err)
	}
	if err := store.RemoveRegisteredAgentContext(context.Background(), "luna"); err != nil {
		t.Fatalf("RemoveRegisteredAgentContext() error = %v", err)
	}

	room, ok, err := store.GetRoom("agora")
	if err != nil || !ok {
		t.Fatalf("GetRoom() ok=%v err=%v", ok, err)
	}
	if len(room.Members) != 1 || room.Members[0] != "socrates" {
		t.Fatalf("unexpected members after removal %#v", room.Members)
	}
	if _, ok, err := store.GetRegisteredAgentContext(context.Background(), "luna"); err != nil || ok {
		t.Fatalf("expected registration removed, ok=%v err=%v", ok, err)
	}
}
