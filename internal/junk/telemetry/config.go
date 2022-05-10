package telemetry

import "os"

type Config struct {
	Exporter    string `json:"exporter"`
	ServiceName string `json:"service-name"`
}

func envOrDefault(keyName, defaultValue string) string {
	if value, ok := os.LookupEnv(keyName); ok {
		return value
	} else {
		return defaultValue
	}
}

func DefaultConfig(serviceName string) Config {
	return Config{
		Exporter:    envOrDefault("OTEL_EXPORTER", "none"),
		ServiceName: envOrDefault("OTEL_SERVICE_NAME", serviceName),
	}
}
