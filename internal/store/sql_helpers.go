package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/noopolis/moltnet/pkg/protocol"
	"modernc.org/sqlite"
)

func isDuplicateRoomError(err error) bool {
	return isUniqueConstraintError(err)
}

func marshalJSON(value any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(bytes)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

type roomQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *SQLStore) getRoomUsingQuerier(ctx context.Context, querier roomQuerier, id string) (protocol.Room, error) {
	query := bindQuery(s.dialect, `
		SELECT r.id, r.network_id, r.fqid, r.name, r.created_at, rm.member_id
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id
		WHERE r.id = ?
		ORDER BY rm.member_id ASC
	`)
	rows, err := querier.QueryContext(ctx, query, id)
	if err != nil {
		return protocol.Room{}, fmt.Errorf("get room %q: %w", id, err)
	}
	defer rows.Close()

	var (
		room  protocol.Room
		found bool
	)
	for rows.Next() {
		var (
			roomID, networkID, fqid, name string
			createdAt                     any
			memberID                      sql.NullString
		)
		if err := rows.Scan(&roomID, &networkID, &fqid, &name, &createdAt, &memberID); err != nil {
			return protocol.Room{}, fmt.Errorf("scan room %q: %w", id, err)
		}
		if !found {
			found = true
			room = protocol.Room{
				ID:        roomID,
				NetworkID: networkID,
				FQID:      fqid,
				Name:      name,
				CreatedAt: parseTime(createdAt),
			}
		}
		if memberID.Valid {
			room.Members = append(room.Members, memberID.String)
		}
	}
	if err := rows.Err(); err != nil {
		return protocol.Room{}, fmt.Errorf("iterate room %q: %w", id, err)
	}
	if !found {
		return protocol.Room{}, fmt.Errorf("%w: %q", ErrRoomNotFound, id)
	}

	return room, nil
}

func getThreadUsingQuerier(ctx context.Context, querier rowQuerier, dialect sqlDialect, id string) (protocol.Thread, bool, error) {
	query := bindQuery(dialect, `SELECT id, network_id, fqid, room_id, parent_message_id, message_count, last_message_at FROM threads WHERE id = ?`)

	var thread protocol.Thread
	var lastMessageAt any
	if err := querier.QueryRowContext(ctx, query, id).Scan(
		&thread.ID,
		&thread.NetworkID,
		&thread.FQID,
		&thread.RoomID,
		&thread.ParentMessageID,
		&thread.MessageCount,
		&lastMessageAt,
	); err != nil {
		if isNoRows(err) {
			return protocol.Thread{}, false, nil
		}
		return protocol.Thread{}, false, fmt.Errorf("get thread %q: %w", id, err)
	}
	thread.LastMessageAt = parseTime(lastMessageAt)
	return thread, true, nil
}

func getDirectConversationUsingQuerier(
	ctx context.Context,
	querier roomQuerier,
	dialect sqlDialect,
	dmID string,
) (protocol.DirectConversation, bool, error) {
	query := bindQuery(dialect, `
		SELECT dc.dm_id, dc.network_id, dc.fqid, dc.message_count, dc.last_message_at, dp.participant_id
		FROM dm_conversations dc
		LEFT JOIN dm_participants dp ON dp.dm_id = dc.dm_id
		WHERE dc.dm_id = ?
		ORDER BY dp.participant_id ASC
	`)
	rows, err := querier.QueryContext(ctx, query, dmID)
	if err != nil {
		return protocol.DirectConversation{}, false, fmt.Errorf("get dm conversation: %w", err)
	}
	defer rows.Close()

	var (
		conversation protocol.DirectConversation
		found        bool
	)
	for rows.Next() {
		var (
			rowDMID, networkID, fqid string
			lastMessageAt            any
			messageCount             int
			participantID            sql.NullString
		)
		if err := rows.Scan(&rowDMID, &networkID, &fqid, &messageCount, &lastMessageAt, &participantID); err != nil {
			return protocol.DirectConversation{}, false, fmt.Errorf("scan dm conversation: %w", err)
		}
		if !found {
			found = true
			conversation = protocol.DirectConversation{
				ID:            rowDMID,
				NetworkID:     networkID,
				FQID:          fqid,
				MessageCount:  messageCount,
				LastMessageAt: parseTime(lastMessageAt),
			}
		}
		if participantID.Valid {
			conversation.ParticipantIDs = append(conversation.ParticipantIDs, participantID.String)
		}
	}
	if err := rows.Err(); err != nil {
		return protocol.DirectConversation{}, false, fmt.Errorf("iterate dm conversation: %w", err)
	}
	return conversation, found, nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return time.Time{}.UTC().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value any) time.Time {
	switch typed := value.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return typed.UTC()
	case []byte:
		return parseTime(string(typed))
	case string:
		if strings.TrimSpace(typed) == "" {
			return time.Time{}
		}
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return time.Time{}
		}
		return parsed.UTC()
	default:
		return time.Time{}
	}
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == 2067 || sqliteErr.Code() == 1555
	}

	return false
}
