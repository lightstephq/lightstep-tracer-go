package lightstep

import (
	"fmt"
	"strconv"

	"github.com/opentracing/opentracing-go"
)

const (
	b3Prefix           = "x-b3-"
	b3FieldNameTraceID = b3Prefix + "traceid"
	b3FieldNameSpanID  = b3Prefix + "spanid"
	b3FieldNameSampled = b3Prefix + "sampled"
)

var theB3Propagator b3Propagator

type b3Propagator struct{}

func b3TraceIDParser(v string) (uint64, uint64, error) {
	// handle 128-bit IDs
	if len(v) == 32 {
		traceID, err := strconv.ParseUint(v[:15], 16, 64)
		var lower uint64
		if err != nil {
			return traceID, lower, err
		}
		lower, err = strconv.ParseUint(v[16:], 16, 64)
		return traceID, lower, err
	}
	val, err := strconv.ParseUint(v, 16, 64)
	return val, 0, err
}

func padTraceID(id uint64, lower uint64) string {
	// pad TraceID to 128 bit
	return fmt.Sprintf("%s%016s", strconv.FormatUint(id, 16), strconv.FormatUint(lower, 16))
}

func (b3Propagator) Inject(
	spanContext opentracing.SpanContext,
	opaqueCarrier interface{},
) error {
	sc, ok := spanContext.(SpanContext)
	if !ok {
		return opentracing.ErrInvalidSpanContext
	}
	sample := "1"
	if len(sc.Baggage[b3FieldNameSampled]) > 0 {
		sample = sc.Baggage[b3FieldNameSampled]
	}

	propagator := textMapPropagator{
		traceIDKey: b3FieldNameTraceID,
		traceID:    padTraceID(sc.TraceID, sc.TraceIDLower),
		spanIDKey:  b3FieldNameSpanID,
		spanID:     strconv.FormatUint(sc.SpanID, 16),
		sampledKey: b3FieldNameSampled,
		sampled:    sample,
	}

	return propagator.Inject(spanContext, opaqueCarrier)
}

func (b3Propagator) Extract(
	opaqueCarrier interface{},
) (opentracing.SpanContext, error) {

	propagator := textMapPropagator{
		traceIDKey:   b3FieldNameTraceID,
		spanIDKey:    b3FieldNameSpanID,
		sampledKey:   b3FieldNameSampled,
		parseTraceID: b3TraceIDParser,
	}

	return propagator.Extract(opaqueCarrier)
}
