package transport

type HTTPConfig struct {
	Console ConsoleConfig
}

type ConsoleConfig struct {
	Analytics ConsoleAnalyticsConfig
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
