package bridgeconfig

import (
	"fmt"
	"net/url"
	"strings"
)

func validateReadReplyConfig(config Config) error {
	if err := validateReadConfig("bridge config read", config.Read); err != nil {
		return err
	}
	if err := validateReplyConfig("bridge config reply", config.Reply); err != nil {
		return err
	}

	for index, room := range config.Rooms {
		prefix := fmt.Sprintf("bridge config rooms[%d]", index)
		if err := validateReadConfig(prefix+".read", room.Read); err != nil {
			return err
		}
		if err := validateReplyConfig(prefix+".reply", room.Reply); err != nil {
			return err
		}
	}

	if config.DMs != nil {
		if err := validateReadConfig("bridge config dms.read", config.DMs.Read); err != nil {
			return err
		}
		if err := validateReplyConfig("bridge config dms.reply", config.DMs.Reply); err != nil {
			return err
		}
	}

	return nil
}

func validateReadConfig(name string, mode ReadConfig) error {
	switch mode {
	case "", ReadAll, ReadMentions, ReadThreadOnly:
		return nil
	default:
		return fmt.Errorf("%s %q is unsupported", name, mode)
	}
}

func validateReplyConfig(name string, mode ReplyConfig) error {
	switch mode {
	case "", ReplyAuto, ReplyManual, ReplyNever:
		return nil
	default:
		return fmt.Errorf("%s %q is unsupported", name, mode)
	}
}

func validateURL(name string, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%s scheme %q is unsupported", name, parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", name)
	}

	return nil
}
