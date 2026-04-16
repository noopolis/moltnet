package bridgeconfig

import (
	"net/url"
	"strings"
)

const (
	DefaultOpenClawGatewayURL = "ws://127.0.0.1:18789"
	DefaultPicoClawEventsURL  = "ws://127.0.0.1:18990/pico/ws"
	DefaultPicoClawCommand    = "picoclaw"
	DefaultTinyClawBaseURL    = "http://127.0.0.1:3777"
	DefaultTinyClawChannel    = "moltnet"
	DefaultClaudeCodeCommand  = "claude"
	DefaultCodexCommand       = "codex"
)

func (c Config) Normalized() Config {
	c.Runtime = c.Runtime.Normalized()
	return c
}

func (r RuntimeConfig) Normalized() RuntimeConfig {
	switch strings.TrimSpace(r.Kind) {
	case RuntimeOpenClaw:
		if noRuntimeSeamConfigured(r) {
			r.GatewayURL = DefaultOpenClawGatewayURL
		}
	case RuntimePicoClaw:
		switch {
		case strings.TrimSpace(r.Command) != "":
			return r
		case strings.TrimSpace(r.ConfigPath) != "":
			r.Command = DefaultPicoClawCommand
		case strings.TrimSpace(r.ControlURL) == "" && strings.TrimSpace(r.EventsURL) == "":
			r.EventsURL = DefaultPicoClawEventsURL
		}
	case RuntimeTinyClaw:
		if strings.TrimSpace(r.ControlURL) == "" {
			r = normalizeTinyClawRuntime(r)
		}
	case RuntimeClaudeCode:
		if strings.TrimSpace(r.Command) == "" {
			r.Command = DefaultClaudeCodeCommand
		}
	case RuntimeCodex:
		if strings.TrimSpace(r.Command) == "" {
			r.Command = DefaultCodexCommand
		}
	}

	return r
}

func noRuntimeSeamConfigured(r RuntimeConfig) bool {
	return strings.TrimSpace(r.GatewayURL) == "" &&
		strings.TrimSpace(r.ControlURL) == "" &&
		strings.TrimSpace(r.EventsURL) == "" &&
		strings.TrimSpace(r.Command) == "" &&
		strings.TrimSpace(r.InboundURL) == "" &&
		strings.TrimSpace(r.OutboundURL) == "" &&
		strings.TrimSpace(r.AckURL) == ""
}

func normalizeTinyClawRuntime(r RuntimeConfig) RuntimeConfig {
	channel := strings.TrimSpace(r.Channel)
	if channel == "" {
		channel = DefaultTinyClawChannel
		r.Channel = channel
	}

	baseURL := tinyClawBaseURL(r)
	if strings.TrimSpace(r.InboundURL) == "" {
		r.InboundURL = strings.TrimRight(baseURL, "/") + "/api/message"
	}
	if strings.TrimSpace(r.OutboundURL) == "" {
		r.OutboundURL = strings.TrimRight(baseURL, "/") + "/api/responses/pending?channel=" + url.QueryEscape(channel)
	}
	if strings.TrimSpace(r.AckURL) == "" {
		r.AckURL = strings.TrimRight(baseURL, "/") + "/api/responses"
	}

	return r
}

func tinyClawBaseURL(r RuntimeConfig) string {
	for _, value := range []string{r.InboundURL, r.OutboundURL, r.AckURL} {
		if baseURL := originURL(value); baseURL != "" {
			return baseURL
		}
	}

	return DefaultTinyClawBaseURL
}

func originURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host
}
