package lightstep

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	// N.B.(jmacd): Do not use google.golang.org/glog in this package.

	google_protobuf "github.com/golang/protobuf/ptypes/timestamp"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
	ot "github.com/opentracing/opentracing-go"
)

// TODO: Move what's left of basictracer/* into this package.

const (
	spansDropped     = "spans.dropped"
	logEncoderErrors = "log_encoder.errors"
	collectorPath    = "/_rpc/v1/reports/binary"

	defaultPlainPort  = 80
	defaultSecurePort = 443

	defaultCollectorHost     = "collector.lightstep.com"
	defaultGRPCCollectorHost = "collector-grpc.lightstep.com"
	defaultAPIHost           = "api.lightstep.com"

	// See the comment for shouldFlush() for more about these tuning
	// parameters.
	defaultMaxReportingPeriod = 2500 * time.Millisecond
	minReportingPeriod        = 500 * time.Millisecond

	defaultMaxSpans       = 1000
	defaultReportTimeout  = 30 * time.Second
	defaultMaxLogKeyLen   = 256
	defaultMaxLogValueLen = 1024
	defaultMaxLogsPerSpan = 500

	// ParentSpanGUIDKey is the tag key used to record the relationship
	// between child and parent spans.
	ParentSpanGUIDKey = "parent_span_guid"
	messageKey        = "message"
	payloadKey        = "payload"

	TracerPlatformValue = "go"
	// Note: TracerVersionValue is generated from ./VERSION

	TracerPlatformKey        = "lightstep.tracer_platform"
	TracerPlatformVersionKey = "lightstep.tracer_platform_version"
	TracerVersionKey         = "lightstep.tracer_version"
	ComponentNameKey         = "lightstep.component_name"
	GUIDKey                  = "lightstep.guid" // <- runtime guid, not span guid
	HostnameKey              = "lightstep.hostname"
	CommandLineKey           = "lightstep.command_line"
)

var (
	defaultReconnectPeriod = 5 * time.Minute

	intType reflect.Type = reflect.TypeOf(int64(0))

	errPreviousReportInFlight = fmt.Errorf("a previous Report is still in flight; aborting Flush()")
	errConnectionWasClosed    = fmt.Errorf("the connection was closed")
	logOneError               sync.Once
)

type GrpcConnection interface {
	Close() error
	GetMethodConfig(string) grpc.MethodConfig
}

// A set of counter values for a given time window
type counterSet struct {
	droppedSpans int64
}

// Endpoint describes a collection or web API host/port and whether or
// not to use plaintext communicatation.
type Endpoint struct {
	Host      string `yaml:"host" usage:"host on which the endpoint is running"`
	Port      int    `yaml:"port" usage:"port on which the endpoint is listening"`
	Plaintext bool   `yaml:"plaintext" usage:"whether or not to encrypt data send to the endpoint"`
}

// A SpanRecorder handles all of the `RawSpan` data generated via an
// associated `Tracer` (see `NewStandardTracer`) instance. It also names
// the containing process and provides access to a straightforward tag map.
type SpanRecorder interface {
	// Implementations must determine whether and where to store `span`.
	RecordSpan(span RawSpan)
}

// GrpcOptions control how the LightStep Tracer behaves.
type GrpcOptions struct {
	// AccessToken is the unique API key for your LightStep project.  It is
	// available on your account page at https://app.lightstep.com/account
	AccessToken string `yaml:"access_token" usage:"access token for reporting to LightStep"`

	// Collector is the host, port, and plaintext option to use
	// for the collector.
	Collector Endpoint `yaml:"collector"`

	// Tags are arbitrary key-value pairs that apply to all spans generated by
	// this Tracer.
	Tags ot.Tags

	// LightStep is the host, port, and plaintext option to use
	// for the LightStep web API.
	LightStepAPI Endpoint `yaml:"lightstep_api"`

	// MaxBufferedSpans is the maximum number of spans that will be buffered
	// before sending them to a collector.
	MaxBufferedSpans int `yaml:"max_buffered_spans"`

	// MaxLogKeyLen is the maximum allowable size (in characters) of an
	// OpenTracing logging key. Longer keys are truncated.
	MaxLogKeyLen int `yaml:"max_log_key_len"`

	// MaxLogValueLen is the maximum allowable size (in characters) of an
	// OpenTracing logging value. Longer values are truncated. Only applies to
	// variable-length value types (strings, interface{}, etc).
	MaxLogValueLen int `yaml:"max_log_value_len"`

	// MaxLogsPerSpan limits the number of logs in a single span.
	MaxLogsPerSpan int `yaml:"max_logs_per_span"`

	// ReportingPeriod is the maximum duration of time between sending spans
	// to a collector.  If zero, the default will be used.
	ReportingPeriod time.Duration `yaml:"reporting_period"`

	ReportTimeout time.Duration `yaml:"report_timeout"`

	// DropSpanLogs turns log events on all Spans into no-ops.
	DropSpanLogs bool `yaml:"drop_span_logs"`

	// Set Verbose to true to enable more text logging.
	Verbose bool `yaml:"verbose"`

	// DEPRECATED: set `UseThrift` to true if you do not want gRPC
	UseGRPC bool `yaml:"usegrpc"`

	// Switch to
	UseThrift bool `yaml:"use_thrift"`

	ReconnectPeriod time.Duration `yaml:"reconnect_period"`

	// For testing purposes only
	GrpcConnector func() (GrpcConnection, cpb.CollectorServiceClient, error)

	// For testing purposes only
	ThriftConnector func() lightstep_thrift.ReportingService
}

func (opts *GrpcOptions) setDefaults() {
	// Note: opts is a copy of the user's data, ok to modify.
	if opts.MaxBufferedSpans == 0 {
		opts.MaxBufferedSpans = defaultMaxSpans
	}
	if opts.MaxLogKeyLen == 0 {
		opts.MaxLogKeyLen = defaultMaxLogKeyLen
	}
	if opts.MaxLogValueLen == 0 {
		opts.MaxLogValueLen = defaultMaxLogValueLen
	}
	if opts.MaxLogsPerSpan == 0 {
		opts.MaxLogsPerSpan = defaultMaxLogsPerSpan
	}
	if opts.ReportingPeriod == 0 {
		opts.ReportingPeriod = defaultMaxReportingPeriod
	}
	if opts.ReportTimeout == 0 {
		opts.ReportTimeout = defaultReportTimeout
	}
	if opts.ReconnectPeriod == 0 {
		opts.ReconnectPeriod = defaultReconnectPeriod
	}
}

// Recorder buffers spans and forwards them to a LightStep collector.
type GrpcRecorder struct {
	lock sync.Mutex

	// Note: the following are divided into immutable fields and
	// mutable fields. The mutable fields are modified under `lock`.

	//////////////////////////////////////////////////////////////
	// IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE
	//////////////////////////////////////////////////////////////

	// Note: there may be a desire to update some of these fields
	// at runtime, in which case suitable changes may be needed
	// for variables accessed during Flush.

	// auth and runtime information
	attributes map[string]string
	startTime  time.Time

	// apiURL is the base URL of the LightStep web API, used for
	// explicit trace collection requests.
	apiURL string

	// accessToken is the access token used for explicit trace
	// collection requests.
	accessToken string

	reporterID         uint64        // the LightStep tracer guid
	verbose            bool          // whether to print verbose messages
	maxLogKeyLen       int           // see GrpcOptions.MaxLogKeyLen
	maxLogValueLen     int           // see GrpcOptions.MaxLogValueLen
	maxReportingPeriod time.Duration // set by GrpcOptions.MaxReportingPeriod
	reconnectPeriod    time.Duration // set by GrpcOptions.ReconnectPeriod
	reportingTimeout   time.Duration // set by GrpcOptions.ReportTimeout

	// Remote service that will receive reports.
	hostPort      string
	backend       cpb.CollectorServiceClient
	conn          GrpcConnection
	connTimestamp time.Time
	creds         grpc.DialOption
	closech       chan struct{}

	//////////////////////////////////////////////////////////
	// MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE
	//////////////////////////////////////////////////////////

	// Two buffers of data.
	buffer   reportBuffer
	flushing reportBuffer

	// Flush state.
	reportInFlight    bool
	lastReportAttempt time.Time

	// We allow our remote peer to disable this instrumentation at any
	// time, turning all potentially costly runtime operations into
	// no-ops.
	//
	// TODO this should use atomic load/store to test disabled
	// prior to taking the lock, do please.
	disabled bool

	// For testing purposes only
	grpcConnector func() (GrpcConnection, cpb.CollectorServiceClient, error)
}

func NewRecorder(opts Options) *GrpcRecorder {
	opts.setDefaults()
	if len(opts.AccessToken) == 0 {
		fmt.Println("LightStep Recorder options.AccessToken must not be empty")
		return nil
	}
	if opts.Tags == nil {
		opts.Tags = make(map[string]interface{})
	}
	// Set some default attributes if not found in options
	if _, found := opts.Tags[ComponentNameKey]; !found {
		opts.Tags[ComponentNameKey] = path.Base(os.Args[0])
	}
	if _, found := opts.Tags[GUIDKey]; found {
		fmt.Printf("Passing in your own %v is no longer supported\n", GUIDKey)
	}
	if _, found := opts.Tags[HostnameKey]; !found {
		hostname, _ := os.Hostname()
		opts.Tags[HostnameKey] = hostname
	}
	if _, found := opts.Tags[CommandLineKey]; !found {
		opts.Tags[CommandLineKey] = strings.Join(os.Args, " ")
	}

	attributes := make(map[string]string)
	for k, v := range opts.Tags {
		attributes[k] = fmt.Sprint(v)
	}
	// Don't let the GrpcOptions override these values. That would be confusing.
	attributes[TracerPlatformKey] = TracerPlatformValue
	attributes[TracerPlatformVersionKey] = runtime.Version()
	attributes[TracerVersionKey] = TracerVersionValue

	now := time.Now()
	rec := &GrpcRecorder{
		accessToken:        opts.AccessToken,
		attributes:         attributes,
		startTime:          now,
		maxReportingPeriod: defaultMaxReportingPeriod,
		reportingTimeout:   opts.ReportTimeout,
		verbose:            opts.Verbose,
		maxLogKeyLen:       opts.MaxLogKeyLen,
		maxLogValueLen:     opts.MaxLogValueLen,
		apiURL:             getAPIURL(opts),
		reporterID:         genSeededGUID(),
		buffer:             newSpansBuffer(opts.MaxBufferedSpans),
		flushing:           newSpansBuffer(opts.MaxBufferedSpans),
		hostPort:           getCollectorHostPort(opts),
		reconnectPeriod:    time.Duration(float64(opts.ReconnectPeriod) * (1 + 0.2*rand.Float64())),
		grpcConnector:      opts.GrpcConnector,
	}

	rec.buffer.setCurrent(now)

	if opts.Collector.Plaintext {
		rec.creds = grpc.WithInsecure()
	} else {
		rec.creds = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))
	}

	return rec
}

func (client *GrpcRecorder) ConnectClient() (Connection, error) {
	var conn Connection
	var backend cpb.CollectorServiceClient
	if client.grpcConnector != nil {
		conn, backend, _ = client.grpcConnector()
	} else {
		conn, err := grpc.Dial(client.hostPort, client.creds)
		if err != nil {
			return nil, err
		}
		backend = cpb.NewCollectorServiceClient(conn)
	}

	client.backend = backend
	return conn, nil
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

func (r *GrpcRecorder) translateTags(tags ot.Tags) []*cpb.KeyValue {
	kvs := make([]*cpb.KeyValue, 0, len(tags))
	for key, tag := range tags {
		kv := r.convertToKeyValue(key, tag)
		kvs = append(kvs, kv)
	}
	return kvs
}

func (r *GrpcRecorder) convertToKeyValue(key string, value interface{}) *cpb.KeyValue {
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
		r.maybeLogInfof("value: %v, %T, is an unsupported type, and has been converted to string", v, v)
	}
	return &kv
}

func (r *GrpcRecorder) translateLogs(lrs []ot.LogRecord, buffer *reportBuffer) []*cpb.Log {
	logs := make([]*cpb.Log, len(lrs))
	for i, lr := range lrs {
		logs[i] = &cpb.Log{
			Timestamp: translateTime(lr.Timestamp),
		}
		marshalFields(r, logs[i], lr.Fields, buffer)
	}
	return logs
}

func (r *GrpcRecorder) translateRawSpan(rs RawSpan, buffer *reportBuffer) *cpb.Span {
	s := &cpb.Span{
		SpanContext:    translateSpanContext(rs.Context),
		OperationName:  rs.Operation,
		References:     translateParentSpanID(rs.ParentSpanID),
		StartTimestamp: translateTime(rs.Start),
		DurationMicros: translateDuration(rs.Duration),
		Tags:           r.translateTags(rs.Tags),
		Logs:           r.translateLogs(rs.Logs, buffer),
	}
	return s
}

func (r *GrpcRecorder) convertRawSpans(buffer *reportBuffer) []*cpb.Span {
	spans := make([]*cpb.Span, len(buffer.rawSpans))
	for i, rs := range buffer.rawSpans {
		s := r.translateRawSpan(rs, buffer)
		spans[i] = s
	}
	return spans
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

func (r *GrpcRecorder) makeReportRequest(buffer *reportBuffer) *cpb.ReportRequest {
	spans := r.convertRawSpans(buffer)
	reporter := convertToReporter(r.attributes, r.reporterID)

	req := cpb.ReportRequest{
		Reporter:        reporter,
		Auth:            &cpb.Auth{r.accessToken},
		Spans:           spans,
		InternalMetrics: convertToInternalMetrics(buffer),
	}
	return &req

}

func (client *GrpcRecorder) Report(ctx context.Context, buffer *reportBuffer) (*CollectorResponse, error) {
	resp, err := client.backend.Report(ctx, client.makeReportRequest(buffer))
	if err != nil {
		return nil, err
	}

	commands := make([]*Command, len(resp.Commands))
	for i, command := range resp.Commands {
		commands[i] = &Command{command.GetDisable()}
	}
	return &CollectorResponse{Errors: resp.Errors, Commands: commands}, nil
}

// maybeLogError logs the first error it receives using the standard log
// package and may also log subsequent errors based on verboseFlag.
func (r *GrpcRecorder) maybeLogError(err error) {
	if r.verbose {
		log.Printf("LightStep error: %v\n", err)
	} else {
		// Even if the flag is not set, always log at least one error.
		logOneError.Do(func() {
			log.Printf("LightStep instrumentation error (%v). Set the Verbose option to enable more logging.\n", err)
		})
	}
}

// maybeLogInfof may format and log its arguments if verboseFlag is set.
func (r *GrpcRecorder) maybeLogInfof(format string, args ...interface{}) {
	if r.verbose {
		s := fmt.Sprintf(format, args...)
		log.Printf("LightStep info: %s\n", s)
	}
}

func getCollectorHostPort(opts Options) string {
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

func getCollectorURL(opts Options) string {
	// TODO This is dead code, remove?
	return getURL(opts.Collector,
		defaultCollectorHost,
		collectorPath)
}

func getAPIURL(opts Options) string {
	return getURL(opts.LightStepAPI, defaultAPIHost, "")
}

func getURL(e Endpoint, host, path string) string {
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
