package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func validateMachineContractRelations(envelope map[string]json.RawMessage, relations []MachineContractRelation) error {
	for _, relation := range relations {
		present := func(name string) bool { value, ok := envelope[name]; return ok && !isNull(value) }
		switch relation.Kind {
		case "exactly_one":
			count := 0
			for _, field := range relation.Fields {
				if present(field) {
					count++
				}
			}
			if count != 1 {
				return fmt.Errorf("exactly one field required")
			}
		case "mutually_exclusive":
			count := 0
			for _, field := range relation.Fields {
				if present(field) {
					count++
				}
			}
			if count > 1 {
				return fmt.Errorf("mutually exclusive fields")
			}
		case "payload_key_equals_field", "result_key_equals_field":
			if len(relation.Fields) != 1 {
				return fmt.Errorf("invalid relation")
			}
			var value string
			if raw, ok := envelope[relation.Fields[0]]; !ok || decodeStrictJSONValue(raw, &value) != nil {
				return fmt.Errorf("missing relation field")
			}
			if relation.Kind == "result_key_equals_field" && (present("error") || present("event")) {
				continue
			}
			if !present(value) {
				return fmt.Errorf("payload mismatch")
			}
		case "field_allowed_when":
			if len(relation.Fields) != 2 {
				return fmt.Errorf("invalid relation")
			}
			if present(relation.Fields[0]) {
				var value string
				if raw, ok := envelope[relation.Fields[1]]; !ok || decodeStrictJSONValue(raw, &value) != nil || value != relation.Value {
					return fmt.Errorf("field not allowed")
				}
			}
		case "present_iff_true":
			if len(relation.Fields) != 2 {
				return fmt.Errorf("invalid relation")
			}
			var enabled bool
			raw, ok := envelope[relation.Fields[1]]
			if !ok || decodeStrictJSONValue(raw, &enabled) != nil || present(relation.Fields[0]) != enabled {
				return fmt.Errorf("presence relation")
			}
		case "exactly_one_when_true":
			if len(relation.Fields) != 3 {
				return fmt.Errorf("invalid relation")
			}
			var enabled bool
			raw, ok := envelope[relation.Fields[2]]
			if !ok || decodeStrictJSONValue(raw, &enabled) != nil {
				return fmt.Errorf("invalid condition")
			}
			if enabled {
				count := 0
				for _, field := range relation.Fields[:2] {
					if present(field) {
						count++
					}
				}
				if count != 1 {
					return fmt.Errorf("conditional exactly one")
				}
			}
		case "absent_when_false":
			if len(relation.Fields) != 3 {
				return fmt.Errorf("invalid relation")
			}
			var enabled bool
			raw, ok := envelope[relation.Fields[2]]
			if !ok || decodeStrictJSONValue(raw, &enabled) != nil {
				return fmt.Errorf("invalid condition")
			}
			if !enabled && (present(relation.Fields[0]) || present(relation.Fields[1])) {
				return fmt.Errorf("conditional absence")
			}
		case "at_least_one_nonempty":
			any := false
			for _, field := range relation.Fields {
				var values []json.RawMessage
				if raw, ok := envelope[field]; ok && decodeStrictJSONValue(raw, &values) == nil && len(values) > 0 {
					any = true
				}
			}
			if !any {
				return fmt.Errorf("at least one nonempty field")
			}
		case "sha256_utf8_matches":
			if len(relation.Fields) != 2 {
				return fmt.Errorf("invalid relation")
			}
			var text, digest string
			if decodeStrictJSONValue(envelope[relation.Fields[0]], &text) != nil || decodeStrictJSONValue(envelope[relation.Fields[1]], &digest) != nil {
				return fmt.Errorf("invalid digest relation")
			}
			sum := sha256.Sum256([]byte(text))
			if digest != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("digest mismatch")
			}
		case "kind_requires_only":
			if len(relation.Fields) != 2 {
				return fmt.Errorf("invalid relation")
			}
			var kind string
			if raw, ok := envelope[relation.Fields[0]]; !ok || decodeStrictJSONValue(raw, &kind) != nil {
				return fmt.Errorf("invalid kind relation")
			}
			if kind == relation.Value && !present(relation.Fields[1]) {
				return fmt.Errorf("kind required field missing")
			}
		default:
			return fmt.Errorf("unknown relation %q", relation.Kind)
		}
	}
	return nil
}

func validateMachineContractField(raw json.RawMessage, field MachineContractField) error {
	if _, ok := machineContractShape(field.Type); ok {
		return validateMachineContractShape(raw, field.Type)
	}
	var text string
	switch field.Type {
	case "boolean":
		var value bool
		return decodeStrictJSONValue(raw, &value)
	case "integer":
		var value int
		if err := decodeStrictJSONValue(raw, &value); err != nil {
			return err
		}
		return validateMachineContractInteger(value, field.Limit)
	case "json_object":
		_, err := decodeJSONEnvelope(string(raw))
		return err
	case "json_value":
		return validateNoDuplicateJSONValues(raw)
	case "unique_identifier_array", "member_id_array", "string_array", "read_message_array", "message_part_array":
		return validateMachineContractArray(raw, field)
	case "string", "identifier", "member_id", "non_blank_string", "lowercase_sha256", "rfc3339_timestamp", "http|https|molt_url", "room_id", "message_id":
		if err := decodeStrictJSONValue(raw, &text); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown scalar type %q", field.Type)
	}
	if err := validateMachineContractString(text, field); err != nil {
		return err
	}
	if len(field.Enum) > 0 && !machineContractEnumSliceContains(field.Enum, text) {
		return fmt.Errorf("enum mismatch")
	}
	return nil
}

func validateMachineContractString(value string, field MachineContractField) error {
	if err := validateMachineContractByteLimit(value, field.Limit); err != nil {
		return err
	}
	switch field.Type {
	case "identifier", "room_id", "message_id":
		return validateMachineIdentifier(value, field.Name, machineContractLimit(field.Limit))
	case "member_id":
		return ValidateMemberID(value)
	case "non_blank_string":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("blank string")
		}
	case "lowercase_sha256":
		grammar, _ := machineContractGrammar("lowercase_sha256")
		if len(value) < grammar.MinBytes || grammar.MaxBytes > 0 && len(value) > grammar.MaxBytes || !machineContractPatternMatches(grammar.Pattern, value) {
			return fmt.Errorf("invalid sha256")
		}
	case "rfc3339_timestamp":
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return err
		}
	case "http|https|molt_url":
		return validateMachineReadPartURL(value)
	}
	return nil
}
