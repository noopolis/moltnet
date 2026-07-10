package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// fixtureRecordedAt is a fixed instant used for both fixture events.
// recorded_at is diagnostics-only per specs/causal-event.v1.schema.json
// (reconciliation never orders or trusts it), so a deterministic value keeps
// the fixture output byte-for-byte reproducible across runs.
var fixtureRecordedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// WriteMessageAcceptedFixture writes exactly two message.accepted causal
// events for runID to w, through the real CausalWriter (so seq assignment
// and CausalEvent.Validate both run for real, the same as production
// moltnet). It is B92's cross-repo conformance fixture: the root
// conformance harness stitches these two events into the daimon/mneme/
// simfile chain (see specs/causal-event.v1.schema.json and
// src/ledger/conformance.ts).
func WriteMessageAcceptedFixture(w io.Writer, runID string) error {
	writer := NewCausalWriter(w)
	streamID := NetworkStreamID("fixture-network")

	// principal_id follows the grammar ^(agent|operator|system):.+ (see
	// specs/causal-event.v1.schema.json), matching how production moltnet
	// stamps a principal derived from registrationCredentialKey(ctx) rather
	// than any caller-claimed protocol.Actor/From field (see
	// internal/rooms/causal.go).
	const principalID = "agent:fixture-agent"

	first, err := buildMessageAcceptedEvent(runID, streamID, "moltnet:fixture-m1", "fixture-m1", principalID, "fixture message 1", fixtureRecordedAt)
	if err != nil {
		return fmt.Errorf("emit-causal-fixture: build first event: %w", err)
	}
	if _, err := writer.Append(first); err != nil {
		return fmt.Errorf("emit-causal-fixture: append first event: %w", err)
	}

	second, err := buildMessageAcceptedEvent(runID, streamID, "moltnet:fixture-m2", "fixture-m2", principalID, "fixture message 2", fixtureRecordedAt.Add(time.Second))
	if err != nil {
		return fmt.Errorf("emit-causal-fixture: build second event: %w", err)
	}
	if _, err := writer.Append(second); err != nil {
		return fmt.Errorf("emit-causal-fixture: append second event: %w", err)
	}

	return nil
}

func buildMessageAcceptedEvent(runID, streamID, eventID, messageID, principalID, text string, recordedAt time.Time) (protocol.CausalEvent, error) {
	payload, err := fixtureMessageAcceptedPayload(messageID, text)
	if err != nil {
		return protocol.CausalEvent{}, err
	}

	return protocol.CausalEvent{
		Version: protocol.CausalEventVersion,
		RunID:   runID,
		EventID: eventID,
		Emitter: protocol.CausalEmitter{
			System:   protocol.CausalSystemMoltnet,
			StreamID: streamID,
		},
		Type:          protocol.EventTypeMessageAccepted,
		PrincipalID:   principalID,
		RecordedAt:    recordedAt,
		CauseEventIDs: []string{},
		Payload:       payload,
	}, nil
}

// fixtureMessageAcceptedPayload mirrors rooms.messageAcceptedPayload
// (internal/rooms/causal.go): content_sha256 is hex(sha256(json.Marshal of
// the canonically-serialized message Parts)), never over raw text alone.
func fixtureMessageAcceptedPayload(messageID, text string) (json.RawMessage, error) {
	parts := []protocol.Part{{Kind: "text", Text: text}}
	partsJSON, err := json.Marshal(parts)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(partsJSON)

	payload := protocol.MessageAcceptedPayload{
		MessageID: messageID,
		Target: protocol.Target{
			Kind:   protocol.TargetKindRoom,
			RoomID: "fixture-room",
		},
		ContentSHA256:  hex.EncodeToString(sum[:]),
		PolicyDecision: protocol.PolicyDecisionAccepted,
	}
	return json.Marshal(payload)
}
