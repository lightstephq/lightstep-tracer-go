package lightstep

import (
	"context"
	"io"
	"net/http"

	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
)

// Connection describes a closable connection. Exposed for testing.
type Connection interface {
	io.Closer
}

// ConnectorFactory is for testing purposes.
type ConnectorFactory func() (interface{}, Connection, error)

// collectorResponse encapsulates internal thrift/grpc responses.
type collectorResponse interface {
	GetErrors() []string
	Disable() bool
}

type reportRequest struct {
	thriftRequest *lightstep_thrift.ReportRequest
	protoRequest  *cpb.ReportRequest
	httpRequest   *http.Request
}

// collectorClient encapsulates internal thrift/grpc transports.
type collectorClient interface {
	Report(context.Context, reportRequest) (collectorResponse, error)
	Translate(context.Context, *reportBuffer) (reportRequest, error)
	ConnectClient() (Connection, error)
	ShouldReconnect() bool
}

func newCollectorClient(opts Options, reporterID uint64, attributes map[string]string) (collectorClient, error) {
	if opts.UseThrift {
		return newThriftCollectorClient(opts, reporterID, attributes), nil
	}

	if opts.UseHttp {
		return newHTTPCollectorClient(opts, reporterID, attributes)
	}

	if opts.UseGRPC {
		return newGrpcCollectorClient(opts, reporterID, attributes), nil
	}

	// No transport specified, defaulting to GRPC
	return newGrpcCollectorClient(opts, reporterID, attributes), nil
}
