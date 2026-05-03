package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *SQLStore) RegisterAgentContext(
	ctx context.Context,
	registration protocol.AgentRegistration,
) (protocol.AgentRegistration, error) {
	registration.AgentToken = ""

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.AgentRegistration{}, fmt.Errorf("begin register agent: %w", err)
	}
	defer rollback(tx)

	existing, ok, err := s.getRegisteredAgentUsingQuerier(ctx, tx, registration.AgentID)
	if err != nil {
		return protocol.AgentRegistration{}, err
	}
	if ok {
		if existing.CredentialKey != registration.CredentialKey {
			return protocol.AgentRegistration{}, ErrAgentCredential
		}
		if registration.DisplayName != "" {
			existing.DisplayName = registration.DisplayName
		}
		existing.UpdatedAt = registration.UpdatedAt

		query := bindQuery(s.dialect, `UPDATE agents SET display_name = ?, updated_at = ? WHERE agent_id = ?`)
		if _, err := tx.ExecContext(ctx, query, existing.DisplayName, formatTime(existing.UpdatedAt), existing.AgentID); err != nil {
			return protocol.AgentRegistration{}, fmt.Errorf("update agent %q: %w", existing.AgentID, err)
		}
		if err := tx.Commit(); err != nil {
			return protocol.AgentRegistration{}, fmt.Errorf("commit update agent: %w", err)
		}
		return existing, nil
	}

	query := bindQuery(s.dialect, `
		INSERT INTO agents (agent_id, network_id, actor_uid, actor_uri, display_name, credential_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := tx.ExecContext(
		ctx,
		query,
		registration.AgentID,
		registration.NetworkID,
		registration.ActorUID,
		registration.ActorURI,
		registration.DisplayName,
		registration.CredentialKey,
		formatTime(registration.CreatedAt),
		formatTime(registration.UpdatedAt),
	); err != nil {
		if isUniqueConstraintError(err) {
			_ = tx.Rollback()
			return s.RegisterAgentContext(ctx, registration)
		}
		return protocol.AgentRegistration{}, fmt.Errorf("insert agent %q: %w", registration.AgentID, err)
	}
	if err := tx.Commit(); err != nil {
		return protocol.AgentRegistration{}, fmt.Errorf("commit register agent: %w", err)
	}

	return registration, nil
}

func (s *SQLStore) ListRegisteredAgentsContext(ctx context.Context) ([]protocol.AgentRegistration, error) {
	query := bindQuery(s.dialect, `
		SELECT agent_id, network_id, actor_uid, actor_uri, display_name, credential_key, created_at, updated_at
		FROM agents
		ORDER BY agent_id ASC
	`)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list registered agents: %w", err)
	}
	defer rows.Close()

	agents := make([]protocol.AgentRegistration, 0)
	for rows.Next() {
		agent, err := scanAgentRegistration(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registered agents: %w", err)
	}

	return agents, nil
}

func (s *SQLStore) GetRegisteredAgentContext(
	ctx context.Context,
	agentID string,
) (protocol.AgentRegistration, bool, error) {
	return s.getRegisteredAgentUsingQuerier(ctx, s.db, agentID)
}

func (s *SQLStore) GetRegisteredAgentByCredentialKeyContext(
	ctx context.Context,
	credentialKey string,
) (protocol.AgentRegistration, bool, error) {
	trimmed := strings.TrimSpace(credentialKey)
	if trimmed == "" {
		return protocol.AgentRegistration{}, false, nil
	}

	query := bindQuery(s.dialect, `
		SELECT agent_id, network_id, actor_uid, actor_uri, display_name, credential_key, created_at, updated_at
		FROM agents
		WHERE credential_key = ?
		ORDER BY agent_id ASC
		LIMIT 1
	`)
	var (
		agent          protocol.AgentRegistration
		displayName    sql.NullString
		createdAtValue any
		updatedAtValue any
	)
	if err := s.db.QueryRowContext(ctx, query, trimmed).Scan(
		&agent.AgentID,
		&agent.NetworkID,
		&agent.ActorUID,
		&agent.ActorURI,
		&displayName,
		&agent.CredentialKey,
		&createdAtValue,
		&updatedAtValue,
	); err != nil {
		if isNoRows(err) {
			return protocol.AgentRegistration{}, false, nil
		}
		return protocol.AgentRegistration{}, false, fmt.Errorf("get registered agent by credential: %w", err)
	}
	if displayName.Valid {
		agent.DisplayName = displayName.String
	}
	agent.CreatedAt = parseTime(createdAtValue)
	agent.UpdatedAt = parseTime(updatedAtValue)
	return agent, true, nil
}

type agentQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *SQLStore) getRegisteredAgentUsingQuerier(
	ctx context.Context,
	querier agentQuerier,
	agentID string,
) (protocol.AgentRegistration, bool, error) {
	query := bindQuery(s.dialect, `
		SELECT agent_id, network_id, actor_uid, actor_uri, display_name, credential_key, created_at, updated_at
		FROM agents
		WHERE agent_id = ?
	`)
	var (
		agent          protocol.AgentRegistration
		displayName    sql.NullString
		createdAtValue any
		updatedAtValue any
	)
	if err := querier.QueryRowContext(ctx, query, agentID).Scan(
		&agent.AgentID,
		&agent.NetworkID,
		&agent.ActorUID,
		&agent.ActorURI,
		&displayName,
		&agent.CredentialKey,
		&createdAtValue,
		&updatedAtValue,
	); err != nil {
		if isNoRows(err) {
			return protocol.AgentRegistration{}, false, nil
		}
		return protocol.AgentRegistration{}, false, fmt.Errorf("get registered agent %q: %w", agentID, err)
	}
	if displayName.Valid {
		agent.DisplayName = displayName.String
	}
	agent.CreatedAt = parseTime(createdAtValue)
	agent.UpdatedAt = parseTime(updatedAtValue)

	return agent, true, nil
}

type agentRow interface {
	Scan(dest ...any) error
}

func scanAgentRegistration(row agentRow) (protocol.AgentRegistration, error) {
	var (
		agent          protocol.AgentRegistration
		displayName    sql.NullString
		createdAtValue any
		updatedAtValue any
	)
	if err := row.Scan(
		&agent.AgentID,
		&agent.NetworkID,
		&agent.ActorUID,
		&agent.ActorURI,
		&displayName,
		&agent.CredentialKey,
		&createdAtValue,
		&updatedAtValue,
	); err != nil {
		return protocol.AgentRegistration{}, fmt.Errorf("scan registered agent: %w", err)
	}
	if displayName.Valid {
		agent.DisplayName = displayName.String
	}
	agent.CreatedAt = parseTime(createdAtValue)
	agent.UpdatedAt = parseTime(updatedAtValue)

	return agent, nil
}
