package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type messageRow struct {
	ID              string
	NetworkID       string
	RoomID          sql.NullString
	ThreadID        sql.NullString
	ParentMessageID sql.NullString
	DMID            sql.NullString
	OriginJSON      string
	TargetJSON      string
	FromJSON        string
	PartsJSON       string
	MentionsJSON    string
	CreatedAt       any
}

type artifactRow struct {
	ID         string
	NetworkID  string
	FQID       string
	MessageID  string
	TargetJSON string
	PartIndex  int
	Kind       string
	MediaType  sql.NullString
	Filename   sql.NullString
	URL        sql.NullString
	CreatedAt  any
}

func scanMessage(rows *sql.Rows) (protocol.Message, error) {
	var row messageRow
	if err := rows.Scan(
		&row.ID,
		&row.NetworkID,
		&row.RoomID,
		&row.ThreadID,
		&row.ParentMessageID,
		&row.DMID,
		&row.OriginJSON,
		&row.TargetJSON,
		&row.FromJSON,
		&row.PartsJSON,
		&row.MentionsJSON,
		&row.CreatedAt,
	); err != nil {
		return protocol.Message{}, fmt.Errorf("scan message row: %w", err)
	}

	var target protocol.Target
	if err := json.Unmarshal([]byte(row.TargetJSON), &target); err != nil {
		return protocol.Message{}, fmt.Errorf("decode message target: %w", err)
	}
	var origin protocol.MessageOrigin
	if err := json.Unmarshal([]byte(row.OriginJSON), &origin); err != nil {
		return protocol.Message{}, fmt.Errorf("decode message origin: %w", err)
	}
	var from protocol.Actor
	if err := json.Unmarshal([]byte(row.FromJSON), &from); err != nil {
		return protocol.Message{}, fmt.Errorf("decode message actor: %w", err)
	}
	var parts []protocol.Part
	if err := json.Unmarshal([]byte(row.PartsJSON), &parts); err != nil {
		return protocol.Message{}, fmt.Errorf("decode message parts: %w", err)
	}
	var mentions []string
	if err := json.Unmarshal([]byte(row.MentionsJSON), &mentions); err != nil {
		return protocol.Message{}, fmt.Errorf("decode message mentions: %w", err)
	}

	return protocol.Message{
		ID:        row.ID,
		NetworkID: row.NetworkID,
		Origin:    origin,
		Target:    target,
		From:      from,
		Parts:     parts,
		Mentions:  mentions,
		CreatedAt: parseTime(row.CreatedAt),
	}, nil
}

func scanArtifact(rows *sql.Rows) (protocol.Artifact, error) {
	var row artifactRow
	if err := rows.Scan(
		&row.ID,
		&row.NetworkID,
		&row.FQID,
		&row.MessageID,
		&row.TargetJSON,
		&row.PartIndex,
		&row.Kind,
		&row.MediaType,
		&row.Filename,
		&row.URL,
		&row.CreatedAt,
	); err != nil {
		return protocol.Artifact{}, fmt.Errorf("scan artifact row: %w", err)
	}

	var target protocol.Target
	if err := json.Unmarshal([]byte(row.TargetJSON), &target); err != nil {
		return protocol.Artifact{}, fmt.Errorf("decode artifact target: %w", err)
	}

	return protocol.Artifact{
		ID:        row.ID,
		NetworkID: row.NetworkID,
		FQID:      row.FQID,
		MessageID: row.MessageID,
		Target:    target,
		PartIndex: row.PartIndex,
		Kind:      row.Kind,
		MediaType: row.MediaType.String,
		Filename:  row.Filename.String,
		URL:       row.URL.String,
		CreatedAt: parseTime(row.CreatedAt),
	}, nil
}
