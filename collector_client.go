package lightstep

import (
	"context"
	"io"
	"net/http"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
)

var accessTokenHeader = http.CanonicalHeaderKey("Lightstep-Access-Token")

// Connection describes a closable connection. Exposed for testing.
type Connection interface {
	io.Closer
}

// ConnectorFactory is for testing purposes.
type ConnectorFactory func() (interface{}, Connection, error)

// collectorResponse encapsulates internal grpc/http responses.
type collectorResponse interface {
	GetErrors() []string
	Disable() bool
	DevMode() bool
}

type reportRequest struct {
	protoRequest *collectorpb.ReportRequest
	httpRequest  *http.Request
}

// collectorClient encapsulates internal grpc/http transports.
type collectorClient interface {
	Report(context.Context, reportRequest) (collectorResponse, error)
	Translate(context.Context, *reportBuffer) (reportRequest, error)
	ConnectClient() (Connection, error)
	ShouldReconnect() bool
}

func newCollectorClient(opts Options, reporterID uint64, attributes map[string]string) (collectorClient, error) {
	if opts.UseHttp {
		return newHTTPCollectorClient(opts, reporterID, attributes)
	}

	if opts.UseGRPC {
		return newGrpcCollectorClient(opts, reporterID, attributes)
	}

	// No transport specified, defaulting to HTTP
	return newHTTPCollectorClient(opts, reporterID, attributes)
}
