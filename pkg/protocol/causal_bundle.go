package protocol

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// CausalBundleParseError reports one line-local parse or validation error.
type CausalBundleParseError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// CausalBundleParseResult is the normalized output of ParseCausalBundle.
type CausalBundleParseResult struct {
	DigestDomains []CausalDigestDomain
	Errors        []CausalBundleParseError
	Events        []CausalEvent
	StreamFinals  []CausalStreamFinal
}

type streamSlot struct {
	runID    string
	system   string
	streamID string
}

type eventSlot struct {
	streamSlot
	seq int
}

func ParseCausalBundle(jsonl string) CausalBundleParseResult {
	result := CausalBundleParseResult{
		DigestDomains: []CausalDigestDomain{},
		Errors:        []CausalBundleParseError{},
		Events:        []CausalEvent{},
		StreamFinals:  []CausalStreamFinal{},
	}
	eventByID := map[string]CausalEvent{}
	eventLineByID := map[string]int{}
	finalBySlot := map[streamSlot]CausalStreamFinal{}
	finalLineBySlot := map[streamSlot]int{}
	observedSlots := map[eventSlot]struct{}{}
	maxObservedSeq := map[streamSlot]int{}

	lines := strings.Split(jsonl, "\n")
	for lineIndex, rawLine := range lines {
		lineNumber := lineIndex + 1
		trimmed := trimJsonWhitespace(rawLine)
		if trimmed == "" {
			continue
		}
		record, err := ParseCanonicalJSON(trimmed)
		if err != nil {
			result.Errors = append(result.Errors, CausalBundleParseError{
				Line:    lineNumber,
				Message: fmt.Sprintf("invalid JSON: %s", err),
			})
			continue
		}
		recordKind, kindErr := classifyCausalRecord(record)
		if kindErr != nil {
			result.Errors = append(result.Errors, CausalBundleParseError{
				Line:    lineNumber,
				Message: kindErr.Error(),
			})
			continue
		}

		switch recordKind {
		case "causal-event":
			event, err := ParseCausalEvent(record)
			if err != nil {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    lineNumber,
					Message: err.Error(),
				})
				continue
			}
			if _, exists := eventByID[event.EventID]; !exists {
				eventByID[event.EventID] = event
				eventLineByID[event.EventID] = lineNumber
			} else {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    lineNumber,
					Message: fmt.Sprintf("duplicate event_id: %s", event.EventID),
				})
			}
			slot := eventSlot{
				streamSlot: streamSlot{
					runID:    event.RunID,
					system:   event.Emitter.System,
					streamID: event.Emitter.StreamID,
				},
				seq: event.Emitter.Seq,
			}
			maxObservedSeq[slot.streamSlot] = max(maxObservedSeq[slot.streamSlot], event.Emitter.Seq)
			if _, exists := observedSlots[slot]; exists {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    lineNumber,
					Message: fmt.Sprintf("duplicate event slot: %s", streamSlotLabel(slot.streamSlot, slot.seq)),
				})
			} else {
				observedSlots[slot] = struct{}{}
			}
			result.Events = append(result.Events, event)
		case "causal-stream-final":
			streamFinal, err := ParseCausalStreamFinal(record)
			if err != nil {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    lineNumber,
					Message: err.Error(),
				})
				continue
			}
			slot := streamSlot{
				runID:    streamFinal.RunID,
				system:   streamFinal.Emitter.System,
				streamID: streamFinal.Emitter.StreamID,
			}
			if _, exists := finalLineBySlot[slot]; exists {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    lineNumber,
					Message: fmt.Sprintf("duplicate stream final for %s", streamPairLabel(slot)),
				})
			} else {
				finalBySlot[slot] = streamFinal
				finalLineBySlot[slot] = lineNumber
			}
			result.StreamFinals = append(result.StreamFinals, streamFinal)
		case "causal-digest-domain":
			domain, err := ParseCausalDigestDomain(record)
			if err != nil {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    lineNumber,
					Message: err.Error(),
				})
				continue
			}
			result.DigestDomains = append(result.DigestDomains, domain)
		}
	}

	for _, event := range result.Events {
		line := eventLineByID[event.EventID]
		for _, causeID := range event.CauseEventIDs {
			cause, ok := eventByID[causeID]
			if !ok {
				continue
			}
			if cause.RunID != event.RunID {
				result.Errors = append(result.Errors, CausalBundleParseError{
					Line:    line,
					Message: fmt.Sprintf("cause %s for event %s belongs to another run", causeID, event.EventID),
				})
			}
		}
	}

	keys := make([]streamSlot, 0, len(maxObservedSeq))
	for slot := range maxObservedSeq {
		keys = append(keys, slot)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].runID != keys[j].runID {
			return keys[i].runID < keys[j].runID
		}
		if keys[i].system != keys[j].system {
			return keys[i].system < keys[j].system
		}
		return keys[i].streamID < keys[j].streamID
	})
	for _, slot := range keys {
		streamFinal, hasFinal := finalBySlot[slot]
		line := finalLineBySlot[slot]
		if !hasFinal {
			continue
		}
		maxSeq := maxObservedSeq[slot]
		if streamFinal.FinalSeq < maxSeq {
			result.Errors = append(result.Errors, CausalBundleParseError{
				Line:    line,
				Message: fmt.Sprintf("stream final for %s is below observed sequence %d", streamPairLabel(slot), maxSeq),
			})
		}
		if streamFinal.FinalSeq == 0 && maxSeq > 0 {
			result.Errors = append(result.Errors, CausalBundleParseError{
				Line:    line,
				Message: fmt.Sprintf("stream final for %s has final_seq 0 but stream has observed events", streamPairLabel(slot)),
			})
		}
	}
	return result
}

func ParseCausalBundleBytes(jsonl []byte) CausalBundleParseResult {
	if hasBOM(jsonl) {
		return invalidBundleBytesResult("invalid JSON: leading BOM is not allowed")
	}
	if !utf8.Valid(jsonl) {
		return invalidBundleBytesResult("invalid JSON: invalid UTF-8: malformed UTF-8 sequence")
	}
	return ParseCausalBundle(string(jsonl))
}

func invalidBundleBytesResult(message string) CausalBundleParseResult {
	return CausalBundleParseResult{
		Errors: []CausalBundleParseError{{
			Line:    1,
			Message: message,
		}},
		DigestDomains: []CausalDigestDomain{},
		Events:        []CausalEvent{},
		StreamFinals:  []CausalStreamFinal{},
	}
}

func classifyCausalRecord(record any) (string, error) {
	object, err := asObject(record)
	if err != nil {
		return "", fmt.Errorf("invalid record: unknown version or malformed record shape")
	}
	version, ok := object["version"]
	if !ok {
		return "", fmt.Errorf("invalid record: unknown version or malformed record shape")
	}
	versionString, ok := version.(string)
	if !ok {
		return "", fmt.Errorf("invalid record: unknown version or malformed record shape")
	}
	switch versionString {
	case CausalEventVersion:
		return "causal-event", nil
	case CausalStreamFinalVersion:
		return "causal-stream-final", nil
	case CausalDigestDomainVersion:
		return "causal-digest-domain", nil
	default:
		return "", fmt.Errorf("invalid record: unknown version or malformed record shape")
	}
}

func streamPairLabel(slot streamSlot) string {
	return fmt.Sprintf("%s::%s:%s", slot.runID, slot.system, slot.streamID)
}

func streamSlotLabel(slot streamSlot, seq int) string {
	return fmt.Sprintf("%s:%d", streamPairLabel(slot), seq)
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
