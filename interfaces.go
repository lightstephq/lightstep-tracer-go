package lightstep

import (
	"io"
	"net/http"

	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
	ot "github.com/opentracing/opentracing-go"
	"golang.org/x/net/context"
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

// A SpanRecorder handles all of the `RawSpan` data generated via an
// associated `Tracer` instance.
type SpanRecorder interface {
	RecordSpan(RawSpan)
}

// Tracer extends the `opentracing.Tracer` interface with methods for manual
// flushing and closing. To access these methods, you can take the global
// tracer and typecast it to a `lightstep.Tracer`. As a convenience, the
// lightstep package provides static functions which perform the typecasting.
type Tracer interface {
	ot.Tracer

	// Close flushes and then terminates the LightStep collector
	Close(context.Context)
	// Flush sends all spans currently in the buffer to the LighStep collector
	Flush(context.Context)
	// Options gets the Options used in New() or NewWithOptions().
	Options() Options
	// Disable prevents the tracer from recording spans or flushing
	Disable()
}

// lightStepStartSpanOption is used to identify lightstep-specific Span options.
type lightStepStartSpanOption interface {
	applyLS(*startSpanOptions)
}
