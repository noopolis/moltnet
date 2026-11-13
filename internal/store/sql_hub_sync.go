package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// hubSyncMigration stores each member's verified sync cursor per hub room.
func hubSyncMigration() migration {
	return migration{Version: 17, Name: "hub_sync_cursors", Statements: []string{
		`CREATE TABLE IF NOT EXISTS hub_sync_cursors (global_room_id TEXT PRIMARY KEY, seq BIGINT NOT NULL, digest TEXT NOT NULL,
			membership_epoch BIGINT NOT NULL DEFAULT 0)`,
	}}
}

func (s *SQLStore) HubReceiptsAfterContext(ctx context.Context, globalRoomID string, hubEpoch, afterSeq int64, limit int) ([]HubReceiptRecord, error) {
	if limit <= 0 {
		limit = protocol.MaxRoomSyncPage
	}
	rows, err := s.db.QueryContext(ctx, bindQuery(s.dialect, `SELECT `+hubReceiptColumns+` FROM hub_receipts
		WHERE global_room_id = ? AND hub_epoch = ? AND room_seq > ? ORDER BY room_seq ASC LIMIT ?`), globalRoomID, hubEpoch, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []HubReceiptRecord
	for rows.Next() {
		record, err := scanHubReceipt(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *SQLStore) HubMessagesContext(ctx context.Context, messageIDs []string) (map[string]protocol.Message, error) {
	result := map[string]protocol.Message{}
	for _, id := range messageIDs {
		rows, err := s.db.QueryContext(ctx, bindQuery(s.dialect, `SELECT m.id, m.network_id, m.room_id, m.thread_id, m.parent_message_id, m.dm_id,
			m.origin_json, m.target_json, m.from_json, m.parts_json, m.mentions_json, m.created_at, m.automation_json FROM messages m WHERE m.id = ?`), id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			message, err := scanMessage(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			result[id] = message
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *SQLStore) HubSyncCursorContext(ctx context.Context, globalRoomID string) (HubSyncCursor, error) {
	cursor := HubSyncCursor{GlobalRoomID: globalRoomID}
	err := s.db.QueryRowContext(ctx, bindQuery(s.dialect, `SELECT seq, digest, membership_epoch FROM hub_sync_cursors WHERE global_room_id = ?`), globalRoomID).Scan(&cursor.Seq, &cursor.Digest, &cursor.MembershipEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return cursor, nil
	}
	return cursor, err
}

func (s *SQLStore) AdvanceHubSyncCursorContext(ctx context.Context, cursor HubSyncCursor) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	// Take the row (or SQLite's write) lock before reading, so two advances
	// cannot both pass the forward-only check.
	if _, err := tx.ExecContext(ctx, bindQuery(s.dialect, `INSERT INTO hub_sync_cursors (global_room_id, seq, digest) VALUES (?, 0, '')
		ON CONFLICT (global_room_id) DO UPDATE SET seq = hub_sync_cursors.seq`), cursor.GlobalRoomID); err != nil {
		return err
	}
	stored := HubSyncCursor{GlobalRoomID: cursor.GlobalRoomID}
	if err := tx.QueryRowContext(ctx, bindQuery(s.dialect, `SELECT seq, digest, membership_epoch FROM hub_sync_cursors WHERE global_room_id = ?`), cursor.GlobalRoomID).Scan(&stored.Seq, &stored.Digest, &stored.MembershipEpoch); err != nil {
		return err
	}
	advance, err := checkCursorAdvance(stored, cursor)
	if err != nil || !advance {
		return err
	}
	if _, err := tx.ExecContext(ctx, bindQuery(s.dialect, `UPDATE hub_sync_cursors SET seq = ?, digest = ?, membership_epoch = ? WHERE global_room_id = ?`), cursor.Seq, cursor.Digest, cursor.MembershipEpoch, cursor.GlobalRoomID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) AttachHubReceiptContext(ctx context.Context, record HubReceiptRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	duplicate, err := s.guardHubReceipt(ctx, tx, &record)
	if err != nil {
		return err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, bindQuery(s.dialect, `SELECT COUNT(*) FROM messages WHERE id = ?`), record.MessageID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: no stored message %s to attach a receipt to", ErrHubReceiptConflict, record.MessageID)
	}
	if duplicate {
		if err := s.bindHubReceipt(ctx, tx, &record); err != nil {
			return err
		}
	} else if err := s.insertHubReceipt(ctx, tx, &record); err != nil {
		return err
	}
	return tx.Commit()
}
