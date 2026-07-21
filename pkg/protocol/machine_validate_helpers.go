package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validateMachineOperation(operation string) error {
	switch operation {
	case MachineOpSendNudge, MachineOpRead, MachineOpSubscribe, MachineOpExport, MachineOpCancel:
		return nil
	default:
		return fmt.Errorf("unsupported operation %q", operation)
	}
}

func validateMachineIdentifier(value string, field string, max int) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not include leading or trailing whitespace", field)
	}
	if len(value) > max {
		return fmt.Errorf("%s must be at most %d bytes", field, max)
	}
	if _, _, ok := ParseScopedAgentID(value); ok {
		return fmt.Errorf("%s must be a local id", field)
	}
	if _, _, ok := ParseAgentFQID(value); ok {
		return fmt.Errorf("%s must be a local id", field)
	}
	if err := ValidateMessageID(value); err != nil {
		return fmt.Errorf("%s %w", field, err)
	}
	return nil
}

func validateMachineStringSlice(values []string, maxCount int, maxLen int, field string) error {
	if len(values) == 0 {
		return nil
	}
	if len(values) > maxCount {
		return fmt.Errorf("%s must contain at most %d entries", field, maxCount)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateMachineIdentifier(value, fmt.Sprintf("%s[%d]", field, index), maxLen); err != nil {
			return err
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s must contain unique values", field)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateJSON(raw json.RawMessage) error {
	var marker any
	return json.Unmarshal(raw, &marker)
}
