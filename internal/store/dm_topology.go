package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type dmTopology struct {
	networkID      string
	canonicalIDs   []string
	participantIDs []string
}

func topologyForDMMessage(message protocol.Message) (dmTopology, error) {
	networkID := strings.TrimSpace(message.NetworkID)
	if networkID == "" {
		return dmTopology{}, dmTopologyConflict()
	}

	uniqueParticipantIDs := protocol.UniqueTrimmedStrings(message.Target.ParticipantIDs)
	canonicalIDs := make([]string, 0, len(uniqueParticipantIDs))
	allLocal := true
	for _, participantID := range uniqueParticipantIDs {
		participantNetwork, agentID, ok := resolveDMIdentity(networkID, participantID)
		if !ok {
			return dmTopology{}, dmTopologyConflict()
		}
		if participantNetwork != networkID {
			allLocal = false
		}
		canonicalIDs = append(canonicalIDs, protocol.ScopedAgentID(participantNetwork, agentID))
	}
	canonicalIDs = protocol.SortedUniqueTrimmedStrings(canonicalIDs)
	if len(canonicalIDs) < 2 || len(canonicalIDs) != len(uniqueParticipantIDs) {
		return dmTopology{}, dmTopologyConflict()
	}

	senderID, ok := canonicalDMActorID(networkID, message.From)
	if !ok || !slices.Contains(canonicalIDs, senderID) {
		return dmTopology{}, dmTopologyConflict()
	}

	participantIDs := append([]string(nil), canonicalIDs...)
	if allLocal {
		participantIDs = make([]string, 0, len(canonicalIDs))
		for _, canonicalID := range canonicalIDs {
			_, agentID, _ := protocol.ParseScopedAgentID(canonicalID)
			participantIDs = append(participantIDs, agentID)
		}
	}

	return dmTopology{
		networkID:      networkID,
		canonicalIDs:   canonicalIDs,
		participantIDs: participantIDs,
	}, nil
}

func topologyForStoredDM(networkID string, participantIDs []string) (dmTopology, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return dmTopology{}, dmTopologyConflict()
	}

	uniqueParticipantIDs := protocol.UniqueTrimmedStrings(participantIDs)
	canonicalIDs := make([]string, 0, len(uniqueParticipantIDs))
	for _, participantID := range uniqueParticipantIDs {
		participantNetwork, agentID, ok := resolveDMIdentity(networkID, participantID)
		if !ok {
			return dmTopology{}, dmTopologyConflict()
		}
		canonicalIDs = append(canonicalIDs, protocol.ScopedAgentID(participantNetwork, agentID))
	}
	canonicalIDs = protocol.SortedUniqueTrimmedStrings(canonicalIDs)
	if len(canonicalIDs) < 2 || len(canonicalIDs) != len(uniqueParticipantIDs) {
		return dmTopology{}, dmTopologyConflict()
	}

	return dmTopology{networkID: networkID, canonicalIDs: canonicalIDs}, nil
}

func (topology dmTopology) matches(networkID string, participantIDs []string) bool {
	existing, err := topologyForStoredDM(networkID, participantIDs)
	return err == nil && topology.networkID == existing.networkID &&
		slices.Equal(topology.canonicalIDs, existing.canonicalIDs)
}

func canonicalDMActorID(defaultNetworkID string, actor protocol.Actor) (string, bool) {
	actorNetworkID := strings.TrimSpace(actor.NetworkID)
	if actorNetworkID == "" {
		actorNetworkID = defaultNetworkID
	}
	networkID, agentID, ok := resolveDMIdentity(actorNetworkID, actor.ID)
	if !ok {
		return "", false
	}
	if supplied := strings.TrimSpace(actor.NetworkID); supplied != "" && supplied != networkID {
		return "", false
	}
	if supplied := strings.TrimSpace(actor.FQID); supplied != "" {
		fqNetwork, fqAgent, parsed := protocol.ParseAgentFQID(supplied)
		if !parsed || strings.TrimSpace(fqNetwork) != networkID || strings.TrimSpace(fqAgent) != agentID {
			return "", false
		}
	}
	return protocol.ScopedAgentID(networkID, agentID), true
}

func resolveDMIdentity(defaultNetworkID string, value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	if networkID, agentID, ok := protocol.ParseScopedAgentID(trimmed); ok {
		return validDMIdentity(networkID, agentID)
	}
	if networkID, agentID, ok := protocol.ParseAgentFQID(trimmed); ok {
		return validDMIdentity(networkID, agentID)
	}
	return validDMIdentity(defaultNetworkID, trimmed)
}

func validDMIdentity(networkID string, agentID string) (string, string, bool) {
	networkID = strings.TrimSpace(networkID)
	agentID = strings.TrimSpace(agentID)
	if protocol.ValidateMessageID(networkID) != nil || protocol.ValidateMessageID(agentID) != nil {
		return "", "", false
	}
	return networkID, agentID, true
}

func dmTopologyConflict() error {
	return fmt.Errorf("%w", ErrDMTopologyConflict)
}

func (s *SQLStore) upsertDMConversation(ctx context.Context, tx *sql.Tx, message protocol.Message) error {
	topology, err := topologyForDMMessage(message)
	if err != nil {
		return err
	}

	insert := bindQuery(s.dialect, `
		INSERT INTO dm_conversations (dm_id, network_id, fqid, message_count, last_message_at)
		VALUES (?, ?, ?, 0, ?)
		ON CONFLICT (dm_id) DO NOTHING
	`)
	result, err := tx.ExecContext(
		ctx,
		insert,
		message.Target.DMID,
		topology.networkID,
		protocol.DMFQID(topology.networkID, message.Target.DMID),
		formatTime(message.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("claim dm conversation: %w", err)
	}
	created, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect dm conversation claim: %w", err)
	}

	if created == 1 {
		if err := s.insertDMParticipants(ctx, tx, message.Target.DMID, topology.participantIDs); err != nil {
			return err
		}
	} else if err := s.requireMatchingDMTopology(ctx, tx, message.Target.DMID, topology); err != nil {
		return err
	}

	update := bindQuery(s.dialect, `
		UPDATE dm_conversations
		SET message_count = message_count + 1, last_message_at = ?
		WHERE dm_id = ?
	`)
	if _, err := tx.ExecContext(ctx, update, formatTime(message.CreatedAt), message.Target.DMID); err != nil {
		return fmt.Errorf("update dm conversation: %w", err)
	}
	return nil
}

func (s *SQLStore) insertDMParticipants(
	ctx context.Context,
	tx *sql.Tx,
	dmID string,
	participantIDs []string,
) error {
	query := bindQuery(s.dialect, `INSERT INTO dm_participants (dm_id, participant_id) VALUES (?, ?)`)
	for _, participantID := range participantIDs {
		if _, err := tx.ExecContext(ctx, query, dmID, participantID); err != nil {
			return fmt.Errorf("insert dm participant: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) requireMatchingDMTopology(
	ctx context.Context,
	tx *sql.Tx,
	dmID string,
	topology dmTopology,
) error {
	query := `SELECT network_id FROM dm_conversations WHERE dm_id = ?`
	if s.dialect == dialectPostgres {
		query += ` FOR UPDATE`
	}
	var networkID string
	if err := tx.QueryRowContext(ctx, bindQuery(s.dialect, query), dmID).Scan(&networkID); err != nil {
		return fmt.Errorf("read dm conversation topology: %w", err)
	}

	participantsQuery := bindQuery(s.dialect, `
		SELECT participant_id FROM dm_participants WHERE dm_id = ? ORDER BY participant_id ASC
	`)
	rows, err := tx.QueryContext(ctx, participantsQuery, dmID)
	if err != nil {
		return fmt.Errorf("read dm participants: %w", err)
	}
	defer rows.Close()
	participantIDs := make([]string, 0)
	for rows.Next() {
		var participantID string
		if err := rows.Scan(&participantID); err != nil {
			return fmt.Errorf("scan dm participant: %w", err)
		}
		participantIDs = append(participantIDs, participantID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate dm participants: %w", err)
	}
	if !topology.matches(networkID, participantIDs) {
		return dmTopologyConflict()
	}
	return nil
}
