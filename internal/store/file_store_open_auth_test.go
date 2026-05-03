package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestFileStoreOpenAgentTokenAuthenticatesAfterReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	fileStore, err := store.NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	service := newFileStoreOpenAuthService(fileStore)
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "agora"}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	registered, err := service.RegisterAgentContext(
		authn.WithMode(context.Background(), authn.ModeOpen),
		protocol.RegisterAgentRequest{RequestedAgentID: "luna", Name: "Luna"},
	)
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if !strings.HasPrefix(registered.AgentToken, authn.AgentTokenPrefix) {
		t.Fatalf("expected open-mode agent token, got %#v", registered)
	}

	state, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(state)
	if !strings.Contains(text, `"credential_key":"`+authn.AgentTokenCredentialKey(registered.AgentToken)+`"`) {
		t.Fatalf("file store did not persist credential key: %s", text)
	}
	if strings.Contains(text, registered.AgentToken) || strings.Contains(text, "agent_token") {
		t.Fatalf("file store persisted agent token secret: %s", text)
	}

	reloadedStore, err := store.NewFileStore(path)
	if err != nil {
		t.Fatalf("reload NewFileStore() error = %v", err)
	}
	reloaded := newFileStoreOpenAuthService(reloadedStore)
	claims, ok, err := reloaded.AuthenticateAgentTokenContext(context.Background(), registered.AgentToken)
	if err != nil || !ok {
		t.Fatalf("AuthenticateAgentTokenContext() ok=%v err=%v", ok, err)
	}
	if !claims.AgentToken() || !claims.AllowsAgent("luna") || !claims.Allows(authn.ScopeWrite) {
		t.Fatalf("unexpected reloaded claims %#v", claims)
	}

	if _, err := reloaded.SendMessageContext(
		authn.WithMode(authn.WithClaims(context.Background(), claims), authn.ModeOpen),
		protocol.SendMessageRequest{
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
			From:   protocol.Actor{Type: "agent", ID: "luna"},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "after reload"}},
		},
	); err != nil {
		t.Fatalf("SendMessageContext() after reload error = %v", err)
	}
}

func newFileStoreOpenAuthService(fileStore *store.FileStore) *rooms.Service {
	return rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             fileStore,
		Messages:          fileStore,
		Broker:            events.NewBroker(),
	})
}
