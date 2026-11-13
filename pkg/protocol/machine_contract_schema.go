package protocol

import (
	"fmt"
	"regexp"
)

var (
	machineOperationValues       = []string{MachineOpSendNudge, MachineOpRead, MachineOpSubscribe, MachineOpExport, MachineOpCancel}
	machineTargetKindValues      = []string{MachineTargetKindRoom, MachineTargetKindDM}
	machineErrorCodeValues       = []string{MachineErrorInvalidRequest, MachineErrorDuplicateRequest, MachineErrorUnsupported, MachineErrorNotFound, MachineErrorConflict, MachineErrorCapacity, MachineErrorTransport, MachineErrorCanceled}
	machineSubscribeReasonValues = []string{MachineSubscribeReasonDone, MachineSubscribeReasonLimit, MachineSubscribeReasonEOF}
	machineCancelStateValues     = []string{MachineCancelStateCanceled, MachineCancelStateAlreadyFinal, MachineCancelStateNotFound}
	machinePartKindValues        = []string{PartKindText, PartKindURL, PartKindData, PartKindFile, PartKindImage, PartKindAudio}
)

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func machineContractEnums() map[string][]string {
	return map[string][]string{
		"cancel_state":     cloneStrings(machineCancelStateValues),
		"error_code":       cloneStrings(machineErrorCodeValues),
		"message_target":   {TargetKindRoom, TargetKindDM},
		"operation":        cloneStrings(machineOperationValues),
		"part_kind":        cloneStrings(machinePartKindValues),
		"subscribe_closed": {MachineSubscribeClosed},
		"subscribe_reason": cloneStrings(machineSubscribeReasonValues),
		"target_kind":      cloneStrings(machineTargetKindValues),
	}
}

func machineContractGrammars() map[string]MachineContractGrammar {
	return map[string]MachineContractGrammar{
		"local_identifier": {
			Pattern:  "^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$",
			MinBytes: 1, MaxBytes: MaxMessageIDLength, Trimmed: true, LocalOnly: true,
		},
		"member_id": {
			MinBytes: 1, Trimmed: true,
			Formats: []string{"local_identifier", "network_id/agent_id", "molt://network_id/agents/agent_id"},
		},
		"lowercase_sha256": {Pattern: "^[0-9a-f]{64}$", MinBytes: 64, MaxBytes: 64},
		"url":              {Formats: []string{"http", "https", "molt"}},
	}
}

func machineContractLimits() map[string]int {
	return map[string]int{
		"max_active_requests":      MachineMaxActiveRequests,
		"max_body_bytes":           MachineMaxBodyBytes,
		"max_cause_bytes":          MachineMaxCauseBytes,
		"max_cause_event_ids":      MachineMaxCauseEventIDs,
		"max_correlation_bytes":    MachineMaxCorrelationBytes,
		"max_correlation_registry": MachineMaxCorrelationRegistry,
		"max_cursor_bytes":         MachineMaxCursorBytes,
		"max_delivery_bytes":       MachineMaxDeliveryBytes,
		"max_delivery_registry":    MachineMaxDeliveryRegistry,
		"max_export_peer_targets":  MachineMaxExportPeerTargets,
		"max_export_room_targets":  MachineMaxExportRoomTargets,
		"max_input_line_bytes":     MachineMaxInputLineBytes,
		"max_message_id_bytes":     MaxMessageIDLength,
		"max_output_line_bytes":    MachineMaxOutputLineBytes,
		"max_read_limit":           MachineMaxReadLimit,
		"max_read_mentions":        MachineMaxReadMentions,
		"max_read_message_parts":   MachineMaxReadMessageParts,
		"max_read_part_data_bytes": MachineMaxReadPartDataBytes,
		"max_read_part_filename":   MachineMaxReadPartFilename,
		"max_read_part_media_type": MachineMaxReadPartMediaType,
		"max_read_part_text_bytes": MachineMaxReadPartTextBytes,
		"max_read_part_url_bytes":  MachineMaxReadPartURLBytes,
		"max_subscribe_events":     MachineMaxSubscribeEvents,
		"max_target_bytes":         MachineMaxTargetBytes,
		"max_transcript_bytes":     MachineMaxTranscriptBytes,
	}
}

func contractField(name, kind string, required bool, limit string, enum ...string) MachineContractField {
	return MachineContractField{Name: name, Type: kind, Required: required, Limit: limit, Enum: enum}
}

func nullableContractField(name, kind string, required bool, limit string, enum ...string) MachineContractField {
	field := contractField(name, kind, required, limit, enum...)
	field.Nullable = true
	return field
}

func machineContractShapes() []MachineContractShape {
	f := contractField
	n := nullableContractField
	return []MachineContractShape{
		{Name: "request", Fields: []MachineContractField{
			f("version", "string", true, "", MachineProtocolV1),
			f("correlation_id", "identifier", true, "max_correlation_bytes"),
			f("operation", "string", true, "", MachineOpSendNudge, MachineOpRead, MachineOpSubscribe, MachineOpExport, MachineOpCancel),
			f("send_nudge", "send_nudge_request", false, ""), f("read", "read_request", false, ""),
			f("subscribe", "subscribe_request", false, ""), f("export", "export_request", false, ""),
			f("cancel", "cancel_request", false, ""),
		}, Relations: []MachineContractRelation{
			{Kind: "exactly_one", Fields: cloneStrings(machineOperationValues)},
			{Kind: "payload_key_equals_field", Fields: []string{"operation"}},
		}},
		{Name: "response", Fields: []MachineContractField{
			f("version", "string", true, "", MachineProtocolV1),
			f("correlation_id", "identifier", true, "max_correlation_bytes"),
			f("operation", "string", true, "", MachineOpSendNudge, MachineOpRead, MachineOpSubscribe, MachineOpExport, MachineOpCancel),
			f("send_nudge", "send_nudge_result", false, ""), f("read", "read_result", false, ""),
			f("subscribe", "subscribe_result", false, ""), f("export", "export_result", false, ""),
			f("cancel", "cancel_result", false, ""), f("event", "subscribe_event", false, ""),
			f("error", "error", false, ""),
		}, Relations: []MachineContractRelation{
			{Kind: "exactly_one", Fields: []string{"send_nudge", "read", "subscribe", "export", "cancel", "event", "error"}},
			{Kind: "result_key_equals_field", Fields: []string{"operation"}},
			{Kind: "field_allowed_when", Fields: []string{"event", "operation"}, Value: MachineOpSubscribe},
		}},
		{Name: "target", Fields: []MachineContractField{
			f("kind", "string", true, "", MachineTargetKindRoom, MachineTargetKindDM),
			f("id", "identifier", true, "max_target_bytes"),
		}},
		{Name: "send_nudge_request", Fields: []MachineContractField{
			f("delivery_id", "identifier", true, "max_delivery_bytes"), f("target", "target", true, ""),
			f("body", "non_blank_string", true, "max_body_bytes"),
			f("origin_message_id", "identifier", false, "max_cursor_bytes"),
			f("cause_event_ids", "unique_identifier_array", false, "max_cause_event_ids|max_cause_bytes"),
		}},
		{Name: "send_nudge_result", Fields: []MachineContractField{
			f("message_id", "identifier", true, "max_target_bytes"), f("event_id", "identifier", true, "max_target_bytes"),
			f("accepted", "boolean", true, ""), f("thread_id", "identifier", false, "max_target_bytes"),
			f("thread_created", "boolean", true, ""), f("dm_id", "identifier", false, "max_target_bytes"),
			f("dm_created", "boolean", true, ""),
		}, Relations: []MachineContractRelation{
			{Kind: "present_iff_true", Fields: []string{"thread_id", "thread_created"}},
			{Kind: "present_iff_true", Fields: []string{"dm_id", "dm_created"}},
		}},
		{Name: "read_request", Fields: []MachineContractField{
			f("target", "target", true, ""), f("limit", "integer", true, "1..max_read_limit"),
			f("before", "identifier", false, "max_cursor_bytes"), f("after", "identifier", false, "max_cursor_bytes"),
		}, Relations: []MachineContractRelation{{Kind: "mutually_exclusive", Fields: []string{"before", "after"}}}},
		{Name: "read_result", Fields: []MachineContractField{
			f("target", "target", true, ""), f("page", "read_page", true, ""),
		}},
		{Name: "read_page", Fields: []MachineContractField{
			n("messages", "read_message_array", true, "max_read_limit"), f("page", "read_page_info", true, ""),
		}},
		{Name: "read_page_info", Fields: []MachineContractField{
			f("has_more", "boolean", true, ""), f("next_before", "identifier", false, "max_cursor_bytes"),
			f("next_after", "identifier", false, "max_cursor_bytes"),
		}, Relations: []MachineContractRelation{
			{Kind: "exactly_one_when_true", Fields: []string{"next_before", "next_after", "has_more"}},
			{Kind: "absent_when_false", Fields: []string{"next_before", "next_after", "has_more"}},
		}},
		{Name: "read_message", Fields: []MachineContractField{
			f("wake", "message_wake", false, ""), f("reply_to", "identifier", false, "max_target_bytes"),
			f("discussion_id", "identifier", false, "max_target_bytes"),
			f("id", "identifier", true, "max_target_bytes"), f("network_id", "identifier", true, "max_target_bytes"),
			f("origin", "message_origin", true, ""), f("target", "message_target", true, ""),
			f("from", "message_actor", true, ""), f("parts", "message_part_array", true, "1..max_read_message_parts"),
			f("mentions", "member_id_array", false, "max_read_mentions"), f("created_at", "rfc3339_timestamp", true, ""),
		}},
		{Name: "message_wake", Fields: []MachineContractField{
			n("recipients", "member_id_array", true, "max_read_mentions"),
			f("discussion_id", "identifier", false, "max_target_bytes"),
			f("turn", "integer", false, ""), f("max_turns", "integer", false, ""),
		}},
		{Name: "message_origin", Fields: []MachineContractField{
			f("claimed_author", "message_actor", false, ""),
			f("claimed_network_id", "string", false, ""),
			f("claimed_message_id", "string", false, ""),
			f("claimed_discovery_network_id", "string", false, ""),
			f("network_id", "identifier", true, "max_target_bytes"), f("message_id", "identifier", true, "max_target_bytes"),
			f("received_via", "identifier", false, "max_target_bytes"),
		}},
		{Name: "message_target", Fields: []MachineContractField{
			f("kind", "string", true, "", TargetKindRoom, TargetKindDM),
			f("room_id", "room_id", false, "max_message_id_bytes"), f("thread_id", "string", false, ""),
			f("parent_message_id", "string", false, ""), f("dm_id", "message_id", false, "max_message_id_bytes"),
			f("participant_ids", "string_array", false, ""),
		}, Relations: []MachineContractRelation{
			{Kind: "kind_requires_only", Fields: []string{"kind", "room_id"}, Value: TargetKindRoom},
			{Kind: "kind_requires_only", Fields: []string{"kind", "dm_id"}, Value: TargetKindDM},
		}},
		{Name: "message_actor", Fields: []MachineContractField{
			f("type", "non_blank_string", true, "max_target_bytes"), f("id", "member_id", true, ""),
			f("name", "string", false, "max_target_bytes"), f("network_id", "identifier", false, "max_target_bytes"),
			f("fqid", "string", false, "max_target_bytes"), f("credential_bound", "boolean", false, ""),
		}},
		{Name: "message_part", Fields: []MachineContractField{
			f("kind", "string", true, "", PartKindText, PartKindURL, PartKindData, PartKindFile, PartKindImage, PartKindAudio),
			f("text", "string", false, "max_read_part_text_bytes"), f("media_type", "string", false, "max_read_part_media_type"),
			f("url", "http|https|molt_url", false, "max_read_part_url_bytes"),
			f("filename", "string", false, "max_read_part_filename"), f("data", "json_object", false, "max_read_part_data_bytes"),
		}},
		{Name: "subscribe_request", Fields: []MachineContractField{
			f("target", "target", true, ""), f("resume_cursor", "identifier", false, "max_cursor_bytes"),
			f("max_events", "integer", true, "1..max_subscribe_events"),
		}},
		{Name: "subscribe_result", Fields: []MachineContractField{
			f("closed", "string", true, "", MachineSubscribeClosed),
			f("reason", "string", true, "", MachineSubscribeReasonDone, MachineSubscribeReasonLimit, MachineSubscribeReasonEOF),
		}},
		{Name: "subscribe_event", Fields: []MachineContractField{
			f("event_id", "identifier", true, "max_target_bytes"), f("type", "non_blank_string", true, "max_target_bytes"),
			f("payload", "json_value", true, "max_output_line_bytes"),
		}},
		{Name: "export_request", Fields: []MachineContractField{
			f("room_ids", "unique_identifier_array", true, "max_export_room_targets|max_target_bytes"),
			f("dm_peer_ids", "unique_identifier_array", true, "max_export_peer_targets|max_target_bytes"),
			f("include_social_speech", "boolean", true, ""),
		}, Relations: []MachineContractRelation{{Kind: "at_least_one_nonempty", Fields: []string{"room_ids", "dm_peer_ids"}}}},
		{Name: "export_result", Fields: []MachineContractField{
			f("version", "string", true, "", MachineExportSchemaVersion),
			f("control_marker", "string", true, "", MachineExportMarker),
			f("transcript", "string", true, "max_transcript_bytes"), f("transcript_sha256", "lowercase_sha256", true, "64"),
		}, Relations: []MachineContractRelation{{Kind: "sha256_utf8_matches", Fields: []string{"transcript", "transcript_sha256"}}}},
		{Name: "cancel_request", Fields: []MachineContractField{
			f("target_correlation_id", "identifier", true, "max_correlation_bytes"),
		}},
		{Name: "cancel_result", Fields: []MachineContractField{
			f("target_correlation_id", "identifier", true, "max_correlation_bytes"),
			f("state", "string", true, "", MachineCancelStateCanceled, MachineCancelStateAlreadyFinal, MachineCancelStateNotFound),
		}},
		{Name: "error", Fields: []MachineContractField{
			f("code", "string", true, "", MachineErrorInvalidRequest, MachineErrorDuplicateRequest, MachineErrorUnsupported, MachineErrorNotFound, MachineErrorConflict, MachineErrorCapacity, MachineErrorTransport, MachineErrorCanceled),
		}},
	}
}

// machineContractShape publishes and admits wire envelopes from the declarations above.
func machineContractShape(name string) (MachineContractShape, bool) {
	for _, shape := range machineContractShapes() {
		if shape.Name == name {
			return shape, true
		}
	}
	return MachineContractShape{}, false
}

func machineContractEnvelopeFields(shapeName string) (map[string]MachineContractField, error) {
	shape, ok := machineContractShape(shapeName)
	if !ok {
		return nil, fmt.Errorf("unknown machine contract shape %q", shapeName)
	}
	fields := make(map[string]MachineContractField, len(shape.Fields))
	for _, field := range shape.Fields {
		fields[field.Name] = field
	}
	return fields, nil
}

func machineContractGrammar(name string) (MachineContractGrammar, bool) {
	grammar, ok := machineContractGrammarValues[name]
	return grammar, ok
}

var machineContractGrammarValues = machineContractGrammars()
var machineContractRegexes = func() map[string]*regexp.Regexp {
	patterns := make(map[string]*regexp.Regexp, len(machineContractGrammarValues))
	for name, grammar := range machineContractGrammarValues {
		if grammar.Pattern != "" {
			patterns[name] = regexp.MustCompile(grammar.Pattern)
		}
	}
	return patterns
}()

func machineContractGrammarMatches(name, value string) bool {
	return machineContractRegexes[name] != nil && machineContractRegexes[name].MatchString(value)
}
