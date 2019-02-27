package conversions

import (
	"encoding/binary"

	"github.com/lightstep/lightstep-tracer-go"
	"go.opencensus.io/trace"
)

func ConvertTraceID(original trace.TraceID) (uint64, bool) {
	if traceID, n := binary.Uvarint(original[8:]); traceID != 0 && n > 0 {
		return traceID, true
	}
	return 0, false
}

func ConvertSpanID(original trace.SpanID) (uint64, bool) {
	if spanID, n := binary.Uvarint(original[:]); spanID != 0 && n > 0 {
		return spanID, true
	}
	return 0, false
}

func ConvertLinkToSpanContext(link trace.Link) lightstep.SpanContext {
	spanContext := lightstep.SpanContext{
		Baggage: make(map[string]string),
	}

	if traceID, ok := ConvertTraceID(link.TraceID); ok {
		spanContext.TraceID = traceID
	}

	if spanID, ok := ConvertSpanID(link.SpanID); ok {
		spanContext.SpanID = spanID
	}

	for k, v := range link.Attributes {
		if attribute, ok := v.(string); ok {
			spanContext.Baggage[k] = attribute
		}
	}

	return spanContext
}
