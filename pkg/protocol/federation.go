package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	RoomFederationNone = "none"
	RoomFederationAll  = "all"
	RoomFederationList = "list"
)

// RoomFederation controls which configured pairings may learn about and relay
// a room. It encodes as "none", "all", or an array of pairing IDs.
type RoomFederation struct {
	Mode     string
	Pairings []string
}

func DefaultRoomFederation() *RoomFederation {
	return &RoomFederation{Mode: RoomFederationNone}
}

func NormalizeRoomFederation(federation *RoomFederation) RoomFederation {
	if federation == nil {
		return RoomFederation{Mode: RoomFederationNone}
	}
	return RoomFederation{
		Mode:     strings.TrimSpace(federation.Mode),
		Pairings: SortedUniqueTrimmedStrings(federation.Pairings),
	}
}

func ValidateRoomFederation(federation *RoomFederation) error {
	if federation == nil {
		return nil
	}
	normalized := NormalizeRoomFederation(federation)
	switch normalized.Mode {
	case RoomFederationNone, RoomFederationAll:
		if len(normalized.Pairings) != 0 {
			return fmt.Errorf("federation %q cannot name pairings", normalized.Mode)
		}
	case RoomFederationList:
		if len(normalized.Pairings) == 0 {
			return fmt.Errorf("federation list must name at least one pairing")
		}
		for _, pairingID := range normalized.Pairings {
			if err := ValidateMessageID(pairingID); err != nil {
				return fmt.Errorf("federation pairing %q %w", pairingID, err)
			}
		}
	default:
		return fmt.Errorf("unsupported federation %q", normalized.Mode)
	}
	return nil
}

func RoomFederationAllows(federation *RoomFederation, pairingID string) bool {
	normalized := NormalizeRoomFederation(federation)
	switch normalized.Mode {
	case RoomFederationAll:
		return true
	case RoomFederationList:
		for _, allowed := range normalized.Pairings {
			if allowed == strings.TrimSpace(pairingID) {
				return true
			}
		}
	}
	return false
}

func (f RoomFederation) MarshalJSON() ([]byte, error) {
	normalized := NormalizeRoomFederation(&f)
	if err := ValidateRoomFederation(&normalized); err != nil {
		return nil, err
	}
	if normalized.Mode == RoomFederationList {
		return json.Marshal(normalized.Pairings)
	}
	return json.Marshal(normalized.Mode)
}

func (f *RoomFederation) UnmarshalJSON(data []byte) error {
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		*f = RoomFederation{Mode: strings.TrimSpace(mode)}
		return ValidateRoomFederation(f)
	}
	var pairings []string
	if err := json.Unmarshal(data, &pairings); err != nil {
		return fmt.Errorf("federation must be a string or array of pairing IDs")
	}
	*f = RoomFederation{Mode: RoomFederationList, Pairings: pairings}
	return ValidateRoomFederation(f)
}

func (f RoomFederation) MarshalYAML() (any, error) {
	data, err := f.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (f *RoomFederation) UnmarshalYAML(node *yaml.Node) error {
	data, err := json.Marshal(yamlNodeValue(node))
	if err != nil {
		return err
	}
	return f.UnmarshalJSON(data)
}

func yamlNodeValue(node *yaml.Node) any {
	if node.Kind == yaml.SequenceNode {
		values := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			values = append(values, child.Value)
		}
		return values
	}
	return strings.TrimSpace(node.Value)
}

func ParseRoomFederation(data []byte) (*RoomFederation, error) {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return DefaultRoomFederation(), nil
	}
	var federation RoomFederation
	if err := json.Unmarshal(data, &federation); err != nil {
		return nil, err
	}
	return &federation, nil
}
