package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestNewFileStoreHandlesMissingAndInvalidFiles(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "missing", "state.json")
	store, err := NewFileStore(missingPath)
	if err != nil {
		t.Fatalf("NewFileStore() missing file error = %v", err)
	}
	rooms, err := store.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if len(rooms) != 0 {
		t.Fatalf("expected empty store, got %#v", rooms)
	}

	invalidPath := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(invalidPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}
	if _, err := NewFileStore(invalidPath); err == nil {
		t.Fatal("expected invalid snapshot error")
	}
}

func TestFileStorePersistsDirectConversationMembers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	now := time.Now().UTC()
	if err := store.AppendMessage(protocol.Message{
		ID:        "msg_dm",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"alpha", "beta"}},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: "text", Text: "ping"}},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reload FileStore error = %v", err)
	}

	conversations, err := reloaded.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(conversations) != 1 || len(conversations[0].ParticipantIDs) != 2 {
		t.Fatalf("unexpected conversations %#v", conversations)
	}
}

func TestFileStorePersistErrors(t *testing.T) {
	t.Parallel()

	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	store.path = filepath.Join(parentFile, "state.json")

	room := protocol.Room{ID: "research", NetworkID: "local", FQID: protocol.RoomFQID("local", "research"), Name: "Research"}
	err = store.CreateRoom(room)
	if err == nil || !strings.Contains(err.Error(), "create Moltnet store dir") {
		t.Fatalf("expected directory creation error, got %v", err)
	}
}

func TestFileStoreRestoresStateOnPersistFailure(t *testing.T) {
	t.Parallel()

	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	store.path = filepath.Join(parentFile, "state.json")
	room := protocol.Room{ID: "research", NetworkID: "local", FQID: protocol.RoomFQID("local", "research"), Name: "Research"}
	if err := store.CreateRoom(room); err == nil {
		t.Fatal("expected create room persist error")
	}
	if _, ok, err := store.GetRoom("research"); err != nil || ok {
		if err != nil {
			t.Fatalf("GetRoom() error = %v", err)
		}
		t.Fatal("expected room rollback after persist failure")
	}

	store.path = filepath.Join(t.TempDir(), "state.json")
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() recovery error = %v", err)
	}

	store.path = filepath.Join(parentFile, "state.json")
	message := protocol.Message{
		ID:        "msg_1",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.AppendMessage(message); err == nil {
		t.Fatal("expected append persist error")
	}
	page, err := store.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected message rollback after persist failure, got %#v", page)
	}
}
