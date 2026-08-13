package protocol

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func (page MachineReadPage) Validate() error {
	if len(page.Messages) > MachineMaxReadLimit {
		return fmt.Errorf("page.messages must contain at most %d messages", MachineMaxReadLimit)
	}
	if err := page.Page.Validate(); err != nil {
		return fmt.Errorf("invalid page info")
	}
	for index, message := range page.Messages {
		if err := message.Validate(); err != nil {
			return fmt.Errorf("page.messages[%d]: %s", index, err)
		}
	}
	if err := validateMachineResponseLineBytes(MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: strings.Repeat("x", MachineMaxCorrelationBytes),
		Operation:     MachineOpRead,
		Read: &MachineReadResult{
			Target: MachineTarget{
				Kind: MachineTargetKindRoom,
				ID:   strings.Repeat("x", MachineMaxTargetBytes),
			},
			Page: page,
		},
	}); err != nil {
		return err
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
				return fmt.Errorf("next_before is invalid")
			}
		}
		if info.NextAfter != "" {
			if err := validateMachineIdentifier(info.NextAfter, "next_after", MachineMaxCursorBytes); err != nil {
				return fmt.Errorf("next_after is invalid")
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
	if err := validateMachineReadTarget(decoded.Target); err != nil {
		return err
	}
	if err := validateReadActor(decoded.From); err != nil {
		return err
	}
	if len(decoded.Parts) == 0 {
		return fmt.Errorf("parts must not be empty")
	}
	if len(decoded.Parts) > MachineMaxReadMessageParts {
		return fmt.Errorf("parts must contain at most %d entries", MachineMaxReadMessageParts)
	}
	for index, part := range decoded.Parts {
		if err := validateMachineReadPart(part); err != nil {
			return fmt.Errorf("parts[%d]: %s", index, err)
		}
	}
	if len(decoded.Mentions) > MachineMaxReadMentions {
		return fmt.Errorf("mentions must contain at most %d entries", MachineMaxReadMentions)
	}
	for index, mention := range decoded.Mentions {
		if strings.TrimSpace(mention) != mention {
			return fmt.Errorf("mentions[%d] contains invalid formatting", index)
		}
		if len(mention) > MachineMaxTargetBytes {
			return fmt.Errorf("mentions[%d] exceeds maximum size", index)
		}
		if err := ValidateMemberID(mention); err != nil {
			return fmt.Errorf("mentions[%d] is invalid", index)
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
	if strings.TrimSpace(actor.Type) != actor.Type {
		return fmt.Errorf("from.type has invalid formatting")
	}
	if len(actor.Type) > MachineMaxTargetBytes {
		return fmt.Errorf("from.type exceeds maximum size")
	}
	if strings.TrimSpace(actor.ID) == "" {
		return fmt.Errorf("from.id is required")
	}
	if strings.TrimSpace(actor.ID) != actor.ID {
		return fmt.Errorf("from.id must not include leading or trailing whitespace")
	}
	if err := ValidateMemberID(actor.ID); err != nil {
		return fmt.Errorf("from.id is invalid")
	}
	if len(actor.Name) > MachineMaxTargetBytes {
		return fmt.Errorf("from.name exceeds maximum size")
	}
	if len(actor.FQID) > MachineMaxTargetBytes {
		return fmt.Errorf("from.fqid exceeds maximum size")
	}
	if actor.NetworkID != "" {
		if strings.TrimSpace(actor.NetworkID) != actor.NetworkID {
			return fmt.Errorf("from.network_id must not include leading or trailing whitespace")
		}
		if len(actor.NetworkID) > MachineMaxTargetBytes {
			return fmt.Errorf("from.network_id exceeds maximum size")
		}
		if err := validateMachineIdentifier(actor.NetworkID, "from.network_id", MachineMaxTargetBytes); err != nil {
			return fmt.Errorf("from.network_id is invalid")
		}
	}
	return nil
}

func validateMachineReadPart(part Part) error {
	if strings.TrimSpace(part.Kind) != part.Kind || part.Kind == "" {
		return fmt.Errorf("part kind is invalid")
	}
	if !IsKnownPartKind(part.Kind) {
		return fmt.Errorf("part kind is unsupported")
	}
	if len(part.Text) > MachineMaxReadPartTextBytes {
		return fmt.Errorf("part.text exceeds maximum size")
	}
	if len(part.MediaType) > MachineMaxReadPartMediaType {
		return fmt.Errorf("part.media_type exceeds maximum size")
	}
	if len(part.Filename) > MachineMaxReadPartFilename {
		return fmt.Errorf("part.filename exceeds maximum size")
	}
	if len(part.URL) > MachineMaxReadPartURLBytes {
		return fmt.Errorf("part.url exceeds maximum size")
	}
	if part.URL != "" {
		if strings.TrimSpace(part.URL) != part.URL {
			return fmt.Errorf("part.url has invalid formatting")
		}
		if err := validateMachineReadPartURL(part.URL); err != nil {
			return fmt.Errorf("part.url is invalid")
		}
	}
	if part.Data != nil {
		rawData, err := json.Marshal(part.Data)
		if err != nil {
			return fmt.Errorf("part.data is invalid")
		}
		if len(rawData) > MachineMaxReadPartDataBytes {
			return fmt.Errorf("part.data exceeds maximum size")
		}
	}
	return nil
}

func validateMachineReadPartURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	switch parsed.Scheme {
	case "http", "https", "molt":
	default:
		return fmt.Errorf("unsupported URL scheme")
	}
	if parsed.Scheme != "molt" && strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("missing URL host")
	}
	return nil
}

func validateMachineReadTarget(target Target) error {
	if strings.TrimSpace(target.Kind) != target.Kind {
		return fmt.Errorf("target.kind has invalid formatting")
	}

	switch target.Kind {
	case TargetKindRoom:
		if target.RoomID == "" {
			return fmt.Errorf("target.room_id is required")
		}
		if strings.TrimSpace(target.RoomID) != target.RoomID {
			return fmt.Errorf("target.room_id has invalid formatting")
		}
		if err := ValidateRoomID(target.RoomID); err != nil {
			return fmt.Errorf("target.room_id is invalid")
		}
		if target.ThreadID != "" || target.ParentMessageID != "" || target.DMID != "" || len(target.ParticipantIDs) != 0 {
			return fmt.Errorf("target contains irrelevant fields")
		}
	case TargetKindDM:
		if strings.TrimSpace(target.DMID) != target.DMID {
			return fmt.Errorf("target.dm_id has invalid formatting")
		}
		if err := ValidateMessageID(target.DMID); err != nil {
			return fmt.Errorf("target.dm_id is invalid")
		}
		if target.RoomID != "" || target.ThreadID != "" || target.ParentMessageID != "" || len(target.ParticipantIDs) != 0 {
			return fmt.Errorf("target contains irrelevant fields")
		}
	default:
		return fmt.Errorf("target.kind is invalid")
	}

	return nil
}
