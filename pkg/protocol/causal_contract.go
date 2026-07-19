package protocol

import (
	"fmt"
	"time"
)

const CausalStreamFinalVersion = "noopolis.causal-stream-final.v1"
const CausalDigestDomainVersion = "noopolis.causal-digest-domain.v1"
const CausalDigestLabelCanonicalEvent = "causal-event/canonical-json"
const CausalDigestLabelExactUTF8 = "content/exact-utf8"
const CausalDigestLabelExactBytes = "content/exact-bytes"
const causalContractCanonicalJSONVersion = "noopolis.canonical-json.v1:utf-8"

var recognizedDigestLabels = map[string]struct{}{
	CausalDigestLabelCanonicalEvent: {},
	CausalDigestLabelExactUTF8:      {},
	CausalDigestLabelExactBytes:     {},
}

// CausalStreamFinal is the stream-final variant of the causal contract.
type CausalStreamFinal struct {
	Version  string                   `json:"version"`
	RunID    string                   `json:"run_id"`
	Emitter  CausalStreamFinalEmitter `json:"emitter"`
	FinalSeq int                      `json:"final_seq"`
}

// CausalDigestDomain declares how a causal subject should be hashed.
type CausalDigestDomain struct {
	Version      string `json:"version"`
	Label        string `json:"label"`
	Hash         string `json:"hash"`
	SubjectBytes string `json:"subject_bytes"`
	Output       string `json:"output"`
}

type CausalStreamFinalEmitter struct {
	System   string `json:"system"`
	StreamID string `json:"stream_id"`
}

func ParseCausalEvent(value any) (CausalEvent, error) {
	record, err := asObject(value)
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: %w", err)
	}
	if len(record) != 9 {
		return CausalEvent{}, fmt.Errorf("invalid causal event: expected 9 fields, got %d", len(record))
	}

	version, err := asStringField(record, "version")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: version: %w", err)
	}
	if version != CausalEventVersion {
		return CausalEvent{}, fmt.Errorf("invalid causal event: version must be %q", CausalEventVersion)
	}
	runID, err := asStringField(record, "run_id")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: run_id: %w", err)
	}
	if runID == "" {
		return CausalEvent{}, fmt.Errorf("invalid causal event: run_id must be non-empty")
	}
	eventID, err := asStringField(record, "event_id")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: event_id: %w", err)
	}
	eventSystem, _, ok := parseCausalEventID(eventID)
	if !ok {
		return CausalEvent{}, fmt.Errorf("invalid causal event: event_id: %q", eventID)
	}
	etype, err := asStringField(record, "type")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: type: %w", err)
	}
	if etype == "" {
		return CausalEvent{}, fmt.Errorf("invalid causal event: type is required")
	}
	principal, err := asStringField(record, "principal_id")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: principal_id: %w", err)
	}
	recordedAtValue, err := asStringField(record, "recorded_at")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: recorded_at: %w", err)
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, recordedAtValue)
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: recorded_at: %w", err)
	}
	payload, err := asObjectField(record, "payload")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: payload: %w", err)
	}
	payloadBytes, err := CanonicalJSONBytes(payload)
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: payload: %w", err)
	}
	causeEventIDs, err := asStringSliceField(record, "cause_event_ids")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: cause_event_ids: %w", err)
	}
	seenCause := map[string]struct{}{}
	for index, causeID := range causeEventIDs {
		if _, exists := seenCause[causeID]; exists {
			return CausalEvent{}, fmt.Errorf("invalid causal event: duplicate cause_event_ids entry %q", causeID)
		}
		seenCause[causeID] = struct{}{}
		if _, _, ok := parseCausalEventID(causeID); !ok {
			return CausalEvent{}, fmt.Errorf("invalid causal event: cause_event_ids[%d]: %q", index, causeID)
		}
	}
	emitter, err := asObjectField(record, "emitter")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: emitter: %w", err)
	}
	if len(emitter) != 3 {
		return CausalEvent{}, fmt.Errorf("invalid causal event: emitter has invalid fields")
	}
	system, err := asStringField(emitter, "system")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: emitter.system: %w", err)
	}
	if !causalSystemRecognized(system) {
		return CausalEvent{}, fmt.Errorf("invalid causal event: emitter.system %q is not a recognized Noopolis authority", system)
	}
	streamID, err := asStringField(emitter, "stream_id")
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: emitter.stream_id: %w", err)
	}
	if streamID == "" {
		return CausalEvent{}, fmt.Errorf("invalid causal event: emitter.stream_id is required")
	}
	seq, err := asSafeInt(emitter["seq"], "emitter.seq", 1)
	if err != nil {
		return CausalEvent{}, fmt.Errorf("invalid causal event: %w", err)
	}
	event := CausalEvent{
		Version:       version,
		RunID:         runID,
		EventID:       eventID,
		Emitter:       CausalEmitter{System: system, StreamID: streamID, Seq: seq},
		Type:          etype,
		PrincipalID:   principal,
		RecordedAt:    recordedAt,
		CauseEventIDs: causeEventIDs,
		Payload:       payloadBytes,
	}
	if eventSystem != event.Emitter.System {
		return CausalEvent{}, fmt.Errorf("invalid causal event: event_id system prefix does not match emitter.system")
	}
	if !causalPrincipalGrammar.MatchString(event.PrincipalID) {
		return CausalEvent{}, fmt.Errorf("invalid causal event: principal_id must match %s", causalPrincipalGrammar.String())
	}
	return event, nil
}

func ParseCausalStreamFinal(value any) (CausalStreamFinal, error) {
	record, err := asObject(value)
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: %w", err)
	}
	if len(record) != 4 {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: expected 4 fields, got %d", len(record))
	}
	version, err := asStringField(record, "version")
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: version: %w", err)
	}
	if version != CausalStreamFinalVersion {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: version must be %q", CausalStreamFinalVersion)
	}
	runID, err := asStringField(record, "run_id")
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: run_id: %w", err)
	}
	if runID == "" {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: run_id must be non-empty")
	}
	finalSeq, err := asSafeInt(record["final_seq"], "final_seq", 0)
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: final_seq: %w", err)
	}
	emitter, err := asObjectField(record, "emitter")
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: emitter: %w", err)
	}
	if len(emitter) != 2 {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: emitter has invalid fields")
	}
	system, err := asStringField(emitter, "system")
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: emitter.system: %w", err)
	}
	if !causalSystemRecognized(system) {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: emitter.system %q is not a recognized Noopolis authority", system)
	}
	streamID, err := asStringField(emitter, "stream_id")
	if err != nil {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: emitter.stream_id: %w", err)
	}
	if streamID == "" {
		return CausalStreamFinal{}, fmt.Errorf("invalid stream final: emitter.stream_id is required")
	}
	return CausalStreamFinal{
		Version:  version,
		RunID:    runID,
		FinalSeq: finalSeq,
		Emitter:  CausalStreamFinalEmitter{System: system, StreamID: streamID},
	}, nil
}

func ParseCausalDigestDomain(value any) (CausalDigestDomain, error) {
	record, err := asObject(value)
	if err != nil {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: %w", err)
	}
	if len(record) != 5 {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: expected 5 fields, got %d", len(record))
	}
	version, err := asStringField(record, "version")
	if err != nil {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: version: %w", err)
	}
	if version != CausalDigestDomainVersion {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: version must be %q", CausalDigestDomainVersion)
	}
	label, err := asStringField(record, "label")
	if err != nil {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: label: %w", err)
	}
	if _, ok := recognizedDigestLabels[label]; !ok {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: unrecognized label %q", label)
	}
	hash, err := asStringField(record, "hash")
	if err != nil {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: hash: %w", err)
	}
	if hash != "sha-256" {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: unsupported hash %q", hash)
	}
	subjectBytes, err := asStringField(record, "subject_bytes")
	if err != nil {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: subject_bytes: %w", err)
	}
	output, err := asStringField(record, "output")
	if err != nil {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: output: %w", err)
	}
	if output != "lowercase-hex" {
		return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: unsupported output %q", output)
	}
	switch label {
	case CausalDigestLabelCanonicalEvent:
		if subjectBytes != causalContractCanonicalJSONVersion {
			return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: subject_bytes must be %q", causalContractCanonicalJSONVersion)
		}
	case CausalDigestLabelExactUTF8:
		if subjectBytes != "exact-utf8" {
			return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: subject_bytes must be exact-utf8")
		}
	case CausalDigestLabelExactBytes:
		if subjectBytes != "exact-bytes" {
			return CausalDigestDomain{}, fmt.Errorf("invalid digest domain: subject_bytes must be exact-bytes")
		}
	}
	return CausalDigestDomain{
		Version:      version,
		Label:        label,
		Hash:         hash,
		SubjectBytes: subjectBytes,
		Output:       output,
	}, nil
}
