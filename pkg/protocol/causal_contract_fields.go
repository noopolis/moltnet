package protocol

import (
	"fmt"
	"math"
	"strings"
)

func parseCausalEventID(raw string) (system string, local string, ok bool) {
	index := strings.Index(raw, ":")
	if index <= 0 || index == len(raw)-1 {
		return "", "", false
	}
	system = raw[:index]
	if !causalSystemRecognized(system) {
		return "", "", false
	}
	return system, raw[index+1:], true
}

// parseCausalCauseID splits a cause id at the first colon. Per B169 D4 a
// cause id is RECONCILIATION content with an OPEN namespace: foreign
// namespaces are legal and must not be checked against
// causalSystemRecognized.
func parseCausalCauseID(raw string) (namespace string, local string, ok bool) {
	index := strings.Index(raw, ":")
	if index <= 0 || index == len(raw)-1 {
		return "", "", false
	}
	return raw[:index], raw[index+1:], true
}

func asObject(value any) (map[string]any, error) {
	record, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object")
	}
	if record == nil {
		return nil, fmt.Errorf("must be a non-null object")
	}
	return record, nil
}

func asStringField(record map[string]any, name string) (string, error) {
	raw, ok := record[name]
	if !ok {
		return "", fmt.Errorf("missing field %q", name)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string", name)
	}
	return value, nil
}

func asObjectField(record map[string]any, name string) (map[string]any, error) {
	raw, ok := record[name]
	if !ok {
		return nil, fmt.Errorf("missing field %q", name)
	}
	return asObject(raw)
}

func asSafeInt(value any, field string, min int) (int, error) {
	switch typed := value.(type) {
	case int:
		return asSafeIntFromSignedFloat64(field, float64(typed), min)
	case int8:
		return asSafeIntFromSignedFloat64(field, float64(typed), min)
	case int16:
		return asSafeIntFromSignedFloat64(field, float64(typed), min)
	case int32:
		return asSafeIntFromSignedFloat64(field, float64(typed), min)
	case int64:
		return asSafeIntFromSignedFloat64(field, float64(typed), min)
	case uint:
		return asSafeIntFromUnsignedFloat64(field, float64(typed), min)
	case uint8:
		return asSafeIntFromUnsignedFloat64(field, float64(typed), min)
	case uint16:
		return asSafeIntFromUnsignedFloat64(field, float64(typed), min)
	case uint32:
		return asSafeIntFromUnsignedFloat64(field, float64(typed), min)
	case uint64:
		return asSafeIntFromUnsignedFloat64(field, float64(typed), min)
	case float64:
		return asSafeIntFromSignedFloat64(field, typed, min)
	default:
		return 0, fmt.Errorf("%s must be a number", field)
	}
}

func asSafeIntFromSignedFloat64(field string, parsed float64, min int) (int, error) {
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%s must be a safe integer", field)
	}
	if parsed == 0 && math.Signbit(parsed) {
		return 0, fmt.Errorf("%s must be a safe integer", field)
	}
	if parsed < float64(min) || math.Trunc(parsed) != parsed {
		return 0, fmt.Errorf("%s must be a safe integer", field)
	}
	if parsed < float64(min) || parsed > maxSafeInteger {
		return 0, fmt.Errorf("%s must be a safe integer", field)
	}
	result := int(parsed)
	if float64(result) != parsed {
		return 0, fmt.Errorf("%s must be a safe integer", field)
	}
	return result, nil
}

func asSafeIntFromUnsignedFloat64(field string, parsed float64, min int) (int, error) {
	if parsed < 0 {
		return 0, fmt.Errorf("%s must be a safe integer", field)
	}
	return asSafeIntFromSignedFloat64(field, parsed, min)
}

func asStringSliceField(record map[string]any, name string) ([]string, error) {
	raw, ok := record[name]
	if !ok {
		return nil, fmt.Errorf("missing field %q", name)
	}
	array, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("field %q must be an array", name)
	}
	values := make([]string, 0, len(array))
	for index, item := range array {
		stringValue, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("field %q[%d] must be a string", name, index)
		}
		values = append(values, stringValue)
	}
	return values, nil
}
