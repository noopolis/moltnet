package clisession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionStorePutGetAndCreate(t *testing.T) {
	t.Parallel()

	store := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))

	if _, ok, err := store.Get("room"); err != nil || ok {
		t.Fatalf("expected missing session, ok=%v err=%v", ok, err)
	}

	record, err := store.Put("room", "session-1")
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if record.RuntimeSessionID != "session-1" || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		t.Fatalf("unexpected record %#v", record)
	}

	record, ok, err := store.Get("room")
	if err != nil || !ok {
		t.Fatalf("expected stored session, ok=%v err=%v", ok, err)
	}
	if record.RuntimeSessionID != "session-1" {
		t.Fatalf("unexpected record %#v", record)
	}

	created, existed, err := store.GetOrCreate("other")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if existed || created.RuntimeSessionID == "" {
		t.Fatalf("unexpected created record %#v existed=%v", created, existed)
	}
	reused, existed, err := store.GetOrCreate("other")
	if err != nil {
		t.Fatalf("GetOrCreate() reuse error = %v", err)
	}
	if !existed || reused.RuntimeSessionID != created.RuntimeSessionID {
		t.Fatalf("expected reused record %#v existed=%v", reused, existed)
	}
}

func TestSessionStoreRejectsCorruptJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatalf("write corrupt store: %v", err)
	}

	_, _, err := NewSessionStore(path).Get("room")
	if err == nil || !strings.Contains(err.Error(), "decode CLI session store") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestGenerateUUIDShape(t *testing.T) {
	t.Parallel()

	uuid, err := GenerateUUID()
	if err != nil {
		t.Fatalf("GenerateUUID() error = %v", err)
	}
	if len(uuid) != 36 || uuid[14] != '4' {
		t.Fatalf("unexpected uuid %q", uuid)
	}
}
