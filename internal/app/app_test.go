package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestNew(t *testing.T) {
	t.Parallel()

	instance, err := New(Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if instance.server == nil {
		t.Fatal("expected server")
	}
	if instance.server.Addr != ":0" {
		t.Fatalf("unexpected addr %q", instance.server.Addr)
	}
	if instance.server.ReadTimeout != defaultReadTimeout {
		t.Fatalf("unexpected read timeout %s", instance.server.ReadTimeout)
	}
	if instance.server.WriteTimeout != defaultWriteTimeout {
		t.Fatalf("unexpected write timeout %s", instance.server.WriteTimeout)
	}
	if instance.server.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("unexpected idle timeout %s", instance.server.IdleTimeout)
	}
}

func TestRunSuccessAndFailure(t *testing.T) {
	t.Parallel()

	success, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})
	if err != nil {
		t.Fatalf("New() success config error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := success.Run(ctx); err != nil {
		t.Fatalf("Run() success path error = %v", err)
	}

	failure, err := New(Config{
		ListenAddr:  "bad::addr",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})
	if err != nil {
		t.Fatalf("New() failure config error = %v", err)
	}

	if err := failure.Run(context.Background()); err == nil {
		t.Fatal("expected invalid addr error")
	}
}

func TestNewSeedsConfiguredRooms(t *testing.T) {
	t.Parallel()

	instance, err := New(Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
		Rooms: []RoomConfig{
			{
				ID:      "research",
				Name:    "Research",
				Members: []string{"orchestrator", "writer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
	response := httptest.NewRecorder()
	instance.server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", response.Code)
	}

	var payload struct {
		Rooms []protocol.Room `json:"rooms"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Rooms) != 1 || payload.Rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", payload.Rooms)
	}
}

func TestNewSeedsConfiguredRoomsIdempotentlyWithPersistentStore(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "moltnet.db")
	config := Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
		Storage: StorageConfig{
			Kind:   storageKindSQLite,
			SQLite: SQLiteStorageConfig{Path: databasePath},
		},
		Rooms: []RoomConfig{{
			ID:      "research",
			Name:    "Research",
			Members: []string{"orchestrator", "writer"},
		}},
	}

	first, err := New(config)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	first.close()

	second, err := New(config)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	defer second.close()

	request := httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
	response := httptest.NewRecorder()
	second.server.Handler.ServeHTTP(response, request)

	var payload struct {
		Rooms []protocol.Room `json:"rooms"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Rooms) != 1 || payload.Rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", payload.Rooms)
	}
}

func TestNewRejectsInvalidSeedRoom(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
		Rooms: []RoomConfig{
			{},
		},
	})
	if err == nil {
		t.Fatal("expected invalid seed room error")
	}
}

func TestBuildStore(t *testing.T) {
	t.Parallel()

	memory, err := buildStore(Config{})
	if err != nil {
		t.Fatalf("buildStore() memory error = %v", err)
	}
	if _, ok := memory.(*store.MemoryStore); !ok {
		t.Fatalf("expected memory store, got %T", memory)
	}

	filePath := filepath.Join(t.TempDir(), "state.json")
	fileStore, err := buildStore(Config{Storage: StorageConfig{
		Kind: storageKindJSON,
		JSON: JSONStorageConfig{Path: filePath},
	}})
	if err != nil {
		t.Fatalf("buildStore() file error = %v", err)
	}
	if _, ok := fileStore.(*store.FileStore); !ok {
		t.Fatalf("expected file store, got %T", fileStore)
	}

	sqlitePath := filepath.Join(t.TempDir(), "moltnet.db")
	sqliteStore, err := buildStore(Config{Storage: StorageConfig{
		Kind:   storageKindSQLite,
		SQLite: SQLiteStorageConfig{Path: sqlitePath},
	}})
	if err != nil {
		t.Fatalf("buildStore() sqlite error = %v", err)
	}
	if closer, ok := sqliteStore.(*store.SQLStore); !ok {
		t.Fatalf("expected SQL store, got %T", sqliteStore)
	} else {
		_ = closer.Close()
	}

	if _, err := buildStore(Config{Storage: StorageConfig{Kind: "wat"}}); err == nil {
		t.Fatal("expected unsupported storage error")
	}
}

type fakeCloser struct {
	called bool
	err    error
}

func (f *fakeCloser) Close() error {
	f.called = true
	return f.err
}

func TestAppClose(t *testing.T) {
	t.Parallel()

	closer := &fakeCloser{}
	app := &App{closers: []io.Closer{closer}}
	app.close()
	if !closer.called {
		t.Fatal("expected closer to be called")
	}

	app.closers = []io.Closer{&fakeCloser{err: errors.New("close failed")}}
	app.close()
}
