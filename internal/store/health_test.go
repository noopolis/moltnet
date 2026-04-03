package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryAndFileStoreHealth(t *testing.T) {
	t.Parallel()

	memory := NewMemoryStore()
	if err := memory.Health(context.Background()); err != nil {
		t.Fatalf("MemoryStore.Health() error = %v", err)
	}

	file, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := file.Health(context.Background()); err != nil {
		t.Fatalf("FileStore.Health() error = %v", err)
	}
}

func TestSQLStoreHealth(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	if err := store.Health(context.Background()); err != nil {
		t.Fatalf("SQLStore.Health() error = %v", err)
	}
}
