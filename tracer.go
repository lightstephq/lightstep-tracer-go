package lightstep

import (
	"fmt"
	"io"
	"reflect"
	"time"

	ot "github.com/opentracing/opentracing-go"
)

// Tracer extends the opentracing.Tracer interface with methods to
// probe implementation state, for use by basictracer consumers.
type Tracer interface {
	ot.Tracer

	// Options gets the Options used in New() or NewWithOptions().
	Config() TracerConfig
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

// NewTracer returns a new Tracer that reports spans to a LightStep
// collector.
func NewTracer(opts GrpcOptions) ot.Tracer {
	options := DefaultTracerConfig()

	if !opts.UseThrift {
		r := NewRecorder(opts)
		if r == nil {
			return ot.NoopTracer{}
		}
		options.Recorder = r
	} else {
		opts.setDefaults()
		// convert opts to ThriftOptions
		thriftOpts := ThriftOptions{
			AccessToken:      opts.AccessToken,
			Collector:        Endpoint{opts.Collector.Host, opts.Collector.Port, opts.Collector.Plaintext},
			Tags:             opts.Tags,
			LightStepAPI:     Endpoint{opts.LightStepAPI.Host, opts.LightStepAPI.Port, opts.LightStepAPI.Plaintext},
			MaxBufferedSpans: opts.MaxBufferedSpans,
			ReportingPeriod:  opts.ReportingPeriod,
			ReportTimeout:    opts.ReportTimeout,
			DropSpanLogs:     opts.DropSpanLogs,
			MaxLogsPerSpan:   opts.MaxLogsPerSpan,
			Verbose:          opts.Verbose,
			MaxLogMessageLen: opts.MaxLogValueLen,
		}
		r := NewThriftRecorder(thriftOpts)
		if r == nil {
			return ot.NoopTracer{}
		}
		options.Recorder = r
	}
	options.DropAllLogs = opts.DropSpanLogs
	options.MaxLogsPerSpan = opts.MaxLogsPerSpan
	return NewTracerImplWithConfig(options)
}

func FlushLightStepTracer(lsTracer ot.Tracer) error {
	basicTracer, ok := lsTracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	basicRecorder := basicTracer.Config().Recorder

	switch t := basicRecorder.(type) {
	case *GrpcRecorder:
		t.Flush()
	case *ThriftRecorder:
		t.Flush()
	default:
		return fmt.Errorf("Not a LightStep Recorder type: %v", reflect.TypeOf(basicRecorder))
	}
	return nil
}

func GetLightStepAccessToken(lsTracer ot.Tracer) (string, error) {
	basicTracer, ok := lsTracer.(Tracer)
	if !ok {
		return "", fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	basicRecorder := basicTracer.Config().Recorder

	switch t := basicRecorder.(type) {
	case *GrpcRecorder:
		return t.accessToken, nil
	case *ThriftRecorder:
		return t.AccessToken, nil
	default:
		return "", fmt.Errorf("Not a LightStep Recorder type: %v", reflect.TypeOf(basicRecorder))
	}
}

func CloseTracer(tracer ot.Tracer) error {
	lsTracer, ok := tracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(tracer))
	}
	recorder, ok := lsTracer.Config().Recorder.(io.Closer)
	if !ok {
		return fmt.Errorf("Recorder does not implement Close: %v", reflect.TypeOf(recorder))
	}

	return recorder.Close()
}

type StartSpanOptions struct {
	Options ot.StartSpanOptions

	// Options to explicitly set span_id, trace_id,
	// parent_span_id, expected to be used when exporting spans
	// from another system into LightStep via opentracing APIs.
	SetSpanID       uint64
	SetParentSpanID uint64
	SetTraceID      uint64
}

type LightStepStartSpanOption interface {
	ApplyLS(*StartSpanOptions)
}

// Implements the `Tracer` interface.
type tracerImpl struct {
	config           TracerConfig
	textPropagator   textMapPropagator
	binaryPropagator lightstepBinaryPropagator
}

// DefaultTracerConfig returns an Options object with a 1 in 64 sampling rate and
// all options disabled. A Recorder needs to be set manually before using the
// returned object with a Tracer.
func DefaultTracerConfig() TracerConfig {
	return TracerConfig{
		MaxLogsPerSpan: 100,
	}
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
