package lightstepoc

import (
	"github.com/lightstep/lightstep-tracer-go"
	"github.com/opentracing/opentracing-go"
)

// By default, the exporter will attempt to connect to a local satellite
const (
	DefaultSatelliteHost = "localhost"
	DefaultSatellitePort = 8360
)

// Option provides configuration for the Exporter
type Option func(*config)

// WithAccessToken sets an access token for communicating with LightStep
func WithAccessToken(accessToken string) Option {
	return func(c *config) {
		c.tracerOptions.AccessToken = accessToken
	}
}

// WithSatelliteHost sets the satellite host to which spans will be sent
func WithSatelliteHost(satelliteHost string) Option {
	return func(c *config) {
		c.tracerOptions.Collector.Host = satelliteHost
	}
}

// WithSatellitePort sets the satellite port to which spans will be sent
func WithSatellitePort(satellitePort int) Option {
	return func(c *config) {
		c.tracerOptions.Collector.Port = satellitePort
	}
}

// WithInsecure prevents the Exporter from communicating over TLS with the satellite,
// i.e., the connection will run over HTTP instead of HTTPS
func WithInsecure(insecure bool) Option {
	return func(c *config) {
		c.tracerOptions.Collector.Plaintext = insecure
	}
}

// WithMetaEventReportingEnabled configures the tracer to send meta events,
// e.g., events for span creation
func WithMetaEventReportingEnabled(metaEventReportingEnabled bool) Option {
	return func(c *config) {
		c.tracerOptions.MetaEventReportingEnabled = metaEventReportingEnabled
	}
}

// WithComponentName overrides the component (service) name that will be used in LightStep
func WithComponentName(componentName string) Option {
	return func(c *config) {
		if componentName != "" {
			c.tracerOptions.Tags[lightstep.ComponentNameKey] = componentName
		}
	}
}

type config struct {
	tracerOptions lightstep.Options
}

func defaultConfig() *config {
	return &config{
		tracerOptions: lightstep.Options{
			Collector: lightstep.Endpoint{
				Host: DefaultSatelliteHost,
				Port: DefaultSatellitePort,
			},
			Tags:    make(opentracing.Tags),
			UseHttp: true,
		},
	}
}
