package transport

import "github.com/noopolis/moltnet/pkg/protocol"

type HTTPConfig struct {
	PublicBaseURL string
	ClockIdentity string
	Console       ConsoleConfig
}

type ConsoleConfig struct {
	LocalOwnerToken protocol.SecretString
	Analytics       ConsoleAnalyticsConfig
}

type ConsoleAnalyticsConfig struct {
	Provider      string
	MeasurementID string
}

func httpConfigFrom(values []HTTPConfig) HTTPConfig {
	if len(values) == 0 {
		return HTTPConfig{}
	}
	return values[0]
}
