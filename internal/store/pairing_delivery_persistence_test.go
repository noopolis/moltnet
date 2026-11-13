package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPairingDeliveryReopens(t *testing.T) {
	for _, backend := range []string{"json", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state")
			open := func() pairingTestStore {
				if backend == "json" {
					storage, err := NewFileStore(path)
					if err != nil {
						t.Fatal(err)
					}
					return storage
				}
				storage, err := NewSQLiteStore(path)
				if err != nil {
					t.Fatal(err)
				}
				return storage
			}
			storage := open()
			preparePairingTestRoom(t, storage)
			ctx := context.Background()
			message := pairingTestMessage("persisted")
			message.Origin = protocol.MessageOrigin{NetworkID: "remote", MessageID: "origin", ReceivedVia: "pair-a"}
			if _, err := storage.AppendMessageWithPairingDeliveriesContext(ctx, message, nil, []string{"pair-a"}); err != nil {
				t.Fatal(err)
			}
			heads, err := storage.ListPairingDeliveryHeadsContext(ctx)
			if err != nil || len(heads) != 1 {
				t.Fatalf("heads = %#v, %v", heads, err)
			}
			head := heads[0]
			head.Attempts, head.NextAttempt = 9, time.Now().UTC().Add(time.Hour)
			if err := storage.UpdatePairingDeliveryContext(ctx, head, "timeout"); err != nil {
				t.Fatal(err)
			}
			if sql, ok := storage.(*SQLStore); ok {
				if err := sql.Close(); err != nil {
					t.Fatal(err)
				}
			}
			storage = open()
			if sql, ok := storage.(*SQLStore); ok {
				defer sql.Close()
			}
			heads, err = storage.ListPairingDeliveryHeadsContext(ctx)
			if err != nil || len(heads) != 1 || heads[0].MessageID != head.MessageID ||
				heads[0].Attempts != head.Attempts || !heads[0].NextAttempt.Equal(head.NextAttempt) ||
				!heads[0].CreatedAt.Equal(head.CreatedAt) || heads[0].Sequence != head.Sequence {
				t.Fatalf("restored delivery = %#v, %v", heads, err)
			}
			state, err := storage.PairingDeliveryStateContext(ctx, "pair-a")
			if err != nil || state.Pending != 1 || state.LastError != "timeout" {
				t.Fatalf("restored state = %#v, %v", state, err)
			}
			message.ID = "changed-retry"
			if err := storage.AppendMessage(message); !errors.Is(err, ErrDuplicateMessage) {
				t.Fatalf("restored dedupe receipt = %v", err)
			}
			if _, err := storage.PrunePairingDeliveriesContext(ctx, "pair-a", time.Now().Add(8*24*time.Hour), 7*24*time.Hour, 100); err != nil {
				t.Fatal(err)
			}
			if sql, ok := storage.(*SQLStore); ok {
				if err := sql.Close(); err != nil {
					t.Fatal(err)
				}
			}
			storage = open()
			if sql, ok := storage.(*SQLStore); ok {
				defer sql.Close()
			}
			state, err = storage.PairingDeliveryStateContext(ctx, "pair-a")
			if err != nil || state.Pending != 0 || state.Dropped != 1 {
				t.Fatalf("restored drop counter = %#v, %v", state, err)
			}
		})
	}
}

func TestPairingDeliverySQLiteAppendRollback(t *testing.T) {
	storage := pairingStoreFactories()["sqlite"](t).(*SQLStore)
	preparePairingTestRoom(t, storage)
	if _, err := storage.db.Exec(`CREATE TRIGGER fail_pairing_delivery BEFORE INSERT ON pairing_deliveries BEGIN SELECT RAISE(ABORT, 'queue failure'); END`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	message := pairingTestMessage("atomic")
	message.Origin = protocol.MessageOrigin{NetworkID: "remote", MessageID: "origin", ReceivedVia: "pair-a"}
	event := protocol.Event{ID: "event-atomic", Message: &message, CreatedAt: message.CreatedAt}
	if _, err := storage.AppendMessageWithPairingDeliveriesContext(ctx, message, &event, []string{"pair-a"}); err == nil {
		t.Fatal("accepted message despite queue insertion failure")
	}
	for _, table := range []string{"messages", "pairing_deliveries", "pairing_receive_receipts", "attachment_delivery_events", "pairing_delivery_states"} {
		var count int
		if err := storage.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback %s count = %d, %v", table, count, err)
		}
	}
	if _, err := storage.db.Exec(`DROP TRIGGER fail_pairing_delivery`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AppendMessageWithPairingDeliveriesContext(ctx, message, &event, []string{"pair-a"}); err != nil {
		t.Fatalf("retry after failed transaction = %v", err)
	}
}

func TestPairingDeliveryJSONAppendRollback(t *testing.T) {
	storage := pairingStoreFactories()["json"](t).(*FileStore)
	preparePairingTestRoom(t, storage)
	path := storage.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage.path = filepath.Join(blocker, "state")
	ctx := context.Background()
	message := pairingTestMessage("atomic")
	if _, err := storage.AppendMessageWithPairingDeliveriesContext(ctx, message, nil, []string{"pair-a"}); err == nil {
		t.Fatal("accepted message despite JSON persistence failure")
	}
	page, err := storage.ListRoomMessages("delivery-room", "", 10)
	if err != nil || len(page.Messages) != 0 {
		t.Fatalf("uncommitted messages = %#v, %v", page, err)
	}
	heads, err := storage.ListPairingDeliveryHeadsContext(ctx)
	if err != nil || len(heads) != 0 {
		t.Fatalf("uncommitted queue = %#v, %v", heads, err)
	}
	storage.path = path
	if _, err := storage.AppendMessageWithPairingDeliveriesContext(ctx, message, nil, []string{"pair-a"}); err != nil {
		t.Fatalf("retry after failed persist = %v", err)
	}
	// A no-op maintenance sweep must not rewrite the entire history snapshot.
	storage.path = filepath.Join(blocker, "state")
	if count, err := storage.PrunePairingDeliveriesContext(ctx, "pair-a", time.Now(), 7*24*time.Hour, 100); err != nil || count != 0 {
		t.Fatalf("no-op prune unexpectedly persisted: %d, %v", count, err)
	}
}

func TestPairingReceiveSQLiteMigrationBackfillsReceipts(t *testing.T) {
	storage := pairingStoreFactories()["sqlite"](t).(*SQLStore)
	preparePairingTestRoom(t, storage)
	message := pairingTestMessage("legacy")
	message.Origin = protocol.MessageOrigin{NetworkID: "remote", MessageID: "origin", ReceivedVia: "pair-a"}
	if err := storage.AppendMessage(message); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`DROP TABLE pairing_receive_receipts`, `DROP TABLE pairing_deliveries`,
		`DROP TABLE pairing_delivery_states`, `DELETE FROM schema_migrations WHERE version BETWEEN 12 AND 18`,
	} {
		if _, err := storage.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.migrate(); err != nil {
		t.Fatal(err)
	}
	message.ID = "post-upgrade-retry"
	if err := storage.AppendMessage(message); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("migration did not preserve origin receipt = %v", err)
	}
	heads, err := storage.ListPairingDeliveryHeadsContext(context.Background())
	if err != nil || len(heads) != 0 {
		t.Fatalf("migration backfilled old outbound history: %#v, %v", heads, err)
	}
}

func TestPairingDeliveryPostgres(t *testing.T) {
	dsn := os.Getenv("MOLTNET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOLTNET_TEST_POSTGRES_DSN is not set")
	}
	storage, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	for _, table := range []string{"pairing_deliveries", "pairing_delivery_states", "pairing_receive_receipts", "artifacts", "attachment_delivery_events", "dm_participants", "messages", "dm_conversations", "threads", "room_members", "rooms"} {
		if _, err := storage.db.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	testPairingDeliveryBackend(t, storage)
	message := pairingTestMessage("pg-received")
	message.Origin = protocol.MessageOrigin{NetworkID: "pg-origin", MessageID: "pg-origin-message", ReceivedVia: "pair-a"}
	if err := storage.AppendMessage(message); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`DROP TABLE pairing_receive_receipts`, `DROP TABLE pairing_deliveries`, `DROP TABLE pairing_delivery_states`, `DELETE FROM schema_migrations WHERE version >= 12`} {
		if _, err := storage.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.migrate(); err != nil {
		t.Fatal(err)
	}
	message.ID = "pg-retry-new-id"
	var duplicate *DuplicateMessageError
	if err := storage.AppendMessage(message); !errors.As(err, &duplicate) || duplicate.MessageID != "pg-received" {
		t.Fatalf("postgres migration origin retry = %#v, %v", duplicate, err)
	}
	heads, err := storage.ListPairingDeliveryHeadsContext(context.Background())
	if err != nil || len(heads) != 0 {
		t.Fatalf("postgres migration created old outbound history = %#v, %v", heads, err)
	}
}
