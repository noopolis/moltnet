package store

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSQLiteStoreRoomFederationRoundTrips(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	room := protocol.Room{
		ID:         "research",
		NetworkID:  "local",
		FQID:       protocol.RoomFQID("local", "research"),
		Name:       "Research",
		Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll},
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.CreateRoomContext(t.Context(), room); err != nil {
		t.Fatalf("CreateRoomContext() error = %v", err)
	}

	got, ok, err := store.GetRoomContext(t.Context(), room.ID)
	if err != nil || !ok {
		t.Fatalf("GetRoomContext() = %#v, %v, %v", got, ok, err)
	}
	assertRoomFederation(t, got.Federation, protocol.RoomFederation{Mode: protocol.RoomFederationAll})

	rooms, err := store.ListRoomsContext(t.Context())
	if err != nil || len(rooms) != 1 {
		t.Fatalf("ListRoomsContext() = %#v, %v", rooms, err)
	}
	assertRoomFederation(t, rooms[0].Federation, protocol.RoomFederation{Mode: protocol.RoomFederationAll})

	room.Name = "Federated Research"
	room.Federation = &protocol.RoomFederation{
		Mode:     protocol.RoomFederationList,
		Pairings: []string{"pair-a", "pair-b"},
	}
	reconciled, err := store.ReconcileRoomContext(t.Context(), room)
	if err != nil {
		t.Fatalf("ReconcileRoomContext() error = %v", err)
	}
	want := protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair-a", "pair-b"}}
	assertRoomFederation(t, reconciled.Federation, want)

	got, ok, err = store.GetRoomContext(t.Context(), room.ID)
	if err != nil || !ok {
		t.Fatalf("GetRoomContext() after reconcile = %#v, %v, %v", got, ok, err)
	}
	assertRoomFederation(t, got.Federation, want)
}

func assertRoomFederation(t *testing.T, got *protocol.RoomFederation, want protocol.RoomFederation) {
	t.Helper()
	if got == nil {
		t.Fatal("room Federation = nil")
	}
	normalizedGot := protocol.NormalizeRoomFederation(got)
	normalizedWant := protocol.NormalizeRoomFederation(&want)
	if normalizedGot.Mode != normalizedWant.Mode || !slices.Equal(normalizedGot.Pairings, normalizedWant.Pairings) {
		t.Fatalf("room Federation = %#v, want %#v", normalizedGot, normalizedWant)
	}
}
