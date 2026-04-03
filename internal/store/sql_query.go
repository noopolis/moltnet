package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type queryRowContexter interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

type messageScope string

const (
	messageScopeRoom   messageScope = "room"
	messageScopeThread messageScope = "thread"
	messageScopeDM     messageScope = "dm"
)

func (s *SQLStore) listMessagesContext(
	ctx context.Context,
	scope messageScope,
	value string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	if page.Before != "" && page.After != "" {
		return protocol.MessagePage{}, ErrInvalidCursor
	}
	if err := protocol.ValidatePageRequest(page); err != nil {
		return protocol.MessagePage{}, ErrInvalidCursor
	}

	limit := normalizePageLimit(page.Limit)
	if page.Before != "" {
		if ok, err := messageCursorExistsContext(ctx, s.db, s.dialect, page.Before); err != nil {
			return protocol.MessagePage{}, fmt.Errorf("list messages: %w", err)
		} else if !ok {
			return protocol.MessagePage{}, ErrInvalidCursor
		}
	}
	if page.After != "" {
		if ok, err := messageCursorExistsContext(ctx, s.db, s.dialect, page.After); err != nil {
			return protocol.MessagePage{}, fmt.Errorf("list messages: %w", err)
		} else if !ok {
			return protocol.MessagePage{}, ErrInvalidCursor
		}
	}
	whereClause, err := messageWhereClause(scope, "m")
	if err != nil {
		return protocol.MessagePage{}, fmt.Errorf("list messages: %w", err)
	}

	args := make([]any, 0, 3)
	query := `
		SELECT m.id, m.network_id, m.room_id, m.thread_id, m.parent_message_id, m.dm_id, m.origin_json, m.target_json, m.from_json, m.parts_json, m.mentions_json, m.created_at
		FROM messages m`
	if page.Before != "" {
		query += ` LEFT JOIN messages before_cursor ON before_cursor.id = ?`
		args = append(args, page.Before)
	}
	if page.After != "" {
		query += ` LEFT JOIN messages after_cursor ON after_cursor.id = ?`
		args = append(args, page.After)
	}
	query += ` WHERE ` + whereClause
	args = append(args, value)
	if page.Before != "" {
		query += ` AND before_cursor.id IS NOT NULL AND (m.created_at < before_cursor.created_at OR (m.created_at = before_cursor.created_at AND m.id < before_cursor.id))`
		query += ` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
		args = append(args, limit+1)
	} else if page.After != "" {
		query += ` AND after_cursor.id IS NOT NULL AND (m.created_at > after_cursor.created_at OR (m.created_at = after_cursor.created_at AND m.id > after_cursor.id))`
		query += ` ORDER BY m.created_at ASC, m.id ASC LIMIT ?`
		args = append(args, limit+1)
	} else {
		query += ` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
		args = append(args, limit+1)
	}

	rows, err := s.db.QueryContext(ctx, bindQuery(s.dialect, query), args...)
	if err != nil {
		return protocol.MessagePage{}, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]protocol.Message, 0, limit+1)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			observability.Logger(ctx, "store.sql", "scope", scope, "value", value, "error", err).
				Warn("skip malformed message row")
			continue
		}
		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return protocol.MessagePage{}, fmt.Errorf("iterate messages: %w", err)
	}

	if page.After != "" {
		return protocol.MessagePage{
			Messages: messagesPageSlice(messages, limit),
			Page:     pageInfoForSlice(messages, limit),
		}, nil
	}

	return pageMessagesDescending(messages, limit), nil
}

func messageWhereClause(scope messageScope, alias string) (string, error) {
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}

	switch scope {
	case messageScopeRoom:
		return column("room_id") + ` = ? AND ` + column("target_kind") + ` = 'room'`, nil
	case messageScopeThread:
		return column("thread_id") + ` = ? AND ` + column("target_kind") + ` = 'thread'`, nil
	case messageScopeDM:
		return column("dm_id") + ` = ? AND ` + column("target_kind") + ` = 'dm'`, nil
	default:
		return "", fmt.Errorf("unsupported message scope %q", scope)
	}
}

func (s *SQLStore) listArtifactsContext(
	ctx context.Context,
	filter protocol.ArtifactFilter,
	page protocol.PageRequest,
) (protocol.ArtifactPage, error) {
	if page.Before != "" && page.After != "" {
		return protocol.ArtifactPage{}, ErrInvalidCursor
	}
	if err := protocol.ValidatePageRequest(page); err != nil {
		return protocol.ArtifactPage{}, ErrInvalidCursor
	}

	limit := normalizePageLimit(page.Limit)
	if page.Before != "" {
		if ok, err := artifactCursorExistsContext(ctx, s.db, s.dialect, page.Before); err != nil {
			return protocol.ArtifactPage{}, fmt.Errorf("list artifacts: %w", err)
		} else if !ok {
			return protocol.ArtifactPage{}, ErrInvalidCursor
		}
	}
	if page.After != "" {
		if ok, err := artifactCursorExistsContext(ctx, s.db, s.dialect, page.After); err != nil {
			return protocol.ArtifactPage{}, fmt.Errorf("list artifacts: %w", err)
		} else if !ok {
			return protocol.ArtifactPage{}, ErrInvalidCursor
		}
	}
	where := "1 = 1"
	args := make([]any, 0, 5)
	query := `
		SELECT a.id, a.network_id, a.fqid, a.message_id, a.target_json, a.part_index, a.kind, a.media_type, a.filename, a.url, a.created_at
		FROM artifacts a`
	if page.Before != "" {
		query += ` LEFT JOIN artifacts before_cursor ON before_cursor.id = ?`
		args = append(args, page.Before)
	}
	if page.After != "" {
		query += ` LEFT JOIN artifacts after_cursor ON after_cursor.id = ?`
		args = append(args, page.After)
	}
	switch {
	case filter.ThreadID != "":
		where = "a.thread_id = ?"
		args = append(args, filter.ThreadID)
	case filter.DMID != "":
		where = "a.dm_id = ?"
		args = append(args, filter.DMID)
	case filter.RoomID != "":
		where = "a.room_id = ?"
		args = append(args, filter.RoomID)
	}
	query += ` WHERE ` + where
	if page.Before != "" {
		query += ` AND before_cursor.id IS NOT NULL AND (a.created_at < before_cursor.created_at OR (a.created_at = before_cursor.created_at AND a.id < before_cursor.id))`
		query += ` ORDER BY a.created_at DESC, a.id DESC LIMIT ?`
		args = append(args, limit+1)
	} else if page.After != "" {
		query += ` AND after_cursor.id IS NOT NULL AND (a.created_at > after_cursor.created_at OR (a.created_at = after_cursor.created_at AND a.id > after_cursor.id))`
		query += ` ORDER BY a.created_at ASC, a.id ASC LIMIT ?`
		args = append(args, limit+1)
	} else {
		query += ` ORDER BY a.created_at DESC, a.id DESC LIMIT ?`
		args = append(args, limit+1)
	}

	rows, err := s.db.QueryContext(ctx, bindQuery(s.dialect, query), args...)
	if err != nil {
		return protocol.ArtifactPage{}, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := make([]protocol.Artifact, 0, limit+1)
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			observability.Logger(ctx, "store.sql", "filter", filter, "error", err).
				Warn("skip malformed artifact row")
			continue
		}
		artifacts = append(artifacts, artifact)
	}

	if err := rows.Err(); err != nil {
		return protocol.ArtifactPage{}, fmt.Errorf("iterate artifacts: %w", err)
	}

	if page.After != "" {
		return protocol.ArtifactPage{
			Artifacts: artifactsPageSlice(artifacts, limit),
			Page:      pageInfoForArtifacts(artifacts, limit),
		}, nil
	}

	return pageArtifactsDescending(artifacts, limit), nil
}

func messageCursorExistsContext(ctx context.Context, queryer queryRowContexter, dialect sqlDialect, id string) (bool, error) {
	query := bindQuery(dialect, `SELECT 1 FROM messages WHERE id = ?`)
	var exists int
	if err := queryer.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func artifactCursorExistsContext(ctx context.Context, queryer queryRowContexter, dialect sqlDialect, id string) (bool, error) {
	query := bindQuery(dialect, `SELECT 1 FROM artifacts WHERE id = ?`)
	var exists int
	if err := queryer.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func pageMessagesDescending(values []protocol.Message, limit int) protocol.MessagePage {
	page := protocol.PageInfo{}
	if len(values) > limit {
		page.HasMore = true
		values = values[:limit]
	}
	if page.HasMore && len(values) > 0 {
		page.NextBefore = values[len(values)-1].ID
	}

	reverseMessages(values)

	return protocol.MessagePage{
		Messages: values,
		Page:     page,
	}
}

func pageArtifactsDescending(values []protocol.Artifact, limit int) protocol.ArtifactPage {
	page := protocol.PageInfo{}
	if len(values) > limit {
		page.HasMore = true
		values = values[:limit]
	}
	if page.HasMore && len(values) > 0 {
		page.NextBefore = values[len(values)-1].ID
	}

	reverseArtifacts(values)

	return protocol.ArtifactPage{
		Artifacts: values,
		Page:      page,
	}
}

func normalizePageLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func messagesPageSlice(values []protocol.Message, limit int) []protocol.Message {
	if len(values) > limit {
		return append([]protocol.Message(nil), values[:limit]...)
	}
	return append([]protocol.Message(nil), values...)
}

func pageInfoForSlice(values []protocol.Message, limit int) protocol.PageInfo {
	page := protocol.PageInfo{}
	if len(values) > limit {
		page.HasMore = true
		values = values[:limit]
	}
	if page.HasMore && len(values) > 0 {
		page.NextAfter = values[len(values)-1].ID
	}
	return page
}

func artifactsPageSlice(values []protocol.Artifact, limit int) []protocol.Artifact {
	if len(values) > limit {
		return append([]protocol.Artifact(nil), values[:limit]...)
	}
	return append([]protocol.Artifact(nil), values...)
}

func pageInfoForArtifacts(values []protocol.Artifact, limit int) protocol.PageInfo {
	page := protocol.PageInfo{}
	if len(values) > limit {
		page.HasMore = true
		values = values[:limit]
	}
	if page.HasMore && len(values) > 0 {
		page.NextAfter = values[len(values)-1].ID
	}
	return page
}

func reverseMessages(values []protocol.Message) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseArtifacts(values []protocol.Artifact) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
