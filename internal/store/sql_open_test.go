package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSQLiteStoreCreatesPrivateDirectoryAndConfiguresPragmas(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "state", "moltnet.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat sqlite dir: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o700 {
		t.Fatalf("expected sqlite dir perms 0700, got %#o", perms)
	}

	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("read sqlite journal_mode: %v", err)
	}
	if journalMode == "" {
		t.Fatal("expected sqlite journal mode to be configured")
	}

	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys;`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read sqlite foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys pragma to be enabled, got %d", foreignKeys)
	}

	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout;`).Scan(&busyTimeout); err != nil {
		t.Fatalf("read sqlite busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

func TestNewSQLiteStoreSupportsCurrentDirectoryPath(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	store, err := NewSQLiteStore("moltnet.db")
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(filepath.Join(directory, "moltnet.db")); err != nil {
		t.Fatalf("stat sqlite db: %v", err)
	}
}

func TestNewSQLiteStoreReturnsDirectoryCreationErrors(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(base, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	if _, err := NewSQLiteStore(filepath.Join(base, "moltnet.db")); err == nil {
		t.Fatal("expected directory creation error")
	}
}

func TestNewPostgresStoreReturnsPingErrorsForUnreachableServer(t *testing.T) {
	t.Parallel()

	_, err := NewPostgresStore("postgres://127.0.0.1:1/moltnet?sslmode=disable")
	if err == nil {
		t.Fatal("expected postgres open failure")
	}
}
