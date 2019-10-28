package lightstep

import (
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

func b3TraceIDParser(v string) (uint64, error) {
	// handle 128-bit IDs by dropping higher 64 bits
	if len(v) == 32 {
		return strconv.ParseUint(v[16:], 16, 64)
	}
	return strconv.ParseUint(v, 16, 64)
}

func padTraceID(id uint64) string {
	// pad TraceID to 128 bit
	return "0000000000000000" + strconv.FormatUint(id, 16)
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
		traceID:    padTraceID(sc.TraceID),
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
