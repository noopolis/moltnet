package bridgeconfig

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func validateWakeConfig(config Config) error {
	for index, room := range config.Rooms {
		prefix := fmt.Sprintf("bridge config rooms[%d]", index)
		if err := validateWakeValue(prefix+".wake", room.Wake); err != nil {
			return err
		}
	}

	if config.DMs != nil {
		if err := validateWakeValue("bridge config dms.wake", config.DMs.Wake); err != nil {
			return err
		}
		if err := ValidateAllowedWakeSenders("bridge config dms.allowed_wake_senders", config.DMs.AllowedWakeSenders); err != nil {
			return err
		}
	}

	return nil
}

// ValidateAllowedWakeSenders validates local unqualified sender IDs for every
// config carrier that embeds DMConfig.
func ValidateAllowedWakeSenders(name string, senders []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "allowed_wake_senders"
	}
	if len(senders) > protocol.MaxMembersPerRequest {
		return fmt.Errorf("%s must contain %d IDs or fewer", name, protocol.MaxMembersPerRequest)
	}
	seen := make(map[string]struct{}, len(senders))
	for index, sender := range senders {
		trimmed := strings.TrimSpace(sender)
		if sender != trimmed {
			return fmt.Errorf("%s[%d] must not contain surrounding whitespace", name, index)
		}
		sender = trimmed
		if _, _, ok := protocol.ParseScopedAgentID(sender); ok {
			return fmt.Errorf("%s[%d] must be an unqualified local member ID", name, index)
		}
		if _, _, ok := protocol.ParseAgentFQID(sender); ok {
			return fmt.Errorf("%s[%d] must be an unqualified local member ID", name, index)
		}
		if err := protocol.ValidateMemberID(sender); err != nil {
			return fmt.Errorf("%s[%d] %w", name, index, err)
		}
		if _, ok := seen[sender]; ok {
			return fmt.Errorf("%s[%d] duplicates %q", name, index, sender)
		}
		seen[sender] = struct{}{}
	}
	return nil
}

func validateWakeValue(name string, mode WakeConfig) error {
	return ValidateWakeValue(name, mode)
}

// ValidateWakeValue validates a single wake policy value for node or client configs.
func ValidateWakeValue(name string, mode WakeConfig) error {
	switch mode {
	case "", WakeAll, WakeMentions, WakeThreadOnly, WakeNever:
		return nil
	default:
		return fmt.Errorf("%s %q is unsupported", name, mode)
	}
}

func validateURLWithSchemes(name string, value string, schemes ...string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}

	allowed := false
	for _, scheme := range schemes {
		if parsed.Scheme == scheme {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%s scheme %q is unsupported", name, parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", name)
	}

	return nil
}

func validateURL(name string, value string) error {
	return validateURLWithSchemes(name, value, "http", "https")
}

func validateSocketURL(name string, value string) error {
	return validateURLWithSchemes(name, value, "ws", "wss")
}
