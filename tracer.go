package lightstep

import (
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"golang.org/x/net/context"

	"os"
	"path"
	"runtime"
	"strings"
	"sync"

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

// A SpanRecorder handles all of the `RawSpan` data generated via an
// associated `Tracer` instance.
type SpanRecorder interface {
	// Implementations must determine whether and where to store `span`.
	RecordSpan(span RawSpan)
}

// Tracer extends the opentracing.Tracer interface with methods to
// probe implementation state, for use by basictracer consumers.
type Tracer interface {
	ot.Tracer

	// Close ends the connection to the LightStep collector
	Close() error
	// Flush sends all spans currently in the buffer to the LighStep collector
	Flush()
	// Disable temporarily stops communication with the LightStep collector
	Disable()
	// Options gets the Options used in New() or NewWithOptions().
	Options() Options
}

// NewTracer returns a new Tracer that reports spans to a LightStep
// collector.
func NewTracer(opts Options) Tracer {
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

	opts.ReconnectPeriod = time.Duration(float64(opts.ReconnectPeriod) * (1 + 0.2*rand.Float64()))
	now := time.Now()
	impl := &tracerImpl{
		opts:       opts,
		reporterID: genSeededGUID(),
		buffer:     newSpansBuffer(opts.MaxBufferedSpans),
		flushing:   newSpansBuffer(opts.MaxBufferedSpans),
	}

	impl.buffer.setCurrent(now)

	if opts.UseThrift {
		impl.client = NewThriftCollectorClient(opts, impl.reporterID, attributes)
	} else {
		impl.client = NewGrpcCollectorClient(opts, impl.reporterID, attributes)
	}

	conn, err := impl.client.ConnectClient()

	if err != nil {
		fmt.Println("Failed to connect to Collector!", err)
		return nil
	}

	impl.conn = conn
	impl.closech = make(chan struct{})

	go impl.reportLoop(impl.closech)

	return impl
}

func FlushLightStepTracer(lsTracer ot.Tracer) error {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	tracer.Flush()
	return nil
}

func GetLightStepAccessToken(lsTracer ot.Tracer) (string, error) {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return "", fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	return tracer.Options().AccessToken, nil
}

func CloseTracer(tracer ot.Tracer) error {
	lsTracer, ok := tracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(tracer))
	}

	return lsTracer.Close()
}

// Implements the `Tracer` interface. Buffers spans and forwards the to a Lightstep collector.
type tracerImpl struct {
	opts             Options
	textPropagator   textMapPropagator
	binaryPropagator lightstepBinaryPropagator

	lock sync.Mutex

	// Note: the following are divided into immutable fields and
	// mutable fields. The mutable fields are modified under `lock`.

	//////////////////////////////////////////////////////////////
	// IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE
	//////////////////////////////////////////////////////////////

	// Note: there may be a desire to update some of these fields
	// at runtime, in which case suitable changes may be needed
	// for variables accessed during Flush.

	reporterID uint64 // the LightStep tracer guid
	//verbose            bool          // whether to print verbose messages
	//maxReportingPeriod time.Duration // set by Options.MaxReportingPeriod
	//reconnectPeriod    time.Duration // set by Options.ReconnectPeriod
	//reportingTimeout   time.Duration // set by Options.ReportTimeout

	// Remote service that will receive reports.
	client  CollectorClient
	conn    Connection
	closech chan struct{}

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
}

func (impl *tracerImpl) Options() Options {
	return impl.opts
}

func (t *tracerImpl) StartSpan(
	operationName string,
	sso ...ot.StartSpanOption,
) ot.Span {
	return newSpan(operationName, t, sso)
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

func (r *tracerImpl) reconnectClient(now time.Time) {
	conn, err := r.client.ConnectClient()
	if err != nil {
		maybeLogInfof("could not reconnect client", r.opts.Verbose)
	} else {
		r.lock.Lock()
		oldConn := r.conn
		r.conn = conn
		r.lock.Unlock()

		oldConn.Close()
		maybeLogInfof("reconnected client connection", r.opts.Verbose)
	}
}

func (r *tracerImpl) Close() error {
	r.lock.Lock()
	conn := r.conn
	closech := r.closech
	r.conn = nil
	r.closech = nil
	r.lock.Unlock()

	if closech != nil {
		close(closech)
	}

	if conn == nil {
		return nil
	}
	return conn.Close()
}

func (r *tracerImpl) RecordSpan(raw RawSpan) {
	r.lock.Lock()
	defer r.lock.Unlock()

	// Early-out for disabled runtimes
	if r.disabled {
		return
	}

	r.buffer.addSpan(raw)

	if r.opts.Recorder != nil {
		r.opts.Recorder.RecordSpan(raw)
	}
}

func (r *tracerImpl) Flush() {
	r.lock.Lock()

	if r.disabled {
		r.lock.Unlock()
		return
	}

	if r.conn == nil {
		maybeLogError(errConnectionWasClosed, r.opts.Verbose)
		r.lock.Unlock()
		return
	}

	if r.reportInFlight == true {
		maybeLogError(errPreviousReportInFlight, r.opts.Verbose)
		r.lock.Unlock()
		return
	}

	// There is not an in-flight report, therefore r.flushing has been reset and
	// is ready to re-use.
	now := time.Now()
	r.buffer, r.flushing = r.flushing, r.buffer
	r.reportInFlight = true
	r.flushing.setFlushing(now)
	r.buffer.setCurrent(now)
	r.lastReportAttempt = now
	r.lock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), r.opts.ReportTimeout)
	defer cancel()
	resp, err := r.client.Report(ctx, &r.flushing)

	if err != nil {
		maybeLogError(err, r.opts.Verbose)
	} else if len(resp.GetErrors()) > 0 {
		// These should never occur, since this library should understand what
		// makes for valid logs and spans, but just in case, log it anyway.
		for _, err := range resp.GetErrors() {
			maybeLogError(fmt.Errorf("Remote report returned error: %s", err), r.opts.Verbose)
		}
	} else {
		maybeLogInfof("Report: resp=%v, err=%v", r.opts.Verbose, resp, err)
	}

	var droppedSent int64
	r.lock.Lock()
	r.reportInFlight = false
	if err != nil {
		// Restore the records that did not get sent correctly
		r.buffer.mergeFrom(&r.flushing)
	} else {
		droppedSent = r.flushing.droppedSpanCount
		r.flushing.clear()
	}
	r.lock.Unlock()

	if droppedSent != 0 {
		maybeLogInfof("client reported %d dropped spans", r.opts.Verbose, droppedSent)
	}

	if resp.Disable() {
		r.Disable()
	}
}

func (r *tracerImpl) Disable() {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.disabled {
		return
	}

	fmt.Printf("Disabling Runtime instance: %p", r)

	r.buffer.clear()
	r.disabled = true
}

// Every minReportingPeriod the reporting loop wakes up and checks to see if
// either (a) the Runtime's max reporting period is about to expire (see
// maxReportingPeriod()), (b) the number of buffered log records is
// approaching kMaxBufferedLogs, or if (c) the number of buffered span records
// is approaching kMaxBufferedSpans. If any of those conditions are true,
// pending data is flushed to the remote peer. If not, the reporting loop waits
// until the next cycle. See Runtime.maybeFlush() for details.
//
// This could alternatively be implemented using flush channels and so forth,
// but that would introduce opportunities for client code to block on the
// runtime library, and we want to avoid that at all costs (even dropping data,
// which can certainly happen with high data rates and/or unresponsive remote
// peers).
func (r *tracerImpl) shouldFlushLocked(now time.Time) bool {
	if now.Add(minReportingPeriod).Sub(r.lastReportAttempt) > r.opts.ReportingPeriod {
		// Flush timeout.
		maybeLogInfof("--> timeout", r.opts.Verbose)
		return true
	} else if r.buffer.isHalfFull() {
		// Too many queued span records.
		maybeLogInfof("--> span queue", r.opts.Verbose)
		return true
	}
	return false
}

func (r *tracerImpl) reportLoop(closech chan struct{}) {
	tickerChan := time.Tick(minReportingPeriod)
	for {
		select {
		case <-tickerChan:
			now := time.Now()

			r.lock.Lock()
			disabled := r.disabled
			reconnect := !r.reportInFlight && r.client.ShouldReconnect()
			shouldFlush := r.shouldFlushLocked(now)
			r.lock.Unlock()

			if disabled {
				return
			}
			if shouldFlush {
				r.Flush()
			}
			if reconnect {
				r.reconnectClient(now)
			}
		case <-closech:
			r.Flush()
			return
		}
	}
}

func getLSCollectorHostPort(opts Options) string {
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

func getLSAPIURL(opts Options) string {
	return getLSURL(opts.LightStepAPI, defaultAPIHost, "")
}

func getLSURL(e Endpoint, host, path string) string {
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
