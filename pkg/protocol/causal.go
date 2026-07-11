package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CausalEventVersion is the literal envelope version required by
// specs/causal-event.v1.schema.json (root/specs, canonical, not vendored here).
const CausalEventVersion = "noopolis.causal-event.v1"

// Recognized Noopolis authorities. Moltnet only ever emits CausalSystemMoltnet,
// but Validate accepts the full shared enum since the envelope is a
// cross-repo contract.
const (
	CausalSystemSimfile = "simfile"
	CausalSystemMoltnet = "moltnet"
	CausalSystemMneme   = "mneme"
	CausalSystemDaimon  = "daimon"
)

// EventTypeMessageAccepted is the causal event type stamped once a message
// has durably landed in a Moltnet network.
const EventTypeMessageAccepted = "message.accepted"

// EventTypeMessageDenied is the causal event type stamped when a message is
// rejected by room write policy (canWriteRoom returning false). Deny paths
// must always be ledger-visible, never a silent drop.
const EventTypeMessageDenied = "message.denied"

// PolicyDecisionAccepted is stamped in MessageAcceptedPayload.PolicyDecision:
// message.accepted causal events are only emitted once a message has already
// cleared write policy and been durably appended.
const PolicyDecisionAccepted = "accepted"

// PolicyDecisionDenied is stamped in MessageDeniedPayload.PolicyDecision for
// message.denied causal events.
const PolicyDecisionDenied = "denied"

// CausalEmitter identifies the authority, stream, and position that produced
// a CausalEvent. Seq is 1-based and contiguous within (run_id, stream_id).
type CausalEmitter struct {
	System   string `json:"system"`
	StreamID string `json:"stream_id"`
	Seq      int    `json:"seq"`
}

// CausalEvent is Moltnet's native Go shape for the canonical
// noopolis.causal-event.v1 wire envelope. It intentionally mirrors the JSON
// Schema field-for-field without importing it: the wire JSON is the
// contract, this struct is Moltnet's own copy (per repo AGENTS.md: prefer
// stdlib, no jsonschema dependency).
type CausalEvent struct {
	Version       string          `json:"version"`
	RunID         string          `json:"run_id"`
	EventID       string          `json:"event_id"`
	Emitter       CausalEmitter   `json:"emitter"`
	Type          string          `json:"type"`
	PrincipalID   string          `json:"principal_id"`
	RecordedAt    time.Time       `json:"recorded_at"`
	CauseEventIDs []string        `json:"cause_event_ids"`
	Payload       json.RawMessage `json:"payload"`
}

// MessageAcceptedPayload is the payload shape for EventTypeMessageAccepted.
type MessageAcceptedPayload struct {
	MessageID      string `json:"message_id"`
	Target         Target `json:"target"`
	ContentSHA256  string `json:"content_sha256"`
	PolicyDecision string `json:"policy_decision"`
}

// MessageDeniedPayload is the payload shape for EventTypeMessageDenied.
type MessageDeniedPayload struct {
	Target         Target `json:"target"`
	Reason         string `json:"reason"`
	ContentSHA256  string `json:"content_sha256"`
	PolicyDecision string `json:"policy_decision"`
}

// Validate checks the envelope-level invariants shared by every Noopolis
// authority (specs/causal-event.v1.schema.json). It does not check
// event-type-specific payload minimums; per specs/CAUSAL.md, those are the
// conformance harness's responsibility, not the envelope's.
func (e CausalEvent) Validate() error {
	if strings.TrimSpace(e.Version) != CausalEventVersion {
		return fmt.Errorf("causal event: version must be %q", CausalEventVersion)
	}
	if strings.TrimSpace(e.RunID) == "" {
		return fmt.Errorf("causal event: run_id is required")
	}
	system, _, ok := splitCausalEventID(e.EventID)
	if !ok {
		return fmt.Errorf("causal event: event_id must be of the form <system>:<local>")
	}
	if !causalSystemRecognized(e.Emitter.System) {
		return fmt.Errorf("causal event: emitter.system %q is not a recognized Noopolis authority", e.Emitter.System)
	}
	if system != e.Emitter.System {
		return fmt.Errorf("causal event: event_id system prefix %q must match emitter.system %q", system, e.Emitter.System)
	}
	if strings.TrimSpace(e.Emitter.StreamID) == "" {
		return fmt.Errorf("causal event: emitter.stream_id is required")
	}
	if e.Emitter.Seq < 1 {
		return fmt.Errorf("causal event: emitter.seq must be >= 1")
	}
	if strings.TrimSpace(e.Type) == "" {
		return fmt.Errorf("causal event: type is required")
	}
	if strings.TrimSpace(e.PrincipalID) == "" {
		return fmt.Errorf("causal event: principal_id is required")
	}
	if e.RecordedAt.IsZero() {
		return fmt.Errorf("causal event: recorded_at is required")
	}
	if e.CauseEventIDs == nil {
		return fmt.Errorf("causal event: cause_event_ids must not be nil (use an empty slice for no causes)")
	}
	for index, causeID := range e.CauseEventIDs {
		if strings.TrimSpace(causeID) == "" {
			return fmt.Errorf("causal event: cause_event_ids[%d] must not be empty", index)
		}
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("causal event: payload is required")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &probe); err != nil {
		return fmt.Errorf("causal event: payload must be a JSON object: %w", err)
	}
	return nil
}

// splitCausalEventID splits an event_id of the form <system>:<local>,
// matching schema pattern ^[^:]+:.+$: a non-empty, colon-free system prefix,
// then a colon, then one or more remaining characters.
func splitCausalEventID(eventID string) (system string, local string, ok bool) {
	index := strings.Index(eventID, ":")
	if index <= 0 || index == len(eventID)-1 {
		return "", "", false
	}
	return eventID[:index], eventID[index+1:], true
}

// MessageEventID formats messageID as moltnet's causal event id for that
// message: "<moltnet>:<messageID>", per the event_id grammar in
// specs/causal-event.v1.schema.json (system prefix, colon, local id).
// Hoisted here so every moltnet-authority "moltnet:" + id construction
// (internal/rooms/causal.go's message.accepted/message.denied stamps, and
// the bridge's control.go/control_delivery.go causal wiring) goes through
// one place instead of each hand-rolling the concatenation.
func MessageEventID(messageID string) string {
	return CausalSystemMoltnet + ":" + messageID
}

func causalSystemRecognized(system string) bool {
	switch system {
	case CausalSystemSimfile, CausalSystemMoltnet, CausalSystemMneme, CausalSystemDaimon:
		return true
	default:
		return false
	}
}
