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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AppendLifecycle{}, fmt.Errorf("begin append message: %w", err)
	}
	defer rollback(tx)

	if err := s.upsertConversation(ctx, tx, message); err != nil {
		return AppendLifecycle{}, err
	}
	if err := s.insertMessage(ctx, tx, message); err != nil {
		return AppendLifecycle{}, err
	}
	if err := s.insertArtifacts(ctx, tx, message); err != nil {
		return AppendLifecycle{}, err
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

func (s *SQLStore) insertMessage(ctx context.Context, tx *sql.Tx, message protocol.Message) error {
	query := bindQuery(s.dialect, `
		INSERT INTO messages (
			id, network_id, target_kind, room_id, thread_id, parent_message_id, dm_id,
			origin_json, target_json, from_json, parts_json, mentions_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)

	_, err := tx.ExecContext(
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
		if isDuplicateMessageError(err) {
			return ErrDuplicateMessage
		}
		return fmt.Errorf("insert message: %w", err)
	}

	return nil
}

func isDuplicateMessageError(err error) bool {
	return isUniqueConstraintError(err)
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
		query := bindQuery(s.dialect, `
			INSERT INTO dm_conversations (dm_id, network_id, fqid, message_count, last_message_at)
			VALUES (?, ?, ?, 1, ?)
			ON CONFLICT (dm_id) DO UPDATE SET
				message_count = dm_conversations.message_count + 1,
				last_message_at = excluded.last_message_at,
				network_id = excluded.network_id,
				fqid = excluded.fqid
		`)
		_, err := tx.ExecContext(ctx, query, message.Target.DMID, message.NetworkID, protocol.DMFQID(message.NetworkID, message.Target.DMID), formatTime(message.CreatedAt))
		if err != nil {
			return fmt.Errorf("upsert dm conversation: %w", err)
		}

		participants := append([]string(nil), message.Target.ParticipantIDs...)
		participants = append(participants, protocol.RemoteParticipantID(message.NetworkID, message.From))
		for _, participantID := range protocol.SortedUniqueTrimmedStrings(participants) {
			memberQuery := bindQuery(s.dialect, `INSERT INTO dm_participants (dm_id, participant_id) VALUES (?, ?) ON CONFLICT (dm_id, participant_id) DO NOTHING`)
			if _, err := tx.ExecContext(ctx, memberQuery, message.Target.DMID, participantID); err != nil {
				return fmt.Errorf("insert dm participant: %w", err)
			}
		}
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
