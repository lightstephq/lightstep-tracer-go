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
func FlushLightStepTracer(lsTracer ot.Tracer) error {
	tracer, ok := lsTracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(lsTracer))
	}

	tracer.Flush()
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
func CloseTracer(tracer ot.Tracer) error {
	lsTracer, ok := tracer.(Tracer)
	if !ok {
		return fmt.Errorf("Not a LightStep Tracer type: %v", reflect.TypeOf(tracer))
	}

	return lsTracer.Close()
}

// Implements the `Tracer` interface. Buffers spans and forwards the to a Lightstep collector.
type tracerImpl struct {
	//////////////////////////////////////////////////////////////
	// IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE IMMUTABLE
	//////////////////////////////////////////////////////////////

	// Note: there may be a desire to update some of these fields
	// at runtime, in which case suitable changes may be needed
	// for variables accessed during Flush.

	reporterID uint64 // the LightStep tracer guid
	opts       Options

	//////////////////////////////////////////////////////////
	// MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE MUTABLE
	//////////////////////////////////////////////////////////

	// the following fields are modified under `lock`.
	lock sync.Mutex

	// Remote service that will receive reports.
	client            collectorClient
	connection        Connection
	closeChannel      chan struct{}
	reportLoopChannel chan struct{}

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
		return nil
	}

	impl.connection = conn
	impl.closeChannel = make(chan struct{})
	impl.reportLoopChannel = make(chan struct{})

	// Important! incase close is called before go routine is kicked off
	closech := impl.closeChannel
	go func() {
		impl.reportLoop(closech)
		close(impl.reportLoopChannel)
	}()

	return impl
}

func (tracer *tracerImpl) Options() Options {
	return tracer.opts
}

func (tracer *tracerImpl) StartSpan(
	operationName string,
	sso ...ot.StartSpanOption,
) ot.Span {
	return newSpan(operationName, tracer, sso)
}

func (tracer *tracerImpl) Inject(sc ot.SpanContext, format interface{}, carrier interface{}) error {
	switch format {
	case ot.TextMap, ot.HTTPHeaders:
		return theTextMapPropagator.Inject(sc, carrier)
	case BinaryCarrier:
		return theBinaryPropagator.Inject(sc, carrier)
	}
	return ot.ErrUnsupportedFormat
}

func (tracer *tracerImpl) Extract(format interface{}, carrier interface{}) (ot.SpanContext, error) {
	switch format {
	case ot.TextMap, ot.HTTPHeaders:
		return theTextMapPropagator.Extract(carrier)
	case BinaryCarrier:
		return theBinaryPropagator.Extract(carrier)
	}
	return nil, ot.ErrUnsupportedFormat
}

func (tracer *tracerImpl) reconnectClient(now time.Time) {
	conn, err := tracer.client.ConnectClient()
	if err != nil {
		maybeLogInfof("could not reconnect client", tracer.opts.Verbose)
	} else {
		tracer.lock.Lock()
		oldConn := tracer.connection
		tracer.connection = conn
		tracer.lock.Unlock()

		oldConn.Close()
		maybeLogInfof("reconnected client connection", tracer.opts.Verbose)
	}
}

// Close flushes and then terminates the LightStep collector.
func (tracer *tracerImpl) Close() error {
	tracer.lock.Lock()
	closech := tracer.closeChannel
	tracer.closeChannel = nil
	tracer.lock.Unlock()

	if closech != nil {
		// notify report loop that we are closing
		close(closech)

		// wait for report loop to finish
		if tracer.reportLoopChannel != nil {
			<-tracer.reportLoopChannel
		}
	}

	// now its safe to close the connection
	tracer.lock.Lock()
	conn := tracer.connection
	tracer.connection = nil
	tracer.reportLoopChannel = nil
	tracer.lock.Unlock()

	if conn == nil {
		return nil
	}

	return conn.Close()
}

// RecordSpan records a finished Span.
func (tracer *tracerImpl) RecordSpan(raw RawSpan) {
	tracer.lock.Lock()
	defer tracer.lock.Unlock()

	// Early-out for disabled runtimes
	if tracer.disabled {
		return
	}

	tracer.buffer.addSpan(raw)

	if tracer.opts.Recorder != nil {
		tracer.opts.Recorder.RecordSpan(raw)
	}
}

// Flush sends all buffered data to the collector.
func (tracer *tracerImpl) Flush() {
	tracer.flushingLock.Lock()
	defer tracer.flushingLock.Unlock()

	err := tracer.preFlush()
	if err != nil {
		maybeLogError(err, tracer.opts.Verbose)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), tracer.opts.ReportTimeout)
	defer cancel()
	req, err := tracer.client.Translate(ctx, &tracer.flushing)
	resp, err := tracer.client.Report(ctx, req)

	if err != nil {
		maybeLogError(err, tracer.opts.Verbose)
	} else if len(resp.GetErrors()) > 0 {
		// These should never occur, since this library should understand what
		// makes for valid logs and spans, but just in case, log it anyway.
		for _, err := range resp.GetErrors() {
			maybeLogError(fmt.Errorf("Remote report returned error: %s", err), tracer.opts.Verbose)
		}
	} else {
		maybeLogInfof("Report: resp=%v, err=%v", tracer.opts.Verbose, resp, err)
	}

	tracer.postFlush(resp, err)
}

func (tracer *tracerImpl) preFlush() error {
	tracer.lock.Lock()
	defer tracer.lock.Unlock()

	if tracer.disabled {
		return errTracerDisabled
	}

	if tracer.connection == nil {
		return errConnectionWasClosed
	}

	now := time.Now()
	tracer.buffer, tracer.flushing = tracer.flushing, tracer.buffer
	tracer.reportInFlight = true
	tracer.flushing.setFlushing(now)
	tracer.buffer.setCurrent(now)
	tracer.lastReportAttempt = now
	return nil
}

func (tracer *tracerImpl) postFlush(resp collectorResponse, err error) {
	var droppedSent int64
	tracer.lock.Lock()
	defer tracer.lock.Unlock()
	tracer.reportInFlight = false
	if err != nil {
		// Restore the records that did not get sent correctly
		tracer.buffer.mergeFrom(&tracer.flushing)
	} else {
		droppedSent = tracer.flushing.droppedSpanCount
		tracer.flushing.clear()

		if resp.Disable() {
			tracer.Disable()
		}
	}
	if droppedSent != 0 {
		maybeLogInfof("client reported %d dropped spans", tracer.opts.Verbose, droppedSent)
	}
}

func (tracer *tracerImpl) Disable() {
	tracer.lock.Lock()
	defer tracer.lock.Unlock()

	if tracer.disabled {
		return
	}

	fmt.Printf("Disabling Runtime instance: %p", tracer)

	tracer.buffer.clear()
	tracer.disabled = true
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
func (tracer *tracerImpl) shouldFlushLocked(now time.Time) bool {
	if now.Add(tracer.opts.MinReportingPeriod).Sub(tracer.lastReportAttempt) > tracer.opts.ReportingPeriod {
		// Flush timeout.
		maybeLogInfof("--> timeout", tracer.opts.Verbose)
		return true
	} else if tracer.buffer.isHalfFull() {
		// Too many queued span records.
		maybeLogInfof("--> span queue", tracer.opts.Verbose)
		return true
	}
	return false
}

func (tracer *tracerImpl) reportLoop(closech chan struct{}) {
	tickerChan := time.Tick(tracer.opts.MinReportingPeriod)
	for {
		select {
		case <-tickerChan:
			now := time.Now()

			tracer.lock.Lock()
			disabled := tracer.disabled
			reconnect := !tracer.reportInFlight && tracer.client.ShouldReconnect()
			shouldFlush := tracer.shouldFlushLocked(now)
			tracer.lock.Unlock()

			if disabled {
				return
			}
			if shouldFlush {
				tracer.Flush()
			}
			if reconnect {
				tracer.reconnectClient(now)
			}
		case <-closech:
			tracer.Flush()
			return
		}
	}
}
