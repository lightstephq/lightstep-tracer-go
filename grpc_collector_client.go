package lightstep

import (
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	// N.B.(jmacd): Do not use google.golang.org/glog in this package.

	google_protobuf "github.com/golang/protobuf/ptypes/timestamp"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	ot "github.com/opentracing/opentracing-go"
)

var (
	defaultReconnectPeriod = 5 * time.Minute

	intType reflect.Type = reflect.TypeOf(int64(0))

	errPreviousReportInFlight = fmt.Errorf("a previous Report is still in flight; aborting Flush()")
	errConnectionWasClosed    = fmt.Errorf("the connection was closed")
)

// GrpcCollectorClient specifies how to send reports back to a LightStep
// collector via grpc
type GrpcCollectorClient struct {
	// auth and runtime information
	attributes map[string]string

	// apiURL is the base URL of the LightStep web API, used for
	// explicit trace collection requests.
	apiURL string

	reporterID uint64

	// accessToken is the access token used for explicit trace
	// collection requests.
	accessToken string

	verbose            bool          // whether to print verbose messages
	maxLogKeyLen       int           // see GrpcOptions.MaxLogKeyLen
	maxLogValueLen     int           // see GrpcOptions.MaxLogValueLen
	maxReportingPeriod time.Duration // set by GrpcOptions.MaxReportingPeriod
	reconnectPeriod    time.Duration // set by GrpcOptions.ReconnectPeriod
	reportingTimeout   time.Duration // set by GrpcOptions.ReportTimeout

	// Remote service that will receive reports.
	hostPort      string
	grpcClient    cpb.CollectorServiceClient
	connTimestamp time.Time
	creds         grpc.DialOption

	// For testing purposes only
	grpcConnectorFactory ConnectorFactory
}

func NewGrpcCollectorClient(opts Options, reporterID uint64, attributes map[string]string) *GrpcCollectorClient {
	rec := &GrpcCollectorClient{
		accessToken:          opts.AccessToken,
		attributes:           attributes,
		maxReportingPeriod:   defaultMaxReportingPeriod,
		reportingTimeout:     opts.ReportTimeout,
		verbose:              opts.Verbose,
		maxLogKeyLen:         opts.MaxLogKeyLen,
		maxLogValueLen:       opts.MaxLogValueLen,
		apiURL:               getGrpcAPIURL(opts),
		reporterID:           reporterID,
		hostPort:             getGrpcCollectorHostPort(opts),
		reconnectPeriod:      time.Duration(float64(opts.ReconnectPeriod) * (1 + 0.2*rand.Float64())),
		grpcConnectorFactory: opts.ConnFactory,
	}

	if opts.Collector.Plaintext {
		rec.creds = grpc.WithInsecure()
	} else {
		rec.creds = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))
	}

	return rec
}

func (client *GrpcCollectorClient) ConnectClient() (Connection, error) {
	now := time.Now()
	var conn Connection
	if client.grpcConnectorFactory != nil {
		unchecked_client, transport, err := client.grpcConnectorFactory()
		if err != nil {
			return nil, err
		}

		grpcClient, ok := unchecked_client.(cpb.CollectorServiceClient)
		if !ok {
			return nil, fmt.Errorf("Grpc connector factory did not provide valid client!")
		}

		conn = transport
		client.grpcClient = grpcClient
	} else {
		transport, err := grpc.Dial(client.hostPort, client.creds)
		if err != nil {
			return nil, err
		}

		conn = transport
		client.grpcClient = cpb.NewCollectorServiceClient(transport)
	}
	client.connTimestamp = now
	return conn, nil
}

func (client *GrpcCollectorClient) ShouldReconnect() bool {
	return time.Now().Sub(client.connTimestamp) > client.reconnectPeriod
}

func (client *GrpcCollectorClient) translateTags(tags ot.Tags) []*cpb.KeyValue {
	kvs := make([]*cpb.KeyValue, 0, len(tags))
	for key, tag := range tags {
		kv := client.convertToKeyValue(key, tag)
		kvs = append(kvs, kv)
	}
	return kvs
}

func (client *GrpcCollectorClient) convertToKeyValue(key string, value interface{}) *cpb.KeyValue {
	kv := cpb.KeyValue{Key: key}
	v := reflect.ValueOf(value)
	k := v.Kind()
	switch k {
	case reflect.String:
		kv.Value = &cpb.KeyValue_StringValue{v.String()}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		kv.Value = &cpb.KeyValue_IntValue{v.Convert(intType).Int()}
	case reflect.Float32, reflect.Float64:
		kv.Value = &cpb.KeyValue_DoubleValue{v.Float()}
	case reflect.Bool:
		kv.Value = &cpb.KeyValue_BoolValue{v.Bool()}
	default:
		kv.Value = &cpb.KeyValue_StringValue{fmt.Sprint(v)}
		maybeLogInfof("value: %v, %T, is an unsupported type, and has been converted to string", client.verbose, v, v)
	}
	return &kv
}

func (client *GrpcCollectorClient) translateLogs(lrs []ot.LogRecord, buffer *reportBuffer) []*cpb.Log {
	logs := make([]*cpb.Log, len(lrs))
	for i, lr := range lrs {
		logs[i] = &cpb.Log{
			Timestamp: translateTime(lr.Timestamp),
		}
		marshalFields(client, logs[i], lr.Fields, buffer)
	}
	return logs
}

func (client *GrpcCollectorClient) translateRawSpan(rs RawSpan, buffer *reportBuffer) *cpb.Span {
	s := &cpb.Span{
		SpanContext:    translateSpanContext(rs.Context),
		OperationName:  rs.Operation,
		References:     translateParentSpanID(rs.ParentSpanID),
		StartTimestamp: translateTime(rs.Start),
		DurationMicros: translateDuration(rs.Duration),
		Tags:           client.translateTags(rs.Tags),
		Logs:           client.translateLogs(rs.Logs, buffer),
	}
	return s
}

func (client *GrpcCollectorClient) convertRawSpans(buffer *reportBuffer) []*cpb.Span {
	spans := make([]*cpb.Span, len(buffer.rawSpans))
	for i, rs := range buffer.rawSpans {
		s := client.translateRawSpan(rs, buffer)
		spans[i] = s
	}
	return spans
}

func (client *GrpcCollectorClient) makeReportRequest(buffer *reportBuffer) *cpb.ReportRequest {
	spans := client.convertRawSpans(buffer)
	reporter := convertToReporter(client.attributes, client.reporterID)

	req := cpb.ReportRequest{
		Reporter:        reporter,
		Auth:            &cpb.Auth{client.accessToken},
		Spans:           spans,
		InternalMetrics: convertToInternalMetrics(buffer),
	}
	return &req

}

func (client *GrpcCollectorClient) Report(ctx context.Context, buffer *reportBuffer) (*CollectorResponse, error) {
	resp, err := client.grpcClient.Report(ctx, client.makeReportRequest(buffer))
	if err != nil {
		return nil, err
	}

	commands := make([]*Command, len(resp.Commands))
	for i, command := range resp.Commands {
		commands[i] = &Command{command.GetDisable()}
	}
	return &CollectorResponse{Errors: resp.Errors, Commands: commands}, nil
}

func translateAttributes(atts map[string]string) []*cpb.KeyValue {
	tags := make([]*cpb.KeyValue, 0, len(atts))
	for k, v := range atts {
		tags = append(tags, &cpb.KeyValue{Key: k, Value: &cpb.KeyValue_StringValue{v}})
	}
	return tags
}

func convertToReporter(atts map[string]string, id uint64) *cpb.Reporter {
	return &cpb.Reporter{
		ReporterId: id,
		Tags:       translateAttributes(atts),
	}
}

func generateMetricsSample(b *reportBuffer) []*cpb.MetricsSample {
	return []*cpb.MetricsSample{
		&cpb.MetricsSample{
			Name:  spansDropped,
			Value: &cpb.MetricsSample_IntValue{b.droppedSpanCount},
		},
		&cpb.MetricsSample{
			Name:  logEncoderErrors,
			Value: &cpb.MetricsSample_IntValue{b.logEncoderErrorCount},
		},
	}
}

func convertToInternalMetrics(b *reportBuffer) *cpb.InternalMetrics {
	return &cpb.InternalMetrics{
		StartTimestamp: translateTime(b.reportStart),
		DurationMicros: translateDurationFromOldestYoungest(b.reportStart, b.reportEnd),
		Counts:         generateMetricsSample(b),
	}
}

func translateSpanContext(sc SpanContext) *cpb.SpanContext {
	return &cpb.SpanContext{
		TraceId: sc.TraceID,
		SpanId:  sc.SpanID,
		Baggage: sc.Baggage,
	}
}

func translateParentSpanID(pid uint64) []*cpb.Reference {
	if pid == 0 {
		return nil
	}
	return []*cpb.Reference{
		&cpb.Reference{
			Relationship: cpb.Reference_CHILD_OF,
			SpanContext:  &cpb.SpanContext{SpanId: pid},
		},
	}
}

func translateTime(t time.Time) *google_protobuf.Timestamp {
	return &google_protobuf.Timestamp{
		Seconds: t.Unix(),
		Nanos:   int32(t.Nanosecond()),
	}
}

func translateDuration(d time.Duration) uint64 {
	return uint64(d) / 1000
}

func translateDurationFromOldestYoungest(ot time.Time, yt time.Time) uint64 {
	return translateDuration(yt.Sub(ot))
}

func getGrpcCollectorHostPort(opts Options) string {
	e := opts.Collector
	host := e.Host
	if host == "" {
		if opts.UseGRPC {
			host = defaultGRPCCollectorHost
		} else {
			host = defaultCollectorHost
		}
	}
	port := e.Port
	if port <= 0 {
		if e.Plaintext {
			port = defaultPlainPort
		} else {
			port = defaultSecurePort
		}
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func getGrpcAPIURL(opts Options) string {
	return getGrpcURL(opts.LightStepAPI, defaultAPIHost, "")
}

func getGrpcURL(e Endpoint, host, path string) string {
	if e.Host != "" {
		host = e.Host
	}
	httpProtocol := "https"
	port := defaultSecurePort
	if e.Plaintext {
		httpProtocol = "http"
		port = defaultPlainPort
	}
	if e.Port > 0 {
		port = e.Port
	}
	return fmt.Sprintf("%s://%s:%d%s", httpProtocol, host, port, path)
}
