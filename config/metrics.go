package config

import (
	"errors"
	"math"
)

var (
	ErrInvalidMetricsPort         = errors.New("metrics.port must be between 0 and 65535 and differ from web.port")
	ErrMetricsAuthWithoutSecurity = errors.New("metrics.auth requires security to be configured")
)

// MetricsConfig configures the Prometheus /metrics endpoint
type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`

	// Port serves /metrics on a dedicated listener instead of web.port (0 = web.port)
	Port int `yaml:"port,omitempty"`

	// Auth requires the request to be authenticated through the security config
	Auth bool `yaml:"auth,omitempty"`
}

// UnmarshalYAML also accepts the legacy `metrics: true|false` form
func (m *MetricsConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var enabled bool
	if err := unmarshal(&enabled); err == nil {
		*m = MetricsConfig{Enabled: enabled}
		return nil
	}
	type plain MetricsConfig
	return unmarshal((*plain)(m))
}

func ValidateMetricsConfig(config *Config) error {
	if !config.Metrics.Enabled {
		return nil
	}
	if config.Metrics.Port < 0 || config.Metrics.Port > math.MaxUint16 || (config.Metrics.Port != 0 && config.Metrics.Port == config.Web.Port) {
		return ErrInvalidMetricsPort
	}
	if config.Metrics.Auth && config.Security == nil {
		return ErrMetricsAuthWithoutSecurity
	}
	return nil
}
