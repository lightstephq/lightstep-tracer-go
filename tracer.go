package lightstep

import (
	"fmt"
	"reflect"
	"time"

	"golang.org/x/net/context"

	"runtime"
	"sync"

	ot "github.com/opentracing/opentracing-go"
)

var (
	errPreviousReportInFlight = fmt.Errorf("a previous Report is still in flight; aborting Flush()")
	errConnectionWasClosed    = fmt.Errorf("the connection was closed")
	errTracerDisabled         = fmt.Errorf("tracer is disabled; aborting Flush()")
)

// FlushLightStepTracer forces a synchronous Flush.
func FlushLightStepTracer(lsTracer ot.Tracer, ctx ...context.Context) error {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	if len(ctx) > 1 {
		return fmt.Errorf("context vargs are for backwards compatability. Only one context may be provided, got %v", len(ctx))
	}

	flushCtx := context.Background()
	if len(ctx) == 1 {
		flushCtx = ctx[0]
	}

	tracer.Flush(flushCtx)
	return nil
}

// GetLightStepAccessToken returns the currently configured AccessToken.
func GetLightStepAccessToken(lsTracer ot.Tracer) (string, error) {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return "", fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	return tracer.Options().AccessToken, nil
}

// CloseTracer synchronously flushes the tracer, then terminates it.
func CloseTracer(tracer ot.Tracer, ctx ...context.Context) error {
	lsTracer, ok := tracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(tracer))
	}

	if len(ctx) > 1 {
		return fmt.Errorf("context vargs are for backwards compatability. Only one context may be provided, got %v", len(ctx))
	}

	closeCtx := context.Background()
	if len(ctx) == 1 {
		closeCtx = ctx[0]
	}

	lsTracer.Close(closeCtx)
	return nil
}

// Implements the `Tracer` interface. Buffers spans and forwards the to a Lightstep collector.
type tracerImpl struct {
	//////////////////////////////////////////////////////////////
	// IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE
	//////////////////////////////////////////////////////////////

	// Note: there may be a desire to update some of these fields
	// at runtime, in which case suitable changes may be needed
	// for variables accessed during Flush.

	reporterID       uint64 // the LightStep tracer guid
	opts             Options
	textPropagator   textMapPropagator
	binaryPropagator lightstepBinaryPropagator

	//////////////////////////////////////////////////////////
	// MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE
	//////////////////////////////////////////////////////////

	// the following fields are modified under `lock`.
	lock sync.Mutex

	// Remote service that will receive reports.
	client       collectorClient
	conn         Connection
	closech      chan struct{}
	reportLoopch chan struct{}

	// Two buffers of data.
	buffer   reportBuffer
	flushing reportBuffer

	// Flush state.
	flushingLock      sync.Mutex
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

// NewTracer creates and starts a new Lightstep Tracer.
func NewTracer(opts Options) Tracer {
	err := opts.Initialize()
	if err != nil {
		// TODO: don't feel comfortable trying to call opts.OnError here since opts.Initialize failed somehow
		// but shouldn't just return nil. Function should be `func NewTracer(Options) (Tracer, error)`
		fmt.Println(err.Error())
		return nil
	}

	attributes := map[string]string{}
	for k, v := range opts.Tags {
		attributes[k] = fmt.Sprint(v)
	}
	// Don't let the GrpcOptions override these values. That would be confusing.
	attributes[TracerPlatformKey] = TracerPlatformValue
	attributes[TracerPlatformVersionKey] = runtime.Version()
	attributes[TracerVersionKey] = TracerVersionValue

	now := time.Now()
	impl := &tracerImpl{
		opts:       opts,
		reporterID: genSeededGUID(),
		buffer:     newSpansBuffer(opts.MaxBufferedSpans),
		flushing:   newSpansBuffer(opts.MaxBufferedSpans),
	}

	impl.buffer.setCurrent(now)

	if opts.UseThrift {
		impl.client = newThriftCollectorClient(opts, impl.reporterID, attributes)
	} else {
		impl.client = newGrpcCollectorClient(opts, impl.reporterID, attributes)
	}

	conn, err := impl.client.ConnectClient()

	if err != nil {
		fmt.Println("Failed to connect to Collector!", err)
		impl.onError(err)
		// TODO: should we still return impl here even though it failed to connect?
		// See above todo. Should probably return impl and err
		return nil
	}

	impl.conn = conn
	impl.closech = make(chan struct{})
	impl.reportLoopch = make(chan struct{})

	// Important! incase close is called before go routine is kicked off
	closech := impl.closech
	go func() {
		impl.reportLoop(closech)
		close(impl.reportLoopch)
	}()

	return impl
}

func (t *tracerImpl) Options() Options {
	return t.opts
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

func (t *tracerImpl) reconnectClient(now time.Time) {
	conn, err := t.client.ConnectClient()
	if err != nil {
		t.onError(err)
	} else {
		t.lock.Lock()
		oldConn := t.conn
		t.conn = conn
		t.lock.Unlock()

		oldConn.Close()
		maybeLogInfof("reconnected client connection", t.opts.Verbose)
	}
}

// Close flushes and then terminates the LightStep collector.
func (t *tracerImpl) Close(ctx context.Context) {
	t.lock.Lock()
	closech := t.closech
	t.closech = nil
	t.lock.Unlock()

	if closech != nil {
		// notify report loop that we are closing
		close(closech)

		// wait for report loop to finish
		if t.reportLoopch != nil {
			select {
			case <-t.reportLoopch:
				// continue
			case <-ctx.Done():
				// context was canceled, abort
				t.onError(ctx.Err())
				return
			}
		}
	}

	// now its safe to close the connection
	t.lock.Lock()
	conn := t.conn
	t.conn = nil
	t.reportLoopch = nil
	t.lock.Unlock()

	if conn != nil {
		err := conn.Close()
		if err != nil {
			t.onError(err)
		}
	}
}

// RecordSpan records a finished Span.
func (t *tracerImpl) RecordSpan(raw RawSpan) {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Early-out for disabled runtimes
	if t.disabled {
		return
	}

	t.buffer.addSpan(raw)

	if t.opts.Recorder != nil {
		t.opts.Recorder.RecordSpan(raw)
	}
}

// Flush sends all buffered data to the collector.
func (t *tracerImpl) Flush(ctx context.Context) {
	t.flushingLock.Lock()
	defer t.flushingLock.Unlock()

	err := t.preFlush()
	if err != nil {
		t.onError(err)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, t.opts.ReportTimeout)
	defer cancel()
	resp, err := t.client.Report(ctx, &t.flushing)

	if err == nil && len(resp.GetErrors()) > 0 {
		// These should never occur, since this library should understand what
		// makes for valid logs and spans, but just in case, log it anyway.
		for _, err := range resp.GetErrors() {
			maybeLogError(fmt.Errorf("Remote report returned error: %s", err), t.opts.Verbose)
		}
	} else {
		maybeLogInfof("Report: resp=%v, err=%v", t.opts.Verbose, resp, err)
	}

	t.postFlush(resp, err)
}

func (t *tracerImpl) preFlush() error {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.disabled {
		return errTracerDisabled
	}

	if t.conn == nil {
		return errConnectionWasClosed
	}

	now := time.Now()
	t.buffer, t.flushing = t.flushing, t.buffer
	t.reportInFlight = true
	t.flushing.setFlushing(now)
	t.buffer.setCurrent(now)
	t.lastReportAttempt = now
	return nil
}

func (t *tracerImpl) postFlush(resp collectorResponse, err error) {
	var droppedSent int64
	t.lock.Lock()
	defer t.lock.Unlock()
	t.reportInFlight = false
	if err != nil {
		t.onError(err)
		// Restore the records that did not get sent correctly
		t.buffer.mergeFrom(&t.flushing)
	} else {
		droppedSent = t.flushing.droppedSpanCount
		t.flushing.clear()

		if resp.Disable() {
			t.Disable()
		}
	}
	if droppedSent != 0 {
		maybeLogInfof("client reported %d dropped spans", t.opts.Verbose, droppedSent)
	}
}

func (t *tracerImpl) Disable() {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.disabled {
		return
	}

	fmt.Printf("Disabling Runtime instance: %p", t)

	t.buffer.clear()
	t.disabled = true
}

func (t *tracerImpl) onError(err error) {
	maybeLogError(err, t.opts.Verbose)
	if t.opts.OnError != nil {
		t.opts.OnError(err)
	}
}

// Every MinReportingPeriod the reporting loop wakes up and checks to see if
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
func (t *tracerImpl) shouldFlushLocked(now time.Time) bool {
	if now.Add(t.opts.MinReportingPeriod).Sub(t.lastReportAttempt) > t.opts.ReportingPeriod {
		// Flush timeout.
		maybeLogInfof("--> timeout", t.opts.Verbose)
		return true
	} else if t.buffer.isHalfFull() {
		// Too many queued span records.
		maybeLogInfof("--> span queue", t.opts.Verbose)
		return true
	}
	return false
}

func (t *tracerImpl) reportLoop(closech chan struct{}) {
	tickerChan := time.Tick(t.opts.MinReportingPeriod)
	for {
		select {
		case <-tickerChan:
			now := time.Now()

			t.lock.Lock()
			disabled := t.disabled
			reconnect := !t.reportInFlight && t.client.ShouldReconnect()
			shouldFlush := t.shouldFlushLocked(now)
			t.lock.Unlock()

			if disabled {
				return
			}
			if shouldFlush {
				t.Flush(context.Background())
			}
			if reconnect {
				t.reconnectClient(now)
			}
		case <-closech:
			t.Flush(context.Background())
			return
		}
	}
}
