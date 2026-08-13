package loop

import (
	"fmt"
	"net/url"
	"strings"
)

func attachmentURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse moltnet base url: %w", err)
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported moltnet base url scheme %q", parsed.Scheme)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/attach"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
