package lightstep

import (
	"sync"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

// Implements the `Span` interface. Created via tracerImpl (see
// `New()`).
type SpanImpl struct {
	tracer     *tracerImpl
	sync.Mutex // protects the fields below
	finished   bool
	raw        RawSpan
	// The number of logs dropped because of MaxLogsPerSpan.
	numDroppedLogs int
}

func newSpan(operationName string, tracer *tracerImpl, sso []opentracing.StartSpanOption) *SpanImpl {
	opts := newStartSpanOptions(sso)

	// Start time.
	startTime := opts.Options.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	// Build the new span. This is the only allocation: We'll return this as
	// an opentracing.Span.
	sp := &SpanImpl{}

	// It's meaningless to provide either SpanID or ParentSpanID
	// without also providing TraceID, so just test for TraceID.
	if opts.SetTraceID != 0 {
		sp.raw.Context.TraceID = opts.SetTraceID
		sp.raw.Context.SpanID = opts.SetSpanID
		sp.raw.ParentSpanID = opts.SetParentSpanID
	}

	if opts.SetRawSpan != nil {
		sp.raw = *opts.SetRawSpan
	}

	// Look for a parent in the list of References.
	//
	// TODO: would be nice if we did something with all References, not just
	//       the first one.
ReferencesLoop:
	for _, ref := range opts.Options.References {
		switch ref.Type {
		case opentracing.ChildOfRef, opentracing.FollowsFromRef:
			refCtx, ok := ref.ReferencedContext.(SpanContext)
			if !ok {
				break ReferencesLoop
			}
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

	sp.tracer = tracer
	if opts.SetRawSpan == nil {
		sp.raw.Operation = operationName
		sp.raw.Start = startTime
		sp.raw.Duration = -1
		sp.raw.Tags = opts.Options.Tags
	}

	if tracer.opts.MetaEventReportingEnabled && !sp.IsMeta() {
		opentracing.StartSpan(LSMetaEvent_SpanStartOperation,
			opentracing.Tag{Key: LSMetaEvent_MetaEventKey, Value: true},
			opentracing.Tag{Key: LSMetaEvent_SpanIdKey, Value: sp.raw.Context.SpanID},
			opentracing.Tag{Key: LSMetaEvent_TraceIdKey, Value: sp.raw.Context.TraceID}).
			Finish()
	}
	return sp
}

func (s *SpanImpl) SetOperationName(operationName string) opentracing.Span {
	s.Lock()
	defer s.Unlock()

	if s.finished {
		return s
	}

	s.raw.Operation = operationName
	return s
}

func (s *SpanImpl) SetTag(key string, value interface{}) opentracing.Span {
	s.Lock()
	defer s.Unlock()

	if s.finished {
		return s
	}

	if s.raw.Tags == nil {
		s.raw.Tags = opentracing.Tags{}
	}
	s.raw.Tags[key] = value
	return s
}

func (s *SpanImpl) LogKV(keyValues ...interface{}) {
	fields, err := log.InterleavedKVToFields(keyValues...)
	if err != nil {
		s.LogFields(log.Error(err), log.String("function", "LogKV"))
		return
	}
	s.LogFields(fields...)
}

func (s *SpanImpl) appendLog(lr opentracing.LogRecord) {
	maxLogs := s.tracer.opts.MaxLogsPerSpan
	if maxLogs == 0 || len(s.raw.Logs) < maxLogs {
		s.raw.Logs = append(s.raw.Logs, lr)
		return
	}

	// We have too many logs. We don't touch the first numOld logs; we treat the
	// rest as a circular buffer and overwrite the oldest log among those.
	numOld := (maxLogs - 1) / 2
	numNew := maxLogs - numOld
	s.raw.Logs[numOld+s.numDroppedLogs%numNew] = lr
	s.numDroppedLogs++
}

func (s *SpanImpl) LogFields(fields ...log.Field) {
	s.Lock()
	defer s.Unlock()

	if s.finished || s.tracer.opts.DropSpanLogs {
		return
	}

	lr := opentracing.LogRecord{
		Fields: fields,
	}
	if lr.Timestamp.IsZero() {
		lr.Timestamp = time.Now()
	}
	s.appendLog(lr)
}

func (s *SpanImpl) LogEvent(event string) {
	s.Log(opentracing.LogData{
		Event: event,
	})
}

func (s *SpanImpl) LogEventWithPayload(event string, payload interface{}) {
	s.Log(opentracing.LogData{
		Event:   event,
		Payload: payload,
	})
}

func (s *SpanImpl) Log(ld opentracing.LogData) {
	s.Lock()
	defer s.Unlock()

	if s.finished || s.tracer.opts.DropSpanLogs {
		return
	}

	if ld.Timestamp.IsZero() {
		ld.Timestamp = time.Now()
	}

	s.appendLog(ld.ToLogRecord())
}

func (s *SpanImpl) Finish() {
	s.FinishWithOptions(opentracing.FinishOptions{})
}

// rotateLogBuffer rotates the records in the buffer: records 0 to pos-1 move at
// the end (i.e. pos circular left shifts).
func rotateLogBuffer(buf []opentracing.LogRecord, pos int) {
	// This algorithm is described in:
	//    http://www.cplusplus.com/reference/algorithm/rotate
	for first, middle, next := 0, pos, pos; first != middle; {
		buf[first], buf[next] = buf[next], buf[first]
		first++
		next++
		if next == len(buf) {
			next = middle
		} else if first == middle {
			middle = next
		}
	}
}

func (s *SpanImpl) FinishWithOptions(opts opentracing.FinishOptions) {
	s.Lock()
	defer s.Unlock()

	if s.finished {
		return
	}

	s.finished = true

	finishTime := opts.FinishTime
	if finishTime.IsZero() {
		finishTime = time.Now()
	}
	duration := finishTime.Sub(s.raw.Start)

	for _, lr := range opts.LogRecords {
		s.appendLog(lr)
	}
	for _, ld := range opts.BulkLogData {
		s.appendLog(ld.ToLogRecord())
	}

	if s.numDroppedLogs > 0 {
		// We dropped some log events, which means that we used part of Logs as a
		// circular buffer (see appendLog). De-circularize it.
		numOld := (len(s.raw.Logs) - 1) / 2
		numNew := len(s.raw.Logs) - numOld
		rotateLogBuffer(s.raw.Logs[numOld:], s.numDroppedLogs%numNew)

		// Replace the log in the middle (the oldest "new" log) with information
		// about the dropped logs. This means that we are effectively dropping one
		// more "new" log.
		numDropped := s.numDroppedLogs + 1
		s.raw.Logs[numOld] = opentracing.LogRecord{
			// Keep the timestamp of the last dropped event.
			Timestamp: s.raw.Logs[numOld].Timestamp,
			Fields: []log.Field{
				log.String("event", "dropped Span logs"),
				log.Int("dropped_log_count", numDropped),
				log.String("component", "basictracer"),
			},
		}
	}

	s.raw.Duration = duration

	s.tracer.RecordSpan(s.raw)
	if s.tracer.opts.MetaEventReportingEnabled && !s.IsMeta() {
		opentracing.StartSpan(LSMetaEvent_SpanFinishOperation,
			opentracing.Tag{Key: LSMetaEvent_MetaEventKey, Value: true},
			opentracing.Tag{Key: LSMetaEvent_SpanIdKey, Value: s.raw.Context.SpanID},
			opentracing.Tag{Key: LSMetaEvent_TraceIdKey, Value: s.raw.Context.TraceID}).
			Finish()
	}
}

func (s *SpanImpl) Tracer() opentracing.Tracer {
	return s.tracer
}

func (s *SpanImpl) Context() opentracing.SpanContext {
	return s.raw.Context
}

func (s *SpanImpl) SetBaggageItem(key, val string) opentracing.Span {
	s.Lock()
	defer s.Unlock()

	if s.finished {
		return s
	}

	s.raw.Context = s.raw.Context.WithBaggageItem(key, val)
	return s
}

func (s *SpanImpl) BaggageItem(key string) string {
	s.Lock()
	defer s.Unlock()
	return s.raw.Context.Baggage[key]
}

func (s *SpanImpl) Operation() string {
	return s.raw.Operation
}

func (s *SpanImpl) Start() time.Time {
	return s.raw.Start
}

func (s *SpanImpl) IsMeta() bool {
	return s.raw.Tags["lightstep.meta_event"] != nil
}

func (s *SpanImpl) Raw() RawSpan {
	return s.raw
}
