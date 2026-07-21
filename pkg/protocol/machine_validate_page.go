package protocol

import (
	"fmt"
	"strings"
)

func (page MachineReadPage) Validate() error {
	if len(page.Messages) > MachineMaxReadLimit {
		return fmt.Errorf("page.messages must contain at most %d messages", MachineMaxReadLimit)
	}
	if err := page.Page.Validate(); err != nil {
		return err
	}
	for index, message := range page.Messages {
		if err := message.Validate(); err != nil {
			return fmt.Errorf("page.messages[%d]: %w", index, err)
		}
	}
	return nil
}

func (info MachineReadPageInfo) Validate() error {
	if info.HasMore == nil {
		return fmt.Errorf("has_more is required")
	}
	if *info.HasMore {
		if info.NextBefore == "" && info.NextAfter == "" {
			return fmt.Errorf("exactly one cursor is required when has_more is true")
		}
		if info.NextBefore != "" && info.NextAfter != "" {
			return fmt.Errorf("next_before and next_after cannot both be set")
		}
		if info.NextBefore != "" {
			if err := validateMachineIdentifier(info.NextBefore, "next_before", MachineMaxCursorBytes); err != nil {
				return err
			}
		}
		if info.NextAfter != "" {
			if err := validateMachineIdentifier(info.NextAfter, "next_after", MachineMaxCursorBytes); err != nil {
				return err
			}
		}
		return nil
	}

	if info.NextBefore != "" {
		return fmt.Errorf("next_before must be absent when has_more is false")
	}
	if info.NextAfter != "" {
		return fmt.Errorf("next_after must be absent when has_more is false")
	}
	return nil
}

func (message MachineReadMessage) Validate() error {
	decoded := Message(message)
	if err := validateMachineIdentifier(decoded.ID, "id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if decoded.NetworkID == "" {
		return fmt.Errorf("network_id is required")
	}
	if err := validateMachineIdentifier(decoded.NetworkID, "network_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if err := decoded.Origin.Validate(); err != nil {
		return err
	}
	if err := ValidateTarget(decoded.Target); err != nil {
		return err
	}
	if err := validateReadActor(decoded.From); err != nil {
		return err
	}
	if len(decoded.Parts) == 0 {
		return fmt.Errorf("parts must not be empty")
	}
	for index, part := range decoded.Parts {
		if err := ValidatePart(part); err != nil {
			return fmt.Errorf("parts[%d] %w", index, err)
		}
	}
	for index, mention := range decoded.Mentions {
		if strings.TrimSpace(mention) != mention {
			return fmt.Errorf("mentions[%d] must not include leading or trailing whitespace", index)
		}
		if err := ValidateMemberID(mention); err != nil {
			return fmt.Errorf("mentions[%d] %w", index, err)
		}
	}
	if decoded.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	return nil
}

func (result MachineReadResult) Validate() error {
	if err := result.Target.Validate(); err != nil {
		return err
	}
	if err := result.Page.Validate(); err != nil {
		return err
	}
	return nil
}

func (origin MessageOrigin) Validate() error {
	if origin.NetworkID == "" {
		return fmt.Errorf("network_id is required")
	}
	if err := validateMachineIdentifier(origin.NetworkID, "origin.network_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if err := validateMachineIdentifier(origin.MessageID, "origin.message_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	return nil
}

func validateReadActor(actor Actor) error {
	if actor.Type == "" {
		return fmt.Errorf("from.type is required")
	}
	if strings.TrimSpace(actor.ID) == "" {
		return fmt.Errorf("from.id is required")
	}
	if strings.TrimSpace(actor.ID) != actor.ID {
		return fmt.Errorf("from.id must not include leading or trailing whitespace")
	}
	if err := ValidateMemberID(actor.ID); err != nil {
		return fmt.Errorf("from.id %w", err)
	}
	if actor.NetworkID != "" {
		if strings.TrimSpace(actor.NetworkID) != actor.NetworkID {
			return fmt.Errorf("from.network_id must not include leading or trailing whitespace")
		}
		if err := validateMachineIdentifier(actor.NetworkID, "from.network_id", MachineMaxTargetBytes); err != nil {
			return err
		}
	}
	return nil
}
