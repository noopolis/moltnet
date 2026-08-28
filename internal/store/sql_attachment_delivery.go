package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func insertAttachmentDeliveryEvent(
	ctx context.Context,
	tx *sql.Tx,
	dialect sqlDialect,
	event protocol.Event,
) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode attachment delivery event: %w", err)
	}
	query := bindQuery(dialect, `
		INSERT INTO attachment_delivery_events (event_id, event_json, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT (event_id) DO NOTHING
	`)
	if _, err := tx.ExecContext(ctx, query, event.ID, string(encoded), formatTime(event.CreatedAt)); err != nil {
		return fmt.Errorf("insert attachment delivery event: %w", err)
	}
	return nil
}

func (s *SQLStore) PrepareAttachmentDeliveryContext(
	ctx context.Context,
	agentID string,
) ([]protocol.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin prepare attachment delivery: %w", err)
	}
	defer rollback(tx)

	id := strings.TrimSpace(agentID)
	now := formatTime(time.Now().UTC())
	insert := bindQuery(s.dialect, `
		INSERT INTO attachment_delivery_cursors (agent_id, event_seq, updated_at)
		SELECT ?, COALESCE(MAX(seq), 0), ? FROM attachment_delivery_events
		WHERE 1 = 1
		ON CONFLICT (agent_id) DO NOTHING
	`)
	result, err := tx.ExecContext(ctx, insert, id, now)
	if err != nil {
		return nil, fmt.Errorf("initialize attachment delivery cursor: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("inspect attachment delivery cursor: %w", err)
	}
	if inserted == 1 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit attachment delivery baseline: %w", err)
		}
		return nil, nil
	}

	var cursor int64
	queryCursor := bindQuery(s.dialect, `SELECT event_seq FROM attachment_delivery_cursors WHERE agent_id = ?`)
	if err := tx.QueryRowContext(ctx, queryCursor, id).Scan(&cursor); err != nil {
		return nil, fmt.Errorf("read attachment delivery cursor: %w", err)
	}
	rows, err := tx.QueryContext(ctx, bindQuery(s.dialect, `
		SELECT event_json FROM attachment_delivery_events WHERE seq > ? ORDER BY seq ASC
	`), cursor)
	if err != nil {
		return nil, fmt.Errorf("list attachment delivery events: %w", err)
	}
	defer rows.Close()

	events := make([]protocol.Event, 0)
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, fmt.Errorf("scan attachment delivery event: %w", err)
		}
		var event protocol.Event
		if err := json.Unmarshal([]byte(encoded), &event); err != nil {
			return nil, fmt.Errorf("decode attachment delivery event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment delivery events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit attachment delivery replay: %w", err)
	}
	return events, nil
}

func (s *SQLStore) AcknowledgeAttachmentDeliveryContext(
	ctx context.Context,
	agentID string,
	eventID string,
) error {
	query := bindQuery(s.dialect, `
		INSERT INTO attachment_delivery_cursors (agent_id, event_seq, updated_at)
		SELECT ?, seq, ? FROM attachment_delivery_events WHERE event_id = ?
		ON CONFLICT (agent_id) DO UPDATE SET
			event_seq = CASE
				WHEN attachment_delivery_cursors.event_seq < excluded.event_seq THEN excluded.event_seq
				ELSE attachment_delivery_cursors.event_seq
			END,
			updated_at = excluded.updated_at
	`)
	if _, err := s.db.ExecContext(
		ctx,
		query,
		strings.TrimSpace(agentID),
		formatTime(time.Now().UTC()),
		strings.TrimSpace(eventID),
	); err != nil {
		return fmt.Errorf("acknowledge attachment delivery: %w", err)
	}
	return nil
}
