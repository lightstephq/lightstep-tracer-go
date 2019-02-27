package lightstepoc

import (
	"github.com/lightstep/lightstep-tracer-go"
	"github.com/opentracing/opentracing-go"
)

const (
	DefaultSatelliteHost = "localhost"
	DefaultSatellitePort = 8360
)

type Option func(*config)

func WithAccessToken(accessToken string) Option {
	return func(c *config) {
		c.tracerOptions.AccessToken = accessToken
	}
}

func WithSatelliteHost(satelliteHost string) Option {
	return func(c *config) {
		c.tracerOptions.Collector.Host = satelliteHost
	}
}

func WithSatellitePort(satellitePort int) Option {
	return func(c *config) {
		c.tracerOptions.Collector.Port = satellitePort
	}
}

func WithInsecure(insecure bool) Option {
	return func(c *config) {
		c.tracerOptions.Collector.Plaintext = insecure
	}
}

func WithMetaEventReportingEnabled(metaEventReportingEnabled bool) Option {
	return func(c *config) {
		c.tracerOptions.MetaEventReportingEnabled = metaEventReportingEnabled
	}
}

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
