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
}
