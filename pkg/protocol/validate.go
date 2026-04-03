package protocol

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const MaxMessageIDLength = 128
const MaxMembersPerRequest = 256

var messageIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func ValidateSendMessageRequest(request SendMessageRequest) error {
	if err := ValidateMemberID(strings.TrimSpace(request.From.ID)); err != nil {
		if strings.TrimSpace(request.From.ID) == "" {
			return fmt.Errorf("from.id is required")
		}
		return fmt.Errorf("from.id %w", err)
	}
	if len(request.Parts) == 0 {
		return fmt.Errorf("parts must not be empty")
	}
	for index, part := range request.Parts {
		if err := ValidatePart(part); err != nil {
			return fmt.Errorf("parts[%d]: %w", index, err)
		}
	}
	if messageID := strings.TrimSpace(request.ID); messageID != "" {
		if err := ValidateMessageID(messageID); err != nil {
			return err
		}
	}
	if err := ValidateTarget(request.Target); err != nil {
		return err
	}

	return nil
}

func ValidateCreateRoomRequest(request CreateRoomRequest) error {
	if err := ValidateRoomID(strings.TrimSpace(request.ID)); err != nil {
		return err
	}
	if err := validateMembers("members", request.Members); err != nil {
		return err
	}
	return nil
}

func ValidateUpdateRoomMembersRequest(request UpdateRoomMembersRequest) error {
	if err := validateMembers("add", request.Add); err != nil {
		return err
	}
	if err := validateMembers("remove", request.Remove); err != nil {
		return err
	}
	return nil
}

func ValidateMessageID(messageID string) error {
	if len(messageID) > MaxMessageIDLength {
		return fmt.Errorf("id must be 128 characters or fewer")
	}
	if !messageIDPattern.MatchString(messageID) {
		return fmt.Errorf("id must match [A-Za-z0-9][A-Za-z0-9._:-]{0,127}")
	}

	return nil
}

func ValidateRoomID(roomID string) error {
	if err := ValidateMessageID(roomID); err != nil {
		return fmt.Errorf("room id %w", err)
	}
	return nil
}

func ValidatePart(part Part) error {
	if !IsKnownPartKind(strings.TrimSpace(part.Kind)) {
		return fmt.Errorf("unsupported part kind %q", part.Kind)
	}
	if text := strings.TrimSpace(part.URL); text != "" {
		if err := ValidatePartURL(text); err != nil {
			return fmt.Errorf("url %w", err)
		}
	}
	return nil
}

func ValidatePartURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("is invalid: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https", "molt":
	default:
		return fmt.Errorf("scheme %q is unsupported", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" && parsed.Scheme != "molt" {
		return fmt.Errorf("host is required")
	}
	return nil
}

func ValidateMemberID(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("is required")
	}
	if networkID, agentID, ok := ParseScopedAgentID(trimmed); ok {
		if err := ValidateMessageID(networkID); err != nil {
			return fmt.Errorf("network id %w", err)
		}
		if err := ValidateMessageID(agentID); err != nil {
			return fmt.Errorf("agent id %w", err)
		}
		return nil
	}
	if networkID, agentID, ok := ParseAgentFQID(trimmed); ok {
		if err := ValidateMessageID(networkID); err != nil {
			return fmt.Errorf("network id %w", err)
		}
		if err := ValidateMessageID(agentID); err != nil {
			return fmt.Errorf("agent id %w", err)
		}
		return nil
	}
	if err := ValidateMessageID(trimmed); err != nil {
		return err
	}
	return nil
}

func validateMembers(name string, values []string) error {
	members := UniqueTrimmedStrings(values)
	if len(members) > MaxMembersPerRequest {
		return fmt.Errorf("%s must contain %d IDs or fewer", name, MaxMembersPerRequest)
	}
	for index, memberID := range members {
		if err := ValidateMemberID(memberID); err != nil {
			return fmt.Errorf("%s[%d] %w", name, index, err)
		}
	}
	return nil
}
