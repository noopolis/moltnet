package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *SQLStore) AppendMessage(message protocol.Message) error {
	return s.AppendMessageContext(context.Background(), message)
}

func (s *SQLStore) AppendMessageContext(ctx context.Context, message protocol.Message) error {
	_, err := s.AppendMessageWithLifecycleContext(ctx, message)
	return err
}

func (s *SQLStore) AppendMessageWithLifecycleContext(ctx context.Context, message protocol.Message) (AppendLifecycle, error) {
	return s.appendMessageWithLifecycleContext(ctx, message, nil)
}

func (s *SQLStore) AppendMessageEventWithLifecycleContext(
	ctx context.Context,
	message protocol.Message,
	event protocol.Event,
) (AppendLifecycle, error) {
	return s.appendMessageWithLifecycleContext(ctx, message, &event)
}

func (s *SQLStore) appendMessageWithLifecycleContext(
	ctx context.Context,
	message protocol.Message,
	event *protocol.Event,
) (AppendLifecycle, error) {
	topology := dmTopology{}
	if message.Target.Kind == protocol.TargetKindDM {
		var err error
		topology, err = topologyForDMMessage(message)
		if err != nil {
			return AppendLifecycle{}, err
		}
		message.Target.ParticipantIDs = topology.participantIDs
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AppendLifecycle{}, fmt.Errorf("begin append message: %w", err)
	}
	defer rollback(tx)

	duplicate, err := messageCursorExistsContext(ctx, tx, s.dialect, message.ID)
	if err != nil {
		return AppendLifecycle{}, fmt.Errorf("check duplicate message: %w", err)
	}
	if duplicate {
		if message.Target.Kind == protocol.TargetKindDM {
			if err := s.requireMatchingDuplicateDM(ctx, tx, message, topology); err != nil {
				return AppendLifecycle{}, err
			}
		}
		return AppendLifecycle{}, ErrDuplicateMessage
	}

	if err := s.upsertConversation(ctx, tx, message); err != nil {
		return AppendLifecycle{}, err
	}
	inserted, err := s.insertMessage(ctx, tx, message)
	if err != nil {
		return AppendLifecycle{}, err
	}
	if !inserted {
		if message.Target.Kind == protocol.TargetKindDM {
			if err := s.requireMatchingDuplicateDM(ctx, tx, message, topology); err != nil {
				return AppendLifecycle{}, err
			}
		}
		return AppendLifecycle{}, ErrDuplicateMessage
	}
	if err := s.insertArtifacts(ctx, tx, message); err != nil {
		return AppendLifecycle{}, err
	}
	if event != nil {
		if err := insertAttachmentDeliveryEvent(ctx, tx, s.dialect, *event); err != nil {
			return AppendLifecycle{}, err
		}
	}

	lifecycle, err := s.appendLifecycleUsingQuerier(ctx, tx, message)
	if err != nil {
		return AppendLifecycle{}, err
	}

	if err := tx.Commit(); err != nil {
		return AppendLifecycle{}, err
	}

	return lifecycle, nil
}

func (s *SQLStore) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListRoomMessagesContext(context.Background(), roomID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *SQLStore) ListRoomMessagesContext(
	ctx context.Context,
	roomID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	return s.listMessagesContext(ctx, messageScopeRoom, roomID, page)
}

func (s *SQLStore) ListThreadMessages(threadID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListThreadMessagesContext(context.Background(), threadID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *SQLStore) ListThreadMessagesContext(
	ctx context.Context,
	threadID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	return s.listMessagesContext(ctx, messageScopeThread, threadID, page)
}

func (s *SQLStore) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListDMMessagesContext(context.Background(), dmID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *SQLStore) ListDMMessagesContext(
	ctx context.Context,
	dmID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	return s.listMessagesContext(ctx, messageScopeDM, dmID, page)
}

func (s *SQLStore) ListArtifacts(filter protocol.ArtifactFilter, before string, limit int) (protocol.ArtifactPage, error) {
	return s.ListArtifactsContext(context.Background(), filter, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *SQLStore) ListArtifactsContext(
	ctx context.Context,
	filter protocol.ArtifactFilter,
	page protocol.PageRequest,
) (protocol.ArtifactPage, error) {
	return s.listArtifactsContext(ctx, filter, page)
}

func (s *SQLStore) insertMessage(ctx context.Context, tx *sql.Tx, message protocol.Message) (bool, error) {
	query := bindQuery(s.dialect, `
		INSERT INTO messages (
			id, network_id, target_kind, room_id, thread_id, parent_message_id, dm_id,
			origin_json, target_json, from_json, parts_json, mentions_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING
	`)

	result, err := tx.ExecContext(
		ctx,
		query,
		message.ID,
		message.NetworkID,
		message.Target.Kind,
		nullableString(message.Target.RoomID),
		nullableString(message.Target.ThreadID),
		nullableString(message.Target.ParentMessageID),
		nullableString(message.Target.DMID),
		marshalJSON(message.Origin),
		marshalJSON(message.Target),
		marshalJSON(message.From),
		marshalJSON(message.Parts),
		marshalJSON(message.Mentions),
		formatTime(message.CreatedAt),
	)
	if err != nil {
		return false, fmt.Errorf("insert message: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect message insert: %w", err)
	}
	return rows == 1, nil
}

func (s *SQLStore) upsertConversation(ctx context.Context, tx *sql.Tx, message protocol.Message) error {
	switch message.Target.Kind {
	case protocol.TargetKindThread:
		query := bindQuery(s.dialect, `
			INSERT INTO threads (id, network_id, fqid, room_id, parent_message_id, message_count, last_message_at)
			VALUES (?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT (id) DO UPDATE SET
				message_count = threads.message_count + 1,
				last_message_at = excluded.last_message_at
		`)
		_, err := tx.ExecContext(ctx, query, message.Target.ThreadID, message.NetworkID, protocol.ThreadFQID(message.NetworkID, message.Target.ThreadID), message.Target.RoomID, message.Target.ParentMessageID, formatTime(message.CreatedAt))
		if err != nil {
			return fmt.Errorf("upsert thread: %w", err)
		}
	case protocol.TargetKindDM:
		return s.upsertDMConversation(ctx, tx, message)
	}

	return nil
}

func (s *SQLStore) insertArtifacts(ctx context.Context, tx *sql.Tx, message protocol.Message) error {
	query := bindQuery(s.dialect, `
		INSERT INTO artifacts (
			id, network_id, fqid, message_id, target_kind, room_id, thread_id, dm_id,
			target_json, part_index, kind, media_type, filename, url, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)

	for index, part := range message.Parts {
		if !isArtifactPart(part) {
			continue
		}
		artifactID := fmt.Sprintf("art_%s_%d", message.ID, index)
		if _, err := tx.ExecContext(
			ctx,
			query,
			artifactID,
			message.NetworkID,
			protocol.ArtifactFQID(message.NetworkID, artifactID),
			message.ID,
			message.Target.Kind,
			nullableString(message.Target.RoomID),
			nullableString(message.Target.ThreadID),
			nullableString(message.Target.DMID),
			marshalJSON(message.Target),
			index,
			part.Kind,
			part.MediaType,
			part.Filename,
			part.URL,
			formatTime(message.CreatedAt),
		); err != nil {
			return fmt.Errorf("insert artifact: %w", err)
		}
	}

	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *SQLStore) appendLifecycleUsingQuerier(ctx context.Context, tx *sql.Tx, message protocol.Message) (AppendLifecycle, error) {
	lifecycle := AppendLifecycle{}

	switch message.Target.Kind {
	case protocol.TargetKindThread:
		thread, ok, err := getThreadUsingQuerier(ctx, tx, s.dialect, message.Target.ThreadID)
		if err != nil {
			return AppendLifecycle{}, err
		}
		if ok && thread.MessageCount == 1 {
			lifecycle.Thread = &thread
		}
	case protocol.TargetKindDM:
		dm, ok, err := getDirectConversationUsingQuerier(ctx, tx, s.dialect, message.Target.DMID)
		if err != nil {
			return AppendLifecycle{}, err
		}
		if ok && dm.MessageCount == 1 {
			lifecycle.DM = &dm
		}
	}

	return lifecycle, nil
}
