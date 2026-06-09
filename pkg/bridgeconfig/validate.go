package bridgeconfig

import (
	"fmt"
	"net/url"
	"strings"
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
