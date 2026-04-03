package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPageArtifactsDescendingAndReverse(t *testing.T) {
	t.Parallel()

	artifacts := []protocol.Artifact{
		{ID: "art_3"},
		{ID: "art_2"},
		{ID: "art_1"},
	}
	page := pageArtifactsDescending(append([]protocol.Artifact(nil), artifacts...), 2)
	if len(page.Artifacts) != 2 || page.Artifacts[0].ID != "art_2" || page.Artifacts[1].ID != "art_3" || !page.Page.HasMore || page.Page.NextBefore != "art_2" {
		t.Fatalf("unexpected artifact page %#v", page)
	}

	reverseArtifacts(artifacts)
	if artifacts[0].ID != "art_1" || artifacts[2].ID != "art_3" {
		t.Fatalf("unexpected reversed artifacts %#v", artifacts)
	}
}

func TestFormatAndParseTime(t *testing.T) {
	t.Parallel()

	if formatTime(time.Time{}) == "" {
		t.Fatal("expected formatted zero time")
	}
	if !parseTime("bad").IsZero() {
		t.Fatal("expected invalid time to parse as zero")
	}

	now := time.Now().UTC().Round(time.Nanosecond)
	if parsed := parseTime(formatTime(now)); !parsed.Equal(now) {
		t.Fatalf("expected parsed time %v, got %v", now, parsed)
	}
}

func TestMarshalJSONAndUniqueStrings(t *testing.T) {
	t.Parallel()

	if got := marshalJSON(map[string]string{"ok": "yes"}); got == "" || got == "null" {
		t.Fatalf("unexpected json %q", got)
	}
	if got := marshalJSON(make(chan int)); got != "null" {
		t.Fatalf("expected unsupported value to marshal as null, got %q", got)
	}

	values := protocol.SortedUniqueTrimmedStrings([]string{"writer", "writer", " ", "orchestrator"})
	if len(values) != 2 || values[0] != "orchestrator" || values[1] != "writer" {
		t.Fatalf("unexpected unique values %#v", values)
	}
}

func TestParseTimeVariantsAndPageSlices(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Round(time.Nanosecond)
	if parsed := parseTime(now); !parsed.Equal(now) {
		t.Fatalf("expected direct time parse %v, got %v", now, parsed)
	}
	if parsed := parseTime([]byte(formatTime(now))); !parsed.Equal(now) {
		t.Fatalf("expected byte time parse %v, got %v", now, parsed)
	}
	if parsed := parseTime(nil); !parsed.IsZero() {
		t.Fatalf("expected nil time to be zero, got %v", parsed)
	}

	messages := []protocol.Message{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := messagesPageSlice(messages, 2); len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("unexpected message slice %#v", got)
	}
	if info := pageInfoForSlice(messages, 2); !info.HasMore || info.NextAfter != "b" {
		t.Fatalf("unexpected message info %#v", info)
	}

	artifacts := []protocol.Artifact{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := artifactsPageSlice(artifacts, 2); len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("unexpected artifact slice %#v", got)
	}
	if info := pageInfoForArtifacts(artifacts, 2); !info.HasMore || info.NextAfter != "b" {
		t.Fatalf("unexpected artifact info %#v", info)
	}
}

func queryDMParticipants(t *testing.T, store *SQLStore, dmID string) []string {
	t.Helper()

	query := bindQuery(store.dialect, `SELECT participant_id FROM dm_participants WHERE dm_id = ? ORDER BY participant_id ASC`)
	rows, err := store.db.QueryContext(context.Background(), query, dmID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	participants := make([]string, 0)
	for rows.Next() {
		var participantID string
		if err := rows.Scan(&participantID); err != nil {
			t.Fatalf("scan dm participant: %v", err)
		}
		participants = append(participants, participantID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate dm participants: %v", err)
	}

	return participants
}

func queryRoomMembers(t *testing.T, store *SQLStore, roomID string) []string {
	t.Helper()

	query := bindQuery(store.dialect, `SELECT member_id FROM room_members WHERE room_id = ? ORDER BY member_id ASC`)
	rows, err := store.db.QueryContext(context.Background(), query, roomID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	members := make([]string, 0)
	for rows.Next() {
		var memberID string
		if err := rows.Scan(&memberID); err != nil {
			t.Fatalf("scan room member: %v", err)
		}
		members = append(members, memberID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate room members: %v", err)
	}

	return members
}

func queryMessageCursorExists(t *testing.T, store *SQLStore, id string) bool {
	t.Helper()

	ok, err := messageCursorExistsContext(context.Background(), store.db, store.dialect, id)
	if err != nil {
		t.Fatalf("messageCursorExistsContext(%q) error = %v", id, err)
	}
	return ok
}

func queryArtifactCursorExists(t *testing.T, store *SQLStore, id string) bool {
	t.Helper()

	ok, err := artifactCursorExistsContext(context.Background(), store.db, store.dialect, id)
	if err != nil {
		t.Fatalf("artifactCursorExistsContext(%q) error = %v", id, err)
	}
	return ok
}

func queryCount(t *testing.T, db *sql.DB, dialect sqlDialect, query string) int {
	t.Helper()

	row := db.QueryRow(bindQuery(dialect, query))
	var value int
	if err := row.Scan(&value); err != nil {
		return 0
	}
	return value
}
