package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *SQLStore) ListThreads(roomID string) ([]protocol.Thread, error) {
	return s.ListThreadsContext(context.Background(), roomID)
}

func (s *SQLStore) ListThreadsContext(ctx context.Context, roomID string) ([]protocol.Thread, error) {
	query := bindQuery(s.dialect, `SELECT id, network_id, fqid, room_id, parent_message_id, message_count, last_message_at FROM threads WHERE room_id = ? ORDER BY id ASC`)
	rows, err := s.db.QueryContext(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	threads := make([]protocol.Thread, 0)
	for rows.Next() {
		var thread protocol.Thread
		var lastMessageAt any
		if err := rows.Scan(&thread.ID, &thread.NetworkID, &thread.FQID, &thread.RoomID, &thread.ParentMessageID, &thread.MessageCount, &lastMessageAt); err != nil {
			return nil, fmt.Errorf("scan thread: %w", err)
		}
		thread.LastMessageAt = parseTime(lastMessageAt)
		threads = append(threads, thread)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate threads: %w", err)
	}

	return threads, nil
}

func (s *SQLStore) ListDirectConversations() ([]protocol.DirectConversation, error) {
	return s.ListDirectConversationsContext(context.Background())
}

func (s *SQLStore) ListDirectConversationsContext(ctx context.Context) ([]protocol.DirectConversation, error) {
	query := bindQuery(s.dialect, `
		SELECT dc.dm_id, dc.network_id, dc.fqid, dc.message_count, dc.last_message_at, dp.participant_id
		FROM dm_conversations dc
		LEFT JOIN dm_participants dp ON dp.dm_id = dc.dm_id
		ORDER BY dc.dm_id ASC, dp.participant_id ASC
	`)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list dm conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]protocol.DirectConversation, 0)
	conversationsByID := make(map[string]*protocol.DirectConversation)
	for rows.Next() {
		var (
			dmID, networkID, fqid string
			lastMessageAt         any
			messageCount          int
			participantID         sql.NullString
		)
		if err := rows.Scan(&dmID, &networkID, &fqid, &messageCount, &lastMessageAt, &participantID); err != nil {
			return nil, fmt.Errorf("scan dm conversation: %w", err)
		}
		conversation, ok := conversationsByID[dmID]
		if !ok {
			conversation = &protocol.DirectConversation{
				ID:            dmID,
				NetworkID:     networkID,
				FQID:          fqid,
				MessageCount:  messageCount,
				LastMessageAt: parseTime(lastMessageAt),
			}
			conversations = append(conversations, *conversation)
			conversation = &conversations[len(conversations)-1]
			conversationsByID[dmID] = conversation
		}
		if participantID.Valid {
			conversation.ParticipantIDs = append(conversation.ParticipantIDs, participantID.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dm conversations: %w", err)
	}

	return conversations, nil
}

func (s *SQLStore) GetDirectConversationContext(
	ctx context.Context,
	dmID string,
) (protocol.DirectConversation, bool, error) {
	return getDirectConversationUsingQuerier(ctx, s.db, s.dialect, dmID)
}

func (s *SQLStore) ListAgentsContext(ctx context.Context) ([]protocol.AgentSummary, error) {
	query := bindQuery(s.dialect, `
		SELECT rm.member_id, r.network_id, r.id
		FROM room_members rm
		JOIN rooms r ON r.id = rm.room_id
		ORDER BY rm.member_id ASC, r.id ASC
	`)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	agents := make([]protocol.AgentSummary, 0)
	agentsByID := make(map[string]*protocol.AgentSummary)
	for rows.Next() {
		var memberID, networkID, roomID string
		if err := rows.Scan(&memberID, &networkID, &roomID); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agent, ok := agentsByID[memberID]
		if !ok {
			agent = &protocol.AgentSummary{
				ID:        memberID,
				NetworkID: networkID,
				FQID:      protocol.AgentFQID(networkID, memberID),
			}
			agents = append(agents, *agent)
			agent = &agents[len(agents)-1]
			agentsByID[memberID] = agent
		}
		agent.Rooms = append(agent.Rooms, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}

	return agents, nil
}

func (s *SQLStore) GetAgentContext(ctx context.Context, agentID string) (protocol.AgentSummary, bool, error) {
	query := bindQuery(s.dialect, `
		SELECT rm.member_id, r.network_id, r.id
		FROM room_members rm
		JOIN rooms r ON r.id = rm.room_id
		WHERE rm.member_id = ?
		ORDER BY r.id ASC
	`)
	rows, err := s.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return protocol.AgentSummary{}, false, fmt.Errorf("get agent %q: %w", agentID, err)
	}
	defer rows.Close()

	var (
		agent protocol.AgentSummary
		found bool
	)
	for rows.Next() {
		var memberID, networkID, roomID string
		if err := rows.Scan(&memberID, &networkID, &roomID); err != nil {
			return protocol.AgentSummary{}, false, fmt.Errorf("scan agent %q: %w", agentID, err)
		}
		if !found {
			found = true
			agent = protocol.AgentSummary{
				ID:        memberID,
				NetworkID: networkID,
				FQID:      protocol.AgentFQID(networkID, memberID),
			}
		}
		agent.Rooms = append(agent.Rooms, roomID)
	}
	if err := rows.Err(); err != nil {
		return protocol.AgentSummary{}, false, fmt.Errorf("iterate agent %q: %w", agentID, err)
	}
	return agent, found, nil
}
