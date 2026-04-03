package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type SQLStore struct {
	db      *sql.DB
	dialect sqlDialect
}

func (s *SQLStore) CreateRoom(room protocol.Room) error {
	return s.CreateRoomContext(context.Background(), room)
}

func (s *SQLStore) CreateRoomContext(ctx context.Context, room protocol.Room) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create room: %w", err)
	}
	defer rollback(tx)

	query := bindQuery(s.dialect, `INSERT INTO rooms (id, network_id, fqid, name, created_at) VALUES (?, ?, ?, ?, ?)`)
	if _, err := tx.ExecContext(ctx, query, room.ID, room.NetworkID, room.FQID, room.Name, formatTime(room.CreatedAt)); err != nil {
		if isDuplicateRoomError(err) {
			return fmt.Errorf("%w: %q", ErrRoomExists, room.ID)
		}
		return fmt.Errorf("insert room: %w", err)
	}
	for _, memberID := range protocol.SortedUniqueTrimmedStrings(room.Members) {
		memberQuery := bindQuery(s.dialect, `INSERT INTO room_members (room_id, member_id) VALUES (?, ?)`)
		if _, err := tx.ExecContext(ctx, memberQuery, room.ID, memberID); err != nil {
			return fmt.Errorf("insert room member: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLStore) GetRoom(id string) (protocol.Room, bool, error) {
	return s.GetRoomContext(context.Background(), id)
}

func (s *SQLStore) GetRoomContext(ctx context.Context, id string) (protocol.Room, bool, error) {
	query := bindQuery(s.dialect, `
		SELECT r.id, r.network_id, r.fqid, r.name, r.created_at, rm.member_id
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id
		WHERE r.id = ?
		ORDER BY rm.member_id ASC
	`)
	rows, err := s.db.QueryContext(ctx, query, id)
	if err != nil {
		return protocol.Room{}, false, fmt.Errorf("get room %q: %w", id, err)
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
			return protocol.Room{}, false, fmt.Errorf("scan room %q: %w", id, err)
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
		return protocol.Room{}, false, fmt.Errorf("iterate room %q: %w", id, err)
	}
	if !found {
		return protocol.Room{}, false, nil
	}
	return room, true, nil
}

func (s *SQLStore) GetThread(id string) (protocol.Thread, bool, error) {
	return s.GetThreadContext(context.Background(), id)
}

func (s *SQLStore) GetThreadContext(ctx context.Context, id string) (protocol.Thread, bool, error) {
	query := bindQuery(s.dialect, `SELECT id, network_id, fqid, room_id, parent_message_id, message_count, last_message_at FROM threads WHERE id = ?`)

	var thread protocol.Thread
	var lastMessageAt any
	if err := s.db.QueryRowContext(ctx, query, id).Scan(
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

func (s *SQLStore) ListRooms() ([]protocol.Room, error) {
	return s.ListRoomsContext(context.Background())
}

func (s *SQLStore) ListRoomsContext(ctx context.Context) ([]protocol.Room, error) {
	query := bindQuery(s.dialect, `
		SELECT r.id, r.network_id, r.fqid, r.name, r.created_at, rm.member_id
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id
		ORDER BY r.id ASC, rm.member_id ASC
	`)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	rooms := make([]protocol.Room, 0)
	roomsByID := make(map[string]*protocol.Room)
	for rows.Next() {
		var (
			roomID, networkID, fqid, name string
			createdAt                     any
			memberID                      sql.NullString
		)
		if err := rows.Scan(&roomID, &networkID, &fqid, &name, &createdAt, &memberID); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		room, ok := roomsByID[roomID]
		if !ok {
			room = &protocol.Room{
				ID:        roomID,
				NetworkID: networkID,
				FQID:      fqid,
				Name:      name,
				CreatedAt: parseTime(createdAt),
			}
			roomsByID[roomID] = room
			rooms = append(rooms, *room)
			room = &rooms[len(rooms)-1]
			roomsByID[roomID] = room
		}
		if memberID.Valid {
			room.Members = append(room.Members, memberID.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rooms: %w", err)
	}

	return rooms, nil
}

func (s *SQLStore) UpdateRoomMembers(roomID string, add []string, remove []string) (protocol.Room, error) {
	return s.UpdateRoomMembersContext(context.Background(), roomID, add, remove)
}

func (s *SQLStore) UpdateRoomMembersContext(
	ctx context.Context,
	roomID string,
	add []string,
	remove []string,
) (protocol.Room, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Room{}, fmt.Errorf("begin update room members: %w", err)
	}
	defer rollback(tx)

	checkQuery := bindQuery(s.dialect, `SELECT COUNT(1) FROM rooms WHERE id = ?`)
	var count int
	if err := tx.QueryRowContext(ctx, checkQuery, roomID).Scan(&count); err != nil {
		return protocol.Room{}, fmt.Errorf("check room %q: %w", roomID, err)
	}
	if count == 0 {
		return protocol.Room{}, fmt.Errorf("%w: %q", ErrRoomNotFound, roomID)
	}

	insertQuery := bindQuery(s.dialect, `INSERT INTO room_members (room_id, member_id) VALUES (?, ?) ON CONFLICT (room_id, member_id) DO NOTHING`)
	for _, memberID := range protocol.SortedUniqueTrimmedStrings(add) {
		if _, err := tx.ExecContext(ctx, insertQuery, roomID, memberID); err != nil {
			return protocol.Room{}, fmt.Errorf("insert room member: %w", err)
		}
	}

	deleteQuery := bindQuery(s.dialect, `DELETE FROM room_members WHERE room_id = ? AND member_id = ?`)
	for _, memberID := range protocol.SortedUniqueTrimmedStrings(remove) {
		if _, err := tx.ExecContext(ctx, deleteQuery, roomID, memberID); err != nil {
			return protocol.Room{}, fmt.Errorf("delete room member: %w", err)
		}
	}

	room, err := s.getRoomUsingQuerier(ctx, tx, roomID)
	if err != nil {
		return protocol.Room{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.Room{}, fmt.Errorf("commit room members: %w", err)
	}

	return room, nil
}
