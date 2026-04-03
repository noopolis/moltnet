package store

import (
	"path/filepath"
	"testing"
)

func TestScanMessageAndArtifactRows(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	messageRows, err := store.db.Query(`
		SELECT
			'msg_1',
			'local',
			NULL,
			NULL,
			NULL,
			NULL,
			'{}',
			'{"kind":"room","room_id":"research"}',
			'{"type":"agent","id":"alpha"}',
			'[{"kind":"text","text":"hello"}]',
			'["beta"]',
			'2026-04-01T09:00:00Z'
	`)
	if err != nil {
		t.Fatalf("message query: %v", err)
	}
	defer messageRows.Close()
	if !messageRows.Next() {
		t.Fatal("expected message row")
	}
	message, err := scanMessage(messageRows)
	if err != nil || message.ID != "msg_1" || message.Target.RoomID != "research" {
		t.Fatalf("scanMessage() = %#v, %v", message, err)
	}
	if err := messageRows.Close(); err != nil {
		t.Fatalf("close message rows: %v", err)
	}

	artifactRows, err := store.db.Query(`
		SELECT
			'art_1',
			'local',
			'molt://local/artifacts/art_1',
			'msg_1',
			'{"kind":"thread","room_id":"research","thread_id":"thread_1"}',
			1,
			'url',
			'text/plain',
			'notes.txt',
			'https://example.com/notes.txt',
			'2026-04-01T09:01:00Z'
	`)
	if err != nil {
		t.Fatalf("artifact query: %v", err)
	}
	defer artifactRows.Close()
	if !artifactRows.Next() {
		t.Fatal("expected artifact row")
	}
	artifact, err := scanArtifact(artifactRows)
	if err != nil || artifact.ID != "art_1" || artifact.Target.ThreadID != "thread_1" {
		t.Fatalf("scanArtifact() = %#v, %v", artifact, err)
	}
}

func TestScanMessageAndArtifactRejectInvalidJSON(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	messageRows, err := store.db.Query(`
		SELECT
			'msg_bad',
			'local',
			NULL,
			NULL,
			NULL,
			NULL,
			'{}',
			'{',
			'{"type":"agent","id":"alpha"}',
			'[{"kind":"text","text":"hello"}]',
			'[]',
			'2026-04-01T09:00:00Z'
	`)
	if err != nil {
		t.Fatalf("message query: %v", err)
	}
	defer messageRows.Close()
	if !messageRows.Next() {
		t.Fatal("expected message row")
	}
	if _, err := scanMessage(messageRows); err == nil {
		t.Fatal("expected invalid message row to fail")
	}
	if err := messageRows.Close(); err != nil {
		t.Fatalf("close message rows: %v", err)
	}

	artifactRows, err := store.db.Query(`
		SELECT
			'art_bad',
			'local',
			'molt://local/artifacts/art_bad',
			'msg_1',
			'{',
			1,
			'url',
			NULL,
			NULL,
			NULL,
			'2026-04-01T09:01:00Z'
	`)
	if err != nil {
		t.Fatalf("artifact query: %v", err)
	}
	defer artifactRows.Close()
	if !artifactRows.Next() {
		t.Fatal("expected artifact row")
	}
	if _, err := scanArtifact(artifactRows); err == nil {
		t.Fatal("expected invalid artifact row to fail")
	}
}
