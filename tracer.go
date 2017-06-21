package lightstep

import (
	"fmt"
	"reflect"
	"time"

	ot "github.com/opentracing/opentracing-go"
)

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

// Tracer extends the opentracing.Tracer interface with methods to
// probe implementation state, for use by basictracer consumers.
type Tracer interface {
	ot.Tracer

	// Options gets the Options used in New() or NewWithOptions().
	Config() TracerConfig
}

type SpanRecorder interface {
	RecordSpan(RawSpan)
}

// Options allows creating a customized Tracer via NewWithOptions. The object
// must not be updated when there is an active tracer using it.
type TracerConfig struct {
	// Recorder receives Spans which have been finished.
	Recorder SpanRecorder
	// DropAllLogs turns log events on all Spans into no-ops.
	// If NewSpanEventListener is set, the callbacks will still fire.
	DropAllLogs bool
	// MaxLogsPerSpan limits the number of Logs in a span (if set to a nonzero
	// value). If a span has more logs than this value, logs are dropped as
	// necessary (and replaced with a log describing how many were dropped).
	//
	// About half of the MaxLogPerSpan logs kept are the oldest logs, and about
	// half are the newest logs.
	//
	// If NewSpanEventListener is set, the callbacks will still fire for all log
	// events. This value is ignored if DropAllLogs is true.
	MaxLogsPerSpan int
}

// DefaultTracerConfig returns an Options object with a 1 in 64 sampling rate and
// all options disabled. A Recorder needs to be set manually before using the
// returned object with a Tracer.
func DefaultTracerConfig() TracerConfig {
	return TracerConfig{
		MaxLogsPerSpan: 100,
	}
}

// NewTracer returns a new Tracer that reports spans to a LightStep
// collector.
func NewTracer(opts Options) ot.Tracer {
	options := DefaultTracerConfig()
	options.Recorder = NewRecorder(opts)
	options.DropAllLogs = opts.DropSpanLogs
	options.MaxLogsPerSpan = opts.MaxLogsPerSpan
	return NewTracerImplWithConfig(options)
}

func FlushLightStepTracer(lsTracer ot.Tracer) error {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	recorder, ok := tracer.Config().Recorder.(*Recorder)
	if !ok {
		return fmt.Errorf("Not a LightStep Recorder type: %v", reflect.TypeOf(tracer.Config().Recorder))
	}

	switch recorder.client.(type) {
	case *GrpcCollectorClient:
		recorder.Flush()
	case *ThriftCollectorClient:
		recorder.Flush()
	default:
		return fmt.Errorf("Not a LightStep Recorder type: %v", reflect.TypeOf(recorder.client))
	}
	return nil
}

func GetLightStepAccessToken(lsTracer ot.Tracer) (string, error) {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return "", fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	recorder, ok := tracer.Config().Recorder.(*Recorder)
	if !ok {
		return "", fmt.Errorf("Not a LighStep Recorder type: %v", reflect.TypeOf(tracer.Config().Recorder))
	}

	switch t := recorder.client.(type) {
	case *GrpcCollectorClient:
		return t.accessToken, nil
	case *ThriftCollectorClient:
		return t.AccessToken, nil
	default:
		return "", fmt.Errorf("Not a LightStep Recorder type: %v", reflect.TypeOf(recorder))
	}
}

func CloseTracer(tracer ot.Tracer) error {
	lsTracer, ok := tracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(tracer))
	}
	recorder, ok := lsTracer.Config().Recorder.(*Recorder)
	if !ok {
		return fmt.Errorf("Not a LighStep Recorder Type: %v", reflect.TypeOf(lsTracer.Config().Recorder))
	}

	return recorder.Close()
}

// Implements the `Tracer` interface.
type tracerImpl struct {
	config           TracerConfig
	textPropagator   textMapPropagator
	binaryPropagator lightstepBinaryPropagator
}

// NewWithOptions creates a customized Tracer.
func NewTracerImplWithConfig(opts TracerConfig) ot.Tracer {
	return &tracerImpl{config: opts}
}

// New creates and returns a standard Tracer which defers completed Spans to
// `recorder`.
// Spans created by this Tracer support the ext.SamplingPriority tag: Setting
// ext.SamplingPriority causes the Span to be Sampled from that point on.
func NewTracerImpl(recorder SpanRecorder) ot.Tracer {
	opts := DefaultTracerConfig()
	opts.Recorder = recorder
	return NewTracerImplWithConfig(opts)
}

func (t *tracerImpl) StartSpan(
	operationName string,
	opts ...ot.StartSpanOption,
) ot.Span {
	sso := StartSpanOptions{}
	for _, o := range opts {
		switch o := o.(type) {
		case LightStepStartSpanOption:
			o.ApplyLS(&sso)
		default:
			o.Apply(&sso.Options)
		}
	}
	return t.startSpanWithOptions(operationName, &sso)
}

func (t *tracerImpl) startSpanWithOptions(
	operationName string,
	opts *StartSpanOptions,
) ot.Span {
	// Start time.
	startTime := opts.Options.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	// Tags.
	tags := opts.Options.Tags

	// Build the new span. This is the only allocation: We'll return this as
	// an opentracing.Span.
	sp := &spanImpl{}

	// It's meaningless to provide wither SpanID or ParentSpanID
	// without also providing TraceID, so just test for TraceID.
	if opts.SetTraceID != 0 {
		sp.raw.Context.TraceID = opts.SetTraceID
		sp.raw.Context.SpanID = opts.SetSpanID
		sp.raw.ParentSpanID = opts.SetParentSpanID
	}

	// Look for a parent in the list of References.
	//
	// TODO: would be nice if basictracer did something with all
	// References, not just the first one.
ReferencesLoop:
	for _, ref := range opts.Options.References {
		switch ref.Type {
		case ot.ChildOfRef,
			ot.FollowsFromRef:

			refCtx := ref.ReferencedContext.(SpanContext)
			sp.raw.Context.TraceID = refCtx.TraceID
			sp.raw.ParentSpanID = refCtx.SpanID

			if l := len(refCtx.Baggage); l > 0 {
				sp.raw.Context.Baggage = make(map[string]string, l)
				for k, v := range refCtx.Baggage {
					sp.raw.Context.Baggage[k] = v
				}
			}
			break ReferencesLoop
		}
	}
	if sp.raw.Context.TraceID == 0 {
		// TraceID not set by parent reference or explicitly
		sp.raw.Context.TraceID, sp.raw.Context.SpanID = genSeededGUID2()
	} else if sp.raw.Context.SpanID == 0 {
		// TraceID set but SpanID not set
		sp.raw.Context.SpanID = genSeededGUID()
	}

	return t.startSpanInternal(
		sp,
		operationName,
		startTime,
		tags,
	)
}

func (t *tracerImpl) startSpanInternal(
	sp *spanImpl,
	operationName string,
	startTime time.Time,
	tags ot.Tags,
) ot.Span {
	sp.tracer = t
	sp.raw.Operation = operationName
	sp.raw.Start = startTime
	sp.raw.Duration = -1
	sp.raw.Tags = tags
	return sp
}

func (t *tracerImpl) Inject(sc ot.SpanContext, format interface{}, carrier interface{}) error {
	switch format {
	case ot.TextMap, ot.HTTPHeaders:
		return t.textPropagator.Inject(sc, carrier)
	case BinaryCarrier:
		return t.binaryPropagator.Inject(sc, carrier)
	}
	return ot.ErrUnsupportedFormat
}

func (t *tracerImpl) Extract(format interface{}, carrier interface{}) (ot.SpanContext, error) {
	switch format {
	case ot.TextMap, ot.HTTPHeaders:
		return t.textPropagator.Extract(carrier)
	case BinaryCarrier:
		return t.binaryPropagator.Extract(carrier)
	}
	return nil, ot.ErrUnsupportedFormat
}

func (t *tracerImpl) Config() TracerConfig {
	return t.config
}
